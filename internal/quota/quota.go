package quota

import (
	"encoding/json"
	"log/slog"
	"os"
	"path"
	"strings"
	"sync"

	"bedrockproxy/internal/config"
	"bedrockproxy/internal/store"
)

const quotaFile = ".bedrockproxy-quotas.json"

// Mode determines what happens when a quota is exceeded.
type Mode string

const (
	ModeWarn     Mode = "warn"
	ModeThrottle Mode = "throttle" // future
	ModeReject   Mode = "reject"
)

// Quota defines a usage limit for callers matching a pattern.
type Quota struct {
	ID                string  `json:"id"`
	Match             string  `json:"match"`               // glob pattern on ARN, or "account:<id>", or "*"
	TokensPerDay      int64   `json:"tokens_per_day"`      // 0 = unlimited
	RequestsPerMinute int     `json:"requests_per_minute"` // 0 = unlimited
	CostPerDay        float64 `json:"cost_per_day"`        // 0 = unlimited
	Mode              Mode    `json:"mode"`
	Enabled           bool    `json:"enabled"`
}

// CheckResult is the outcome of a quota check.
type CheckResult struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
	QuotaID string `json:"quota_id,omitempty"`
}

// QuotaWithUsage is returned by the API to show current usage alongside limits.
type QuotaWithUsage struct {
	Quota
	TokensUsedToday    int64   `json:"tokens_used_today"`
	CostUsedToday      float64 `json:"cost_used_today"`
	RequestsLastMinute int     `json:"requests_last_minute"`
}

// Engine evaluates quotas against caller usage data from the store.
type Engine struct {
	mu       sync.RWMutex
	quotas   map[string]Quota
	store    *store.Store
	skipDisk bool // when true, skip disk persistence (for tests)
}

// NewTestEngine creates a quota engine without disk persistence. For use in tests.
func NewTestEngine(s *store.Store) *Engine {
	return &Engine{
		quotas:   make(map[string]Quota),
		store:    s,
		skipDisk: true,
	}
}

// NewEngine creates a quota engine. Config quotas are loaded first as defaults,
// then .bedrockproxy-quotas.json overrides on top (same ID = override).
func NewEngine(s *store.Store, defaults []config.QuotaConfig) *Engine {
	e := &Engine{
		quotas: make(map[string]Quota),
		store:  s,
	}

	// Load config defaults
	for _, d := range defaults {
		mode := ModeWarn
		if d.Mode == "reject" {
			mode = ModeReject
		}
		e.quotas[d.ID] = Quota{
			ID:                d.ID,
			Match:             d.Match,
			TokensPerDay:      d.TokensPerDay,
			RequestsPerMinute: d.RequestsPerMinute,
			CostPerDay:        d.CostPerDay,
			Mode:              mode,
			Enabled:           d.Enabled,
		}
	}

	// JSON file overrides config defaults (same ID = replace)
	e.loadFromDisk()
	return e
}

// Check evaluates all enabled quotas against the caller's current usage.
// It returns the result for the best (most specific) matching quota.
func (e *Engine) Check(callerARN, callerAccountID string) CheckResult {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var bestQuota *Quota
	var bestSpecificity int

	for _, q := range e.quotas {
		if !q.Enabled {
			continue
		}
		spec := matchSpecificity(q.Match, callerARN, callerAccountID)
		if spec > bestSpecificity || (spec > 0 && bestQuota == nil) {
			qq := q // copy
			bestQuota = &qq
			bestSpecificity = spec
		}
	}

	if bestQuota == nil {
		return CheckResult{Allowed: true}
	}

	tokens, cost, reqsLastMin := e.store.GetCallerUsageToday(callerAccountID, callerARN)

	if bestQuota.TokensPerDay > 0 && tokens >= bestQuota.TokensPerDay {
		return CheckResult{
			Allowed: false,
			Reason:  "daily token limit exceeded",
			QuotaID: bestQuota.ID,
		}
	}
	if bestQuota.CostPerDay > 0 && cost >= bestQuota.CostPerDay {
		return CheckResult{
			Allowed: false,
			Reason:  "daily cost limit exceeded",
			QuotaID: bestQuota.ID,
		}
	}
	if bestQuota.RequestsPerMinute > 0 && reqsLastMin >= bestQuota.RequestsPerMinute {
		return CheckResult{
			Allowed: false,
			Reason:  "requests per minute limit exceeded",
			QuotaID: bestQuota.ID,
		}
	}

	return CheckResult{Allowed: true, QuotaID: bestQuota.ID}
}

// GetQuotas returns all configured quotas.
func (e *Engine) GetQuotas() []Quota {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]Quota, 0, len(e.quotas))
	for _, q := range e.quotas {
		result = append(result, q)
	}
	return result
}

// GetQuotasWithUsage returns all quotas enriched with current usage data.
func (e *Engine) GetQuotasWithUsage() []QuotaWithUsage {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]QuotaWithUsage, 0, len(e.quotas))
	for _, q := range e.quotas {
		accountID, arn := extractMatchTarget(q.Match)
		tokens, cost, reqs := e.store.GetCallerUsageToday(accountID, arn)
		result = append(result, QuotaWithUsage{
			Quota:              q,
			TokensUsedToday:    tokens,
			CostUsedToday:      cost,
			RequestsLastMinute: reqs,
		})
	}
	return result
}

// SetQuota adds or updates a quota and persists to disk.
func (e *Engine) SetQuota(q Quota) {
	e.mu.Lock()
	e.quotas[q.ID] = q
	e.mu.Unlock()

	e.saveToDisk()
}

// DeleteQuota removes a quota by ID and persists to disk.
func (e *Engine) DeleteQuota(id string) {
	e.mu.Lock()
	delete(e.quotas, id)
	e.mu.Unlock()

	e.saveToDisk()
}

// GetMode returns the mode of the quota with the given ID, or empty string.
func (e *Engine) GetMode(quotaID string) Mode {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if q, ok := e.quotas[quotaID]; ok {
		return q.Mode
	}
	return ""
}

// GetQuotaByID returns a pointer to the quota with the given ID, or nil.
func (e *Engine) GetQuotaByID(id string) *Quota {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if q, ok := e.quotas[id]; ok {
		return &q
	}
	return nil
}

func (e *Engine) loadFromDisk() {
	data, err := os.ReadFile(quotaFile)
	if err != nil {
		return
	}
	var quotas []Quota
	if err := json.Unmarshal(data, &quotas); err != nil {
		slog.Warn("failed to parse quota file", "error", err)
		return
	}
	for _, q := range quotas {
		e.quotas[q.ID] = q
	}
	slog.Info("loaded quotas from disk", "count", len(quotas))
}

func (e *Engine) saveToDisk() {
	if e.skipDisk {
		return
	}
	e.mu.RLock()
	quotas := make([]Quota, 0, len(e.quotas))
	for _, q := range e.quotas {
		quotas = append(quotas, q)
	}
	e.mu.RUnlock()

	data, err := json.MarshalIndent(quotas, "", "  ")
	if err != nil {
		slog.Warn("failed to marshal quotas", "error", err)
		return
	}
	if err := os.WriteFile(quotaFile, data, 0644); err != nil {
		slog.Warn("failed to save quotas", "error", err)
	}
}

// matchSpecificity returns a specificity score for how well a quota match
// pattern matches the given caller. Returns 0 for no match.
// Higher values = more specific match.
func matchSpecificity(pattern, callerARN, callerAccountID string) int {
	if pattern == "*" {
		return 1 // wildcard matches everything, lowest specificity
	}

	// "account:<id>" prefix matches all callers from that account
	if strings.HasPrefix(pattern, "account:") {
		accountID := strings.TrimPrefix(pattern, "account:")
		if callerAccountID == accountID {
			return 2
		}
		return 0
	}

	// Glob match on the caller's role ARN.
	// path.Match treats '/' as separator so '*' won't cross it.
	// We replace '/' with a placeholder to allow '*' to match full ARNs.
	if callerARN != "" {
		safePattern := strings.ReplaceAll(pattern, "/", "\x00")
		safeARN := strings.ReplaceAll(callerARN, "/", "\x00")
		matched, err := path.Match(safePattern, safeARN)
		if err == nil && matched {
			return 3 // most specific
		}
	}

	return 0
}

// extractMatchTarget converts a quota match pattern into accountID and ARN
// for usage lookups. For "account:xxx" it returns the account ID.
// For glob patterns we return the pattern as-is for the ARN (the store
// method will handle matching).
func extractMatchTarget(match string) (accountID, arn string) {
	if match == "*" {
		return "*", "*"
	}
	if strings.HasPrefix(match, "account:") {
		return strings.TrimPrefix(match, "account:"), ""
	}
	return "", match
}
