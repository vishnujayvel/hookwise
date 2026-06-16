package feeds

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapInsightsSummary_PopulatedShape(t *testing.T) {
	s := InsightsSummary{
		TotalSessions:   3,
		TotalLinesAdded: 42,
		AvgDurationMin:  12.5,
		TopTools: []InsightsToolCount{
			{Name: "Read", Count: 25},
			{Name: "Edit", Count: 18},
		},
		PeakHourUTC:      14,
		DaysActive:       2,
		RecentDaysActive: 1,
		Recent: InsightsRecentSession{
			ID:          "sess-1",
			DurationMin: 9.0,
			LinesAdded:  7,
		},
	}

	data := MapInsightsSummary(s, 30)

	// Top-level keys and values.
	assert.Equal(t, 3, data["total_sessions"])
	assert.Equal(t, 0, data["total_messages"])
	assert.Equal(t, 42, data["total_lines_added"])
	assert.Equal(t, 12.5, data["avg_duration_minutes"])
	assert.Equal(t, 14, data["peak_hour"])
	assert.Equal(t, 2, data["days_active"])
	assert.Equal(t, 30, data["staleness_days"])
	assert.Equal(t, 0, data["recent_msgs_per_day"])
	assert.Equal(t, 0, data["recent_messages"])
	assert.Equal(t, 1, data["recent_days_active"])
	assert.Equal(t, 0, data["friction_total"])

	// friction_counts is an empty map (not nil).
	fc, ok := data["friction_counts"].(map[string]interface{})
	require.True(t, ok, "friction_counts must be map[string]interface{}")
	assert.Empty(t, fc)

	// top_tools: correct type, length, and values.
	tt, ok := data["top_tools"].([]map[string]interface{})
	require.True(t, ok, "top_tools must be []map[string]interface{}")
	require.Len(t, tt, 2)
	assert.Equal(t, "Read", tt[0]["name"])
	assert.Equal(t, 25, tt[0]["count"])
	assert.Equal(t, "Edit", tt[1]["name"])
	assert.Equal(t, 18, tt[1]["count"])

	// recent_session sub-map.
	rs, ok := data["recent_session"].(map[string]interface{})
	require.True(t, ok, "recent_session must be map[string]interface{}")
	assert.Equal(t, "sess-1", rs["id"])
	assert.Equal(t, 9.0, rs["duration_minutes"])
	assert.Equal(t, 7, rs["lines_added"])
	assert.Equal(t, 0, rs["friction_count"])
	assert.Equal(t, "", rs["outcome"])
	assert.Equal(t, 0, rs["tool_errors"])
}

func TestMapInsightsSummary_ZeroValueMatchesZeroedDefaults(t *testing.T) {
	data := MapInsightsSummary(InsightsSummary{}, 7)

	// All numeric top-level fields are zero.
	assert.Equal(t, 0, data["total_sessions"])
	assert.Equal(t, 0, data["total_messages"])
	assert.Equal(t, 0, data["total_lines_added"])
	assert.Equal(t, float64(0), data["avg_duration_minutes"])
	assert.Equal(t, 0, data["peak_hour"])
	assert.Equal(t, 0, data["days_active"])
	assert.Equal(t, 7, data["staleness_days"])
	assert.Equal(t, 0, data["recent_msgs_per_day"])
	assert.Equal(t, 0, data["recent_messages"])
	assert.Equal(t, 0, data["recent_days_active"])
	assert.Equal(t, 0, data["friction_total"])

	// top_tools is a non-nil empty slice (matches zeroedEnvelope []map[string]interface{}{}).
	tt, ok := data["top_tools"].([]map[string]interface{})
	require.True(t, ok, "top_tools must be []map[string]interface{}")
	assert.NotNil(t, tt)
	assert.Len(t, tt, 0)

	// friction_counts is an empty map.
	fc, ok := data["friction_counts"].(map[string]interface{})
	require.True(t, ok, "friction_counts must be map[string]interface{}")
	assert.Empty(t, fc)

	// Key set matches the 14 keys in zeroedEnvelope.
	expectedKeys := []string{
		"total_sessions", "total_messages", "total_lines_added", "avg_duration_minutes",
		"top_tools", "friction_counts", "friction_total", "peak_hour", "days_active",
		"staleness_days", "recent_msgs_per_day", "recent_messages", "recent_days_active",
		"recent_session",
	}
	assert.Len(t, data, len(expectedKeys), "key count must match zeroedEnvelope")
	for _, k := range expectedKeys {
		assert.Contains(t, data, k, "missing key: %s", k)
	}

	// recent_session sub-map with zero/empty values.
	rs, ok := data["recent_session"].(map[string]interface{})
	require.True(t, ok, "recent_session must be map[string]interface{}")
	assert.Equal(t, "", rs["id"])
	assert.Equal(t, float64(0), rs["duration_minutes"])
	assert.Equal(t, 0, rs["lines_added"])
	assert.Equal(t, 0, rs["friction_count"])
	assert.Equal(t, "", rs["outcome"])
	assert.Equal(t, 0, rs["tool_errors"])
}
