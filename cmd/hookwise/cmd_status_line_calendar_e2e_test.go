//go:build integration

// Dead-daemon end-to-end coverage for the calendar status-line segment
// (scout hw-xthx blind spot #4). A real daemon writes the calendar envelope
// to disk; after the daemon stops, the segment must keep rendering until the
// embedded timestamp ages past ttl_seconds, then disappear — even though the
// cache file's mtime stays fresh. Freshness is content-based
// (bridge.IsEnvelopeFresh), never mtime-based.
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishnujayvel/hookwise/internal/bridge"
	"github.com/vishnujayvel/hookwise/internal/core"
	"github.com/vishnujayvel/hookwise/internal/feeds"
)

// calendarE2ESocketPath returns a unix socket path under /tmp for a test
// daemon. macOS /var/folders temp paths (from t.TempDir) can exceed the
// 104-byte unix socket limit, so sockets get their own short dir.
func calendarE2ESocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hw-sock-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "d.sock")
}

// calendarE2EProducer emits a calendar envelope in the exact shape the real
// producer writes (schema pinned by feeds.CalendarTestFixture), with an
// upcoming next_event so the rendered segment is non-empty.
type calendarE2EProducer struct {
	eventStart string
}

func (p *calendarE2EProducer) Name() string { return "calendar" }
func (p *calendarE2EProducer) Produce(_ context.Context) (interface{}, error) {
	return feeds.NewEnvelope("calendar", map[string]interface{}{
		"events": []interface{}{
			map[string]interface{}{
				"name":       "Standup",
				"start":      p.eventStart,
				"end":        p.eventStart,
				"all_day":    false,
				"is_current": false,
			},
		},
		"next_event": map[string]interface{}{
			"name":  "Standup",
			"start": p.eventStart,
		},
	}), nil
}

func TestIntegration_CalendarStatusLine_DeadDaemonStaleOmitted(t *testing.T) {
	tmpDir := t.TempDir()
	// #228 isolation contract: never touch the real ~/.hookwise.
	t.Setenv("HOOKWISE_STATE_DIR", tmpDir)

	// runStatusLine reads the feed cache from GetStateDir()/state — point the
	// daemon's cache writer at the exact directory the status line reads.
	cacheDir := filepath.Join(core.GetStateDir(), "state")
	require.Equal(t, filepath.Join(tmpDir, "state"), cacheDir,
		"HOOKWISE_STATE_DIR override must be in effect before starting the daemon")

	eventStart := time.Now().Add(45 * time.Minute).UTC().Format(time.RFC3339)
	registry := feeds.NewRegistry()
	registry.Register(&calendarE2EProducer{eventStart: eventStart})

	// Calendar is a recognised feed and defaults to disabled; without the
	// explicit enable the daemon skips it and calendar.json is never written.
	daemon := feeds.NewDaemon(core.DaemonConfig{}, core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{Enabled: true},
	}, registry)
	daemon.SetPIDFile(filepath.Join(tmpDir, "test-daemon.pid"))
	daemon.SetCacheDir(cacheDir)
	// DefaultSocketPath is frozen at package init from ~/.hookwise, so
	// HOOKWISE_STATE_DIR does not move it; isolate explicitly (#228).
	daemon.SetSocketPath(calendarE2ESocketPath(t))
	daemon.SetStaggerOffset(10 * time.Millisecond)

	require.NoError(t, daemon.Start())
	cacheFile := filepath.Join(cacheDir, "calendar.json")
	require.Eventually(t, func() bool {
		_, err := os.Stat(cacheFile)
		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "daemon should write calendar.json")

	// Daemon is dead from here on — everything below reads its leftovers.
	require.NoError(t, daemon.Stop())

	// --- Phase 1: fresh envelope renders the segment -----------------------
	// Presence detector: proves the omission assertion in phase 2 is
	// meaningful, not a test that would pass on any empty output.
	collected, err := bridge.CollectFeedCache(cacheDir)
	require.NoError(t, err)
	require.Contains(t, collected, "calendar")

	freshSegment := stripANSI(renderBuiltinSegment("calendar", collected, nil))
	require.NotEmpty(t, freshSegment, "fresh envelope from a dead daemon must still render")
	assert.Contains(t, freshSegment, "Standup")
	assert.Contains(t, freshSegment, "\U0001f4c5")

	// The daemon must have injected ttl_seconds into the on-disk data map;
	// the aging step below derives its offset from this real value.
	envelope, ok := collected["calendar"].(map[string]interface{})
	require.True(t, ok)
	require.True(t, bridge.IsEnvelopeFresh(envelope), "just-written envelope must be fresh")
	data, ok := envelope["data"].(map[string]interface{})
	require.True(t, ok)
	ttl, ok := data["ttl_seconds"].(float64)
	require.True(t, ok, "daemon must inject ttl_seconds into the cache envelope")

	// --- Phase 2: age the embedded timestamp past TTL ----------------------
	// Rewrite ONLY the envelope's timestamp; rewriting the file makes its
	// mtime NOW, i.e. mtime-fresh but content-stale — exactly the divergence
	// a dead daemon leaves behind. The gate must key on content, not mtime.
	raw, err := os.ReadFile(cacheFile)
	require.NoError(t, err)
	var onDisk map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &onDisk))
	agedTS := time.Now().Add(-(time.Duration(ttl)*time.Second + time.Minute))
	onDisk["timestamp"] = agedTS.UTC().Format(time.RFC3339)
	aged, err := json.Marshal(onDisk)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cacheFile, aged, 0o600))

	info, err := os.Stat(cacheFile)
	require.NoError(t, err)
	require.Less(t, time.Since(info.ModTime()), time.Minute,
		"file mtime must be fresh — staleness below must come from the embedded timestamp")

	// --- Phase 3: stale envelope is omitted from the status line -----------
	collected, err = bridge.CollectFeedCache(cacheDir)
	require.NoError(t, err)
	require.Contains(t, collected, "calendar", "stale entry is still collected; the render gate drops it")

	staleEnvelope, ok := collected["calendar"].(map[string]interface{})
	require.True(t, ok)
	assert.False(t, bridge.IsEnvelopeFresh(staleEnvelope), "aged envelope must be past TTL")

	staleSegment := renderBuiltinSegment("calendar", collected, nil)
	assert.Empty(t, staleSegment,
		"stale calendar envelope must be omitted from the status line (stale = absent)")
}
