package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// seedWarnings writes a warnings.json under <stateDir>/state/ with the given
// (source) entries, each stamped fresh (now) so ReadWarnings' TTL keeps them
// active. Returns the stateDir to pass to renderWarningSegment.
func seedWarnings(t *testing.T, sources ...string) string {
	t.Helper()
	stateDir := t.TempDir()
	dir := filepath.Join(stateDir, "state")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	now := time.Now().UTC().Format(time.RFC3339)
	warnings := make([]core.Warning, 0, len(sources))
	for _, s := range sources {
		warnings = append(warnings, core.Warning{Source: s, Message: "m", Timestamp: now})
	}
	data, err := json.Marshal(warnings)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "warnings.json"), data, 0o600))
	return stateDir
}

// TestRenderWarningSegment pins the rendered status-line warning segment, which
// previously had no output-test coverage (issue #181). It asserts:
//   - no warnings  → empty segment;
//   - one warning  → singular noun "warning" + the source in a labelled form;
//   - N warnings   → count + plural "warnings" + the MOST RECENT source (last).
//
// The ANSI colour codes wrap the payload as a prefix/suffix, so the human-
// readable core string appears verbatim and Contains assertions are exact.
func TestRenderWarningSegment(t *testing.T) {
	t.Run("no warnings renders empty", func(t *testing.T) {
		stateDir := seedWarnings(t) // no entries
		assert.Equal(t, "", renderWarningSegment(stateDir))
	})

	t.Run("missing file renders empty", func(t *testing.T) {
		assert.Equal(t, "", renderWarningSegment(t.TempDir()))
	})

	t.Run("single warning uses singular noun", func(t *testing.T) {
		stateDir := seedWarnings(t, "cost")
		out := renderWarningSegment(stateDir)
		assert.Contains(t, out, "⚠ 1 warning (cost)",
			"single warning must read '⚠ 1 warning (cost)' (singular, labelled)")
		assert.NotContains(t, out, "1 warnings",
			"singular count must not pluralise the noun")
	})

	t.Run("multiple warnings pluralise and show most-recent source", func(t *testing.T) {
		// 'memories' is appended last, so it is the most-recent source.
		stateDir := seedWarnings(t, "cost", "weather", "memories")
		out := renderWarningSegment(stateDir)
		assert.Contains(t, out, "⚠ 3 warnings (memories)",
			"multiple warnings must read '⚠ 3 warnings (<most-recent source>)'")
		assert.NotContains(t, out, "(cost)",
			"the segment must surface the most-recent source, not the oldest")
	})
}
