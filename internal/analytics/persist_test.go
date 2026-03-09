package analytics

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPersistAcrossConnections(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dolt-persist-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// Connection 1: write + commit + close
	db1, err := Open(tmpDir)
	require.NoError(t, err)
	a1 := NewAnalytics(db1)
	require.NoError(t, a1.StartSession(ctx, "persist-001", time.Now()))
	hash, err := db1.CommitDispatch(ctx, "SessionStart", "persist-001")
	t.Logf("Commit hash: %q, err: %v", hash, err)

	// Use UTC date to match StartSession's UTC storage format.
	today := time.Now().UTC().Format("2006-01-02")

	// Verify data is visible on SAME connection before closing
	s1, err := a1.DailySummary(ctx, today)
	require.NoError(t, err)
	t.Logf("Same-connection sessions: %d", s1.TotalSessions)
	require.GreaterOrEqual(t, s1.TotalSessions, 1, "should be visible on same connection")

	db1.Close()

	// Connection 2: read
	db2, err := Open(tmpDir)
	require.NoError(t, err)
	defer db2.Close()
	a2 := NewAnalytics(db2)
	summary, err := a2.DailySummary(ctx, today)
	require.NoError(t, err)
	t.Logf("Cross-connection sessions: %d", summary.TotalSessions)
	require.GreaterOrEqual(t, summary.TotalSessions, 1, "data should persist across connections")
}
