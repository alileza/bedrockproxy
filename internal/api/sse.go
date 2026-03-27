package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// EventBus manages SSE client connections and broadcasts events.
type EventBus struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{
		clients: make(map[chan []byte]struct{}),
	}
}

// Broadcast sends an event to all connected SSE clients.
func (eb *EventBus) Broadcast(eventType string, data any) {
	payload, err := json.Marshal(map[string]any{
		"type": eventType,
		"data": data,
	})
	if err != nil {
		return
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()
	for ch := range eb.clients {
		select {
		case ch <- payload:
		default:
			// Drop message if client is slow.
		}
	}
}

// NotifyFunc returns a callback suitable for usage.Tracker.Notify.
func (eb *EventBus) NotifyFunc() func() {
	return func() {
		eb.Broadcast("request", map[string]string{"event": "new_request"})
	}
}

func (eb *EventBus) subscribe() chan []byte {
	ch := make(chan []byte, 16)
	eb.mu.Lock()
	eb.clients[ch] = struct{}{}
	eb.mu.Unlock()
	return ch
}

func (eb *EventBus) unsubscribe(ch chan []byte) {
	eb.mu.Lock()
	delete(eb.clients, ch)
	eb.mu.Unlock()
	close(ch)
}

// HandleSSE is the HTTP handler for GET /api/events.
func (eb *EventBus) HandleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := eb.subscribe()
	defer eb.unsubscribe(ch)

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
