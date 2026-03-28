package usage

import (
	"context"
	"strings"
	"sync"

	"bedrockproxy/internal/config"
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
