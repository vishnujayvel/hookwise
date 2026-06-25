package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/notifications"
)

// TestNotifications_HelpTextHonest guards against advertising a deleted producer.
// #207 removed the guard-effectiveness producer (and coaching is gone too); budget
// is the only producer that still runs (producers.go RunAll -> CheckBudget). The
// command's Long help must not promise "guard effectiveness" output that can never
// appear — the exact "advertise vaporware" honesty gap the relaunch audit flagged.
func TestNotifications_HelpTextHonest(t *testing.T) {
	long := strings.ToLower(newNotificationsCmd().Long)
	assert.NotContains(t, long, "guard effectiveness",
		"help must not advertise the deleted guard-effectiveness producer")
	assert.Contains(t, long, "budget",
		"help should describe the budget producer that actually exists")
}

// TestNotifications_ReadsConfigDBPath is the #109 regression test: when a project
// config sets a custom analytics.db_path, `notifications` (with no --data-dir flag)
// must read THAT database — the same one `dispatch` writes to — not the default
// ~/.hookwise/analytics.db. Before the fix, notifications ignored config and printed
// "No notifications." from the default empty DB.
func TestNotifications_ReadsConfigDBPath(t *testing.T) {
	tmpDir := t.TempDir()
	// Isolate the default state dir so the "default" DB is empty (and absent).
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))

	customDB := filepath.Join(tmpDir, "custom-notifications.db")

	// A project dir with a hookwise.yaml pointing analytics at the custom DB.
	projectDir := filepath.Join(tmpDir, "project")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	cfgYAML := "analytics:\n  enabled: true\n  db_path: " + customDB + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "hookwise.yaml"), []byte(cfgYAML), 0o644))

	// Seed one notification into the CUSTOM DB via the real NotificationService.
	db := openTestDB(t, customDB)
	ns := notifications.NewNotificationService(db)
	require.NoError(t, ns.Create(context.Background(), "budget", "budget_threshold", "Cost exceeded $99.00"))
	db.Close()

	// Run `notifications` from the project dir with NO --data-dir flag.
	t.Chdir(projectDir)
	cmd := newNotificationsCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "Cost exceeded $99.00",
		"notifications must read the config's db_path and show seeded notification, not 'No notifications.' from the default DB")
}
