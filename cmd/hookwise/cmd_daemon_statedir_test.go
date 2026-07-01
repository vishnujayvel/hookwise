package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// #116 Divergence B: the TUI PID path resolved from the init-time-frozen
// core.DefaultStateDir var, so under HOOKWISE_STATE_DIR the PID landed in
// ~/.hookwise/tui.pid and the daemon's liveness check could not find it.
// tuiPIDPath must honor the override by reading core.GetStateDir() at call time.
func TestTUIPIDPath_HonorsStateDirOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmp)

	got := tuiPIDPath()

	assert.Equal(t, filepath.Join(tmp, "tui.pid"), got,
		"tuiPIDPath must resolve under HOOKWISE_STATE_DIR, not the frozen default")
}
