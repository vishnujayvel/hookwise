package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

const (
	hnTopStoriesURL = "https://hacker-news.firebaseio.com/v0/topstories.json"
	hnItemURLFmt    = "https://hacker-news.firebaseio.com/v0/item/%d.json"
	hnDefaultMax    = 5
)

// NewsProducer fetches live stories from the HackerNews Firebase API.
// It implements ConfigAware to receive MaxStories and Source from feed config.
type NewsProducer struct {
	mu       sync.Mutex
	feedsCfg core.FeedsConfig
	client   *http.Client
}

func (p *NewsProducer) Name() string { return "news" }

// SetFeedsConfig receives the feed configuration (ConfigAware interface).
func (p *NewsProducer) SetFeedsConfig(cfg core.FeedsConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.feedsCfg = cfg
}

// hnItem is the subset of fields we read from each HackerNews item.
type hnItem struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Score int    `json:"score"`
}

func (p *NewsProducer) Produce(ctx context.Context) (interface{}, error) {
	p.mu.Lock()
	cfg := p.feedsCfg.News
	p.mu.Unlock()

	// Only "hackernews" is implemented. Any other non-empty Source → fallback.
	if cfg.Source != "" && cfg.Source != "hackernews" {
		return p.fallback(), nil
	}

	maxStories := cfg.MaxStories
	if maxStories <= 0 {
		maxStories = hnDefaultMax
	}

	if p.client == nil {
		p.client = &http.Client{Timeout: 5 * time.Second}
	}

	// Fetch top story IDs.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hnTopStoriesURL, nil)
	if err != nil {
		core.Logger().Warn("news: failed to build top-stories request", "error", err)
		return p.fallback(), nil
	}

	resp, err := p.client.Do(req)
	if err != nil {
		core.Logger().Warn("news: top-stories request failed", "error", err)
		return p.fallback(), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		core.Logger().Warn("news: top-stories non-200", "status", resp.StatusCode)
		return p.fallback(), nil
	}

	var ids []int
	if err := json.NewDecoder(resp.Body).Decode(&ids); err != nil {
		core.Logger().Warn("news: failed to decode top-stories IDs", "error", err)
		return p.fallback(), nil
	}

	if len(ids) > maxStories {
		ids = ids[:maxStories]
	}

	// Fetch all items concurrently, preserving topstories order.
	// results[i] holds the fetched item for ids[i]; ok[i] == false means skip.
	type result struct {
		item hnItem
		ok   bool
	}
	results := make([]result, len(ids))

	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(idx, storyID int) {
			defer wg.Done()
			item, ok := p.fetchItem(ctx, storyID)
			results[idx] = result{item: item, ok: ok}
		}(i, id)
	}
	wg.Wait()

	stories := make([]interface{}, 0, len(ids))
	for _, r := range results {
		if !r.ok {
			continue
		}
		stories = append(stories, map[string]interface{}{
			"title": r.item.Title,
			"url":   r.item.URL,
			"score": r.item.Score,
		})
	}

	return NewEnvelope("news", map[string]interface{}{
		"stories": stories,
		"source":  "hackernews",
	}), nil
}

// fetchItem fetches a single HackerNews item by ID.
// Returns the item and true on success; false on any error (fail-soft).
func (p *NewsProducer) fetchItem(ctx context.Context, id int) (hnItem, bool) {
	url := fmt.Sprintf(hnItemURLFmt, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		core.Logger().Warn("news: failed to build item request", "id", id, "error", err)
		return hnItem{}, false
	}

	resp, err := p.client.Do(req)
	if err != nil {
		core.Logger().Warn("news: item request failed", "id", id, "error", err)
		return hnItem{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		core.Logger().Warn("news: item non-200", "id", id, "status", resp.StatusCode)
		return hnItem{}, false
	}

	var item hnItem
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		core.Logger().Warn("news: failed to decode item", "id", id, "error", err)
		return hnItem{}, false
	}

	return item, true
}

// fallback returns an empty-stories envelope. Source is "unavailable", never "placeholder".
func (p *NewsProducer) fallback() map[string]interface{} {
	return NewEnvelope("news", map[string]interface{}{
		"stories": []interface{}{},
		"source":  "unavailable",
	})
}
