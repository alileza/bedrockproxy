package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"bedrockproxy/internal/auth"
	"bedrockproxy/internal/proxy"
	"bedrockproxy/internal/quota"
	"bedrockproxy/internal/store"
)

// extractAccountFromARN pulls the account ID from an ARN like arn:aws:sts::123456789012:...
func extractAccountFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 5 {
		return parts[4]
	}
	return ""
}

type Router struct {
	store    *store.Store
	proxy    *proxy.Proxy
	resolver *auth.Resolver
	quotaEng *quota.Engine
	mux      *http.ServeMux
	events   *EventBus
}

// RouterOption configures optional Router dependencies.
type RouterOption func(*Router)

// WithQuotaEngine sets the quota engine on the router.
func WithQuotaEngine(e *quota.Engine) RouterOption {
	return func(r *Router) {
		r.quotaEng = e
	}
}

func NewRouter(s *store.Store, proxy *proxy.Proxy, resolver *auth.Resolver, events *EventBus, opts ...RouterOption) *Router {
	r := &Router{store: s, proxy: proxy, resolver: resolver, mux: http.NewServeMux(), events: events}
	for _, o := range opts {
		o(r)
	}
	r.routes()
	return r
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

func (r *Router) routes() {
	// Bedrock proxy endpoints (clients call these)
	r.mux.HandleFunc("POST /model/{modelId}/converse", r.proxy.HandleConverse)
	r.mux.HandleFunc("POST /model/{modelId}/invoke", r.proxy.HandleInvokeModel)

	// Prometheus metrics
	r.mux.Handle("GET /metrics", promhttp.Handler())

	// Caller self-registration — caller hits this to register their ARN
	r.mux.HandleFunc("POST /api/register-caller", r.handleRegisterCaller)

	// Dashboard API endpoints
	r.mux.HandleFunc("GET /api/ws", r.events.HandleWS)
	r.mux.HandleFunc("GET /api/health", r.handleHealth)
	r.mux.HandleFunc("GET /api/usage/summary", r.handleUsageSummary)
	r.mux.HandleFunc("GET /api/usage/callers", r.handleCallers)
	r.mux.HandleFunc("GET /api/usage/activity", r.handleActivity)
	r.mux.HandleFunc("GET /api/models", r.handleModels)

	// Quota management
	r.mux.HandleFunc("GET /api/quotas", r.handleGetQuotas)
	r.mux.HandleFunc("POST /api/quotas", r.handleSetQuota)
	r.mux.HandleFunc("DELETE /api/quotas/{id}", r.handleDeleteQuota)
}

func (r *Router) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

func (r *Router) handleUsageSummary(w http.ResponseWriter, req *http.Request) {
	minutes := queryInt(req, "minutes", 43200) // default 30 days
	since := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)

	summary := r.store.GetSummary(since)
	writeJSON(w, summary)
}

func (r *Router) handleCallers(w http.ResponseWriter, req *http.Request) {
	minutes := queryInt(req, "minutes", 43200)
	since := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)

	callers := r.store.GetCallers(since)
	if callers == nil {
		callers = []store.CallerStats{}
	}
	writeJSON(w, callers)
}

func (r *Router) handleActivity(w http.ResponseWriter, req *http.Request) {
	limit := queryInt(req, "limit", 50)

	activities := r.store.GetActivity(limit)

	type activity struct {
		ID           int64   `json:"id"`
		Caller       string  `json:"caller"`
		ModelID      string  `json:"model_id"`
		Operation    string  `json:"operation"`
		InputTokens  int     `json:"input_tokens"`
		OutputTokens int     `json:"output_tokens"`
		CostUSD      float64 `json:"cost_usd"`
		LatencyMs    int     `json:"latency_ms"`
		StatusCode   int     `json:"status_code"`
		CreatedAt    string  `json:"created_at"`
	}

	result := make([]activity, 0, len(activities))
	for _, a := range activities {
		result = append(result, activity{
			ID:           a.ID,
			Caller:       a.AccessKeyID, // Already enriched by GetActivity
			ModelID:      a.ModelID,
			Operation:    a.Operation,
			InputTokens:  a.InputTokens,
			OutputTokens: a.OutputTokens,
			CostUSD:      a.CostUSD,
			LatencyMs:    a.LatencyMs,
			StatusCode:   a.StatusCode,
			CreatedAt:    a.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	writeJSON(w, result)
}

func (r *Router) handleModels(w http.ResponseWriter, _ *http.Request) {
	models := r.store.GetModels()
	if models == nil {
		models = []store.Model{}
	}
	writeJSON(w, models)
}

func (r *Router) handleRegisterCaller(w http.ResponseWriter, req *http.Request) {
	caller, err := auth.ParseSigV4(req)
	if err != nil {
		http.Error(w, `{"message":"missing SigV4 Authorization header"}`, http.StatusUnauthorized)
		return
	}

	var body struct {
		ARN       string `json:"arn"`
		AccountID string `json:"account_id"`
		UserID    string `json:"user_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.ARN == "" {
		http.Error(w, `{"message":"body must contain 'arn'"}`, http.StatusBadRequest)
		return
	}

	// Extract account ID from ARN: arn:aws:sts::123456789012:assumed-role/...
	if accountID := extractAccountFromARN(body.ARN); accountID != "" {
		r.store.EnsureCaller(caller.AccessKeyID)
		r.store.UpdateCallerAccount(caller.AccessKeyID, accountID)
	}
	if r.resolver != nil {
		r.resolver.UpdateRoleARN(req.Context(), caller.AccessKeyID, body.ARN)
	}

	writeJSON(w, map[string]string{"status": "registered", "access_key_id": caller.AccessKeyID, "arn": body.ARN})
}

func (r *Router) handleGetQuotas(w http.ResponseWriter, _ *http.Request) {
	if r.quotaEng == nil {
		writeJSON(w, []any{})
		return
	}
	quotas := r.quotaEng.GetQuotasWithUsage()
	if quotas == nil {
		quotas = []quota.QuotaWithUsage{}
	}
	writeJSON(w, quotas)
}

func (r *Router) handleSetQuota(w http.ResponseWriter, req *http.Request) {
	if r.quotaEng == nil {
		http.Error(w, `{"message":"quota engine not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var q quota.Quota
	if err := json.NewDecoder(req.Body).Decode(&q); err != nil {
		http.Error(w, `{"message":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if q.ID == "" {
		http.Error(w, `{"message":"id is required"}`, http.StatusBadRequest)
		return
	}
	if q.Match == "" {
		http.Error(w, `{"message":"match is required"}`, http.StatusBadRequest)
		return
	}

	r.quotaEng.SetQuota(q)
	writeJSON(w, q)
}

func (r *Router) handleDeleteQuota(w http.ResponseWriter, req *http.Request) {
	if r.quotaEng == nil {
		http.Error(w, `{"message":"quota engine not configured"}`, http.StatusServiceUnavailable)
		return
	}

	id := req.PathValue("id")
	if id == "" {
		http.Error(w, `{"message":"id is required"}`, http.StatusBadRequest)
		return
	}

	r.quotaEng.DeleteQuota(id)
	writeJSON(w, map[string]string{"status": "deleted"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func queryInt(r *http.Request, key string, defaultVal int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}
