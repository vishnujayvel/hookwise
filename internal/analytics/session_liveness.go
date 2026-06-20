package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SessionLiveness is a read-side projection of a session used by the
// cross-session fleet badge (issue #211, Model D). It carries only what the
// badge needs: whether the session has ended, and when it was last active.
//
// The badge's status vocabulary is derived, not stored: Ended==true is "done",
// Ended==false is "running". LastActivity is the latest of the session's
// started_at, ended_at, and most recent event timestamp — all already written
// by the normal dispatch flow, so producing it requires no new writes and never
// touches the per-tool hot path.
type SessionLiveness struct {
	ID           string
	Ended        bool
	LastActivity time.Time
}

// RecentSessions returns liveness records for sessions whose LastActivity is at
// or after `since`. Sessions that ended before the cutoff, and crashed/abandoned
// sessions (never ended, no recent events) both fall out — so the fleet badge
// only ever reflects work that is genuinely current.
//
// Read-only and cheap enough for the status line: a SQL prefilter drops
// long-finished sessions (ended_at lexicographically before `since`, safe for
// the RFC3339-UTC timestamps this package writes); the precise LastActivity cut
// is applied in Go. Rows that fail to parse are skipped (fail-soft, ARCH-1).
func (a *Analytics) RecentSessions(ctx context.Context, since time.Time) ([]SessionLiveness, error) {
	sinceStr := since.UTC().Format(time.RFC3339)
	rows, err := a.db.Query(ctx,
		`SELECT s.id, s.started_at, s.ended_at, MAX(e.timestamp) AS last_event
		 FROM sessions s
		 LEFT JOIN events e ON e.session_id = s.id
		 WHERE s.ended_at IS NULL OR s.ended_at >= ?
		 GROUP BY s.id`,
		sinceStr,
	)
	if err != nil {
		return nil, fmt.Errorf("analytics: recent sessions: %w", err)
	}
	defer rows.Close()

	var out []SessionLiveness
	for rows.Next() {
		var id, startedAt string
		var endedAt, lastEvent sql.NullString
		if err := rows.Scan(&id, &startedAt, &endedAt, &lastEvent); err != nil {
			return nil, fmt.Errorf("analytics: recent sessions scan: %w", err)
		}

		// LastActivity = latest of started_at, ended_at, most recent event.
		last, ok := parseRFC3339(startedAt)
		if !ok {
			continue // unparseable started_at — skip rather than guess (fail-soft)
		}
		if endedAt.Valid {
			if t, ok := parseRFC3339(endedAt.String); ok && t.After(last) {
				last = t
			}
		}
		if lastEvent.Valid {
			if t, ok := parseRFC3339(lastEvent.String); ok && t.After(last) {
				last = t
			}
		}

		if last.Before(since) {
			continue // stale: crashed/abandoned or otherwise inactive
		}
		out = append(out, SessionLiveness{ID: id, Ended: endedAt.Valid, LastActivity: last})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("analytics: recent sessions rows: %w", err)
	}
	return out, nil
}

// parseRFC3339 parses a timestamp written by this package (always RFC3339 UTC).
func parseRFC3339(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
