package feeds

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// TestDefaultDaemonLogPath_HonorsStateDirOverride verifies that
// defaultDaemonLogPath (used by spawnDaemon to redirect the spawned
// daemon's stdout/stderr) resolves under HOOKWISE_STATE_DIR when set.
// Pre-fix: spawnDaemon opened the frozen core.DefaultDaemonLogPath package
// var, computed at init from core.HomeDir() → red under the override.
func TestDefaultDaemonLogPath_HonorsStateDirOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmp)

	got := defaultDaemonLogPath()
	assert.Equal(t, filepath.Join(tmp, "daemon.log"), got)
}

// TestDefaultDaemonLogPath_DefaultUnchanged verifies the no-override default
// is byte-identical to the legacy default (the since-removed
// core.DefaultDaemonLogPath package var, ~/.hookwise/daemon.log).
func TestDefaultDaemonLogPath_DefaultUnchanged(t *testing.T) {
	t.Setenv("HOOKWISE_STATE_DIR", "")

	got := defaultDaemonLogPath()
	assert.Equal(t, filepath.Join(core.DefaultStateDir, "daemon.log"), got)
}

// TestDefaultDaemonSocketPath_HonorsStateDirOverride verifies that
// defaultDaemonSocketPath (used by spawnDaemon to decide whether --socket
// needs to be passed to the spawned daemon) resolves under
// HOOKWISE_STATE_DIR when set.
func TestDefaultDaemonSocketPath_HonorsStateDirOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmp)

	got := defaultDaemonSocketPath()
	assert.Equal(t, filepath.Join(tmp, "daemon.sock"), got)
}

// TestDefaultDaemonSocketPath_DefaultUnchanged verifies the no-override
// default is byte-identical to the legacy core.DefaultSocketPath value.
func TestDefaultDaemonSocketPath_DefaultUnchanged(t *testing.T) {
	t.Setenv("HOOKWISE_STATE_DIR", "")

	got := defaultDaemonSocketPath()
	assert.Equal(t, core.DefaultSocketPath, got)
}

// TestSpawnDaemon_EnsureDirHonorsStateDirOverride verifies that spawnDaemon's
// state-dir creation (needed for the daemon log and socket) happens under
// HOOKWISE_STATE_DIR. spawnDaemon always bails out early with
// errDaemonAutostartDisabled under `go test` (FORK-SAFETY guard, see
// isTestBinary), but the core.EnsureDir call runs BEFORE that guard, so the
// override directory is still created as a side effect and observable here.
func TestSpawnDaemon_EnsureDirHonorsStateDirOverride(t *testing.T) {
	tmp := t.TempDir()
	overrideDir := filepath.Join(tmp, "state-override")
	t.Setenv("HOOKWISE_STATE_DIR", overrideDir)

	client := NewDaemonClient(filepath.Join(overrideDir, "daemon.sock"))
	err := client.spawnDaemon("")
	assert.ErrorIs(t, err, errDaemonAutostartDisabled,
		"spawnDaemon must still refuse to re-exec from a *.test binary")

	info, statErr := os.Stat(overrideDir)
	assert.NoError(t, statErr, "state dir must be created under the HOOKWISE_STATE_DIR override")
	assert.True(t, info.IsDir())
}
