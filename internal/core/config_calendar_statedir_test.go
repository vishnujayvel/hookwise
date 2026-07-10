package core

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGetDefaultConfig_CalendarTokenPath_HonorsStateDirOverride verifies that
// GetDefaultConfig's Calendar.TokenPath default resolves under
// HOOKWISE_STATE_DIR when set at config-load time. Pre-fix: it used the
// frozen DefaultCalendarTokenPath package var, computed at init from
// HomeDir() → red under the override.
func TestGetDefaultConfig_CalendarTokenPath_HonorsStateDirOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmp)

	cfg := GetDefaultConfig()
	assert.Equal(t, filepath.Join(tmp, "calendar-token.json"), cfg.Feeds.Calendar.TokenPath)
}

// TestGetDefaultConfig_CalendarTokenPath_DefaultUnchanged verifies that when
// HOOKWISE_STATE_DIR is empty, the default is byte-identical to the legacy
// DefaultCalendarTokenPath value. No-regression test.
func TestGetDefaultConfig_CalendarTokenPath_DefaultUnchanged(t *testing.T) {
	t.Setenv("HOOKWISE_STATE_DIR", "")

	cfg := GetDefaultConfig()
	assert.Equal(t, DefaultCalendarTokenPath, cfg.Feeds.Calendar.TokenPath)
}
