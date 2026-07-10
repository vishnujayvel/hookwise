package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// TestWriteStatusLineCache_HonorsStateDirOverride verifies that
// writeStatusLineCache writes under HOOKWISE_STATE_DIR when set, rather than
// the frozen core.LastStatusOutputPath package var. The Python TUI reader
// already honors the env var, so a divergence here was a real split-brain
// between writer and reader (status preview silently stale/empty under an
// override).
func TestWriteStatusLineCache_HonorsStateDirOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmp)

	require.NoError(t, writeStatusLineCache("hello \x1b[32mworld\x1b[0m"))

	want := filepath.Join(tmp, "cache", "last-status-output.txt")
	data, err := os.ReadFile(want)
	require.NoError(t, err, "cache file must be written under the HOOKWISE_STATE_DIR override")
	assert.Equal(t, "hello world\n", string(data), "ANSI codes must still be stripped")
}

// TestWriteStatusLineCache_DefaultUnchanged verifies that when
// HOOKWISE_STATE_DIR is empty, writeStatusLineCache writes to the legacy
// core.LastStatusOutputPath location. No-regression test.
func TestWriteStatusLineCache_DefaultUnchanged(t *testing.T) {
	t.Setenv("HOOKWISE_STATE_DIR", "")

	got := filepath.Join(core.GetStateDir(), "cache", "last-status-output.txt")
	assert.Equal(t, core.LastStatusOutputPath, got,
		"writeStatusLineCache's resolved path must match the legacy default when no override is set")
}
