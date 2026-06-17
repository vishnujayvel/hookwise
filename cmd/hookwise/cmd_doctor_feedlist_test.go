package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// TestKnownBuiltinFeeds_MirrorsGetFeedInterval enforces the invariant documented
// on knownBuiltinFeeds ("Mirrors the cases in getFeedInterval's switch
// statement"). The two lists are hand-maintained in separate places; if they
// drift, doctor misbehaves — a builtin recognised by isKnownFeed but absent from
// getFeedInterval's switch resolves to interval 0, and a name in the switch but
// missing from knownBuiltinFeeds makes isKnownFeed treat a real feed's orphan
// cache file as unknown (silently skipped). Nothing else guarded this; this pins
// it. Each builtin is given a DISTINCT interval so a missing switch case (which
// would fall through to the default 0) is caught per-feed.
func TestKnownBuiltinFeeds_MirrorsGetFeedInterval(t *testing.T) {
	cfg := &core.HooksConfig{}
	cfg.Feeds.Project.IntervalSeconds = 11
	cfg.Feeds.News.IntervalSeconds = 22
	cfg.Feeds.Calendar.IntervalSeconds = 33
	cfg.Feeds.Weather.IntervalSeconds = 44
	cfg.Feeds.Memories.IntervalSeconds = 55
	cfg.Feeds.Insights.IntervalSeconds = 66

	// The test's own expectation map must cover exactly the builtin list — if a
	// builtin is added to knownBuiltinFeeds without updating this map, the
	// completeness check below fails, forcing the author to also give it an
	// interval field (and thus a getFeedInterval case).
	wantInterval := map[string]int{
		"project":  11,
		"news":     22,
		"calendar": 33,
		"weather":  44,
		"memories": 55,
		"insights": 66,
	}

	require.Len(t, wantInterval, len(knownBuiltinFeeds),
		"wantInterval map must cover exactly knownBuiltinFeeds")

	for _, feed := range knownBuiltinFeeds {
		want, ok := wantInterval[feed]
		require.Truef(t, ok, "knownBuiltinFeeds entry %q has no expected interval — a builtin was added without a getFeedInterval mapping", feed)
		assert.Equalf(t, want, getFeedInterval(cfg, feed),
			"getFeedInterval(%q) must resolve via its own switch case, not fall through to the default 0", feed)
	}
}

// TestIsKnownFeed pins isKnownFeed across builtins, custom feeds, and unknowns —
// it had no coverage. Builtins are recognised; a declared custom feed is
// recognised; an orphan/unknown name is not.
func TestIsKnownFeed(t *testing.T) {
	cfg := &core.HooksConfig{}
	cfg.Feeds.Custom = []core.CustomFeedConfig{{Name: "mycustom"}}

	for _, feed := range knownBuiltinFeeds {
		assert.Truef(t, isKnownFeed(feed, cfg), "builtin %q must be known", feed)
	}
	assert.True(t, isKnownFeed("mycustom", cfg), "declared custom feed must be known")
	assert.False(t, isKnownFeed("nonexistent-orphan", cfg), "unknown orphan must not be known")

	// nil config: builtins still known, customs cannot be (no panic).
	assert.True(t, isKnownFeed("project", nil), "builtin must be known with nil config")
	assert.False(t, isKnownFeed("mycustom", nil), "custom cannot be resolved with nil config")
}
