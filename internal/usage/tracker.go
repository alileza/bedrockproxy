package usage

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"bedrockproxy/internal/config"
)

// Request represents a single proxied Bedrock call.
type Request struct {
	AccessKeyID  string
	ModelID      string
	Operation    string
	InputTokens  int
	OutputTokens int
	LatencyMs    int
	StatusCode   int
	ErrorMessage string
}

// Tracker records usage to PostgreSQL.
type Tracker struct {
	pool   *pgxpool.Pool
	prices map[string]config.ModelConfig
	mu     sync.RWMutex
	Notify func()
}

func NewTracker(pool *pgxpool.Pool, models []config.ModelConfig) *Tracker {
	prices := make(map[string]config.ModelConfig, len(models))
	for _, m := range models {
		prices[m.ID] = m
	}
	return &Tracker{pool: pool, prices: prices}
}

func (t *Tracker) Record(_ context.Context, req Request) {
	go t.record(context.Background(), req)
}

func (t *Tracker) record(ctx context.Context, req Request) {
	callerID, err := t.ensureCaller(ctx, req.AccessKeyID)
	if err != nil {
		slog.Error("ensure caller failed", "access_key_id", req.AccessKeyID, "error", err)
		return
	}

	costUSD := t.calculateCost(req.ModelID, req.InputTokens, req.OutputTokens)

	_, err = t.pool.Exec(ctx, `
		INSERT INTO requests (caller_id, model_id, operation, input_tokens, output_tokens, cost_usd, latency_ms, status_code, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, callerID, req.ModelID, req.Operation, req.InputTokens, req.OutputTokens, costUSD, req.LatencyMs, req.StatusCode, nilIfEmpty(req.ErrorMessage))
	if err != nil {
		slog.Error("insert request failed", "error", err)
		return
	}
	if t.Notify != nil {
		t.Notify()
	}
}

func (t *Tracker) ensureCaller(ctx context.Context, accessKeyID string) (int64, error) {
	var id int64
	err := t.pool.QueryRow(ctx, `
		INSERT INTO callers (access_key_id) VALUES ($1)
		ON CONFLICT (access_key_id) DO UPDATE SET last_seen_at = NOW()
		RETURNING id
	`, accessKeyID).Scan(&id)
	return id, err
}

func (t *Tracker) calculateCost(modelID string, inputTokens, outputTokens int) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	m, ok := t.prices[modelID]
	if !ok {
		// Try stripping region prefix: eu.anthropic.claude-... -> anthropic.claude-...
		stripped := modelID
		if idx := strings.Index(modelID, "."); idx != -1 && idx < 4 {
			stripped = modelID[idx+1:]
		}
		m, ok = t.prices[stripped]
		if !ok {
			return 0
		}
	}
	inputCost := float64(inputTokens) * m.InputPricePerMillion / 1_000_000
	outputCost := float64(outputTokens) * m.OutputPricePerMillion / 1_000_000
	return inputCost + outputCost
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
