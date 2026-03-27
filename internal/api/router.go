package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"bedrockproxy/internal/proxy"
)

type Router struct {
	pool  *pgxpool.Pool
	proxy *proxy.Proxy
	mux   *http.ServeMux
	events *EventBus
}

func NewRouter(pool *pgxpool.Pool, proxy *proxy.Proxy, events *EventBus) *Router {
	r := &Router{pool: pool, proxy: proxy, mux: http.NewServeMux(), events: events}
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

	// Dashboard API endpoints
	r.mux.HandleFunc("GET /api/ws", r.events.HandleWS)
	r.mux.HandleFunc("GET /api/health", r.handleHealth)
	r.mux.HandleFunc("GET /api/usage/summary", r.handleUsageSummary)
	r.mux.HandleFunc("GET /api/usage/callers", r.handleCallers)
	r.mux.HandleFunc("GET /api/usage/activity", r.handleActivity)
	r.mux.HandleFunc("GET /api/models", r.handleModels)
}

func (r *Router) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

func (r *Router) handleUsageSummary(w http.ResponseWriter, req *http.Request) {
	minutes := queryInt(req, "minutes", 43200) // default 30 days

	rows, err := r.pool.Query(req.Context(), `
		SELECT
			COUNT(*)                          AS total_requests,
			COALESCE(SUM(input_tokens), 0)    AS total_input_tokens,
			COALESCE(SUM(output_tokens), 0)   AS total_output_tokens,
			COALESCE(SUM(cost_usd), 0)        AS total_cost_usd,
			COUNT(DISTINCT caller_id)          AS unique_callers
		FROM requests
		WHERE created_at >= NOW() - $1 * INTERVAL '1 minute'
	`, minutes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var summary struct {
		TotalRequests    int64   `json:"total_requests"`
		TotalInputTokens int64   `json:"total_input_tokens"`
		TotalOutputTokens int64  `json:"total_output_tokens"`
		TotalCostUSD     float64 `json:"total_cost_usd"`
		UniqueCallers    int     `json:"unique_callers"`
	}

	if rows.Next() {
		rows.Scan(&summary.TotalRequests, &summary.TotalInputTokens, &summary.TotalOutputTokens, &summary.TotalCostUSD, &summary.UniqueCallers)
	}
	writeJSON(w, summary)
}

func (r *Router) handleCallers(w http.ResponseWriter, req *http.Request) {
	minutes := queryInt(req, "minutes", 43200)

	rows, err := r.pool.Query(req.Context(), `
		SELECT
			c.access_key_id,
			COALESCE(
				c.role_arn,
				CASE WHEN c.account_id IS NOT NULL THEN 'arn:aws:iam::' || c.account_id || ':access-key/' || c.access_key_id
				ELSE c.access_key_id END
			) AS display_name,
			COUNT(*)              AS total_requests,
			SUM(r.input_tokens)   AS total_input_tokens,
			SUM(r.output_tokens)  AS total_output_tokens,
			SUM(r.cost_usd)       AS total_cost_usd
		FROM requests r
		JOIN callers c ON c.id = r.caller_id
		WHERE r.created_at >= NOW() - $1 * INTERVAL '1 minute'
		GROUP BY c.access_key_id, c.role_arn, c.account_id
		ORDER BY total_cost_usd DESC
		LIMIT 100
	`, minutes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type caller struct {
		AccessKeyID      string  `json:"access_key_id"`
		DisplayName      string  `json:"display_name"`
		TotalRequests    int64   `json:"total_requests"`
		TotalInputTokens int64   `json:"total_input_tokens"`
		TotalOutputTokens int64  `json:"total_output_tokens"`
		TotalCostUSD     float64 `json:"total_cost_usd"`
	}

	var callers []caller
	for rows.Next() {
		var c caller
		rows.Scan(&c.AccessKeyID, &c.DisplayName, &c.TotalRequests, &c.TotalInputTokens, &c.TotalOutputTokens, &c.TotalCostUSD)
		callers = append(callers, c)
	}
	if callers == nil {
		callers = []caller{}
	}
	writeJSON(w, callers)
}

func (r *Router) handleActivity(w http.ResponseWriter, req *http.Request) {
	limit := queryInt(req, "limit", 50)

	rows, err := r.pool.Query(req.Context(), `
		SELECT
			r.id,
			COALESCE(
				c.role_arn,
				CASE WHEN c.account_id IS NOT NULL THEN 'arn:aws:iam::' || c.account_id || ':access-key/' || c.access_key_id
				ELSE c.access_key_id END
			) AS caller,
			r.model_id,
			r.operation,
			r.input_tokens,
			r.output_tokens,
			r.cost_usd,
			r.latency_ms,
			r.status_code,
			r.created_at
		FROM requests r
		JOIN callers c ON c.id = r.caller_id
		ORDER BY r.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

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

	var activities []activity
	for rows.Next() {
		var a activity
		rows.Scan(&a.ID, &a.Caller, &a.ModelID, &a.Operation, &a.InputTokens, &a.OutputTokens, &a.CostUSD, &a.LatencyMs, &a.StatusCode, &a.CreatedAt)
		activities = append(activities, a)
	}
	if activities == nil {
		activities = []activity{}
	}
	writeJSON(w, activities)
}

func (r *Router) handleModels(w http.ResponseWriter, req *http.Request) {
	rows, err := r.pool.Query(req.Context(), `
		SELECT id, name, input_price_per_million, output_price_per_million, enabled, created_at
		FROM models
		ORDER BY name
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type model struct {
		ID                    string  `json:"id"`
		Name                  string  `json:"name"`
		InputPricePerMillion  float64 `json:"input_price_per_million"`
		OutputPricePerMillion float64 `json:"output_price_per_million"`
		Enabled               bool    `json:"enabled"`
		CreatedAt             string  `json:"created_at"`
	}

	var models []model
	for rows.Next() {
		var m model
		rows.Scan(&m.ID, &m.Name, &m.InputPricePerMillion, &m.OutputPricePerMillion, &m.Enabled, &m.CreatedAt)
		models = append(models, m)
	}
	if models == nil {
		models = []model{}
	}
	writeJSON(w, models)
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
