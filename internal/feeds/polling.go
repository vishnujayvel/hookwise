package feeds

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

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
func (d *Daemon) runProducer(ctx context.Context, p Producer) {
	data, err := p.Produce(ctx)
	if err != nil {
		log.Printf("feeds: producer %q error: %v", p.Name(), err)
		return
	}

	// ARCH-3: Write to JSON cache only, NOT Dolt.
	cachePath := filepath.Join(d.cacheDir, p.Name()+".json")
	if err := core.AtomicWriteJSON(cachePath, data); err != nil {
		log.Printf("feeds: cache write %q error: %v", p.Name(), err)
	}
}

// runAllFeeds launches goroutines for all registered producers with
// staggered start times.
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
	for i, p := range producers {
		interval := d.intervalFor(p.Name())
		d.wg.Add(1)

		// Stagger start: offset each feed goroutine.
		if i > 0 && d.staggerOffset > 0 {
			time.Sleep(d.staggerOffset)
		}

		go d.pollFeed(ctx, p, interval)
	}
}

// intervalFor returns the configured polling interval for the named feed,
// falling back to defaultInterval if not configured.
func (d *Daemon) intervalFor(name string) time.Duration {
	var seconds int

	switch name {
	case "pulse":
		seconds = d.feeds.Pulse.IntervalSeconds
	case "project":
		seconds = d.feeds.Project.IntervalSeconds
	case "calendar":
		seconds = d.feeds.Calendar.IntervalSeconds
	case "news":
		seconds = d.feeds.News.IntervalSeconds
	case "weather":
		seconds = d.feeds.Weather.IntervalSeconds
	case "practice":
		seconds = d.feeds.Practice.IntervalSeconds
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
