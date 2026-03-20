package feeds

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// NewsProducer envelope structure tests
// ---------------------------------------------------------------------------

func TestNewsProducer_EnvelopeStructure(t *testing.T) {
	p := &NewsProducer{}
	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: Produce must not return error")

	data := assertValidEnvelope(t, result, "news")

	// Verify expected data keys.
	stories, ok := data["stories"].([]interface{})
	require.True(t, ok, "data.stories must be []interface{}")
	assert.NotEmpty(t, stories, "placeholder should have at least one story")

	// Each story should have title, url, score.
	story, ok := stories[0].(map[string]interface{})
	require.True(t, ok, "each story must be a map")
	_, ok = story["title"].(string)
	assert.True(t, ok, "story.title must be a string")
	_, ok = story["url"].(string)
	assert.True(t, ok, "story.url must be a string")
	_, ok = story["score"]
	assert.True(t, ok, "story should have score")
}

func TestNewsProducer_EnvelopeNoTopLevelSourceKey(t *testing.T) {
	p := &NewsProducer{}
	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	// assertValidEnvelope checks no "source" at the envelope top level.
	// The news producer has "source" inside data (as a data field, not envelope key),
	// which is allowed — only top-level "source" is the Bug #29 violation.
	assertValidEnvelope(t, result, "news")
}

func TestNewsProducer_Name(t *testing.T) {
	p := &NewsProducer{}
	assert.Equal(t, "news", p.Name())
}

// ---------------------------------------------------------------------------
// NewsProducer produces valid result without configuration
// ---------------------------------------------------------------------------

func TestNewsProducer_NoConfigNeeded(t *testing.T) {
	// NewsProducer is a simple placeholder — it should always succeed
	// regardless of environment or configuration.
	p := &NewsProducer{}
	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "news", m["type"])
	assert.Len(t, m, 3, "envelope must have exactly 3 keys")
}
