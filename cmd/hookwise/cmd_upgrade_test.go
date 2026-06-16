package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// TestRunUpgrade_ImportsIntoConfigDBPath is the #109 regression test: when a
// project config sets a custom analytics.db_path, `hookwise upgrade` (with no
// --data-dir flag) must import the legacy data into THAT database -- the same
// one `dispatch` writes to and `stats` reads from -- not the default
// ~/.hookwise/analytics.db.
//
// Before the fix, upgrade passed the empty --data-dir flag straight through to
// migration.Run, which fell back to analytics.DefaultDBPath(). A user with a
// custom db_path would migrate their data into a DB that dispatch/stats never
// touch, so `hookwise stats` kept showing $0 after a "successful" upgrade.
//
// Hermetic: HOME is pinned to a temp dir so both the legacy-source detection
// (~/.hookwise/state/cost-state.json) and the buggy default-target fallback
// (~/.hookwise/analytics.db) resolve inside the temp dir, never touching the
// real ~/.hookwise.
func TestRunUpgrade_ImportsIntoConfigDBPath(t *testing.T) {
	tmpDir := t.TempDir()
	// HomeDir() resolves from $HOME; pinning it isolates DefaultDBPath() and the
	// legacy-source location so this test cannot read or write real state.
	t.Setenv("HOME", tmpDir)
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise-state"))

	// A legacy cost-state.json makes DetectTypeScript find a source so migration
	// actually opens the target DB (it short-circuits before opening when no
	// source is detected). No legacy analytics.db => the default target path is
	// not pre-created, keeping the assertion below a clean discriminator.
	legacyStateDir := filepath.Join(tmpDir, ".hookwise", "state")
	require.NoError(t, os.MkdirAll(legacyStateDir, 0o700))
	costJSON := `{"dailyCosts":{"2025-03-06":1.5},"sessionCosts":{"sess-x":1.5},"today":"2025-03-06","totalToday":1.5}`
	require.NoError(t, os.WriteFile(filepath.Join(legacyStateDir, "cost-state.json"), []byte(costJSON), 0o644))

	// Project config points analytics at a custom DB path (distinct directory).
	customDB := filepath.Join(tmpDir, "custom", "analytics.db")
	projectDir := filepath.Join(tmpDir, "project")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	cfgYAML := "version: 1\nanalytics:\n  enabled: true\n  db_path: " + customDB + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, core.ProjectConfigFile), []byte(cfgYAML), 0o644))

	// Run `upgrade` against the project, with NO --data-dir flag.
	cmd := newUpgradeCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--project-dir", projectDir})
	require.NoError(t, cmd.Execute(), "upgrade output:\n%s", buf.String())

	// The migrated DB must land at the config path, not the default.
	assert.FileExists(t, customDB,
		"upgrade must import into the config's analytics.db_path, not the default DB")
	assert.NoFileExists(t, analytics.DefaultDBPath(),
		"upgrade must NOT create/import into the default DB when a custom db_path is configured")
}
