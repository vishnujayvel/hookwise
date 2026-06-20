package fleet

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fixed reference time so tests are deterministic.
var now = time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

const staleness = 2 * time.Minute

func sess(id string, status Status, ageSeconds int) Session {
	return Session{ID: id, Status: status, LastHeartbeat: now.Add(-time.Duration(ageSeconds) * time.Second)}
}

func TestAggregate_Empty(t *testing.T) {
	s := Aggregate(nil, now, staleness)
	assert.Equal(t, Snapshot{}, s)
	assert.Equal(t, 0, s.Total)
	assert.Equal(t, "", s.Badge(), "no sessions renders nothing")
}

func TestAggregate_MixedFresh(t *testing.T) {
	s := Aggregate([]Session{
		sess("a", StatusRunning, 5),
		sess("b", StatusRunning, 10),
		sess("c", StatusDone, 1),
		sess("d", StatusBlocked, 3),
	}, now, staleness)

	assert.Equal(t, 2, s.Running)
	assert.Equal(t, 1, s.Done)
	assert.Equal(t, 1, s.Blocked)
	assert.Equal(t, 0, s.Other)
	assert.Equal(t, 4, s.Total)
	assert.Equal(t, "run:2 done:1 blk:1", s.Badge())
}

// A session whose heartbeat is older than the staleness window is treated as
// crashed/abandoned and excluded from every count (the Model D liveness rule).
func TestAggregate_StaleExcluded(t *testing.T) {
	s := Aggregate([]Session{
		sess("live", StatusRunning, 30),
		sess("crashed", StatusRunning, 600), // 10m old, staleness is 2m
	}, now, staleness)

	assert.Equal(t, 1, s.Running, "stale session must not be counted")
	assert.Equal(t, 1, s.Total)
	assert.Equal(t, "run:1", s.Badge())
}

// A session exactly at the staleness boundary is still live (boundary is
// exclusive on the stale side: age == staleness counts as fresh).
func TestAggregate_BoundaryIsFresh(t *testing.T) {
	s := Aggregate([]Session{sess("edge", StatusRunning, int(staleness.Seconds()))}, now, staleness)
	assert.Equal(t, 1, s.Running)
	assert.Equal(t, 1, s.Total)
}

// Unknown/unrecognised status strings are tallied under Other and counted in
// Total (so the badge's Total never undercounts), but are not shown in the
// compact badge, which only surfaces the three canonical states.
func TestAggregate_UnknownStatusToOther(t *testing.T) {
	s := Aggregate([]Session{
		sess("x", Status("frobnicating"), 5),
		sess("y", StatusDone, 5),
	}, now, staleness)

	assert.Equal(t, 1, s.Other)
	assert.Equal(t, 1, s.Done)
	assert.Equal(t, 2, s.Total)
	assert.Equal(t, "done:1", s.Badge(), "unknown statuses are excluded from the compact badge")
}

func TestBadge_OmitsZeroCategories(t *testing.T) {
	assert.Equal(t, "done:3", Snapshot{Done: 3, Total: 3}.Badge())
	assert.Equal(t, "run:1 blk:2", Snapshot{Running: 1, Blocked: 2, Total: 3}.Badge())
	assert.Equal(t, "", Snapshot{}.Badge())
}
