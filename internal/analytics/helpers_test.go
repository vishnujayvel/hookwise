package analytics

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// testOpen creates a temporary directory, opens a fresh SQLite analytics DB
// (as an analytics.db file inside it), and returns the DB plus a cleanup
// function.
func testOpen(t *testing.T) (*DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "hookwise-analytics-test-*")
	require.NoError(t, err)

	db, err := Open(filepath.Join(tmpDir, "analytics.db"))
	require.NoError(t, err, "Open should succeed on a fresh temp dir")

	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return db, cleanup
}
