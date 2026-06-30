package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
)

// TestCheckCostHonesty_SessionsButZeroCost is the doctor-honesty regression:
// a session was recorded today but $0 cost was computed for it. This is the
// exact signature of a dead cost writer (the failure that kept `hookwise stats`
// and the cost status-line segment silently at $0). doctor must WARN, not pass
// silently. A started-but-never-costed session reproduces it directly — no
// EndSession, so estimated_cost_usd stays 0.
func TestCheckCostHonesty_SessionsButZeroCost(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	db := openTestDB(t, dbPath)
	a := analytics.NewAnalytics(db)
	require.NoError(t, a.StartSession(context.Background(), "cost-honesty-zero", time.Now().UTC()))
	db.Close()

	buf := &bytes.Buffer{}
	// Cost tracking ENABLED: $0-despite-sessions is the dead-writer signal → WARN.
	warnings := checkCostHonesty(buf, dbPath, true)

	out := buf.String()
	assert.Equal(t, 1, warnings, "a recorded session with $0 cost (tracking enabled) must produce exactly one warning")
	assert.Contains(t, out, "WARN  cost:")
	assert.Contains(t, out, "$0.00 computed",
		"warning must name the zero-cost-despite-sessions condition")
}

// TestCheckCostHonesty_DisabledTrackingZeroCost is the regression for the
// false-positive "cost tracking may be dead" warning: with cost tracking
// DISABLED (the default), a $0 total despite recorded sessions is expected, not
// a malfunction. doctor must report a benign INFO, not a WARN.
func TestCheckCostHonesty_DisabledTrackingZeroCost(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	db := openTestDB(t, dbPath)
	a := analytics.NewAnalytics(db)
	require.NoError(t, a.StartSession(context.Background(), "cost-disabled-zero", time.Now().UTC()))
	db.Close()

	buf := &bytes.Buffer{}
	warnings := checkCostHonesty(buf, dbPath, false) // tracking disabled

	out := buf.String()
	assert.Equal(t, 0, warnings, "disabled cost tracking with $0 must not warn")
	assert.Contains(t, out, "INFO  cost: tracking disabled",
		"disabled tracking must be reported as benign INFO")
	assert.NotContains(t, out, "WARN", "disabled tracking must never emit a cost WARN")
}

// TestCheckCostHonesty_SessionsWithCost verifies the healthy path: sessions
// recorded AND non-zero cost computed → PASS, no warning. This is the
// discriminating negative — it must NOT fire the dead-writer warning when the
// writer is alive.
func TestCheckCostHonesty_SessionsWithCost(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	db := openTestDB(t, dbPath)
	a := analytics.NewAnalytics(db)
	ctx := context.Background()
	require.NoError(t, a.StartSession(ctx, "cost-honesty-live", time.Now().UTC()))
	require.NoError(t, a.EndSession(ctx, "cost-honesty-live", time.Now().UTC(),
		analytics.SessionStats{EstimatedCostUSD: 12.34}))
	db.Close()

	buf := &bytes.Buffer{}
	warnings := checkCostHonesty(buf, dbPath, true)

	out := buf.String()
	assert.Equal(t, 0, warnings, "a session with non-zero cost must not warn")
	assert.Contains(t, out, "PASS  cost: $12.34 across 1 session(s) today")
	assert.NotContains(t, out, "WARN")
}

// TestCheckCostHonesty_NoSessions verifies that an empty/idle install (no
// sessions today) is INFO, not WARN — $0 with zero sessions is genuinely "no
// work yet", not a dead writer. Warning here would be a false-positive nag on
// every fresh install.
func TestCheckCostHonesty_NoSessions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	db := openTestDB(t, dbPath)
	db.Close() // schema created, but no sessions recorded

	buf := &bytes.Buffer{}
	warnings := checkCostHonesty(buf, dbPath, false)

	out := buf.String()
	assert.Equal(t, 0, warnings, "no sessions today must not warn")
	assert.Contains(t, out, "INFO  cost: no sessions recorded today yet")
	assert.NotContains(t, out, "WARN")
}

// TestCheckCostHonesty_NoDB verifies graceful no-op when the analytics DB does
// not yet exist — the analytics check already reports DB absence, so the cost
// check must stay silent (zero output, zero warnings) rather than double-report
// or crash.
func TestCheckCostHonesty_NoDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "does-not-exist.db")

	buf := &bytes.Buffer{}
	warnings := checkCostHonesty(buf, dbPath, false)

	assert.Equal(t, 0, warnings)
	assert.Empty(t, buf.String(), "absent DB must produce no cost output (analytics check owns that report)")
}
