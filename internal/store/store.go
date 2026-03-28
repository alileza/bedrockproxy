package store

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bedrockproxy/internal/config"
)

// Request represents a single proxied Bedrock call.
type Request struct {
	ID           int64     `json:"id"`
	CallerARN    string    `json:"caller_arn"`
	ModelID      string    `json:"model_id"`
	Operation    string    `json:"operation"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	CostUSD      float64   `json:"cost_usd"`
	LatencyMs    int       `json:"latency_ms"`
	StatusCode   int       `json:"status_code"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Caller represents a caller identity.
type Caller struct {
	ARN         string
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

// Summary holds aggregated usage stats.
type Summary struct {
	TotalRequests     int64   `json:"total_requests"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	TotalCostUSD      float64 `json:"total_cost_usd"`
	UniqueCallers     int     `json:"unique_callers"`
}

// CallerStats holds per-caller usage breakdown.
type CallerStats struct {
	AccountID         string  `json:"account_id"`
	Role              string  `json:"role"`
	TotalRequests     int64   `json:"total_requests"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	TotalCostUSD      float64 `json:"total_cost_usd"`
}

// Model represents a configured Bedrock model.
type Model struct {
	ID                    string  `json:"id"`
	Name                  string  `json:"name"`
	InputPricePerMillion  float64 `json:"input_price_per_million"`
	OutputPricePerMillion float64 `json:"output_price_per_million"`
	Enabled               bool    `json:"enabled"`
	CreatedAt             string  `json:"created_at"`
}

// Store is a thread-safe in-memory store that replaces PostgreSQL.
type Store struct {
	mu       sync.RWMutex
	requests []Request
	callers  map[string]*Caller // keyed by caller ARN
	models   []Model
	nextID   atomic.Int64
}

// New creates a new in-memory store initialized with the given model configs.
func New(models []config.ModelConfig) *Store {
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

// RecordRequest appends a request and updates the caller's last_seen.
func (s *Store) RecordRequest(req Request) {
	req.ID = s.nextID.Add(1)
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.requests = append(s.requests, req)

	c := s.ensureCallerLocked(req.CallerARN)
	c.LastSeenAt = req.CreatedAt
}

// EnsureCaller returns an existing caller or creates a new one.
func (s *Store) EnsureCaller(arn string) *Caller {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureCallerLocked(arn)
}

func (s *Store) ensureCallerLocked(arn string) *Caller {
	if c, ok := s.callers[arn]; ok {
		return c
	}
	now := time.Now().UTC()
	c := &Caller{
		ARN:         arn,
		FirstSeenAt: now,
		LastSeenAt:  now,
	}
	s.callers[arn] = c
	return c
}

// extractAccountFromARN pulls the account ID from an ARN like arn:aws:sts::123456789012:...
func extractAccountFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 5 {
		return parts[4]
	}
	return ""
}

// GetSummary returns aggregated usage stats for requests since the given time.
func (s *Store) GetSummary(since time.Time) Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var summary Summary
	callerSet := make(map[string]struct{})

	for i := range s.requests {
		r := &s.requests[i]
		if r.CreatedAt.Before(since) {
			continue
		}
		summary.TotalRequests++
		summary.TotalInputTokens += int64(r.InputTokens)
		summary.TotalOutputTokens += int64(r.OutputTokens)
		summary.TotalCostUSD += r.CostUSD
		callerSet[r.CallerARN] = struct{}{}
	}
	summary.UniqueCallers = len(callerSet)
	return summary
}

// GetCallers returns per-caller usage breakdown since the given time, sorted by cost descending.
func (s *Store) GetCallers(since time.Time) []CallerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agg := make(map[string]*CallerStats)

	for i := range s.requests {
		r := &s.requests[i]
		if r.CreatedAt.Before(since) {
			continue
		}

		arn := r.CallerARN
		accountID := extractAccountFromARN(arn)
		if accountID == "" {
			accountID = "unknown"
		}

		cs, ok := agg[arn]
		if !ok {
			cs = &CallerStats{AccountID: accountID, Role: arn}
			agg[arn] = cs
		}
		cs.TotalRequests++
		cs.TotalInputTokens += int64(r.InputTokens)
		cs.TotalOutputTokens += int64(r.OutputTokens)
		cs.TotalCostUSD += r.CostUSD
	}

	result := make([]CallerStats, 0, len(agg))
	for _, cs := range agg {
		result = append(result, *cs)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalCostUSD > result[j].TotalCostUSD
	})

	if len(result) > 100 {
		result = result[:100]
	}
	return result
}

// GetActivity returns the most recent requests, newest first.
func (s *Store) GetActivity(limit int) []Request {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := len(s.requests)
	if limit > n {
		limit = n
	}

	result := make([]Request, limit)
	for i := 0; i < limit; i++ {
		result[i] = s.requests[n-1-i]
	}
	return result
}

// UpdateModels replaces the model list. Config-provided models should already
// be merged by the caller; this is used to inject auto-discovered pricing.
func (s *Store) UpdateModels(models []Model) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models = models
}

// GetModels returns the configured models.
func (s *Store) GetModels() []Model {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Model, len(s.models))
	copy(result, s.models)
	return result
}

// GetCallerUsageToday returns token and cost usage since midnight UTC, plus
// request count from the last 60 seconds, for callers matching the given
// accountID or caller ARN. Pass "*" for both to aggregate all callers.
func (s *Store) GetCallerUsageToday(accountID, callerARN string) (tokens int64, cost float64, requestsLastMinute int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	oneMinuteAgo := now.Add(-60 * time.Second)

	for i := range s.requests {
		r := &s.requests[i]
		if r.CreatedAt.Before(midnight) {
			continue
		}

		if !s.callerMatchesLocked(r.CallerARN, accountID, callerARN) {
			continue
		}

		tokens += int64(r.InputTokens) + int64(r.OutputTokens)
		cost += r.CostUSD

		if !r.CreatedAt.Before(oneMinuteAgo) {
			requestsLastMinute++
		}
	}
	return
}

// callerMatchesLocked checks whether the request's caller ARN matches the
// given accountID or callerARN pattern. Must hold s.mu.
func (s *Store) callerMatchesLocked(reqCallerARN, accountID, callerARN string) bool {
	if accountID == "*" && callerARN == "*" {
		return true
	}

	if accountID != "" && accountID != "*" {
		reqAccountID := extractAccountFromARN(reqCallerARN)
		if reqAccountID == accountID {
			return true
		}
	}
	if callerARN != "" && callerARN != "*" && reqCallerARN == callerARN {
		return true
	}
	return false
}

// FlushRequests returns all current requests and clears the internal slice.
func (s *Store) FlushRequests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.requests) == 0 {
		return nil
	}

	flushed := s.requests
	s.requests = nil
	return flushed
}
