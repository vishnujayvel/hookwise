//go:build integration

// Package integration provides end-to-end tests that validate cross-package
// flows in the hookwise Go codebase. These tests exercise real interactions
// between core, analytics, feeds, bridge, and migration packages.
//
// Run with:
//
//	go test -tags integration -race ./internal/integration/...
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/bridge"
	"github.com/vishnujayvel/hookwise/internal/core"
	"github.com/vishnujayvel/hookwise/internal/feeds"
	"github.com/vishnujayvel/hookwise/internal/migration"
	_ "modernc.org/sqlite"
)

// =========================================================================
// Helpers
// =========================================================================

// openTestDolt creates a fresh Dolt database in a temp directory.
func openTestDolt(t *testing.T) (*analytics.DB, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := analytics.Open(dir)
	require.NoError(t, err)
	return db, func() {
		_ = db.Close()
	}
}

// mockProducer is a simple Producer that returns static data in the
// Go-envelope format expected by the daemon's cache writer.
type mockProducer struct {
	name string
	data map[string]interface{}
}

func (m *mockProducer) Name() string { return m.name }
func (m *mockProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      m.name,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      m.data,
	}, nil
}

// createMockSQLite builds a SQLite database with the TypeScript schema and
// test data at the given path.
func createMockSQLite(t *testing.T, dbPath string, sessions, events, authorship int) {
	t.Helper()

	sqliteDB, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer sqliteDB.Close()

	ddl := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			started_at TEXT NOT NULL,
			ended_at TEXT,
			total_tool_calls INTEGER DEFAULT 0,
			file_edits_count INTEGER DEFAULT 0,
			ai_authored_lines INTEGER DEFAULT 0,
			human_verified_lines INTEGER DEFAULT 0,
			estimated_cost_usd REAL DEFAULT 0.0
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			tool_name TEXT,
			timestamp TEXT NOT NULL,
			file_path TEXT,
			lines_added INTEGER DEFAULT 0,
			lines_removed INTEGER DEFAULT 0,
			ai_confidence_score REAL
		)`,
		`CREATE TABLE IF NOT EXISTS authorship_ledger (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			file_path TEXT NOT NULL,
			tool_name TEXT NOT NULL,
			lines_changed INTEGER NOT NULL,
			ai_score REAL NOT NULL,
			classification TEXT NOT NULL,
			timestamp TEXT NOT NULL
		)`,
	}
	for _, stmt := range ddl {
		_, err := sqliteDB.Exec(stmt)
		require.NoError(t, err)
	}

	for i := 0; i < sessions; i++ {
		_, err := sqliteDB.Exec(
			`INSERT INTO sessions (id, started_at, ended_at, total_tool_calls, file_edits_count,
			                       ai_authored_lines, human_verified_lines, estimated_cost_usd)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			fmtID("integ-sess", i),
			"2025-12-01T10:00:00Z",
			"2025-12-01T11:00:00Z",
			10+i, 3+i, 50+i*10, 20+i, 0.50+float64(i)*0.25,
		)
		require.NoError(t, err)
	}

	for i := 0; i < events; i++ {
		sessionID := fmtID("integ-sess", i%max(sessions, 1))
		_, err := sqliteDB.Exec(
			`INSERT INTO events (session_id, event_type, tool_name, timestamp, file_path,
			                     lines_added, lines_removed, ai_confidence_score)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			sessionID, "PostToolUse", "Write",
			"2025-12-01T10:30:00Z", "/src/main.go",
			15, 3, 0.85,
		)
		require.NoError(t, err)
	}

	for i := 0; i < authorship; i++ {
		sessionID := fmtID("integ-sess", i%max(sessions, 1))
		_, err := sqliteDB.Exec(
			`INSERT INTO authorship_ledger (session_id, file_path, tool_name, lines_changed,
			                                ai_score, classification, timestamp)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			sessionID, "/src/main.go", "Write",
			20+i, 0.85, "high_probability_ai",
			"2025-12-01T10:35:00Z",
		)
		require.NoError(t, err)
	}
}

func fmtID(prefix string, i int) string {
	return prefix + "-" + string(rune('A'+i))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// writeFeedFile writes a JSON file to dir/<name>.json.
func writeFeedFile(t *testing.T, dir, name string, data interface{}) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	content, err := json.MarshalIndent(data, "", "  ")
	require.NoError(t, err)
	content = append(content, '\n')
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".json"), content, 0o600))
}

// =========================================================================
// Test 1: Full dispatch flow — guards + config + Dolt writes
// =========================================================================
//
// This test exercises the full dispatch pipeline:
// - Load a config with guards
// - Dispatch a PreToolUse event
// - Verify guards evaluate correctly
// - Write analytics data to Dolt
// - Verify Dolt tables have the expected rows
// - Commit and verify the Dolt commit hash

func TestIntegration_FullDispatchWithDoltWrites(t *testing.T) {
	// Isolate state directory
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmpDir)

	// Open a fresh Dolt DB
	doltDB, cleanup := openTestDolt(t)
	defer cleanup()

	ctx := context.Background()

	// --- Phase A: Dispatch with guards ---
	config := core.GetDefaultConfig()
	config.Guards = []core.GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "Bash is blocked in integration test"},
		{Match: "Write", Action: "warn", Reason: "Write operations are warned"},
		{Match: "mcp__*", Action: "confirm", Reason: "MCP tools require confirmation"},
	}
	config.Analytics.Enabled = false // we control Dolt writes manually

	// Dispatch 1: blocked tool
	payload := core.HookPayload{
		SessionID: "integ-session-001",
		ToolName:  "Bash",
		ToolInput: map[string]interface{}{"command": "rm -rf /"},
	}
	result := core.Dispatch(core.EventPreToolUse, payload, config)
	assert.Equal(t, 0, result.ExitCode, "ARCH-1: blocked tool still returns exit 0")
	require.NotNil(t, result.Stdout)
	assert.Contains(t, *result.Stdout, "deny", "blocked tool should produce deny decision")
	assert.Contains(t, *result.Stdout, "Bash is blocked", "reason should appear in stdout")

	// Dispatch 2: warned tool
	payload.ToolName = "Write"
	result = core.Dispatch(core.EventPreToolUse, payload, config)
	assert.Equal(t, 0, result.ExitCode)
	// Warn adds context, does not block
	if result.Stdout != nil {
		assert.Contains(t, *result.Stdout, "Guard warning")
	}

	// Dispatch 3: unrecognized event type -> no output
	result = core.Dispatch("UnknownEvent", payload, config)
	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout, "unrecognized event should produce no stdout")

	// Dispatch 4: confirm tool
	payload.ToolName = "mcp__gmail__send"
	result = core.Dispatch(core.EventPreToolUse, payload, config)
	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout)
	assert.Contains(t, *result.Stdout, "ask", "confirm should produce ask decision")

	// --- Phase B: Analytics writes to Dolt ---
	a := analytics.NewAnalytics(doltDB)
	sessionID := "integ-session-001"
	startTime := time.Date(2025, 12, 1, 10, 0, 0, 0, time.UTC)

	require.NoError(t, a.StartSession(ctx, sessionID, startTime))

	// Record events
	for i := 0; i < 5; i++ {
		require.NoError(t, a.RecordEvent(ctx, sessionID, analytics.EventRecord{
			EventType:         "PostToolUse",
			ToolName:          "Write",
			Timestamp:         startTime.Add(time.Duration(i) * time.Minute),
			FilePath:          "/src/main.go",
			LinesAdded:        10 + i,
			LinesRemoved:      2,
			AIConfidenceScore: 0.85,
		}))
	}

	// End session
	endTime := startTime.Add(1 * time.Hour)
	require.NoError(t, a.EndSession(ctx, sessionID, endTime, analytics.SessionStats{
		TotalToolCalls:     5,
		FileEditsCount:     5,
		AIAuthoredLines:    60,
		HumanVerifiedLines: 20,
		EstimatedCostUSD:   0.75,
	}))

	// --- Phase C: Verify Dolt tables ---
	var sessionCount int
	err := doltDB.QueryRow(ctx, "SELECT COUNT(*) FROM sessions").Scan(&sessionCount)
	require.NoError(t, err)
	assert.Equal(t, 1, sessionCount, "should have exactly 1 session")

	var eventCount int
	err = doltDB.QueryRow(ctx, "SELECT COUNT(*) FROM events WHERE session_id = ?", sessionID).Scan(&eventCount)
	require.NoError(t, err)
	assert.Equal(t, 5, eventCount, "should have 5 events")

	// --- Phase D: Dolt commit ---
	hash, err := doltDB.CommitDispatch(ctx, "PostToolUse", sessionID)
	require.NoError(t, err)
	assert.NotEmpty(t, hash, "Dolt commit should produce a non-empty hash")

	// --- Phase E: Verify via Dolt log ---
	logs, err := doltDB.Log(ctx, 2)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(logs), 1)
	assert.Contains(t, logs[0].Message, "dispatch:PostToolUse")
	assert.Contains(t, logs[0].Message, sessionID)
}

// =========================================================================
// Test 2: Daemon lifecycle — producers write cache, bridge flattens
// =========================================================================
//
// This test validates the daemon -> cache -> bridge pipeline:
// - Start a daemon with mock producers
// - Wait for producers to write their JSON cache files
// - Stop the daemon
// - Run bridge.CollectFeedCache to collect the raw cache
// - Run bridge.FlattenForTUI to convert to TUI format
// - Validate the TUI format has updated_at/ttl_seconds/flattened fields

func TestIntegration_DaemonLifecycleWithCacheBridge(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmpDir)

	cacheDir := filepath.Join(tmpDir, "feed-cache")
	pidFile := filepath.Join(tmpDir, "test-daemon.pid")

	registry := feeds.NewRegistry()
	registry.Register(&mockProducer{
		name: "pulse",
		data: map[string]interface{}{
			"session_count":   3,
			"active_sessions": 1,
		},
	})
	registry.Register(&mockProducer{
		name: "weather",
		data: map[string]interface{}{
			"temperature": 72,
			"unit":        "F",
			"condition":   "sunny",
		},
	})

	daemon := feeds.NewDaemon(core.DaemonConfig{}, core.FeedsConfig{}, registry)
	daemon.SetPIDFile(pidFile)
	daemon.SetCacheDir(cacheDir)
	daemon.SetStaggerOffset(10 * time.Millisecond) // fast for tests

	// Start daemon
	require.NoError(t, daemon.Start())

	// Wait for producers to write cache files (initial run happens immediately)
	time.Sleep(500 * time.Millisecond)

	// Stop daemon
	require.NoError(t, daemon.Stop())

	// Verify cache files were written
	pulseFile := filepath.Join(cacheDir, "pulse.json")
	weatherFile := filepath.Join(cacheDir, "weather.json")
	assert.FileExists(t, pulseFile, "pulse cache file should exist")
	assert.FileExists(t, weatherFile, "weather cache file should exist")

	// Collect via bridge
	collected, err := bridge.CollectFeedCache(cacheDir)
	require.NoError(t, err)
	assert.Len(t, collected, 2)
	assert.Contains(t, collected, "pulse")
	assert.Contains(t, collected, "weather")

	// Validate Go-envelope format
	require.NoError(t, bridge.ValidateGoEnvelopeFormat(collected))

	// Flatten for TUI
	flattened := bridge.FlattenForTUI(collected)
	assert.Len(t, flattened, 2)

	// Validate TUI format
	require.NoError(t, bridge.ValidateCacheFormat(flattened))

	// Verify pulse entry is flattened correctly
	pulseEntry, ok := flattened["pulse"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, pulseEntry["updated_at"], "should have updated_at")
	assert.Equal(t, bridge.DefaultTTLSeconds, pulseEntry["ttl_seconds"], "should have ttl_seconds")
	assert.Equal(t, float64(3), pulseEntry["session_count"], "data field should be at top level")
	assert.Equal(t, float64(1), pulseEntry["active_sessions"], "data field should be at top level")
	assert.NotContains(t, pulseEntry, "type", "envelope field should not be present")
	assert.NotContains(t, pulseEntry, "data", "envelope 'data' key should not be present")
	assert.NotContains(t, pulseEntry, "timestamp", "envelope 'timestamp' should be renamed to updated_at")

	// Verify weather entry
	weatherEntry, ok := flattened["weather"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(72), weatherEntry["temperature"])
	assert.Equal(t, "F", weatherEntry["unit"])
	assert.Equal(t, "sunny", weatherEntry["condition"])
}

// =========================================================================
// Test 3: TUI bridge reads Go-written cache correctly
// =========================================================================
//
// This test validates that the full pipeline from Go-envelope cache files
// to WriteTUICacheTo produces a valid TUI-compatible JSON file.

func TestIntegration_TUIBridgeReadsGoWrittenCache(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmpDir)

	cacheDir := filepath.Join(tmpDir, "feed-cache")
	outPath := filepath.Join(tmpDir, "status-line-cache.json")

	// Simulate what the daemon produces: Go-envelope format files
	ts := time.Now().UTC().Format(time.RFC3339)
	feeds := []struct {
		name string
		data map[string]interface{}
	}{
		{"pulse", map[string]interface{}{"session_count": float64(5), "active_sessions": float64(2)}},
		{"project", map[string]interface{}{"name": "hookwise", "branch": "main", "last_commit": "abc1234"}},
		{"weather", map[string]interface{}{"temperature": float64(68), "unit": "F", "condition": "cloudy"}},
		{"news", map[string]interface{}{"stories": []interface{}{}, "source": "hackernews"}},
	}

	for _, f := range feeds {
		envelope := map[string]interface{}{
			"type":      f.name,
			"timestamp": ts,
			"data":      f.data,
		}
		writeFeedFile(t, cacheDir, f.name, envelope)
	}

	// Run the bridge
	require.NoError(t, bridge.WriteTUICacheTo(cacheDir, outPath))

	// Read and parse the output
	raw, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &parsed))
	assert.Len(t, parsed, 4, "should have 4 feed entries")

	// Validate each entry has the TUI format
	for _, f := range feeds {
		entry, ok := parsed[f.name].(map[string]interface{})
		require.True(t, ok, "entry %q should be a map", f.name)

		// Required TUI fields
		assert.NotEmpty(t, entry["updated_at"], "entry %q should have updated_at", f.name)
		assert.Equal(t, float64(bridge.DefaultTTLSeconds), entry["ttl_seconds"],
			"entry %q should have ttl_seconds", f.name)

		// Data fields should be at top level (not nested under "data")
		for k, v := range f.data {
			assert.Equal(t, v, entry[k],
				"entry %q field %q should be at top level with correct value", f.name, k)
		}

		// Envelope fields should NOT be present
		assert.NotContains(t, entry, "type", "entry %q should not have envelope 'type'", f.name)
		assert.NotContains(t, entry, "data", "entry %q should not have envelope 'data'", f.name)
		assert.NotContains(t, entry, "timestamp", "entry %q should not have envelope 'timestamp'", f.name)
	}

	// Validate via ValidateCacheFormat
	require.NoError(t, bridge.ValidateCacheFormat(parsed))
}

// =========================================================================
// Test 4: Upgrade from mock TypeScript data (dry-run)
// =========================================================================
//
// This test creates a mock TypeScript installation (SQLite + cost-state.json)
// and runs the migration in dry-run mode to validate detection and counting.

func TestIntegration_UpgradeFromMockTypeScript_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise-state"))

	hwDir := filepath.Join(tmpDir, ".hookwise")
	require.NoError(t, os.MkdirAll(filepath.Join(hwDir, "state"), 0o700))

	// Create mock SQLite analytics.db
	sqlitePath := filepath.Join(hwDir, "analytics.db")
	createMockSQLite(t, sqlitePath, 3, 8, 5)

	// Create mock cost-state.json
	costState := map[string]interface{}{
		"dailyCosts":   map[string]interface{}{"2025-12-01": 2.50},
		"sessionCosts": map[string]interface{}{"sess-001": 1.25},
		"today":        "2025-12-01",
		"totalToday":   2.50,
	}
	costJSON, err := json.MarshalIndent(costState, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(hwDir, "state", "cost-state.json"), costJSON, 0o644))

	var buf bytes.Buffer
	result := migration.Run(migration.MigrationOpts{
		HomeDir:    tmpDir,
		DryRun:     true,
		ProjectDir: tmpDir,
		Writer:     &buf,
	})

	// Verify detection
	assert.True(t, result.SQLiteDetected, "should detect SQLite")
	assert.True(t, result.CostStateDetected, "should detect cost-state.json")

	// Verify counts
	assert.Equal(t, 3, result.SessionsImported, "should count 3 sessions")
	assert.Equal(t, 8, result.EventsImported, "should count 8 events")
	assert.Equal(t, 5, result.AuthorshipImported, "should count 5 authorship entries")
	assert.True(t, result.CostStateImported, "should import cost state in dry-run")

	// Verify dry-run output
	output := buf.String()
	assert.Contains(t, output, "dry-run")
	assert.Contains(t, output, "3 sessions")
	assert.Contains(t, output, "8 events")
	assert.Contains(t, output, "5 authorship")
	assert.Empty(t, result.Errors, "should have no errors")
}

// =========================================================================
// Test 5: Upgrade from mock TypeScript data (live migration with Dolt)
// =========================================================================
//
// This test runs a full live migration from mock TypeScript data into Dolt,
// then verifies the data was written correctly and a Dolt commit was made.

func TestIntegration_UpgradeFromMockTypeScript_Live(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise-state"))

	hwDir := filepath.Join(tmpDir, ".hookwise")
	require.NoError(t, os.MkdirAll(filepath.Join(hwDir, "state"), 0o700))

	// Create mock SQLite
	sqlitePath := filepath.Join(hwDir, "analytics.db")
	createMockSQLite(t, sqlitePath, 2, 4, 3)

	// Create mock cost-state.json
	costState := map[string]interface{}{
		"dailyCosts":   map[string]interface{}{"2025-12-01": 1.75},
		"sessionCosts": map[string]interface{}{"integ-sess-A": 0.50},
		"today":        "2025-12-01",
		"totalToday":   1.75,
	}
	costJSON, err := json.MarshalIndent(costState, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(hwDir, "state", "cost-state.json"), costJSON, 0o644))

	// Create hookwise.yaml so config validation passes
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"),
		[]byte("version: 1\nguards: []\n"), 0o644))

	doltDir := filepath.Join(tmpDir, "dolt-data")

	var buf bytes.Buffer
	result := migration.Run(migration.MigrationOpts{
		HomeDir:     tmpDir,
		DryRun:      false,
		DoltDataDir: doltDir,
		ProjectDir:  tmpDir,
		Writer:      &buf,
	})

	assert.True(t, result.SQLiteDetected)
	assert.True(t, result.CostStateDetected)
	assert.Equal(t, 2, result.SessionsImported)
	assert.Equal(t, 4, result.EventsImported)
	assert.Equal(t, 3, result.AuthorshipImported)
	assert.True(t, result.CostStateImported)
	assert.True(t, result.ConfigValid)
	assert.Empty(t, result.Errors)
	assert.NotEmpty(t, result.DoltCommitHash, "live migration should produce a Dolt commit")

	output := buf.String()
	assert.Contains(t, output, "Dolt commit:")
	assert.Contains(t, output, "hookwise upgrade")
}

// =========================================================================
// Test 6: Dispatch fail-open guarantee (ARCH-1)
// =========================================================================
//
// Verifies that dispatch never returns a non-zero exit code for unrecognized
// events, empty configs, or any normal scenario.

func TestIntegration_DispatchFailOpenGuarantee(t *testing.T) {
	config := core.GetDefaultConfig()
	config.Analytics.Enabled = false

	testCases := []struct {
		name      string
		eventType string
		payload   core.HookPayload
	}{
		{
			name:      "unrecognized event type",
			eventType: "TotallyFakeEvent",
			payload:   core.HookPayload{SessionID: "s1"},
		},
		{
			name:      "PreToolUse with no guards",
			eventType: core.EventPreToolUse,
			payload:   core.HookPayload{SessionID: "s1", ToolName: "Bash"},
		},
		{
			name:      "PostToolUse",
			eventType: core.EventPostToolUse,
			payload:   core.HookPayload{SessionID: "s1", ToolName: "Write"},
		},
		{
			name:      "SessionStart",
			eventType: core.EventSessionStart,
			payload:   core.HookPayload{SessionID: "s1"},
		},
		{
			name:      "Stop event",
			eventType: core.EventStop,
			payload:   core.HookPayload{SessionID: "s1"},
		},
		{
			name:      "empty session ID",
			eventType: core.EventPreToolUse,
			payload:   core.HookPayload{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := core.Dispatch(tc.eventType, tc.payload, config)
			assert.Equal(t, 0, result.ExitCode,
				"ARCH-1: dispatch must never return non-zero exit code for %q", tc.name)
		})
	}
}

// =========================================================================
// Test 7: Guard + handler integration — inline handler with context
// =========================================================================
//
// Tests that guards and inline handlers work together in a dispatch:
// guards evaluate first, then inline handlers provide context.

func TestIntegration_GuardsAndInlineHandlers(t *testing.T) {
	config := core.GetDefaultConfig()
	config.Analytics.Enabled = false

	// Guard: warn on Write
	config.Guards = []core.GuardRuleConfig{
		{Match: "Write", Action: "warn", Reason: "Write operations need review"},
	}

	// Inline handler: add context on SessionStart
	config.Handlers = []core.CustomHandlerConfig{
		{
			Name:   "session-greeting",
			Type:   "inline",
			Events: []string{core.EventSessionStart},
			Phase:  "context",
			Action: map[string]interface{}{
				"additionalContext": "Welcome to hookwise session",
			},
		},
	}

	// Test 1: PreToolUse with Write -> guard warns
	result := core.Dispatch(core.EventPreToolUse, core.HookPayload{
		SessionID: "s1",
		ToolName:  "Write",
	}, config)
	assert.Equal(t, 0, result.ExitCode)
	if result.Stdout != nil {
		assert.Contains(t, *result.Stdout, "Guard warning")
	}

	// Test 2: SessionStart -> inline handler provides context
	result = core.Dispatch(core.EventSessionStart, core.HookPayload{
		SessionID: "s1",
	}, config)
	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout)
	assert.Contains(t, *result.Stdout, "Welcome to hookwise session")
}

// =========================================================================
// Test 8: Daemon ARCH-3 compliance — writes JSON cache only, NOT Dolt
// =========================================================================
//
// Validates that the daemon writes JSON cache files but does NOT write to
// any Dolt database. This is the fundamental ARCH-3 constraint.

func TestIntegration_DaemonWritesCacheNotDolt(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmpDir)

	cacheDir := filepath.Join(tmpDir, "feed-cache")
	pidFile := filepath.Join(tmpDir, "test-daemon.pid")

	registry := feeds.NewRegistry()
	registry.Register(&mockProducer{
		name: "pulse",
		data: map[string]interface{}{"count": 1},
	})

	daemon := feeds.NewDaemon(core.DaemonConfig{}, core.FeedsConfig{}, registry)
	daemon.SetPIDFile(pidFile)
	daemon.SetCacheDir(cacheDir)
	daemon.SetStaggerOffset(0)

	require.NoError(t, daemon.Start())
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, daemon.Stop())

	// Cache file should exist (JSON)
	cacheFile := filepath.Join(cacheDir, "pulse.json")
	assert.FileExists(t, cacheFile)

	data, err := os.ReadFile(cacheFile)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.NotEmpty(t, parsed, "cache file should contain data")

	// No Dolt database should have been created by the daemon
	doltDir := filepath.Join(tmpDir, "dolt")
	_, err = os.Stat(doltDir)
	assert.True(t, os.IsNotExist(err), "ARCH-3: daemon should not create a Dolt database")
}

// =========================================================================
// Test 9: Analytics roundtrip — write + read + stats
// =========================================================================
//
// Validates the full analytics lifecycle: start session, record events,
// record authorship, end session, query daily summary and tool breakdown.

func TestIntegration_AnalyticsRoundtrip(t *testing.T) {
	doltDB, cleanup := openTestDolt(t)
	defer cleanup()

	ctx := context.Background()
	a := analytics.NewAnalytics(doltDB)
	sessionID := "analytics-roundtrip-001"
	baseTime := time.Date(2025, 12, 15, 10, 0, 0, 0, time.UTC)

	// Start session
	require.NoError(t, a.StartSession(ctx, sessionID, baseTime))

	// Record various tool events
	tools := []string{"Write", "Read", "Bash", "Write", "Write", "Read"}
	for i, tool := range tools {
		require.NoError(t, a.RecordEvent(ctx, sessionID, analytics.EventRecord{
			EventType:         "PostToolUse",
			ToolName:          tool,
			Timestamp:         baseTime.Add(time.Duration(i) * time.Minute),
			FilePath:          "/src/file.go",
			LinesAdded:        10 + i,
			LinesRemoved:      2,
			AIConfidenceScore: 0.8,
		}))
	}

	// Record authorship
	require.NoError(t, a.RecordAuthorship(ctx, analytics.AuthorshipEntry{
		SessionID:    sessionID,
		FilePath:     "/src/file.go",
		ToolName:     "Write",
		LinesChanged: 50,
		AIScore:      0.85,
		Timestamp:    baseTime.Add(5 * time.Minute),
	}))

	// End session
	require.NoError(t, a.EndSession(ctx, sessionID, baseTime.Add(1*time.Hour), analytics.SessionStats{
		TotalToolCalls:     6,
		FileEditsCount:     3,
		AIAuthoredLines:    50,
		HumanVerifiedLines: 20,
		EstimatedCostUSD:   0.65,
	}))

	// Query daily summary
	date := "2025-12-15"
	summary, err := a.DailySummary(ctx, date)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.TotalSessions)
	assert.Equal(t, 6, summary.TotalEvents)
	assert.Equal(t, 6, summary.TotalToolCalls)
	assert.Equal(t, 50, summary.AIAuthoredLines)

	// Query tool breakdown
	breakdown, err := a.ToolBreakdown(ctx, date)
	require.NoError(t, err)
	require.NotEmpty(t, breakdown)

	// Write should be most frequent (3 times)
	assert.Equal(t, "Write", breakdown[0].ToolName)
	assert.Equal(t, 3, breakdown[0].Count)

	// Commit
	hash, err := doltDB.CommitDispatch(ctx, "PostToolUse", sessionID)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	// Verify authorship classification
	var classification string
	err = doltDB.QueryRow(ctx,
		"SELECT classification FROM authorship_ledger WHERE session_id = ?", sessionID,
	).Scan(&classification)
	require.NoError(t, err)
	assert.Equal(t, "high_probability_ai", classification, "0.85 AI score should classify as high_probability_ai")
}

// =========================================================================
// Test 10: Config loading + guard evaluation cross-package integration
// =========================================================================
//
// Tests that a config loaded from a YAML file produces guards that evaluate
// correctly in the dispatch pipeline.

func TestIntegration_ConfigLoadAndGuardDispatch(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise-state"))

	// Write a hookwise.yaml config
	configYAML := `version: 1
guards:
  - match: "Bash"
    action: block
    reason: "Bash is blocked for safety"
    when: 'tool_input.command contains "rm"'
  - match: "Bash"
    action: warn
    reason: "Bash usage noted"
  - match: "mcp__*"
    action: confirm
    reason: "External tool access"
analytics:
  enabled: false
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(configYAML), 0o644))

	// Load config
	config, err := core.LoadConfig(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, 1, config.Version)
	assert.Len(t, config.Guards, 3)

	// Dispatch with "rm" command -> block (first rule matches)
	result := core.Dispatch(core.EventPreToolUse, core.HookPayload{
		SessionID: "s1",
		ToolName:  "Bash",
		ToolInput: map[string]interface{}{"command": "rm -rf /tmp"},
	}, config)
	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout)
	assert.Contains(t, *result.Stdout, "deny")
	assert.Contains(t, *result.Stdout, "Bash is blocked for safety")

	// Dispatch with "ls" command -> warn (first rule skipped due to when, second matches)
	result = core.Dispatch(core.EventPreToolUse, core.HookPayload{
		SessionID: "s1",
		ToolName:  "Bash",
		ToolInput: map[string]interface{}{"command": "ls -la"},
	}, config)
	assert.Equal(t, 0, result.ExitCode)
	// Warn rule adds context
	if result.Stdout != nil {
		assert.Contains(t, *result.Stdout, "Guard warning")
		assert.Contains(t, *result.Stdout, "Bash usage noted")
	}

	// Dispatch MCP tool -> confirm
	result = core.Dispatch(core.EventPreToolUse, core.HookPayload{
		SessionID: "s1",
		ToolName:  "mcp__slack__send_message",
	}, config)
	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout)
	assert.Contains(t, *result.Stdout, "ask")
}
