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
// Cost State (R4.7)
// ---------------------------------------------------------------------------

// CostState represents the singleton cost state stored in the
// cost_state SQLite table (id=1).
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

// ReadCostState loads the singleton cost state from SQLite.
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
		if err := json.Unmarshal([]byte(dailyCostsJSON.String), &state.DailyCosts); err != nil {
			core.Logger().Warn("cost state: invalid daily_costs JSON", "error", err)
		}
		if state.DailyCosts == nil {
			state.DailyCosts = make(map[string]float64)
		}
	}
	if sessionCostsJSON.Valid && sessionCostsJSON.String != "" {
		if err := json.Unmarshal([]byte(sessionCostsJSON.String), &state.SessionCosts); err != nil {
			core.Logger().Warn("cost state: invalid session_costs JSON", "error", err)
		}
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

// FeedCacheEntry represents a single entry in the feed_cache SQLite table.
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
		core.Logger().Warn("feed cache: invalid updated_at, using current time", "key", key, "value", updatedAt, "error", err)
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

// ReadAllFeedCache reads all non-expired feed cache entries and returns them
// as a map keyed by cache_key, with the data payload as values.
func (d *DB) ReadAllFeedCache(ctx context.Context) (map[string]interface{}, error) {
	rows, err := d.Query(ctx,
		`SELECT cache_key, data, updated_at, ttl_seconds FROM feed_cache`)
	if err != nil {
		return nil, fmt.Errorf("analytics: read all feed_cache: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	result := make(map[string]interface{})
	for rows.Next() {
		var (
			cacheKey, dataJSON, updatedAt string
			ttlSeconds                    int
		)
		if err := rows.Scan(&cacheKey, &dataJSON, &updatedAt, &ttlSeconds); err != nil {
			return nil, fmt.Errorf("analytics: scan feed_cache: %w", err)
		}
		// Check TTL freshness (consistent with ReadFeedCache).
		if ttlSeconds > 0 {
			if parsedTime, err := parseTimeFlex(updatedAt); err == nil {
				if now.After(parsedTime.Add(time.Duration(ttlSeconds) * time.Second)) {
					continue // expired
				}
			}
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

// parseTimeFlex delegates to the canonical core.ParseTimeFlex.
func parseTimeFlex(s string) (time.Time, error) {
	return core.ParseTimeFlex(s)
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
