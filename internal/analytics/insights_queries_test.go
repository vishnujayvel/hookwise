package analytics

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test 1: Empty DB returns zero-value InsightsSummary, nil error
// ---------------------------------------------------------------------------

func TestInsightsSummary_EmptyDB(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	got, err := a.InsightsSummary(context.Background(), 30)
	require.NoError(t, err)
	assert.Equal(t, InsightsSummary{}, got, "empty DB should return zero-value summary")
}

// ---------------------------------------------------------------------------
// Test 2: Multi-session fixture — all fields asserted
// ---------------------------------------------------------------------------

func TestInsightsSummary_MultiSession(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()

	// Session 1: day 1, 2025-03-05, 60-minute duration
	t1Start := time.Date(2025, 3, 5, 9, 0, 0, 0, time.UTC)
	t1End := t1Start.Add(60 * time.Minute)
	require.NoError(t, a.StartSession(ctx, "ins-s1", t1Start))
	require.NoError(t, a.EndSession(ctx, "ins-s1", t1End, SessionStats{EstimatedCostUSD: 0.10}))

	// Events for session 1 — Write ×3, Bash ×1, at hour 09
	for i, tool := range []string{"Write", "Write", "Write", "Bash"} {
		require.NoError(t, a.RecordEvent(ctx, "ins-s1", EventRecord{
			EventType:  "PostToolUse",
			ToolName:   tool,
			Timestamp:  t1Start.Add(time.Duration(i) * time.Minute),
			LinesAdded: 10,
		}))
	}

	// Session 2: day 2, 2025-03-06, 120-minute duration
	t2Start := time.Date(2025, 3, 6, 14, 0, 0, 0, time.UTC)
	t2End := t2Start.Add(120 * time.Minute)
	require.NoError(t, a.StartSession(ctx, "ins-s2", t2Start))
	require.NoError(t, a.EndSession(ctx, "ins-s2", t2End, SessionStats{EstimatedCostUSD: 0.20}))

	// Events for session 2 — Write ×1, Read ×2, at hour 14
	for i, tool := range []string{"Write", "Read", "Read"} {
		require.NoError(t, a.RecordEvent(ctx, "ins-s2", EventRecord{
			EventType:  "PostToolUse",
			ToolName:   tool,
			Timestamp:  t2Start.Add(time.Duration(i) * time.Minute),
			LinesAdded: 5,
		}))
	}

	// cutoffDays=0 means no cutoff — include all sessions.
	got, err := a.InsightsSummary(ctx, 0)
	require.NoError(t, err)

	// TotalSessions: 2
	assert.Equal(t, 2, got.TotalSessions)

	// TotalLinesAdded: 4×10 + 3×5 = 55
	assert.Equal(t, 55, got.TotalLinesAdded)

	// AvgDurationMin: (60 + 120) / 2 = 90
	assert.InDelta(t, 90.0, got.AvgDurationMin, 0.01)

	// DaysActive: 2 distinct dates
	assert.Equal(t, 2, got.DaysActive)

	// TopTools: Write appears 4 times (s1×3 + s2×1), Read 2 times, Bash 1 time
	require.GreaterOrEqual(t, len(got.TopTools), 3)
	assert.Equal(t, "Write", got.TopTools[0].Name)
	assert.Equal(t, 4, got.TopTools[0].Count)
	assert.Equal(t, "Read", got.TopTools[1].Name)
	assert.Equal(t, 2, got.TopTools[1].Count)
	assert.Equal(t, "Bash", got.TopTools[2].Name)
	assert.Equal(t, 1, got.TopTools[2].Count)

	// PeakHourUTC: hour 9 has 4 events, hour 14 has 3 → peak = 9
	assert.Equal(t, 9, got.PeakHourUTC)

	// Recent: session ins-s2 (later started_at), 120-min duration, 15 lines, $0.20
	assert.Equal(t, "ins-s2", got.Recent.ID)
	assert.InDelta(t, 120.0, got.Recent.DurationMin, 0.01)
	assert.Equal(t, 15, got.Recent.LinesAdded)
	assert.InDelta(t, 0.20, got.Recent.EstCostUSD, 0.001)
}

// ---------------------------------------------------------------------------
// Test 3: Session without ended_at — counted in totals, excluded from AvgDuration
// ---------------------------------------------------------------------------

func TestInsightsSummary_SessionWithoutEndedAt(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()

	// Completed session: 30-minute duration
	start1 := time.Date(2025, 4, 1, 10, 0, 0, 0, time.UTC)
	require.NoError(t, a.StartSession(ctx, "ins-e1", start1))
	require.NoError(t, a.EndSession(ctx, "ins-e1", start1.Add(30*time.Minute), SessionStats{}))

	// Open session (no ended_at): started on 2025-04-02
	start2 := time.Date(2025, 4, 2, 11, 0, 0, 0, time.UTC)
	require.NoError(t, a.StartSession(ctx, "ins-e2", start2))
	// No EndSession call → ended_at remains NULL.

	got, err := a.InsightsSummary(ctx, 0)
	require.NoError(t, err)

	// Both sessions counted.
	assert.Equal(t, 2, got.TotalSessions)

	// DaysActive: 2 distinct dates.
	assert.Equal(t, 2, got.DaysActive)

	// AvgDurationMin: only ins-e1 qualifies (30 min). ins-e2 has no ended_at.
	assert.InDelta(t, 30.0, got.AvgDurationMin, 0.01)
}

// ---------------------------------------------------------------------------
// Test 4: Cutoff filtering — sessions outside the window excluded
// ---------------------------------------------------------------------------

func TestInsightsSummary_CutoffFiltering(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()

	// Old session: 1 year ago — must be outside any short cutoff.
	oldStart := time.Now().UTC().Add(-400 * 24 * time.Hour)
	require.NoError(t, a.StartSession(ctx, "ins-old", oldStart))
	require.NoError(t, a.EndSession(ctx, "ins-old", oldStart.Add(60*time.Minute), SessionStats{EstimatedCostUSD: 5.0}))
	require.NoError(t, a.RecordEvent(ctx, "ins-old", EventRecord{
		EventType:  "PostToolUse",
		ToolName:   "Write",
		Timestamp:  oldStart,
		LinesAdded: 100,
	}))

	// Recent session: 5 days ago — within a 30-day window.
	recentStart := time.Now().UTC().Add(-5 * 24 * time.Hour)
	require.NoError(t, a.StartSession(ctx, "ins-new", recentStart))
	require.NoError(t, a.EndSession(ctx, "ins-new", recentStart.Add(20*time.Minute), SessionStats{EstimatedCostUSD: 0.50}))
	require.NoError(t, a.RecordEvent(ctx, "ins-new", EventRecord{
		EventType:  "PostToolUse",
		ToolName:   "Bash",
		Timestamp:  recentStart,
		LinesAdded: 7,
	}))

	got, err := a.InsightsSummary(ctx, 30)
	require.NoError(t, err)

	// Only the recent session should be in the window.
	assert.Equal(t, 1, got.TotalSessions)
	assert.Equal(t, 7, got.TotalLinesAdded)
	assert.InDelta(t, 20.0, got.AvgDurationMin, 0.01)

	// Recent is ins-new.
	assert.Equal(t, "ins-new", got.Recent.ID)
	assert.InDelta(t, 0.50, got.Recent.EstCostUSD, 0.001)

	// TopTools should only contain Bash (old session's Write excluded).
	require.Len(t, got.TopTools, 1)
	assert.Equal(t, "Bash", got.TopTools[0].Name)
}

// ---------------------------------------------------------------------------
// Test 5: TopTools capped at 10 entries
// ---------------------------------------------------------------------------

func TestInsightsSummary_TopToolsCappedAt10(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	ts := time.Date(2025, 5, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, a.StartSession(ctx, "ins-cap", ts))

	// Insert 12 distinct tool names.
	toolNames := []string{
		"Alpha", "Beta", "Gamma", "Delta", "Epsilon",
		"Zeta", "Eta", "Theta", "Iota", "Kappa",
		"Lambda", "Mu",
	}
	for i, name := range toolNames {
		require.NoError(t, a.RecordEvent(ctx, "ins-cap", EventRecord{
			EventType: "PostToolUse",
			ToolName:  name,
			Timestamp: ts.Add(time.Duration(i) * time.Minute),
		}))
	}

	got, err := a.InsightsSummary(ctx, 0)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(got.TopTools), 10, "TopTools should be capped at 10")
}

// ---------------------------------------------------------------------------
// Test 6: RecentDaysActive scoped to 7-day window regardless of cutoffDays
// ---------------------------------------------------------------------------

func TestInsightsSummary_RecentDaysActive(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()

	// Session within last 7 days (2 days ago).
	recent := time.Now().UTC().Add(-2 * 24 * time.Hour)
	require.NoError(t, a.StartSession(ctx, "ins-r7", recent))
	require.NoError(t, a.EndSession(ctx, "ins-r7", recent.Add(10*time.Minute), SessionStats{}))

	// Session 20 days ago (within 30-day window but outside 7-day window).
	older := time.Now().UTC().Add(-20 * 24 * time.Hour)
	require.NoError(t, a.StartSession(ctx, "ins-r20", older))
	require.NoError(t, a.EndSession(ctx, "ins-r20", older.Add(10*time.Minute), SessionStats{}))

	got, err := a.InsightsSummary(ctx, 30)
	require.NoError(t, err)

	// Both sessions visible in the 30-day window.
	assert.Equal(t, 2, got.TotalSessions)

	// RecentDaysActive should only count the session within the last 7 days.
	assert.Equal(t, 1, got.RecentDaysActive)
}
