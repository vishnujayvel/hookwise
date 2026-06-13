package analytics

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// TestSnapshot_CreatesOpenableDBWithData verifies that Snapshot produces a valid
// SQLite file that contains the same schema tables and the rows present in the
// source database at snapshot time.
func TestSnapshot_CreatesOpenableDBWithData(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	// Insert a row we can look for in the snapshot.
	_, err := db.Exec(ctx,
		`INSERT INTO sessions (id, started_at, total_tool_calls) VALUES (?, ?, ?)`,
		"sess-1", "2026-06-07T00:00:00Z", 7)
	require.NoError(t, err)

	snapDir := t.TempDir()
	path, err := db.Snapshot(ctx, snapDir)
	require.NoError(t, err)
	require.FileExists(t, path)
	assert.True(t, strings.HasSuffix(path, ".db"), "snapshot path should end in .db")

	// Open the snapshot directly with the modernc driver (read-only intent).
	snap, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer snap.Close()

	// All ten schema tables must be present in the snapshot.
	wantTables := []string{
		"sessions", "events", "authorship_ledger", "metacognition_logs",
		"agent_spans", "feed_cache", "coaching_state", "cost_state",
		"notifications", "schema_meta",
	}
	for _, tbl := range wantTables {
		var name string
		row := snap.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl)
		require.NoError(t, row.Scan(&name), "table %q should exist in snapshot", tbl)
		assert.Equal(t, tbl, name)
	}

	// The inserted row must survive into the snapshot.
	var calls int
	row := snap.QueryRowContext(ctx, `SELECT total_tool_calls FROM sessions WHERE id=?`, "sess-1")
	require.NoError(t, row.Scan(&calls))
	assert.Equal(t, 7, calls)
}

// TestSnapshot_SameSecondCollision verifies two snapshots taken back-to-back
// both succeed and land at distinct paths.
func TestSnapshot_SameSecondCollision(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	snapDir := t.TempDir()

	p1, err := db.Snapshot(ctx, snapDir)
	require.NoError(t, err)
	p2, err := db.Snapshot(ctx, snapDir)
	require.NoError(t, err)

	assert.NotEqual(t, p1, p2, "back-to-back snapshots must not collide")
	require.FileExists(t, p1)
	require.FileExists(t, p2)
}

// TestListSnapshots_OnlyMatchingFilesSorted verifies ListSnapshots returns only
// snapshot-named files in oldest→newest order and ignores unrelated files.
func TestListSnapshots_OnlyMatchingFilesSorted(t *testing.T) {
	dir := t.TempDir()

	// Two valid snapshot names (lexicographic == chronological) and noise.
	mustWrite(t, filepath.Join(dir, "20260101T000000Z.db"))
	mustWrite(t, filepath.Join(dir, "20260102T000000Z.db"))
	mustWrite(t, filepath.Join(dir, "notes.txt"))
	mustWrite(t, filepath.Join(dir, "analytics.db"))     // not timestamped → ignored
	mustWrite(t, filepath.Join(dir, "20260101T000000Z")) // no .db suffix → ignored

	got, err := ListSnapshots(dir)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, filepath.Join(dir, "20260101T000000Z.db"), got[0])
	assert.Equal(t, filepath.Join(dir, "20260102T000000Z.db"), got[1])
}

// TestListSnapshots_MissingDir verifies a missing directory yields an empty
// result and no error.
func TestListSnapshots_MissingDir(t *testing.T) {
	got, err := ListSnapshots(filepath.Join(t.TempDir(), "does-not-exist"))
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestPruneSnapshots_KeepsNewest verifies prune removes the oldest files beyond
// keep and retains exactly the newest keep.
func TestPruneSnapshots_KeepsNewest(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"20260101T000000Z.db",
		"20260102T000000Z.db",
		"20260103T000000Z.db",
		"20260104T000000Z.db",
		"20260105T000000Z.db",
	}
	for _, n := range names {
		mustWrite(t, filepath.Join(dir, n))
	}

	pruned, err := PruneSnapshots(dir, 2)
	require.NoError(t, err)

	// Two oldest removed.
	require.Len(t, pruned, 3)
	assert.Equal(t, filepath.Join(dir, "20260101T000000Z.db"), pruned[0])
	assert.Equal(t, filepath.Join(dir, "20260102T000000Z.db"), pruned[1])
	assert.Equal(t, filepath.Join(dir, "20260103T000000Z.db"), pruned[2])

	// Two newest retained.
	remaining, err := ListSnapshots(dir)
	require.NoError(t, err)
	require.Len(t, remaining, 2)
	assert.Equal(t, filepath.Join(dir, "20260104T000000Z.db"), remaining[0])
	assert.Equal(t, filepath.Join(dir, "20260105T000000Z.db"), remaining[1])
}

// TestPruneSnapshots_NoOpWhenWithinKeep verifies nothing is pruned when the
// count is at or below keep.
func TestPruneSnapshots_NoOpWhenWithinKeep(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "20260101T000000Z.db"))
	mustWrite(t, filepath.Join(dir, "20260102T000000Z.db"))

	pruned, err := PruneSnapshots(dir, 5)
	require.NoError(t, err)
	assert.Empty(t, pruned)

	remaining, err := ListSnapshots(dir)
	require.NoError(t, err)
	assert.Len(t, remaining, 2)
}

// TestPruneSnapshots_KeepZeroRetainsAll verifies keep<=0 is treated as "retain
// all" rather than deleting everything.
func TestPruneSnapshots_KeepZeroRetainsAll(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "20260101T000000Z.db"))
	mustWrite(t, filepath.Join(dir, "20260102T000000Z.db"))

	pruned, err := PruneSnapshots(dir, 0)
	require.NoError(t, err)
	assert.Empty(t, pruned)

	remaining, err := ListSnapshots(dir)
	require.NoError(t, err)
	assert.Len(t, remaining, 2)
}

// TestPruneSnapshots_IgnoresNonSnapshotFiles verifies prune never touches files
// that do not match the snapshot naming pattern.
func TestPruneSnapshots_IgnoresNonSnapshotFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "20260101T000000Z.db"))
	mustWrite(t, filepath.Join(dir, "20260102T000000Z.db"))
	mustWrite(t, filepath.Join(dir, "20260103T000000Z.db"))
	mustWrite(t, filepath.Join(dir, "important.txt"))

	_, err := PruneSnapshots(dir, 1)
	require.NoError(t, err)

	// The non-snapshot file must still be present.
	assert.FileExists(t, filepath.Join(dir, "important.txt"))
}

// TestDefaultSnapshotsDir verifies the default path is under ~/.hookwise.
func TestDefaultSnapshotsDir(t *testing.T) {
	got := DefaultSnapshotsDir()
	assert.True(t, strings.HasSuffix(got, filepath.Join(".hookwise", "snapshots")),
		"DefaultSnapshotsDir should end in .hookwise/snapshots, got %q", got)
	assert.True(t, filepath.IsAbs(got), "DefaultSnapshotsDir should be absolute")
}

// mustWrite creates an empty file at path, failing the test on error.
func mustWrite(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
}
