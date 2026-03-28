package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bedrockproxy/internal/config"
	"bedrockproxy/internal/store"
)

// newTestRouter creates a Router with a real store and nil proxy.
// The proxy is nil so we cannot test proxy endpoints, but all
// /api/* dashboard endpoints work fine.
func newTestRouter(models ...config.ModelConfig) (*Router, *store.Store) {
	s := store.New(models)
	events := NewEventBus()
	r := NewRouter(s, nil, events)
	return r, s
}

func TestHealthEndpoint(t *testing.T) {
	router, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
}

func TestUsageSummaryEndpoint(t *testing.T) {
	router, s := newTestRouter()

	s.RecordRequest(store.Request{
		CallerARN:    "arn:aws:sts::111111111111:assumed-role/R/s",
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.01,
		CreatedAt:    time.Now().UTC(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/usage/summary?minutes=60", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body store.Summary
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", body.TotalRequests)
	}
	if body.TotalInputTokens != 100 {
		t.Errorf("TotalInputTokens = %d, want 100", body.TotalInputTokens)
	}
}

func TestUsageSummaryEndpoint_DefaultMinutes(t *testing.T) {
	router, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/usage/summary", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestCallersEndpoint(t *testing.T) {
	router, s := newTestRouter()

	s.RecordRequest(store.Request{
		CallerARN: "arn:aws:sts::111111111111:assumed-role/R/s",
		CostUSD:   0.05,
		CreatedAt: time.Now().UTC(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/usage/callers?minutes=60", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []store.CallerStats
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(body) != 1 {
		t.Errorf("expected 1 caller, got %d", len(body))
	}
}

func TestCallersEndpoint_EmptyReturnsArray(t *testing.T) {
	router, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/usage/callers?minutes=60", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// Must be a JSON array, not null.
	body := strings.TrimSpace(w.Body.String())
	if body != "[]" {
		t.Errorf("expected empty array '[]', got %q", body)
	}
}

func TestActivityEndpoint(t *testing.T) {
	router, s := newTestRouter()

	s.RecordRequest(store.Request{
		CallerARN:    "arn:aws:sts::111111111111:assumed-role/R/s",
		ModelID:      "model-a",
		Operation:    "Converse",
		InputTokens:  100,
		OutputTokens: 50,
		StatusCode:   200,
		CreatedAt:    time.Now().UTC(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/usage/activity?limit=10", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(body))
	}

	// Verify expected fields exist in the response.
	expectedFields := []string{"id", "caller", "model_id", "operation", "input_tokens", "output_tokens", "cost_usd", "latency_ms", "status_code", "created_at"}
	for _, field := range expectedFields {
		if _, ok := body[0][field]; !ok {
			t.Errorf("missing field %q in activity response", field)
		}
	}
}

func TestActivityEndpoint_EmptyReturnsArray(t *testing.T) {
	router, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/usage/activity?limit=10", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := strings.TrimSpace(w.Body.String())
	if body != "[]" {
		t.Errorf("expected empty array '[]', got %q", body)
	}
}

func TestModelsEndpoint(t *testing.T) {
	models := []config.ModelConfig{
		{ID: "anthropic.claude-3", Name: "Claude 3", InputPricePerMillion: 3.0, OutputPricePerMillion: 15.0, Enabled: true},
		{ID: "anthropic.claude-3-haiku", Name: "Claude 3 Haiku", InputPricePerMillion: 0.25, OutputPricePerMillion: 1.25, Enabled: true},
	}
	router, _ := newTestRouter(models...)

	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []store.Model
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("expected 2 models, got %d", len(body))
	}
	if body[0].ID != "anthropic.claude-3" {
		t.Errorf("models[0].ID = %q, want %q", body[0].ID, "anthropic.claude-3")
	}
}

func TestModelsEndpoint_EmptyReturnsArray(t *testing.T) {
	router, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := strings.TrimSpace(w.Body.String())
	if body != "[]" {
		t.Errorf("expected empty array '[]', got %q", body)
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

func TestQueryInt(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		key        string
		defaultVal int
		want       int
	}{
		{name: "present", query: "limit=25", key: "limit", defaultVal: 50, want: 25},
		{name: "missing uses default", query: "", key: "limit", defaultVal: 50, want: 50},
		{name: "invalid uses default", query: "limit=abc", key: "limit", defaultVal: 50, want: 50},
		{name: "zero value", query: "limit=0", key: "limit", defaultVal: 50, want: 0},
		{name: "negative value", query: "limit=-1", key: "limit", defaultVal: 50, want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.query != "" {
				url += "?" + tt.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			got := queryInt(req, tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("queryInt = %d, want %d", got, tt.want)
			}
		})
	}
}
