package feeds

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// ConfigAware is an optional interface that producers can implement to receive
// the feed configuration before producing data.
type ConfigAware interface {
	SetFeedsConfig(cfg core.FeedsConfig)
}

// defaultInterval is the fallback polling interval when a producer has no
// explicit configuration.
const defaultInterval = 60 * time.Second

// defaultStaggerOffset is the delay between starting successive feed goroutines,
// preventing a thundering-herd of simultaneous fetches.
const defaultStaggerOffset = 2 * time.Second

// pollFeed runs a single feed producer in a loop until stopCh is closed.
// Each successful Produce() result is written to the JSON cache file
// (ARCH-3: JSON cache only, not Dolt).
func (d *Daemon) pollFeed(ctx context.Context, p Producer, interval time.Duration) {
	defer d.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run once immediately on start.
	d.runProducer(ctx, p)

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.runProducer(ctx, p)
		}
	}
}

// runProducer executes a single producer and writes the result to the JSON cache.
// Panics in Produce() are recovered so that a failing producer cannot crash
// the daemon goroutine (ARCH-1 fail-open guarantee).
func (d *Daemon) runProducer(ctx context.Context, p Producer) {
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

	// ARCH-3: Write to JSON cache only, NOT Dolt.
	cachePath := filepath.Join(d.cacheDir, p.Name()+".json")
	if err := core.AtomicWriteJSON(cachePath, data); err != nil {
		core.Logger().Error("feeds: cache write error", "producer", p.Name(), "error", err)
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
// falling back to defaultInterval if not configured.
func (d *Daemon) intervalFor(name string) time.Duration {
	var seconds int

	switch name {
	case "project":
		seconds = d.feeds.Project.IntervalSeconds
	case "calendar":
		seconds = d.feeds.Calendar.IntervalSeconds
	case "news":
		seconds = d.feeds.News.IntervalSeconds
	case "weather":
		seconds = d.feeds.Weather.IntervalSeconds
	case "memories":
		seconds = d.feeds.Memories.IntervalSeconds
	case "insights":
		seconds = d.feeds.Insights.IntervalSeconds
	}

	if seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return defaultInterval
}

// isEnabled returns true if the named feed is enabled in the config.
// Feeds that have no explicit Enabled field default to true (fail-open:
// unrecognised feeds are also considered enabled).
func (d *Daemon) isEnabled(name string) bool {
	switch name {
	case "project":
		return d.feeds.Project.Enabled
	case "calendar":
		return d.feeds.Calendar.Enabled
	case "news":
		return d.feeds.News.Enabled
	case "weather":
		return d.feeds.Weather.Enabled
	case "memories":
		return d.feeds.Memories.Enabled
	case "insights":
		return d.feeds.Insights.Enabled
	default:
		// Unknown feeds (custom, etc.) are enabled by default (fail-open).
		return true
	}
}
