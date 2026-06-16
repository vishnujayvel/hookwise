package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ToolCount pairs a tool name with how many times it was used.
type ToolCount struct {
	Name  string
	Count int
}

// RecentSessionInsight holds key metrics for the most-recent session.
type RecentSessionInsight struct {
	ID          string
	DurationMin float64
	LinesAdded  int
	EstCostUSD  float64
}

// InsightsSummary aggregates usage statistics over a configurable rolling window.
type InsightsSummary struct {
	TotalSessions    int
	TotalLinesAdded  int
	AvgDurationMin   float64
	TopTools         []ToolCount
	PeakHourUTC      int
	DaysActive       int
	RecentDaysActive int
	Recent           RecentSessionInsight
}

// ---------------------------------------------------------------------------
// InsightsSummary query
// ---------------------------------------------------------------------------

// InsightsSummary returns aggregate usage statistics over a rolling window.
//
// cutoffDays defines how many days back to look (e.g. 30 for the last 30 days).
// If cutoffDays <= 0, all sessions are included (no lower bound).
//
// On an empty DB or no sessions in the window, a zero-value InsightsSummary is
// returned with a nil error (ARCH-1 fail-open).
func (a *Analytics) InsightsSummary(ctx context.Context, cutoffDays int) (InsightsSummary, error) {
	cutoff := cutoffForDays(cutoffDays)
	recentCutoff := cutoffForDays(7)

	var s InsightsSummary

	// ------------------------------------------------------------------
	// TotalSessions & DaysActive
	// ------------------------------------------------------------------
	if err := a.querySessionCounts(ctx, cutoff, &s); err != nil {
		return InsightsSummary{}, err
	}

	// Short-circuit: nothing to aggregate if there are no sessions.
	if s.TotalSessions == 0 {
		return InsightsSummary{}, nil
	}

	// ------------------------------------------------------------------
	// AvgDurationMin (sessions with ended_at only)
	// ------------------------------------------------------------------
	if err := a.queryAvgDuration(ctx, cutoff, &s); err != nil {
		return InsightsSummary{}, err
	}

	// ------------------------------------------------------------------
	// TotalLinesAdded, TopTools, PeakHourUTC (events join)
	// ------------------------------------------------------------------
	if err := a.queryEventAggregates(ctx, cutoff, &s); err != nil {
		return InsightsSummary{}, err
	}

	// ------------------------------------------------------------------
	// RecentDaysActive (last 7 days)
	// ------------------------------------------------------------------
	if err := a.queryRecentDaysActive(ctx, recentCutoff, &s); err != nil {
		return InsightsSummary{}, err
	}

	// ------------------------------------------------------------------
	// Recent session
	// ------------------------------------------------------------------
	if err := a.queryRecentSession(ctx, cutoff, &s); err != nil {
		return InsightsSummary{}, err
	}

	return s, nil
}

// ---------------------------------------------------------------------------
// Sub-queries
// ---------------------------------------------------------------------------

// cutoffForDays returns a UTC ISO-8601 string suitable for lexicographic
// comparison against TEXT timestamps stored in SQLite. When days <= 0, the
// empty string is returned to signal "no cutoff".
func cutoffForDays(days int) string {
	if days <= 0 {
		return ""
	}
	return time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
}

// cutoffClause returns a SQL snippet and args slice for the session window.
// col is the column name to compare against (e.g. "started_at").
func cutoffClause(col, cutoff string) (string, []interface{}) {
	if cutoff == "" {
		return "1=1", nil
	}
	return col + " >= ?", []interface{}{cutoff}
}

func (a *Analytics) querySessionCounts(ctx context.Context, cutoff string, s *InsightsSummary) error {
	clause, args := cutoffClause("started_at", cutoff)
	err := a.db.QueryRow(ctx,
		`SELECT COUNT(*), COUNT(DISTINCT date(started_at))
		 FROM sessions
		 WHERE `+clause,
		args...,
	).Scan(&s.TotalSessions, &s.DaysActive)
	if err != nil {
		return fmt.Errorf("analytics: insights session counts: %w", err)
	}
	return nil
}

func (a *Analytics) queryAvgDuration(ctx context.Context, cutoff string, s *InsightsSummary) error {
	clause, args := cutoffClause("started_at", cutoff)
	var avg sql.NullFloat64
	err := a.db.QueryRow(ctx,
		`SELECT AVG((julianday(ended_at) - julianday(started_at)) * 1440.0)
		 FROM sessions
		 WHERE ended_at IS NOT NULL AND `+clause,
		args...,
	).Scan(&avg)
	if err != nil {
		return fmt.Errorf("analytics: insights avg duration: %w", err)
	}
	if avg.Valid {
		s.AvgDurationMin = avg.Float64
	}
	return nil
}

func (a *Analytics) queryEventAggregates(ctx context.Context, cutoff string, s *InsightsSummary) error {
	// TotalLinesAdded and PeakHourUTC via events JOINed to sessions.
	clause, args := cutoffClause("s.started_at", cutoff)

	// --- TotalLinesAdded ---
	var totalLines sql.NullInt64
	err := a.db.QueryRow(ctx,
		`SELECT SUM(e.lines_added)
		 FROM events e
		 JOIN sessions s ON s.id = e.session_id
		 WHERE `+clause,
		args...,
	).Scan(&totalLines)
	if err != nil {
		return fmt.Errorf("analytics: insights total lines: %w", err)
	}
	if totalLines.Valid {
		s.TotalLinesAdded = int(totalLines.Int64)
	}

	// --- PeakHourUTC ---
	// Use QueryRow (not Query) so the single connection is released as soon as the
	// row is scanned. Under SetMaxOpenConns(1) (ARCH-2 single-writer), holding an
	// open *sql.Rows here while the TopTools query below also needs the connection
	// deadlocks: the second query blocks forever waiting for the one connection.
	var peakHour, peakCnt int
	err = a.db.QueryRow(ctx,
		`SELECT CAST(strftime('%H', e.timestamp) AS INTEGER) AS hr, COUNT(*) AS cnt
		 FROM events e
		 JOIN sessions s ON s.id = e.session_id
		 WHERE `+clause+`
		 GROUP BY hr
		 ORDER BY cnt DESC
		 LIMIT 1`,
		args...,
	).Scan(&peakHour, &peakCnt)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("analytics: insights peak hour: %w", err)
	}
	if err == nil {
		s.PeakHourUTC = peakHour
	}

	// --- TopTools ---
	toolRows, err := a.db.Query(ctx,
		`SELECT e.tool_name, COUNT(*) AS cnt
		 FROM events e
		 JOIN sessions s ON s.id = e.session_id
		 WHERE e.tool_name IS NOT NULL AND e.tool_name != '' AND `+clause+`
		 GROUP BY e.tool_name
		 ORDER BY cnt DESC
		 LIMIT 10`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("analytics: insights top tools: %w", err)
	}
	defer toolRows.Close()
	for toolRows.Next() {
		var tc ToolCount
		if err := toolRows.Scan(&tc.Name, &tc.Count); err != nil {
			return fmt.Errorf("analytics: insights top tools scan: %w", err)
		}
		s.TopTools = append(s.TopTools, tc)
	}
	if err := toolRows.Err(); err != nil {
		return fmt.Errorf("analytics: insights top tools rows: %w", err)
	}

	return nil
}

func (a *Analytics) queryRecentDaysActive(ctx context.Context, recentCutoff string, s *InsightsSummary) error {
	clause, args := cutoffClause("started_at", recentCutoff)
	err := a.db.QueryRow(ctx,
		`SELECT COUNT(DISTINCT date(started_at))
		 FROM sessions
		 WHERE `+clause,
		args...,
	).Scan(&s.RecentDaysActive)
	if err != nil {
		return fmt.Errorf("analytics: insights recent days active: %w", err)
	}
	return nil
}

func (a *Analytics) queryRecentSession(ctx context.Context, cutoff string, s *InsightsSummary) error {
	clause, args := cutoffClause("started_at", cutoff)

	// Find the most-recent session ID and its base fields.
	var (
		id         string
		startedAt  string
		endedAt    sql.NullString
		estCostUSD float64
	)
	err := a.db.QueryRow(ctx,
		`SELECT id, started_at, ended_at, COALESCE(estimated_cost_usd, 0)
		 FROM sessions
		 WHERE `+clause+`
		 ORDER BY started_at DESC
		 LIMIT 1`,
		args...,
	).Scan(&id, &startedAt, &endedAt, &estCostUSD)
	if err == sql.ErrNoRows {
		return nil // no sessions in window — zero-value Recent is fine
	}
	if err != nil {
		return fmt.Errorf("analytics: insights recent session: %w", err)
	}

	s.Recent.ID = id
	s.Recent.EstCostUSD = estCostUSD

	// Duration: only when ended_at is present.
	if endedAt.Valid && endedAt.String != "" {
		start, errS := time.Parse(time.RFC3339, startedAt)
		end, errE := time.Parse(time.RFC3339, endedAt.String)
		if errS == nil && errE == nil {
			s.Recent.DurationMin = end.Sub(start).Minutes()
		}
	}

	// LinesAdded: sum from events for this session.
	var linesAdded sql.NullInt64
	err = a.db.QueryRow(ctx,
		`SELECT SUM(lines_added) FROM events WHERE session_id = ?`,
		id,
	).Scan(&linesAdded)
	if err != nil {
		return fmt.Errorf("analytics: insights recent session lines: %w", err)
	}
	if linesAdded.Valid {
		s.Recent.LinesAdded = int(linesAdded.Int64)
	}

	return nil
}
