package feeds

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// TestNewDaemon_HonorsStateDirOverride verifies that NewDaemon's default
// pidFile, cacheDir, and socketPath are resolved from core.GetStateDir() at
// call time, so HOOKWISE_STATE_DIR is honored. Pre-fix: these defaulted from
// the frozen core.DefaultPIDPath / core.DefaultCachePath / core.DefaultSocketPath
// package vars, computed at init from core.HomeDir() → red under the override.
func TestNewDaemon_HonorsStateDirOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmp)

	d := NewDaemon(core.DaemonConfig{}, core.FeedsConfig{}, NewRegistry())

	assert.Equal(t, filepath.Join(tmp, "daemon.pid"), d.pidFile)
	assert.Equal(t, filepath.Join(tmp, "state"), d.cacheDir)
	assert.Equal(t, filepath.Join(tmp, "daemon.sock"), d.socketPath)
}

// TestNewDaemon_DefaultUnchanged verifies that when HOOKWISE_STATE_DIR is
// empty, NewDaemon's defaults are byte-identical to the legacy
// core.DefaultPIDPath / DefaultCachePath-derived / DefaultSocketPath values.
// No-regression test: behaviour is identical before and after the fix.
func TestNewDaemon_DefaultUnchanged(t *testing.T) {
	t.Setenv("HOOKWISE_STATE_DIR", "")

	d := NewDaemon(core.DaemonConfig{}, core.FeedsConfig{}, NewRegistry())

	assert.Equal(t, core.DefaultPIDPath, d.pidFile)
	assert.Equal(t, filepath.Dir(core.DefaultCachePath), d.cacheDir)
	assert.Equal(t, core.DefaultSocketPath, d.socketPath)
}
