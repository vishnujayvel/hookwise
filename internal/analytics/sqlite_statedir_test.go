package analytics

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// TestDefaultDBPath_HonorsStateDirOverride verifies that DefaultDBPath
// returns a path rooted in HOOKWISE_STATE_DIR when that variable is set.
// Pre-fix: the function recomputed core.HomeDir()/.hookwise, ignoring the env
// var → red. Post-fix: it uses core.GetStateDir() → green. This is the real
// dispatch/stats/status-line analytics path, so a divergence here is the
// highest-priority instance of the bug class (see also DefaultSnapshotsDir,
// tuiPIDPath in PR #227).
func TestDefaultDBPath_HonorsStateDirOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmp)

	got := DefaultDBPath()
	assert.Equal(t, filepath.Join(tmp, "analytics.db"), got)
}

// TestDefaultDBPath_DefaultUnchanged verifies that when HOOKWISE_STATE_DIR is
// empty, DefaultDBPath falls back to the standard ~/.hookwise/analytics.db
// path. No-regression test: behaviour is identical before and after the fix.
func TestDefaultDBPath_DefaultUnchanged(t *testing.T) {
	t.Setenv("HOOKWISE_STATE_DIR", "")

	got := DefaultDBPath()
	assert.Equal(t, filepath.Join(core.DefaultStateDir, "analytics.db"), got)
}
