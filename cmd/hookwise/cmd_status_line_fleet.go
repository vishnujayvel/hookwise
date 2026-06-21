package main

import (
	"context"
	"time"

	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/fleet"
)

// fleetStalenessWindow bounds which sessions count as part of the live fleet.
// A session with no activity (start, end, or event) within this window is
// treated as crashed/abandoned and excluded — the badge only reflects work that
// is genuinely current. Mirrors the feed-cache TTL philosophy.
const fleetStalenessWindow = 15 * time.Minute

// renderFleetSegment renders a cross-session fleet badge (e.g. "fleet run:2
// done:1") from the live sessions recorded in the analytics DB at dbPath — the
// read side of the Model D fleet board (#211). Fail-open: any error (missing DB,
// query failure) yields an empty segment, so the status line never breaks.
func renderFleetSegment(dbPath string, now time.Time, staleness time.Duration) string {
	db, err := analytics.Open(dbPath)
	if err != nil {
		return ""
	}
	defer db.Close()
	return fleetBadge(analytics.NewAnalytics(db), now, staleness)
}

// fleetBadge is the testable core: it reads live sessions, aggregates them, and
// renders the badge. It returns "" when fewer than two sessions are live — a
// solo session is not a fleet, and a constant "run:1" would be noise rather than
// signal (precise self-exclusion would need the current session_id from the
// status-line stdin payload, a follow-up).
func fleetBadge(a *analytics.Analytics, now time.Time, staleness time.Duration) string {
	live, err := a.RecentSessions(context.Background(), now.Add(-staleness))
	if err != nil {
		return ""
	}

	sessions := make([]fleet.Session, 0, len(live))
	for _, s := range live {
		status := fleet.StatusRunning
		if s.Ended {
			status = fleet.StatusDone
		}
		sessions = append(sessions, fleet.Session{
			ID:            s.ID,
			Status:        status,
			LastHeartbeat: s.LastActivity,
		})
	}

	snap := fleet.Aggregate(sessions, now, staleness)
	if snap.Total < 2 {
		return "" // not a fleet — suppress solo-session noise
	}
	badge := snap.Badge()
	if badge == "" {
		return ""
	}
	return ansiCyan + "fleet " + badge + ansiReset
}
