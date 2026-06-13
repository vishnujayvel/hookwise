package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// fakeHNTransport routes HackerNews API requests to in-memory responses.
type fakeHNTransport struct {
	topStoriesIDs    []int
	items            map[int]map[string]interface{}
	failTopStories   bool
	topStoriesStatus int // 0 means 200
}

func (f *fakeHNTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if f.failTopStories && strings.Contains(req.URL.Path, "topstories") {
		return nil, fmt.Errorf("simulated transport error")
	}

	status := http.StatusOK
	var body string

	switch {
	case strings.HasSuffix(req.URL.Path, "topstories.json"):
		if f.topStoriesStatus != 0 {
			status = f.topStoriesStatus
		}
		data, _ := json.Marshal(f.topStoriesIDs)
		body = string(data)

	default:
		// Parse item ID from path like /v0/item/123.json
		var id int
		_, err := fmt.Sscanf(req.URL.Path, "/v0/item/%d.json", &id)
		if err != nil || f.items == nil {
			status = http.StatusNotFound
			body = `{}`
		} else if item, ok := f.items[id]; ok {
			data, _ := json.Marshal(item)
			body = string(data)
		} else {
			status = http.StatusNotFound
			body = `{}`
		}
	}

	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// newNewsProducerWithTransport builds a NewsProducer with an injected HTTP transport.
func newNewsProducerWithTransport(t *testing.T, transport http.RoundTripper, cfg core.FeedsConfig) *NewsProducer {
	t.Helper()
	p := &NewsProducer{
		client: &http.Client{Transport: transport},
	}
	p.SetFeedsConfig(cfg)
	return p
}

// ---------------------------------------------------------------------------
// Test A: Real stories returned, source=="hackernews", MaxStories respected
// ---------------------------------------------------------------------------

func TestNewsProducer_RealStories(t *testing.T) {
	transport := &fakeHNTransport{
		topStoriesIDs: []int{1, 2, 3, 4, 5, 6},
		items: map[int]map[string]interface{}{
			1: {"title": "Go 1.24 Released", "url": "https://go.dev/blog/go1.24", "score": 300},
			2: {"title": "SQLite is underrated", "url": "https://sqlite.org", "score": 250},
			3: {"title": "Claude Code ships agents", "url": "https://claude.ai", "score": 200},
		},
	}

	cfg := core.FeedsConfig{
		News: core.NewsFeedConfig{Source: "hackernews", MaxStories: 3},
	}
	p := newNewsProducerWithTransport(t, transport, cfg)

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	envelope, ok := result.(map[string]interface{})
	require.True(t, ok, "result must be a map")
	assert.Equal(t, "news", envelope["type"])

	data, ok := envelope["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "hackernews", data["source"])

	stories, ok := data["stories"].([]interface{})
	require.True(t, ok)
	// MaxStories=3, but only items 1/2/3 have entries; item IDs 4/5/6 are missing → skipped.
	assert.LessOrEqual(t, len(stories), 3, "should not exceed MaxStories")
	assert.GreaterOrEqual(t, len(stories), 1, "should return at least the known items")

	// Verify field names.
	first := stories[0].(map[string]interface{})
	assert.Contains(t, first, "title")
	assert.Contains(t, first, "url")
	assert.Contains(t, first, "score")
}

func TestNewsProducer_MaxStoriesLimit(t *testing.T) {
	// 10 IDs in top-stories, MaxStories=2 → only 2 items fetched.
	ids := make([]int, 10)
	items := make(map[int]map[string]interface{}, 10)
	for i := 1; i <= 10; i++ {
		ids[i-1] = i
		items[i] = map[string]interface{}{
			"title": fmt.Sprintf("Story %d", i),
			"url":   fmt.Sprintf("https://example.com/%d", i),
			"score": i * 10,
		}
	}

	transport := &fakeHNTransport{topStoriesIDs: ids, items: items}
	cfg := core.FeedsConfig{
		News: core.NewsFeedConfig{Source: "hackernews", MaxStories: 2},
	}
	p := newNewsProducerWithTransport(t, transport, cfg)

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	stories := data["stories"].([]interface{})
	assert.Len(t, stories, 2, "MaxStories=2 must cap the result at 2")
}

func TestNewsProducer_DefaultMaxStoriesWhenZero(t *testing.T) {
	ids := make([]int, 10)
	items := make(map[int]map[string]interface{}, 10)
	for i := 1; i <= 10; i++ {
		ids[i-1] = i
		items[i] = map[string]interface{}{
			"title": fmt.Sprintf("Story %d", i),
			"url":   fmt.Sprintf("https://example.com/%d", i),
			"score": i,
		}
	}

	transport := &fakeHNTransport{topStoriesIDs: ids, items: items}
	// MaxStories=0 → default (hnDefaultMax=5)
	cfg := core.FeedsConfig{
		News: core.NewsFeedConfig{Source: "hackernews", MaxStories: 0},
	}
	p := newNewsProducerWithTransport(t, transport, cfg)

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	stories := data["stories"].([]interface{})
	assert.LessOrEqual(t, len(stories), hnDefaultMax, "zero MaxStories should apply default cap")
}

// ---------------------------------------------------------------------------
// Test B: Transport error → fallback (empty stories, source=="unavailable", no error)
// ---------------------------------------------------------------------------

func TestNewsProducer_TransportError_ReturnsFallback(t *testing.T) {
	transport := &fakeHNTransport{failTopStories: true}
	cfg := core.FeedsConfig{
		News: core.NewsFeedConfig{Source: "hackernews", MaxStories: 5},
	}
	p := newNewsProducerWithTransport(t, transport, cfg)

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: must not return error on transport failure")

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	assert.Equal(t, "unavailable", data["source"])

	stories, ok := data["stories"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, stories, "fallback must return empty stories slice")
}

// ---------------------------------------------------------------------------
// Test C: Non-200 HTTP status → fallback
// ---------------------------------------------------------------------------

func TestNewsProducer_Non200Response_ReturnsFallback(t *testing.T) {
	transport := &fakeHNTransport{
		topStoriesIDs:    []int{1},
		topStoriesStatus: http.StatusInternalServerError,
	}
	cfg := core.FeedsConfig{
		News: core.NewsFeedConfig{Source: "hackernews", MaxStories: 5},
	}
	p := newNewsProducerWithTransport(t, transport, cfg)

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: must not return error on non-200 response")

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	assert.Equal(t, "unavailable", data["source"])

	stories := data["stories"].([]interface{})
	assert.Empty(t, stories)
}

// ---------------------------------------------------------------------------
// Test D: Source != "hackernews" → fallback without hitting the network
// ---------------------------------------------------------------------------

func TestNewsProducer_UnknownSource_ReturnsFallbackWithoutNetwork(t *testing.T) {
	// Transport panics if called — verifies the network is never hit.
	transport := &panicTransport{}
	cfg := core.FeedsConfig{
		News: core.NewsFeedConfig{Source: "rss", MaxStories: 5},
	}
	p := newNewsProducerWithTransport(t, transport, cfg)

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	assert.Equal(t, "unavailable", data["source"])

	stories := data["stories"].([]interface{})
	assert.Empty(t, stories)
}

// panicTransport panics if the network is accidentally called.
type panicTransport struct{}

func (p *panicTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	panic("network should not be called for unknown Source")
}

// ---------------------------------------------------------------------------
// Test F: Concurrent fetch preserves topstories order
// ---------------------------------------------------------------------------

// orderedFakeTransport serves items but deliberately introduces artificial
// ordering skew: it is safe for concurrent use (all fields are read-only after
// construction, no mutable state).
type orderedFakeTransport struct {
	topStoriesIDs []int
	items         map[int]map[string]interface{}
}

func (f *orderedFakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	status := http.StatusOK
	var body string

	switch {
	case strings.HasSuffix(req.URL.Path, "topstories.json"):
		data, _ := json.Marshal(f.topStoriesIDs)
		body = string(data)
	default:
		var id int
		_, err := fmt.Sscanf(req.URL.Path, "/v0/item/%d.json", &id)
		if err != nil {
			status = http.StatusNotFound
			body = `{}`
		} else if item, ok := f.items[id]; ok {
			data, _ := json.Marshal(item)
			body = string(data)
		} else {
			status = http.StatusNotFound
			body = `{}`
		}
	}

	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func TestNewsProducer_ConcurrentFetch_PreservesTopStoriesOrder(t *testing.T) {
	// IDs 10, 20, 30 — output must appear in exactly this order regardless of
	// goroutine scheduling.
	transport := &orderedFakeTransport{
		topStoriesIDs: []int{10, 20, 30},
		items: map[int]map[string]interface{}{
			10: {"title": "Story-Ten", "url": "https://example.com/10", "score": 100},
			20: {"title": "Story-Twenty", "url": "https://example.com/20", "score": 200},
			30: {"title": "Story-Thirty", "url": "https://example.com/30", "score": 300},
		},
	}

	cfg := core.FeedsConfig{
		News: core.NewsFeedConfig{Source: "hackernews", MaxStories: 3},
	}
	p := newNewsProducerWithTransport(t, transport, cfg)

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	assert.Equal(t, "hackernews", data["source"])

	stories, ok := data["stories"].([]interface{})
	require.True(t, ok)
	require.Len(t, stories, 3, "all three items must be returned")

	titles := []string{
		stories[0].(map[string]interface{})["title"].(string),
		stories[1].(map[string]interface{})["title"].(string),
		stories[2].(map[string]interface{})["title"].(string),
	}
	assert.Equal(t, []string{"Story-Ten", "Story-Twenty", "Story-Thirty"}, titles,
		"stories must appear in topstories ID order (10→20→30), not completion order")
}

// ---------------------------------------------------------------------------
// Test G: One item fetch fails → that story skipped, rest present, source=="hackernews"
// ---------------------------------------------------------------------------

// partialFailTransport makes item 20 return a non-200; others succeed.
// All fields are read-only — safe for concurrent RoundTrip calls.
type partialFailTransport struct {
	topStoriesIDs []int
	items         map[int]map[string]interface{}
	failID        int
}

func (f *partialFailTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "topstories.json") {
		data, _ := json.Marshal(f.topStoriesIDs)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(data))),
			Header:     make(http.Header),
		}, nil
	}

	var id int
	fmt.Sscanf(req.URL.Path, "/v0/item/%d.json", &id) //nolint:errcheck

	if id == f.failID {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	}

	if item, ok := f.items[id]; ok {
		data, _ := json.Marshal(item)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(data))),
			Header:     make(http.Header),
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Header:     make(http.Header),
	}, nil
}

func TestNewsProducer_OneItemFails_OthersPresent_SourceHackernews(t *testing.T) {
	// Item 20 will return 500; items 10 and 30 succeed.
	transport := &partialFailTransport{
		topStoriesIDs: []int{10, 20, 30},
		items: map[int]map[string]interface{}{
			10: {"title": "Story-Ten", "url": "https://example.com/10", "score": 10},
			30: {"title": "Story-Thirty", "url": "https://example.com/30", "score": 30},
		},
		failID: 20,
	}

	cfg := core.FeedsConfig{
		News: core.NewsFeedConfig{Source: "hackernews", MaxStories: 3},
	}
	p := newNewsProducerWithTransport(t, transport, cfg)

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: partial item failure must not return an error")

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	// Source must still be "hackernews" — the topstories call succeeded.
	assert.Equal(t, "hackernews", data["source"],
		"source must be 'hackernews' on partial item failure, not 'unavailable'")

	stories, ok := data["stories"].([]interface{})
	require.True(t, ok)
	require.Len(t, stories, 2, "failed item 20 must be skipped; items 10 and 30 must be present")

	titles := []string{
		stories[0].(map[string]interface{})["title"].(string),
		stories[1].(map[string]interface{})["title"].(string),
	}
	assert.Equal(t, []string{"Story-Ten", "Story-Thirty"}, titles,
		"surviving stories must preserve topstories order (10 before 30)")
}

// ---------------------------------------------------------------------------
// Test E: Fallback envelope never contains source:"placeholder"
// ---------------------------------------------------------------------------

func TestNewsProducer_FallbackNeverPlaceholder(t *testing.T) {
	transport := &fakeHNTransport{failTopStories: true}
	cfg := core.FeedsConfig{
		News: core.NewsFeedConfig{Source: "hackernews"},
	}
	p := newNewsProducerWithTransport(t, transport, cfg)

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	src, _ := data["source"].(string)
	assert.NotEqual(t, "placeholder", src, "source must never be 'placeholder'")
}
