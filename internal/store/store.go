package store

import (
	"encoding/json"
	"log/slog"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"bedrockproxy/internal/config"
)

const callerCacheFile = ".bedrockproxy-callers.json"

// callerEntry is the on-disk format for caller cache.
type callerEntry struct {
	AccountID string `json:"account_id"`
	RoleARN   string `json:"role_arn"`
}

// Request represents a single proxied Bedrock call.
type Request struct {
	ID           int64   `json:"id"`
	AccessKeyID  string  `json:"access_key_id"`
	ModelID      string  `json:"model_id"`
	Operation    string  `json:"operation"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	LatencyMs    int     `json:"latency_ms"`
	StatusCode   int     `json:"status_code"`
	ErrorMessage string  `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Caller represents a resolved IAM identity.
type Caller struct {
	AccessKeyID string
	AccountID   string
	RoleARN     string
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
	callers  map[string]*Caller
	models   []Model
	nextID   atomic.Int64
}

// New creates a new in-memory store initialized with the given model configs.
// Loads cached caller identities from disk if available.
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

	s.loadCallerCache()
	return s
}

func (s *Store) loadCallerCache() {
	data, err := os.ReadFile(callerCacheFile)
	if err != nil {
		return
	}
	var cache map[string]callerEntry
	if err := json.Unmarshal(data, &cache); err != nil {
		return
	}
	now := time.Now().UTC()
	for accountID, entry := range cache {
		// Store a synthetic caller keyed by account ID
		// When a real access key comes in from this account, FindARNByAccount will match it
		syntheticKey := "_account_" + accountID
		s.callers[syntheticKey] = &Caller{
			AccessKeyID: syntheticKey,
			AccountID:   entry.AccountID,
			RoleARN:     entry.RoleARN,
			FirstSeenAt: now,
			LastSeenAt:  now,
		}
	}
	slog.Info("loaded caller cache", "entries", len(cache))
}

func (s *Store) saveCallerCache() {
	s.mu.RLock()
	cache := make(map[string]callerEntry)
	for _, c := range s.callers {
		if c.AccountID != "" && c.RoleARN != "" {
			// Key by account ID so it survives key rotation
			cache[c.AccountID] = callerEntry{
				AccountID: c.AccountID,
				RoleARN:   c.RoleARN,
			}
		}
	}
	s.mu.RUnlock()

	if len(cache) == 0 {
		return
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		slog.Warn("failed to marshal caller cache", "error", err)
		return
	}
	if err := os.WriteFile(callerCacheFile, data, 0644); err != nil {
		slog.Warn("failed to save caller cache", "error", err)
	}
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

	c := s.ensureCallerLocked(req.AccessKeyID)
	c.LastSeenAt = req.CreatedAt
}

// EnsureCaller returns an existing caller or creates a new one.
func (s *Store) EnsureCaller(accessKeyID string) *Caller {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureCallerLocked(accessKeyID)
}

func (s *Store) ensureCallerLocked(accessKeyID string) *Caller {
	if c, ok := s.callers[accessKeyID]; ok {
		return c
	}
	now := time.Now().UTC()
	c := &Caller{
		AccessKeyID: accessKeyID,
		FirstSeenAt: now,
		LastSeenAt:  now,
	}
	s.callers[accessKeyID] = c
	return c
}

// UpdateCallerARN sets the role ARN for a caller and propagates to all callers in the same account.
func (s *Store) UpdateCallerARN(accessKeyID, roleARN string) {
	s.mu.Lock()
	c := s.ensureCallerLocked(accessKeyID)
	c.RoleARN = roleARN

	// Propagate to siblings in the same account
	if c.AccountID != "" {
		for _, other := range s.callers {
			if other.AccountID == c.AccountID && other.RoleARN == "" {
				other.RoleARN = roleARN
			}
		}
	}
	s.mu.Unlock()

	s.saveCallerCache()
}

// UpdateCallerAccount sets the account ID for a caller.
func (s *Store) UpdateCallerAccount(accessKeyID, accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c := s.ensureCallerLocked(accessKeyID)
	if c.AccountID == "" {
		c.AccountID = accountID
	}
}

// GetCallerRoleARN returns the role ARN for a caller, or empty string if not found.
func (s *Store) GetCallerRoleARN(accessKeyID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if c, ok := s.callers[accessKeyID]; ok {
		return c.RoleARN
	}
	return ""
}

// GetCallerAccountID returns the account ID for a caller, or empty string if not found.
func (s *Store) GetCallerAccountID(accessKeyID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if c, ok := s.callers[accessKeyID]; ok {
		return c.AccountID
	}
	return ""
}

// FindARNByAccount looks for a role ARN from any caller in the given account.
func (s *Store) FindARNByAccount(accountID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, c := range s.callers {
		if c.AccountID == accountID && c.RoleARN != "" {
			return c.RoleARN
		}
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
		callerSet[r.AccessKeyID] = struct{}{}
	}
	summary.UniqueCallers = len(callerSet)
	return summary
}

// GetCallers returns per-caller usage breakdown since the given time, sorted by cost descending.
func (s *Store) GetCallers(since time.Time) []CallerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type callerKey struct {
		accountID string
		role      string
	}
	agg := make(map[callerKey]*CallerStats)

	for i := range s.requests {
		r := &s.requests[i]
		if r.CreatedAt.Before(since) {
			continue
		}

		c := s.callers[r.AccessKeyID]
		accountID := "unknown"
		role := r.AccessKeyID
		if c != nil {
			if c.AccountID != "" {
				accountID = c.AccountID
			}
			if c.RoleARN != "" {
				role = c.RoleARN
			}
		}

		key := callerKey{accountID: accountID, role: role}
		cs, ok := agg[key]
		if !ok {
			cs = &CallerStats{AccountID: accountID, Role: role}
			agg[key] = cs
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
		req := s.requests[n-1-i]

		// Enrich with caller info for the "caller" display field
		if c, ok := s.callers[req.AccessKeyID]; ok {
			if c.RoleARN != "" {
				req.AccessKeyID = c.RoleARN
			} else if c.AccountID != "" {
				req.AccessKeyID = "arn:aws:iam::" + c.AccountID + ":access-key/" + req.AccessKeyID
			}
		}

		result[i] = req
	}
	return result
}

// GetModels returns the configured models.
func (s *Store) GetModels() []Model {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Model, len(s.models))
	copy(result, s.models)
	return result
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
