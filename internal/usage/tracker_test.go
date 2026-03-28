package usage

import (
	"math"
	"sync"
	"testing"
	"time"

	"bedrockproxy/internal/config"
	"bedrockproxy/internal/store"
)

func newTestStore() *store.Store {
	// Create store without calling store.New to avoid file I/O.
	// We build it the same way store_test.go does.
	return store.New(nil)
}

func testModels() []config.ModelConfig {
	return []config.ModelConfig{
		{
			ID:                    "anthropic.claude-3-sonnet",
			Name:                  "Claude 3 Sonnet",
			InputPricePerMillion:  3.0,
			OutputPricePerMillion: 15.0,
			Enabled:               true,
		},
		{
			ID:                    "anthropic.claude-3-haiku",
			Name:                  "Claude 3 Haiku",
			InputPricePerMillion:  0.25,
			OutputPricePerMillion: 1.25,
			Enabled:               true,
		},
	}
}

func TestCalculateCost_ExactMatch(t *testing.T) {
	s := newTestStore()
	tracker := NewTracker(s, testModels())

	cost := tracker.calculateCost("anthropic.claude-3-sonnet", 1_000_000, 1_000_000)

	// input: 1M * 3.0 / 1M = 3.0
	// output: 1M * 15.0 / 1M = 15.0
	// total = 18.0
	want := 18.0
	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}

func TestCalculateCost_SmallTokenCounts(t *testing.T) {
	s := newTestStore()
	tracker := NewTracker(s, testModels())

	cost := tracker.calculateCost("anthropic.claude-3-haiku", 500, 200)

	// input: 500 * 0.25 / 1_000_000 = 0.000125
	// output: 200 * 1.25 / 1_000_000 = 0.000250
	// total = 0.000375
	want := 0.000375
	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}

func TestCalculateCost_EUPrefix(t *testing.T) {
	s := newTestStore()
	tracker := NewTracker(s, testModels())

	// "eu.anthropic.claude-3-sonnet" should strip "eu." and match "anthropic.claude-3-sonnet".
	cost := tracker.calculateCost("eu.anthropic.claude-3-sonnet", 1000, 500)

	inputCost := 1000.0 * 3.0 / 1_000_000
	outputCost := 500.0 * 15.0 / 1_000_000
	want := inputCost + outputCost

	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}

func TestCalculateCost_USPrefix(t *testing.T) {
	s := newTestStore()
	tracker := NewTracker(s, testModels())

	// "us.anthropic.claude-3-haiku" should strip "us." (idx=2, < 4).
	cost := tracker.calculateCost("us.anthropic.claude-3-haiku", 1000, 1000)

	inputCost := 1000.0 * 0.25 / 1_000_000
	outputCost := 1000.0 * 1.25 / 1_000_000
	want := inputCost + outputCost

	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}

func TestCalculateCost_UnknownModel(t *testing.T) {
	s := newTestStore()
	tracker := NewTracker(s, testModels())

	cost := tracker.calculateCost("totally.unknown.model", 1000, 500)
	if cost != 0 {
		t.Errorf("cost = %f, want 0 for unknown model", cost)
	}
}

func TestCalculateCost_UnknownModelNoPrefix(t *testing.T) {
	s := newTestStore()
	tracker := NewTracker(s, testModels())

	cost := tracker.calculateCost("unknown-model", 1000, 500)
	if cost != 0 {
		t.Errorf("cost = %f, want 0 for unknown model without prefix", cost)
	}
}

func TestCalculateCost_ZeroTokens(t *testing.T) {
	s := newTestStore()
	tracker := NewTracker(s, testModels())

	cost := tracker.calculateCost("anthropic.claude-3-sonnet", 0, 0)
	if cost != 0 {
		t.Errorf("cost = %f, want 0 for zero tokens", cost)
	}
}

func TestRecord_CreatesCallerAndRequest(t *testing.T) {
	s := newTestStore()
	tracker := NewTracker(s, testModels())

	// record is synchronous (unlike Record which spawns a goroutine).
	tracker.record(Request{
		AccessKeyID:  "AKID_TRACKER",
		ModelID:      "anthropic.claude-3-sonnet",
		Operation:    "Converse",
		InputTokens:  1000,
		OutputTokens: 500,
		StatusCode:   200,
	})

	// Verify request was stored.
	summary := s.GetSummary(time.Now().Add(-1 * time.Hour))
	if summary.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", summary.TotalRequests)
	}
	if summary.TotalInputTokens != 1000 {
		t.Errorf("TotalInputTokens = %d, want 1000", summary.TotalInputTokens)
	}
	if summary.TotalOutputTokens != 500 {
		t.Errorf("TotalOutputTokens = %d, want 500", summary.TotalOutputTokens)
	}
	if summary.TotalCostUSD == 0 {
		t.Error("TotalCostUSD should be non-zero for known model")
	}
}

func TestRecord_NotifyCalled(t *testing.T) {
	s := newTestStore()
	tracker := NewTracker(s, testModels())

	var mu sync.Mutex
	notifyCalled := false
	tracker.Notify = func() {
		mu.Lock()
		notifyCalled = true
		mu.Unlock()
	}

	tracker.record(Request{
		AccessKeyID: "AKID1",
		ModelID:     "anthropic.claude-3-sonnet",
		StatusCode:  200,
	})

	mu.Lock()
	defer mu.Unlock()
	if !notifyCalled {
		t.Error("Notify callback was not called")
	}
}

func TestRecord_NoNotifyWhenNil(t *testing.T) {
	s := newTestStore()
	tracker := NewTracker(s, testModels())
	// Notify is nil by default; this should not panic.
	tracker.record(Request{
		AccessKeyID: "AKID1",
		ModelID:     "anthropic.claude-3-sonnet",
		StatusCode:  200,
	})
}

func TestUpdatePrices_AddsNewModels(t *testing.T) {
	s := newTestStore()
	tracker := NewTracker(s, testModels())

	// New model not in initial config.
	tracker.UpdatePrices([]config.ModelConfig{
		{
			ID:                    "meta.llama-3",
			Name:                  "Llama 3",
			InputPricePerMillion:  1.0,
			OutputPricePerMillion: 2.0,
		},
	})

	cost := tracker.calculateCost("meta.llama-3", 1_000_000, 1_000_000)
	// input: 1M * 1.0 / 1M = 1.0, output: 1M * 2.0 / 1M = 2.0, total = 3.0
	want := 3.0
	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}

func TestUpdatePrices_DoesNotOverwriteExisting(t *testing.T) {
	s := newTestStore()
	tracker := NewTracker(s, testModels())

	// Try to overwrite an existing model's pricing.
	tracker.UpdatePrices([]config.ModelConfig{
		{
			ID:                    "anthropic.claude-3-sonnet",
			Name:                  "Claude 3 Sonnet Overridden",
			InputPricePerMillion:  999.0,
			OutputPricePerMillion: 999.0,
		},
	})

	cost := tracker.calculateCost("anthropic.claude-3-sonnet", 1_000_000, 1_000_000)
	// Should still use original pricing: 3.0 + 15.0 = 18.0
	want := 18.0
	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("cost = %f, want %f (existing should not be overwritten)", cost, want)
	}
}
