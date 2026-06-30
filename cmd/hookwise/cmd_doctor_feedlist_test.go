package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/vishnujayvel/hookwise/internal/core"
	"github.com/vishnujayvel/hookwise/internal/feeds"
)

// TestFeedsFromConfig_AgreesWithDaemonEffectiveFeeds is the #1 guarantee: doctor's
// per-feed verdict (enabled + interval) must match what the daemon actually
// polls. Doctor's fallback builder (feedsFromConfig) and the daemon's
// EffectiveFeeds both resolve through the SAME shared feeds.FeedEnabled /
// feeds.EffectiveIntervalSeconds helpers, so they cannot diverge by
// construction; this pins that invariant (and that knownBuiltinFeeds matches the
// daemon's registered builtins).
func TestFeedsFromConfig_AgreesWithDaemonEffectiveFeeds(t *testing.T) {
	cfg := core.GetDefaultConfig()
	cfg.Feeds.Weather.Enabled = true
	cfg.Feeds.Weather.IntervalSeconds = 900
	cfg.Feeds.Calendar.Enabled = true
	cfg.Feeds.Calendar.IntervalSeconds = 300
	cfg.Feeds.News.Enabled = false
	cfg.Feeds.Custom = []core.CustomFeedConfig{
		{Name: "pulse", Command: "echo {}", IntervalSeconds: 120, Enabled: true},
	}

	// The daemon's authoritative view of the same config.
	registry := feeds.NewRegistry()
	feeds.RegisterBuiltins(registry)
	registry.Register(feeds.NewCustomProducer("pulse", "echo {}", 0))
	d := feeds.NewDaemon(core.DaemonConfig{}, cfg.Feeds, registry)
	daemonView := map[string]feeds.FeedStatus{}
	for _, f := range d.EffectiveFeeds() {
		daemonView[f.Name] = f
	}

	// Doctor's fallback view of the same config.
	doctorView := feedsFromConfig(&cfg)

	for _, name := range append(append([]string{}, knownBuiltinFeeds...), "pulse") {
		assert.Equalf(t, daemonView[name].Enabled, doctorView[name].Enabled,
			"enabled mismatch for %q — doctor diverges from daemon", name)
		assert.Equalf(t, daemonView[name].IntervalSeconds, doctorView[name].IntervalSeconds,
			"interval mismatch for %q — doctor diverges from daemon", name)
	}
}

// TestFeedsFromConfig_BuiltinsAndCustoms pins that feedsFromConfig enumerates
// every builtin plus declared customs, and resolves enabled/interval (unset
// interval → the shared default, not 0).
func TestFeedsFromConfig_BuiltinsAndCustoms(t *testing.T) {
	cfg := core.GetDefaultConfig()
	cfg.Feeds.Weather.Enabled = true
	cfg.Feeds.Weather.IntervalSeconds = 900
	cfg.Feeds.Memories.IntervalSeconds = 0 // unset → should resolve to the default
	cfg.Feeds.Custom = []core.CustomFeedConfig{{Name: "pulse", Enabled: true, IntervalSeconds: 120}}

	got := feedsFromConfig(&cfg)

	for _, name := range knownBuiltinFeeds {
		_, ok := got[name]
		assert.Truef(t, ok, "builtin %q must be present", name)
	}
	assert.True(t, got["weather"].Enabled)
	assert.Equal(t, 900, got["weather"].IntervalSeconds)
	assert.True(t, got["pulse"].Enabled)
	assert.Equal(t, 120, got["pulse"].IntervalSeconds)
	// A feed with no explicit interval resolves to the shared default, not 0.
	assert.Equal(t, feeds.DefaultIntervalSeconds, got["memories"].IntervalSeconds)
}

// A nil config yields an empty map — doctor then treats every cache file as an
// orphan and skips it (fail-open, no panic).
func TestFeedsFromConfig_NilConfigEmpty(t *testing.T) {
	assert.Empty(t, feedsFromConfig(nil))
}
