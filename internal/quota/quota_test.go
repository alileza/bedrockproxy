package quota

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"bedrockproxy/internal/store"
)

func newTestStore() *store.Store {
	return store.New(nil)
}

func newTestEngine(s *store.Store) *Engine {
	return &Engine{
		quotas: make(map[string]Quota),
		store:  s,
	}
}

func TestMatchSpecificity_Wildcard(t *testing.T) {
	spec := matchSpecificity("*", "arn:aws:sts::123:assumed-role/R/s", "123")
	if spec != 1 {
		t.Errorf("wildcard specificity = %d, want 1", spec)
	}
}

func TestMatchSpecificity_AccountPrefix(t *testing.T) {
	spec := matchSpecificity("account:123", "arn:aws:sts::123:assumed-role/R/s", "123")
	if spec != 2 {
		t.Errorf("account match specificity = %d, want 2", spec)
	}

	spec = matchSpecificity("account:999", "arn:aws:sts::123:assumed-role/R/s", "123")
	if spec != 0 {
		t.Errorf("non-matching account specificity = %d, want 0", spec)
	}
}

func TestMatchSpecificity_GlobARN(t *testing.T) {
	spec := matchSpecificity("arn:aws:sts::123:*", "arn:aws:sts::123:assumed-role/R/s", "123")
	if spec != 3 {
		t.Errorf("glob ARN specificity = %d, want 3", spec)
	}

	spec = matchSpecificity("arn:aws:sts::999:*", "arn:aws:sts::123:assumed-role/R/s", "123")
	if spec != 0 {
		t.Errorf("non-matching glob specificity = %d, want 0", spec)
	}
}

func TestMatchSpecificity_NoMatch(t *testing.T) {
	spec := matchSpecificity("account:999", "", "123")
	if spec != 0 {
		t.Errorf("no-match specificity = %d, want 0", spec)
	}
}

func TestCheck_UnderLimit(t *testing.T) {
	s := newTestStore()
	e := newTestEngine(s)

	// Set up a caller
	c := s.EnsureCaller("AKID1")
	c.AccountID = "123"
	c.RoleARN = "arn:aws:sts::123:assumed-role/Role/s"

	// Record some usage
	s.RecordRequest(store.Request{
		AccessKeyID:  "AKID1",
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.01,
		CreatedAt:    time.Now().UTC(),
	})

	e.SetQuota(Quota{
		ID:           "q1",
		Match:        "account:123",
		TokensPerDay: 10000,
		CostPerDay:   100.0,
		Mode:         ModeReject,
		Enabled:      true,
	})

	result := e.Check("arn:aws:sts::123:assumed-role/Role/s", "123")
	if !result.Allowed {
		t.Errorf("expected allowed, got blocked: %s", result.Reason)
	}
}

func TestCheck_TokensExceeded(t *testing.T) {
	s := newTestStore()
	e := newTestEngine(s)

	c := s.EnsureCaller("AKID1")
	c.AccountID = "123"
	c.RoleARN = "arn:aws:sts::123:assumed-role/Role/s"

	s.RecordRequest(store.Request{
		AccessKeyID:  "AKID1",
		InputTokens:  5000,
		OutputTokens: 5001,
		CostUSD:      0.01,
		CreatedAt:    time.Now().UTC(),
	})

	e.SetQuota(Quota{
		ID:           "q1",
		Match:        "account:123",
		TokensPerDay: 10000,
		Mode:         ModeReject,
		Enabled:      true,
	})

	result := e.Check("arn:aws:sts::123:assumed-role/Role/s", "123")
	if result.Allowed {
		t.Error("expected blocked for token limit exceeded")
	}
	if result.Reason != "daily token limit exceeded" {
		t.Errorf("reason = %q, want 'daily token limit exceeded'", result.Reason)
	}
}

func TestCheck_CostExceeded(t *testing.T) {
	s := newTestStore()
	e := newTestEngine(s)

	c := s.EnsureCaller("AKID1")
	c.AccountID = "123"
	c.RoleARN = "arn:aws:sts::123:assumed-role/Role/s"

	s.RecordRequest(store.Request{
		AccessKeyID: "AKID1",
		CostUSD:     51.0,
		CreatedAt:   time.Now().UTC(),
	})

	e.SetQuota(Quota{
		ID:         "q1",
		Match:      "account:123",
		CostPerDay: 50.0,
		Mode:       ModeReject,
		Enabled:    true,
	})

	result := e.Check("arn:aws:sts::123:assumed-role/Role/s", "123")
	if result.Allowed {
		t.Error("expected blocked for cost limit exceeded")
	}
	if result.Reason != "daily cost limit exceeded" {
		t.Errorf("reason = %q, want 'daily cost limit exceeded'", result.Reason)
	}
}

func TestCheck_RequestsPerMinuteExceeded(t *testing.T) {
	s := newTestStore()
	e := newTestEngine(s)

	c := s.EnsureCaller("AKID1")
	c.AccountID = "123"
	c.RoleARN = "arn:aws:sts::123:assumed-role/Role/s"

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		s.RecordRequest(store.Request{
			AccessKeyID: "AKID1",
			CreatedAt:   now.Add(-time.Duration(i) * time.Second),
		})
	}

	e.SetQuota(Quota{
		ID:                "q1",
		Match:             "account:123",
		RequestsPerMinute: 5,
		Mode:              ModeReject,
		Enabled:           true,
	})

	result := e.Check("arn:aws:sts::123:assumed-role/Role/s", "123")
	if result.Allowed {
		t.Error("expected blocked for requests per minute exceeded")
	}
	if result.Reason != "requests per minute limit exceeded" {
		t.Errorf("reason = %q, want 'requests per minute limit exceeded'", result.Reason)
	}
}

func TestCheck_WarnModeAllows(t *testing.T) {
	s := newTestStore()
	e := newTestEngine(s)

	c := s.EnsureCaller("AKID1")
	c.AccountID = "123"
	c.RoleARN = "arn:aws:sts::123:assumed-role/Role/s"

	s.RecordRequest(store.Request{
		AccessKeyID: "AKID1",
		CostUSD:     51.0,
		CreatedAt:   time.Now().UTC(),
	})

	e.SetQuota(Quota{
		ID:         "q1",
		Match:      "account:123",
		CostPerDay: 50.0,
		Mode:       ModeWarn,
		Enabled:    true,
	})

	// Check still returns not-allowed, but the proxy handler uses GetMode to decide behavior.
	result := e.Check("arn:aws:sts::123:assumed-role/Role/s", "123")
	if result.Allowed {
		t.Error("check should still report exceeded even in warn mode")
	}

	mode := e.GetMode(result.QuotaID)
	if mode != ModeWarn {
		t.Errorf("mode = %q, want %q", mode, ModeWarn)
	}
}

func TestCheck_DisabledQuotaSkipped(t *testing.T) {
	s := newTestStore()
	e := newTestEngine(s)

	c := s.EnsureCaller("AKID1")
	c.AccountID = "123"
	c.RoleARN = "arn:aws:sts::123:assumed-role/Role/s"

	s.RecordRequest(store.Request{
		AccessKeyID: "AKID1",
		CostUSD:     51.0,
		CreatedAt:   time.Now().UTC(),
	})

	e.SetQuota(Quota{
		ID:         "q1",
		Match:      "account:123",
		CostPerDay: 50.0,
		Mode:       ModeReject,
		Enabled:    false, // disabled
	})

	result := e.Check("arn:aws:sts::123:assumed-role/Role/s", "123")
	if !result.Allowed {
		t.Error("disabled quota should not block requests")
	}
}

func TestCheck_NoQuotas(t *testing.T) {
	s := newTestStore()
	e := newTestEngine(s)

	result := e.Check("arn:aws:sts::123:assumed-role/Role/s", "123")
	if !result.Allowed {
		t.Error("no quotas configured should allow all requests")
	}
}

func TestCheck_BestMatchWins(t *testing.T) {
	s := newTestStore()
	e := newTestEngine(s)

	c := s.EnsureCaller("AKID1")
	c.AccountID = "123"
	c.RoleARN = "arn:aws:sts::123:assumed-role/Role/s"

	s.RecordRequest(store.Request{
		AccessKeyID: "AKID1",
		CostUSD:     51.0,
		CreatedAt:   time.Now().UTC(),
	})

	// Wildcard quota with high limit (should not be used)
	e.quotas["q-wildcard"] = Quota{
		ID:         "q-wildcard",
		Match:      "*",
		CostPerDay: 1000.0,
		Mode:       ModeReject,
		Enabled:    true,
	}

	// Account-specific quota with low limit (should win)
	e.quotas["q-account"] = Quota{
		ID:         "q-account",
		Match:      "account:123",
		CostPerDay: 50.0,
		Mode:       ModeReject,
		Enabled:    true,
	}

	result := e.Check("arn:aws:sts::123:assumed-role/Role/s", "123")
	if result.Allowed {
		t.Error("account-specific quota should have blocked this")
	}
	if result.QuotaID != "q-account" {
		t.Errorf("quota_id = %q, want 'q-account'", result.QuotaID)
	}
}

func TestSetQuota_And_DeleteQuota(t *testing.T) {
	s := newTestStore()
	e := newTestEngine(s)

	e.SetQuota(Quota{ID: "q1", Match: "*", Enabled: true})
	e.SetQuota(Quota{ID: "q2", Match: "account:123", Enabled: true})

	quotas := e.GetQuotas()
	if len(quotas) != 2 {
		t.Fatalf("expected 2 quotas, got %d", len(quotas))
	}

	e.DeleteQuota("q1")
	quotas = e.GetQuotas()
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota after delete, got %d", len(quotas))
	}
	if quotas[0].ID != "q2" {
		t.Errorf("remaining quota ID = %q, want 'q2'", quotas[0].ID)
	}

	// Clean up disk file
	os.Remove(quotaFile)
}

func TestPersistence_SaveAndLoad(t *testing.T) {
	// Clean up before and after
	os.Remove(quotaFile)
	defer os.Remove(quotaFile)

	s := newTestStore()
	e := newTestEngine(s)

	e.SetQuota(Quota{
		ID:           "q1",
		Match:        "account:123",
		TokensPerDay: 50000,
		CostPerDay:   25.0,
		Mode:         ModeReject,
		Enabled:      true,
	})

	// Verify file was written
	data, err := os.ReadFile(quotaFile)
	if err != nil {
		t.Fatalf("failed to read quota file: %v", err)
	}

	var loaded []Quota
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to parse quota file: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 quota in file, got %d", len(loaded))
	}
	if loaded[0].ID != "q1" {
		t.Errorf("loaded quota ID = %q, want 'q1'", loaded[0].ID)
	}

	// Create a new engine and verify it loads from disk
	e2 := NewEngine(s, nil)
	quotas := e2.GetQuotas()
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota loaded from disk, got %d", len(quotas))
	}
	if quotas[0].Match != "account:123" {
		t.Errorf("loaded match = %q, want 'account:123'", quotas[0].Match)
	}
	if quotas[0].TokensPerDay != 50000 {
		t.Errorf("loaded TokensPerDay = %d, want 50000", quotas[0].TokensPerDay)
	}
}

func TestGetQuotasWithUsage(t *testing.T) {
	s := newTestStore()
	e := newTestEngine(s)

	c := s.EnsureCaller("AKID1")
	c.AccountID = "123"
	c.RoleARN = "arn:aws:sts::123:assumed-role/Role/s"

	s.RecordRequest(store.Request{
		AccessKeyID:  "AKID1",
		InputTokens:  200,
		OutputTokens: 100,
		CostUSD:      0.05,
		CreatedAt:    time.Now().UTC(),
	})

	e.quotas["q1"] = Quota{
		ID:           "q1",
		Match:        "account:123",
		TokensPerDay: 10000,
		CostPerDay:   100.0,
		Mode:         ModeWarn,
		Enabled:      true,
	}

	result := e.GetQuotasWithUsage()
	if len(result) != 1 {
		t.Fatalf("expected 1 quota with usage, got %d", len(result))
	}

	if result[0].TokensUsedToday != 300 {
		t.Errorf("TokensUsedToday = %d, want 300", result[0].TokensUsedToday)
	}
	if result[0].CostUsedToday != 0.05 {
		t.Errorf("CostUsedToday = %f, want 0.05", result[0].CostUsedToday)
	}
}

func TestExtractMatchTarget(t *testing.T) {
	tests := []struct {
		match       string
		wantAccount string
		wantARN     string
	}{
		{"*", "*", "*"},
		{"account:123", "123", ""},
		{"arn:aws:sts::123:*", "", "arn:aws:sts::123:*"},
	}

	for _, tt := range tests {
		account, arn := extractMatchTarget(tt.match)
		if account != tt.wantAccount || arn != tt.wantARN {
			t.Errorf("extractMatchTarget(%q) = (%q, %q), want (%q, %q)",
				tt.match, account, arn, tt.wantAccount, tt.wantARN)
		}
	}
}
