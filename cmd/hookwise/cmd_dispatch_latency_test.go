package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// queryLatency reads the dispatch_latency_ms of the single event row for the
// given session.
func queryLatency(t *testing.T, dbPath, sessionID string) sql.NullInt64 {
	t.Helper()
	db := openTestDB(t, dbPath)
	defer db.Close()
	var latency sql.NullInt64
	require.NoError(t, db.QueryRow(context.Background(),
		"SELECT dispatch_latency_ms FROM events WHERE session_id = ?", sessionID,
	).Scan(&latency))
	return latency
}

// TestRecordAnalytics_WritesDispatchLatency is the gh#37 write-path test: a
// PostToolUse dispatch with a measured latency must persist a non-NULL
// dispatch_latency_ms on the event row.
func TestRecordAnalytics_WritesDispatchLatency(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))
	dbPath := filepath.Join(tmpDir, "analytics.db")
	sid := "latency-write-001"

	latencyMs := int64(12)
	recordAnalytics(context.Background(), core.EventPostToolUse,
		core.HookPayload{SessionID: sid, ToolName: "Bash"},
		dbPath, core.CostTrackingConfig{}, &latencyMs)

	got := queryLatency(t, dbPath, sid)
	require.True(t, got.Valid, "dispatch must write a non-NULL latency")
	assert.Equal(t, int64(12), got.Int64)
}

// TestRecordAnalytics_NilLatencyStaysNull verifies the nil contract: callers
// without a measurement (and a real 0ms dispatch stays distinguishable from
// them) persist NULL, never a fabricated value.
func TestRecordAnalytics_NilLatencyStaysNull(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))
	dbPath := filepath.Join(tmpDir, "analytics.db")
	sid := "latency-nil-001"

	recordAnalytics(context.Background(), core.EventPostToolUse,
		core.HookPayload{SessionID: sid, ToolName: "Bash"},
		dbPath, core.CostTrackingConfig{}, nil)

	got := queryLatency(t, dbPath, sid)
	assert.False(t, got.Valid, "nil latency must persist as NULL")
}

// TestRecordAnalytics_ZeroLatencyIsNotNull pins the 0ms-vs-NULL distinction:
// a sub-millisecond dispatch rounds to 0ms and must still persist as a real
// measurement, not be dropped as "absent".
func TestRecordAnalytics_ZeroLatencyIsNotNull(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))
	dbPath := filepath.Join(tmpDir, "analytics.db")
	sid := "latency-zero-001"

	latencyMs := int64(0)
	recordAnalytics(context.Background(), core.EventPostToolUse,
		core.HookPayload{SessionID: sid, ToolName: "Bash"},
		dbPath, core.CostTrackingConfig{}, &latencyMs)

	got := queryLatency(t, dbPath, sid)
	require.True(t, got.Valid, "a real 0ms measurement must not persist as NULL")
	assert.Equal(t, int64(0), got.Int64)
}

// seedLatencyEvents writes n PostToolUse events with the given latencies at
// the given timestamp directly through the analytics layer.
func seedLatencyEvents(t *testing.T, dbPath string, ts time.Time, latencies ...int64) {
	t.Helper()
	db := openTestDB(t, dbPath)
	defer db.Close()
	a := analytics.NewAnalytics(db)
	for i, ms := range latencies {
		ms := ms
		require.NoError(t, a.RecordEvent(context.Background(),
			fmt.Sprintf("latency-seed-%d", i),
			analytics.EventRecord{
				EventType:         "PostToolUse",
				ToolName:          "Bash",
				Timestamp:         ts,
				DispatchLatencyMs: &ms,
			}))
	}
}

// TestRunStats_LatencySection verifies stats renders the latency section with
// percentiles when today's events carry latency data.
func TestRunStats_LatencySection(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))
	dbPath := filepath.Join(tmpDir, "analytics.db")

	// stats aggregates by UTC day; seed with UTC now so LIKE matches.
	seedLatencyEvents(t, dbPath, time.Now().UTC(), 10, 20, 30, 40)

	cmd := newStatsCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--data-dir", dbPath})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "Dispatch latency (ms):")
	assert.Contains(t, out, "overall")
	assert.Contains(t, out, "PostToolUse", "per-event-type breakdown must render")
	// 10,20,30,40 → avg 25.0, p50=20, p95/p99/max=40 (nearest-rank).
	assert.Contains(t, out, "avg   25.0")
	assert.Contains(t, out, "p50   20")
	assert.Contains(t, out, "max   40")
	assert.Contains(t, out, "(n=4)")
}

// TestRunStats_LatencySectionOmittedWhenEmpty is the older-DB honesty test:
// with no latency data at all (rows predate the column or no dispatches), the
// section must be absent entirely — never rendered with fabricated zeros.
func TestRunStats_LatencySectionOmittedWhenEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))
	dbPath := filepath.Join(tmpDir, "analytics.db")

	// Seed one event WITHOUT latency (the migrated-older-DB shape).
	db := openTestDB(t, dbPath)
	a := analytics.NewAnalytics(db)
	require.NoError(t, a.RecordEvent(context.Background(), "no-latency-session",
		analytics.EventRecord{EventType: "PostToolUse", ToolName: "Bash", Timestamp: time.Now().UTC()}))
	db.Close()

	cmd := newStatsCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--data-dir", dbPath})
	require.NoError(t, cmd.Execute())

	assert.NotContains(t, buf.String(), "Dispatch latency",
		"latency section must be skipped entirely when no latency data exists")
}
