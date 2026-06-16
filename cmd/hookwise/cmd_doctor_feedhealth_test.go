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

// TestCheckFeedHealth_OrphanSkipped verifies that orphan cache files (files with
// no corresponding built-in or custom feed) are silently skipped and do not
// produce any output or increment the warning count.
func TestCheckFeedHealth_OrphanSkipped(t *testing.T) {
	cacheDir := t.TempDir()

	// (a) Orphan: practice.json — no Go producer, not in cfg.Feeds.Custom.
	writeFeedFile(t, cacheDir, "practice", "placeholder")
	// (b) Known built-in: news.json — placeholder should still warn.
	writeFeedFile(t, cacheDir, "news", "placeholder")

	cfg := core.GetDefaultConfig()
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
