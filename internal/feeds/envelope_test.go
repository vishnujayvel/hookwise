package feeds

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEnvelope_HasExactlyThreeKeys(t *testing.T) {
	env := NewEnvelope("test-feed", map[string]interface{}{"key": "value"})
	assert.Len(t, env, 3, "envelope must have exactly 3 keys")
}

func TestNewEnvelope_CorrectFieldNames(t *testing.T) {
	env := NewEnvelope("weather", map[string]interface{}{"temp": 72})

	_, hasType := env["type"]
	_, hasTimestamp := env["timestamp"]
	_, hasData := env["data"]

	assert.True(t, hasType, "envelope must contain 'type' key")
	assert.True(t, hasTimestamp, "envelope must contain 'timestamp' key")
	assert.True(t, hasData, "envelope must contain 'data' key")

	assert.Equal(t, "weather", env["type"])
	assert.Equal(t, map[string]interface{}{"temp": 72}, env["data"])
}

func TestNewEnvelope_TimestampFormat(t *testing.T) {
	env := NewEnvelope("test", map[string]interface{}{})

	ts, ok := env["timestamp"].(string)
	require.True(t, ok, "timestamp must be a string")

	_, err := time.Parse(time.RFC3339, ts)
	assert.NoError(t, err, "timestamp must be valid RFC3339")
}

func TestNewEnvelopeAt_UsesProvidedTimestamp(t *testing.T) {
	fixedTime := time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC)
	env := NewEnvelopeAt("calendar", map[string]interface{}{"events": []interface{}{}}, fixedTime)

	assert.Equal(t, "2026-03-07T10:00:00Z", env["timestamp"])
	assert.Equal(t, "calendar", env["type"])
}

func TestNewEnvelope_NoSourceKey(t *testing.T) {
	// Regression guard for Bug #29: the "source" key must never appear
	// in the envelope's top-level keys.
	env := NewEnvelope("weather", map[string]interface{}{"temperature": 72})

	_, hasSource := env["source"]
	assert.False(t, hasSource, "envelope must NOT contain a 'source' key (Bug #29 regression)")
}
