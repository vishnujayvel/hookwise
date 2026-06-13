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
