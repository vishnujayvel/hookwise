package feeds

import (
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// DefaultIntervalSeconds is the poll cadence applied to an enabled feed that
// does not specify its own interval. It is the single source for this default,
// shared by the daemon's poll loop and doctor's feed-health report so the two
// can never disagree on cadence (#1).
const DefaultIntervalSeconds = 60

// FeedEnabled reports whether the named feed is enabled in cfg. Truly-unknown
// feeds are enabled by default (fail-open), matching the daemon's poll-gate.
// This is the single source of feed-enabled truth shared by the daemon and
// doctor, so doctor never reports a feed enabled/disabled differently from what
// the daemon actually polls (#1).
func FeedEnabled(cfg core.FeedsConfig, name string) bool {
	switch name {
	case "project":
		return cfg.Project.Enabled
	case "calendar":
		return cfg.Calendar.Enabled
	case "news":
		return cfg.News.Enabled
	case "weather":
		return cfg.Weather.Enabled
	case "memories":
		return cfg.Memories.Enabled
	case "insights":
		return cfg.Insights.Enabled
	default:
		for _, c := range cfg.Custom {
			if c.Name == name {
				return c.Enabled
			}
		}
		// Truly unknown feeds are enabled by default (fail-open).
		return true
	}
}

// EffectiveIntervalSeconds returns the poll interval (seconds) for the named
// feed, applying DefaultIntervalSeconds when the config leaves it unset. Shared
// by the daemon and doctor (see FeedEnabled).
func EffectiveIntervalSeconds(cfg core.FeedsConfig, name string) int {
	var seconds int
	switch name {
	case "project":
		seconds = cfg.Project.IntervalSeconds
	case "calendar":
		seconds = cfg.Calendar.IntervalSeconds
	case "news":
		seconds = cfg.News.IntervalSeconds
	case "weather":
		seconds = cfg.Weather.IntervalSeconds
	case "memories":
		seconds = cfg.Memories.IntervalSeconds
	case "insights":
		seconds = cfg.Insights.IntervalSeconds
	default:
		for _, c := range cfg.Custom {
			if c.Name == name {
				seconds = c.IntervalSeconds
				break
			}
		}
	}
	if seconds > 0 {
		return seconds
	}
	return DefaultIntervalSeconds
}

// FeedStatus is the effective per-feed configuration the daemon is actively
// polling with. It is exposed over GET /feeds and consumed by `hookwise doctor`
// (#1): because it is derived from the daemon's in-memory config + registry, it
// reflects what is REALLY running rather than a re-derivation from on-disk
// config that could have diverged.
type FeedStatus struct {
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	IntervalSeconds int    `json:"interval_seconds"`
	// AuthDead is true when the producer implements AuthReporter and its
	// credential is known-dead (e.g. revoked/expired OAuth refresh token).
	// Doctor surfaces this as a WARN so a silently-frozen feed is visible.
	AuthDead bool `json:"auth_dead,omitempty"`
}

// AuthReporter is an optional interface producers implement to report a dead
// credential (revoked/expired token) so the daemon can expose it over
// GET /feeds. Implementations must be safe for concurrent use — the socket
// handler calls AuthDead while the poll loop runs the producer.
type AuthReporter interface {
	AuthDead() bool
}

// EffectiveFeeds returns the effective per-feed config the daemon is polling
// with: every registered producer (builtins + customs) with its enabled flag
// and resolved interval. This is the authoritative answer to "what is the
// daemon actually doing", which doctor reports against (#1) instead of
// re-deriving a possibly-divergent view from on-disk config.
//
// d.feeds and the registry are immutable after NewDaemon/Start (the poll loop
// only reads them), so no lock is needed; a fresh slice is returned each call.
func (d *Daemon) EffectiveFeeds() []FeedStatus {
	producers := d.registry.All()
	out := make([]FeedStatus, 0, len(producers))
	for _, p := range producers {
		name := p.Name()
		fs := FeedStatus{
			Name:            name,
			Enabled:         d.isEnabled(name),
			IntervalSeconds: int(d.intervalFor(name) / time.Second),
		}
		// Runtime health beyond config: producers that track credential
		// death report it here (AuthDead locks internally).
		if ar, ok := p.(AuthReporter); ok {
			fs.AuthDead = ar.AuthDead()
		}
		out = append(out, fs)
	}
	return out
}
