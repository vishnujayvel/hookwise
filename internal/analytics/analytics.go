package analytics

import (
	"context"
	"fmt"
	"math"
	"time"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// SessionStats holds aggregate counters for a completed session.
type SessionStats struct {
	TotalToolCalls     int
	FileEditsCount     int
	AIAuthoredLines    int
	HumanVerifiedLines int
	EstimatedCostUSD   float64
}

// EventRecord describes a single event to be persisted.
type EventRecord struct {
	EventType         string
	ToolName          string
	Timestamp         time.Time
	FilePath          string
	LinesAdded        int
	LinesRemoved      int
	AIConfidenceScore float64
}

// AuthorshipEntry describes a single authorship ledger row.
type AuthorshipEntry struct {
	SessionID    string
	FilePath     string
	ToolName     string
	LinesChanged int
	AIScore      float64
	Timestamp    time.Time
}

// DailySummaryResult holds aggregated stats for a single day.
type DailySummaryResult struct {
	Date               string
	TotalSessions      int
	TotalEvents        int
	TotalToolCalls     int
	TotalFileEdits     int
	AIAuthoredLines    int
	HumanVerifiedLines int
	EstimatedCostUSD   float64
}

// ToolBreakdownEntry shows how many times a given tool was used.
type ToolBreakdownEntry struct {
	ToolName   string
	Count      int
	Percentage float64
}

// ---------------------------------------------------------------------------
// Analytics – high-level operations on the Dolt DB
// ---------------------------------------------------------------------------

// Analytics wraps *DB and provides high-level session, event, authorship,
// and stats operations.
type Analytics struct {
	db *DB
}

// NewAnalytics creates a new Analytics instance backed by the given DB.
func NewAnalytics(db *DB) *Analytics {
	return &Analytics{db: db}
}

// ---------------------------------------------------------------------------
// Session lifecycle
// ---------------------------------------------------------------------------

// StartSession creates a new session row.
func (a *Analytics) StartSession(ctx context.Context, sessionID string, startedAt time.Time) error {
	_, err := a.db.Exec(ctx,
		"INSERT INTO sessions (id, started_at) VALUES (?, ?)",
		sessionID, startedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("analytics: start session: %w", err)
	}
	return nil
}

// EndSession updates the session row with end timestamp and summary stats.
// If the session does not exist, the UPDATE is a no-op (0 rows affected),
// which is treated as success (upsert-safe behaviour).
func (a *Analytics) EndSession(ctx context.Context, sessionID string, endedAt time.Time, stats SessionStats) error {
	_, err := a.db.Exec(ctx,
		`UPDATE sessions SET
			ended_at = ?,
			total_tool_calls = ?,
			file_edits_count = ?,
			ai_authored_lines = ?,
			human_verified_lines = ?,
			estimated_cost_usd = ?
		WHERE id = ?`,
		endedAt.UTC().Format(time.RFC3339),
		stats.TotalToolCalls,
		stats.FileEditsCount,
		stats.AIAuthoredLines,
		stats.HumanVerifiedLines,
		stats.EstimatedCostUSD,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("analytics: end session: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Event recording
// ---------------------------------------------------------------------------

// RecordEvent inserts a row into the events table.
func (a *Analytics) RecordEvent(ctx context.Context, sessionID string, event EventRecord) error {
	_, err := a.db.Exec(ctx,
		`INSERT INTO events (session_id, event_type, tool_name, timestamp, file_path, lines_added, lines_removed, ai_confidence_score)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID,
		event.EventType,
		event.ToolName,
		event.Timestamp.UTC().Format(time.RFC3339),
		event.FilePath,
		event.LinesAdded,
		event.LinesRemoved,
		event.AIConfidenceScore,
	)
	if err != nil {
		return fmt.Errorf("analytics: record event: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Authorship classification
// ---------------------------------------------------------------------------

// ClassifyAuthorship returns the classification label for a given AI
// confidence score using the project's four-tier thresholds:
//
//	>= 0.8  → "high_probability_ai"
//	>= 0.5  → "likely_ai"
//	>= 0.2  → "mixed_verified"
//	<  0.2  → "human_authored"
func ClassifyAuthorship(aiScore float64) string {
	switch {
	case aiScore >= 0.8:
		return "high_probability_ai"
	case aiScore >= 0.5:
		return "likely_ai"
	case aiScore >= 0.2:
		return "mixed_verified"
	default:
		return "human_authored"
	}
}

// RecordAuthorship classifies the entry and inserts a row into the
// authorship_ledger table.
func (a *Analytics) RecordAuthorship(ctx context.Context, entry AuthorshipEntry) error {
	classification := ClassifyAuthorship(entry.AIScore)
	_, err := a.db.Exec(ctx,
		`INSERT INTO authorship_ledger (session_id, file_path, tool_name, lines_changed, ai_score, classification, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.SessionID,
		entry.FilePath,
		entry.ToolName,
		entry.LinesChanged,
		entry.AIScore,
		classification,
		entry.Timestamp.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("analytics: record authorship: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Stats queries
// ---------------------------------------------------------------------------

// DailySummary returns aggregate stats for a given date (YYYY-MM-DD).
// If no data exists for the date, all numeric fields are zero.
func (a *Analytics) DailySummary(ctx context.Context, date string) (*DailySummaryResult, error) {
	result := &DailySummaryResult{Date: date}

	// Count sessions that started on this date.
	err := a.db.QueryRow(ctx,
		"SELECT COUNT(*) FROM sessions WHERE started_at LIKE ?",
		date+"%",
	).Scan(&result.TotalSessions)
	if err != nil {
		return nil, fmt.Errorf("analytics: daily summary sessions: %w", err)
	}

	// Count events on this date.
	err = a.db.QueryRow(ctx,
		"SELECT COUNT(*) FROM events WHERE timestamp LIKE ?",
		date+"%",
	).Scan(&result.TotalEvents)
	if err != nil {
		return nil, fmt.Errorf("analytics: daily summary events: %w", err)
	}

	// Aggregate session-level stats for sessions started on this date.
	err = a.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(total_tool_calls), 0),
		        COALESCE(SUM(file_edits_count), 0),
		        COALESCE(SUM(ai_authored_lines), 0),
		        COALESCE(SUM(human_verified_lines), 0),
		        COALESCE(SUM(estimated_cost_usd), 0)
		 FROM sessions WHERE started_at LIKE ?`,
		date+"%",
	).Scan(
		&result.TotalToolCalls,
		&result.TotalFileEdits,
		&result.AIAuthoredLines,
		&result.HumanVerifiedLines,
		&result.EstimatedCostUSD,
	)
	if err != nil {
		return nil, fmt.Errorf("analytics: daily summary aggregates: %w", err)
	}

	return result, nil
}

// ToolBreakdown returns per-tool event counts for a given date (YYYY-MM-DD),
// ordered by count descending.  Each entry includes the percentage of the
// total.
func (a *Analytics) ToolBreakdown(ctx context.Context, date string) ([]ToolBreakdownEntry, error) {
	rows, err := a.db.Query(ctx,
		`SELECT tool_name, COUNT(*) AS cnt
		 FROM events
		 WHERE timestamp LIKE ? AND tool_name != ''
		 GROUP BY tool_name
		 ORDER BY cnt DESC`,
		date+"%",
	)
	if err != nil {
		return nil, fmt.Errorf("analytics: tool breakdown: %w", err)
	}
	defer rows.Close()

	var entries []ToolBreakdownEntry
	total := 0
	for rows.Next() {
		var e ToolBreakdownEntry
		if err := rows.Scan(&e.ToolName, &e.Count); err != nil {
			return nil, fmt.Errorf("analytics: tool breakdown scan: %w", err)
		}
		total += e.Count
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("analytics: tool breakdown rows: %w", err)
	}

	// Compute percentages.
	if total > 0 {
		for i := range entries {
			entries[i].Percentage = math.Round(float64(entries[i].Count)/float64(total)*10000) / 100
		}
	}

	return entries, nil
}
