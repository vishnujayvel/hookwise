package feeds

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// ---------------------------------------------------------------------------
// InsightsProducer envelope structure tests
// ---------------------------------------------------------------------------

func TestInsightsProducer_EnvelopeStructure(t *testing.T) {
	p := &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: filepath.Join(t.TempDir(), "nonexistent"),
			StalenessDays: 30,
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: Produce must not return error")

	data := assertValidEnvelope(t, result, "insights")

	// Verify core data keys exist.
	_, ok := data["total_sessions"]
	assert.True(t, ok, "data must have 'total_sessions'")
	_, ok = data["total_messages"]
	assert.True(t, ok, "data must have 'total_messages'")
	_, ok = data["total_lines_added"]
	assert.True(t, ok, "data must have 'total_lines_added'")
	_, ok = data["avg_duration_minutes"]
	assert.True(t, ok, "data must have 'avg_duration_minutes'")
	_, ok = data["top_tools"]
	assert.True(t, ok, "data must have 'top_tools'")
	_, ok = data["friction_counts"]
	assert.True(t, ok, "data must have 'friction_counts'")
	_, ok = data["friction_total"]
	assert.True(t, ok, "data must have 'friction_total'")
	_, ok = data["peak_hour"]
	assert.True(t, ok, "data must have 'peak_hour'")
	_, ok = data["days_active"]
	assert.True(t, ok, "data must have 'days_active'")
	_, ok = data["staleness_days"]
	assert.True(t, ok, "data must have 'staleness_days'")
	_, ok = data["recent_session"]
	assert.True(t, ok, "data must have 'recent_session'")
	_, ok = data["recent_msgs_per_day"]
	assert.True(t, ok, "data must have 'recent_msgs_per_day'")
	_, ok = data["recent_messages"]
	assert.True(t, ok, "data must have 'recent_messages'")
	_, ok = data["recent_days_active"]
	assert.True(t, ok, "data must have 'recent_days_active'")
}

func TestInsightsProducer_EnvelopeNoSourceKey(t *testing.T) {
	p := &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: filepath.Join(t.TempDir(), "nonexistent"),
			StalenessDays: 30,
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := assertValidEnvelope(t, result, "insights")

	// Bug #29 regression: no "source" in data.
	_, hasSource := data["source"]
	assert.False(t, hasSource, "data must NOT contain 'source' key (Bug #29)")
}

// ---------------------------------------------------------------------------
// InsightsProducer error/fallback path tests
// ---------------------------------------------------------------------------

func TestInsightsProducer_ZeroedEnvelope_ValidStructure(t *testing.T) {
	p := &InsightsProducer{}
	result := p.zeroedEnvelope(30)

	data := assertValidEnvelope(t, result, "insights")

	// All numeric fields should be zero.
	assert.Equal(t, 0, toInt(data["total_sessions"]))
	assert.Equal(t, 0, toInt(data["total_messages"]))
	assert.Equal(t, 0, toInt(data["total_lines_added"]))
	assert.Equal(t, float64(0), data["avg_duration_minutes"])
	assert.Equal(t, 0, toInt(data["friction_total"]))
	assert.Equal(t, 0, toInt(data["peak_hour"]))
	assert.Equal(t, 0, toInt(data["days_active"]))
	assert.Equal(t, 30, toInt(data["staleness_days"]))
}

func TestInsightsProducer_ZeroedEnvelope_RecentSession(t *testing.T) {
	p := &InsightsProducer{}
	result := p.zeroedEnvelope(30)

	data := assertValidEnvelope(t, result, "insights")

	recentSession, ok := data["recent_session"].(map[string]interface{})
	require.True(t, ok, "recent_session must be a map")

	assert.Equal(t, "", recentSession["id"])
	assert.Equal(t, 0, toInt(recentSession["duration_minutes"]))
	assert.Equal(t, 0, toInt(recentSession["lines_added"]))
	assert.Equal(t, 0, toInt(recentSession["friction_count"]))
	assert.Equal(t, "", recentSession["outcome"])
	assert.Equal(t, 0, toInt(recentSession["tool_errors"]))
}

func TestInsightsProducer_MissingDir_FailOpen(t *testing.T) {
	p := &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: "/absolutely/does/not/exist",
			StalenessDays: 7,
		},
	})

	result, err := p.Produce(context.Background())

	// ARCH-1: missing directory must not produce error.
	require.NoError(t, err, "ARCH-1: missing data dir must fail-open")
	data := assertValidEnvelope(t, result, "insights")
	assert.Equal(t, 0, toInt(data["total_sessions"]))
}

func TestInsightsProducer_CancelledContext_FailOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	p := &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: filepath.Join(t.TempDir(), "data"),
			StalenessDays: 30,
		},
	})

	result, err := p.Produce(ctx)
	require.NoError(t, err, "ARCH-1: cancelled context must fail-open")
	assertValidEnvelope(t, result, "insights")
}

func TestInsightsProducer_DefaultStalenessDays(t *testing.T) {
	p := &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: filepath.Join(t.TempDir(), "nonexistent"),
			StalenessDays: 0, // zero → should default to 30
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := assertValidEnvelope(t, result, "insights")
	assert.Equal(t, 30, toInt(data["staleness_days"]), "default staleness should be 30 days")
}

func TestInsightsProducer_TestFixture_FieldConsistency(t *testing.T) {
	fixture := InsightsTestFixture()
	fixtureData := assertValidEnvelope(t, fixture, "insights")

	// Compare keys against a zeroed envelope (which has all the same fields).
	p := &InsightsProducer{}
	zeroed := p.zeroedEnvelope(30)
	zeroedData, ok := zeroed["data"].(map[string]interface{})
	require.True(t, ok, "zeroed envelope data should be a map")

	for key := range zeroedData {
		_, ok := fixtureData[key]
		assert.True(t, ok, "fixture data missing key %q", key)
	}
	for key := range fixtureData {
		_, ok := zeroedData[key]
		assert.True(t, ok, "fixture data has extra key %q", key)
	}
}
