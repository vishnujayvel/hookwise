package feeds

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// ttlGraceSeconds keeps a cache entry "fresh" a bit past its poll interval so
// one slow/missed poll doesn't blank the segment; a truly stopped daemon still
// ages it out.
const ttlGraceSeconds = 120

// feedTTLFloorSeconds mirrors bridge.DefaultTTLSeconds (kept local to avoid a
// feeds→bridge import, which cycles with bridge_test → feeds). Fast feeds never
// get a TTL shorter than this floor.
const feedTTLFloorSeconds = 300

// feedCacheTTLSeconds computes the TTL to inject into a cache envelope for a
// given poll interval: interval + grace, floored at feedTTLFloorSeconds.
func feedCacheTTLSeconds(interval time.Duration) int {
	ttl := int(interval.Seconds()) + ttlGraceSeconds
	if ttl < feedTTLFloorSeconds {
		ttl = feedTTLFloorSeconds
	}
	return ttl
}

// ConfigAware is an optional interface that producers can implement to receive
// the feed configuration before producing data.
type ConfigAware interface {
	SetFeedsConfig(cfg core.FeedsConfig)
}

// defaultInterval is the fallback polling interval when a producer has no
// explicit configuration.
const defaultInterval = DefaultIntervalSeconds * time.Second

// defaultStaggerOffset is the delay between starting successive feed goroutines,
// preventing a thundering-herd of simultaneous fetches.
const defaultStaggerOffset = 2 * time.Second

// pollFeed runs a single feed producer in a loop until stopCh is closed.
// Each successful Produce() result is written to the JSON cache file
// (ARCH-3: JSON cache only, not the analytics DB).
func (d *Daemon) pollFeed(ctx context.Context, p Producer, interval time.Duration) {
	defer d.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run once immediately on start.
	d.runProducer(ctx, p, interval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.runProducer(ctx, p, interval)
		}
	}
}

// runProducer executes a single producer and writes the result to the JSON cache.
// Panics in Produce() are recovered so that a failing producer cannot crash
// the daemon goroutine (ARCH-1 fail-open guarantee).
//
// interval is the poll interval for this producer; it is used to compute and
// inject a ttl_seconds value into the envelope's data map so the status-line
// segment stays visible until the next poll (+ grace). Producers that already
// set ttl_seconds are left unchanged.
func (d *Daemon) runProducer(ctx context.Context, p Producer, interval time.Duration) {
	// IDLE-1: Reset idle timer on producer poll completion, including
	// error/panic paths — any activity counts.
	defer d.resetIdleTimer()
	defer func() {
		if r := recover(); r != nil {
			core.Logger().Error("feeds: producer panic recovered", "producer", p.Name(), "recovered", fmt.Sprintf("%v", r))
		}
	}()

	data, err := p.Produce(ctx)
	if err != nil {
		core.Logger().Error("feeds: producer error", "producer", p.Name(), "error", err)
		return
	}

	// Inject ttl_seconds into the envelope's data map so the bridge freshness
	// check keeps the segment visible until the next poll + grace period.
	// Producers that already set ttl_seconds are left unchanged (ok guards are
	// fail-safe for non-canonical shapes).
	if env, ok := data.(map[string]interface{}); ok {
		if dataMap, ok := env["data"].(map[string]interface{}); ok {
			if _, exists := dataMap["ttl_seconds"]; !exists {
				dataMap["ttl_seconds"] = feedCacheTTLSeconds(interval)
			}
		}
	}

	// ARCH-3: Write to JSON cache only, NOT the analytics DB.
	cachePath := filepath.Join(d.cacheDir, p.Name()+".json")
	if err := core.AtomicWriteJSON(cachePath, data); err != nil {
		core.Logger().Error("feeds: cache write error", "producer", p.Name(), "error", err)
	}

	// Regenerate the merged TUI cache so the Python TUI sees the fresh feed.
	// Wired by the cmd layer (bridge.WriteTUICacheTo) to avoid a feeds→bridge
	// import cycle. Panics are already covered by this function's recover().
	if d.postPollHook != nil {
		d.postPollHook(d.cacheDir)
	}
}

// runAllFeeds launches goroutines for all registered producers with
// staggered start times. Producers whose feed is disabled in the config
// are skipped entirely (BP1).
func (d *Daemon) runAllFeeds() {
	ctx, cancel := context.WithCancel(context.Background())

	// When stopCh closes, cancel the context for all producers.
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		<-d.stopCh
		cancel()
	}()

	producers := d.registry.All()
	started := 0
	for _, p := range producers {
		if !d.isEnabled(p.Name()) {
			core.Logger().Debug("feeds: skipping disabled producer", "producer", p.Name())
			continue
		}

		// Inject config into config-aware producers.
		if ca, ok := p.(ConfigAware); ok {
			ca.SetFeedsConfig(d.feeds)
		}

		interval := d.intervalFor(p.Name())
		d.wg.Add(1)

		// Stagger start: offset each feed goroutine.
		if started > 0 && d.staggerOffset > 0 {
			time.Sleep(d.staggerOffset)
		}
		started++

		go d.pollFeed(ctx, p, interval)
	}
}

// intervalFor returns the configured polling interval for the named feed,
// falling back to defaultInterval if not configured. Delegates to the shared
// EffectiveIntervalSeconds so the daemon and doctor resolve intervals
// identically (#1).
func (d *Daemon) intervalFor(name string) time.Duration {
	return time.Duration(EffectiveIntervalSeconds(d.feeds, name)) * time.Second
}

// isEnabled returns true if the named feed is enabled in the config. Delegates
// to the shared FeedEnabled so the daemon and doctor agree on enabled-state
// (#1); unrecognised feeds default to enabled (fail-open).
func (d *Daemon) isEnabled(name string) bool {
	return FeedEnabled(d.feeds, name)
}
