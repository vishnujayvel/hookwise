package analytics

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testAnalytics opens a fresh Dolt DB and returns an Analytics instance.
func testAnalytics(t *testing.T) (*Analytics, func()) {
	t.Helper()
	db, cleanup := testOpen(t)
	return NewAnalytics(db), cleanup
}

// ---------------------------------------------------------------------------
// Test 1: StartSession creates a session row
// ---------------------------------------------------------------------------

func TestStartSession_CreatesRow(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Date(2025, 3, 6, 10, 0, 0, 0, time.UTC)

	err := a.StartSession(ctx, "sess-start-1", now)
	require.NoError(t, err)

	var id, startedAt string
	err = a.db.QueryRow(ctx, "SELECT id, started_at FROM sessions WHERE id = ?", "sess-start-1").
		Scan(&id, &startedAt)
	require.NoError(t, err)
	assert.Equal(t, "sess-start-1", id)
	assert.Equal(t, "2025-03-06T10:00:00Z", startedAt)
}

// ---------------------------------------------------------------------------
// Test 2: EndSession updates with stats
// ---------------------------------------------------------------------------

func TestEndSession_UpdatesStats(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	start := time.Date(2025, 3, 6, 10, 0, 0, 0, time.UTC)
	end := time.Date(2025, 3, 6, 11, 30, 0, 0, time.UTC)

	require.NoError(t, a.StartSession(ctx, "sess-end-1", start))

	stats := SessionStats{
		TotalToolCalls:     42,
		FileEditsCount:     7,
		AIAuthoredLines:    200,
		HumanVerifiedLines: 50,
		EstimatedCostUSD:   1.23,
	}
	err := a.EndSession(ctx, "sess-end-1", end, stats)
	require.NoError(t, err)

	var endedAt string
	var toolCalls, fileEdits, aiLines, humanLines int
	var cost float64
	err = a.db.QueryRow(ctx,
		`SELECT ended_at, total_tool_calls, file_edits_count,
		        ai_authored_lines, human_verified_lines, estimated_cost_usd
		 FROM sessions WHERE id = ?`, "sess-end-1").
		Scan(&endedAt, &toolCalls, &fileEdits, &aiLines, &humanLines, &cost)
	require.NoError(t, err)

	assert.Equal(t, "2025-03-06T11:30:00Z", endedAt)
	assert.Equal(t, 42, toolCalls)
	assert.Equal(t, 7, fileEdits)
	assert.Equal(t, 200, aiLines)
	assert.Equal(t, 50, humanLines)
	assert.InDelta(t, 1.23, cost, 0.001)
}

// ---------------------------------------------------------------------------
// Test 3: RecordEvent inserts into events table
// ---------------------------------------------------------------------------

func TestRecordEvent_InsertsRow(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	ts := time.Date(2025, 3, 6, 12, 0, 0, 0, time.UTC)

	// Create a session so the foreign-key-like relationship is valid.
	require.NoError(t, a.StartSession(ctx, "sess-ev-1", ts))

	event := EventRecord{
		EventType:         "PostToolUse",
		ToolName:          "Write",
		Timestamp:         ts,
		FilePath:          "/src/main.go",
		LinesAdded:        15,
		LinesRemoved:      3,
		AIConfidenceScore: 0.85,
	}
	err := a.RecordEvent(ctx, "sess-ev-1", event)
	require.NoError(t, err)

	var eventType, toolName, filePath string
	var linesAdded, linesRemoved int
	var aiScore float64
	err = a.db.QueryRow(ctx,
		`SELECT event_type, tool_name, file_path, lines_added, lines_removed, ai_confidence_score
		 FROM events WHERE session_id = ?`, "sess-ev-1").
		Scan(&eventType, &toolName, &filePath, &linesAdded, &linesRemoved, &aiScore)
	require.NoError(t, err)

	assert.Equal(t, "PostToolUse", eventType)
	assert.Equal(t, "Write", toolName)
	assert.Equal(t, "/src/main.go", filePath)
	assert.Equal(t, 15, linesAdded)
	assert.Equal(t, 3, linesRemoved)
	assert.InDelta(t, 0.85, aiScore, 0.001)
}

// ---------------------------------------------------------------------------
// Test 4: RecordAuthorship with all 4 classification levels
// ---------------------------------------------------------------------------

func TestRecordAuthorship_AllClassifications(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	ts := time.Date(2025, 3, 6, 13, 0, 0, 0, time.UTC)

	tests := []struct {
		score          float64
		classification string
	}{
		{0.95, "high_probability_ai"},
		{0.65, "likely_ai"},
		{0.35, "mixed_verified"},
		{0.10, "human_authored"},
	}

	for i, tc := range tests {
		entry := AuthorshipEntry{
			SessionID:    "sess-auth",
			FilePath:     "/src/file.go",
			ToolName:     "Write",
			LinesChanged: 10 + i,
			AIScore:      tc.score,
			Timestamp:    ts.Add(time.Duration(i) * time.Minute),
		}
		err := a.RecordAuthorship(ctx, entry)
		require.NoError(t, err, "score=%.2f", tc.score)
	}

	rows, err := a.db.Query(ctx,
		"SELECT ai_score, classification FROM authorship_ledger ORDER BY ai_score DESC")
	require.NoError(t, err)
	defer rows.Close()

	var results []struct {
		score          float64
		classification string
	}
	for rows.Next() {
		var s float64
		var c string
		require.NoError(t, rows.Scan(&s, &c))
		results = append(results, struct {
			score          float64
			classification string
		}{s, c})
	}
	require.NoError(t, rows.Err())
	require.Len(t, results, 4)

	assert.Equal(t, "high_probability_ai", results[0].classification)
	assert.Equal(t, "likely_ai", results[1].classification)
	assert.Equal(t, "mixed_verified", results[2].classification)
	assert.Equal(t, "human_authored", results[3].classification)
}

// ---------------------------------------------------------------------------
// Test 5: ClassifyAuthorship boundary values
// ---------------------------------------------------------------------------

func TestClassifyAuthorship_Boundaries(t *testing.T) {
	tests := []struct {
		score    float64
		expected string
	}{
		{0.0, "human_authored"},
		{0.19, "human_authored"},
		{0.2, "mixed_verified"},
		{0.49, "mixed_verified"},
		{0.5, "likely_ai"},
		{0.79, "likely_ai"},
		{0.8, "high_probability_ai"},
		{1.0, "high_probability_ai"},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("score_%.2f", tc.score), func(t *testing.T) {
			got := ClassifyAuthorship(tc.score)
			assert.Equal(t, tc.expected, got, "score=%.2f", tc.score)
		})
	}
}

// ---------------------------------------------------------------------------
// Test 6: DailySummary with multiple sessions
// ---------------------------------------------------------------------------

func TestDailySummary_MultipleSessions(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	date := "2025-03-06"

	// Create two sessions on the same date.
	t1 := time.Date(2025, 3, 6, 9, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 3, 6, 14, 0, 0, 0, time.UTC)

	require.NoError(t, a.StartSession(ctx, "ds-s1", t1))
	require.NoError(t, a.StartSession(ctx, "ds-s2", t2))

	require.NoError(t, a.EndSession(ctx, "ds-s1", t1.Add(time.Hour), SessionStats{
		TotalToolCalls:     10,
		FileEditsCount:     3,
		AIAuthoredLines:    100,
		HumanVerifiedLines: 20,
		EstimatedCostUSD:   0.50,
	}))
	require.NoError(t, a.EndSession(ctx, "ds-s2", t2.Add(2*time.Hour), SessionStats{
		TotalToolCalls:     20,
		FileEditsCount:     5,
		AIAuthoredLines:    200,
		HumanVerifiedLines: 30,
		EstimatedCostUSD:   1.00,
	}))

	// Also insert some events on the same date.
	for i := 0; i < 5; i++ {
		require.NoError(t, a.RecordEvent(ctx, "ds-s1", EventRecord{
			EventType: "PostToolUse",
			ToolName:  "Write",
			Timestamp: t1.Add(time.Duration(i) * time.Minute),
		}))
	}

	result, err := a.DailySummary(ctx, date)
	require.NoError(t, err)

	assert.Equal(t, date, result.Date)
	assert.Equal(t, 2, result.TotalSessions)
	assert.Equal(t, 5, result.TotalEvents)
	assert.Equal(t, 30, result.TotalToolCalls)
	assert.Equal(t, 8, result.TotalFileEdits)
	assert.Equal(t, 300, result.AIAuthoredLines)
	assert.Equal(t, 50, result.HumanVerifiedLines)
	assert.InDelta(t, 1.50, result.EstimatedCostUSD, 0.001)
}

// ---------------------------------------------------------------------------
// Test 7: ToolBreakdown with mixed tools
// ---------------------------------------------------------------------------

func TestToolBreakdown_MixedTools(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	ts := time.Date(2025, 3, 6, 10, 0, 0, 0, time.UTC)
	require.NoError(t, a.StartSession(ctx, "tb-s1", ts))

	// 3 Write, 2 Bash, 1 Read = 6 total
	tools := []string{"Write", "Write", "Write", "Bash", "Bash", "Read"}
	for i, tool := range tools {
		require.NoError(t, a.RecordEvent(ctx, "tb-s1", EventRecord{
			EventType: "PostToolUse",
			ToolName:  tool,
			Timestamp: ts.Add(time.Duration(i) * time.Minute),
		}))
	}

	entries, err := a.ToolBreakdown(ctx, "2025-03-06")
	require.NoError(t, err)
	require.Len(t, entries, 3)

	// Ordered by count descending.
	assert.Equal(t, "Write", entries[0].ToolName)
	assert.Equal(t, 3, entries[0].Count)

	assert.Equal(t, "Bash", entries[1].ToolName)
	assert.Equal(t, 2, entries[1].Count)

	assert.Equal(t, "Read", entries[2].ToolName)
	assert.Equal(t, 1, entries[2].Count)
}

// ---------------------------------------------------------------------------
// Test 8: DailySummary with no data returns zero values
// ---------------------------------------------------------------------------

func TestDailySummary_NoData(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	result, err := a.DailySummary(ctx, "1999-01-01")
	require.NoError(t, err)

	assert.Equal(t, "1999-01-01", result.Date)
	assert.Equal(t, 0, result.TotalSessions)
	assert.Equal(t, 0, result.TotalEvents)
	assert.Equal(t, 0, result.TotalToolCalls)
	assert.Equal(t, 0, result.TotalFileEdits)
	assert.Equal(t, 0, result.AIAuthoredLines)
	assert.Equal(t, 0, result.HumanVerifiedLines)
	assert.InDelta(t, 0.0, result.EstimatedCostUSD, 0.001)
}

// ---------------------------------------------------------------------------
// Test 9: Full session lifecycle: start → record events → end → query stats
// ---------------------------------------------------------------------------

func TestSessionLifecycle_EndToEnd(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := "lifecycle-1"
	start := time.Date(2025, 4, 10, 8, 0, 0, 0, time.UTC)

	// 1. Start session.
	require.NoError(t, a.StartSession(ctx, sessionID, start))

	// 2. Record several events.
	events := []EventRecord{
		{EventType: "PostToolUse", ToolName: "Write", Timestamp: start.Add(1 * time.Minute), FilePath: "/a.go", LinesAdded: 20, AIConfidenceScore: 0.9},
		{EventType: "PostToolUse", ToolName: "Bash", Timestamp: start.Add(2 * time.Minute)},
		{EventType: "PostToolUse", ToolName: "Write", Timestamp: start.Add(3 * time.Minute), FilePath: "/b.go", LinesAdded: 10, LinesRemoved: 5, AIConfidenceScore: 0.3},
	}
	for _, ev := range events {
		require.NoError(t, a.RecordEvent(ctx, sessionID, ev))
	}

	// 3. Record authorship entries.
	require.NoError(t, a.RecordAuthorship(ctx, AuthorshipEntry{
		SessionID: sessionID, FilePath: "/a.go", ToolName: "Write",
		LinesChanged: 20, AIScore: 0.9, Timestamp: start.Add(1 * time.Minute),
	}))
	require.NoError(t, a.RecordAuthorship(ctx, AuthorshipEntry{
		SessionID: sessionID, FilePath: "/b.go", ToolName: "Write",
		LinesChanged: 10, AIScore: 0.3, Timestamp: start.Add(3 * time.Minute),
	}))

	// 4. End session.
	end := start.Add(30 * time.Minute)
	require.NoError(t, a.EndSession(ctx, sessionID, end, SessionStats{
		TotalToolCalls:     3,
		FileEditsCount:     2,
		AIAuthoredLines:    30,
		HumanVerifiedLines: 5,
		EstimatedCostUSD:   0.75,
	}))

	// 5. Query daily summary.
	summary, err := a.DailySummary(ctx, "2025-04-10")
	require.NoError(t, err)
	assert.Equal(t, 1, summary.TotalSessions)
	assert.Equal(t, 3, summary.TotalEvents)
	assert.Equal(t, 3, summary.TotalToolCalls)
	assert.Equal(t, 2, summary.TotalFileEdits)

	// 6. Query tool breakdown.
	breakdown, err := a.ToolBreakdown(ctx, "2025-04-10")
	require.NoError(t, err)
	require.Len(t, breakdown, 2) // Write and Bash
	assert.Equal(t, "Write", breakdown[0].ToolName)
	assert.Equal(t, 2, breakdown[0].Count)
	assert.Equal(t, "Bash", breakdown[1].ToolName)
	assert.Equal(t, 1, breakdown[1].Count)

	// 7. Verify authorship ledger.
	var count int
	err = a.db.QueryRow(ctx,
		"SELECT COUNT(*) FROM authorship_ledger WHERE session_id = ?", sessionID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

// ---------------------------------------------------------------------------
// Test 10: RecordEvent with missing optional fields
// ---------------------------------------------------------------------------

func TestRecordEvent_MissingOptionalFields(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	ts := time.Date(2025, 3, 6, 14, 0, 0, 0, time.UTC)
	require.NoError(t, a.StartSession(ctx, "sess-opt", ts))

	// Minimal event — only required fields; optionals are zero-valued.
	event := EventRecord{
		EventType: "PostToolUse",
		Timestamp: ts,
	}
	err := a.RecordEvent(ctx, "sess-opt", event)
	require.NoError(t, err)

	var toolName, filePath string
	var linesAdded, linesRemoved int
	var aiScore float64
	err = a.db.QueryRow(ctx,
		`SELECT tool_name, file_path, lines_added, lines_removed, ai_confidence_score
		 FROM events WHERE session_id = ?`, "sess-opt").
		Scan(&toolName, &filePath, &linesAdded, &linesRemoved, &aiScore)
	require.NoError(t, err)

	assert.Equal(t, "", toolName)
	assert.Equal(t, "", filePath)
	assert.Equal(t, 0, linesAdded)
	assert.Equal(t, 0, linesRemoved)
	assert.InDelta(t, 0.0, aiScore, 0.001)
}

// ---------------------------------------------------------------------------
// Test 11: EndSession for non-existent session (upsert-safe)
// ---------------------------------------------------------------------------

func TestEndSession_NonExistent_NoError(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	end := time.Date(2025, 3, 6, 12, 0, 0, 0, time.UTC)

	// Ending a session that was never started should not error.
	err := a.EndSession(ctx, "does-not-exist", end, SessionStats{
		TotalToolCalls: 5,
	})
	require.NoError(t, err)

	// Verify nothing was inserted.
	var count int
	err = a.db.QueryRow(ctx, "SELECT COUNT(*) FROM sessions WHERE id = ?", "does-not-exist").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// ---------------------------------------------------------------------------
// Test 12: ToolBreakdown percentages sum to ~100%
// ---------------------------------------------------------------------------

func TestToolBreakdown_PercentagesSum(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	ts := time.Date(2025, 3, 6, 15, 0, 0, 0, time.UTC)
	require.NoError(t, a.StartSession(ctx, "pct-s1", ts))

	// Insert 7 events across 3 tools.
	tools := []string{"Write", "Write", "Write", "Bash", "Bash", "Read", "Read"}
	for i, tool := range tools {
		require.NoError(t, a.RecordEvent(ctx, "pct-s1", EventRecord{
			EventType: "PostToolUse",
			ToolName:  tool,
			Timestamp: ts.Add(time.Duration(i) * time.Minute),
		}))
	}

	entries, err := a.ToolBreakdown(ctx, "2025-03-06")
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	var total float64
	for _, e := range entries {
		total += e.Percentage
	}
	assert.InDelta(t, 100.0, total, 0.5, "percentages should sum to approximately 100%%")
}

// ---------------------------------------------------------------------------
// Test 13: NewAnalytics returns non-nil
// ---------------------------------------------------------------------------

func TestNewAnalytics_NotNil(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	a := NewAnalytics(db)
	require.NotNil(t, a)
	assert.Equal(t, db, a.db)
}

// ---------------------------------------------------------------------------
// Test 14: Multiple events for same session
// ---------------------------------------------------------------------------

func TestRecordEvent_MultipleForSameSession(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	ts := time.Date(2025, 3, 6, 16, 0, 0, 0, time.UTC)
	require.NoError(t, a.StartSession(ctx, "multi-ev", ts))

	for i := 0; i < 5; i++ {
		require.NoError(t, a.RecordEvent(ctx, "multi-ev", EventRecord{
			EventType: "PostToolUse",
			ToolName:  "Write",
			Timestamp: ts.Add(time.Duration(i) * time.Minute),
			FilePath:  fmt.Sprintf("/file_%d.go", i),
			LinesAdded: 10,
		}))
	}

	var count int
	err := a.db.QueryRow(ctx, "SELECT COUNT(*) FROM events WHERE session_id = ?", "multi-ev").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 5, count)
}

// ---------------------------------------------------------------------------
// Test 15: ToolBreakdown with no events returns empty slice
// ---------------------------------------------------------------------------

func TestToolBreakdown_NoEvents(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()
	entries, err := a.ToolBreakdown(ctx, "1999-01-01")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// ---------------------------------------------------------------------------
// Test 16: DailySummary isolates dates correctly
// ---------------------------------------------------------------------------

func TestDailySummary_DateIsolation(t *testing.T) {
	a, cleanup := testAnalytics(t)
	defer cleanup()

	ctx := context.Background()

	// Session on 2025-03-05
	t1 := time.Date(2025, 3, 5, 10, 0, 0, 0, time.UTC)
	require.NoError(t, a.StartSession(ctx, "iso-s1", t1))
	require.NoError(t, a.EndSession(ctx, "iso-s1", t1.Add(time.Hour), SessionStats{
		TotalToolCalls: 10,
	}))

	// Session on 2025-03-06
	t2 := time.Date(2025, 3, 6, 10, 0, 0, 0, time.UTC)
	require.NoError(t, a.StartSession(ctx, "iso-s2", t2))
	require.NoError(t, a.EndSession(ctx, "iso-s2", t2.Add(time.Hour), SessionStats{
		TotalToolCalls: 20,
	}))

	// Query for 2025-03-06 should only see the second session.
	result, err := a.DailySummary(ctx, "2025-03-06")
	require.NoError(t, err)
	assert.Equal(t, 1, result.TotalSessions)
	assert.Equal(t, 20, result.TotalToolCalls)

	// Query for 2025-03-05 should only see the first session.
	result, err = a.DailySummary(ctx, "2025-03-05")
	require.NoError(t, err)
	assert.Equal(t, 1, result.TotalSessions)
	assert.Equal(t, 10, result.TotalToolCalls)
}

