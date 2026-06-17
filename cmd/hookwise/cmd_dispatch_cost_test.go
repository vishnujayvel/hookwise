package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// writeSonnetFixture writes a .jsonl transcript with one assistant line:
// claude-sonnet-4, input_tokens=1_000_000, output_tokens=1_000_000.
// At built-in Sonnet rates ($3/MTok input, $15/MTok output) this costs
// exactly $18.00 USD.
func writeSonnetFixture(t *testing.T) string {
	t.Helper()
	line := `{"type":"assistant","message":{"id":"msg_01","model":"claude-sonnet-4","role":"assistant","usage":{"input_tokens":1000000,"output_tokens":1000000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}},"timestamp":"2026-06-15T00:00:00Z"}`
	f, err := os.CreateTemp(t.TempDir(), "transcript-*.jsonl")
	require.NoError(t, err)
	_, err = f.WriteString(line + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// openTestDB opens (or creates) the analytics DB in dataDir so the test can
// assert post-call state without duplicating the open logic in recordAnalytics.
func openTestDB(t *testing.T, dataDir string) *analytics.DB {
	t.Helper()
	db, err := analytics.Open(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

const expectedSonnetCost = 18.00 // $3*1 + $15*1 (1M tokens each, Sonnet rates)

// TestRecordAnalytics_CostStop verifies that calling recordAnalytics with a
// Stop event and a real transcript path writes the expected cost into both
// the cost_state table and the sessions table.
func TestRecordAnalytics_CostStop(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))
	dbPath := filepath.Join(tmpDir, "analytics.db")

	transcriptPath := writeSonnetFixture(t)
	sid := "cost-test-session-001"

	// Pre-create the session so EndSession's UPDATE has a row to touch.
	db := openTestDB(t, dbPath)
	a := analytics.NewAnalytics(db)
	require.NoError(t, a.StartSession(context.Background(), sid, time.Now()))
	db.Close()

	costCfg := core.CostTrackingConfig{Enabled: true}
	payload := core.HookPayload{
		SessionID:      sid,
		TranscriptPath: transcriptPath,
	}
	recordAnalytics(context.Background(), core.EventStop, payload, dbPath, costCfg)

	// Re-open to assert.
	db = openTestDB(t, dbPath)
	defer db.Close()

	// Assert cost_state.TotalToday and SessionCosts[sid].
	state, err := db.ReadCostState(context.Background())
	require.NoError(t, err)
	assert.InDelta(t, expectedSonnetCost, state.TotalToday, 0.001, "TotalToday should equal $18.00")
	assert.InDelta(t, expectedSonnetCost, state.SessionCosts[sid], 0.001, "SessionCosts[sid] should equal $18.00")

	// Assert the sessions table also holds the cost via DailySummary.
	a = analytics.NewAnalytics(db)
	today := time.Now().UTC().Format("2006-01-02")
	summary, err := a.DailySummary(context.Background(), today)
	require.NoError(t, err)
	assert.InDelta(t, expectedSonnetCost, summary.EstimatedCostUSD, 0.001, "DailySummary.EstimatedCostUSD should equal $18.00")
}

// TestRecordAnalytics_CostStop_Idempotent verifies that re-running a Stop for
// the same session does NOT double-count cost (delta = 0, TotalToday unchanged).
func TestRecordAnalytics_CostStop_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))
	dbPath := filepath.Join(tmpDir, "analytics.db")

	transcriptPath := writeSonnetFixture(t)
	sid := "cost-test-session-002"

	db := openTestDB(t, dbPath)
	a := analytics.NewAnalytics(db)
	require.NoError(t, a.StartSession(context.Background(), sid, time.Now()))
	db.Close()

	costCfg := core.CostTrackingConfig{Enabled: true}
	payload := core.HookPayload{
		SessionID:      sid,
		TranscriptPath: transcriptPath,
	}

	// First Stop.
	recordAnalytics(context.Background(), core.EventStop, payload, dbPath, costCfg)

	// Second Stop — same transcript, same session.
	recordAnalytics(context.Background(), core.EventStop, payload, dbPath, costCfg)

	db = openTestDB(t, dbPath)
	defer db.Close()

	state, err := db.ReadCostState(context.Background())
	require.NoError(t, err)
	assert.InDelta(t, expectedSonnetCost, state.TotalToday, 0.001,
		"TotalToday must NOT be doubled on second Stop (idempotency)")
}

// TestRecordAnalytics_CostStop_TranscriptGrows is the per-turn reality: Stop
// fires once per turn against the SAME session whose transcript GROWS each turn.
// The idempotent test only proves "same file twice => no change"; this proves
// the stronger property the design depends on -- recompute-from-full-file plus a
// delta against the prior stored cost yields the correct INCREMENT, never a
// double-count of earlier turns. Turn 1 = $18, transcript doubles, turn 2 must
// land at $36 total (delta +$18), not $54 (naive add of the full recompute) and
// not $18 (no update).
func TestRecordAnalytics_CostStop_TranscriptGrows(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))
	dbPath := filepath.Join(tmpDir, "analytics.db")

	transcriptPath := writeSonnetFixture(t) // one $18 assistant line
	sid := "cost-test-session-grow"

	db := openTestDB(t, dbPath)
	a := analytics.NewAnalytics(db)
	require.NoError(t, a.StartSession(context.Background(), sid, time.Now()))
	db.Close()

	costCfg := core.CostTrackingConfig{Enabled: true}
	payload := core.HookPayload{SessionID: sid, TranscriptPath: transcriptPath}

	// Turn 1: single line => $18.
	recordAnalytics(context.Background(), core.EventStop, payload, dbPath, costCfg)

	// The transcript grows by one more identical assistant line (another $18)
	// before the next turn's Stop, exactly as a live session accumulates usage.
	line := `{"type":"assistant","message":{"id":"msg_02","model":"claude-sonnet-4","role":"assistant","usage":{"input_tokens":1000000,"output_tokens":1000000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}},"timestamp":"2026-06-15T00:01:00Z"}`
	f, err := os.OpenFile(transcriptPath, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(line + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Turn 2: two lines => $36 recomputed; delta +$18 applied.
	recordAnalytics(context.Background(), core.EventStop, payload, dbPath, costCfg)

	db = openTestDB(t, dbPath)
	defer db.Close()

	state, err := db.ReadCostState(context.Background())
	require.NoError(t, err)
	assert.InDelta(t, 2*expectedSonnetCost, state.TotalToday, 0.001,
		"TotalToday must equal the full $36 recompute, not double-count turn 1")
	assert.InDelta(t, 2*expectedSonnetCost, state.SessionCosts[sid], 0.001,
		"SessionCosts[sid] must be overwritten with the full recomputed $36")

	a = analytics.NewAnalytics(db)
	today := time.Now().UTC().Format("2006-01-02")
	summary, err := a.DailySummary(context.Background(), today)
	require.NoError(t, err)
	assert.InDelta(t, 2*expectedSonnetCost, summary.EstimatedCostUSD, 0.001,
		"DailySummary.EstimatedCostUSD must reflect the grown-transcript recompute")
}

// TestRecordAnalytics_CostStop_NegativeDelta verifies that when a session's
// recomputed cost is LOWER than its previously stored SessionCosts (reachable
// via a lowered CostTracking.Rates override between two Stops, or a truncated/
// rotated transcript), the non-negative clamp is applied CONSISTENTLY: both
// TotalToday and DailyCosts[today] floor at 0 and stay equal. The bug this guards
// against: TotalToday was clamped while DailyCosts[today] was not, letting the two
// redundant views of "today's total" diverge permanently (DailyCosts going
// negative, so later increments dig out from a negative floor).
func TestRecordAnalytics_CostStop_NegativeDelta(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))
	dbPath := filepath.Join(tmpDir, "analytics.db")

	transcriptPath := writeSonnetFixture(t) // recomputes to $18
	sid := "cost-test-session-negdelta"
	today := time.Now().Format("2006-01-02") // local date, matching the closure

	// Pre-create the session and seed a cost state where this session is already
	// recorded at $36 (higher than the $18 transcript will recompute to) while
	// today's running totals sit at only $2 — so the -$18 delta drives TotalToday
	// below zero and trips the clamp.
	db := openTestDB(t, dbPath)
	a := analytics.NewAnalytics(db)
	require.NoError(t, a.StartSession(context.Background(), sid, time.Now()))
	require.NoError(t, db.WriteCostState(context.Background(), &analytics.CostState{
		DailyCosts:   map[string]float64{today: 2.0},
		SessionCosts: map[string]float64{sid: 36.0},
		Today:        today,
		TotalToday:   2.0,
	}))
	db.Close()

	costCfg := core.CostTrackingConfig{Enabled: true}
	payload := core.HookPayload{SessionID: sid, TranscriptPath: transcriptPath}
	recordAnalytics(context.Background(), core.EventStop, payload, dbPath, costCfg)

	db = openTestDB(t, dbPath)
	defer db.Close()

	state, err := db.ReadCostState(context.Background())
	require.NoError(t, err)
	assert.InDelta(t, 0.0, state.TotalToday, 0.001,
		"TotalToday must clamp to 0 on a negative delta")
	assert.InDelta(t, 0.0, state.DailyCosts[today], 0.001,
		"DailyCosts[today] must clamp to 0 too, not go negative")
	assert.InDelta(t, state.TotalToday, state.DailyCosts[today], 0.001,
		"TotalToday and DailyCosts[today] must stay equal (consistent daily total)")
	assert.InDelta(t, expectedSonnetCost, state.SessionCosts[sid], 0.001,
		"SessionCosts[sid] must be overwritten with the recomputed $18")
}

// TestRecordAnalytics_CostStop_Disabled verifies that when CostTracking.Enabled=false,
// no cost is written and TotalToday stays 0.
func TestRecordAnalytics_CostStop_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))
	dbPath := filepath.Join(tmpDir, "analytics.db")

	transcriptPath := writeSonnetFixture(t)
	sid := "cost-test-session-003"

	db := openTestDB(t, dbPath)
	a := analytics.NewAnalytics(db)
	require.NoError(t, a.StartSession(context.Background(), sid, time.Now()))
	db.Close()

	costCfg := core.CostTrackingConfig{Enabled: false}
	payload := core.HookPayload{
		SessionID:      sid,
		TranscriptPath: transcriptPath,
	}
	recordAnalytics(context.Background(), core.EventStop, payload, dbPath, costCfg)

	db = openTestDB(t, dbPath)
	defer db.Close()

	state, err := db.ReadCostState(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0.0, state.TotalToday, "TotalToday must remain 0 when cost tracking disabled")
	assert.Empty(t, state.SessionCosts, "SessionCosts must be empty when cost tracking disabled")
}

// TestRecordAnalytics_CostStop_NoTranscript verifies that an empty TranscriptPath
// does not panic, EndSession is still called (session ends cleanly), and cost is 0.
func TestRecordAnalytics_CostStop_NoTranscript(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))
	dbPath := filepath.Join(tmpDir, "analytics.db")

	sid := "cost-test-session-004"

	db := openTestDB(t, dbPath)
	a := analytics.NewAnalytics(db)
	require.NoError(t, a.StartSession(context.Background(), sid, time.Now()))
	db.Close()

	costCfg := core.CostTrackingConfig{Enabled: true}
	payload := core.HookPayload{
		SessionID:      sid,
		TranscriptPath: "", // no transcript path
	}

	// Must not panic.
	require.NotPanics(t, func() {
		recordAnalytics(context.Background(), core.EventStop, payload, dbPath, costCfg)
	})

	db = openTestDB(t, dbPath)
	defer db.Close()

	state, err := db.ReadCostState(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0.0, state.TotalToday, "TotalToday must be 0 with no transcript")

	// Session should still have been ended with cost=0.
	a = analytics.NewAnalytics(db)
	today := time.Now().UTC().Format("2006-01-02")
	summary, err := a.DailySummary(context.Background(), today)
	require.NoError(t, err)
	assert.Equal(t, 0.0, summary.EstimatedCostUSD, "EstimatedCostUSD must be 0 with no transcript")
}

// TestRecordAnalytics_CostStop_RatesOverride verifies the CostTrackingConfig.Rates
// override flows end-to-end through the Stop seam: config -> ComputeWithRates
// (cmd_dispatch.go:211) -> recorded cost state. pricing_test.go covers
// ComputeWithRates in isolation, but nothing proves dispatch actually PASSES
// costCfg.Rates through. A refactor that dropped the Rates arg (passing nil) would
// pass every pricing unit test while silently disabling user rate overrides -- the
// dead-feature class this whole cost port exists to prevent. Override sonnet.input
// $3 -> $99: the $18 fixture ($3 in + $15 out, 1M each) must now record $114
// ($99 in + $15 out), not the built-in $18.
func TestRecordAnalytics_CostStop_RatesOverride(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))
	dbPath := filepath.Join(tmpDir, "analytics.db")

	transcriptPath := writeSonnetFixture(t)
	sid := "cost-test-session-rates"

	db := openTestDB(t, dbPath)
	a := analytics.NewAnalytics(db)
	require.NoError(t, a.StartSession(context.Background(), sid, time.Now()))
	db.Close()

	const overriddenCost = 114.00 // $99 input (overridden from $3) + $15 output
	costCfg := core.CostTrackingConfig{
		Enabled: true,
		Rates:   map[string]float64{"sonnet.input": 99.0},
	}
	payload := core.HookPayload{SessionID: sid, TranscriptPath: transcriptPath}
	recordAnalytics(context.Background(), core.EventStop, payload, dbPath, costCfg)

	db = openTestDB(t, dbPath)
	defer db.Close()

	state, err := db.ReadCostState(context.Background())
	require.NoError(t, err)
	assert.InDelta(t, overriddenCost, state.SessionCosts[sid], 0.001,
		"override sonnet.input=$99 must reach SessionCosts[sid]; default $18 means Rates was not passed through")
	assert.InDelta(t, overriddenCost, state.TotalToday, 0.001,
		"override must also flow into TotalToday")
}
