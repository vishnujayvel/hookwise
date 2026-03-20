package feeds

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertValidEnvelope verifies that a producer result has the canonical
// three-key envelope structure (type, timestamp, data) with no "source"
// key (Bug #29 regression guard).
func assertValidEnvelope(t *testing.T, result interface{}, expectedType string) map[string]interface{} {
	t.Helper()

	m, ok := result.(map[string]interface{})
	require.True(t, ok, "result must be map[string]interface{}")

	// Exactly 3 keys: type, timestamp, data.
	assert.Len(t, m, 3, "envelope must have exactly 3 keys")

	// Correct type value.
	assert.Equal(t, expectedType, m["type"], "envelope type must match producer name")

	// Timestamp exists and is valid RFC3339.
	ts, ok := m["timestamp"].(string)
	require.True(t, ok, "timestamp must be a string")
	_, err := time.Parse(time.RFC3339, ts)
	assert.NoError(t, err, "timestamp must be valid RFC3339")

	// Data key exists and is a map.
	data, ok := m["data"].(map[string]interface{})
	require.True(t, ok, "data must be map[string]interface{}")

	// Bug #29 regression guard: no "source" key at top level.
	_, hasSource := m["source"]
	assert.False(t, hasSource, "envelope must NOT contain top-level 'source' key (Bug #29 regression)")

	return data
}
