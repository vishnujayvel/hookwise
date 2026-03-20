package feeds

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// MemoriesProducer envelope structure tests
// ---------------------------------------------------------------------------

func TestMemoriesProducer_EnvelopeStructure(t *testing.T) {
	p := &MemoriesProducer{}
	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: Produce must not return error")

	data := assertValidEnvelope(t, result, "memories")

	// Verify expected data keys.
	recentMemories, ok := data["recent_memories"].([]interface{})
	require.True(t, ok, "data.recent_memories must be []interface{}")
	assert.Empty(t, recentMemories, "placeholder should have empty recent_memories")

	totalCount := toInt(data["total_count"])
	assert.Equal(t, 0, totalCount, "placeholder total_count should be 0")
}

func TestMemoriesProducer_EnvelopeNoTopLevelSourceKey(t *testing.T) {
	p := &MemoriesProducer{}
	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	// assertValidEnvelope checks no "source" at the envelope top level.
	assertValidEnvelope(t, result, "memories")
}

func TestMemoriesProducer_Name(t *testing.T) {
	p := &MemoriesProducer{}
	assert.Equal(t, "memories", p.Name())
}

// ---------------------------------------------------------------------------
// MemoriesProducer always produces valid result
// ---------------------------------------------------------------------------

func TestMemoriesProducer_NoConfigNeeded(t *testing.T) {
	// MemoriesProducer is a simple placeholder — it should always succeed
	// regardless of environment or configuration.
	p := &MemoriesProducer{}
	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "memories", m["type"])
	assert.Len(t, m, 3, "envelope must have exactly 3 keys")
}

func TestMemoriesProducer_DataFieldsPresent(t *testing.T) {
	p := &MemoriesProducer{}
	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := assertValidEnvelope(t, result, "memories")

	// Verify all expected keys exist.
	_, hasRecentMemories := data["recent_memories"]
	assert.True(t, hasRecentMemories, "data must have 'recent_memories' key")

	_, hasTotalCount := data["total_count"]
	assert.True(t, hasTotalCount, "data must have 'total_count' key")

	_, hasSource := data["source"]
	// Note: MemoriesProducer currently includes "source" in data.
	// This is allowed — Bug #29 only guards against top-level "source".
	_ = hasSource // no assertion needed on data-level source
}
