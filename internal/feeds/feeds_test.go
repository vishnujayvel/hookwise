package feeds

import (
	"context"
	"encoding/json"
	"fmt"
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
