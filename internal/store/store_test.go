package store

import (
	"math"
	"sync"
	"testing"
	"time"

	"bedrockproxy/internal/config"
)

func floatEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func newTestStore(models ...config.ModelConfig) *Store {
	s := &Store{
		callers: make(map[string]*Caller),
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, m := range models {
		s.models = append(s.models, Model{
			ID:                    m.ID,
			Name:                  m.Name,
			InputPricePerMillion:  m.InputPricePerMillion,
			OutputPricePerMillion: m.OutputPricePerMillion,
			Enabled:               m.Enabled,
			CreatedAt:             now,
		})
	}
	return s
}

func TestRecordRequest(t *testing.T) {
	s := newTestStore()

	s.RecordRequest(Request{
		CallerARN:    "arn:aws:sts::111111111111:assumed-role/RoleA/session",
		ModelID:      "model-a",
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.01,
	})

	s.RecordRequest(Request{
		CallerARN:    "arn:aws:sts::222222222222:assumed-role/RoleB/session",
		ModelID:      "model-b",
		InputTokens:  200,
		OutputTokens: 100,
		CostUSD:      0.02,
	})

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(s.requests))
	}

	if s.requests[0].ID != 1 {
		t.Errorf("first request ID = %d, want 1", s.requests[0].ID)
	}
	if s.requests[1].ID != 2 {
		t.Errorf("second request ID = %d, want 2", s.requests[1].ID)
	}

	if s.requests[0].CreatedAt.IsZero() {
		t.Error("first request CreatedAt should be set automatically")
	}
}

func TestRecordRequest_PreservesCreatedAt(t *testing.T) {
	s := newTestStore()
	fixedTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	s.RecordRequest(Request{
		CallerARN: "arn:aws:sts::111111111111:assumed-role/R/s",
		CreatedAt: fixedTime,
	})

	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.requests[0].CreatedAt.Equal(fixedTime) {
		t.Errorf("CreatedAt = %v, want %v", s.requests[0].CreatedAt, fixedTime)
	}
}

func TestEnsureCaller(t *testing.T) {
	s := newTestStore()

	c1 := s.EnsureCaller("arn:aws:sts::111111111111:assumed-role/R/s")
	if c1 == nil {
		t.Fatal("EnsureCaller returned nil for new caller")
	}
	if c1.ARN != "arn:aws:sts::111111111111:assumed-role/R/s" {
		t.Errorf("ARN = %q, want %q", c1.ARN, "arn:aws:sts::111111111111:assumed-role/R/s")
	}
	if c1.FirstSeenAt.IsZero() {
		t.Error("FirstSeenAt should be set")
	}

	// Calling again returns the same instance.
	c2 := s.EnsureCaller("arn:aws:sts::111111111111:assumed-role/R/s")
	if c1 != c2 {
		t.Error("EnsureCaller should return the same pointer for existing caller")
	}

	// Different ARN creates a different caller.
	c3 := s.EnsureCaller("arn:aws:sts::222222222222:assumed-role/R/s")
	if c3 == c1 {
		t.Error("different ARN should create different caller")
	}
}

func TestGetSummary(t *testing.T) {
	s := newTestStore()
	now := time.Now().UTC()

	s.RecordRequest(Request{
		CallerARN:    "arn:aws:sts::111111111111:assumed-role/R/s",
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.01,
		CreatedAt:    now.Add(-10 * time.Minute),
	})
	s.RecordRequest(Request{
		CallerARN:    "arn:aws:sts::222222222222:assumed-role/R/s",
		InputTokens:  200,
		OutputTokens: 100,
		CostUSD:      0.02,
		CreatedAt:    now.Add(-5 * time.Minute),
	})
	// Old request that should be filtered out.
	s.RecordRequest(Request{
		CallerARN:    "arn:aws:sts::333333333333:assumed-role/R/s",
		InputTokens:  999,
		OutputTokens: 999,
		CostUSD:      9.99,
		CreatedAt:    now.Add(-2 * time.Hour),
	})

	summary := s.GetSummary(now.Add(-30 * time.Minute))

	if summary.TotalRequests != 2 {
		t.Errorf("TotalRequests = %d, want 2", summary.TotalRequests)
	}
	if summary.TotalInputTokens != 300 {
		t.Errorf("TotalInputTokens = %d, want 300", summary.TotalInputTokens)
	}
	if summary.TotalOutputTokens != 150 {
		t.Errorf("TotalOutputTokens = %d, want 150", summary.TotalOutputTokens)
	}
	if !floatEqual(summary.TotalCostUSD, 0.03) {
		t.Errorf("TotalCostUSD = %f, want 0.03", summary.TotalCostUSD)
	}
	if summary.UniqueCallers != 2 {
		t.Errorf("UniqueCallers = %d, want 2", summary.UniqueCallers)
	}
}

func TestGetSummary_Empty(t *testing.T) {
	s := newTestStore()
	summary := s.GetSummary(time.Now().Add(-1 * time.Hour))

	if summary.TotalRequests != 0 {
		t.Errorf("TotalRequests = %d, want 0", summary.TotalRequests)
	}
}

func TestGetCallers(t *testing.T) {
	s := newTestStore()
	now := time.Now().UTC()

	arn1 := "arn:aws:sts::111111111111:assumed-role/RoleA/session"
	arn2 := "arn:aws:sts::222222222222:assumed-role/RoleB/session"

	s.RecordRequest(Request{
		CallerARN: arn1,
		CostUSD:   0.10,
		CreatedAt: now,
	})
	s.RecordRequest(Request{
		CallerARN: arn1,
		CostUSD:   0.05,
		CreatedAt: now,
	})

	s.RecordRequest(Request{
		CallerARN: arn2,
		CostUSD:   0.20,
		CreatedAt: now,
	})

	callers := s.GetCallers(now.Add(-1 * time.Hour))

	if len(callers) != 2 {
		t.Fatalf("expected 2 caller stats, got %d", len(callers))
	}

	// Sorted by cost descending, so arn2 should be first.
	if !floatEqual(callers[0].TotalCostUSD, 0.20) {
		t.Errorf("callers[0].TotalCostUSD = %f, want 0.20", callers[0].TotalCostUSD)
	}
	if callers[0].TotalRequests != 1 {
		t.Errorf("callers[0].TotalRequests = %d, want 1", callers[0].TotalRequests)
	}
	if callers[0].AccountID != "222222222222" {
		t.Errorf("callers[0].AccountID = %q, want %q", callers[0].AccountID, "222222222222")
	}
	if !floatEqual(callers[1].TotalCostUSD, 0.15) {
		t.Errorf("callers[1].TotalCostUSD = %f, want 0.15", callers[1].TotalCostUSD)
	}
	if callers[1].TotalRequests != 2 {
		t.Errorf("callers[1].TotalRequests = %d, want 2", callers[1].TotalRequests)
	}
}

func TestGetCallers_UnknownCaller(t *testing.T) {
	s := newTestStore()
	now := time.Now().UTC()

	// Record a request with an ARN that has no parseable account.
	s.RecordRequest(Request{
		CallerARN: "unknown-caller",
		CostUSD:   0.05,
		CreatedAt: now,
	})

	callers := s.GetCallers(now.Add(-1 * time.Hour))
	if len(callers) != 1 {
		t.Fatalf("expected 1 caller stat, got %d", len(callers))
	}
	if callers[0].AccountID != "unknown" {
		t.Errorf("AccountID = %q, want %q", callers[0].AccountID, "unknown")
	}
}

func TestGetActivity(t *testing.T) {
	s := newTestStore()
	now := time.Now().UTC()

	for i := 0; i < 5; i++ {
		s.RecordRequest(Request{
			CallerARN: "arn:aws:sts::111111111111:assumed-role/R/s",
			ModelID:   "model-a",
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}

	// Limit fewer than total.
	activity := s.GetActivity(3)
	if len(activity) != 3 {
		t.Fatalf("expected 3 activities, got %d", len(activity))
	}

	// Newest first: IDs should be 5, 4, 3 (descending).
	if activity[0].ID != 5 {
		t.Errorf("activity[0].ID = %d, want 5", activity[0].ID)
	}
	if activity[1].ID != 4 {
		t.Errorf("activity[1].ID = %d, want 4", activity[1].ID)
	}
	if activity[2].ID != 3 {
		t.Errorf("activity[2].ID = %d, want 3", activity[2].ID)
	}
}

func TestGetActivity_LimitExceedsTotal(t *testing.T) {
	s := newTestStore()

	s.RecordRequest(Request{CallerARN: "arn:aws:sts::111111111111:assumed-role/R/s"})

	activity := s.GetActivity(100)
	if len(activity) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activity))
	}
}

func TestGetActivity_ShowsCallerARN(t *testing.T) {
	s := newTestStore()

	arn := "arn:aws:iam::123456789012:role/TestRole"
	s.RecordRequest(Request{CallerARN: arn})

	activity := s.GetActivity(1)
	if activity[0].CallerARN != arn {
		t.Errorf("CallerARN = %q, want %q", activity[0].CallerARN, arn)
	}
}

func TestFlushRequests(t *testing.T) {
	s := newTestStore()

	s.RecordRequest(Request{CallerARN: "arn:aws:sts::111111111111:assumed-role/R/s"})
	s.RecordRequest(Request{CallerARN: "arn:aws:sts::222222222222:assumed-role/R/s"})

	flushed := s.FlushRequests()
	if len(flushed) != 2 {
		t.Fatalf("expected 2 flushed requests, got %d", len(flushed))
	}

	// After flush, internal slice is cleared.
	s.mu.RLock()
	remaining := len(s.requests)
	s.mu.RUnlock()

	if remaining != 0 {
		t.Errorf("expected 0 remaining requests after flush, got %d", remaining)
	}

	// Second flush returns nil.
	flushed2 := s.FlushRequests()
	if flushed2 != nil {
		t.Errorf("expected nil from second flush, got %d items", len(flushed2))
	}
}

func TestGetModels(t *testing.T) {
	models := []config.ModelConfig{
		{ID: "anthropic.claude-3", Name: "Claude 3", InputPricePerMillion: 3.0, OutputPricePerMillion: 15.0, Enabled: true},
		{ID: "anthropic.claude-3-haiku", Name: "Claude 3 Haiku", InputPricePerMillion: 0.25, OutputPricePerMillion: 1.25, Enabled: true},
	}
	s := newTestStore(models...)

	got := s.GetModels()
	if len(got) != 2 {
		t.Fatalf("expected 2 models, got %d", len(got))
	}
	if got[0].ID != "anthropic.claude-3" {
		t.Errorf("models[0].ID = %q, want %q", got[0].ID, "anthropic.claude-3")
	}
	if got[1].Name != "Claude 3 Haiku" {
		t.Errorf("models[1].Name = %q, want %q", got[1].Name, "Claude 3 Haiku")
	}
}

func TestGetModels_ReturnsDefensiveCopy(t *testing.T) {
	s := newTestStore(config.ModelConfig{ID: "m1", Name: "Model 1"})

	got := s.GetModels()
	got[0].ID = "mutated"

	original := s.GetModels()
	if original[0].ID != "m1" {
		t.Error("GetModels did not return a defensive copy")
	}
}

func TestConcurrentWrites(t *testing.T) {
	s := newTestStore()

	const goroutines = 50
	const requestsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < requestsPerGoroutine; i++ {
				s.RecordRequest(Request{
					CallerARN:    "arn:aws:sts::111111111111:assumed-role/Concurrent/s",
					ModelID:      "model-a",
					InputTokens:  1,
					OutputTokens: 1,
					CostUSD:      0.001,
				})
			}
		}(g)
	}

	wg.Wait()

	s.mu.RLock()
	total := len(s.requests)
	s.mu.RUnlock()

	expected := goroutines * requestsPerGoroutine
	if total != expected {
		t.Errorf("total requests = %d, want %d", total, expected)
	}

	// Verify IDs are unique.
	s.mu.RLock()
	idSet := make(map[int64]struct{}, total)
	for _, r := range s.requests {
		if _, dup := idSet[r.ID]; dup {
			t.Errorf("duplicate ID %d", r.ID)
		}
		idSet[r.ID] = struct{}{}
	}
	s.mu.RUnlock()

	if len(idSet) != expected {
		t.Errorf("unique IDs = %d, want %d", len(idSet), expected)
	}
}

func TestConcurrentEnsureCaller(t *testing.T) {
	s := newTestStore()

	const goroutines = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	results := make([]*Caller, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = s.EnsureCaller("arn:aws:sts::111111111111:assumed-role/Shared/s")
		}(i)
	}

	wg.Wait()

	// All goroutines should get the same caller pointer.
	for i := 1; i < goroutines; i++ {
		if results[i] != results[0] {
			t.Fatalf("goroutine %d got different caller pointer", i)
		}
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	s := newTestStore()
	now := time.Now().UTC()

	// Pre-populate some data.
	for i := 0; i < 10; i++ {
		s.RecordRequest(Request{
			CallerARN: "arn:aws:sts::111111111111:assumed-role/Pre/s",
			CostUSD:   0.01,
			CreatedAt: now,
		})
	}

	var wg sync.WaitGroup
	const workers = 20

	// Writers.
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				s.RecordRequest(Request{
					CallerARN: "arn:aws:sts::111111111111:assumed-role/W/s",
					CostUSD:   0.001,
					CreatedAt: now,
				})
			}
		}()
	}

	// Readers.
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = s.GetSummary(now.Add(-1 * time.Hour))
				_ = s.GetCallers(now.Add(-1 * time.Hour))
				_ = s.GetActivity(10)
			}
		}()
	}

	wg.Wait()
}

func TestUpdateModels(t *testing.T) {
	s := newTestStore(config.ModelConfig{ID: "m1", Name: "Model 1"})

	// Verify initial state.
	if got := s.GetModels(); len(got) != 1 {
		t.Fatalf("expected 1 model initially, got %d", len(got))
	}

	// Update with new models.
	s.UpdateModels([]Model{
		{ID: "m1", Name: "Model 1 Updated"},
		{ID: "m2", Name: "Model 2"},
	})

	got := s.GetModels()
	if len(got) != 2 {
		t.Fatalf("expected 2 models after update, got %d", len(got))
	}
	if got[0].Name != "Model 1 Updated" {
		t.Errorf("models[0].Name = %q, want %q", got[0].Name, "Model 1 Updated")
	}
	if got[1].ID != "m2" {
		t.Errorf("models[1].ID = %q, want %q", got[1].ID, "m2")
	}
}

func TestUpdateModels_Empty(t *testing.T) {
	s := newTestStore(config.ModelConfig{ID: "m1", Name: "Model 1"})

	s.UpdateModels(nil)

	got := s.GetModels()
	if len(got) != 0 {
		t.Errorf("expected 0 models after nil update, got %d", len(got))
	}
}

func TestExtractAccountFromARN(t *testing.T) {
	tests := []struct {
		name string
		arn  string
		want string
	}{
		{
			name: "valid STS ARN",
			arn:  "arn:aws:sts::123456789012:assumed-role/MyRole/session",
			want: "123456789012",
		},
		{
			name: "valid IAM ARN",
			arn:  "arn:aws:iam::987654321098:role/AdminRole",
			want: "987654321098",
		},
		{
			name: "short invalid ARN",
			arn:  "arn:aws:sts",
			want: "",
		},
		{
			name: "empty string",
			arn:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAccountFromARN(tt.arn)
			if got != tt.want {
				t.Errorf("extractAccountFromARN(%q) = %q, want %q", tt.arn, got, tt.want)
			}
		})
	}
}

func TestCallerMatchesLocked(t *testing.T) {
	s := newTestStore()

	// Test wildcard match
	if !s.callerMatchesLocked("arn:aws:sts::123:assumed-role/R/s", "*", "*") {
		t.Error("wildcard should match everything")
	}

	// Test account match
	if !s.callerMatchesLocked("arn:aws:sts::123:assumed-role/R/s", "123", "") {
		t.Error("account match should work")
	}

	// Test ARN match
	if !s.callerMatchesLocked("arn:aws:sts::123:assumed-role/R/s", "", "arn:aws:sts::123:assumed-role/R/s") {
		t.Error("exact ARN match should work")
	}

	// Test non-matching
	if s.callerMatchesLocked("arn:aws:sts::123:assumed-role/R/s", "999", "") {
		t.Error("non-matching account should not match")
	}
}
