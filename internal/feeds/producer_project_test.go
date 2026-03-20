package feeds

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ProjectProducer envelope structure tests
// ---------------------------------------------------------------------------

func TestProjectProducer_EnvelopeStructure(t *testing.T) {
	p := &ProjectProducer{}
	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: Produce must not return error")

	data := assertValidEnvelope(t, result, "project")

	// Verify all expected data keys exist with correct types.
	_, ok := data["name"].(string)
	assert.True(t, ok, "data.name must be a string")
	_, ok = data["branch"].(string)
	assert.True(t, ok, "data.branch must be a string")
	_, ok = data["last_commit"].(string)
	assert.True(t, ok, "data.last_commit must be a string")
	_, ok = data["dirty"].(bool)
	assert.True(t, ok, "data.dirty must be a bool")
}

func TestProjectProducer_EnvelopeNoSourceKey(t *testing.T) {
	p := &ProjectProducer{}
	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := assertValidEnvelope(t, result, "project")

	// Bug #29 regression: no "source" in data either.
	_, hasSource := data["source"]
	assert.False(t, hasSource, "data must NOT contain 'source' key (Bug #29)")
}

// ---------------------------------------------------------------------------
// ProjectProducer error/fallback path tests
// ---------------------------------------------------------------------------

func TestProjectProducer_FallbackResult_ValidEnvelope(t *testing.T) {
	p := &ProjectProducer{}
	result, err := p.fallbackResult()
	require.NoError(t, err)

	data := assertValidEnvelope(t, result, "project")

	// Fallback should have empty strings and false dirty.
	assert.Equal(t, "", data["name"])
	assert.Equal(t, "", data["branch"])
	assert.Equal(t, "", data["last_commit"])
	assert.Equal(t, false, data["dirty"])
}

func TestProjectProducer_NonGitDir_FailOpen(t *testing.T) {
	// Run from a temp directory that is not a git repo.
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	p := &ProjectProducer{}
	result, err := p.Produce(context.Background())

	// ARCH-1: must not return error.
	require.NoError(t, err, "ARCH-1: non-git dir must not produce error")

	// Must still return a valid envelope.
	data := assertValidEnvelope(t, result, "project")

	// Fields should be empty strings (fallback values).
	assert.Equal(t, "", data["name"])
	assert.Equal(t, "", data["branch"])
	assert.Equal(t, "", data["last_commit"])
	assert.Equal(t, false, data["dirty"])
}

func TestProjectProducer_CancelledContext_FailOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	p := &ProjectProducer{}
	result, err := p.Produce(ctx)

	// ARCH-1: cancelled context should fail-open, returning fallback.
	require.NoError(t, err, "ARCH-1: cancelled context must not produce error")
	assertValidEnvelope(t, result, "project")
}
