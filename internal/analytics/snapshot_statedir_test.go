package analytics

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// TestDefaultSnapshotsDir_HonorsStateDirOverride verifies that DefaultSnapshotsDir
// returns a path rooted in HOOKWISE_STATE_DIR when that variable is set.
// Pre-fix: the function used core.HomeDir()/.hookwise, ignoring the env var → red.
// Post-fix: it uses core.GetStateDir() which returns the env var value → green.
func TestDefaultSnapshotsDir_HonorsStateDirOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmp)

	got := DefaultSnapshotsDir()
	assert.Equal(t, filepath.Join(tmp, "snapshots"), got)
}

// TestDefaultSnapshotsDir_DefaultUnchanged verifies that when HOOKWISE_STATE_DIR
// is empty, DefaultSnapshotsDir falls back to the standard ~/.hookwise/snapshots path.
// This is a no-regression test: behaviour is identical before and after the fix.
func TestDefaultSnapshotsDir_DefaultUnchanged(t *testing.T) {
	t.Setenv("HOOKWISE_STATE_DIR", "")

	got := DefaultSnapshotsDir()
	assert.Equal(t, filepath.Join(core.DefaultStateDir, "snapshots"), got)
}
