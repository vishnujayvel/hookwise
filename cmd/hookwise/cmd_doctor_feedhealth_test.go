package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// writeFeedFile writes a minimal feed cache envelope to cacheDir/<name>.json.
// source=="placeholder" produces a placeholder envelope; anything else omits the
// source field so the feed looks like real (non-placeholder) data.
func writeFeedFile(t *testing.T, cacheDir, name, source string) {
	t.Helper()
	data := map[string]interface{}{"key": "value"}
	if source == "placeholder" {
		data = map[string]interface{}{"source": "placeholder"}
	}
	envelope := map[string]interface{}{
		"type":      name,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      data,
	}
	raw, err := json.Marshal(envelope)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(cacheDir, name+".json"), raw, 0o644)
	require.NoError(t, err)
}

// writeFeedFileAged writes a feed cache envelope whose timestamp is ageSeconds in
// the past, so staleness checks (age > 2*interval) can be exercised. The data is
// real (non-placeholder, non-empty).
func writeFeedFileAged(t *testing.T, cacheDir, name string, ageSeconds int) {
	t.Helper()
	envelope := map[string]interface{}{
		"type":      name,
		"timestamp": time.Now().UTC().Add(-time.Duration(ageSeconds) * time.Second).Format(time.RFC3339),
		"data":      map[string]interface{}{"key": "value"},
	}
	raw, err := json.Marshal(envelope)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, name+".json"), raw, 0o644))
}

// TestCheckFeedHealth_OrphanSkipped verifies that orphan cache files (files with
// no corresponding built-in or custom feed) are silently skipped and do not
// produce any output or increment the warning count.
func TestCheckFeedHealth_OrphanSkipped(t *testing.T) {
	cacheDir := t.TempDir()

	// (a) Orphan: practice.json — no Go producer, not in cfg.Feeds.Custom.
	writeFeedFile(t, cacheDir, "practice", "placeholder")
	// (b) Known built-in: news.json — placeholder should still warn (when enabled).
	writeFeedFile(t, cacheDir, "news", "placeholder")

	cfg := core.GetDefaultConfig()
	// News is disabled by default; enable it so this test exercises the
	// known-vs-orphan distinction, not the disabled-feed suppression (which has
	// its own coverage below).
	cfg.Feeds.News.Enabled = true
	// Ensure no custom feed named "practice".
	cfg.Feeds.Custom = nil

	var buf bytes.Buffer
	count := checkFeedHealth(&buf, cacheDir, &cfg)
	out := buf.String()

	// Known feed must still warn.
	assert.Contains(t, out, "feed:news", "known feed (news) must appear in output")
	assert.Contains(t, out, "placeholder", "known feed must warn about placeholder data")

	// Orphan must be silently skipped.
	assert.NotContains(t, out, "practice", "orphan feed (practice) must not appear in output")

	// Warning count must reflect only the known feed.
	assert.Equal(t, 1, count, "warning count must be 1 (only the known news feed)")
}

// TestCheckFeedHealth_DisabledFeedStaleNotWarned is the regression test for the
// live `feed:news`/`feed:memories stale data` warnings: a feed that is DISABLED
// in config but left a stale cache file behind must NOT be reported as "stale"
// (a disabled feed is not expected to be polled). It is reported as a benign
// INFO "disabled" line and does not increment the warning count.
func TestCheckFeedHealth_DisabledFeedStaleNotWarned(t *testing.T) {
	cacheDir := t.TempDir()
	// news default interval is 1800s → stale threshold 3600s. 8h old = stale.
	writeFeedFileAged(t, cacheDir, "news", 8*3600)

	cfg := core.GetDefaultConfig()
	cfg.Feeds.News.Enabled = false // disabled

	var buf bytes.Buffer
	count := checkFeedHealth(&buf, cacheDir, &cfg)
	out := buf.String()

	assert.NotContains(t, out, "WARN  feed:news", "disabled feed must not produce a WARN")
	assert.NotContains(t, out, "stale data", "disabled feed must not be reported as stale")
	assert.Contains(t, out, "feed:news: disabled", "disabled feed should be reported as a benign INFO disabled line")
	assert.Equal(t, 0, count, "disabled stale feed must contribute zero warnings")
}

// TestCheckFeedHealth_EnabledFeedStaleStillWarns verifies the converse: an
// ENABLED feed with a stale cache (the `feed:calendar` case) is still correctly
// flagged WARN. The enabled-gate must not suppress genuine staleness.
func TestCheckFeedHealth_EnabledFeedStaleStillWarns(t *testing.T) {
	cacheDir := t.TempDir()
	// calendar default interval 300s → threshold 600s. 8h old = stale.
	writeFeedFileAged(t, cacheDir, "calendar", 8*3600)

	cfg := core.GetDefaultConfig()
	cfg.Feeds.Calendar.Enabled = true // enabled

	var buf bytes.Buffer
	count := checkFeedHealth(&buf, cacheDir, &cfg)
	out := buf.String()

	assert.Contains(t, out, "WARN  feed:calendar: stale data", "enabled stale feed must still warn")
	assert.Equal(t, 1, count, "enabled stale feed must contribute one warning")
}

// TestCheckFeedHealth_DisabledFeedPlaceholderNotWarned verifies a disabled feed
// whose cache is placeholder data is also suppressed to a benign INFO line
// (placeholder is expected when a feed never ran because it is off).
func TestCheckFeedHealth_DisabledFeedPlaceholderNotWarned(t *testing.T) {
	cacheDir := t.TempDir()
	writeFeedFile(t, cacheDir, "memories", "placeholder")

	cfg := core.GetDefaultConfig()
	cfg.Feeds.Memories.Enabled = false

	var buf bytes.Buffer
	count := checkFeedHealth(&buf, cacheDir, &cfg)
	out := buf.String()

	assert.NotContains(t, out, "WARN  feed:memories", "disabled placeholder feed must not warn")
	assert.Contains(t, out, "feed:memories: disabled", "disabled placeholder feed reported as INFO disabled")
	assert.Equal(t, 0, count, "disabled placeholder feed contributes zero warnings")
}

// TestCheckFeedHealth_CustomFeedTreatedAsKnown verifies that a feed listed in
// cfg.Feeds.Custom is treated as known and its placeholder file triggers a
// warning (i.e. it is NOT silently skipped).
func TestCheckFeedHealth_CustomFeedTreatedAsKnown(t *testing.T) {
	cacheDir := t.TempDir()

	// A custom feed named "pulse" — placeholder cache file.
	writeFeedFile(t, cacheDir, "pulse", "placeholder")

	cfg := core.GetDefaultConfig()
	cfg.Feeds.Custom = []core.CustomFeedConfig{
		{Name: "pulse", Command: "echo pulse", IntervalSeconds: 60, Enabled: true},
	}

	var buf bytes.Buffer
	count := checkFeedHealth(&buf, cacheDir, &cfg)
	out := buf.String()

	assert.Contains(t, out, "feed:pulse", "custom feed (pulse) must appear in output")
	assert.Contains(t, out, "placeholder", "custom feed must warn about placeholder data")
	assert.Equal(t, 1, count, "warning count must be 1 for the custom feed")
}

// writeInsightsFeedFile writes a fresh insights cache envelope with the given
// total_sessions value and a representative set of other zeroed/non-zero fields.
// This mirrors the real zeroedEnvelope shape (fully-keyed, not empty).
func writeInsightsFeedFile(t *testing.T, cacheDir string, totalSessions int) {
	t.Helper()
	data := map[string]interface{}{
		"total_sessions":       totalSessions,
		"total_messages":       0,
		"total_lines_added":    0,
		"avg_duration_minutes": float64(0),
		"top_tools":            []interface{}{},
		"friction_counts":      map[string]interface{}{},
		"friction_total":       0,
		"peak_hour":            0,
		"days_active":          0,
		"staleness_days":       0,
		"recent_msgs_per_day":  0,
		"recent_messages":      0,
		"recent_days_active":   0,
		"recent_session":       map[string]interface{}{},
	}
	envelope := map[string]interface{}{
		"type":      "insights",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      data,
	}
	raw, err := json.Marshal(envelope)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(cacheDir, "insights.json"), raw, 0o644)
	require.NoError(t, err)
}

// TestCheckFeedHealth_InsightsZeroSessionsWarns verifies that a fresh insights
// cache with total_sessions==0 (the zeroedEnvelope shape) emits a WARN rather
// than a false-positive OK. The data map is non-empty so the generic empty-map
// guard would NOT catch this without the zero-liveness check (issue #98).
func TestCheckFeedHealth_InsightsZeroSessionsWarns(t *testing.T) {
	cacheDir := t.TempDir()
	writeInsightsFeedFile(t, cacheDir, 0)

	cfg := core.GetDefaultConfig()
	cfg.Feeds.Insights.Enabled = true

	var buf bytes.Buffer
	count := checkFeedHealth(&buf, cacheDir, &cfg)
	out := buf.String()

	assert.Contains(t, out, "WARN  feed:insights: cache fresh but no sessions recorded",
		"zero-sessions insights envelope must warn")
	assert.NotContains(t, out, "feed:insights: OK",
		"zero-sessions insights envelope must NOT report OK")
	assert.Equal(t, 1, count, "warning count must be 1 for the zero-sessions insights feed")
}

// TestCheckFeedHealth_InsightsWithSessionsOK verifies that a fresh insights cache
// with total_sessions>0 is reported as healthy (no zero-liveness WARN).
func TestCheckFeedHealth_InsightsWithSessionsOK(t *testing.T) {
	cacheDir := t.TempDir()
	writeInsightsFeedFile(t, cacheDir, 5)

	cfg := core.GetDefaultConfig()
	cfg.Feeds.Insights.Enabled = true

	var buf bytes.Buffer
	count := checkFeedHealth(&buf, cacheDir, &cfg)
	out := buf.String()

	assert.Contains(t, out, "feed:insights: OK",
		"insights feed with sessions must report OK")
	assert.NotContains(t, out, "cache fresh but no sessions recorded",
		"insights feed with sessions must NOT emit zero-liveness warning")
	assert.Equal(t, 0, count, "warning count must be 0 for a healthy insights feed")
}
