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
		AccessKeyID:  "AKID1",
		ModelID:      "model-a",
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.01,
	})

	s.RecordRequest(Request{
		AccessKeyID:  "AKID2",
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
		AccessKeyID: "AKID1",
		CreatedAt:   fixedTime,
	})

	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.requests[0].CreatedAt.Equal(fixedTime) {
		t.Errorf("CreatedAt = %v, want %v", s.requests[0].CreatedAt, fixedTime)
	}
}

func TestEnsureCaller(t *testing.T) {
	s := newTestStore()

	c1 := s.EnsureCaller("AKID1")
	if c1 == nil {
		t.Fatal("EnsureCaller returned nil for new caller")
	}
	if c1.AccessKeyID != "AKID1" {
		t.Errorf("AccessKeyID = %q, want %q", c1.AccessKeyID, "AKID1")
	}
	if c1.FirstSeenAt.IsZero() {
		t.Error("FirstSeenAt should be set")
	}

	// Calling again returns the same instance.
	c2 := s.EnsureCaller("AKID1")
	if c1 != c2 {
		t.Error("EnsureCaller should return the same pointer for existing caller")
	}

	// Different key creates a different caller.
	c3 := s.EnsureCaller("AKID2")
	if c3 == c1 {
		t.Error("different access key should create different caller")
	}
}

func TestUpdateCallerARN_Propagation(t *testing.T) {
	s := newTestStore()

	// Set up two callers in the same account.
	c1 := s.EnsureCaller("AKID1")
	c1.AccountID = "111111111111"

	c2 := s.EnsureCaller("AKID2")
	c2.AccountID = "111111111111"

	// Update ARN for the first caller; should propagate to sibling.
	s.UpdateCallerARN("AKID1", "arn:aws:sts::111111111111:assumed-role/MyRole/session")

	s.mu.RLock()
	defer s.mu.RUnlock()

	if c1.RoleARN != "arn:aws:sts::111111111111:assumed-role/MyRole/session" {
		t.Errorf("c1 RoleARN = %q, want the set ARN", c1.RoleARN)
	}
	if c2.RoleARN != "arn:aws:sts::111111111111:assumed-role/MyRole/session" {
		t.Errorf("c2 RoleARN = %q, want propagated ARN", c2.RoleARN)
	}
}

func TestUpdateCallerARN_NoPropagationToDifferentAccount(t *testing.T) {
	s := newTestStore()

	c1 := s.EnsureCaller("AKID1")
	c1.AccountID = "111111111111"

	c2 := s.EnsureCaller("AKID2")
	c2.AccountID = "222222222222"

	s.UpdateCallerARN("AKID1", "arn:aws:sts::111111111111:assumed-role/MyRole/session")

	s.mu.RLock()
	defer s.mu.RUnlock()

	if c2.RoleARN != "" {
		t.Errorf("c2 RoleARN = %q, want empty (different account)", c2.RoleARN)
	}
}

func TestGetSummary(t *testing.T) {
	s := newTestStore()
	now := time.Now().UTC()

	s.RecordRequest(Request{
		AccessKeyID:  "AKID1",
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.01,
		CreatedAt:    now.Add(-10 * time.Minute),
	})
	s.RecordRequest(Request{
		AccessKeyID:  "AKID2",
		InputTokens:  200,
		OutputTokens: 100,
		CostUSD:      0.02,
		CreatedAt:    now.Add(-5 * time.Minute),
	})
	// Old request that should be filtered out.
	s.RecordRequest(Request{
		AccessKeyID:  "AKID3",
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

	c := s.EnsureCaller("AKID1")
	c.AccountID = "111111111111"
	c.RoleARN = "arn:aws:sts::111111111111:assumed-role/RoleA/session"

	s.RecordRequest(Request{
		AccessKeyID: "AKID1",
		CostUSD:     0.10,
		CreatedAt:   now,
	})
	s.RecordRequest(Request{
		AccessKeyID: "AKID1",
		CostUSD:     0.05,
		CreatedAt:   now,
	})

	c2 := s.EnsureCaller("AKID2")
	c2.AccountID = "222222222222"
	c2.RoleARN = "arn:aws:sts::222222222222:assumed-role/RoleB/session"

	s.RecordRequest(Request{
		AccessKeyID: "AKID2",
		CostUSD:     0.20,
		CreatedAt:   now,
	})

	callers := s.GetCallers(now.Add(-1 * time.Hour))

	if len(callers) != 2 {
		t.Fatalf("expected 2 caller stats, got %d", len(callers))
	}

	// Sorted by cost descending, so AKID2's group should be first.
	if !floatEqual(callers[0].TotalCostUSD, 0.20) {
		t.Errorf("callers[0].TotalCostUSD = %f, want 0.20", callers[0].TotalCostUSD)
	}
	if callers[0].TotalRequests != 1 {
		t.Errorf("callers[0].TotalRequests = %d, want 1", callers[0].TotalRequests)
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

	// Record a request without pre-creating the caller via EnsureCaller+account info.
	// RecordRequest calls ensureCallerLocked, but account/role will be empty.
	s.RecordRequest(Request{
		AccessKeyID: "AKIDUNKNOWN",
		CostUSD:     0.05,
		CreatedAt:   now,
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
			AccessKeyID: "AKID1",
			ModelID:     "model-a",
			CreatedAt:   now.Add(time.Duration(i) * time.Minute),
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

	s.RecordRequest(Request{AccessKeyID: "AKID1"})

	activity := s.GetActivity(100)
	if len(activity) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activity))
	}
}

func TestGetActivity_EnrichesWithRoleARN(t *testing.T) {
	s := newTestStore()

	c := s.EnsureCaller("AKID1")
	c.RoleARN = "arn:aws:iam::123456789012:role/TestRole"

	s.RecordRequest(Request{AccessKeyID: "AKID1"})

	activity := s.GetActivity(1)
	if activity[0].AccessKeyID != "arn:aws:iam::123456789012:role/TestRole" {
		t.Errorf("AccessKeyID = %q, want enriched ARN", activity[0].AccessKeyID)
	}
}

func TestGetActivity_EnrichesWithAccountID(t *testing.T) {
	s := newTestStore()

	c := s.EnsureCaller("AKID1")
	c.AccountID = "123456789012"

	s.RecordRequest(Request{AccessKeyID: "AKID1"})

	activity := s.GetActivity(1)
	want := "arn:aws:iam::123456789012:access-key/AKID1"
	if activity[0].AccessKeyID != want {
		t.Errorf("AccessKeyID = %q, want %q", activity[0].AccessKeyID, want)
	}
}

func TestFlushRequests(t *testing.T) {
	s := newTestStore()

	s.RecordRequest(Request{AccessKeyID: "AKID1"})
	s.RecordRequest(Request{AccessKeyID: "AKID2"})

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
					AccessKeyID:  "AKID_concurrent",
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
			results[idx] = s.EnsureCaller("SHARED_KEY")
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
			AccessKeyID: "AKID_PRE",
			CostUSD:     0.01,
			CreatedAt:   now,
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
					AccessKeyID: "AKID_W",
					CostUSD:     0.001,
					CreatedAt:   now,
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

func TestUpdateCallerAccount(t *testing.T) {
	s := newTestStore()

	s.EnsureCaller("AKID1")
	s.UpdateCallerAccount("AKID1", "123456789012")

	acct := s.GetCallerAccountID("AKID1")
	if acct != "123456789012" {
		t.Errorf("GetCallerAccountID = %q, want %q", acct, "123456789012")
	}

	// Should not overwrite an existing account ID.
	s.UpdateCallerAccount("AKID1", "999999999999")
	acct = s.GetCallerAccountID("AKID1")
	if acct != "123456789012" {
		t.Errorf("account ID should not be overwritten, got %q", acct)
	}
}

func TestFindARNByAccount(t *testing.T) {
	s := newTestStore()

	c := s.EnsureCaller("AKID1")
	c.AccountID = "111111111111"
	c.RoleARN = "arn:aws:sts::111111111111:assumed-role/MyRole/session"

	arn := s.FindARNByAccount("111111111111")
	if arn != "arn:aws:sts::111111111111:assumed-role/MyRole/session" {
		t.Errorf("FindARNByAccount = %q, want the stored ARN", arn)
	}

	// Non-existent account returns empty.
	arn = s.FindARNByAccount("999999999999")
	if arn != "" {
		t.Errorf("FindARNByAccount for unknown account = %q, want empty", arn)
	}
}
