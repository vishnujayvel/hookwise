package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mockProducer is a test producer that counts invocations and returns
// configurable data.
type mockProducer struct {
	name      string
	callCount atomic.Int64
	data      interface{}
	err       error
	delay     time.Duration
}

func (m *mockProducer) Name() string { return m.name }
func (m *mockProducer) Produce(_ context.Context) (interface{}, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	m.callCount.Add(1)
	return m.data, m.err
}

func newMockProducer(name string, data interface{}) *mockProducer {
	return &mockProducer{name: name, data: data}
}

// newTestDaemon creates a Daemon wired to temp directories for PID file and cache.
func newTestDaemon(t *testing.T, registry *Registry) (*Daemon, string) {
	t.Helper()
	tmpDir := t.TempDir()

	d := NewDaemon(core.DaemonConfig{}, core.FeedsConfig{}, registry)
	d.SetPIDFile(filepath.Join(tmpDir, "daemon.pid"))
	d.SetCacheDir(filepath.Join(tmpDir, "cache"))

	return d, tmpDir
}

// ---------------------------------------------------------------------------
// Test 1: Producer registry — register, get, all
// ---------------------------------------------------------------------------

func TestRegistry_RegisterGetAll(t *testing.T) {
	r := NewRegistry()

	p1 := newMockProducer("alpha", "data-a")
	p2 := newMockProducer("beta", "data-b")
	p3 := newMockProducer("gamma", "data-c")

	r.Register(p1)
	r.Register(p2)
	r.Register(p3)

	// Get existing.
	got, ok := r.Get("alpha")
	require.True(t, ok)
	assert.Equal(t, "alpha", got.Name())

	// Get missing.
	_, ok = r.Get("nonexistent")
	assert.False(t, ok)

	// All returns every producer.
	all := r.All()
	assert.Len(t, all, 3)

	names := make([]string, len(all))
	for i, p := range all {
		names[i] = p.Name()
	}
	sort.Strings(names)
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, names)
}

// ---------------------------------------------------------------------------
// Test 2: Daemon PID file creation and cleanup
// ---------------------------------------------------------------------------

func TestDaemon_PIDFileCreationAndCleanup(t *testing.T) {
	r := NewRegistry()
	d, tmpDir := newTestDaemon(t, r)

	pidPath := filepath.Join(tmpDir, "daemon.pid")

	require.NoError(t, d.Start())

	// PID file should exist.
	data, err := os.ReadFile(pidPath)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)

	// Stop cleans up.
	require.NoError(t, d.Stop())

	_, err = os.Stat(pidPath)
	assert.True(t, os.IsNotExist(err), "PID file should be removed after Stop")
}

// ---------------------------------------------------------------------------
// Test 3: Daemon stale PID detection
// ---------------------------------------------------------------------------

func TestDaemon_StalePIDDetection(t *testing.T) {
	r := NewRegistry()
	d, tmpDir := newTestDaemon(t, r)

	pidPath := filepath.Join(tmpDir, "daemon.pid")

	// Write a stale PID file with a PID that does not exist.
	// Use a very high PID that is extremely unlikely to be alive.
	stalePID := 2147483647
	require.NoError(t, os.MkdirAll(filepath.Dir(pidPath), 0o700))
	require.NoError(t, os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", stalePID)), 0o600))

	// Start should succeed because the stale process is not alive.
	require.NoError(t, d.Start())

	// Verify PID file was updated with current PID.
	data, err := os.ReadFile(pidPath)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)

	require.NoError(t, d.Stop())
}

// ---------------------------------------------------------------------------
// Test 4: Daemon Start/Stop lifecycle
// ---------------------------------------------------------------------------

func TestDaemon_StartStopLifecycle(t *testing.T) {
	r := NewRegistry()
	mp := newMockProducer("test-feed", map[string]interface{}{"value": 42})
	r.Register(mp)

	d, _ := newTestDaemon(t, r)

	require.NoError(t, d.Start())

	// Wait briefly for the initial produce call.
	time.Sleep(200 * time.Millisecond)

	// The producer should have been called at least once (immediate run on start).
	assert.GreaterOrEqual(t, mp.callCount.Load(), int64(1))

	require.NoError(t, d.Stop())
}

// ---------------------------------------------------------------------------
// Test 5: Signal handling (SIGTERM -> graceful stop)
// ---------------------------------------------------------------------------

func TestDaemon_SignalHandling(t *testing.T) {
	r := NewRegistry()
	mp := newMockProducer("sig-feed", map[string]interface{}{"status": "ok"})
	r.Register(mp)

	d, tmpDir := newTestDaemon(t, r)

	require.NoError(t, d.Start())

	// Wait briefly for goroutines to start.
	time.Sleep(200 * time.Millisecond)

	// Send SIGTERM to ourselves — the daemon's signal handler should catch it
	// and trigger graceful shutdown.
	proc, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, proc.Signal(syscall.SIGTERM))

	// Wait for the daemon to shut down gracefully.
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Graceful shutdown completed.
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not shut down within timeout after SIGTERM")
	}

	// Clean up PID file manually since Stop() wasn't called (signal handler closed stopCh).
	pidPath := filepath.Join(tmpDir, "daemon.pid")
	os.Remove(pidPath)
}

// ---------------------------------------------------------------------------
// Test 6: Feed polling with mock producer — produces data, writes to cache
// ---------------------------------------------------------------------------

func TestDaemon_PollFeedWritesCache(t *testing.T) {
	r := NewRegistry()
	feedData := map[string]interface{}{"temperature": 72, "unit": "F"}
	mp := newMockProducer("weather", feedData)
	r.Register(mp)

	d, tmpDir := newTestDaemon(t, r)
	// Enable the weather feed so the daemon doesn't skip it (BP1).
	d.feeds.Weather.Enabled = true
	cacheDir := filepath.Join(tmpDir, "cache")
	d.SetCacheDir(cacheDir)

	require.NoError(t, d.Start())

	// Wait for the initial produce + cache write.
	time.Sleep(300 * time.Millisecond)

	require.NoError(t, d.Stop())

	// Verify cache file was written.
	cachePath := filepath.Join(cacheDir, "weather.json")
	data, err := os.ReadFile(cachePath)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, float64(72), parsed["temperature"])
	assert.Equal(t, "F", parsed["unit"])
}

// ---------------------------------------------------------------------------
// Test 7: Staggered start timing
// ---------------------------------------------------------------------------

func TestDaemon_StaggeredStartTiming(t *testing.T) {
	r := NewRegistry()

	testStagger := 50 * time.Millisecond

	// Register 3 producers that record their first invocation time.
	for i := 0; i < 3; i++ {
		r.Register(&timingProducer{
			name:      fmt.Sprintf("feed-%d", i),
			data:      map[string]interface{}{"index": i},
			firstCall: &atomic.Int64{},
		})
	}

	d, _ := newTestDaemon(t, r)
	d.SetStaggerOffset(testStagger)

	require.NoError(t, d.Start())

	// Wait enough for all staggered starts to complete.
	time.Sleep(time.Duration(3)*testStagger + 200*time.Millisecond)

	require.NoError(t, d.Stop())

	// Collect first-call times from the timing producers.
	allProducers := r.All()
	var times []int64
	for _, p := range allProducers {
		if tp, ok := p.(*timingProducer); ok {
			fc := tp.firstCall.Load()
			if fc > 0 {
				times = append(times, fc)
			}
		}
	}

	// We should have at least 2 recorded times to check stagger.
	require.GreaterOrEqual(t, len(times), 2, "expected at least 2 timing records")
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	// The gap between the first and last producer's first call should be
	// roughly (N-1)*testStagger. Allow some tolerance (half as minimum).
	gap := time.Duration(times[len(times)-1] - times[0])
	expectedMin := time.Duration(len(times)-1) * testStagger / 2
	assert.GreaterOrEqual(t, gap, expectedMin,
		"stagger gap %v should be at least %v", gap, expectedMin)
}

// timingProducer wraps a producer and records the time of its first call.
type timingProducer struct {
	name      string
	data      interface{}
	firstCall *atomic.Int64
}

func (tp *timingProducer) Name() string { return tp.name }
func (tp *timingProducer) Produce(_ context.Context) (interface{}, error) {
	tp.firstCall.CompareAndSwap(0, time.Now().UnixNano())
	return tp.data, nil
}

// ---------------------------------------------------------------------------
// Test 8: Custom producer with mock command
// ---------------------------------------------------------------------------

func TestCustomProducer_Success(t *testing.T) {
	cp := NewCustomProducer("test-custom", `echo '{"greeting":"hello","count":42}'`, 5*time.Second)

	assert.Equal(t, "test-custom", cp.Name())

	result, err := cp.Produce(context.Background())
	require.NoError(t, err)

	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "test-custom", m["type"])
	assert.NotEmpty(t, m["timestamp"])

	data, ok := m["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "hello", data["greeting"])
	assert.Equal(t, float64(42), data["count"])
}

// ---------------------------------------------------------------------------
// Test 9: Custom producer timeout
// ---------------------------------------------------------------------------

func TestCustomProducer_Timeout(t *testing.T) {
	// Command sleeps for 10 seconds, but timeout is 200ms.
	cp := NewCustomProducer("slow-feed", "sleep 10", 200*time.Millisecond)

	_, err := cp.Produce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

// ---------------------------------------------------------------------------
// Test 10: RegisterBuiltins registers all 8 producers
// ---------------------------------------------------------------------------

func TestRegisterBuiltins_All8(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	all := r.All()
	assert.Len(t, all, 8, "RegisterBuiltins should register exactly 8 producers")

	expectedNames := []string{
		"pulse", "project", "news", "calendar",
		"weather", "practice", "memories", "insights",
	}
	sort.Strings(expectedNames)

	names := make([]string, len(all))
	for i, p := range all {
		names[i] = p.Name()
	}
	sort.Strings(names)

	assert.Equal(t, expectedNames, names)

	// Each built-in should produce valid data.
	ctx := context.Background()
	for _, p := range all {
		data, err := p.Produce(ctx)
		require.NoError(t, err, "producer %q should not error", p.Name())

		m, ok := data.(map[string]interface{})
		require.True(t, ok, "producer %q should return map", p.Name())
		assert.Equal(t, p.Name(), m["type"])
		assert.NotEmpty(t, m["timestamp"])
		assert.NotNil(t, m["data"])
	}
}

// ---------------------------------------------------------------------------
// Test 11: Daemon Status() returns correct state
// ---------------------------------------------------------------------------

func TestDaemon_Status(t *testing.T) {
	r := NewRegistry()
	r.Register(newMockProducer("f1", nil))
	r.Register(newMockProducer("f2", nil))

	d, _ := newTestDaemon(t, r)
	d.SetStaggerOffset(10 * time.Millisecond)

	// Before start: not running.
	status := d.Status()
	assert.False(t, status.Running)
	assert.Equal(t, 0, status.PID)
	assert.Equal(t, 2, status.FeedCount)

	// After start: running.
	require.NoError(t, d.Start())
	time.Sleep(100 * time.Millisecond) // allow stagger to complete
	status = d.Status()
	assert.True(t, status.Running)
	assert.Equal(t, os.Getpid(), status.PID)
	assert.Equal(t, 2, status.FeedCount)

	// After stop: not running.
	require.NoError(t, d.Stop())
	status = d.Status()
	assert.False(t, status.Running)
}

// ---------------------------------------------------------------------------
// Test 12: Double-start prevention (PID file exists and process alive)
// ---------------------------------------------------------------------------

func TestDaemon_DoubleStartPrevention(t *testing.T) {
	r := NewRegistry()
	d, _ := newTestDaemon(t, r)

	require.NoError(t, d.Start())

	// Second start should fail because the PID file exists and current process is alive.
	err := d.Start()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	require.NoError(t, d.Stop())
}

// ---------------------------------------------------------------------------
// Test 13: Custom producer with invalid JSON output
// ---------------------------------------------------------------------------

func TestCustomProducer_InvalidJSON(t *testing.T) {
	cp := NewCustomProducer("bad-json", `echo 'not json at all'`, 5*time.Second)

	_, err := cp.Produce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}

// ---------------------------------------------------------------------------
// Test 14: Custom producer with command failure
// ---------------------------------------------------------------------------

func TestCustomProducer_CommandFailure(t *testing.T) {
	cp := NewCustomProducer("fail-cmd", "exit 1", 5*time.Second)

	_, err := cp.Produce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command failed")
}

// ---------------------------------------------------------------------------
// Test 15: Registry overwrite (re-register same name replaces)
// ---------------------------------------------------------------------------

func TestRegistry_OverwriteSameName(t *testing.T) {
	r := NewRegistry()

	p1 := newMockProducer("feed", "data-1")
	p2 := newMockProducer("feed", "data-2")

	r.Register(p1)
	r.Register(p2)

	got, ok := r.Get("feed")
	require.True(t, ok)

	// The second registration should have replaced the first.
	data, err := got.Produce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "data-2", data)

	// All should have exactly 1 entry.
	assert.Len(t, r.All(), 1)
}

// ---------------------------------------------------------------------------
// Test 16: StopByPIDFile sends SIGTERM
// ---------------------------------------------------------------------------

func TestStopByPIDFile_NonExistentFile(t *testing.T) {
	err := StopByPIDFile(filepath.Join(t.TempDir(), "nonexistent.pid"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read pid file")
}

// ---------------------------------------------------------------------------
// Test 17: Polling interval configuration
// ---------------------------------------------------------------------------

func TestDaemon_IntervalFor(t *testing.T) {
	r := NewRegistry()
	d := NewDaemon(core.DaemonConfig{}, core.FeedsConfig{
		Pulse:   core.PulseFeedConfig{IntervalSeconds: 30},
		Weather: core.WeatherFeedConfig{IntervalSeconds: 900},
	}, r)

	assert.Equal(t, 30*time.Second, d.intervalFor("pulse"))
	assert.Equal(t, 900*time.Second, d.intervalFor("weather"))
	assert.Equal(t, defaultInterval, d.intervalFor("unknown-feed"))
	assert.Equal(t, defaultInterval, d.intervalFor("project")) // 0 -> default
}

// ---------------------------------------------------------------------------
// Test 18: Custom producer default timeout
// ---------------------------------------------------------------------------

func TestCustomProducer_DefaultTimeout(t *testing.T) {
	cp := NewCustomProducer("default-timeout", `echo '{"ok":true}'`, 0)
	assert.Equal(t, 10*time.Second, cp.timeout)
}

// ---------------------------------------------------------------------------
// Test 19: Daemon skips disabled feeds (BP1)
// ---------------------------------------------------------------------------

func TestDaemon_SkipsDisabledFeeds(t *testing.T) {
	r := NewRegistry()

	// Register two producers: one for an enabled feed, one for a disabled feed.
	enabledProducer := newMockProducer("pulse", map[string]interface{}{"ok": true})
	disabledProducer := newMockProducer("weather", map[string]interface{}{"temp": 72})
	r.Register(enabledProducer)
	r.Register(disabledProducer)

	d, _ := newTestDaemon(t, r)
	// Enable pulse, leave weather disabled (zero value = false).
	d.feeds.Pulse.Enabled = true
	// Weather.Enabled is false by default (zero value).
	d.SetStaggerOffset(0)

	require.NoError(t, d.Start())
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, d.Stop())

	// Pulse (enabled) should have been called.
	assert.GreaterOrEqual(t, enabledProducer.callCount.Load(), int64(1),
		"enabled producer should have been called")

	// Weather (disabled) should NOT have been called.
	assert.Equal(t, int64(0), disabledProducer.callCount.Load(),
		"disabled producer should not have been called")
}

// ---------------------------------------------------------------------------
// Test 20: Daemon isEnabled — default for unknown feeds is true (fail-open)
// ---------------------------------------------------------------------------

func TestDaemon_IsEnabled(t *testing.T) {
	r := NewRegistry()
	d := NewDaemon(core.DaemonConfig{}, core.FeedsConfig{
		Pulse:   core.PulseFeedConfig{Enabled: true},
		Weather: core.WeatherFeedConfig{Enabled: false},
	}, r)

	assert.True(t, d.isEnabled("pulse"), "pulse should be enabled")
	assert.False(t, d.isEnabled("weather"), "weather should be disabled")
	assert.True(t, d.isEnabled("unknown-custom-feed"), "unknown feeds default to enabled (fail-open)")
}

// ---------------------------------------------------------------------------
// InsightsProducer Tests
// ---------------------------------------------------------------------------

// writeSessionMeta creates a session-meta JSON file in the given directory.
func writeSessionMeta(t *testing.T, dir string, session map[string]interface{}) {
	t.Helper()
	id, _ := session["session_id"].(string)
	if id == "" {
		id = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	data, err := json.Marshal(session)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, id+".json"), data, 0o644))
}

// writeFacet creates a facets JSON file in the given directory.
func writeFacet(t *testing.T, dir string, facet map[string]interface{}) {
	t.Helper()
	sid, _ := facet["session_id"].(string)
	if sid == "" {
		sid = fmt.Sprintf("facet-%d", time.Now().UnixNano())
	}
	data, err := json.Marshal(facet)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, sid+".json"), data, 0o644))
}

func TestInsightsProducer_NoUsageData(t *testing.T) {
	p := &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: filepath.Join(t.TempDir(), "nonexistent"),
			StalenessDays: 30,
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "insights", m["type"])

	data, ok := m["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0, toInt(data["total_sessions"]))
	assert.Equal(t, 0, toInt(data["total_messages"]))

	// Must NOT have "source": "placeholder".
	_, hasSource := data["source"]
	assert.False(t, hasSource, "zeroed envelope should not have 'source' field")
}

func TestInsightsProducer_AggregatesSessionMeta(t *testing.T) {
	tmpDir := t.TempDir()
	metaDir := filepath.Join(tmpDir, "session-meta")
	require.NoError(t, os.MkdirAll(metaDir, 0o700))

	now := time.Now().UTC()

	writeSessionMeta(t, metaDir, map[string]interface{}{
		"session_id":         "s1",
		"start_time":         now.Add(-1 * time.Hour).Format(time.RFC3339),
		"user_message_count": 10,
		"lines_added":        100,
		"duration_minutes":   30.0,
		"tool_counts":        map[string]interface{}{"Read": 5, "Edit": 3},
		"message_hours":      []interface{}{14.0, 14.0, 15.0},
	})
	writeSessionMeta(t, metaDir, map[string]interface{}{
		"session_id":         "s2",
		"start_time":         now.Add(-2 * time.Hour).Format(time.RFC3339),
		"user_message_count": 20,
		"lines_added":        200,
		"duration_minutes":   45.0,
		"tool_counts":        map[string]interface{}{"Read": 10, "Bash": 7},
		"message_hours":      []interface{}{14.0, 16.0},
	})

	p := &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: tmpDir,
			StalenessDays: 30,
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	m := result.(map[string]interface{})
	data := m["data"].(map[string]interface{})

	assert.Equal(t, 2, toInt(data["total_sessions"]))
	assert.Equal(t, 30, toInt(data["total_messages"]))
	assert.Equal(t, 300, toInt(data["total_lines_added"]))

	// avg_duration = (30 + 45) / 2 = 37.5
	avgDur, ok := data["avg_duration_minutes"].(float64)
	require.True(t, ok)
	assert.InDelta(t, 37.5, avgDur, 0.1)
}

func TestInsightsProducer_FiltersByStaleness(t *testing.T) {
	tmpDir := t.TempDir()
	metaDir := filepath.Join(tmpDir, "session-meta")
	require.NoError(t, os.MkdirAll(metaDir, 0o700))

	now := time.Now().UTC()

	// Recent session (within 7 days).
	writeSessionMeta(t, metaDir, map[string]interface{}{
		"session_id":         "recent",
		"start_time":         now.Add(-1 * time.Hour).Format(time.RFC3339),
		"user_message_count": 10,
		"lines_added":        100,
		"duration_minutes":   30.0,
	})

	// Old session (60 days ago — outside 30-day window).
	writeSessionMeta(t, metaDir, map[string]interface{}{
		"session_id":         "old",
		"start_time":         now.Add(-60 * 24 * time.Hour).Format(time.RFC3339),
		"user_message_count": 50,
		"lines_added":        500,
		"duration_minutes":   120.0,
	})

	p := &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: tmpDir,
			StalenessDays: 30,
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})

	// Only the recent session should be counted.
	assert.Equal(t, 1, toInt(data["total_sessions"]))
	assert.Equal(t, 10, toInt(data["total_messages"]))
	assert.Equal(t, 100, toInt(data["total_lines_added"]))
}

func TestInsightsProducer_TopToolsLimited(t *testing.T) {
	tmpDir := t.TempDir()
	metaDir := filepath.Join(tmpDir, "session-meta")
	require.NoError(t, os.MkdirAll(metaDir, 0o700))

	now := time.Now().UTC()

	// Create a session with 15 different tools.
	tools := make(map[string]interface{})
	for i := 0; i < 15; i++ {
		tools[fmt.Sprintf("Tool%02d", i)] = i + 1
	}

	writeSessionMeta(t, metaDir, map[string]interface{}{
		"session_id":         "many-tools",
		"start_time":         now.Add(-1 * time.Hour).Format(time.RFC3339),
		"user_message_count": 10,
		"lines_added":        100,
		"duration_minutes":   30.0,
		"tool_counts":        tools,
	})

	p := &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: tmpDir,
			StalenessDays: 30,
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	// top_tools may be []map[string]interface{} (direct from producer) or
	// []interface{} (after JSON round-trip). Handle both safely.
	switch tt := data["top_tools"].(type) {
	case []map[string]interface{}:
		assert.LessOrEqual(t, len(tt), 10, "top_tools should be limited to 10")
	case []interface{}:
		assert.LessOrEqual(t, len(tt), 10, "top_tools should be limited to 10")
	default:
		t.Fatalf("top_tools has unexpected type %T", data["top_tools"])
	}
}

func TestInsightsProducer_FrictionFromFacets(t *testing.T) {
	tmpDir := t.TempDir()
	metaDir := filepath.Join(tmpDir, "session-meta")
	facetsDir := filepath.Join(tmpDir, "facets")
	require.NoError(t, os.MkdirAll(metaDir, 0o700))
	require.NoError(t, os.MkdirAll(facetsDir, 0o700))

	now := time.Now().UTC()

	writeSessionMeta(t, metaDir, map[string]interface{}{
		"session_id":         "s1",
		"start_time":         now.Add(-1 * time.Hour).Format(time.RFC3339),
		"user_message_count": 10,
		"lines_added":        100,
		"duration_minutes":   30.0,
	})

	writeFacet(t, facetsDir, map[string]interface{}{
		"session_id": "s1",
		"friction_counts": map[string]interface{}{
			"wrong_approach":      3,
			"misunderstood_request": 2,
		},
	})

	// Facet for an unknown session should be ignored.
	writeFacet(t, facetsDir, map[string]interface{}{
		"session_id": "unknown-session",
		"friction_counts": map[string]interface{}{
			"tool_error": 10,
		},
	})

	p := &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: tmpDir,
			StalenessDays: 30,
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})

	assert.Equal(t, 5, toInt(data["friction_total"]))

	fc, ok := data["friction_counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 3, toInt(fc["wrong_approach"]))
	assert.Equal(t, 2, toInt(fc["misunderstood_request"]))
	// tool_error from unknown session should NOT be included.
	_, hasToolError := fc["tool_error"]
	assert.False(t, hasToolError)
}

func TestInsightsProducer_NoPlaceholderSource(t *testing.T) {
	tmpDir := t.TempDir()
	metaDir := filepath.Join(tmpDir, "session-meta")
	require.NoError(t, os.MkdirAll(metaDir, 0o700))

	now := time.Now().UTC()
	writeSessionMeta(t, metaDir, map[string]interface{}{
		"session_id":         "s1",
		"start_time":         now.Add(-1 * time.Hour).Format(time.RFC3339),
		"user_message_count": 5,
		"lines_added":        50,
		"duration_minutes":   15.0,
	})

	p := &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: tmpDir,
			StalenessDays: 30,
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	_, hasSource := data["source"]
	assert.False(t, hasSource, "real insights data should NOT have 'source' field")
}

func TestInsightsProducer_MalformedFilesSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	metaDir := filepath.Join(tmpDir, "session-meta")
	require.NoError(t, os.MkdirAll(metaDir, 0o700))

	now := time.Now().UTC()

	// Valid session.
	writeSessionMeta(t, metaDir, map[string]interface{}{
		"session_id":         "valid",
		"start_time":         now.Add(-1 * time.Hour).Format(time.RFC3339),
		"user_message_count": 10,
		"lines_added":        100,
		"duration_minutes":   30.0,
	})

	// Malformed JSON file.
	require.NoError(t, os.WriteFile(filepath.Join(metaDir, "bad.json"), []byte("not json"), 0o644))

	// Non-object JSON (array).
	require.NoError(t, os.WriteFile(filepath.Join(metaDir, "array.json"), []byte("[1,2,3]"), 0o644))

	p := &InsightsProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Insights: core.InsightsFeedConfig{
			UsageDataPath: tmpDir,
			StalenessDays: 30,
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	assert.Equal(t, 1, toInt(data["total_sessions"]), "should only count the valid session")
}

// ---------------------------------------------------------------------------
// Calendar Producer Tests
// ---------------------------------------------------------------------------

// TestCalendarProducer_ImplementsConfigAware is a compile-time check.
var _ ConfigAware = (*CalendarProducer)(nil)

func TestCalendarProducer_NoToken(t *testing.T) {
	p := &CalendarProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			TokenPath: "/nonexistent/path/token.json",
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: must not return error")

	envelope := result.(map[string]interface{})
	assert.Equal(t, "calendar", envelope["type"])

	data := envelope["data"].(map[string]interface{})
	events := data["events"].([]interface{})
	assert.Empty(t, events, "no token → empty events")
	assert.Nil(t, data["next_event"], "no token → nil next_event")
}

func TestCalendarProducer_NoPlaceholderSource(t *testing.T) {
	p := &CalendarProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			TokenPath: "/nonexistent/path/token.json",
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	envelope := result.(map[string]interface{})
	data := envelope["data"].(map[string]interface{})
	_, hasSource := data["source"]
	assert.False(t, hasSource, "output must not contain 'source' key")
}

// writeFakeToken creates a Python google-auth format token file for testing.
func writeFakeToken(t *testing.T, dir string) string {
	t.Helper()
	tokenPath := filepath.Join(dir, "token.json")
	tok := map[string]interface{}{
		"token":         "fake-access-token",
		"refresh_token": "fake-refresh-token",
		"token_uri":     "https://oauth2.googleapis.com/token",
		"client_id":     "fake-client-id.apps.googleusercontent.com",
		"client_secret": "fake-client-secret",
		"expiry":        time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(tok)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tokenPath, data, 0600))
	return tokenPath
}

// fakeCalendarAPIResponse returns a Google Calendar API events list JSON response.
func fakeCalendarAPIResponse(events []map[string]interface{}) string {
	resp := map[string]interface{}{
		"kind":    "calendar#events",
		"summary": "test@example.com",
		"items":   events,
	}
	data, _ := json.Marshal(resp)
	return string(data)
}

func TestCalendarProducer_ParsesEvents(t *testing.T) {
	now := time.Now()
	eventStart := now.Add(30 * time.Minute).UTC().Format(time.RFC3339)
	eventEnd := now.Add(60 * time.Minute).UTC().Format(time.RFC3339)

	apiResp := fakeCalendarAPIResponse([]map[string]interface{}{
		{
			"summary": "Team Standup",
			"start":   map[string]string{"dateTime": eventStart},
			"end":     map[string]string{"dateTime": eventEnd},
		},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, apiResp)
	}))
	defer srv.Close()

	tokenPath := writeFakeToken(t, t.TempDir())
	p := &CalendarProducer{baseURL: srv.URL}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			TokenPath: tokenPath,
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	envelope := result.(map[string]interface{})
	data := envelope["data"].(map[string]interface{})
	events := data["events"].([]interface{})
	require.Len(t, events, 1)

	ev := events[0].(map[string]interface{})
	assert.Equal(t, "Team Standup", ev["name"])
	assert.False(t, ev["all_day"].(bool))
	assert.False(t, ev["is_current"].(bool))

	nextEvent := data["next_event"].(map[string]interface{})
	assert.Equal(t, "Team Standup", nextEvent["name"])
	// next_event now stores absolute start time (relative computed at render time).
	assert.NotEmpty(t, nextEvent["start"], "next_event should have absolute start time")
}

func TestCalendarProducer_FindsNextEvent(t *testing.T) {
	now := time.Now()

	// First event is current (started 10m ago, ends in 20m).
	currentStart := now.Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	currentEnd := now.Add(20 * time.Minute).UTC().Format(time.RFC3339)
	// Second event is upcoming (starts in 45m).
	upcomingStart := now.Add(45 * time.Minute).UTC().Format(time.RFC3339)
	upcomingEnd := now.Add(75 * time.Minute).UTC().Format(time.RFC3339)

	apiResp := fakeCalendarAPIResponse([]map[string]interface{}{
		{
			"summary": "Current Meeting",
			"start":   map[string]string{"dateTime": currentStart},
			"end":     map[string]string{"dateTime": currentEnd},
		},
		{
			"summary": "Next Meeting",
			"start":   map[string]string{"dateTime": upcomingStart},
			"end":     map[string]string{"dateTime": upcomingEnd},
		},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, apiResp)
	}))
	defer srv.Close()

	tokenPath := writeFakeToken(t, t.TempDir())
	p := &CalendarProducer{baseURL: srv.URL}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{TokenPath: tokenPath},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	events := data["events"].([]interface{})
	require.Len(t, events, 2)

	// First event should be marked as current.
	assert.True(t, events[0].(map[string]interface{})["is_current"].(bool))

	// next_event should be the second (first non-current) event.
	nextEvent := data["next_event"].(map[string]interface{})
	assert.Equal(t, "Next Meeting", nextEvent["name"])
	assert.NotEmpty(t, nextEvent["start"], "next_event should have absolute start time")
}

func TestCalendarProducer_AllDayEvents(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	apiResp := fakeCalendarAPIResponse([]map[string]interface{}{
		{
			"summary": "Company Holiday",
			"start":   map[string]string{"date": today},
			"end":     map[string]string{"date": today},
		},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, apiResp)
	}))
	defer srv.Close()

	tokenPath := writeFakeToken(t, t.TempDir())
	p := &CalendarProducer{baseURL: srv.URL}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{TokenPath: tokenPath},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	events := data["events"].([]interface{})
	require.Len(t, events, 1)

	ev := events[0].(map[string]interface{})
	assert.True(t, ev["all_day"].(bool))
	assert.Equal(t, today, ev["start"])
}

func TestCalendarProducer_RelativeTime(t *testing.T) {
	now := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		start    time.Time
		expected string
	}{
		{"5 minutes", now.Add(5 * time.Minute), "in 5m"},
		{"45 minutes", now.Add(45 * time.Minute), "in 45m"},
		{"2 hours", now.Add(2 * time.Hour), "in 2h"},
		{"2h 30m", now.Add(2*time.Hour + 30*time.Minute), "in 2h 30m"},
		{"2h 3m rounds", now.Add(2*time.Hour + 3*time.Minute), "in 2h"},
		{"past/now", now.Add(-1 * time.Minute), "now"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := relativeTimeString(now, tt.start)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalendarProducer_RelativeTime_Tomorrow(t *testing.T) {
	now := time.Date(2026, 3, 8, 22, 0, 0, 0, time.UTC)
	tomorrow3pm := time.Date(2026, 3, 9, 15, 0, 0, 0, time.UTC)

	result := relativeTimeString(now, tomorrow3pm)
	assert.Contains(t, result, "tomorrow")
	assert.Contains(t, result, "3:00pm")
}

func TestCalendarProducer_RelativeTime_NextWeek(t *testing.T) {
	now := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC) // Sunday
	wed9am := time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC)

	result := relativeTimeString(now, wed9am)
	assert.Contains(t, result, "Wed")
	assert.Contains(t, result, "9:00am")
}

func TestCalendarProducer_FallbackOnAPIError(t *testing.T) {
	callCount := 0
	now := time.Now()
	eventStart := now.Add(30 * time.Minute).UTC().Format(time.RFC3339)
	eventEnd := now.Add(60 * time.Minute).UTC().Format(time.RFC3339)

	goodResp := fakeCalendarAPIResponse([]map[string]interface{}{
		{
			"summary": "Standup",
			"start":   map[string]string{"dateTime": eventStart},
			"end":     map[string]string{"dateTime": eventEnd},
		},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, goodResp)
		} else {
			http.Error(w, "Internal Server Error", 500)
		}
	}))
	defer srv.Close()

	tokenPath := writeFakeToken(t, t.TempDir())
	p := &CalendarProducer{baseURL: srv.URL}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{TokenPath: tokenPath},
	})

	// First call succeeds.
	result1, err := p.Produce(context.Background())
	require.NoError(t, err)
	data1 := result1.(map[string]interface{})["data"].(map[string]interface{})
	require.Len(t, data1["events"].([]interface{}), 1)

	// Second call fails → returns cached result.
	result2, err := p.Produce(context.Background())
	require.NoError(t, err)
	data2 := result2.(map[string]interface{})["data"].(map[string]interface{})
	require.Len(t, data2["events"].([]interface{}), 1, "fallback should return cached events")
}

// ---------------------------------------------------------------------------
// ProjectProducer Tests
// ---------------------------------------------------------------------------

// TestProjectProducer_ImplementsConfigAware is a compile-time check.
var _ ConfigAware = (*ProjectProducer)(nil)

func TestProjectProducer_ReturnsGitData(t *testing.T) {
	p := &ProjectProducer{}

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: must not return error")

	envelope, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "project", envelope["type"])
	assert.NotEmpty(t, envelope["timestamp"])

	data, ok := envelope["data"].(map[string]interface{})
	require.True(t, ok)

	// We're running in the hookwise repo, so these should be non-empty.
	name, ok := data["name"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, name, "name should be non-empty in a git repo")

	branch, ok := data["branch"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, branch, "branch should be non-empty in a git repo")

	commit, ok := data["last_commit"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, commit, "last_commit should be non-empty in a git repo")

	_, ok = data["dirty"].(bool)
	assert.True(t, ok, "dirty should be a bool")

	// Must NOT have "source": "placeholder".
	_, hasSource := data["source"]
	assert.False(t, hasSource, "real git data should NOT have 'source' field")
}

func TestProjectProducer_NonGitDirectory(t *testing.T) {
	// Create a temp directory that is NOT a git repo.
	tmpDir := t.TempDir()

	// Save and restore the working directory.
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { os.Chdir(origDir) })

	p := &ProjectProducer{}

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: must not return error even outside a git repo")

	envelope, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "project", envelope["type"])
	assert.NotEmpty(t, envelope["timestamp"])

	data, ok := envelope["data"].(map[string]interface{})
	require.True(t, ok)

	// Fields may be empty but must exist and have correct types.
	_, ok = data["name"].(string)
	assert.True(t, ok, "name should be a string")
	_, ok = data["branch"].(string)
	assert.True(t, ok, "branch should be a string")
	_, ok = data["last_commit"].(string)
	assert.True(t, ok, "last_commit should be a string")
	_, ok = data["dirty"].(bool)
	assert.True(t, ok, "dirty should be a bool")

	// Must NOT have "source": "placeholder".
	_, hasSource := data["source"]
	assert.False(t, hasSource, "fallback should NOT have 'source' field")
}

func TestProjectTestFixture_FieldConsistency(t *testing.T) {
	fixture := ProjectTestFixture()

	// The fixture must have the same top-level and data-level field names
	// as a real Produce() call to prevent cross-boundary Bug #29 pattern.
	p := &ProjectProducer{}
	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	realEnvelope := result.(map[string]interface{})
	fixtureEnvelope := fixture

	// Top-level keys must match.
	for key := range realEnvelope {
		_, ok := fixtureEnvelope[key]
		assert.True(t, ok, "fixture missing top-level key %q", key)
	}
	for key := range fixtureEnvelope {
		_, ok := realEnvelope[key]
		assert.True(t, ok, "fixture has extra top-level key %q", key)
	}

	// Data-level keys must match.
	realData := realEnvelope["data"].(map[string]interface{})
	fixtureData := fixtureEnvelope["data"].(map[string]interface{})

	for key := range realData {
		_, ok := fixtureData[key]
		assert.True(t, ok, "fixture data missing key %q", key)
	}
	for key := range fixtureData {
		_, ok := realData[key]
		assert.True(t, ok, "fixture data has extra key %q", key)
	}
}
