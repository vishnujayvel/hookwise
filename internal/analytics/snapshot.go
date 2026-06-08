package analytics

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// snapshotTimeFormat is the colon-free, lexicographically sortable UTC timestamp
// layout used for snapshot filenames (e.g. "20060102T150405Z.db"). Colons are
// avoided so the names are valid on every filesystem and sort by time.
const snapshotTimeFormat = "20060102T150405Z"

// snapshotFileRe matches snapshot filenames produced by Snapshot. Only files
// matching this pattern are enumerated or pruned, so unrelated files in the
// snapshots directory are never touched.
var snapshotFileRe = regexp.MustCompile(`^\d{8}T\d{6}Z\.db$`)

// DefaultSnapshotsDir returns the conventional directory for analytics snapshots
// (~/.hookwise/snapshots), mirroring DefaultDBPath.
func DefaultSnapshotsDir() string {
	return filepath.Join(core.HomeDir(), ".hookwise", "snapshots")
}

// Snapshot writes a consistent point-in-time copy of the analytics database to
// snapshotsDir using SQLite's "VACUUM INTO". The destination filename is the
// current UTC time formatted as snapshotTimeFormat plus a ".db" suffix.
//
// The snapshots directory is created (0700) if missing. VACUUM INTO fails if the
// destination already exists; on a same-second collision the call retries with a
// numeric suffix ("-1", "-2", …) so two snapshots in the same second both
// succeed. Returns the absolute path of the snapshot written.
func (d *DB) Snapshot(ctx context.Context, snapshotsDir string) (string, error) {
	if snapshotsDir == "" {
		snapshotsDir = DefaultSnapshotsDir()
	}
	absDir, err := filepath.Abs(snapshotsDir)
	if err != nil {
		return "", fmt.Errorf("analytics: snapshot abs dir: %w", err)
	}
	if err := os.MkdirAll(absDir, 0o700); err != nil {
		return "", fmt.Errorf("analytics: snapshot mkdir %s: %w", absDir, err)
	}

	base := time.Now().UTC().Format(snapshotTimeFormat)
	dest := filepath.Join(absDir, base+".db")

	// Same-second collision handling: VACUUM INTO refuses to overwrite an
	// existing file, so probe for a free name before issuing the statement.
	for i := 1; ; i++ {
		if _, statErr := os.Stat(dest); os.IsNotExist(statErr) {
			break
		}
		dest = filepath.Join(absDir, fmt.Sprintf("%s-%d.db", base, i))
		if i > 1000 {
			return "", fmt.Errorf("analytics: snapshot: could not find free filename in %s", absDir)
		}
	}

	// VACUUM INTO does not accept a bound parameter for the path; the path is a
	// single-quoted SQL string literal. dest is derived from our own timestamp
	// format and absDir, so it contains no single quotes — but escape defensively.
	quoted := "'" + escapeSQLLiteral(dest) + "'"
	if _, err := d.db.ExecContext(ctx, "VACUUM INTO "+quoted); err != nil {
		return "", fmt.Errorf("analytics: VACUUM INTO %s: %w", dest, err)
	}

	return dest, nil
}

// escapeSQLLiteral doubles single quotes for safe inclusion in a SQL string
// literal.
func escapeSQLLiteral(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\'' {
			out = append(out, '\'')
		}
		out = append(out, r)
	}
	return string(out)
}

// ListSnapshots returns the absolute paths of existing snapshot files in
// snapshotsDir that match the snapshot naming pattern, sorted oldest → newest.
// Because the timestamp filename format is lexicographically ordered by time, a
// plain string sort yields chronological order. A missing directory yields an
// empty slice and no error.
func ListSnapshots(snapshotsDir string) ([]string, error) {
	if snapshotsDir == "" {
		snapshotsDir = DefaultSnapshotsDir()
	}
	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("analytics: list snapshots: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if snapshotFileRe.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // oldest → newest (timestamp format sorts chronologically)

	paths := make([]string, len(names))
	for i, n := range names {
		paths[i] = filepath.Join(snapshotsDir, n)
	}
	return paths, nil
}

// PruneSnapshots deletes the oldest snapshot files in snapshotsDir beyond the
// most recent keep, touching only files that match the snapshot naming pattern.
// It returns the absolute paths of the files removed. A keep of <= 0 prunes
// nothing (treated as "retain all") to avoid accidentally deleting every
// snapshot from a misconfigured retention value.
func PruneSnapshots(snapshotsDir string, keep int) ([]string, error) {
	if keep <= 0 {
		return nil, nil
	}

	snaps, err := ListSnapshots(snapshotsDir)
	if err != nil {
		return nil, err
	}
	if len(snaps) <= keep {
		return nil, nil
	}

	// snaps is oldest → newest; the prefix before the last `keep` are removed.
	toPrune := snaps[:len(snaps)-keep]
	var pruned []string
	for _, p := range toPrune {
		if err := os.Remove(p); err != nil {
			return pruned, fmt.Errorf("analytics: prune snapshot %s: %w", p, err)
		}
		pruned = append(pruned, p)
	}
	return pruned, nil
}
