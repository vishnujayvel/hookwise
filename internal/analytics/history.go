package analytics

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ---------------------------------------------------------------------------
// Snapshot-native types
// ---------------------------------------------------------------------------

// SnapshotInfo describes a single periodic snapshot file.
type SnapshotInfo struct {
	Path      string           // absolute path to the .db file
	Name      string           // basename without .db, e.g. "20060102T150405Z"
	Time      time.Time        // parsed from Name via snapshotTimeFormat
	SizeBytes int64            // file size in bytes
	RowCounts map[string]int64 // table name -> row count
}

// TableDelta holds the row-count change for one table between two snapshots.
type TableDelta struct {
	Table    string
	FromRows int64
	ToRows   int64
	Delta    int64 // ToRows - FromRows
}

// SnapshotDiff is the result of comparing two snapshots.
type SnapshotDiff struct {
	From   SnapshotInfo
	To     SnapshotInfo
	Tables []TableDelta // sorted by table name; all tables present in either snapshot
}

// ---------------------------------------------------------------------------
// ListSnapshotInfos
// ---------------------------------------------------------------------------

// ListSnapshotInfos returns metadata for snapshot files in snapshotsDir,
// ordered newest-first. Each SnapshotInfo includes file size and per-table row
// counts gathered by opening the snapshot read-only. limit <= 0 means no limit.
func ListSnapshotInfos(snapshotsDir string, limit int) ([]SnapshotInfo, error) {
	paths, err := ListSnapshots(snapshotsDir)
	if err != nil {
		return nil, err
	}

	// Reverse to newest-first.
	for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
		paths[i], paths[j] = paths[j], paths[i]
	}

	if limit > 0 && len(paths) > limit {
		paths = paths[:limit]
	}

	infos := make([]SnapshotInfo, 0, len(paths))
	for _, p := range paths {
		info, err := snapshotInfoFromPath(p)
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// snapshotInfoFromPath builds a SnapshotInfo for the given absolute path.
func snapshotInfoFromPath(absPath string) (SnapshotInfo, error) {
	base := filepath.Base(absPath)
	// Strip ".db" suffix to get the name.
	name := base
	if len(name) > 3 && name[len(name)-3:] == ".db" {
		name = name[:len(name)-3]
	}

	t, err := time.ParseInLocation(snapshotTimeFormat, name, time.UTC)
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("analytics: parse snapshot time %q: %w", name, err)
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("analytics: stat snapshot %s: %w", absPath, err)
	}

	rowCounts, err := snapshotRowCounts(absPath)
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("analytics: row counts for %s: %w", absPath, err)
	}

	return SnapshotInfo{
		Path:      absPath,
		Name:      name,
		Time:      t,
		SizeBytes: fi.Size(),
		RowCounts: rowCounts,
	}, nil
}

// snapshotRowCounts opens a snapshot read-only and returns per-table row counts
// for all tables found in sqlite_master.
func snapshotRowCounts(absPath string) (map[string]int64, error) {
	dsn := "file:" + absPath + "?mode=ro&_pragma=query_only(true)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", absPath, err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping %s: %w", absPath, err)
	}

	rows, err := db.Query(
		"SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name",
	)
	if err != nil {
		return nil, fmt.Errorf("list tables in %s: %w", absPath, err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan table name in %s: %w", absPath, err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tables in %s: %w", absPath, err)
	}

	counts := make(map[string]int64, len(tables))
	for _, tbl := range tables {
		var n int64
		// Double-quote the identifier; table names come from sqlite_master.
		if err := db.QueryRow(`SELECT COUNT(*) FROM "` + tbl + `"`).Scan(&n); err != nil {
			return nil, fmt.Errorf("count %q in %s: %w", tbl, absPath, err)
		}
		counts[tbl] = n
	}
	return counts, nil
}

// ---------------------------------------------------------------------------
// ResolveSnapshot
// ---------------------------------------------------------------------------

// ResolveSnapshot resolves a user-supplied ref string to a SnapshotInfo.
//
// Accepted ref forms:
//   - "latest" — the newest snapshot
//   - "prev"   — the second-newest (error if fewer than 2 exist)
//   - exact Name match (e.g. "20060102T150405Z")
//   - prefix match against Name (e.g. "20060102"); if multiple match, the
//     newest is chosen; if none match, a clear error is returned.
func ResolveSnapshot(snapshotsDir, ref string) (SnapshotInfo, error) {
	paths, err := ListSnapshots(snapshotsDir) // oldest → newest
	if err != nil {
		return SnapshotInfo{}, err
	}

	n := len(paths)
	if n == 0 {
		return SnapshotInfo{}, fmt.Errorf("analytics: no snapshots found in %s", snapshotsDir)
	}

	switch ref {
	case "latest":
		return snapshotInfoFromPath(paths[n-1])
	case "prev":
		if n < 2 {
			return SnapshotInfo{}, fmt.Errorf("analytics: need at least 2 snapshots for \"prev\", found %d", n)
		}
		return snapshotInfoFromPath(paths[n-2])
	}

	// Build names list (same order, oldest → newest).
	names := make([]string, n)
	for i, p := range paths {
		base := filepath.Base(p)
		if len(base) > 3 && base[len(base)-3:] == ".db" {
			names[i] = base[:len(base)-3]
		} else {
			names[i] = base
		}
	}

	// Exact match first.
	for i := n - 1; i >= 0; i-- {
		if names[i] == ref {
			return snapshotInfoFromPath(paths[i])
		}
	}

	// Prefix match — collect all; pick newest (highest index).
	var matched []int
	for i, nm := range names {
		if len(nm) >= len(ref) && nm[:len(ref)] == ref {
			matched = append(matched, i)
		}
	}
	if len(matched) == 0 {
		return SnapshotInfo{}, fmt.Errorf(
			"analytics: snapshot ref %q not found; %d snapshot(s) exist in %s",
			ref, n, snapshotsDir,
		)
	}
	// newest = highest index among matches
	best := matched[len(matched)-1]
	return snapshotInfoFromPath(paths[best])
}

// ---------------------------------------------------------------------------
// DiffSnapshots
// ---------------------------------------------------------------------------

// DiffSnapshots resolves fromRef and toRef to snapshot files and computes
// per-table row-count deltas over the union of tables in both snapshots.
// Tables missing from one snapshot count as 0 rows there. Results are sorted
// by table name.
func DiffSnapshots(snapshotsDir, fromRef, toRef string) (SnapshotDiff, error) {
	from, err := ResolveSnapshot(snapshotsDir, fromRef)
	if err != nil {
		return SnapshotDiff{}, fmt.Errorf("analytics: resolve from-ref %q: %w", fromRef, err)
	}
	to, err := ResolveSnapshot(snapshotsDir, toRef)
	if err != nil {
		return SnapshotDiff{}, fmt.Errorf("analytics: resolve to-ref %q: %w", toRef, err)
	}

	// Union of all table names.
	tableSet := make(map[string]struct{})
	for tbl := range from.RowCounts {
		tableSet[tbl] = struct{}{}
	}
	for tbl := range to.RowCounts {
		tableSet[tbl] = struct{}{}
	}

	tableNames := make([]string, 0, len(tableSet))
	for tbl := range tableSet {
		tableNames = append(tableNames, tbl)
	}
	sort.Strings(tableNames)

	deltas := make([]TableDelta, 0, len(tableNames))
	for _, tbl := range tableNames {
		fromRows := from.RowCounts[tbl]
		toRows := to.RowCounts[tbl]
		deltas = append(deltas, TableDelta{
			Table:    tbl,
			FromRows: fromRows,
			ToRows:   toRows,
			Delta:    toRows - fromRows,
		})
	}

	return SnapshotDiff{
		From:   from,
		To:     to,
		Tables: deltas,
	}, nil
}
