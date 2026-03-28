package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"bedrockproxy/internal/auth"
	"bedrockproxy/internal/metrics"
	"bedrockproxy/internal/quota"
	"bedrockproxy/internal/usage"
)

// Proxy handles forwarding requests to AWS Bedrock.
type Proxy struct {
	client   *bedrockruntime.Client
	tracker  *usage.Tracker
	resolver *auth.Resolver
	quotaEng *quota.Engine
	region   string
}

func New(ctx context.Context, region string, tracker *usage.Tracker, resolver *auth.Resolver, opts ...Option) (*Proxy, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := bedrockruntime.NewFromConfig(cfg)

	p := &Proxy{
		client:   client,
		tracker:  tracker,
		resolver: resolver,
		region:   region,
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

// converseRequest is the JSON body format for the Converse API.
type converseRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content []struct {
			Text string `json:"text,omitempty"`
		} `json:"content"`
	} `json:"messages"`
	System []struct {
		Text string `json:"text,omitempty"`
	} `json:"system,omitempty"`
	InferenceConfig *struct {
		MaxTokens   *int32   `json:"maxTokens,omitempty"`
		Temperature *float32 `json:"temperature,omitempty"`
		TopP        *float32 `json:"topP,omitempty"`
		StopSequences []string `json:"stopSequences,omitempty"`
	} `json:"inferenceConfig,omitempty"`
}

func (cr *converseRequest) toSDK(modelID string) *bedrockruntime.ConverseInput {
	input := &bedrockruntime.ConverseInput{
		ModelId: aws.String(modelID),
	}

	for _, msg := range cr.Messages {
		m := types.Message{Role: types.ConversationRole(msg.Role)}
		for _, c := range msg.Content {
			if c.Text != "" {
				m.Content = append(m.Content, &types.ContentBlockMemberText{Value: c.Text})
			}
		}
		input.Messages = append(input.Messages, m)
	}

	for _, s := range cr.System {
		if s.Text != "" {
			input.System = append(input.System, &types.SystemContentBlockMemberText{Value: s.Text})
		}
	}

	if cr.InferenceConfig != nil {
		input.InferenceConfig = &types.InferenceConfiguration{
			MaxTokens:     cr.InferenceConfig.MaxTokens,
			Temperature:   cr.InferenceConfig.Temperature,
			TopP:          cr.InferenceConfig.TopP,
			StopSequences: cr.InferenceConfig.StopSequences,
		}
	}

	return input
}

// HandleConverse handles the Bedrock Converse API.
// POST /model/{modelId}/converse
func (p *Proxy) HandleConverse(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("modelId")
	if modelID == "" {
		http.Error(w, `{"message":"missing modelId"}`, http.StatusBadRequest)
		return
	}

	caller, err := auth.ParseSigV4(r)
	if err != nil {
		slog.Warn("failed to parse SigV4", "error", err)
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	p.resolveCaller(r, caller)

	if blocked := p.checkQuota(w, caller.AccessKeyID); blocked {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"message":"failed to read request body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req converseRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf(`{"message":"invalid request body: %s"}`, err), http.StatusBadRequest)
		return
	}

	metrics.ActiveRequests.Inc()
	defer metrics.ActiveRequests.Dec()

	start := time.Now()
	input := req.toSDK(modelID)

	output, err := p.client.Converse(r.Context(), input)
	latency := time.Since(start)

	if err != nil {
		slog.Error("bedrock converse failed", "model", modelID, "error", err)
		p.tracker.Record(r.Context(), usage.Request{
			AccessKeyID:  caller.AccessKeyID,
			ModelID:      modelID,
			Operation:    "Converse",
			LatencyMs:    int(latency.Milliseconds()),
			StatusCode:   500,
			ErrorMessage: err.Error(),
		})
		http.Error(w, fmt.Sprintf(`{"message":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}

	var inputTokens, outputTokens int
	if output.Usage != nil {
		inputTokens = int(aws.ToInt32(output.Usage.InputTokens))
		outputTokens = int(aws.ToInt32(output.Usage.OutputTokens))
	}

	p.tracker.Record(r.Context(), usage.Request{
		AccessKeyID:  caller.AccessKeyID,
		ModelID:      modelID,
		Operation:    "Converse",
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		LatencyMs:    int(latency.Milliseconds()),
		StatusCode:   200,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(output)
}

// HandleInvokeModel handles the Bedrock InvokeModel API.
// POST /model/{modelId}/invoke
func (p *Proxy) HandleInvokeModel(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("modelId")
	if modelID == "" {
		http.Error(w, `{"message":"missing modelId"}`, http.StatusBadRequest)
		return
	}

	caller, err := auth.ParseSigV4(r)
	if err != nil {
		slog.Warn("failed to parse SigV4", "error", err)
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	p.resolveCaller(r, caller)

	if blocked := p.checkQuota(w, caller.AccessKeyID); blocked {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"message":"failed to read request body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	metrics.ActiveRequests.Inc()
	defer metrics.ActiveRequests.Dec()

	start := time.Now()

	output, err := p.client.InvokeModel(r.Context(), &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(modelID),
		Body:        body,
		ContentType: aws.String(r.Header.Get("Content-Type")),
		Accept:      aws.String(r.Header.Get("Accept")),
	})
	latency := time.Since(start)

	if err != nil {
		slog.Error("bedrock invoke model failed", "model", modelID, "error", err)
		p.tracker.Record(r.Context(), usage.Request{
			AccessKeyID:  caller.AccessKeyID,
			ModelID:      modelID,
			Operation:    "InvokeModel",
			LatencyMs:    int(latency.Milliseconds()),
			StatusCode:   500,
			ErrorMessage: err.Error(),
		})
		http.Error(w, fmt.Sprintf(`{"message":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}

	inputTokens, outputTokens := extractTokenCounts(output.Body)

	p.tracker.Record(r.Context(), usage.Request{
		AccessKeyID:  caller.AccessKeyID,
		ModelID:      modelID,
		Operation:    "InvokeModel",
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		LatencyMs:    int(latency.Milliseconds()),
		StatusCode:   200,
	})

	w.Header().Set("Content-Type", aws.ToString(output.ContentType))
	w.Write(output.Body)
}

// extractTokenCounts tries to parse token usage from the model response body.
func extractTokenCounts(body []byte) (inputTokens, outputTokens int) {
	var resp struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&resp); err == nil {
		return resp.Usage.InputTokens, resp.Usage.OutputTokens
	}
	return 0, 0
}

// checkQuota evaluates the caller against the quota engine.
// Returns true if the request was blocked (response already written).
func (p *Proxy) checkQuota(w http.ResponseWriter, accessKeyID string) bool {
	if p.quotaEng == nil {
		return false
	}

	callerARN := ""
	callerAccountID := ""
	if p.resolver != nil {
		callerARN = p.resolver.GetRoleARN(accessKeyID)
		callerAccountID = p.resolver.GetAccountID(accessKeyID)
	}

	result := p.quotaEng.Check(callerARN, callerAccountID)
	if result.Allowed {
		return false
	}

	mode := p.quotaEng.GetMode(result.QuotaID)
	callerLabel := callerARN
	if callerLabel == "" {
		callerLabel = accessKeyID
	}

	metrics.QuotaExceededTotal.WithLabelValues(result.QuotaID, string(mode), callerLabel).Inc()

	if mode == quota.ModeReject {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "quota exceeded: " + result.Reason,
		})
		return true
	}

	// warn mode — log but allow
	slog.Warn("quota exceeded (warn mode)",
		"quota_id", result.QuotaID,
		"reason", result.Reason,
		"caller", callerLabel,
	)
	return false
}

// resolveCaller triggers identity resolution for the caller.
// Checks X-Bedrock-Caller-ARN header first (for testing/manual override),
// then falls back to STS resolution.
func (p *Proxy) resolveCaller(r *http.Request, caller *auth.CallerIdentity) {
	if p.resolver == nil {
		return
	}

	// Allow explicit ARN override via header (useful for testing and internal routing)
	if arn := r.Header.Get("X-Bedrock-Caller-ARN"); arn != "" {
		p.resolver.UpdateRoleARN(r.Context(), caller.AccessKeyID, arn)
		return
	}

	p.resolver.Resolve(r.Context(), caller.AccessKeyID)
}
