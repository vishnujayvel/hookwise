package feeds

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// EffectiveFeeds is the daemon's authoritative view of what it is actually
// polling: every registered producer (builtins + customs) with its enabled flag
// and effective interval resolved from the daemon's own config. doctor consumes
// this over GET /feeds so it reports against the daemon's real state (#1).
func TestDaemonEffectiveFeeds_ReflectsConfigAndRegistry(t *testing.T) {
	registry := NewRegistry()
	RegisterBuiltins(registry)

	feedsCfg := core.FeedsConfig{
		Weather:  core.WeatherFeedConfig{Enabled: true, IntervalSeconds: 900},
		Calendar: core.CalendarFeedConfig{Enabled: false},
		// project/news/memories/insights left zero-valued (disabled, no interval)
	}
	d := NewDaemon(core.DaemonConfig{}, feedsCfg, registry)

	byName := map[string]FeedStatus{}
	for _, f := range d.EffectiveFeeds() {
		byName[f.Name] = f
	}

	// Every registered builtin is reported.
	for _, n := range []string{"project", "calendar", "news", "weather", "memories", "insights"} {
		_, ok := byName[n]
		assert.True(t, ok, "builtin %q should be reported by EffectiveFeeds", n)
	}

	// Enabled + explicit interval round-trip.
	assert.True(t, byName["weather"].Enabled)
	assert.Equal(t, 900, byName["weather"].IntervalSeconds)

	// Disabled feed reported as disabled.
	assert.False(t, byName["calendar"].Enabled)

	// An enabled feed with no explicit interval reports the daemon's effective
	// default, not 0 — so doctor judges staleness against the real poll cadence.
	assert.Equal(t, int(defaultInterval.Seconds()), byName["project"].IntervalSeconds)
}

// Custom feeds (#124) must be reported too, or doctor re-derives a divergent set.
func TestDaemonEffectiveFeeds_IncludesCustomFeeds(t *testing.T) {
	registry := NewRegistry()
	RegisterBuiltins(registry)
	registry.Register(NewCustomProducer("stocks", "echo {}", 0))

	custom := []core.CustomFeedConfig{
		{Name: "stocks", Command: "echo {}", IntervalSeconds: 120, Enabled: true},
	}
	d := NewDaemon(core.DaemonConfig{}, core.FeedsConfig{Custom: custom}, registry)

	byName := map[string]FeedStatus{}
	for _, f := range d.EffectiveFeeds() {
		byName[f.Name] = f
	}
	got, ok := byName["stocks"]
	require.True(t, ok, "custom feed should be reported")
	assert.True(t, got.Enabled)
	assert.Equal(t, 120, got.IntervalSeconds)
}

// FeedEnabled / EffectiveIntervalSeconds are the single source of truth shared
// by the daemon's poll-gate and doctor's report (#1).
func TestFeedEnabledAndInterval_SharedSemantics(t *testing.T) {
	cfg := core.FeedsConfig{
		Weather:  core.WeatherFeedConfig{Enabled: true, IntervalSeconds: 900},
		Calendar: core.CalendarFeedConfig{Enabled: false},
		Custom:   []core.CustomFeedConfig{{Name: "stocks", Enabled: true, IntervalSeconds: 120}},
	}

	assert.True(t, FeedEnabled(cfg, "weather"))
	assert.False(t, FeedEnabled(cfg, "calendar"))
	assert.True(t, FeedEnabled(cfg, "stocks"))
	// Truly-unknown feeds fail open to enabled (matches the daemon poll-gate).
	assert.True(t, FeedEnabled(cfg, "does-not-exist"))

	assert.Equal(t, 900, EffectiveIntervalSeconds(cfg, "weather"))
	assert.Equal(t, 120, EffectiveIntervalSeconds(cfg, "stocks"))
	// Unset interval resolves to the shared default, not 0.
	assert.Equal(t, DefaultIntervalSeconds, EffectiveIntervalSeconds(cfg, "calendar"))
	assert.Equal(t, DefaultIntervalSeconds, EffectiveIntervalSeconds(cfg, "news"))
}

// The client's EffectiveFeeds() must round-trip the daemon's GET /feeds payload
// byte-faithfully — this is the wire contract doctor relies on (#1).
func TestDaemonClientEffectiveFeeds_RoundTripsOverSocket(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "d.sock")
	want := []FeedStatus{
		{Name: "weather", Enabled: true, IntervalSeconds: 900},
		{Name: "calendar", Enabled: false, IntervalSeconds: 300},
	}

	srv := NewSocketServer(socketPath, func() {}, time.Now())
	srv.SetFeedsProvider(func() []FeedStatus { return want })
	require.NoError(t, srv.Start())
	defer func() { _ = srv.Shutdown(context.Background()) }()

	client := NewDaemonClient(socketPath)
	got, err := client.EffectiveFeeds()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// With no provider wired, GET /feeds returns an empty list, not an error — so a
// daemon built without the provider never breaks doctor.
func TestSocketFeeds_NoProviderReturnsEmpty(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "d.sock")
	srv := NewSocketServer(socketPath, func() {}, time.Now())
	require.NoError(t, srv.Start())
	defer func() { _ = srv.Shutdown(context.Background()) }()

	got, err := NewDaemonClient(socketPath).EffectiveFeeds()
	require.NoError(t, err)
	assert.Empty(t, got)
}

// AuthDead must flow from an AuthReporter producer into EffectiveFeeds so
// doctor can surface a dead credential (hw-b15m). The calendar producer is
// the real implementation: mark it auth-dead and assert GET /feeds sees it.
func TestDaemonEffectiveFeeds_SurfacesAuthDead(t *testing.T) {
	registry := NewRegistry()
	RegisterBuiltins(registry)

	d := NewDaemon(core.DaemonConfig{}, core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{Enabled: true},
	}, registry)

	byName := map[string]FeedStatus{}
	for _, f := range d.EffectiveFeeds() {
		byName[f.Name] = f
	}
	require.Contains(t, byName, "calendar")
	assert.False(t, byName["calendar"].AuthDead, "healthy producer must not report auth-dead")

	// Flip the real producer to auth-dead and re-read.
	for _, p := range registry.All() {
		if cp, ok := p.(*CalendarProducer); ok {
			cp.markAuthDead()
		}
	}
	byName = map[string]FeedStatus{}
	for _, f := range d.EffectiveFeeds() {
		byName[f.Name] = f
	}
	assert.True(t, byName["calendar"].AuthDead, "auth-dead producer must be reported over EffectiveFeeds")
}
