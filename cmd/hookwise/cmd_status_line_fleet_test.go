package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
)

// seedFleetDB returns an open Analytics over a temp DB plus its path.
func seedFleetDB(t *testing.T) (*analytics.Analytics, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "analytics.db")
	db, err := analytics.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return analytics.NewAnalytics(db), path
}

var fleetNow = time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

const fleetStale = 15 * time.Minute

func TestFleetBadge_SoloSuppressed(t *testing.T) {
	a, _ := seedFleetDB(t)
	ctx := context.Background()
	// A single live session is not a fleet — a "run:1" badge would be pure noise.
	require.NoError(t, a.StartSession(ctx, "solo", fleetNow.Add(-1*time.Minute)))

	assert.Equal(t, "", fleetBadge(a, fleetNow, fleetStale))
}

func TestFleetBadge_Empty(t *testing.T) {
	a, _ := seedFleetDB(t)
	assert.Equal(t, "", fleetBadge(a, fleetNow, fleetStale))
}

func TestFleetBadge_TwoRunning(t *testing.T) {
	a, _ := seedFleetDB(t)
	ctx := context.Background()
	require.NoError(t, a.StartSession(ctx, "s1", fleetNow.Add(-1*time.Minute)))
	require.NoError(t, a.StartSession(ctx, "s2", fleetNow.Add(-2*time.Minute)))

	got := fleetBadge(a, fleetNow, fleetStale)
	assert.Contains(t, got, "fleet ")
	assert.Contains(t, got, "run:2")
}

func TestFleetBadge_RunningPlusDone(t *testing.T) {
	a, _ := seedFleetDB(t)
	ctx := context.Background()
	require.NoError(t, a.StartSession(ctx, "live", fleetNow.Add(-1*time.Minute)))
	require.NoError(t, a.StartSession(ctx, "fin", fleetNow.Add(-10*time.Minute)))
	require.NoError(t, a.EndSession(ctx, "fin", fleetNow.Add(-2*time.Minute), analytics.SessionStats{}))

	got := fleetBadge(a, fleetNow, fleetStale)
	assert.Contains(t, got, "run:1")
	assert.Contains(t, got, "done:1")
}

// A crashed/stale session is excluded, dropping the live fleet back to 1 -> the
// badge suppresses (solo).
func TestFleetBadge_StaleDropsBelowFleet(t *testing.T) {
	a, _ := seedFleetDB(t)
	ctx := context.Background()
	require.NoError(t, a.StartSession(ctx, "live", fleetNow.Add(-1*time.Minute)))
	require.NoError(t, a.StartSession(ctx, "crashed", fleetNow.Add(-3*time.Hour)))

	assert.Equal(t, "", fleetBadge(a, fleetNow, fleetStale))
}

// renderFleetSegment opens by path and is fail-open on a bad/missing DB.
func TestRenderFleetSegment_FailOpenOnBadPath(t *testing.T) {
	assert.Equal(t, "", renderFleetSegment(filepath.Join(t.TempDir(), "does-not-exist.db"), fleetNow, fleetStale))
}
