// Package fleet computes a cross-session "fleet status" snapshot from the live
// state of multiple Claude Code sessions — the read-side aggregation behind a
// status-line badge such as "run:2 done:1 blk:1".
//
// It is pure and has zero dependencies on storage, dispatch, or the daemon: it
// takes a set of session records plus a clock and a staleness window and returns
// counts. The write side (recording a session's status + heartbeat) and the
// status-line wiring live elsewhere; this package is the load-bearing, fully
// unit-tested core (the "Model D" snapshot — see issue #211).
//
// Liveness rule: a session whose heartbeat is older than the staleness window is
// treated as crashed/abandoned and excluded from every count, so a badge never
// reports work that is no longer happening.
package fleet

import (
	"strconv"
	"strings"
	"time"
)

// Status is a session's current lifecycle state.
type Status string

const (
	// StatusRunning: the session is mid-turn / actively working.
	StatusRunning Status = "running"
	// StatusDone: the session finished its last turn (Stop seam).
	StatusDone Status = "done"
	// StatusBlocked: the session is waiting on the user (a guard confirm, a
	// permission prompt) or surfaced an error.
	StatusBlocked Status = "blocked"
)

// Session is a point-in-time record of one session's state, as persisted by the
// write side.
type Session struct {
	ID            string
	Status        Status
	LastHeartbeat time.Time
}

// Snapshot is the aggregated, staleness-filtered fleet state.
type Snapshot struct {
	Running int
	Done    int
	Blocked int
	Other   int // sessions with an unrecognised status string
	Total   int // live (non-stale) sessions across all statuses
}

// Aggregate computes a Snapshot from sessions as of now, excluding any whose
// heartbeat is older than staleness (age strictly greater than staleness is
// stale; age == staleness is still live). A nil/empty slice yields the zero
// Snapshot.
func Aggregate(sessions []Session, now time.Time, staleness time.Duration) Snapshot {
	var s Snapshot
	for _, sess := range sessions {
		age := now.Sub(sess.LastHeartbeat)
		if age > staleness {
			continue // crashed/abandoned — not part of the live fleet
		}
		s.Total++
		switch sess.Status {
		case StatusRunning:
			s.Running++
		case StatusDone:
			s.Done++
		case StatusBlocked:
			s.Blocked++
		default:
			s.Other++
		}
	}
	return s
}

// Badge renders a compact, terminal-safe status-line fragment, e.g.
// "run:2 done:1 blk:1". Categories with a zero count are omitted; a Snapshot
// with no live sessions renders the empty string. Only the three canonical
// states are surfaced — Other counts toward Total but is intentionally not shown
// (an unknown status is a write-side bug, not something to advertise).
func (s Snapshot) Badge() string {
	parts := make([]string, 0, 3)
	if s.Running > 0 {
		parts = append(parts, "run:"+strconv.Itoa(s.Running))
	}
	if s.Done > 0 {
		parts = append(parts, "done:"+strconv.Itoa(s.Done))
	}
	if s.Blocked > 0 {
		parts = append(parts, "blk:"+strconv.Itoa(s.Blocked))
	}
	return strings.Join(parts, " ")
}
