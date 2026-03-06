package migration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testDoltDB opens a fresh Dolt DB in a temp directory for testing.
func testDoltDB(t *testing.T) (*analytics.DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "hookwise-migration-dolt-*")
	require.NoError(t, err)

	db, err := analytics.Open(tmpDir)
	require.NoError(t, err)

	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return db, cleanup
}

// createTestSQLite creates a SQLite database at the given path with the
// TypeScript schema and optional test data.
func createTestSQLite(t *testing.T, dbPath string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	// Create the TypeScript-style schema.
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
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}

	return db
}

// seedSQLiteData inserts test data into a SQLite database.
func seedSQLiteData(t *testing.T, db *sql.DB, sessionCount, eventCount, authorshipCount int) {
	t.Helper()

	for i := 0; i < sessionCount; i++ {
		_, err := db.Exec(
			`INSERT INTO sessions (id, started_at, ended_at, total_tool_calls, file_edits_count,
			                       ai_authored_lines, human_verified_lines, estimated_cost_usd)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			fmtID("sess", i),
			"2025-03-06T10:00:00Z",
			"2025-03-06T11:00:00Z",
			10+i, 3+i, 50+i*10, 20+i, 0.50+float64(i)*0.25,
		)
		require.NoError(t, err)
	}

	for i := 0; i < eventCount; i++ {
		sessionID := fmtID("sess", i%sessionCount)
		_, err := db.Exec(
			`INSERT INTO events (session_id, event_type, tool_name, timestamp, file_path,
			                     lines_added, lines_removed, ai_confidence_score)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			sessionID, "PostToolUse", "Write",
			"2025-03-06T10:30:00Z",
			"/src/file.go",
			15, 3, 0.85,
		)
		require.NoError(t, err)
	}

	for i := 0; i < authorshipCount; i++ {
		sessionID := fmtID("sess", i%sessionCount)
		_, err := db.Exec(
			`INSERT INTO authorship_ledger (session_id, file_path, tool_name, lines_changed,
			                                ai_score, classification, timestamp)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			sessionID, "/src/file.go", "Write",
			20+i, 0.85, "high_probability_ai",
			"2025-03-06T10:35:00Z",
		)
		require.NoError(t, err)
	}
}

func fmtID(prefix string, i int) string {
	return prefix + "-" + string(rune('A'+i))
}

// createCostStateJSON writes a cost-state.json file at the given path.
func createCostStateJSON(t *testing.T, dir string) string {
	t.Helper()

	stateDir := filepath.Join(dir, "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))

	costState := CostStateJSON{
		DailyCosts: map[string]float64{
			"2025-03-05": 1.25,
			"2025-03-06": 3.50,
		},
		SessionCosts: map[string]float64{
			"sess-001": 0.75,
			"sess-002": 2.10,
		},
		Today:      "2025-03-06",
		TotalToday: 3.50,
	}

	data, err := json.MarshalIndent(costState, "", "  ")
	require.NoError(t, err)

	path := filepath.Join(stateDir, "cost-state.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	return path
}

// ---------------------------------------------------------------------------
// Test 1: DetectTypeScript finds SQLite and cost-state.json
// ---------------------------------------------------------------------------

func TestDetectTypeScript_BothFound(t *testing.T) {
	tmpDir := t.TempDir()
	hwDir := filepath.Join(tmpDir, ".hookwise")
	require.NoError(t, os.MkdirAll(filepath.Join(hwDir, "state"), 0o700))

	// Create analytics.db.
	dbPath := filepath.Join(hwDir, "analytics.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("fake"), 0o644))

	// Create cost-state.json.
	costPath := filepath.Join(hwDir, "state", "cost-state.json")
	require.NoError(t, os.WriteFile(costPath, []byte("{}"), 0o644))

	opts := MigrationOpts{HomeDir: tmpDir}
	sqlitePath, costStatePath := DetectTypeScript(opts)

	assert.Equal(t, dbPath, sqlitePath)
	assert.Equal(t, costPath, costStatePath)
}

// ---------------------------------------------------------------------------
// Test 2: DetectTypeScript when nothing exists
// ---------------------------------------------------------------------------

func TestDetectTypeScript_NothingFound(t *testing.T) {
	tmpDir := t.TempDir()

	opts := MigrationOpts{HomeDir: tmpDir}
	sqlitePath, costStatePath := DetectTypeScript(opts)

	assert.Empty(t, sqlitePath)
	assert.Empty(t, costStatePath)
}

// ---------------------------------------------------------------------------
// Test 3: DetectTypeScript with only SQLite
// ---------------------------------------------------------------------------

func TestDetectTypeScript_OnlySQLite(t *testing.T) {
	tmpDir := t.TempDir()
	hwDir := filepath.Join(tmpDir, ".hookwise")
	require.NoError(t, os.MkdirAll(hwDir, 0o700))

	dbPath := filepath.Join(hwDir, "analytics.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("fake"), 0o644))

	opts := MigrationOpts{HomeDir: tmpDir}
	sqlitePath, costStatePath := DetectTypeScript(opts)

	assert.Equal(t, dbPath, sqlitePath)
	assert.Empty(t, costStatePath)
}

// ---------------------------------------------------------------------------
// Test 4: MigrateSQLite imports sessions
// ---------------------------------------------------------------------------

func TestMigrateSQLite_ImportsSessions(t *testing.T) {
	doltDB, cleanup := testDoltDB(t)
	defer cleanup()

	tmpDir := t.TempDir()
	sqlitePath := filepath.Join(tmpDir, "analytics.db")
	sqliteDB := createTestSQLite(t, sqlitePath)
	seedSQLiteData(t, sqliteDB, 3, 0, 0)
	sqliteDB.Close()

	ctx := context.Background()
	var buf bytes.Buffer
	sessions, events, authorship, errs := MigrateSQLite(ctx, doltDB, sqlitePath, false, &buf)

	assert.Empty(t, errs)
	assert.Equal(t, 3, sessions)
	assert.Equal(t, 0, events)
	assert.Equal(t, 0, authorship)

	// Verify data in Dolt.
	var count int
	err := doltDB.QueryRow(ctx, "SELECT COUNT(*) FROM sessions").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	assert.Contains(t, buf.String(), "3 rows imported")
}

// ---------------------------------------------------------------------------
// Test 5: MigrateSQLite imports events
// ---------------------------------------------------------------------------

func TestMigrateSQLite_ImportsEvents(t *testing.T) {
	doltDB, cleanup := testDoltDB(t)
	defer cleanup()

	tmpDir := t.TempDir()
	sqlitePath := filepath.Join(tmpDir, "analytics.db")
	sqliteDB := createTestSQLite(t, sqlitePath)
	seedSQLiteData(t, sqliteDB, 2, 5, 0)
	sqliteDB.Close()

	ctx := context.Background()
	var buf bytes.Buffer
	sessions, events, _, errs := MigrateSQLite(ctx, doltDB, sqlitePath, false, &buf)

	assert.Empty(t, errs)
	assert.Equal(t, 2, sessions)
	assert.Equal(t, 5, events)

	// Verify event count in Dolt.
	var count int
	err := doltDB.QueryRow(ctx, "SELECT COUNT(*) FROM events").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 5, count)
}

// ---------------------------------------------------------------------------
// Test 6: MigrateSQLite imports authorship
// ---------------------------------------------------------------------------

func TestMigrateSQLite_ImportsAuthorship(t *testing.T) {
	doltDB, cleanup := testDoltDB(t)
	defer cleanup()

	tmpDir := t.TempDir()
	sqlitePath := filepath.Join(tmpDir, "analytics.db")
	sqliteDB := createTestSQLite(t, sqlitePath)
	seedSQLiteData(t, sqliteDB, 2, 0, 4)
	sqliteDB.Close()

	ctx := context.Background()
	var buf bytes.Buffer
	_, _, authorship, errs := MigrateSQLite(ctx, doltDB, sqlitePath, false, &buf)

	assert.Empty(t, errs)
	assert.Equal(t, 4, authorship)

	// Verify in Dolt.
	var count int
	err := doltDB.QueryRow(ctx, "SELECT COUNT(*) FROM authorship_ledger").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 4, count)
}

// ---------------------------------------------------------------------------
// Test 7: MigrateSQLite dry-run does not write
// ---------------------------------------------------------------------------

func TestMigrateSQLite_DryRunDoesNotWrite(t *testing.T) {
	tmpDir := t.TempDir()
	sqlitePath := filepath.Join(tmpDir, "analytics.db")
	sqliteDB := createTestSQLite(t, sqlitePath)
	seedSQLiteData(t, sqliteDB, 3, 5, 4)
	sqliteDB.Close()

	ctx := context.Background()
	var buf bytes.Buffer
	// Pass nil for Dolt DB -- dry-run should not attempt writes.
	sessions, events, authorship, errs := MigrateSQLite(ctx, nil, sqlitePath, true, &buf)

	assert.Empty(t, errs)
	assert.Equal(t, 3, sessions)
	assert.Equal(t, 5, events)
	assert.Equal(t, 4, authorship)

	output := buf.String()
	assert.Contains(t, output, "[dry-run]")
	assert.Contains(t, output, "3 sessions")
	assert.Contains(t, output, "5 events")
	assert.Contains(t, output, "4 authorship")
}

// ---------------------------------------------------------------------------
// Test 8: MigrateCostState imports cost data
// ---------------------------------------------------------------------------

func TestMigrateCostState_ImportsData(t *testing.T) {
	doltDB, cleanup := testDoltDB(t)
	defer cleanup()

	tmpDir := t.TempDir()
	costPath := createCostStateJSON(t, filepath.Join(tmpDir, ".hookwise"))

	ctx := context.Background()
	var buf bytes.Buffer
	imported, errs := MigrateCostState(ctx, doltDB, costPath, false, &buf)

	assert.Empty(t, errs)
	assert.True(t, imported)

	// Verify in Dolt.
	state, err := doltDB.ReadCostState(ctx)
	require.NoError(t, err)
	assert.InDelta(t, 1.25, state.DailyCosts["2025-03-05"], 0.01)
	assert.InDelta(t, 3.50, state.DailyCosts["2025-03-06"], 0.01)
	assert.InDelta(t, 0.75, state.SessionCosts["sess-001"], 0.01)

	assert.Contains(t, buf.String(), "cost state")
}

// ---------------------------------------------------------------------------
// Test 9: MigrateCostState dry-run does not write
// ---------------------------------------------------------------------------

func TestMigrateCostState_DryRunDoesNotWrite(t *testing.T) {
	tmpDir := t.TempDir()
	costPath := createCostStateJSON(t, filepath.Join(tmpDir, ".hookwise"))

	ctx := context.Background()
	var buf bytes.Buffer
	imported, errs := MigrateCostState(ctx, nil, costPath, true, &buf)

	assert.Empty(t, errs)
	assert.True(t, imported)
	assert.Contains(t, buf.String(), "[dry-run]")
	assert.Contains(t, buf.String(), "today=2025-03-06")
}

// ---------------------------------------------------------------------------
// Test 10: MigrateCostState with invalid JSON
// ---------------------------------------------------------------------------

func TestMigrateCostState_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "cost-state.json")
	require.NoError(t, os.WriteFile(invalidPath, []byte("not json"), 0o644))

	ctx := context.Background()
	var buf bytes.Buffer
	imported, errs := MigrateCostState(ctx, nil, invalidPath, false, &buf)

	assert.False(t, imported)
	assert.NotEmpty(t, errs)
	assert.Contains(t, errs[0], "parsing cost-state.json")
}

// ---------------------------------------------------------------------------
// Test 11: MigrateCostState with missing file
// ---------------------------------------------------------------------------

func TestMigrateCostState_MissingFile(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	imported, errs := MigrateCostState(ctx, nil, "/nonexistent/cost-state.json", false, &buf)

	assert.False(t, imported)
	assert.NotEmpty(t, errs)
	assert.Contains(t, errs[0], "reading")
}

// ---------------------------------------------------------------------------
// Test 12: ValidateConfig with valid config
// ---------------------------------------------------------------------------

func TestValidateConfig_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise-state"))

	configPath := filepath.Join(tmpDir, "hookwise.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("version: 1\nguards: []\nanalytics:\n  enabled: true\n"), 0o644))

	var buf bytes.Buffer
	valid, errs := ValidateConfig(tmpDir, &buf)

	assert.True(t, valid)
	assert.Empty(t, errs)
	assert.Contains(t, buf.String(), "parsed successfully")
	assert.Contains(t, buf.String(), "version=1")
}

// ---------------------------------------------------------------------------
// Test 13: ValidateConfig with no config file (not an error)
// ---------------------------------------------------------------------------

func TestValidateConfig_NoConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise-state"))

	var buf bytes.Buffer
	valid, errs := ValidateConfig(tmpDir, &buf)

	assert.True(t, valid)
	assert.Empty(t, errs)
	assert.Contains(t, buf.String(), "not found")
}

// ---------------------------------------------------------------------------
// Test 14: Run with nothing to migrate
// ---------------------------------------------------------------------------

func TestRun_NothingToMigrate(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise-state"))

	var buf bytes.Buffer
	result := Run(MigrationOpts{
		HomeDir: tmpDir,
		DryRun:  true,
		Writer:  &buf,
	})

	assert.False(t, result.SQLiteDetected)
	assert.False(t, result.CostStateDetected)
	assert.Equal(t, 0, result.SessionsImported)
	assert.True(t, result.ConfigValid)
	assert.Contains(t, buf.String(), "Nothing to migrate")
}

// ---------------------------------------------------------------------------
// Test 15: Run dry-run with SQLite and cost state
// ---------------------------------------------------------------------------

func TestRun_DryRunFull(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise-state"))

	hwDir := filepath.Join(tmpDir, ".hookwise")
	require.NoError(t, os.MkdirAll(filepath.Join(hwDir, "state"), 0o700))

	// Create SQLite with data.
	sqlitePath := filepath.Join(hwDir, "analytics.db")
	sqliteDB := createTestSQLite(t, sqlitePath)
	seedSQLiteData(t, sqliteDB, 2, 4, 3)
	sqliteDB.Close()

	// Create cost-state.json.
	createCostStateJSON(t, hwDir)

	var buf bytes.Buffer
	result := Run(MigrationOpts{
		HomeDir:    tmpDir,
		DryRun:     true,
		ProjectDir: tmpDir,
		Writer:     &buf,
	})

	assert.True(t, result.SQLiteDetected)
	assert.True(t, result.CostStateDetected)
	assert.Equal(t, 2, result.SessionsImported)
	assert.Equal(t, 4, result.EventsImported)
	assert.Equal(t, 3, result.AuthorshipImported)
	assert.True(t, result.CostStateImported)

	output := buf.String()
	assert.Contains(t, output, "dry-run")
	assert.Contains(t, output, "2 sessions")
	assert.Contains(t, output, "4 events")
	assert.Contains(t, output, "3 authorship")
	assert.Empty(t, result.Errors)
}

// ---------------------------------------------------------------------------
// Test 16: Run live migration with Dolt commit
// ---------------------------------------------------------------------------

func TestRun_LiveMigration(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise-state"))

	hwDir := filepath.Join(tmpDir, ".hookwise")
	require.NoError(t, os.MkdirAll(filepath.Join(hwDir, "state"), 0o700))

	// Create SQLite.
	sqlitePath := filepath.Join(hwDir, "analytics.db")
	sqliteDB := createTestSQLite(t, sqlitePath)
	seedSQLiteData(t, sqliteDB, 2, 3, 2)
	sqliteDB.Close()

	// Create cost-state.json.
	createCostStateJSON(t, hwDir)

	// Create hookwise.yaml so config validation passes.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"),
		[]byte("version: 1\nguards: []\n"), 0o644))

	doltDir := filepath.Join(tmpDir, "dolt-data")

	var buf bytes.Buffer
	result := Run(MigrationOpts{
		HomeDir:     tmpDir,
		DryRun:      false,
		DoltDataDir: doltDir,
		ProjectDir:  tmpDir,
		Writer:      &buf,
	})

	assert.True(t, result.SQLiteDetected)
	assert.True(t, result.CostStateDetected)
	assert.Equal(t, 2, result.SessionsImported)
	assert.Equal(t, 3, result.EventsImported)
	assert.Equal(t, 2, result.AuthorshipImported)
	assert.True(t, result.CostStateImported)
	assert.True(t, result.ConfigValid)
	assert.Empty(t, result.Errors)
	assert.NotEmpty(t, result.DoltCommitHash)

	output := buf.String()
	assert.Contains(t, output, "Dolt commit:")
	assert.Contains(t, output, "hookwise upgrade")
}

// ---------------------------------------------------------------------------
// Test 17: Original SQLite file is not modified
// ---------------------------------------------------------------------------

func TestMigrateSQLite_OriginalUntouched(t *testing.T) {
	doltDB, cleanup := testDoltDB(t)
	defer cleanup()

	tmpDir := t.TempDir()
	sqlitePath := filepath.Join(tmpDir, "analytics.db")
	sqliteDB := createTestSQLite(t, sqlitePath)
	seedSQLiteData(t, sqliteDB, 2, 3, 1)
	sqliteDB.Close()

	// Record file modification time before migration.
	infoBefore, err := os.Stat(sqlitePath)
	require.NoError(t, err)
	modTimeBefore := infoBefore.ModTime()
	sizeBefore := infoBefore.Size()

	ctx := context.Background()
	var buf bytes.Buffer
	_, _, _, errs := MigrateSQLite(ctx, doltDB, sqlitePath, false, &buf)
	assert.Empty(t, errs)

	// File should not have been modified.
	infoAfter, err := os.Stat(sqlitePath)
	require.NoError(t, err)
	assert.Equal(t, modTimeBefore, infoAfter.ModTime(), "SQLite file mod time should be unchanged")
	assert.Equal(t, sizeBefore, infoAfter.Size(), "SQLite file size should be unchanged")
}

// ---------------------------------------------------------------------------
// Test 18: Original cost-state.json is not modified
// ---------------------------------------------------------------------------

func TestMigrateCostState_OriginalUntouched(t *testing.T) {
	doltDB, cleanup := testDoltDB(t)
	defer cleanup()

	tmpDir := t.TempDir()
	costPath := createCostStateJSON(t, filepath.Join(tmpDir, ".hookwise"))

	// Record before.
	dataBefore, err := os.ReadFile(costPath)
	require.NoError(t, err)

	ctx := context.Background()
	var buf bytes.Buffer
	_, errs := MigrateCostState(ctx, doltDB, costPath, false, &buf)
	assert.Empty(t, errs)

	// File should be identical.
	dataAfter, err := os.ReadFile(costPath)
	require.NoError(t, err)
	assert.Equal(t, dataBefore, dataAfter, "cost-state.json should not be modified")
}

// ---------------------------------------------------------------------------
// Test 19: MigrateSQLite with missing table gracefully handles error
// ---------------------------------------------------------------------------

func TestMigrateSQLite_MissingTable(t *testing.T) {
	tmpDir := t.TempDir()
	sqlitePath := filepath.Join(tmpDir, "empty.db")

	// Create empty SQLite DB with no tables.
	db, err := sql.Open("sqlite", sqlitePath)
	require.NoError(t, err)
	db.Close()

	ctx := context.Background()
	var buf bytes.Buffer
	sessions, events, authorship, errs := MigrateSQLite(ctx, nil, sqlitePath, true, &buf)

	// Should report errors for missing tables but not panic.
	assert.NotEmpty(t, errs)
	assert.Equal(t, 0, sessions)
	assert.Equal(t, 0, events)
	assert.Equal(t, 0, authorship)
}

// ---------------------------------------------------------------------------
// Test 20: parseTimeFlexible handles multiple formats
// ---------------------------------------------------------------------------

func TestParseTimeFlexible(t *testing.T) {
	tests := []struct {
		input    string
		expected string // expected date portion
	}{
		{"2025-03-06T10:00:00Z", "2025-03-06"},
		{"2025-03-06T10:00:00+00:00", "2025-03-06"},
		{"2025-03-06 10:00:00", "2025-03-06"},
		{"2025-03-06", "2025-03-06"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := parseTimeFlexible(tc.input)
			assert.Contains(t, result.Format("2006-01-02"), tc.expected)
		})
	}
}

// ---------------------------------------------------------------------------
// Test 21: CostStateJSON with nil maps
// ---------------------------------------------------------------------------

func TestMigrateCostState_NilMapsInJSON(t *testing.T) {
	doltDB, cleanup := testDoltDB(t)
	defer cleanup()

	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "cost-state.json")

	// Write JSON with null/missing maps.
	data := `{"today": "2025-03-06", "totalToday": 1.50}`
	require.NoError(t, os.WriteFile(jsonPath, []byte(data), 0o644))

	ctx := context.Background()
	var buf bytes.Buffer
	imported, errs := MigrateCostState(ctx, doltDB, jsonPath, false, &buf)

	assert.Empty(t, errs)
	assert.True(t, imported)

	// Verify maps are initialized to empty (not nil).
	state, err := doltDB.ReadCostState(ctx)
	require.NoError(t, err)
	assert.NotNil(t, state.DailyCosts)
	assert.NotNil(t, state.SessionCosts)
}

// ---------------------------------------------------------------------------
// Test 22: Validate row counts match between SQLite and Dolt
// ---------------------------------------------------------------------------

func TestMigrateSQLite_RowCountsMatch(t *testing.T) {
	doltDB, cleanup := testDoltDB(t)
	defer cleanup()

	tmpDir := t.TempDir()
	sqlitePath := filepath.Join(tmpDir, "analytics.db")
	sqliteDB := createTestSQLite(t, sqlitePath)
	seedSQLiteData(t, sqliteDB, 5, 10, 8)
	sqliteDB.Close()

	ctx := context.Background()
	var buf bytes.Buffer
	sessions, events, authorship, errs := MigrateSQLite(ctx, doltDB, sqlitePath, false, &buf)
	assert.Empty(t, errs)

	// Verify counts match what was seeded.
	assert.Equal(t, 5, sessions)
	assert.Equal(t, 10, events)
	assert.Equal(t, 8, authorship)

	// Cross-check with Dolt.
	var doltSessions, doltEvents, doltAuthorship int
	require.NoError(t, doltDB.QueryRow(ctx, "SELECT COUNT(*) FROM sessions").Scan(&doltSessions))
	require.NoError(t, doltDB.QueryRow(ctx, "SELECT COUNT(*) FROM events").Scan(&doltEvents))
	require.NoError(t, doltDB.QueryRow(ctx, "SELECT COUNT(*) FROM authorship_ledger").Scan(&doltAuthorship))

	assert.Equal(t, sessions, doltSessions, "session row counts should match")
	assert.Equal(t, events, doltEvents, "event row counts should match")
	assert.Equal(t, authorship, doltAuthorship, "authorship row counts should match")
}

// ---------------------------------------------------------------------------
// Test 23: Session data fidelity (values survive the migration)
// ---------------------------------------------------------------------------

func TestMigrateSQLite_SessionDataFidelity(t *testing.T) {
	doltDB, cleanup := testDoltDB(t)
	defer cleanup()

	tmpDir := t.TempDir()
	sqlitePath := filepath.Join(tmpDir, "analytics.db")
	sqliteDB := createTestSQLite(t, sqlitePath)

	// Insert a specific session with known values.
	_, err := sqliteDB.Exec(
		`INSERT INTO sessions (id, started_at, ended_at, total_tool_calls, file_edits_count,
		                       ai_authored_lines, human_verified_lines, estimated_cost_usd)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"fidelity-sess", "2025-03-06T10:00:00Z", "2025-03-06T12:30:00Z",
		42, 7, 200, 50, 1.23,
	)
	require.NoError(t, err)
	sqliteDB.Close()

	ctx := context.Background()
	var buf bytes.Buffer
	_, _, _, errs := MigrateSQLite(ctx, doltDB, sqlitePath, false, &buf)
	assert.Empty(t, errs)

	// Read back from Dolt and verify field values.
	var (
		id, startedAt, endedAt                          string
		toolCalls, fileEdits, aiLines, humanLines int
		cost                                            float64
	)
	err = doltDB.QueryRow(ctx,
		`SELECT id, started_at, ended_at, total_tool_calls, file_edits_count,
		        ai_authored_lines, human_verified_lines, estimated_cost_usd
		 FROM sessions WHERE id = ?`, "fidelity-sess").
		Scan(&id, &startedAt, &endedAt, &toolCalls, &fileEdits, &aiLines, &humanLines, &cost)
	require.NoError(t, err)

	assert.Equal(t, "fidelity-sess", id)
	assert.Equal(t, "2025-03-06T10:00:00Z", startedAt)
	assert.Equal(t, "2025-03-06T12:30:00Z", endedAt)
	assert.Equal(t, 42, toolCalls)
	assert.Equal(t, 7, fileEdits)
	assert.Equal(t, 200, aiLines)
	assert.Equal(t, 50, humanLines)
	assert.InDelta(t, 1.23, cost, 0.001)
}

// ---------------------------------------------------------------------------
// Test 24: Run output mentions all phases
// ---------------------------------------------------------------------------

func TestRun_OutputCoversAllPhases(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise-state"))

	hwDir := filepath.Join(tmpDir, ".hookwise")
	require.NoError(t, os.MkdirAll(filepath.Join(hwDir, "state"), 0o700))

	sqlitePath := filepath.Join(hwDir, "analytics.db")
	sqliteDB := createTestSQLite(t, sqlitePath)
	seedSQLiteData(t, sqliteDB, 1, 1, 1)
	sqliteDB.Close()
	createCostStateJSON(t, hwDir)

	var buf bytes.Buffer
	_ = Run(MigrationOpts{
		HomeDir:    tmpDir,
		DryRun:     true,
		ProjectDir: tmpDir,
		Writer:     &buf,
	})

	output := buf.String()

	// Should mention detection.
	assert.Contains(t, output, "Detecting TypeScript")
	// Should mention SQLite migration.
	assert.Contains(t, output, "SQLite")
	// Should mention cost state.
	assert.Contains(t, output, "cost state")
	// Should mention config validation.
	assert.Contains(t, output, "config")
	// Should mention Dolt commit intention.
	assert.True(t, strings.Contains(output, "commit") || strings.Contains(output, "Commit"),
		"output should mention Dolt commit")
	// Should have summary.
	assert.Contains(t, output, "Migration summary")
}
