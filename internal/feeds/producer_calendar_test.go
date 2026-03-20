package feeds

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// ---------------------------------------------------------------------------
// CalendarProducer envelope structure tests
// ---------------------------------------------------------------------------

func TestCalendarProducer_EnvelopeStructure_Fallback(t *testing.T) {
	// Without a valid token, CalendarProducer returns its fallback envelope.
	p := &CalendarProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			TokenPath: "/nonexistent/token.json",
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: Produce must not return error")

	data := assertValidEnvelope(t, result, "calendar")

	// Fallback should have empty events and nil next_event.
	events, ok := data["events"].([]interface{})
	require.True(t, ok, "data.events must be []interface{}")
	assert.Empty(t, events)
	assert.Nil(t, data["next_event"])
}

func TestCalendarProducer_EnvelopeNoSourceKey(t *testing.T) {
	p := &CalendarProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			TokenPath: "/nonexistent/token.json",
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := assertValidEnvelope(t, result, "calendar")

	// Bug #29 regression: no "source" in data.
	_, hasSource := data["source"]
	assert.False(t, hasSource, "data must NOT contain 'source' key (Bug #29)")
}

// ---------------------------------------------------------------------------
// CalendarProducer error/fallback path tests
// ---------------------------------------------------------------------------

func TestCalendarProducer_FallbackResult_NoCachedData(t *testing.T) {
	p := &CalendarProducer{}
	result := p.fallbackResult("test reason")

	data := assertValidEnvelope(t, result, "calendar")
	events, ok := data["events"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, events)
	assert.Nil(t, data["next_event"])
}

func TestCalendarProducer_MissingToken_FailOpen(t *testing.T) {
	p := &CalendarProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			TokenPath: "/absolutely/does/not/exist/token.json",
		},
	})

	result, err := p.Produce(context.Background())

	// ARCH-1: missing token must not produce an error.
	require.NoError(t, err, "ARCH-1: missing token must fail-open")
	assertValidEnvelope(t, result, "calendar")
}

func TestCalendarProducer_DefaultTokenPath(t *testing.T) {
	// When TokenPath is empty, the producer should use the default path
	// and still fail-open if that path doesn't exist.
	p := &CalendarProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			TokenPath: "", // empty → uses DefaultCalendarTokenPath
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: default token path must fail-open if file missing")
	assertValidEnvelope(t, result, "calendar")
}

func TestCalendarProducer_TestFixture_FieldConsistency(t *testing.T) {
	// CalendarTestFixture must have the same keys as a real fallback envelope.
	fixture := CalendarTestFixture()
	fixtureData := assertValidEnvelope(t, fixture, "calendar")

	p := &CalendarProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			TokenPath: "/nonexistent/token.json",
		},
	})
	result, err := p.Produce(context.Background())
	require.NoError(t, err)
	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "result should be a map")
	realData, ok := resultMap["data"].(map[string]interface{})
	require.True(t, ok, "data should be a map")

	// All keys in the real envelope must appear in the fixture.
	for key := range realData {
		_, ok := fixtureData[key]
		assert.True(t, ok, "fixture data missing key %q", key)
	}
	for key := range fixtureData {
		_, ok := realData[key]
		assert.True(t, ok, "fixture data has extra key %q", key)
	}
}
