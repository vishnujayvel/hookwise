//go:build integration

package chaos

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/bridge"
	"github.com/vishnujayvel/hookwise/internal/core"
	"github.com/vishnujayvel/hookwise/internal/feeds"
)

// =========================================================================
// Task 4.1: Dispatch resilience under failure conditions
// =========================================================================

// TestChaos_DispatchFailOpen_AnalyticsEnabled verifies ARCH-1: even with
// analytics enabled (but no DB available), dispatch returns exit 0.
func TestChaos_DispatchFailOpen_AnalyticsEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmpDir)

	config := core.GetDefaultConfig()
	// Enable analytics — but we provide no valid Dolt DB path.
	// The side effects will attempt analytics and fail gracefully.
	config.Analytics.Enabled = true

	// Test across multiple event types
	events := []string{
		core.EventPreToolUse,
		core.EventPostToolUse,
		core.EventSessionStart,
		core.EventSessionEnd,
		core.EventStop,
	}

	for _, eventType := range events {
		t.Run(eventType, func(t *testing.T) {
			result := core.Dispatch(eventType, core.HookPayload{
				SessionID: "chaos-session-001",
				ToolName:  "Bash",
				ToolInput: map[string]interface{}{"command": "ls"},
			}, config)
			assert.Equal(t, 0, result.ExitCode,
				"ARCH-1 violation: dispatch returned non-zero exit code for %s with analytics enabled", eventType)
		})
	}
}

// TestChaos_SafeDispatchRandomPanic verifies that SafeDispatch recovers
// from panics with various types and always returns exit 0.
func TestChaos_SafeDispatchRandomPanic(t *testing.T) {
	panicValues := []interface{}{
		"string panic",
		fmt.Errorf("error panic"),
		42,
		struct{ msg string }{"struct panic"},
		[]byte("bytes panic"),
		map[string]int{"panic": 1},
	}

	for i, panicVal := range panicValues {
		t.Run(fmt.Sprintf("panic_type_%d", i), func(t *testing.T) {
			pv := panicVal // capture
			result := core.SafeDispatch(func() core.DispatchResult {
				panic(pv)
			})
			assert.Equal(t, 0, result.ExitCode,
				"SafeDispatch should return exit 0 after panic with %T", pv)
		})
	}
}

// TestChaos_DispatchWithMalformedPayload verifies dispatch handles
// extreme/malformed payloads gracefully.
func TestChaos_DispatchWithMalformedPayload(t *testing.T) {
	config := core.GetDefaultConfig()
	config.Analytics.Enabled = false

	payloads := []core.HookPayload{
		{}, // completely empty
		{SessionID: "", ToolName: "", ToolInput: nil},
		{SessionID: "s1", ToolName: "Bash", ToolInput: map[string]interface{}{
			"deeply": map[string]interface{}{
				"nested": map[string]interface{}{
					"value": "deep",
				},
			},
		}},
		{SessionID: "s1", ToolName: "Bash", ToolInput: map[string]interface{}{
			"command": string(make([]byte, 10000)), // huge string
		}},
	}

	for i, payload := range payloads {
		t.Run(fmt.Sprintf("payload_%d", i), func(t *testing.T) {
			result := core.Dispatch(core.EventPreToolUse, payload, config)
			assert.Equal(t, 0, result.ExitCode,
				"ARCH-1: dispatch must handle malformed payload gracefully")
		})
	}
}

// =========================================================================
// Task 4.2: Cache and bridge resilience under corrupt data
// =========================================================================

// TestChaos_CorruptCacheSkipped verifies that CollectFeedCache skips
// corrupt JSON files and returns valid entries.
func TestChaos_CorruptCacheSkipped(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "feed-cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o700))

	// Write one valid cache file
	validData := map[string]interface{}{
		"type":      "pulse",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      map[string]interface{}{"count": float64(5)},
	}
	validJSON, err := json.MarshalIndent(validData, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "pulse.json"), validJSON, 0o600))

	// Write corrupt JSON files
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "corrupt1.json"), []byte("{{{invalid"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "corrupt2.json"), []byte(""), 0o600))
	// Write a binary-garbage file
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "corrupt3.json"), []byte{0xFF, 0xFE, 0x00, 0x01}, 0o600))

	// CollectFeedCache should skip corrupt entries
	collected, err := bridge.CollectFeedCache(cacheDir)
	require.NoError(t, err, "CollectFeedCache should not error on corrupt files")
	assert.Contains(t, collected, "pulse", "valid entry should be collected")

	// Verify the valid entry has the expected structure
	pulseEntry, ok := collected["pulse"].(map[string]interface{})
	require.True(t, ok, "pulse entry should be a map")
	assert.Equal(t, "pulse", pulseEntry["type"])

	// Corrupt files (invalid JSON, empty, binary) should all be skipped
	_, hasCorrupt1 := collected["corrupt1"]
	_, hasCorrupt2 := collected["corrupt2"]
	_, hasCorrupt3 := collected["corrupt3"]
	assert.False(t, hasCorrupt1, "corrupt1 (invalid JSON) should be skipped")
	assert.False(t, hasCorrupt2, "corrupt2 (empty) should be skipped")
	assert.False(t, hasCorrupt3, "corrupt3 (binary) should be skipped")
}

// TestChaos_EmptyCacheDir verifies bridge handles an empty cache directory.
func TestChaos_EmptyCacheDir(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "feed-cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o700))

	collected, err := bridge.CollectFeedCache(cacheDir)
	require.NoError(t, err)
	assert.Empty(t, collected)
}

// TestChaos_NonexistentCacheDir verifies bridge handles a missing directory.
func TestChaos_NonexistentCacheDir(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "does-not-exist")
	collected, err := bridge.CollectFeedCache(cacheDir)
	// CollectFeedCache returns empty map and no error for nonexistent dir
	assert.NoError(t, err)
	assert.Empty(t, collected)
}

// TestChaos_CacheDirWithNonJSONFiles verifies bridge ignores non-JSON files.
func TestChaos_CacheDirWithNonJSONFiles(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "feed-cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o700))

	// Write non-JSON files that should be ignored
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "readme.txt"), []byte("hello"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, ".hidden"), []byte("secret"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "subdir"), 0o700))

	// Write one valid JSON file
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "valid.json"), []byte(`{"ok":true}`), 0o600))

	collected, err := bridge.CollectFeedCache(cacheDir)
	require.NoError(t, err)
	assert.Len(t, collected, 1, "only .json files should be collected")
	assert.Contains(t, collected, "valid")
}

// =========================================================================
// Task 4.3: Daemon and dispatch resilience under panics
// =========================================================================

// panicProducer is a feed producer that panics during Produce().
type panicProducer struct {
	name     string
	panicMsg string
}

func (p *panicProducer) Name() string { return p.name }
func (p *panicProducer) Produce(_ context.Context) (interface{}, error) {
	panic(p.panicMsg)
}

// goodProducer is a feed producer that returns valid data.
type goodProducer struct {
	name string
	data map[string]interface{}
}

func (g *goodProducer) Name() string { return g.name }
func (g *goodProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      g.name,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      g.data,
	}, nil
}

// TestChaos_ProducerPanicRecovery verifies that when a producer panics,
// the daemon recovers and continues polling other producers.
func TestChaos_ProducerPanicRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmpDir)

	cacheDir := filepath.Join(tmpDir, "feed-cache")
	pidFile := filepath.Join(tmpDir, "test-daemon.pid")

	registry := feeds.NewRegistry()
	registry.Register(&panicProducer{name: "panicker", panicMsg: "chaos test panic"})
	registry.Register(&goodProducer{
		name: "survivor",
		data: map[string]interface{}{"alive": true},
	})

	daemon := feeds.NewDaemon(core.DaemonConfig{}, core.FeedsConfig{}, registry)
	daemon.SetPIDFile(pidFile)
	daemon.SetCacheDir(cacheDir)
	daemon.SetStaggerOffset(0)

	require.NoError(t, daemon.Start())
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(cacheDir, "survivor.json"))
		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "survivor.json was not written before timeout")
	require.NoError(t, daemon.Stop())

	// The good producer's cache file should exist
	survivorFile := filepath.Join(cacheDir, "survivor.json")
	assert.FileExists(t, survivorFile, "good producer should still write cache despite panicking sibling")

	// Read and verify the survivor's data
	data, err := os.ReadFile(survivorFile)
	require.NoError(t, err)
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.NotEmpty(t, parsed)

	// The panicker should NOT have a cache file
	panickerFile := filepath.Join(cacheDir, "panicker.json")
	_, err = os.Stat(panickerFile)
	assert.True(t, os.IsNotExist(err), "panicking producer should not have written a cache file")
}

// TestChaos_ConcurrentDispatchStress verifies no panics or data races
// when multiple goroutines dispatch simultaneously.
func TestChaos_ConcurrentDispatchStress(t *testing.T) {
	config := core.GetDefaultConfig()
	config.Analytics.Enabled = false
	config.Guards = []core.GuardRuleConfig{
		{Match: "Bash", Action: "warn", Reason: "concurrent test"},
		{Match: "mcp__*", Action: "confirm", Reason: "concurrent test"},
	}

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			toolName := "Bash"
			if idx%3 == 0 {
				toolName = "mcp__test__tool"
			}
			if idx%5 == 0 {
				toolName = "Write"
			}

			result := core.Dispatch(core.EventPreToolUse, core.HookPayload{
				SessionID: fmt.Sprintf("stress-%d", idx),
				ToolName:  toolName,
				ToolInput: map[string]interface{}{
					"command": fmt.Sprintf("test command %d", idx),
				},
			}, config)

			assert.Equal(t, 0, result.ExitCode)
		}(i)
	}

	wg.Wait()
}

// TestChaos_ConcurrentSafeDispatchWithPanics verifies SafeDispatch is safe
// to call concurrently even when some calls panic.
func TestChaos_ConcurrentSafeDispatchWithPanics(t *testing.T) {
	var wg sync.WaitGroup
	const goroutines = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result := core.SafeDispatch(func() core.DispatchResult {
				if idx%2 == 0 {
					panic(fmt.Sprintf("concurrent panic %d", idx))
				}
				return core.DispatchResult{ExitCode: 0}
			})
			assert.Equal(t, 0, result.ExitCode)
		}(i)
	}

	wg.Wait()
}

// TestChaos_DispatchUnrecognizedEventType verifies that unrecognized event
// types return exit 0 without error.
func TestChaos_DispatchUnrecognizedEventType(t *testing.T) {
	config := core.GetDefaultConfig()

	bogusEvents := []string{
		"",
		"FakeEvent",
		"pre_tool_use",
		"PRETOOLUSE",
		"PreToolUse\n",
		"PreToolUse ",
		string(make([]byte, 1000)),
	}

	for i, evt := range bogusEvents {
		t.Run(fmt.Sprintf("bogus_%d", i), func(t *testing.T) {
			result := core.Dispatch(evt, core.HookPayload{
				SessionID: "test",
				ToolName:  "Bash",
			}, config)
			assert.Equal(t, 0, result.ExitCode,
				"unrecognized event type should return exit 0")
			assert.Nil(t, result.Stdout,
				"unrecognized event type should produce no stdout")
		})
	}
}

// TestChaos_GuardEvaluationWithEdgeCasePatterns verifies guard evaluation
// doesn't panic on pathological glob patterns.
func TestChaos_GuardEvaluationWithEdgeCasePatterns(t *testing.T) {
	config := core.GetDefaultConfig()
	config.Analytics.Enabled = false

	edgeCaseGuards := []core.GuardRuleConfig{
		{Match: "****", Action: "warn", Reason: "excessive wildcards"},
		{Match: "", Action: "block", Reason: "empty match"},
		{Match: "a]b[c", Action: "warn", Reason: "malformed brackets"},
		{Match: string(make([]byte, 500)), Action: "warn", Reason: "huge pattern"},
	}

	for i, guard := range edgeCaseGuards {
		t.Run(fmt.Sprintf("guard_%d", i), func(t *testing.T) {
			cfg := config
			cfg.Guards = []core.GuardRuleConfig{guard}
			result := core.Dispatch(core.EventPreToolUse, core.HookPayload{
				SessionID: "test",
				ToolName:  "Bash",
				ToolInput: map[string]interface{}{"command": "ls"},
			}, cfg)
			assert.Equal(t, 0, result.ExitCode,
				"edge-case guard pattern should not cause non-zero exit")
		})
	}
}
