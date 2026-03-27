package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// EventBus manages WebSocket client connections and broadcasts events.
type EventBus struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{
		clients: make(map[*websocket.Conn]struct{}),
	}
}

// Broadcast sends an event to all connected WebSocket clients.
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
	for conn := range eb.clients {
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			conn.Close()
			delete(eb.clients, conn)
		}
	}
}

// NotifyFunc returns a callback suitable for usage.Tracker.Notify.
func (eb *EventBus) NotifyFunc() func() {
	return func() {
		eb.Broadcast("request", map[string]string{"event": "new_request"})
	}
}

// HandleWS is the HTTP handler for GET /api/ws.
func (eb *EventBus) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	eb.mu.Lock()
	eb.clients[conn] = struct{}{}
	eb.mu.Unlock()

	// Set up ping/pong for connection health
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		return nil
	})

	// Ping ticker to keep connection alive and detect dead clients
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				break
			}
		}
	}()

	// Read loop — just consume messages (client shouldn't send any, but we need to read to detect close)
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}

	eb.mu.Lock()
	delete(eb.clients, conn)
	eb.mu.Unlock()
	conn.Close()
}
