package analytics

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// ---------------------------------------------------------------------------
// Coaching State (R4.6)
// ---------------------------------------------------------------------------

// CoachingState represents the singleton coaching state stored in the
// coaching_state Dolt table (id=1).
type CoachingState struct {
	LastPromptAt    *time.Time             `json:"lastPromptAt,omitempty"`
	PromptHistory   []string               `json:"promptHistory,omitempty"`
	CurrentMode     string                 `json:"currentMode"`
	ModeStartedAt   *time.Time             `json:"modeStartedAt,omitempty"`
	ToolingMinutes  float64                `json:"toolingMinutes"`
	AlertLevel      string                 `json:"alertLevel"`
	TodayDate       string                 `json:"todayDate"`
	PracticeCount   int                    `json:"practiceCount"`
	LastLargeChange map[string]interface{} `json:"lastLargeChange,omitempty"`
}

// defaultCoachingState returns a zero-value coaching state for today.
func defaultCoachingState() *CoachingState {
	return &CoachingState{
		CurrentMode:   "coding",
		AlertLevel:    "none",
		TodayDate:     time.Now().Format("2006-01-02"),
		PracticeCount: 0,
	}
}

// ReadCoachingState loads the singleton coaching state from Dolt.
// If no row exists, it returns a default state.
func (d *DB) ReadCoachingState(ctx context.Context) (*CoachingState, error) {
	row := d.QueryRow(ctx,
		`SELECT last_prompt_at, prompt_history, current_mode, mode_started_at,
		        tooling_minutes, alert_level, today_date, practice_count, last_large_change
		 FROM coaching_state WHERE id = 1`)

	var (
		lastPromptAt   sql.NullString
		promptHistJSON sql.NullString
		currentMode    sql.NullString
		modeStartedAt  sql.NullString
		toolingMinutes sql.NullFloat64
		alertLevel     sql.NullString
		todayDate      sql.NullString
		practiceCount  sql.NullInt64
		lastLargeJSON  sql.NullString
	)

	err := row.Scan(
		&lastPromptAt, &promptHistJSON, &currentMode, &modeStartedAt,
		&toolingMinutes, &alertLevel, &todayDate, &practiceCount, &lastLargeJSON,
	)
	if err == sql.ErrNoRows {
		return defaultCoachingState(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("analytics: read coaching_state: %w", err)
	}

	state := &CoachingState{
		CurrentMode:    stringOrDefault(currentMode, "coding"),
		ToolingMinutes: floatOrDefault(toolingMinutes, 0),
		AlertLevel:     stringOrDefault(alertLevel, "none"),
		TodayDate:      stringOrDefault(todayDate, time.Now().Format("2006-01-02")),
		PracticeCount:  intOrDefault(practiceCount, 0),
	}

	if lastPromptAt.Valid && lastPromptAt.String != "" {
		if t, err := parseTimeFlex(lastPromptAt.String); err == nil {
			state.LastPromptAt = &t
		}
	}
	if modeStartedAt.Valid && modeStartedAt.String != "" {
		if t, err := parseTimeFlex(modeStartedAt.String); err == nil {
			state.ModeStartedAt = &t
		}
	}
	if promptHistJSON.Valid && promptHistJSON.String != "" {
		var hist []string
		if err := json.Unmarshal([]byte(promptHistJSON.String), &hist); err == nil {
			state.PromptHistory = hist
		}
	}
	if lastLargeJSON.Valid && lastLargeJSON.String != "" {
		var lc map[string]interface{}
		if err := json.Unmarshal([]byte(lastLargeJSON.String), &lc); err == nil {
			state.LastLargeChange = lc
		}
	}

	return state, nil
}

// WriteCoachingState persists the coaching state to the singleton row (id=1).
func (d *DB) WriteCoachingState(ctx context.Context, state *CoachingState) error {
	promptHistJSON, err := json.Marshal(state.PromptHistory)
	if err != nil {
		return fmt.Errorf("analytics: marshal prompt_history: %w", err)
	}

	lastLargeJSON, err := json.Marshal(state.LastLargeChange)
	if err != nil {
		return fmt.Errorf("analytics: marshal last_large_change: %w", err)
	}

	var lastPromptStr, modeStartedStr interface{}
	if state.LastPromptAt != nil {
		lastPromptStr = state.LastPromptAt.Format(time.RFC3339)
	}
	if state.ModeStartedAt != nil {
		modeStartedStr = state.ModeStartedAt.Format(time.RFC3339)
	}

	_, err = d.Exec(ctx,
		`REPLACE INTO coaching_state
		 (id, last_prompt_at, prompt_history, current_mode, mode_started_at,
		  tooling_minutes, alert_level, today_date, practice_count, last_large_change)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		lastPromptStr, string(promptHistJSON), state.CurrentMode, modeStartedStr,
		state.ToolingMinutes, state.AlertLevel, state.TodayDate, state.PracticeCount,
		string(lastLargeJSON),
	)
	if err != nil {
		return fmt.Errorf("analytics: write coaching_state: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Cost State (R4.7)
// ---------------------------------------------------------------------------

// CostState represents the singleton cost state stored in the
// cost_state Dolt table (id=1).
type CostState struct {
	DailyCosts   map[string]float64 `json:"dailyCosts"`
	SessionCosts map[string]float64 `json:"sessionCosts"`
	Today        string             `json:"today"`
	TotalToday   float64            `json:"totalToday"`
}

// defaultCostState returns a zero-value cost state for today.
func defaultCostState() *CostState {
	return &CostState{
		DailyCosts:   make(map[string]float64),
		SessionCosts: make(map[string]float64),
		Today:        time.Now().Format("2006-01-02"),
		TotalToday:   0,
	}
}

// ReadCostState loads the singleton cost state from Dolt.
// If no row exists, it returns a default state with today's date.
// If the stored date differs from today, TotalToday is reset to 0.
func (d *DB) ReadCostState(ctx context.Context) (*CostState, error) {
	row := d.QueryRow(ctx,
		`SELECT daily_costs, session_costs, today, total_today
		 FROM cost_state WHERE id = 1`)

	var (
		dailyCostsJSON   sql.NullString
		sessionCostsJSON sql.NullString
		today            sql.NullString
		totalToday       sql.NullFloat64
	)

	err := row.Scan(&dailyCostsJSON, &sessionCostsJSON, &today, &totalToday)
	if err == sql.ErrNoRows {
		return defaultCostState(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("analytics: read cost_state: %w", err)
	}

	state := &CostState{
		DailyCosts:   make(map[string]float64),
		SessionCosts: make(map[string]float64),
		Today:        stringOrDefault(today, time.Now().Format("2006-01-02")),
		TotalToday:   floatOrDefault(totalToday, 0),
	}

	if dailyCostsJSON.Valid && dailyCostsJSON.String != "" {
		_ = json.Unmarshal([]byte(dailyCostsJSON.String), &state.DailyCosts)
		if state.DailyCosts == nil {
			state.DailyCosts = make(map[string]float64)
		}
	}
	if sessionCostsJSON.Valid && sessionCostsJSON.String != "" {
		_ = json.Unmarshal([]byte(sessionCostsJSON.String), &state.SessionCosts)
		if state.SessionCosts == nil {
			state.SessionCosts = make(map[string]float64)
		}
	}

	// Date boundary reset: if stored date != today, reset TotalToday.
	currentToday := time.Now().Format("2006-01-02")
	if state.Today != currentToday {
		state.TotalToday = 0
		state.Today = currentToday
	}

	return state, nil
}

// WriteCostState persists the cost state to the singleton row (id=1).
func (d *DB) WriteCostState(ctx context.Context, state *CostState) error {
	dailyCostsJSON, err := json.Marshal(state.DailyCosts)
	if err != nil {
		return fmt.Errorf("analytics: marshal daily_costs: %w", err)
	}

	sessionCostsJSON, err := json.Marshal(state.SessionCosts)
	if err != nil {
		return fmt.Errorf("analytics: marshal session_costs: %w", err)
	}

	_, err = d.Exec(ctx,
		`REPLACE INTO cost_state
		 (id, daily_costs, session_costs, today, total_today)
		 VALUES (1, ?, ?, ?, ?)`,
		string(dailyCostsJSON), string(sessionCostsJSON), state.Today, state.TotalToday,
	)
	if err != nil {
		return fmt.Errorf("analytics: write cost_state: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Feed Cache (R4.7, R9.1)
// ---------------------------------------------------------------------------

// FeedCacheEntry represents a single entry in the feed_cache Dolt table.
type FeedCacheEntry struct {
	Key        string      `json:"key"`
	Data       interface{} `json:"data"`
	UpdatedAt  time.Time   `json:"updatedAt"`
	TTLSeconds int         `json:"ttlSeconds"`
}

// ReadFeedCache loads a single feed cache entry by key.
// Returns nil if the key does not exist or the entry has expired.
func (d *DB) ReadFeedCache(ctx context.Context, key string) (*FeedCacheEntry, error) {
	row := d.QueryRow(ctx,
		`SELECT cache_key, data, updated_at, ttl_seconds
		 FROM feed_cache WHERE cache_key = ?`, key)

	var (
		cacheKey   string
		dataJSON   string
		updatedAt  string
		ttlSeconds int
	)

	err := row.Scan(&cacheKey, &dataJSON, &updatedAt, &ttlSeconds)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("analytics: read feed_cache: %w", err)
	}

	parsedTime, err := parseTimeFlex(updatedAt)
	if err != nil {
		parsedTime = time.Now()
	}

	// Check TTL freshness.
	if ttlSeconds > 0 {
		expiry := parsedTime.Add(time.Duration(ttlSeconds) * time.Second)
		if time.Now().After(expiry) {
			return nil, nil // expired
		}
	}

	var data interface{}
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return nil, fmt.Errorf("analytics: unmarshal feed_cache data: %w", err)
	}

	return &FeedCacheEntry{
		Key:        cacheKey,
		Data:       data,
		UpdatedAt:  parsedTime,
		TTLSeconds: ttlSeconds,
	}, nil
}

// WriteFeedCache writes or replaces a feed cache entry.
func (d *DB) WriteFeedCache(ctx context.Context, entry FeedCacheEntry) error {
	dataJSON, err := json.Marshal(entry.Data)
	if err != nil {
		return fmt.Errorf("analytics: marshal feed_cache data: %w", err)
	}

	updatedAt := entry.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}

	_, err = d.Exec(ctx,
		`REPLACE INTO feed_cache (cache_key, data, updated_at, ttl_seconds)
		 VALUES (?, ?, ?, ?)`,
		entry.Key, string(dataJSON), updatedAt.Format(time.RFC3339), entry.TTLSeconds,
	)
	if err != nil {
		return fmt.Errorf("analytics: write feed_cache: %w", err)
	}
	return nil
}

// ReadAllFeedCache reads all feed cache entries and returns them as a map
// keyed by cache_key, with the data payload as values.
func (d *DB) ReadAllFeedCache(ctx context.Context) (map[string]interface{}, error) {
	rows, err := d.Query(ctx,
		`SELECT cache_key, data FROM feed_cache`)
	if err != nil {
		return nil, fmt.Errorf("analytics: read all feed_cache: %w", err)
	}
	defer rows.Close()

	result := make(map[string]interface{})
	for rows.Next() {
		var cacheKey, dataJSON string
		if err := rows.Scan(&cacheKey, &dataJSON); err != nil {
			return nil, fmt.Errorf("analytics: scan feed_cache: %w", err)
		}
		var data interface{}
		if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
			continue // skip entries with invalid JSON
		}
		result[cacheKey] = data
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("analytics: feed_cache rows: %w", err)
	}
	return result, nil
}

// WriteFeedCacheJSON writes the feed cache data to the JSON bridge file
// at ~/.hookwise/state/status-line-cache.json for the Python TUI (R9.1).
//
// The output format is: {"key1": <data>, "key2": <data>, ...}
// This function respects ARCH-3: it is called from the dispatch process,
// not the daemon.
func WriteFeedCacheJSON(cacheData map[string]interface{}) error {
	path := filepath.Join(core.GetStateDir(), "state", "status-line-cache.json")
	return core.AtomicWriteJSON(path, cacheData)
}

// WriteFeedCacheJSONTo writes the feed cache data to a specified path.
// This variant is used in tests to avoid writing to the real state directory.
func WriteFeedCacheJSONTo(path string, cacheData map[string]interface{}) error {
	return core.AtomicWriteJSON(path, cacheData)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// parseTimeFlex tries several common time formats.
func parseTimeFlex(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}

// stringOrDefault returns the NullString value or the default.
func stringOrDefault(ns sql.NullString, def string) string {
	if ns.Valid && ns.String != "" {
		return ns.String
	}
	return def
}

// floatOrDefault returns the NullFloat64 value or the default.
func floatOrDefault(nf sql.NullFloat64, def float64) float64 {
	if nf.Valid {
		return nf.Float64
	}
	return def
}

// intOrDefault returns the NullInt64 value as int or the default.
func intOrDefault(ni sql.NullInt64, def int) int {
	if ni.Valid {
		return int(ni.Int64)
	}
	return def
}
