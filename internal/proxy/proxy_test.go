package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"

	"bedrockproxy/internal/config"
	"bedrockproxy/internal/quota"
	"bedrockproxy/internal/store"
	"bedrockproxy/internal/usage"
)

// staticCreds implements aws.CredentialsProvider for testing.
type staticCreds struct{}

func (staticCreds) Retrieve(ctx context.Context) (aws.Credentials, error) {
	return aws.Credentials{
		AccessKeyID:     "AKIAPROXYTEST1234567",
		SecretAccessKey: "proxy-secret-key-for-testing-only",
	}, nil
}

const testCallerARN = "arn:aws:sts::123456789012:assumed-role/TestRole/session"

// newTestProxy creates a proxy pointed at the given test server.
func newTestProxy(t *testing.T, backend *httptest.Server, models ...config.ModelConfig) (*Proxy, *store.Store) {
	t.Helper()
	s := store.New(models)
	tracker := usage.NewTracker(s, models)

	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}

	return &Proxy{
		target:  target,
		signer:  v4.NewSigner(),
		creds:   staticCreds{},
		tracker: tracker,
		region:  "us-east-1",
		client:  backend.Client(),
	}, s
}

// newTestProxyWithQuota creates a proxy with a quota engine.
// Uses NewTestEngine to avoid disk file interference.
func newTestProxyWithQuota(t *testing.T, backend *httptest.Server, quotas []quota.Quota) (*Proxy, *store.Store) {
	t.Helper()
	s := store.New(nil)
	tracker := usage.NewTracker(s, nil)

	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}

	// Build quota engine directly (bypass disk loading).
	qe := quota.NewTestEngine(s)
	for _, q := range quotas {
		qe.SetQuota(q)
	}

	return &Proxy{
		target:   target,
		signer:   v4.NewSigner(),
		creds:    staticCreds{},
		tracker:  tracker,
		region:   "us-east-1",
		client:   backend.Client(),
		quotaEng: qe,
	}, s
}

// callerQuery returns a query string with the caller param set.
func callerQuery(callerARN string) string {
	return "?caller=" + url.QueryEscape(callerARN)
}

// ---- Tests ----

func TestParsePathInfo(t *testing.T) {
	tests := []struct {
		path      string
		wantModel string
		wantOp    string
	}{
		{"/model/anthropic.claude-3-sonnet/converse", "anthropic.claude-3-sonnet", "converse"},
		{"/model/anthropic.claude-3-sonnet/converse-stream", "anthropic.claude-3-sonnet", "converse-stream"},
		{"/model/anthropic.claude-3-sonnet/invoke", "anthropic.claude-3-sonnet", "invoke"},
		{"/model/anthropic.claude-3-sonnet/invoke-with-response-stream", "anthropic.claude-3-sonnet", "invoke-with-response-stream"},
		{"/model/meta.llama3-70b-instruct-v1:0/converse", "meta.llama3-70b-instruct-v1:0", "converse"},
		{"/model/anthropic.claude-3-sonnet/count-tokens", "anthropic.claude-3-sonnet", "count-tokens"},
		{"/guardrail/gr-abc123/version/1/apply", "", "guardrail"},
		{"/async-invoke", "", "async-invoke"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			model, op := parsePathInfo(tt.path)
			if model != tt.wantModel {
				t.Errorf("modelID = %q, want %q", model, tt.wantModel)
			}
			if op != tt.wantOp {
				t.Errorf("operation = %q, want %q", op, tt.wantOp)
			}
		})
	}
}

func TestIsStreamingResponse(t *testing.T) {
	tests := []struct {
		name string
		path string
		ct   string
		want bool
	}{
		{"converse-stream path", "/model/x/converse-stream", "application/json", true},
		{"invoke-with-response-stream path", "/model/x/invoke-with-response-stream", "application/json", true},
		{"event stream content type", "/model/x/invoke", "application/vnd.amazon.eventstream", true},
		{"regular converse", "/model/x/converse", "application/json", false},
		{"regular invoke", "/model/x/invoke", "application/json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{}}
			resp.Header.Set("Content-Type", tt.ct)
			got := isStreamingResponse(tt.path, resp)
			if got != tt.want {
				t.Errorf("isStreamingResponse = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractTokenCounts_ConverseFormat(t *testing.T) {
	body := `{"output":{"message":{"role":"assistant","content":[{"text":"Hello"}]}},"usage":{"inputTokens":100,"outputTokens":50}}`
	input, output := extractTokenCounts([]byte(body))
	if input != 100 {
		t.Errorf("inputTokens = %d, want 100", input)
	}
	if output != 50 {
		t.Errorf("outputTokens = %d, want 50", output)
	}
}

func TestExtractTokenCounts_MessagesAPIFormat(t *testing.T) {
	body := `{"id":"msg_123","type":"message","role":"assistant","content":[{"type":"text","text":"Hi"}],"usage":{"input_tokens":200,"output_tokens":75}}`
	input, output := extractTokenCounts([]byte(body))
	if input != 200 {
		t.Errorf("inputTokens = %d, want 200", input)
	}
	if output != 75 {
		t.Errorf("outputTokens = %d, want 75", output)
	}
}

func TestExtractTokenCounts_InvalidJSON(t *testing.T) {
	input, output := extractTokenCounts([]byte("not json"))
	if input != 0 || output != 0 {
		t.Errorf("expected 0/0 for invalid JSON, got %d/%d", input, output)
	}
}

func TestExtractTokenCounts_NoUsage(t *testing.T) {
	body := `{"result":"something"}`
	input, output := extractTokenCounts([]byte(body))
	if input != 0 || output != 0 {
		t.Errorf("expected 0/0 for no usage, got %d/%d", input, output)
	}
}

func TestHeaderInt(t *testing.T) {
	h := http.Header{}
	h.Set("X-Token-Count", "42")
	h.Set("X-Bad", "abc")

	if got := headerInt(h, "X-Token-Count"); got != 42 {
		t.Errorf("got %d, want 42", got)
	}
	if got := headerInt(h, "X-Bad"); got != 0 {
		t.Errorf("got %d, want 0 for non-integer", got)
	}
	if got := headerInt(h, "X-Missing"); got != 0 {
		t.Errorf("got %d, want 0 for missing", got)
	}
}

func TestHashPayload(t *testing.T) {
	h := hashPayload([]byte("hello"))
	if len(h) != 64 {
		t.Errorf("hash length = %d, want 64 hex chars", len(h))
	}
	// SHA256 of "hello" is well-known.
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if h != want {
		t.Errorf("hash = %q, want %q", h, want)
	}
}

func TestHandleProxy_ForwardPath(t *testing.T) {
	var receivedPath string
	var receivedMethod string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"output": map[string]any{"message": map[string]any{"role": "assistant"}},
			"usage":  map[string]int{"inputTokens": 10, "outputTokens": 5},
		})
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	body := `{"messages":[{"role":"user","content":[{"text":"hello"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/model/anthropic.claude-3-sonnet/converse"+callerQuery(testCallerARN), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if receivedPath != "/model/anthropic.claude-3-sonnet/converse" {
		t.Errorf("backend received path = %q, want /model/anthropic.claude-3-sonnet/converse", receivedPath)
	}
	if receivedMethod != http.MethodPost {
		t.Errorf("backend received method = %q, want POST", receivedMethod)
	}
}

func TestHandleProxy_ForwardQueryParams(t *testing.T) {
	var receivedQuery url.Values
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/invoke?caller="+url.QueryEscape(testCallerARN)+"&param=value&foo=bar", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if receivedQuery.Get("param") != "value" {
		t.Errorf("backend query param = %q, want 'value'", receivedQuery.Get("param"))
	}
	if receivedQuery.Get("foo") != "bar" {
		t.Errorf("backend query foo = %q, want 'bar'", receivedQuery.Get("foo"))
	}
	// caller param should be stripped
	if receivedQuery.Get("caller") != "" {
		t.Errorf("caller query param should be stripped, got %q", receivedQuery.Get("caller"))
	}
}

func TestHandleProxy_ForwardRequestBody(t *testing.T) {
	var receivedBody []byte
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	sentBody := `{"messages":[{"role":"user","content":[{"text":"test body forwarding"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/model/test-model/invoke"+callerQuery(testCallerARN), strings.NewReader(sentBody))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if string(receivedBody) != sentBody {
		t.Errorf("backend received body = %q, want %q", string(receivedBody), sentBody)
	}
}

func TestHandleProxy_Error429Forwarded(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"message":"ThrottlingException: Rate exceeded"}`))
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/converse"+callerQuery(testCallerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	body := w.Body.String()
	if !strings.Contains(body, "ThrottlingException") {
		t.Errorf("response body should contain ThrottlingException, got %q", body)
	}
}

func TestHandleProxy_Error400Forwarded(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"ValidationException: Invalid model ID"}`))
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/model/invalid-model/converse"+callerQuery(testCallerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	body := w.Body.String()
	if !strings.Contains(body, "ValidationException") {
		t.Errorf("response body should contain ValidationException, got %q", body)
	}
}

func TestHandleProxy_Error500Forwarded(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message":"InternalServerError"}`))
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/converse"+callerQuery(testCallerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandleProxy_MissingCallerParam(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("backend should not have been called")
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/converse", strings.NewReader(`{}`))
	// No caller query param.

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "missing 'caller' query parameter") {
		t.Errorf("response should mention missing caller param, got %q", w.Body.String())
	}
}

func TestHandleProxy_NonStreamingUsageExtracted(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"output": map[string]any{"message": map[string]any{"role": "assistant"}},
			"usage":  map[string]int{"inputTokens": 150, "outputTokens": 75},
		})
	}))
	defer backend.Close()

	models := []config.ModelConfig{
		{ID: "anthropic.claude-3-sonnet", Name: "Claude 3 Sonnet", InputPricePerMillion: 3.0, OutputPricePerMillion: 15.0, Enabled: true},
	}
	p, s := newTestProxy(t, backend, models...)

	req := httptest.NewRequest(http.MethodPost, "/model/anthropic.claude-3-sonnet/converse"+callerQuery(testCallerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// Wait a bit for the async tracker goroutine.
	time.Sleep(100 * time.Millisecond)

	summary := s.GetSummary(time.Now().Add(-1 * time.Minute))
	if summary.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", summary.TotalRequests)
	}
	if summary.TotalInputTokens != 150 {
		t.Errorf("TotalInputTokens = %d, want 150", summary.TotalInputTokens)
	}
	if summary.TotalOutputTokens != 75 {
		t.Errorf("TotalOutputTokens = %d, want 75", summary.TotalOutputTokens)
	}
}

func TestHandleProxy_StreamingResponsePipedThrough(t *testing.T) {
	// Simulate a streaming response with event stream content type.
	streamData := []byte("chunk1-data\nchunk2-data\nchunk3-data\n")
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.Header().Set("X-Amzn-Bedrock-Input-Token-Count", "100")
		w.Header().Set("X-Amzn-Bedrock-Output-Token-Count", "200")
		w.WriteHeader(http.StatusOK)
		// Write in chunks to simulate streaming.
		for i := 0; i < len(streamData); i += 12 {
			end := i + 12
			if end > len(streamData) {
				end = len(streamData)
			}
			w.Write(streamData[i:end])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer backend.Close()

	p, s := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/converse-stream"+callerQuery(testCallerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify the full stream was piped through.
	if !bytes.Equal(w.Body.Bytes(), streamData) {
		t.Errorf("response body = %q, want %q", w.Body.String(), string(streamData))
	}

	// Verify content type was preserved.
	ct := w.Header().Get("Content-Type")
	if ct != "application/vnd.amazon.eventstream" {
		t.Errorf("Content-Type = %q, want application/vnd.amazon.eventstream", ct)
	}

	// Wait for async tracker.
	time.Sleep(100 * time.Millisecond)

	summary := s.GetSummary(time.Now().Add(-1 * time.Minute))
	if summary.TotalRequests != 1 {
		t.Errorf("TotalRequests = %d, want 1", summary.TotalRequests)
	}
	if summary.TotalInputTokens != 100 {
		t.Errorf("TotalInputTokens = %d, want 100", summary.TotalInputTokens)
	}
	if summary.TotalOutputTokens != 200 {
		t.Errorf("TotalOutputTokens = %d, want 200", summary.TotalOutputTokens)
	}
}

func TestHandleProxy_InvokeWithResponseStream(t *testing.T) {
	streamData := []byte("stream-payload-bytes")
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/model/test-model/invoke-with-response-stream" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.WriteHeader(http.StatusOK)
		w.Write(streamData)
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/invoke-with-response-stream"+callerQuery(testCallerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !bytes.Equal(w.Body.Bytes(), streamData) {
		t.Errorf("response body mismatch")
	}
}

func TestHandleProxy_CountTokens(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/model/test-model/count-tokens" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"totalTokens":42}`))
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/count-tokens"+callerQuery(testCallerARN), strings.NewReader(`{"messages":[]}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "totalTokens") {
		t.Errorf("response should contain totalTokens, got %q", w.Body.String())
	}
}

func TestHandleProxy_GuardrailPath(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/guardrail/") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"action":"ALLOWED"}`))
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/guardrail/gr-abc123/version/1/apply"+callerQuery(testCallerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleProxy_ResponseHeadersForwarded(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Amzn-RequestId", "test-request-id-123")
		w.Header().Set("X-Amzn-Bedrock-Content-Type", "application/json")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/invoke"+callerQuery(testCallerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("X-Amzn-RequestId"); got != "test-request-id-123" {
		t.Errorf("X-Amzn-RequestId = %q, want %q", got, "test-request-id-123")
	}
}

func TestHandleProxy_QuotaReject(t *testing.T) {
	backendCalled := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	callerARN := "arn:aws:sts::123456789012:assumed-role/TestRole/session"

	p, s := newTestProxyWithQuota(t, backend, []quota.Quota{
		{
			ID:           "test-quota",
			Match:        "account:123456789012",
			TokensPerDay: 100,
			Mode:         quota.ModeReject,
			Enabled:      true,
		},
	})

	// Record enough usage to exceed quota.
	s.RecordRequest(store.Request{
		CallerARN:    callerARN,
		InputTokens:  50,
		OutputTokens: 51,
		CreatedAt:    time.Now().UTC(),
	})

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/converse"+callerQuery(callerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if backendCalled {
		t.Error("backend should not be called when quota rejects")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if !strings.Contains(w.Body.String(), "quota exceeded") {
		t.Errorf("response should mention quota exceeded, got %q", w.Body.String())
	}
}

func TestHandleProxy_QuotaWarnAllows(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	callerARN := "arn:aws:sts::123456789012:assumed-role/TestRole/session"

	p, s := newTestProxyWithQuota(t, backend, []quota.Quota{
		{
			ID:           "warn-quota",
			Match:        "account:123456789012",
			TokensPerDay: 100,
			Mode:         quota.ModeWarn,
			Enabled:      true,
		},
	})

	// Record enough usage to exceed quota.
	s.RecordRequest(store.Request{
		CallerARN:    callerARN,
		InputTokens:  50,
		OutputTokens: 51,
		CreatedAt:    time.Now().UTC(),
	})

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/converse"+callerQuery(callerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	// Warn mode should allow the request through.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (warn mode should allow)", w.Code, http.StatusOK)
	}
}

func TestHandleProxy_RequestIsSigned(t *testing.T) {
	var receivedAuthHeader string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/invoke"+callerQuery(testCallerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	// The proxy should re-sign with its own credentials (AKIAPROXYTEST...).
	if !strings.Contains(receivedAuthHeader, "AWS4-HMAC-SHA256") {
		t.Error("outbound request should be signed with SigV4")
	}
	if !strings.Contains(receivedAuthHeader, "AKIAPROXYTEST1234567") {
		t.Errorf("outbound request should use proxy credentials, got %q", receivedAuthHeader)
	}
}

func TestHandleProxy_ContentTypeHeaderForwarded(t *testing.T) {
	var receivedContentType string
	var receivedAccept string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		receivedAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/invoke"+callerQuery(testCallerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if receivedContentType != "application/json" {
		t.Errorf("backend Content-Type = %q, want application/json", receivedContentType)
	}
	if receivedAccept != "application/json" {
		t.Errorf("backend Accept = %q, want application/json", receivedAccept)
	}
}

func TestHandleProxy_UsageFromHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Amzn-Bedrock-Input-Token-Count", "333")
		w.Header().Set("X-Amzn-Bedrock-Output-Token-Count", "444")
		w.WriteHeader(http.StatusOK)
		// Response body has no usage info.
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer backend.Close()

	p, s := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/invoke"+callerQuery(testCallerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	time.Sleep(100 * time.Millisecond)

	summary := s.GetSummary(time.Now().Add(-1 * time.Minute))
	if summary.TotalInputTokens != 333 {
		t.Errorf("TotalInputTokens = %d, want 333", summary.TotalInputTokens)
	}
	if summary.TotalOutputTokens != 444 {
		t.Errorf("TotalOutputTokens = %d, want 444", summary.TotalOutputTokens)
	}
}

func TestHandleProxy_ConcurrentRequests(t *testing.T) {
	var mu sync.Mutex
	requestCount := 0

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"usage":{"inputTokens":1,"outputTokens":1}}`))
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	const concurrency = 20
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/model/model-%d/converse%s", i, callerQuery(testCallerARN)), strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			p.HandleProxy(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("request %d: status = %d, want %d", i, w.Code, http.StatusOK)
			}
		}(i)
	}

	wg.Wait()

	mu.Lock()
	if requestCount != concurrency {
		t.Errorf("backend received %d requests, want %d", requestCount, concurrency)
	}
	mu.Unlock()
}

func TestHandleProxy_LargeResponseBody(t *testing.T) {
	// Generate a large response body (~1MB).
	largePayload := bytes.Repeat([]byte("x"), 1024*1024)
	respBody := fmt.Sprintf(`{"data":"%s","usage":{"input_tokens":10,"output_tokens":5}}`, string(largePayload))

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(respBody))
	}))
	defer backend.Close()

	p, _ := newTestProxy(t, backend)

	req := httptest.NewRequest(http.MethodPost, "/model/test-model/invoke"+callerQuery(testCallerARN), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	p.HandleProxy(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.Len() != len(respBody) {
		t.Errorf("response body length = %d, want %d", w.Body.Len(), len(respBody))
	}
}

func TestHandleProxy_AllPathPatterns(t *testing.T) {
	paths := []string{
		"/model/anthropic.claude-3-sonnet/converse",
		"/model/anthropic.claude-3-sonnet/converse-stream",
		"/model/anthropic.claude-3-sonnet/invoke",
		"/model/anthropic.claude-3-sonnet/invoke-with-response-stream",
		"/model/anthropic.claude-3-sonnet/count-tokens",
		"/model/us.anthropic.claude-3-sonnet/converse",
		"/model/eu.anthropic.claude-3-sonnet/invoke",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != path {
					t.Errorf("backend path = %q, want %q", r.URL.Path, path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{}`))
			}))
			defer backend.Close()

			p, _ := newTestProxy(t, backend)

			req := httptest.NewRequest(http.MethodPost, path+callerQuery(testCallerARN), strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			p.HandleProxy(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d for path %s", w.Code, http.StatusOK, path)
			}
		})
	}
}
