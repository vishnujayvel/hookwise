//go:build integration

// Calendar entries in the chaos matrix (scout hw-xthx blind spot #4).
// The calendar feed had zero chaos coverage while weather/pulse were
// exercised; these tests mirror the existing corrupt-cache and
// producer-panic patterns for calendar, and add the dead-daemon staleness
// transition (embedded timestamp past TTL while file mtime stays fresh).
package chaos

import (
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

// calendarFeedData returns calendar data in the schema pinned by
// feeds.CalendarTestFixture, with an upcoming next_event.
func calendarFeedData(eventStart time.Time) map[string]interface{} {
	start := eventStart.UTC().Format(time.RFC3339)
	return map[string]interface{}{
		"events": []interface{}{
			map[string]interface{}{
				"name":       "Standup",
				"start":      start,
				"end":        eventStart.Add(30 * time.Minute).UTC().Format(time.RFC3339),
				"all_day":    false,
				"is_current": false,
			},
		},
		"next_event": map[string]interface{}{
			"name":  "Standup",
			"start": start,
		},
	}
}

// TestChaos_CalendarCorruptCacheSkipped verifies that CollectFeedCache
// returns a valid calendar entry while skipping corrupt sibling files —
// the calendar mirror of TestChaos_CorruptCacheSkipped.
func TestChaos_CalendarCorruptCacheSkipped(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "feed-cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o700))

	validData := map[string]interface{}{
		"type":      "calendar",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      calendarFeedData(time.Now().Add(time.Hour)),
	}
	validJSON, err := json.MarshalIndent(validData, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "calendar.json"), validJSON, 0o600))

	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "corrupt1.json"), []byte("{{{invalid"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "corrupt2.json"), []byte(""), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "corrupt3.json"), []byte{0xFF, 0xFE, 0x00, 0x01}, 0o600))

	collected, err := bridge.CollectFeedCache(cacheDir)
	require.NoError(t, err, "CollectFeedCache should not error on corrupt siblings")
	require.Contains(t, collected, "calendar", "valid calendar entry should be collected")

	calEntry, ok := collected["calendar"].(map[string]interface{})
	require.True(t, ok, "calendar entry should be a map")
	assert.Equal(t, "calendar", calEntry["type"])
	assert.True(t, bridge.IsEnvelopeFresh(calEntry), "just-written calendar entry should be fresh")

	for _, name := range []string{"corrupt1", "corrupt2", "corrupt3"} {
		assert.NotContains(t, collected, name, "%s should be skipped", name)
	}
}

// TestChaos_CalendarProducerPanicRecovery verifies that a panicking calendar
// producer cannot crash the daemon or block sibling feeds, and leaves no
// calendar cache file behind — the calendar mirror of
// TestChaos_ProducerPanicRecovery.
func TestChaos_CalendarProducerPanicRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmpDir)

	cacheDir := filepath.Join(tmpDir, "feed-cache")
	pidFile := filepath.Join(tmpDir, "test-daemon.pid")

	registry := feeds.NewRegistry()
	registry.Register(&panicProducer{name: "calendar", panicMsg: "calendar chaos panic"})
	registry.Register(&goodProducer{
		name: "survivor",
		data: map[string]interface{}{"alive": true},
	})

	// Calendar is a recognised feed and defaults to disabled; enable it so the
	// daemon actually polls the panicking producer instead of skipping it.
	daemon := feeds.NewDaemon(core.DaemonConfig{}, core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{Enabled: true},
	}, registry)
	daemon.SetPIDFile(pidFile)
	daemon.SetCacheDir(cacheDir)
	// Isolate the unix socket: DefaultSocketPath is frozen at init and ignores
	// HOOKWISE_STATE_DIR (#228).
	daemon.SetSocketPath(shortSocketPath(t))
	daemon.SetStaggerOffset(0)

	require.NoError(t, daemon.Start())
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(cacheDir, "survivor.json"))
		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "survivor.json was not written before timeout")
	require.NoError(t, daemon.Stop())

	assert.FileExists(t, filepath.Join(cacheDir, "survivor.json"),
		"sibling feed should still write cache despite panicking calendar producer")

	_, err := os.Stat(filepath.Join(cacheDir, "calendar.json"))
	assert.True(t, os.IsNotExist(err), "panicking calendar producer should not have written a cache file")
}

// TestChaos_CalendarDeadDaemonStaleTransition verifies the fresh→stale
// transition for a calendar envelope left behind by a stopped daemon: the
// entry stays fresh right after the daemon dies, and flips stale once its
// embedded timestamp ages past the daemon-injected ttl_seconds — even though
// the cache file's mtime is current. Consumers keying on IsEnvelopeFresh
// (the Go status line) drop the segment; nothing may key on mtime.
func TestChaos_CalendarDeadDaemonStaleTransition(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmpDir)

	cacheDir := filepath.Join(tmpDir, "feed-cache")
	pidFile := filepath.Join(tmpDir, "test-daemon.pid")

	registry := feeds.NewRegistry()
	registry.Register(&goodProducer{
		name: "calendar",
		data: calendarFeedData(time.Now().Add(time.Hour)),
	})

	daemon := feeds.NewDaemon(core.DaemonConfig{}, core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{Enabled: true},
	}, registry)
	daemon.SetPIDFile(pidFile)
	daemon.SetCacheDir(cacheDir)
	daemon.SetSocketPath(shortSocketPath(t))
	daemon.SetStaggerOffset(0)

	cacheFile := filepath.Join(cacheDir, "calendar.json")
	require.NoError(t, daemon.Start())
	require.Eventually(t, func() bool {
		_, err := os.Stat(cacheFile)
		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "calendar.json was not written before timeout")
	require.NoError(t, daemon.Stop())

	// Right after the daemon dies the leftover envelope is still fresh.
	collected, err := bridge.CollectFeedCache(cacheDir)
	require.NoError(t, err)
	envelope, ok := collected["calendar"].(map[string]interface{})
	require.True(t, ok, "calendar entry should be a map")
	require.True(t, bridge.IsEnvelopeFresh(envelope), "envelope must be fresh immediately after daemon stop")

	data, ok := envelope["data"].(map[string]interface{})
	require.True(t, ok)
	ttl, ok := data["ttl_seconds"].(float64)
	require.True(t, ok, "daemon must inject ttl_seconds into the calendar envelope")

	// Age the embedded timestamp past TTL. Rewriting the file sets its mtime
	// to NOW: mtime-fresh, content-stale — the dead-daemon divergence.
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
		"file mtime must be fresh — staleness must come from the embedded timestamp")

	collected, err = bridge.CollectFeedCache(cacheDir)
	require.NoError(t, err)
	staleEnvelope, ok := collected["calendar"].(map[string]interface{})
	require.True(t, ok)
	assert.False(t, bridge.IsEnvelopeFresh(staleEnvelope),
		"envelope aged past ttl_seconds must be stale despite a fresh file mtime")
}
