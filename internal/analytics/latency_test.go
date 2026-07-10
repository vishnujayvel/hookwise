package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyEventsDDL is the pre-gh#37 events table shape, without the
// dispatch_latency_ms column, used to simulate a DB created by an older
// hookwise binary.
const legacyEventsDDL = `CREATE TABLE events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	tool_name TEXT,
	timestamp TEXT NOT NULL,
	file_path TEXT,
	lines_added INTEGER DEFAULT 0,
	lines_removed INTEGER DEFAULT 0,
	ai_confidence_score REAL
)`

// eventsColumns returns the column names of the events table.
func eventsColumns(t *testing.T, dbPath string) []string {
	t.Helper()
	raw, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer raw.Close()

	rows, err := raw.Query("PRAGMA table_info(events)")
	require.NoError(t, err)
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
		cols = append(cols, name)
	}
	require.NoError(t, rows.Err())
	return cols
}

// TestMigration_DispatchLatencyColumn_OldDB verifies the gh#37 migration:
// opening a DB whose events table predates dispatch_latency_ms adds the
// column, and pre-existing rows read back as NULL (never fabricated as 0).
func TestMigration_DispatchLatencyColumn_OldDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")

	// Build an "old binary" DB: legacy events table + one pre-migration row.
	raw, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = raw.Exec(legacyEventsDDL)
	require.NoError(t, err)
	_, err = raw.Exec(
		`INSERT INTO events (session_id, event_type, tool_name, timestamp) VALUES (?, ?, ?, ?)`,
		"old-session", "PostToolUse", "Bash", "2026-01-01T00:00:00Z")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	assert.NotContains(t, eventsColumns(t, dbPath), "dispatch_latency_ms",
		"precondition: legacy DB must not have the column yet")

	// Opening through the analytics layer must run the migration.
	db, err := Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	assert.Contains(t, eventsColumns(t, dbPath), "dispatch_latency_ms",
		"Open must add dispatch_latency_ms to a pre-existing events table")

	// Pre-migration rows stay NULL.
	db, err = Open(dbPath)
	require.NoError(t, err)
	defer db.Close()
	var latency sql.NullInt64
	require.NoError(t, db.QueryRow(context.Background(),
		"SELECT dispatch_latency_ms FROM events WHERE session_id = ?", "old-session",
	).Scan(&latency))
	assert.False(t, latency.Valid, "pre-migration rows must read back as NULL, not 0")
}

// TestMigration_DispatchLatencyColumn_Idempotent verifies re-opening an
// already-migrated DB is a no-op (the tolerate-duplicate-column guard), for
// both migrated-old and created-fresh DBs.
func TestMigration_DispatchLatencyColumn_Idempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")

	for i := 0; i < 3; i++ {
		db, err := Open(dbPath)
		require.NoError(t, err, "open #%d must not fail on an already-present column", i+1)
		require.NoError(t, db.Close())
	}
	cols := eventsColumns(t, dbPath)
	count := 0
	for _, c := range cols {
		if c == "dispatch_latency_ms" {
			count++
		}
	}
	assert.Equal(t, 1, count, "column must exist exactly once after repeated opens")
}

// seedLatencyEvent inserts one PostToolUse event with the given latency at
// the given timestamp.
func seedLatencyEvent(t *testing.T, a *Analytics, ts time.Time, latencyMs int64) {
	t.Helper()
	require.NoError(t, a.RecordEvent(context.Background(), "latency-session", EventRecord{
		EventType:         "PostToolUse",
		ToolName:          "Bash",
		Timestamp:         ts,
		DispatchLatencyMs: &latencyMs,
	}))
}

// TestLatencyStats_Percentiles seeds a known distribution and verifies
// avg/p50/p95/p99/max via the nearest-rank method.
func TestLatencyStats_Percentiles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	db, err := Open(dbPath)
	require.NoError(t, err)
	defer db.Close()
	a := NewAnalytics(db)

	// 100 values: 1..100ms. Nearest-rank: p50=50, p95=95, p99=99, max=100.
	now := time.Now().UTC()
	for ms := int64(1); ms <= 100; ms++ {
		seedLatencyEvent(t, a, now, ms)
	}

	result, err := a.LatencyStats(context.Background(), now.Format("2006-01-02"))
	require.NoError(t, err)

	overall := result.Overall
	assert.Equal(t, 100, overall.Count)
	assert.InDelta(t, 50.5, overall.AvgMs, 0.001)
	assert.Equal(t, int64(50), overall.P50Ms)
	assert.Equal(t, int64(95), overall.P95Ms)
	assert.Equal(t, int64(99), overall.P99Ms)
	assert.Equal(t, int64(100), overall.MaxMs)

	require.Len(t, result.ByEvent, 1)
	assert.Equal(t, "PostToolUse", result.ByEvent[0].EventType)
	assert.Equal(t, overall.P95Ms, result.ByEvent[0].P95Ms,
		"single event type must mirror the overall percentiles")
}

// TestLatencyStats_PerEventTypeOrdering verifies the per-type breakdown is
// grouped correctly and ordered by count descending.
func TestLatencyStats_PerEventTypeOrdering(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	db, err := Open(dbPath)
	require.NoError(t, err)
	defer db.Close()
	a := NewAnalytics(db)

	now := time.Now().UTC()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		ms := int64(10 * (i + 1))
		require.NoError(t, a.RecordEvent(ctx, "s", EventRecord{
			EventType: "PostToolUse", Timestamp: now, DispatchLatencyMs: &ms}))
	}
	ms := int64(200)
	require.NoError(t, a.RecordEvent(ctx, "s", EventRecord{
		EventType: "PreToolUse", Timestamp: now, DispatchLatencyMs: &ms}))

	result, err := a.LatencyStats(ctx, now.Format("2006-01-02"))
	require.NoError(t, err)

	assert.Equal(t, 4, result.Overall.Count)
	require.Len(t, result.ByEvent, 2)
	assert.Equal(t, "PostToolUse", result.ByEvent[0].EventType, "higher count first")
	assert.Equal(t, 3, result.ByEvent[0].Count)
	assert.Equal(t, "PreToolUse", result.ByEvent[1].EventType)
	assert.Equal(t, int64(200), result.ByEvent[1].MaxMs)
}

// TestLatencyStats_NullRowsExcluded verifies that events without a measured
// latency (NULL rows, e.g. written by an older binary after migration) are
// excluded from the aggregates rather than counted as 0ms.
func TestLatencyStats_NullRowsExcluded(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	db, err := Open(dbPath)
	require.NoError(t, err)
	defer db.Close()
	a := NewAnalytics(db)

	now := time.Now().UTC()
	ctx := context.Background()
	// One NULL-latency event and one measured event.
	require.NoError(t, a.RecordEvent(ctx, "s", EventRecord{
		EventType: "PostToolUse", Timestamp: now})) // DispatchLatencyMs nil
	ms := int64(40)
	require.NoError(t, a.RecordEvent(ctx, "s", EventRecord{
		EventType: "PostToolUse", Timestamp: now, DispatchLatencyMs: &ms}))

	result, err := a.LatencyStats(ctx, now.Format("2006-01-02"))
	require.NoError(t, err)

	assert.Equal(t, 1, result.Overall.Count, "NULL rows must not count")
	assert.InDelta(t, 40.0, result.Overall.AvgMs, 0.001,
		"a NULL row averaged in as 0 would drag this to 20")
}

// TestLatencyStats_Empty verifies the no-data contract used by stats to skip
// the section: Overall.Count == 0 and no per-type buckets.
func TestLatencyStats_Empty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	db, err := Open(dbPath)
	require.NoError(t, err)
	defer db.Close()

	result, err := NewAnalytics(db).LatencyStats(context.Background(),
		time.Now().UTC().Format("2006-01-02"))
	require.NoError(t, err)

	assert.Equal(t, 0, result.Overall.Count)
	assert.Empty(t, result.ByEvent)
}

// TestPercentileNearestRank pins the nearest-rank edge cases the bucket math
// relies on (single element, small N where ranks collapse).
func TestPercentileNearestRank(t *testing.T) {
	cases := []struct {
		name   string
		sorted []int64
		q      float64
		want   int64
	}{
		{"single element p50", []int64{7}, 0.50, 7},
		{"single element p99", []int64{7}, 0.99, 7},
		{"two elements p50 is first", []int64{1, 9}, 0.50, 1},
		{"two elements p95 is second", []int64{1, 9}, 0.95, 9},
		{"ten elements p99 is last", []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 0.99, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, percentileNearestRank(tc.sorted, tc.q),
				fmt.Sprintf("q=%v over %v", tc.q, tc.sorted))
		})
	}
}
