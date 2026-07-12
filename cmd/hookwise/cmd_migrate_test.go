package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// runMigrateImportScenario is the shared body of the #109 regression test:
// when a project config sets a custom analytics.db_path, the migration command
// (with no --data-dir flag) must import the legacy data into THAT database --
// the same one `dispatch` writes to and `stats` reads from -- not the default
// ~/.hookwise/analytics.db.
//
// Before the fix, the command passed the empty --data-dir flag straight through
// to migration.Run, which fell back to analytics.DefaultDBPath(). A user with a
// custom db_path would migrate their data into a DB that dispatch/stats never
// touch, so `hookwise stats` kept showing $0 after a "successful" migration.
//
// It runs for both `migrate` and its deprecated `upgrade` alias so the alias is
// proven to execute the identical import path. Returns the combined command
// output for caller-specific assertions (e.g. the deprecation notice).
//
// Hermetic: HOME is pinned to a temp dir so both the legacy-source detection
// (~/.hookwise/state/cost-state.json) and the buggy default-target fallback
// (~/.hookwise/analytics.db) resolve inside the temp dir, never touching the
// real ~/.hookwise.
func runMigrateImportScenario(t *testing.T, cmd *cobra.Command) string {
	t.Helper()

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

	// Run the command against the project, with NO --data-dir flag.
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--project-dir", projectDir})
	require.NoError(t, cmd.Execute(), "command output:\n%s", buf.String())

	// The migrated DB must land at the config path, not the default.
	assert.FileExists(t, customDB,
		"migration must import into the config's analytics.db_path, not the default DB")
	assert.NoFileExists(t, analytics.DefaultDBPath(),
		"migration must NOT create/import into the default DB when a custom db_path is configured")

	return buf.String()
}

// TestRunMigrate_ImportsIntoConfigDBPath is the #109 regression test on the
// canonical `migrate` command name.
func TestRunMigrate_ImportsIntoConfigDBPath(t *testing.T) {
	output := runMigrateImportScenario(t, newMigrateCmd())
	assert.NotContains(t, output, "deprecated",
		"migrate is the canonical name and must not print a deprecation notice")
}

// TestRunUpgrade_ImportsIntoConfigDBPath proves the deprecated `upgrade` alias
// still executes the identical migration path (#109 regression coverage on the
// alias) and that cobra prints the one-line deprecation pointer to `migrate`.
func TestRunUpgrade_ImportsIntoConfigDBPath(t *testing.T) {
	output := runMigrateImportScenario(t, newUpgradeCmd())
	assert.Contains(t, output, `Command "upgrade" is deprecated, use "hookwise migrate" instead`,
		"upgrade alias must print the deprecation notice before running")
}

// TestUpgradeAlias_HiddenAndDeprecated pins the alias wiring: `upgrade` is
// hidden from help/completion, marked deprecated, and exposes the exact same
// flag surface as `migrate` (it IS the migrate command with a different name).
func TestUpgradeAlias_HiddenAndDeprecated(t *testing.T) {
	migrate := newMigrateCmd()
	upgrade := newUpgradeCmd()

	assert.True(t, upgrade.Hidden, "upgrade alias must be hidden from help")
	assert.NotEmpty(t, upgrade.Deprecated, "upgrade alias must be marked deprecated")
	assert.False(t, migrate.Hidden, "migrate must be visible in help")
	assert.Empty(t, migrate.Deprecated, "migrate must not be marked deprecated")

	for _, flag := range []string{"dry-run", "data-dir", "project-dir"} {
		assert.NotNil(t, upgrade.Flags().Lookup(flag), "upgrade alias must keep the --%s flag", flag)
		assert.NotNil(t, migrate.Flags().Lookup(flag), "migrate must have the --%s flag", flag)
	}

	// The root help must advertise migrate, not the deprecated alias (cobra
	// excludes deprecated commands from help and completion automatically).
	root := newRootCmd()
	helpBuf := &bytes.Buffer{}
	root.SetOut(helpBuf)
	root.SetErr(helpBuf)
	root.SetArgs([]string{"--help"})
	require.NoError(t, root.Execute())
	help := helpBuf.String()
	assert.Contains(t, help, "migrate", "root help must list the migrate command")
	assert.NotContains(t, help, "upgrade", "root help must not list the deprecated upgrade alias")
}
