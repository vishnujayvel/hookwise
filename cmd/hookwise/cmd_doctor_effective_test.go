package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishnujayvel/hookwise/internal/core"
	"github.com/vishnujayvel/hookwise/internal/feeds"
)

// A project hookwise.yaml with a feeds: block is ignored by the singleton daemon
// (#89); doctor must WARN and list the keys so the user migrates them to global.
func TestCheckProjectFeedsIgnored_WarnsAndListsKeys(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, core.ProjectConfigFile), []byte(`
feeds:
  calendar:
    enabled: true
  weather:
    latitude: 1.0
`), 0o644))

	var buf bytes.Buffer
	count := checkProjectFeedsIgnored(&buf, cwd)
	out := buf.String()

	assert.Equal(t, 1, count)
	assert.Contains(t, out, "WARN  config:")
	assert.Contains(t, out, "are ignored")
	// Lists the feed keys (sorted), including a key that is only a sub-setting.
	assert.Contains(t, out, "calendar")
	assert.Contains(t, out, "weather")
}

// No project config, or a project config with no feeds: block, must not warn.
func TestCheckProjectFeedsIgnored_NoFeedsBlockSilent(t *testing.T) {
	cwd := t.TempDir()
	// (a) no project config at all.
	var buf bytes.Buffer
	assert.Equal(t, 0, checkProjectFeedsIgnored(&buf, cwd))

	// (b) project config without a feeds block.
	require.NoError(t, os.WriteFile(filepath.Join(cwd, core.ProjectConfigFile), []byte(`
guards:
  - match: "Read"
    action: warn
`), 0o644))
	buf.Reset()
	assert.Equal(t, 0, checkProjectFeedsIgnored(&buf, cwd))
	assert.Empty(t, buf.String())
}

// When the daemon's runtime feed config differs from the on-disk global config,
// doctor warns to restart the daemon (config is read once at startup).
func TestCheckFeedConfigDrift(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", globalDir)
	require.NoError(t, os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(`
version: 1
feeds:
  weather:
    enabled: true
    interval_seconds: 900
`), 0o644))

	gcfg, err := core.LoadGlobalConfig()
	require.NoError(t, err)
	onDisk := feedsFromConfig(&gcfg)

	// (a) Daemon polling the same config → no drift.
	var buf bytes.Buffer
	assert.Equal(t, 0, checkFeedConfigDrift(&buf, onDisk))
	assert.Empty(t, buf.String())

	// (b) Daemon polling a diverged config (weather disabled) → drift WARN.
	drifted := map[string]feeds.FeedStatus{}
	for k, v := range onDisk {
		drifted[k] = v
	}
	w := drifted["weather"]
	w.Enabled = false
	drifted["weather"] = w

	buf.Reset()
	assert.Equal(t, 1, checkFeedConfigDrift(&buf, drifted))
	assert.Contains(t, buf.String(), "WARN  feed-config:")
	assert.Contains(t, buf.String(), "restart the daemon")
	assert.Contains(t, buf.String(), "weather", "drift WARN should name the drifted feed")

	// (c) Daemon polls a custom feed that was removed from the on-disk config
	// (asymmetric membership) → still drift, even though no shared feed differs.
	membership := map[string]feeds.FeedStatus{}
	for k, v := range onDisk {
		membership[k] = v
	}
	membership["stocks"] = feeds.FeedStatus{Name: "stocks", Enabled: true, IntervalSeconds: 120}

	buf.Reset()
	assert.Equal(t, 1, checkFeedConfigDrift(&buf, membership))
	assert.Contains(t, buf.String(), "stocks", "drift WARN must catch a daemon-only custom feed")
}

// With the daemon down, resolveEffectiveFeeds falls back to the on-disk global
// config and reports source "global".
func TestResolveEffectiveFeeds_DaemonDownFallsBackToGlobal(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", globalDir)
	require.NoError(t, os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(`
version: 1
feeds:
  weather:
    enabled: true
    interval_seconds: 900
`), 0o644))

	// A client pointed at a socket that does not exist (daemon down).
	client := feeds.NewDaemonClient(filepath.Join(globalDir, "nonexistent.sock"))
	m, source := resolveEffectiveFeeds(client, false)

	assert.Equal(t, "global", source)
	require.Contains(t, m, "weather")
	assert.True(t, m["weather"].Enabled)
	assert.Equal(t, 900, m["weather"].IntervalSeconds)
}

// A producer-reported dead credential (FeedStatus.AuthDead, hw-b15m) must
// surface as a doctor WARN — without it, a permanently-broken feed fails open
// with cached data and looks healthy forever.
func TestCheckFeedAuthDead(t *testing.T) {
	var buf bytes.Buffer
	healthy := map[string]feeds.FeedStatus{
		"calendar": {Name: "calendar", Enabled: true},
		"weather":  {Name: "weather", Enabled: true},
	}
	assert.Equal(t, 0, checkFeedAuthDead(&buf, healthy))
	assert.Empty(t, buf.String())

	buf.Reset()
	dead := map[string]feeds.FeedStatus{
		"calendar": {Name: "calendar", Enabled: true, AuthDead: true},
		"weather":  {Name: "weather", Enabled: true},
	}
	assert.Equal(t, 1, checkFeedAuthDead(&buf, dead))
	assert.Contains(t, buf.String(), "WARN  feed:calendar: credential dead")
	assert.NotContains(t, buf.String(), "feed:weather")
}
