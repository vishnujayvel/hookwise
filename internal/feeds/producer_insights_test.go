package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// ---- helpers ----------------------------------------------------------------

// writeJSON marshals v and writes it to dir/<name>.json.
func writeJSON(t *testing.T, dir, name string, v interface{}) {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".json"), data, 0o644))
}

// newInsightsProducer creates a producer pointed at a temp dir.
// It creates session-meta/ and facets/ subdirs and returns their paths.
func newInsightsProducer(t *testing.T, stalenessDays int) (p *InsightsProducer, sessionDir, facetDir string) {
	t.Helper()
	base := t.TempDir()
	sessionDir = filepath.Join(base, "session-meta")
	facetDir = filepath.Join(base, "facets")
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))
	require.NoError(t, os.MkdirAll(facetDir, 0o755))

	p = &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: base,
			StalenessDays: stalenessDays,
		},
	})
	return p, sessionDir, facetDir
}

// produce runs Produce and extracts the data sub-map from the envelope.
func produceData(t *testing.T, p *InsightsProducer) map[string]interface{} {
	t.Helper()
	result, err := p.Produce(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	env, ok := result.(map[string]interface{})
	require.True(t, ok, "result must be a map")
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok, "envelope must carry a data map")
	return data
}

// recentStart returns an RFC3339 timestamp offset from now (negative = past).
func recentStart(d time.Duration) string {
	return time.Now().UTC().Add(d).Format(time.RFC3339)
}

// ---- test functions ----------------------------------------------------------

// TestInsights_EmptyDir_ZeroedEnvelope pins ARCH-1: fail-open on missing data.
func TestInsights_EmptyDir_ZeroedEnvelope(t *testing.T) {
	p, _, _ := newInsightsProducer(t, 30)
	data := produceData(t, p)

	assert.Equal(t, 0, toInt(data["total_sessions"]), "total_sessions should be 0")
	assert.Equal(t, 0, toInt(data["total_messages"]), "total_messages should be 0")
	assert.Equal(t, 0, toInt(data["total_lines_added"]), "total_lines_added should be 0")
	assert.Equal(t, float64(0), toFloat(data["avg_duration_minutes"]), "avg_duration_minutes should be 0")
	assert.Equal(t, 0, toInt(data["friction_total"]), "friction_total should be 0")
	assert.Equal(t, 0, toInt(data["days_active"]), "days_active should be 0")
	assert.Equal(t, 30, toInt(data["staleness_days"]), "staleness_days should echo config")

	topTools, ok := data["top_tools"].([]map[string]interface{})
	require.True(t, ok, "top_tools must be []map[string]interface{}")
	assert.Empty(t, topTools, "top_tools should be empty slice")

	rs, ok := data["recent_session"].(map[string]interface{})
	require.True(t, ok, "recent_session must be a map")
	assert.Equal(t, "", rs["id"], "recent_session.id should be empty")
	assert.Equal(t, 0, toInt(rs["duration_minutes"]), "recent_session.duration_minutes should be 0")
	assert.Equal(t, 0, toInt(rs["lines_added"]), "recent_session.lines_added should be 0")
	assert.Equal(t, 0, toInt(rs["friction_count"]), "recent_session.friction_count should be 0")
	assert.Equal(t, 0, toInt(rs["tool_errors"]), "recent_session.tool_errors should be 0")
}

// TestInsights_MissingDir_ZeroedEnvelope ensures a nonexistent base dir is fail-open.
func TestInsights_MissingDir_ZeroedEnvelope(t *testing.T) {
	p := &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: filepath.Join(t.TempDir(), "does-not-exist"),
			StalenessDays: 30,
		},
	})
	data := produceData(t, p)
	assert.Equal(t, 0, toInt(data["total_sessions"]))
}

// TestInsights_StalenessDaysDefault pins that StalenessDays<=0 defaults to 30.
func TestInsights_StalenessDaysDefault(t *testing.T) {
	cases := []struct {
		name  string
		input int
	}{
		{"zero", 0},
		{"negative", -5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, _, _ := newInsightsProducer(t, tc.input)
			data := produceData(t, p)
			assert.Equal(t, 30, toInt(data["staleness_days"]),
				"staleness_days must echo the effective default (30) when config is %d", tc.input)
		})
	}
}

// TestInsights_StalenessFiltering pins that old sessions are excluded and recent ones counted.
func TestInsights_StalenessFiltering(t *testing.T) {
	p, sessionDir, _ := newInsightsProducer(t, 30)

	// Valid: 1 hour ago — within 30-day window.
	writeJSON(t, sessionDir, "recent", map[string]interface{}{
		"session_id":        "s-recent",
		"start_time":        recentStart(-1 * time.Hour),
		"user_message_count": 5,
		"lines_added":       10,
		"duration_minutes":  20.0,
	})

	// Stale: 90 days ago — outside 30-day window.
	writeJSON(t, sessionDir, "stale", map[string]interface{}{
		"session_id":        "s-stale",
		"start_time":        recentStart(-90 * 24 * time.Hour),
		"user_message_count": 99,
		"lines_added":       999,
		"duration_minutes":  99.0,
	})

	// Missing start_time: must be skipped, not panic.
	writeJSON(t, sessionDir, "no-start-time", map[string]interface{}{
		"session_id":        "s-notime",
		"user_message_count": 1,
	})

	// Unparseable start_time: must be skipped, not panic.
	writeJSON(t, sessionDir, "bad-start-time", map[string]interface{}{
		"session_id": "s-badtime",
		"start_time": "not-a-date",
	})

	data := produceData(t, p)
	assert.Equal(t, 1, toInt(data["total_sessions"]), "only the recent session should be counted")
	assert.Equal(t, 5, toInt(data["total_messages"]), "total_messages must come from valid session only")
	assert.Equal(t, 10, toInt(data["total_lines_added"]), "total_lines_added must come from valid session only")
}

// TestInsights_Aggregation pins total_messages, total_lines_added, and avg_duration_minutes.
func TestInsights_Aggregation(t *testing.T) {
	p, sessionDir, _ := newInsightsProducer(t, 30)

	sessions := []map[string]interface{}{
		{
			"session_id":        "s1",
			"start_time":        recentStart(-2 * time.Hour),
			"user_message_count": 7,
			"lines_added":       100,
			"duration_minutes":  10.0,
		},
		{
			"session_id":        "s2",
			"start_time":        recentStart(-3 * time.Hour),
			"user_message_count": 13,
			"lines_added":       200,
			"duration_minutes":  15.0,
		},
	}
	for i, s := range sessions {
		writeJSON(t, sessionDir, fmt.Sprintf("s%d", i+1), s)
	}

	data := produceData(t, p)
	assert.Equal(t, 2, toInt(data["total_sessions"]))
	assert.Equal(t, 20, toInt(data["total_messages"]), "total_messages = 7+13")
	assert.Equal(t, 300, toInt(data["total_lines_added"]), "total_lines_added = 100+200")
	// avg_duration = (10+15)/2 = 12.5, rounded to 1 decimal.
	assert.Equal(t, 12.5, toFloat(data["avg_duration_minutes"]),
		"avg_duration_minutes = 12.5 (mean of 10 and 15)")
}

// TestInsights_AvgDurationRounding pins math.Round(x*10)/10 behavior.
func TestInsights_AvgDurationRounding(t *testing.T) {
	p, sessionDir, _ := newInsightsProducer(t, 30)

	// 3 sessions: durations 7, 8, 9 → mean = 8.0 exactly.
	for i, dur := range []float64{7.0, 8.0, 9.0} {
		writeJSON(t, sessionDir, fmt.Sprintf("s%d", i), map[string]interface{}{
			"session_id":       fmt.Sprintf("sid-%d", i),
			"start_time":       recentStart(-time.Duration(i+1) * time.Hour),
			"duration_minutes": dur,
		})
	}
	data := produceData(t, p)
	assert.Equal(t, 8.0, toFloat(data["avg_duration_minutes"]))
}

// TestInsights_TopTools pins sorting (count DESC, name ASC) and cap at 10.
func TestInsights_TopTools(t *testing.T) {
	p, sessionDir, _ := newInsightsProducer(t, 30)

	// Build tool_counts: 12 distinct tools so we exceed the cap.
	// Tool "alpha" has the highest count (50). Tools "b01"–"b10" have count 2.
	// Tool "zz-low" has count 1 (must be outside top 10 because cap is 10).
	toolCounts := map[string]interface{}{
		"alpha":  50,
		"b01":    2,
		"b02":    2,
		"b03":    2,
		"b04":    2,
		"b05":    2,
		"b06":    2,
		"b07":    2,
		"b08":    2,
		"b09":    2,
		"b10":    2,
		"zz-low": 1,
	}

	writeJSON(t, sessionDir, "s1", map[string]interface{}{
		"session_id":  "sid-tools",
		"start_time":  recentStart(-1 * time.Hour),
		"tool_counts": toolCounts,
	})

	data := produceData(t, p)

	topToolsRaw, ok := data["top_tools"].([]map[string]interface{})
	require.True(t, ok, "top_tools must be []map[string]interface{}")
	assert.Len(t, topToolsRaw, 10, "top_tools must be capped at 10")

	// First entry must be "alpha" with count 50 (highest).
	require.NotEmpty(t, topToolsRaw)
	assert.Equal(t, "alpha", topToolsRaw[0]["name"], "first tool must be alpha (highest count)")
	assert.Equal(t, 50, toInt(topToolsRaw[0]["count"]))

	indexOf := func(name string) int {
		for i, e := range topToolsRaw {
			if e["name"] == name {
				return i
			}
		}
		return -1
	}

	// Tie-break by name ASC among the count=2 entries: "b01" precedes "b09".
	// Both survive the cap, so both indices are >= 0 and strictly ordered.
	require.GreaterOrEqual(t, indexOf("b01"), 0, "b01 must be present")
	require.GreaterOrEqual(t, indexOf("b09"), 0, "b09 must be present")
	assert.Less(t, indexOf("b01"), indexOf("b09"), "tie-break: b01 before b09 (name ASC)")

	// The cap keeps alpha + b01..b09 (name ASC). "b10" is the 11th entry and
	// "zz-low" the 12th (lower count) — both fall outside the top 10.
	assert.Equal(t, -1, indexOf("b10"), "b10 must be cut by the 10-cap (name ASC tie-break)")
	assert.Equal(t, -1, indexOf("zz-low"), "zz-low must be outside the top 10")
}

// TestInsights_FrictionFiltering pins that only facets for valid sessions are counted.
func TestInsights_FrictionFiltering(t *testing.T) {
	p, sessionDir, facetDir := newInsightsProducer(t, 30)

	// One valid session.
	writeJSON(t, sessionDir, "s1", map[string]interface{}{
		"session_id": "sid-valid",
		"start_time": recentStart(-1 * time.Hour),
	})
	// One stale session (its facet must be ignored).
	writeJSON(t, sessionDir, "s-stale", map[string]interface{}{
		"session_id": "sid-stale",
		"start_time": recentStart(-90 * 24 * time.Hour),
	})

	// Facet for the valid session: friction_counts with 2 categories.
	writeJSON(t, facetDir, "f1", map[string]interface{}{
		"session_id": "sid-valid",
		"friction_counts": map[string]interface{}{
			"permission_denied": 3,
			"tool_error":        2,
		},
		"outcome": "success",
	})

	// Facet for the stale session: must be ignored.
	writeJSON(t, facetDir, "f-stale", map[string]interface{}{
		"session_id": "sid-stale",
		"friction_counts": map[string]interface{}{
			"permission_denied": 999,
		},
	})

	// Facet for an unknown session_id: must also be ignored.
	writeJSON(t, facetDir, "f-unknown", map[string]interface{}{
		"session_id": "sid-unknown",
		"friction_counts": map[string]interface{}{
			"permission_denied": 500,
		},
	})

	data := produceData(t, p)
	assert.Equal(t, 5, toInt(data["friction_total"]),
		"friction_total must sum only valid-session facet counts (3+2=5)")
}

// TestInsights_RecentSession pins that recent_session reflects the most recent session.
func TestInsights_RecentSession(t *testing.T) {
	p, sessionDir, facetDir := newInsightsProducer(t, 30)

	// Older session.
	writeJSON(t, sessionDir, "s-old", map[string]interface{}{
		"session_id":       "sid-old",
		"start_time":       recentStart(-5 * time.Hour),
		"duration_minutes": 10.0,
		"lines_added":      50,
		"tool_errors":      1,
	})

	// Newer session — must win as "recent_session".
	writeJSON(t, sessionDir, "s-new", map[string]interface{}{
		"session_id":       "sid-new",
		"start_time":       recentStart(-1 * time.Hour),
		"duration_minutes": 25.0,
		"lines_added":      120,
		"tool_errors":      3,
	})

	// Facet for the newest session.
	writeJSON(t, facetDir, "f-new", map[string]interface{}{
		"session_id": "sid-new",
		"friction_counts": map[string]interface{}{
			"tool_error": 4,
		},
		"outcome": "completed",
	})

	data := produceData(t, p)
	rs, ok := data["recent_session"].(map[string]interface{})
	require.True(t, ok, "recent_session must be a map")

	assert.Equal(t, "sid-new", rs["id"], "must be the newest session")
	assert.Equal(t, 25.0, toFloat(rs["duration_minutes"]))
	assert.Equal(t, 120, toInt(rs["lines_added"]))
	assert.Equal(t, 3, toInt(rs["tool_errors"]))
	assert.Equal(t, "completed", rs["outcome"])
	assert.Equal(t, 4, toInt(rs["friction_count"]))
}

// TestInsights_RecentMetrics pins recent_messages / recent_days_active / recent_msgs_per_day
// (7-day window). A session 10 days old is in "total" but excluded from "recent".
func TestInsights_RecentMetrics(t *testing.T) {
	// Use stalenessDays=20 so the 10-day-old session is within the staleness window
	// but outside the 7-day "recent" window.
	p, sessionDir, _ := newInsightsProducer(t, 20)

	now := time.Now().UTC()

	// Session within 7-day window (2 days ago).
	writeJSON(t, sessionDir, "s-recent", map[string]interface{}{
		"session_id":        "sid-r1",
		"start_time":        now.Add(-2 * 24 * time.Hour).Format(time.RFC3339),
		"user_message_count": 10,
	})

	// Session 10 days ago: within staleness(20) but NOT within recent(7).
	writeJSON(t, sessionDir, "s-10d", map[string]interface{}{
		"session_id":        "sid-r2",
		"start_time":        now.Add(-10 * 24 * time.Hour).Format(time.RFC3339),
		"user_message_count": 50,
	})

	data := produceData(t, p)

	// Both sessions count toward total.
	assert.Equal(t, 2, toInt(data["total_sessions"]))
	assert.Equal(t, 60, toInt(data["total_messages"]), "total_messages = 10 + 50")

	// Only the 2-day-old session counts as "recent".
	assert.Equal(t, 10, toInt(data["recent_messages"]), "recent_messages = 10 only")
	assert.Equal(t, 1, toInt(data["recent_days_active"]), "1 day with recent activity")
	// recent_msgs_per_day: 10/1 = 10.
	assert.Equal(t, 10, toInt(data["recent_msgs_per_day"]))
}

// TestInsights_RecentMsgsPerDayRounding pins the rounding of recent_msgs_per_day.
func TestInsights_RecentMsgsPerDayRounding(t *testing.T) {
	p, sessionDir, _ := newInsightsProducer(t, 30)
	now := time.Now().UTC()

	// Two sessions on different days within 7 days: 7 and 8 messages → mean = 7.5 → rounds to 8.
	writeJSON(t, sessionDir, "s1", map[string]interface{}{
		"session_id":        "sid-1",
		"start_time":        now.Add(-1 * 24 * time.Hour).Format(time.RFC3339),
		"user_message_count": 7,
	})
	writeJSON(t, sessionDir, "s2", map[string]interface{}{
		"session_id":        "sid-2",
		"start_time":        now.Add(-2 * 24 * time.Hour).Format(time.RFC3339),
		"user_message_count": 8,
	})

	data := produceData(t, p)
	// 15 messages / 2 days = 7.5 → math.Round → 8.
	assert.Equal(t, 8, toInt(data["recent_msgs_per_day"]))
}

// TestInsights_ToInt pins toInt conversions for all supported types and zero-fallbacks.
func TestInsights_ToInt(t *testing.T) {
	cases := []struct {
		name  string
		input interface{}
		want  int
	}{
		{"float64", float64(42.9), 42},
		{"int", int(7), 7},
		{"int64", int64(100), 100},
		{"nil", nil, 0},
		{"string", "hello", 0},
		{"bool", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, toInt(tc.input))
		})
	}
}

// TestInsights_ToFloat pins toFloat conversions for all supported types and zero-fallbacks.
func TestInsights_ToFloat(t *testing.T) {
	cases := []struct {
		name  string
		input interface{}
		want  float64
	}{
		{"float64", float64(3.14), 3.14},
		{"int", int(5), 5.0},
		{"int64", int64(9), 9.0},
		{"nil", nil, 0.0},
		{"string", "x", 0.0},
		{"bool", false, 0.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, toFloat(tc.input))
		})
	}
}

// TestInsights_ReadJSONFiles pins readJSONFiles behavior: nonexistent dir, malformed JSON,
// non-.json files, and valid parsing.
func TestInsights_ReadJSONFiles(t *testing.T) {
	t.Run("nonexistent dir returns nil", func(t *testing.T) {
		result := readJSONFiles(filepath.Join(t.TempDir(), "ghost"))
		assert.Nil(t, result)
	})

	t.Run("skips malformed JSON", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not valid"), 0o644))
		result := readJSONFiles(dir)
		assert.Empty(t, result)
	})

	t.Run("skips non-.json files", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(`{"a":1}`), 0o644))
		result := readJSONFiles(dir)
		assert.Empty(t, result)
	})

	t.Run("parses valid JSON files", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.json"), []byte(`{"x":1}`), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "b.json"), []byte(`{"y":2}`), 0o644))
		result := readJSONFiles(dir)
		assert.Len(t, result, 2)
		keys := map[string]bool{}
		for _, m := range result {
			for k := range m {
				keys[k] = true
			}
		}
		assert.True(t, keys["x"] || keys["y"], "parsed objects must carry expected keys")
	})

	t.Run("mixed valid+invalid skips bad, keeps good", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ok.json"), []byte(`{"k":3}`), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`]nope`), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "skip.csv"), []byte(`k,3`), 0o644))
		result := readJSONFiles(dir)
		assert.Len(t, result, 1)
		assert.Equal(t, float64(3), result[0]["k"])
	})
}

// TestInsights_StalenessEchoedInNonZero pins staleness_days is echoed even with valid data.
func TestInsights_StalenessEchoedInNonZero(t *testing.T) {
	p, sessionDir, _ := newInsightsProducer(t, 14)
	writeJSON(t, sessionDir, "s1", map[string]interface{}{
		"session_id":        "sid-x",
		"start_time":        recentStart(-1 * time.Hour),
		"user_message_count": 3,
	})

	data := produceData(t, p)
	assert.Equal(t, 14, toInt(data["staleness_days"]))
}
