package main

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestCheckDispatchLatency_WarnsOverThreshold seeds a 7-day window whose
// average dispatch latency exceeds the 50ms threshold and expects exactly one
// WARN.
func TestCheckDispatchLatency_WarnsOverThreshold(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	// avg(60,80,100) = 80ms > 50ms.
	seedLatencyEvents(t, dbPath, time.Now().UTC(), 60, 80, 100)

	buf := &bytes.Buffer{}
	warnings := checkDispatchLatency(buf, dbPath)

	out := buf.String()
	assert.Equal(t, 1, warnings, "avg over threshold must produce exactly one warning")
	assert.Contains(t, out, "WARN  latency:")
	assert.Contains(t, out, "80.0ms", "warning must state the measured average")
	assert.Contains(t, out, "exceeds 50ms")
}

// TestCheckDispatchLatency_PassesUnderThreshold is the discriminating
// negative: a healthy average must PASS, not warn.
func TestCheckDispatchLatency_PassesUnderThreshold(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	// avg(10,20,30) = 20ms <= 50ms.
	seedLatencyEvents(t, dbPath, time.Now().UTC(), 10, 20, 30)

	buf := &bytes.Buffer{}
	warnings := checkDispatchLatency(buf, dbPath)

	out := buf.String()
	assert.Equal(t, 0, warnings, "healthy average must not warn")
	assert.Contains(t, out, "PASS  latency: avg dispatch 20.0ms")
	assert.NotContains(t, out, "WARN")
}

// TestCheckDispatchLatency_IgnoresOldEvents verifies the 7-day window: slow
// dispatches older than 7 days must not trigger the warning (and with nothing
// recent, the check stays silent).
func TestCheckDispatchLatency_IgnoresOldEvents(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	// Very slow, but 8 days ago — outside the window.
	seedLatencyEvents(t, dbPath, time.Now().UTC().AddDate(0, 0, -8), 500, 900)

	buf := &bytes.Buffer{}
	warnings := checkDispatchLatency(buf, dbPath)

	assert.Equal(t, 0, warnings, "events outside the 7-day window must not warn")
	assert.Empty(t, buf.String(), "no in-window data must produce no output")
}

// TestCheckDispatchLatency_NoLatencyDataSilent is the disabled-subsystem
// honesty test (#222/#223 precedent): a DB whose events all predate the
// dispatch_latency_ms column (NULL rows only) has no measurements — the check
// must emit nothing rather than warn or fabricate a PASS.
func TestCheckDispatchLatency_NoLatencyDataSilent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	db := openTestDB(t, dbPath) // schema created, zero events
	db.Close()

	buf := &bytes.Buffer{}
	warnings := checkDispatchLatency(buf, dbPath)

	assert.Equal(t, 0, warnings)
	assert.Empty(t, buf.String(), "absent latency data must produce no output")
}

// TestCheckDispatchLatency_NoDB verifies graceful no-op when the analytics DB
// does not exist — the analytics check (Check 3) owns that report.
func TestCheckDispatchLatency_NoDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "does-not-exist.db")

	buf := &bytes.Buffer{}
	warnings := checkDispatchLatency(buf, dbPath)

	assert.Equal(t, 0, warnings)
	assert.Empty(t, buf.String())
}
