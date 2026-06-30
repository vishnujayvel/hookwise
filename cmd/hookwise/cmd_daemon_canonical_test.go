package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// #89: the feed daemon is a singleton (one socket/cache/state dir), so the
// config it polls with MUST be canonical — sourced from the global config only,
// independent of which project directory cold-started it. resolveDaemonConfig is
// the seam the daemon-run command uses; this proves its effective feed config is
// identical no matter the cwd, even when conflicting project hookwise.yaml feeds
// blocks are present.
func TestResolveDaemonConfig_IdenticalAcrossColdStartCwds(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", globalDir)
	// Global config: weather ON, news OFF.
	globalConfig := `
version: 1
feeds:
  weather:
    enabled: true
  news:
    enabled: false
`
	require.NoError(t, os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalConfig), 0o644))

	// Two project dirs with CONFLICTING feed blocks — under the old per-project
	// model each would have driven a different daemon feed set.
	dirA := t.TempDir() // weather OFF, news ON
	require.NoError(t, os.WriteFile(filepath.Join(dirA, "hookwise.yaml"), []byte(`
feeds:
  weather:
    enabled: false
  news:
    enabled: true
`), 0o644))
	dirB := t.TempDir() // weather ON, calendar ON
	require.NoError(t, os.WriteFile(filepath.Join(dirB, "hookwise.yaml"), []byte(`
feeds:
  weather:
    enabled: true
  calendar:
    enabled: true
`), 0o644))

	// Precondition: LoadConfig(dir) DOES diverge by project (the overlay is real),
	// so the determinism below is meaningful, not vacuous.
	cfgA, err := core.LoadConfig(dirA)
	require.NoError(t, err)
	cfgB, err := core.LoadConfig(dirB)
	require.NoError(t, err)
	require.NotEqual(t, cfgA.Feeds.Weather.Enabled, cfgB.Feeds.Weather.Enabled,
		"precondition: project overlays must diverge for the determinism test to be meaningful")

	// resolveDaemonConfig must be IDENTICAL regardless of cwd.
	t.Chdir(dirA)
	daemonA := resolveDaemonConfig()
	t.Chdir(dirB)
	daemonB := resolveDaemonConfig()

	assert.Equal(t, daemonA.Feeds, daemonB.Feeds,
		"daemon feed config must be identical across cold-start cwds (#89)")
	// And it reflects the GLOBAL config (weather ON, news OFF), not either project.
	assert.True(t, daemonA.Feeds.Weather.Enabled, "daemon must honor global weather=ON, not project override")
	assert.False(t, daemonA.Feeds.News.Enabled, "daemon must honor global news=OFF, not project override")
}

// With no global config file, resolveDaemonConfig fails open to defaults
// (ARCH-1) rather than crashing or adopting a project config.
func TestResolveDaemonConfig_NoGlobalFileFailsOpenToDefaults(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", globalDir)

	cfg := resolveDaemonConfig()
	assert.Equal(t, core.GetDefaultConfig().Feeds, cfg.Feeds)
}
