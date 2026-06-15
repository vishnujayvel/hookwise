package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// TestResolveAnalyticsDBPath verifies the reader/writer path-resolution contract:
// an explicit --data-dir flag wins; otherwise the loaded config's
// Analytics.DBPath is used (which may be "" → analytics.Open falls back to the
// default). This is the fix for #105: stats must agree with dispatch.
func TestResolveAnalyticsDBPath(t *testing.T) {
	cfg := core.GetDefaultConfig()
	cfg.Analytics.DBPath = "/custom/from/config.db"

	// Explicit flag wins over config.
	assert.Equal(t, "/explicit/flag.db",
		resolveAnalyticsDBPath("/explicit/flag.db", cfg),
		"non-empty flag must take precedence over config")

	// Empty flag falls back to config.
	assert.Equal(t, "/custom/from/config.db",
		resolveAnalyticsDBPath("", cfg),
		"empty flag must fall back to config Analytics.DBPath")

	// Empty flag + empty config DBPath → empty (analytics.Open resolves default).
	cfg.Analytics.DBPath = ""
	assert.Equal(t, "",
		resolveAnalyticsDBPath("", cfg),
		"empty flag + empty config must return empty for Open() to default")
}

// TestRunStats_ReadsConfigDBPath is the #105 regression test: when a project
// config sets a custom analytics.db_path, `stats` (with no --data-dir flag)
// must read THAT database — the same one `dispatch` writes to — not the
// default ~/.hookwise/analytics.db. Before the fix, stats ignored config and
// printed $0.00.
func TestRunStats_ReadsConfigDBPath(t *testing.T) {
	tmpDir := t.TempDir()
	// Isolate the default state dir so the "default" DB is empty (and absent).
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))

	customDB := filepath.Join(tmpDir, "custom-analytics.db")

	// A project dir with a hookwise.yaml pointing analytics at the custom DB.
	projectDir := filepath.Join(tmpDir, "project")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	cfgYAML := "analytics:\n  enabled: true\n  db_path: " + customDB + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "hookwise.yaml"), []byte(cfgYAML), 0o644))

	// Seed $18.00 of cost into the CUSTOM DB via the real writer path.
	sid := "stats-config-dbpath-001"
	db := openTestDB(t, customDB)
	a := analytics.NewAnalytics(db)
	require.NoError(t, a.StartSession(context.Background(), sid, time.Now()))
	db.Close()

	transcriptPath := writeSonnetFixture(t)
	recordAnalytics(context.Background(), core.EventStop, core.HookPayload{
		SessionID:      sid,
		TranscriptPath: transcriptPath,
	}, customDB, core.CostTrackingConfig{Enabled: true})

	// Run `stats` from the project dir with NO --data-dir flag.
	t.Chdir(projectDir)
	cmd := newStatsCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "$18.00",
		"stats must read the config's db_path and show seeded cost, not $0.00 from the default DB")
}
