package usage

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"bedrockproxy/internal/config"
	"bedrockproxy/internal/metrics"
	"bedrockproxy/internal/store"
)

// Request represents a single proxied Bedrock call.
type Request struct {
	AccessKeyID  string
	ModelID      string
	Operation    string
	InputTokens  int
	OutputTokens int
	LatencyMs    int
	StatusCode   int
	ErrorMessage string
}

// Tracker records usage to the in-memory store.
type Tracker struct {
	store  *store.Store
	prices map[string]config.ModelConfig
	mu     sync.RWMutex
	Notify func()
}

func NewTracker(s *store.Store, models []config.ModelConfig) *Tracker {
	prices := make(map[string]config.ModelConfig, len(models))
	for _, m := range models {
		prices[m.ID] = m
	}
	return &Tracker{store: s, prices: prices}
}

// UpdatePrices merges new model pricing into the tracker. Existing entries
// are not overwritten (config takes precedence).
func (t *Tracker) UpdatePrices(models []config.ModelConfig) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, m := range models {
		if _, exists := t.prices[m.ID]; !exists {
			t.prices[m.ID] = m
		}
	}
}

func (t *Tracker) Record(_ context.Context, req Request) {
	go t.record(req)
}

func (t *Tracker) record(req Request) {
	costUSD := t.calculateCost(req.ModelID, req.InputTokens, req.OutputTokens)

	t.store.RecordRequest(store.Request{
		AccessKeyID:  req.AccessKeyID,
		ModelID:      req.ModelID,
		Operation:    req.Operation,
		InputTokens:  req.InputTokens,
		OutputTokens: req.OutputTokens,
		CostUSD:      costUSD,
		LatencyMs:    req.LatencyMs,
		StatusCode:   req.StatusCode,
		ErrorMessage: req.ErrorMessage,
	})

	// Prometheus metrics
	status := fmt.Sprintf("%d", req.StatusCode)
	metrics.RequestsTotal.WithLabelValues(req.ModelID, req.Operation, status).Inc()
	metrics.RequestDuration.WithLabelValues(req.ModelID, req.Operation).Observe(float64(req.LatencyMs) / 1000)
	metrics.InputTokensTotal.WithLabelValues(req.ModelID, req.AccessKeyID).Add(float64(req.InputTokens))
	metrics.OutputTokensTotal.WithLabelValues(req.ModelID, req.AccessKeyID).Add(float64(req.OutputTokens))
	if costUSD > 0 {
		metrics.CostTotal.WithLabelValues(req.ModelID, req.AccessKeyID).Add(costUSD)
	}

	if t.Notify != nil {
		t.Notify()
	}
}

func (t *Tracker) calculateCost(modelID string, inputTokens, outputTokens int) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	m, ok := t.prices[modelID]
	if !ok {
		// Try stripping region prefix: eu.anthropic.claude-... -> anthropic.claude-...
		stripped := modelID
		if idx := strings.Index(modelID, "."); idx != -1 && idx < 4 {
			stripped = modelID[idx+1:]
		}
		m, ok = t.prices[stripped]
		if !ok {
			return 0
		}
	}
	inputCost := float64(inputTokens) * m.InputPricePerMillion / 1_000_000
	outputCost := float64(outputTokens) * m.OutputPricePerMillion / 1_000_000
	return inputCost + outputCost
}
