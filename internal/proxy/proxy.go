package proxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"

	"bedrockproxy/internal/metrics"
	"bedrockproxy/internal/quota"
	"bedrockproxy/internal/usage"
)

// Proxy handles forwarding requests to AWS Bedrock as a transparent reverse proxy.
type Proxy struct {
	target   *url.URL
	signer   *v4.Signer
	creds    aws.CredentialsProvider
	tracker  *usage.Tracker
	quotaEng *quota.Engine
	region   string
	client   *http.Client
}

func New(ctx context.Context, region string, tracker *usage.Tracker, opts ...Option) (*Proxy, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	target, _ := url.Parse(fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", region))

	p := &Proxy{
		target:  target,
		signer:  v4.NewSigner(),
		creds:   cfg.Credentials,
		tracker: tracker,
		region:  region,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
	for _, o := range opts {
		o(p)
	}
	return p, nil
}

// Option configures optional Proxy dependencies.
type Option func(*Proxy)

// WithQuotaEngine sets the quota engine for the proxy.
func WithQuotaEngine(e *quota.Engine) Option {
	return func(p *Proxy) {
		p.quotaEng = e
	}
}

// extractAccountFromARN pulls the account ID from an ARN like arn:aws:sts::123456789012:...
func extractAccountFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 5 {
		return parts[4]
	}
	return ""
}

// HandleProxy is the main handler that transparently proxies all Bedrock Runtime operations.
func (p *Proxy) HandleProxy(w http.ResponseWriter, r *http.Request) {
	callerARN := r.URL.Query().Get("caller")
	if callerARN == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"missing 'caller' query parameter. Set endpoint_url to http://host:port?caller=YOUR_ARN"}`))
		return
	}

	callerAccountID := extractAccountFromARN(callerARN)

	if blocked := p.checkQuota(w, callerARN, callerAccountID); blocked {
		return
	}

	// Read request body for signing.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"message":"failed to read request body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	metrics.ActiveRequests.Inc()
	defer metrics.ActiveRequests.Dec()

	start := time.Now()

	// Build the outbound request to Bedrock.
	// Strip the "caller" query param before forwarding.
	bedrockURL := *p.target
	bedrockURL.Path = r.URL.Path
	q := r.URL.Query()
	q.Del("caller")
	bedrockURL.RawQuery = q.Encode()

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, bedrockURL.String(), bytes.NewReader(body))
	if err != nil {
		slog.Error("failed to create bedrock request", "error", err)
		http.Error(w, `{"message":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Copy relevant headers from the original request.
	for _, h := range []string{"Content-Type", "Accept", "X-Amzn-Bedrock-Accept"} {
		if v := r.Header.Get(h); v != "" {
			outReq.Header.Set(h, v)
		}
	}

	// Compute payload hash for SigV4 signing.
	payloadHash := hashPayload(body)

	// Sign the request with the proxy's own AWS credentials.
	creds, err := p.creds.Retrieve(r.Context())
	if err != nil {
		slog.Error("failed to retrieve aws credentials", "error", err)
		http.Error(w, `{"message":"proxy credential error"}`, http.StatusInternalServerError)
		return
	}

	if err := p.signer.SignHTTP(r.Context(), creds, outReq, payloadHash, "bedrock", p.region, time.Now()); err != nil {
		slog.Error("failed to sign bedrock request", "error", err)
		http.Error(w, `{"message":"proxy signing error"}`, http.StatusInternalServerError)
		return
	}

	// Forward the request to Bedrock.
	resp, err := p.client.Do(outReq)
	if err != nil {
		latency := time.Since(start)
		modelID, operation := parsePathInfo(r.URL.Path)
		slog.Error("bedrock request failed", "error", err, "path", r.URL.Path)
		p.tracker.Record(r.Context(), usage.Request{
			CallerARN:    callerARN,
			ModelID:      modelID,
			Operation:    operation,
			LatencyMs:    int(latency.Milliseconds()),
			StatusCode:   502,
			ErrorMessage: err.Error(),
		})
		http.Error(w, fmt.Sprintf(`{"message":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	latency := time.Since(start)
	modelID, operation := parsePathInfo(r.URL.Path)

	isStreaming := isStreamingResponse(r.URL.Path, resp)

	// Copy response headers to the client.
	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if isStreaming {
		// Stream the response directly to the client.
		p.streamResponse(w, resp, r.Context(), callerARN, modelID, operation, start, resp.StatusCode)
	} else {
		// Non-streaming: read full body, extract usage, and write.
		p.forwardResponse(w, resp, r.Context(), callerARN, modelID, operation, latency)
	}
}

// streamResponse pipes the Bedrock response directly to the client for streaming responses.
// It attempts to extract token counts from response headers.
func (p *Proxy) streamResponse(w http.ResponseWriter, resp *http.Response, ctx context.Context, callerARN, modelID, operation string, startTime time.Time, statusCode int) {
	// Stream the body to the client.
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if canFlush {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}

	// Try to extract token counts from response headers.
	inputTokens := headerInt(resp.Header, "X-Amzn-Bedrock-Input-Token-Count")
	outputTokens := headerInt(resp.Header, "X-Amzn-Bedrock-Output-Token-Count")

	p.tracker.Record(ctx, usage.Request{
		CallerARN:    callerARN,
		ModelID:      modelID,
		Operation:    operation,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		LatencyMs:    int(time.Since(startTime).Milliseconds()),
		StatusCode:   statusCode,
	})
}

// forwardResponse handles non-streaming responses: reads the full body, extracts usage, writes to client.
func (p *Proxy) forwardResponse(w http.ResponseWriter, resp *http.Response, ctx context.Context, callerARN, modelID, operation string, latency time.Duration) {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("failed to read bedrock response body", "error", err)
		return
	}

	w.Write(respBody)

	// Extract token usage from the response body.
	inputTokens, outputTokens := extractTokenCounts(respBody)

	// Also check response headers as fallback.
	if inputTokens == 0 && outputTokens == 0 {
		inputTokens = headerInt(resp.Header, "X-Amzn-Bedrock-Input-Token-Count")
		outputTokens = headerInt(resp.Header, "X-Amzn-Bedrock-Output-Token-Count")
	}

	p.tracker.Record(ctx, usage.Request{
		CallerARN:    callerARN,
		ModelID:      modelID,
		Operation:    operation,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		LatencyMs:    int(latency.Milliseconds()),
		StatusCode:   resp.StatusCode,
	})
}

// extractTokenCounts tries to parse token usage from the model response body.
// Supports both Converse API format and InvokeModel (Messages API) format.
func extractTokenCounts(body []byte) (inputTokens, outputTokens int) {
	// Try Converse API format: { "usage": { "inputTokens": N, "outputTokens": N } }
	var converseResp struct {
		Usage struct {
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &converseResp); err == nil && (converseResp.Usage.InputTokens > 0 || converseResp.Usage.OutputTokens > 0) {
		return converseResp.Usage.InputTokens, converseResp.Usage.OutputTokens
	}

	// Try Messages API / InvokeModel format: { "usage": { "input_tokens": N, "output_tokens": N } }
	var messagesResp struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &messagesResp); err == nil {
		return messagesResp.Usage.InputTokens, messagesResp.Usage.OutputTokens
	}

	return 0, 0
}

// checkQuota evaluates the caller against the quota engine.
// Returns true if the request was blocked (response already written).
func (p *Proxy) checkQuota(w http.ResponseWriter, callerARN, callerAccountID string) bool {
	if p.quotaEng == nil {
		return false
	}

	result := p.quotaEng.Check(callerARN, callerAccountID)
	if result.Allowed {
		return false
	}

	mode := p.quotaEng.GetMode(result.QuotaID)
	callerLabel := callerARN

	metrics.QuotaExceededTotal.WithLabelValues(result.QuotaID, string(mode), callerLabel).Inc()

	if mode == quota.ModeReject {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "quota exceeded: " + result.Reason,
		})
		return true
	}

	// warn mode -- log but allow
	slog.Warn("quota exceeded (warn mode)",
		"quota_id", result.QuotaID,
		"reason", result.Reason,
		"caller", callerLabel,
	)
	return false
}

// parsePathInfo extracts model ID and operation from a Bedrock path.
// Example paths:
//
//	/model/anthropic.claude-3-sonnet/converse
//	/model/anthropic.claude-3-sonnet/converse-stream
//	/model/anthropic.claude-3-sonnet/invoke
//	/model/anthropic.claude-3-sonnet/invoke-with-response-stream
func parsePathInfo(path string) (modelID, operation string) {
	// Remove leading slash.
	path = strings.TrimPrefix(path, "/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) >= 3 && parts[0] == "model" {
		modelID = parts[1]
		operation = parts[2]
		return
	}
	// For non-model paths (guardrail, async-invoke), use the first segment as operation.
	if len(parts) >= 1 {
		operation = parts[0]
	}
	return
}

// isStreamingResponse determines if a response is a streaming response based on the
// request path and response content type.
func isStreamingResponse(path string, resp *http.Response) bool {
	// Check path for known streaming operations.
	if strings.Contains(path, "converse-stream") || strings.Contains(path, "invoke-with-response-stream") {
		return true
	}
	// Check content type for event stream.
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/vnd.amazon.eventstream") {
		return true
	}
	return false
}

// hashPayload computes the SHA256 hash of the payload for SigV4 signing.
func hashPayload(payload []byte) string {
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])
}

// headerInt extracts an integer value from a response header, returning 0 if not found or invalid.
func headerInt(h http.Header, key string) int {
	v := h.Get(key)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}
