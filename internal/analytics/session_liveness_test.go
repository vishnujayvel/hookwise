package analytics

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recentByID indexes a RecentSessions result for assertion convenience.
func recentByID(rs []SessionLiveness) map[string]SessionLiveness {
	m := make(map[string]SessionLiveness, len(rs))
	for _, r := range rs {
		m[r.ID] = r
	}
	return m
}

// TestRecentSessions_LivenessAndStatus pins the read-side projection behind the
// fleet badge (issue #211, Model D). Liveness = latest of {started_at, ended_at,
// most recent event}. A session is included iff that timestamp is at/after the
// `since` cutoff, so crashed/abandoned sessions (no end, no recent activity) and
// long-finished sessions both fall out. `Ended` distinguishes done from running.
func TestRecentSessions_LivenessAndStatus(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }

	// A: running, started long ago but a recent event keeps it live.
	require.NoError(t, a.StartSession(ctx, "A-running-active", ago(2*time.Hour)))
	require.NoError(t, a.RecordEvent(ctx, "A-running-active", EventRecord{
		EventType: "PostToolUse", ToolName: "Edit", Timestamp: ago(1 * time.Minute)}))

	// B: running, just started, no events yet — still live via started_at.
	require.NoError(t, a.StartSession(ctx, "B-running-fresh", ago(3*time.Minute)))

	// C: done recently (ended within window).
	require.NoError(t, a.StartSession(ctx, "C-done-recent", ago(30*time.Minute)))
	require.NoError(t, a.EndSession(ctx, "C-done-recent", ago(2*time.Minute), SessionStats{}))

	// D: crashed — started long ago, never ended, no recent activity.
	require.NoError(t, a.StartSession(ctx, "D-crashed", ago(3*time.Hour)))

	// E: done long ago — outside the window.
	require.NoError(t, a.StartSession(ctx, "E-done-old", ago(5*time.Hour)))
	require.NoError(t, a.EndSession(ctx, "E-done-old", ago(4*time.Hour), SessionStats{}))

	since := ago(15 * time.Minute)
	rs, err := a.RecentSessions(ctx, since)
	require.NoError(t, err)

	by := recentByID(rs)
	assert.Contains(t, by, "A-running-active")
	assert.Contains(t, by, "B-running-fresh")
	assert.Contains(t, by, "C-done-recent")
	assert.NotContains(t, by, "D-crashed", "crashed session (stale) must be excluded")
	assert.NotContains(t, by, "E-done-old", "long-finished session must be excluded")
	assert.Len(t, rs, 3)

	assert.False(t, by["A-running-active"].Ended, "running session is not Ended")
	assert.False(t, by["B-running-fresh"].Ended)
	assert.True(t, by["C-done-recent"].Ended, "ended session is Ended")

	// LastActivity reflects the most recent signal (A's event, not its old start).
	assert.WithinDuration(t, ago(1*time.Minute), by["A-running-active"].LastActivity, time.Second)
}

func TestRecentSessions_Empty(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()
	rs, err := a.RecentSessions(context.Background(), time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Empty(t, rs)
}
