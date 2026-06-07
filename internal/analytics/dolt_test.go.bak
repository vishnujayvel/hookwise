package analytics

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testOpen creates a temporary directory, opens a fresh Dolt DB in it,
// and returns the DB plus a cleanup function.
func testOpen(t *testing.T) (*DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "hookwise-dolt-test-*")
	require.NoError(t, err)

	db, err := Open(tmpDir)
	require.NoError(t, err, "Open should succeed on a fresh temp dir")

	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return db, cleanup
}

// ---------------------------------------------------------------------------
// Task 5.4 — Integration tests (minimum 10)
// ---------------------------------------------------------------------------

// Test 1: Database creation from scratch
func TestOpen_FreshDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hookwise-dolt-fresh-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	db, err := Open(tmpDir)
	require.NoError(t, err, "Open on a brand-new temp directory should succeed")
	require.NotNil(t, db)
	assert.NotNil(t, db.db, "underlying *sql.DB should be set")
	assert.Equal(t, "hookwise", db.dbName)
	require.NoError(t, db.Close())

	// The hookwise sub-directory should exist after Open().
	info, err := os.Stat(filepath.Join(tmpDir, "hookwise"))
	require.NoError(t, err, "hookwise database directory should have been created")
	assert.True(t, info.IsDir())
}

// Test 2: Schema initialisation creates all 10 tables
func TestOpen_SchemaCreatesAllTables(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	expectedTables := []string{
		"sessions",
		"events",
		"authorship_ledger",
		"metacognition_logs",
		"agent_spans",
		"feed_cache",
		"coaching_state",
		"cost_state",
		"notifications",
		"schema_meta",
	}

	rows, err := db.Query(ctx, "SHOW TABLES")
	require.NoError(t, err)
	defer rows.Close()

	found := make(map[string]bool)
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		found[name] = true
	}
	require.NoError(t, rows.Err())

	for _, table := range expectedTables {
		assert.True(t, found[table], "table %q should exist", table)
	}
}

// Test 3: Idempotent schema — calling Open twice doesn't error
func TestOpen_Idempotent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hookwise-dolt-idem-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	db1, err := Open(tmpDir)
	require.NoError(t, err)
	require.NoError(t, db1.Close())

	// Re-open the same directory — schema should be applied idempotently.
	db2, err := Open(tmpDir)
	require.NoError(t, err, "second Open on same directory should succeed")
	require.NoError(t, db2.Close())
}

// Test 4: Insert into sessions table
func TestExec_InsertSession(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	res, err := db.Exec(ctx,
		"INSERT INTO sessions (id, started_at) VALUES (?, ?)",
		"sess-001", "2025-03-06T10:00:00Z")
	require.NoError(t, err)

	affected, err := res.RowsAffected()
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)
}

// Test 5: Query sessions table
func TestQuery_SelectSession(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	_, err := db.Exec(ctx,
		"INSERT INTO sessions (id, started_at, total_tool_calls) VALUES (?, ?, ?)",
		"sess-002", "2025-03-06T11:00:00Z", 42)
	require.NoError(t, err)

	row := db.QueryRow(ctx, "SELECT id, total_tool_calls FROM sessions WHERE id = ?", "sess-002")
	var id string
	var toolCalls int
	require.NoError(t, row.Scan(&id, &toolCalls))
	assert.Equal(t, "sess-002", id)
	assert.Equal(t, 42, toolCalls)
}

// Test 6: Insert into events table with auto-increment
func TestExec_InsertEvents(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	// Insert a session first (events reference session_id)
	_, err := db.Exec(ctx,
		"INSERT INTO sessions (id, started_at) VALUES (?, ?)",
		"sess-ev", "2025-03-06T12:00:00Z")
	require.NoError(t, err)

	res, err := db.Exec(ctx,
		`INSERT INTO events (session_id, event_type, tool_name, timestamp, lines_added)
		 VALUES (?, ?, ?, ?, ?)`,
		"sess-ev", "PostToolUse", "Write", "2025-03-06T12:01:00Z", 15)
	require.NoError(t, err)

	lastID, err := res.LastInsertId()
	require.NoError(t, err)
	assert.True(t, lastID > 0, "auto-increment should produce a positive ID")
}

// Test 7: Commit creates a Dolt commit visible in log
func TestCommit_AppearsInLog(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	// Make a data change so there's something to commit.
	_, err := db.Exec(ctx,
		"INSERT INTO schema_meta (meta_key, meta_value) VALUES (?, ?)",
		"version", "1.0.0")
	require.NoError(t, err)

	hash, err := db.Commit(ctx, "test: initial data")
	require.NoError(t, err)
	assert.NotEmpty(t, hash, "commit should return a non-empty hash")

	entries, err := db.Log(ctx, 5)
	require.NoError(t, err)
	require.NotEmpty(t, entries, "log should contain at least one entry")

	// The most recent commit should match our message.
	found := false
	for _, e := range entries {
		if e.Message == "test: initial data" {
			found = true
			assert.Equal(t, hash, e.CommitHash)
			break
		}
	}
	assert.True(t, found, "our commit message should appear in the log")
}

// Test 8: CommitDispatch formats message correctly
func TestCommitDispatch_MessageFormat(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	_, err := db.Exec(ctx,
		"INSERT INTO schema_meta (meta_key, meta_value) VALUES (?, ?)",
		"dispatch_test", "value")
	require.NoError(t, err)

	hash, err := db.CommitDispatch(ctx, "PreToolUse", "sess-abc123")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	entries, err := db.Log(ctx, 1)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	assert.Equal(t, "dispatch:PreToolUse session:sess-abc123", entries[0].Message)
}

// Test 9: Commit with no changes returns empty hash and no error
func TestCommit_NoChanges(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	// Nothing has been written, so committing should be a no-op.
	hash, err := db.Commit(ctx, "should be empty")
	require.NoError(t, err, "commit with no changes should not error")
	assert.Empty(t, hash, "hash should be empty when nothing to commit")
}

// Test 10: Log returns entries in reverse chronological order
func TestLog_ReturnsMultipleEntries(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	// First commit
	_, err := db.Exec(ctx,
		"INSERT INTO schema_meta (meta_key, meta_value) VALUES (?, ?)",
		"k1", "v1")
	require.NoError(t, err)
	_, err = db.Commit(ctx, "commit-one")
	require.NoError(t, err)

	// Second commit
	_, err = db.Exec(ctx,
		"REPLACE INTO schema_meta (meta_key, meta_value) VALUES (?, ?)",
		"k1", "v2")
	require.NoError(t, err)
	_, err = db.Commit(ctx, "commit-two")
	require.NoError(t, err)

	entries, err := db.Log(ctx, 10)
	require.NoError(t, err)
	// Should have at least our 2 commits (plus possibly initial commit).
	require.GreaterOrEqual(t, len(entries), 2, "log should have at least 2 entries")

	// Most recent first
	assert.Equal(t, "commit-two", entries[0].Message)
	assert.Equal(t, "commit-one", entries[1].Message)
}

// Test 11: Log with limit
func TestLog_RespectsLimit(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	// Create 3 commits
	for i := 0; i < 3; i++ {
		_, err := db.Exec(ctx,
			"REPLACE INTO schema_meta (meta_key, meta_value) VALUES (?, ?)",
			"counter", string(rune('0'+i)))
		require.NoError(t, err)
		_, err = db.Commit(ctx, "commit-"+string(rune('A'+i)))
		require.NoError(t, err)
	}

	entries, err := db.Log(ctx, 2)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "limit=2 should return exactly 2 entries")
}

// Test 12: Diff detects changes using dolt_diff_summary
func TestDiff_DetectsChanges(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	// Make an initial commit to have a baseline ref.
	_, err := db.Exec(ctx,
		"INSERT INTO schema_meta (meta_key, meta_value) VALUES (?, ?)",
		"baseline", "0")
	require.NoError(t, err)
	_, err = db.Commit(ctx, "baseline")
	require.NoError(t, err)

	baselineLog, err := db.Log(ctx, 1)
	require.NoError(t, err)
	require.NotEmpty(t, baselineLog)
	baseHash := baselineLog[0].CommitHash

	// Insert data into sessions and commit.
	_, err = db.Exec(ctx,
		"INSERT INTO sessions (id, started_at) VALUES (?, ?)",
		"diff-sess", "2025-03-06T14:00:00Z")
	require.NoError(t, err)
	_, err = db.Commit(ctx, "add session")
	require.NoError(t, err)

	headLog, err := db.Log(ctx, 1)
	require.NoError(t, err)
	headHash := headLog[0].CommitHash

	diffs, err := db.Diff(ctx, baseHash, headHash)
	require.NoError(t, err)

	// dolt_diff_summary returns per-table entries showing which tables changed.
	// We should see at least the sessions table with a "modified" diff_type
	// (it's "modified" because the table already existed and a row was added).
	found := false
	for _, d := range diffs {
		if d.TableName == "sessions" {
			found = true
			// The diff_type from dolt_diff_summary is "modified" when rows change.
			assert.Equal(t, "modified", d.DiffType)
			assert.Equal(t, baseHash, d.FromCommit)
			assert.Equal(t, headHash, d.ToCommit)
			assert.NotNil(t, d.RowData)
			break
		}
	}
	assert.True(t, found, "diff should contain an entry for the sessions table")
}

// Test 13: Multiple tables can be written and queried in one session
func TestMultiTable_WriteAndQuery(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	// Sessions
	_, err := db.Exec(ctx,
		"INSERT INTO sessions (id, started_at) VALUES (?, ?)",
		"mt-sess", "2025-03-06T15:00:00Z")
	require.NoError(t, err)

	// Events
	_, err = db.Exec(ctx,
		`INSERT INTO events (session_id, event_type, tool_name, timestamp)
		 VALUES (?, ?, ?, ?)`,
		"mt-sess", "PreToolUse", "Bash", "2025-03-06T15:01:00Z")
	require.NoError(t, err)

	// Coaching state
	_, err = db.Exec(ctx,
		`INSERT INTO coaching_state (id, current_mode, alert_level, tooling_minutes)
		 VALUES (1, 'coding', 'none', 0)`)
	require.NoError(t, err)

	// Cost state
	_, err = db.Exec(ctx,
		`INSERT INTO cost_state (id, today, total_today) VALUES (1, '2025-03-06', 0.5)`)
	require.NoError(t, err)

	// Verify all inserts
	var sessCount, evCount, coachCount, costCount int
	require.NoError(t, db.QueryRow(ctx, "SELECT COUNT(*) FROM sessions").Scan(&sessCount))
	require.NoError(t, db.QueryRow(ctx, "SELECT COUNT(*) FROM events").Scan(&evCount))
	require.NoError(t, db.QueryRow(ctx, "SELECT COUNT(*) FROM coaching_state").Scan(&coachCount))
	require.NoError(t, db.QueryRow(ctx, "SELECT COUNT(*) FROM cost_state").Scan(&costCount))

	assert.Equal(t, 1, sessCount)
	assert.Equal(t, 1, evCount)
	assert.Equal(t, 1, coachCount)
	assert.Equal(t, 1, costCount)
}

// Test 14: Close is safe to call multiple times
func TestClose_Idempotent(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	require.NoError(t, db.Close())
	// Second close — should not panic or return an unexpected error.
	// It may return an error (e.g., "sql: database is closed") which is fine.
	_ = db.Close()
}

// Test 15: Open with empty dataDir uses default and creates dir structure
func TestOpen_EmptyDataDir_UsesDefault(t *testing.T) {
	// We don't actually call Open("") here because it would use the real
	// home directory.  Instead, verify DefaultDoltDir returns a plausible path.
	dir := DefaultDoltDir()
	assert.Contains(t, dir, ".hookwise")
	assert.Contains(t, dir, "dolt")
}

// Test 16: Feed cache table with JSON column
func TestFeedCache_JSONColumn(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	jsonData := `{"temperature": 72, "unit": "F"}`
	_, err := db.Exec(ctx,
		`INSERT INTO feed_cache (cache_key, data, updated_at, ttl_seconds)
		 VALUES (?, ?, ?, ?)`,
		"weather", jsonData, "2025-03-06T16:00:00Z", 300)
	require.NoError(t, err)

	var data string
	err = db.QueryRow(ctx, "SELECT data FROM feed_cache WHERE cache_key = ?", "weather").Scan(&data)
	require.NoError(t, err)
	assert.JSONEq(t, jsonData, data)
}

// Test 17: Notifications table with auto-increment
func TestNotifications_AutoIncrement(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, err := db.Exec(ctx,
			`INSERT INTO notifications (producer, notification_type, content, created_at)
			 VALUES (?, ?, ?, ?)`,
			"pulse", "alert", "test content", "2025-03-06T17:00:00Z")
		require.NoError(t, err)
	}

	var count int
	err := db.QueryRow(ctx, "SELECT COUNT(*) FROM notifications").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

// Test 18: Log entry fields are populated correctly
func TestLog_EntryFields(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	_, err := db.Exec(ctx,
		"INSERT INTO schema_meta (meta_key, meta_value) VALUES (?, ?)",
		"field_test", "yes")
	require.NoError(t, err)

	_, err = db.Commit(ctx, "field check commit")
	require.NoError(t, err)

	entries, err := db.Log(ctx, 1)
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	e := entries[0]
	assert.Equal(t, "field check commit", e.Message)
	assert.NotEmpty(t, e.CommitHash)
	assert.Equal(t, "hookwise", e.Committer)
	assert.Equal(t, "hookwise@local", e.Email)
	assert.False(t, e.Date.IsZero(), "date should be parsed")
}

// Test 19: Diff rejects SQL injection attempts
func TestDiff_RejectsInvalidRefs(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	tests := []struct {
		name    string
		fromRef string
		toRef   string
	}{
		{"SQL injection single quote", "'; DROP TABLE sessions; --", "HEAD"},
		{"SQL injection in toRef", "HEAD", "'; DROP TABLE sessions; --"},
		{"spaces", "HEAD 1", "HEAD"},
		{"parentheses", "HEAD()", "HEAD"},
		{"semicolon", "HEAD;", "HEAD"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Diff(ctx, tc.fromRef, tc.toRef)
			require.Error(t, err, "Diff should reject invalid ref: %s", tc.name)
			assert.Contains(t, err.Error(), "invalid")
		})
	}
}
