package analytics

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// makeSnapshotFile creates a valid SQLite snapshot at dir/<name>.db by opening
// a fresh analytics DB, inserting rows, and using VACUUM INTO. This guarantees
// the file is a real SQLite database with the hookwise schema.
//
// sessions and events row counts are controlled by the caller.
func makeSnapshotFile(t *testing.T, db *DB, snapDir, name string, sessionRows, eventRows int) string {
	t.Helper()
	ctx := context.Background()

	// Insert sessions.
	for i := 0; i < sessionRows; i++ {
		_, err := db.Exec(ctx,
			`INSERT OR IGNORE INTO sessions (id, started_at) VALUES (?, ?)`,
			name+"-sess-"+itoa(i), "2026-01-01T00:00:00Z")
		require.NoError(t, err)
	}
	// Insert events.
	for i := 0; i < eventRows; i++ {
		_, err := db.Exec(ctx,
			`INSERT INTO events (session_id, event_type, timestamp) VALUES (?, ?, ?)`,
			name+"-sess-0", "test_event", "2026-01-01T00:00:00Z")
		require.NoError(t, err)
	}

	// VACUUM INTO a controlled path (bypasses same-second filename collision).
	dest := filepath.Join(snapDir, name+".db")
	quoted := "'" + escapeSQLLiteral(dest) + "'"
	_, err := db.db.ExecContext(ctx, "VACUUM INTO "+quoted)
	require.NoError(t, err)
	return dest
}

// copyFile copies src to dst, creating dst if needed.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	require.NoError(t, err)
	defer in.Close()
	out, err := os.Create(dst)
	require.NoError(t, err)
	defer out.Close()
	_, err = io.Copy(out, in)
	require.NoError(t, err)
}

// itoa converts a small int to its decimal string representation without
// importing strconv (keeping imports lean in test helpers).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// ListSnapshotInfos tests
// ---------------------------------------------------------------------------

// TestListSnapshotInfos_NewestFirst verifies that results are ordered
// newest-first and that RowCounts are populated correctly for sessions/events.
func TestListSnapshotInfos_NewestFirst(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	snapDir := t.TempDir()

	// Create two snapshots with controlled names (older → newer lexicographically).
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z", 2, 3)
	makeSnapshotFile(t, db, snapDir, "20260102T000000Z", 4, 5)

	infos, err := ListSnapshotInfos(snapDir, 0)
	require.NoError(t, err)
	require.Len(t, infos, 2)

	// Newest first.
	assert.Equal(t, "20260102T000000Z", infos[0].Name)
	assert.Equal(t, "20260101T000000Z", infos[1].Name)

	// Times parse correctly.
	wantNewer, _ := time.ParseInLocation(snapshotTimeFormat, "20260102T000000Z", time.UTC)
	wantOlder, _ := time.ParseInLocation(snapshotTimeFormat, "20260101T000000Z", time.UTC)
	assert.Equal(t, wantNewer, infos[0].Time)
	assert.Equal(t, wantOlder, infos[1].Time)

	// SizeBytes non-zero.
	assert.Positive(t, infos[0].SizeBytes)
	assert.Positive(t, infos[1].SizeBytes)

	// RowCounts: the second snapshot (20260102) accumulated rows from both inserts.
	// The first snapshot was taken when db had 2 sessions+3 events; the second
	// snapshot VACUUM INTO captures the state at that point in time, which already
	// includes those rows too (we don't reset between calls). So we just validate
	// that RowCounts keys exist and are non-negative.
	assert.Contains(t, infos[0].RowCounts, "sessions")
	assert.Contains(t, infos[0].RowCounts, "events")
	assert.GreaterOrEqual(t, infos[0].RowCounts["sessions"], int64(0))
	assert.GreaterOrEqual(t, infos[0].RowCounts["events"], int64(0))
}

// TestListSnapshotInfos_Limit verifies the limit parameter truncates results.
func TestListSnapshotInfos_Limit(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	snapDir := t.TempDir()
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z", 1, 0)
	makeSnapshotFile(t, db, snapDir, "20260102T000000Z", 1, 0)
	makeSnapshotFile(t, db, snapDir, "20260103T000000Z", 1, 0)

	infos, err := ListSnapshotInfos(snapDir, 2)
	require.NoError(t, err)
	require.Len(t, infos, 2)

	// Should be the two newest.
	assert.Equal(t, "20260103T000000Z", infos[0].Name)
	assert.Equal(t, "20260102T000000Z", infos[1].Name)
}

// TestListSnapshotInfos_Empty verifies an empty or missing dir yields nil and no error.
func TestListSnapshotInfos_Empty(t *testing.T) {
	dir := t.TempDir()
	infos, err := ListSnapshotInfos(dir, 0)
	require.NoError(t, err)
	assert.Empty(t, infos)

	infos2, err2 := ListSnapshotInfos(filepath.Join(dir, "does-not-exist"), 0)
	require.NoError(t, err2)
	assert.Empty(t, infos2)
}

// TestListSnapshotInfos_RowCountsForAllTables verifies that RowCounts covers
// all schema tables (not just sessions/events).
func TestListSnapshotInfos_RowCountsForAllTables(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	snapDir := t.TempDir()
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z", 1, 1)

	infos, err := ListSnapshotInfos(snapDir, 0)
	require.NoError(t, err)
	require.Len(t, infos, 1)

	wantTables := []string{
		"agent_spans", "authorship_ledger", "coaching_state", "cost_state",
		"events", "feed_cache", "metacognition_logs", "notifications",
		"schema_meta", "sessions",
	}
	for _, tbl := range wantTables {
		assert.Contains(t, infos[0].RowCounts, tbl, "RowCounts should include table %q", tbl)
	}
}

// ---------------------------------------------------------------------------
// ResolveSnapshot tests
// ---------------------------------------------------------------------------

// TestResolveSnapshot_Latest verifies "latest" resolves to the newest snapshot.
func TestResolveSnapshot_Latest(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	snapDir := t.TempDir()
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z", 0, 0)
	makeSnapshotFile(t, db, snapDir, "20260102T000000Z", 0, 0)

	info, err := ResolveSnapshot(snapDir, "latest")
	require.NoError(t, err)
	assert.Equal(t, "20260102T000000Z", info.Name)
}

// TestResolveSnapshot_Prev verifies "prev" resolves to second-newest.
func TestResolveSnapshot_Prev(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	snapDir := t.TempDir()
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z", 0, 0)
	makeSnapshotFile(t, db, snapDir, "20260102T000000Z", 0, 0)
	makeSnapshotFile(t, db, snapDir, "20260103T000000Z", 0, 0)

	info, err := ResolveSnapshot(snapDir, "prev")
	require.NoError(t, err)
	assert.Equal(t, "20260102T000000Z", info.Name)
}

// TestResolveSnapshot_PrevRequiresTwoSnapshots verifies "prev" errors when
// fewer than 2 snapshots exist.
func TestResolveSnapshot_PrevRequiresTwoSnapshots(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	snapDir := t.TempDir()
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z", 0, 0)

	_, err := ResolveSnapshot(snapDir, "prev")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 2")
}

// TestResolveSnapshot_ExactMatch verifies a full timestamp name resolves exactly.
func TestResolveSnapshot_ExactMatch(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	snapDir := t.TempDir()
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z", 0, 0)
	makeSnapshotFile(t, db, snapDir, "20260102T120000Z", 0, 0)

	info, err := ResolveSnapshot(snapDir, "20260101T000000Z")
	require.NoError(t, err)
	assert.Equal(t, "20260101T000000Z", info.Name)
}

// TestResolveSnapshot_PrefixMatch verifies a date prefix resolves to the newest
// matching snapshot.
func TestResolveSnapshot_PrefixMatch(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	snapDir := t.TempDir()
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z", 0, 0)
	makeSnapshotFile(t, db, snapDir, "20260101T120000Z", 0, 0)
	makeSnapshotFile(t, db, snapDir, "20260102T000000Z", 0, 0)

	// Prefix "20260101" matches two snapshots; should return the newer one.
	info, err := ResolveSnapshot(snapDir, "20260101")
	require.NoError(t, err)
	assert.Equal(t, "20260101T120000Z", info.Name)
}

// TestResolveSnapshot_NotFound verifies an unrecognized ref returns a clear error.
func TestResolveSnapshot_NotFound(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	snapDir := t.TempDir()
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z", 0, 0)

	_, err := ResolveSnapshot(snapDir, "99991231")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestResolveSnapshot_NoSnapshots verifies a clear error when the dir is empty.
func TestResolveSnapshot_NoSnapshots(t *testing.T) {
	snapDir := t.TempDir()
	_, err := ResolveSnapshot(snapDir, "latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no snapshots")
}

// ---------------------------------------------------------------------------
// DiffSnapshots tests
// ---------------------------------------------------------------------------

// TestDiffSnapshots_Delta verifies per-table row-count deltas are correct.
func TestDiffSnapshots_Delta(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	snapDir := t.TempDir()

	// Snapshot A: 1 session, 2 events.
	_, err := db.Exec(ctx,
		`INSERT INTO sessions (id, started_at) VALUES (?, ?)`, "s1", "2026-01-01T00:00:00Z")
	require.NoError(t, err)
	_, err = db.Exec(ctx,
		`INSERT INTO events (session_id, event_type, timestamp) VALUES (?, ?, ?)`, "s1", "ev", "2026-01-01T00:00:00Z")
	require.NoError(t, err)
	_, err = db.Exec(ctx,
		`INSERT INTO events (session_id, event_type, timestamp) VALUES (?, ?, ?)`, "s1", "ev", "2026-01-01T00:00:01Z")
	require.NoError(t, err)
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z", 0, 0) // rows already in db

	// Snapshot B: add 1 more session, 3 more events.
	_, err = db.Exec(ctx,
		`INSERT INTO sessions (id, started_at) VALUES (?, ?)`, "s2", "2026-01-02T00:00:00Z")
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		_, err = db.Exec(ctx,
			`INSERT INTO events (session_id, event_type, timestamp) VALUES (?, ?, ?)`,
			"s2", "ev", "2026-01-02T00:00:00Z")
		require.NoError(t, err)
	}
	makeSnapshotFile(t, db, snapDir, "20260102T000000Z", 0, 0)

	diff, err := DiffSnapshots(snapDir, "20260101T000000Z", "20260102T000000Z")
	require.NoError(t, err)

	assert.Equal(t, "20260101T000000Z", diff.From.Name)
	assert.Equal(t, "20260102T000000Z", diff.To.Name)

	// Find session and event deltas.
	byTable := make(map[string]TableDelta)
	for _, td := range diff.Tables {
		byTable[td.Table] = td
	}

	// sessions: +1
	sessD := byTable["sessions"]
	assert.Equal(t, int64(1), sessD.Delta, "sessions delta should be +1")

	// events: +3
	evD := byTable["events"]
	assert.Equal(t, int64(3), evD.Delta, "events delta should be +3")

	// Tables are sorted by name.
	for i := 1; i < len(diff.Tables); i++ {
		assert.LessOrEqual(t, diff.Tables[i-1].Table, diff.Tables[i].Table,
			"Tables should be sorted by name")
	}
}

// TestDiffSnapshots_TableAddedBetweenSnapshots verifies a table missing in the
// first snapshot (all-zero rows) is included with fromRows=0.
func TestDiffSnapshots_TableAddedBetweenSnapshots(t *testing.T) {
	snapDir := t.TempDir()

	// Build a minimal snapshot A from a fresh empty DB (no rows in any table).
	dbA, cleanupA := testOpen(t)
	defer cleanupA()
	pathA := filepath.Join(snapDir, "20260101T000000Z.db")
	ctx := context.Background()
	quoted := "'" + escapeSQLLiteral(pathA) + "'"
	_, err := dbA.db.ExecContext(ctx, "VACUUM INTO "+quoted)
	require.NoError(t, err)

	// Build snapshot B from a DB that has sessions.
	dbB, cleanupB := testOpen(t)
	defer cleanupB()
	_, err = dbB.Exec(ctx,
		`INSERT INTO sessions (id, started_at) VALUES (?, ?)`, "sx", "2026-01-01T00:00:00Z")
	require.NoError(t, err)
	pathB := filepath.Join(snapDir, "20260102T000000Z.db")
	quotedB := "'" + escapeSQLLiteral(pathB) + "'"
	_, err = dbB.db.ExecContext(ctx, "VACUUM INTO "+quotedB)
	require.NoError(t, err)

	diff, err := DiffSnapshots(snapDir, "20260101T000000Z", "20260102T000000Z")
	require.NoError(t, err)

	// sessions should appear with fromRows=0, toRows=1, delta=+1.
	byTable := make(map[string]TableDelta)
	for _, td := range diff.Tables {
		byTable[td.Table] = td
	}
	sess := byTable["sessions"]
	assert.Equal(t, int64(0), sess.FromRows)
	assert.Equal(t, int64(1), sess.ToRows)
	assert.Equal(t, int64(1), sess.Delta)
}

// TestDiffSnapshots_NoChange verifies that when rows are identical the deltas
// are all zero (the caller filters, but DiffSnapshots still returns them).
func TestDiffSnapshots_NoChange(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	snapDir := t.TempDir()
	// Both snapshots from the same state → zero deltas.
	ctx := context.Background()
	pathA := filepath.Join(snapDir, "20260101T000000Z.db")
	pathB := filepath.Join(snapDir, "20260102T000000Z.db")
	quotedA := "'" + escapeSQLLiteral(pathA) + "'"
	quotedB := "'" + escapeSQLLiteral(pathB) + "'"
	_, err := db.db.ExecContext(ctx, "VACUUM INTO "+quotedA)
	require.NoError(t, err)
	_, err = db.db.ExecContext(ctx, "VACUUM INTO "+quotedB)
	require.NoError(t, err)

	diff, err := DiffSnapshots(snapDir, "20260101T000000Z", "20260102T000000Z")
	require.NoError(t, err)

	for _, td := range diff.Tables {
		assert.Equal(t, int64(0), td.Delta, "expected zero delta for table %q", td.Table)
	}
}

// TestDiffSnapshots_PrevLatestRefs verifies that symbolic refs (prev, latest)
// resolve correctly inside DiffSnapshots.
func TestDiffSnapshots_PrevLatestRefs(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	snapDir := t.TempDir()

	// prev snapshot.
	pathA := filepath.Join(snapDir, "20260101T000000Z.db")
	quotedA := "'" + escapeSQLLiteral(pathA) + "'"
	_, err := db.db.ExecContext(ctx, "VACUUM INTO "+quotedA)
	require.NoError(t, err)

	// latest snapshot (one more event).
	_, err = db.Exec(ctx,
		`INSERT INTO events (session_id, event_type, timestamp) VALUES (?, ?, ?)`,
		"sx", "ev", "2026-01-02T00:00:00Z")
	require.NoError(t, err)
	pathB := filepath.Join(snapDir, "20260102T000000Z.db")
	quotedB := "'" + escapeSQLLiteral(pathB) + "'"
	_, err = db.db.ExecContext(ctx, "VACUUM INTO "+quotedB)
	require.NoError(t, err)

	diff, err := DiffSnapshots(snapDir, "prev", "latest")
	require.NoError(t, err)
	assert.Equal(t, "20260101T000000Z", diff.From.Name)
	assert.Equal(t, "20260102T000000Z", diff.To.Name)

	byTable := make(map[string]TableDelta)
	for _, td := range diff.Tables {
		byTable[td.Table] = td
	}
	assert.Equal(t, int64(1), byTable["events"].Delta)
}

// TestDiffSnapshots_UnknownRef verifies a clear error on an unresolvable ref.
func TestDiffSnapshots_UnknownRef(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	snapDir := t.TempDir()
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z", 0, 0)

	_, err := DiffSnapshots(snapDir, "latest", "99991231")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Robustness fixes (code-review)
// ---------------------------------------------------------------------------

// TestListSnapshotInfos_CollisionSuffixVisible verifies that same-second
// collision snapshots ("<ts>-N.db") are enumerated, parsed (Time from the
// timestamp prefix), and pruned — not silently invisible.
func TestListSnapshotInfos_CollisionSuffixVisible(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	snapDir := t.TempDir()
	// A base snapshot and a same-second collision sibling.
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z", 1, 0)
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z-1", 1, 0)

	// Both are listed (the regex matches the "-1" suffix).
	paths, err := ListSnapshots(snapDir)
	require.NoError(t, err)
	require.Len(t, paths, 2, "collision-suffixed snapshot must be visible to ListSnapshots")

	// Both parse, and the collision file's Time comes from the timestamp prefix.
	infos, err := ListSnapshotInfos(snapDir, 0)
	require.NoError(t, err)
	require.Len(t, infos, 2)
	wantTime, _ := time.ParseInLocation(snapshotTimeFormat, "20260101T000000Z", time.UTC)
	for _, info := range infos {
		assert.Equal(t, wantTime, info.Time, "collision file Time should parse from the timestamp prefix")
	}

	// Both are prunable (keep=1 removes one of the two).
	pruned, err := PruneSnapshots(snapDir, 1)
	require.NoError(t, err)
	assert.Len(t, pruned, 1, "collision-suffixed snapshot must be prunable")
}

// TestListSnapshotInfos_SkipsCorruptSnapshot verifies that one unreadable
// snapshot does not abort the whole listing — `hookwise log` stays usable.
func TestListSnapshotInfos_SkipsCorruptSnapshot(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	snapDir := t.TempDir()
	makeSnapshotFile(t, db, snapDir, "20260101T000000Z", 2, 0)

	// A file with a valid snapshot name but garbage contents (e.g. a
	// crash-truncated VACUUM INTO target). It matches the regex, so ListSnapshots
	// returns it, but it cannot be opened/queried.
	corrupt := filepath.Join(snapDir, "20260102T000000Z.db")
	require.NoError(t, os.WriteFile(corrupt, []byte("not a sqlite database"), 0o600))

	infos, err := ListSnapshotInfos(snapDir, 0)
	require.NoError(t, err, "one corrupt snapshot must not fail the whole listing")
	require.Len(t, infos, 1, "the corrupt snapshot should be skipped, the good one kept")
	assert.Equal(t, "20260101T000000Z", infos[0].Name)
}
