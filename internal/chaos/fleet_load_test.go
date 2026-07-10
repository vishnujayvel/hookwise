//go:build integration

package chaos

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
)

// =========================================================================
// Fleet-load: ARCH-2 single-writer SQLite under multi-session concurrency
// =========================================================================
//
// ARCH-2 pins each hookwise process to a single writer connection
// (SetMaxOpenConns(1)) on a WAL-mode SQLite database. That guarantees
// serialization *within* one process, but the real deployment is a fleet:
// the owner routinely runs 5+ concurrent Claude Code sessions, each
// dispatching hookwise hooks as a separate process with its own
// single-writer connection to the SAME analytics.db. Cross-process
// contention is arbitrated only by WAL + busy_timeout(5000).
//
// These tests model that fleet at the DB layer: N goroutine "sessions",
// each with its OWN *analytics.DB handle (own sql.DB pool capped at 1,
// exactly like a separate process), interleaving inserts (events,
// StartSession) and updates (EndSession upserts) against one shared
// database file. Unlike dispatch-path tests, errors here are captured
// directly from the writer API — ARCH-1 fail-open would otherwise
// swallow them and hide lost writes.

const (
	fleetSessions    = 8                // concurrent "Claude Code sessions"
	writesPerSession = 50               // interleaved analytics writes per session
	fleetLoadBudget  = 30 * time.Second // liveness canary, not a benchmark
)

// fleetErrors collects writer/reader errors across goroutines.
type fleetErrors struct {
	mu   sync.Mutex
	errs []string
}

func (f *fleetErrors) add(format string, args ...interface{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs = append(f.errs, fmt.Sprintf(format, args...))
}

func (f *fleetErrors) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.errs...)
}

// runFleetSession simulates one Claude Code session's analytics traffic:
// a StartSession insert, then writesPerSession RecordEvent inserts with a
// periodic EndSession upsert mixed in (first fire hits the UPDATE path of
// the ON CONFLICT clause, exercising insert+update interleaving).
func runFleetSession(ctx context.Context, t *testing.T, dbPath string, sessionIdx int, errs *fleetErrors) {
	t.Helper()

	// Own handle = own single-writer connection, like a separate process.
	db, err := analytics.Open(dbPath)
	if err != nil {
		errs.add("session %d: open: %v", sessionIdx, err)
		return
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			errs.add("session %d: close: %v", sessionIdx, cerr)
		}
	}()

	a := analytics.NewAnalytics(db)
	sessionID := fmt.Sprintf("fleet-session-%03d", sessionIdx)
	now := time.Now().UTC()

	if err := a.StartSession(ctx, sessionID, now); err != nil {
		errs.add("session %d: start: %v", sessionIdx, err)
		return
	}

	for i := 0; i < writesPerSession; i++ {
		event := analytics.EventRecord{
			EventType:         "PostToolUse",
			ToolName:          fmt.Sprintf("Tool%d", i%5),
			Timestamp:         now,
			FilePath:          fmt.Sprintf("/tmp/fleet/%d/%d.go", sessionIdx, i),
			LinesAdded:        i,
			LinesRemoved:      i % 3,
			AIConfidenceScore: 0.5,
		}
		if err := a.RecordEvent(ctx, sessionID, event); err != nil {
			errs.add("session %d: event %d: %v", sessionIdx, i, err)
		}

		// Every 10th write, upsert session stats — the sessions row already
		// exists from StartSession, so this hits the UPDATE conflict path.
		if i%10 == 9 {
			stats := analytics.SessionStats{
				TotalToolCalls: i + 1,
				FileEditsCount: (i + 1) / 2,
			}
			if err := a.EndSession(ctx, sessionID, now, stats); err != nil {
				errs.add("session %d: end-session at %d: %v", sessionIdx, i, err)
			}
		}
	}
}

// runFleetLoad drives fleetSessions concurrent writer goroutines and,
// optionally, a stats-style reader loop running for the whole write window.
// It returns the total wall-clock time of the write phase.
func runFleetLoad(t *testing.T, withReader bool) time.Duration {
	t.Helper()

	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmpDir)
	dbPath := filepath.Join(tmpDir, "analytics.db")
	ctx := context.Background()

	// Create the schema once before the fleet piles on, mirroring reality:
	// the fleet always joins an already-initialized database.
	seed, err := analytics.Open(dbPath)
	require.NoError(t, err, "initial open must succeed")
	require.NoError(t, seed.Close())

	errs := &fleetErrors{}
	start := time.Now()

	var readerWG sync.WaitGroup
	readerDone := make(chan struct{})
	var readerIterations int
	if withReader {
		readerWG.Add(1)
		go func() {
			defer readerWG.Done()
			rdb, rerr := analytics.Open(dbPath)
			if rerr != nil {
				errs.add("reader: open: %v", rerr)
				return
			}
			defer func() { _ = rdb.Close() }()
			ra := analytics.NewAnalytics(rdb)
			today := time.Now().UTC().Format("2006-01-02")
			for {
				select {
				case <-readerDone:
					return
				default:
				}
				// Stats-style queries, same shapes `hookwise stats` runs.
				if _, qerr := ra.DailySummary(ctx, today); qerr != nil {
					errs.add("reader: daily summary: %v", qerr)
				}
				if _, qerr := ra.ToolBreakdown(ctx, today); qerr != nil {
					errs.add("reader: tool breakdown: %v", qerr)
				}
				readerIterations++
			}
		}()
	}

	var writerWG sync.WaitGroup
	for s := 0; s < fleetSessions; s++ {
		writerWG.Add(1)
		go func(idx int) {
			defer writerWG.Done()
			runFleetSession(ctx, t, dbPath, idx, errs)
		}(s)
	}
	writerWG.Wait()
	elapsed := time.Since(start)

	if withReader {
		close(readerDone)
		readerWG.Wait()
		assert.Positive(t, readerIterations,
			"reader loop should complete at least one stats pass during the write window")
	}

	// Zero tolerated errors: fail-open isn't in play here, every writer and
	// reader error was captured at the API boundary.
	assert.Empty(t, errs.list(), "fleet load surfaced writer/reader errors")

	// No lost writes: every event from every session must be present.
	verify, err := analytics.Open(dbPath)
	require.NoError(t, err, "verification open must succeed")
	defer func() { _ = verify.Close() }()

	var eventCount int
	require.NoError(t, verify.QueryRow(ctx,
		"SELECT COUNT(*) FROM events").Scan(&eventCount))
	assert.Equal(t, fleetSessions*writesPerSession, eventCount,
		"lost writes: expected all events from all sessions to be persisted")

	var sessionCount int
	require.NoError(t, verify.QueryRow(ctx,
		"SELECT COUNT(*) FROM sessions").Scan(&sessionCount))
	assert.Equal(t, fleetSessions, sessionCount,
		"every fleet session should have exactly one sessions row (upserts must not duplicate)")

	// Liveness canary: WAL + busy_timeout must arbitrate this load well
	// inside the budget. Blowing it means lock convoying or starvation.
	assert.Less(t, elapsed, fleetLoadBudget,
		"fleet write load exceeded liveness budget")

	return elapsed
}

// TestChaos_FleetLoadSingleWriterDB proves ARCH-2 survives fleet-style
// concurrency: 8 simulated sessions (one single-writer connection each)
// interleaving inserts and updates against one shared analytics DB, with
// zero surfaced errors, zero lost writes, and bounded wall-clock.
func TestChaos_FleetLoadSingleWriterDB(t *testing.T) {
	elapsed := runFleetLoad(t, false)
	t.Logf("fleet load (%d sessions × %d writes) completed in %v",
		fleetSessions, writesPerSession, elapsed)
}

// TestChaos_FleetLoadWithConcurrentReader adds a continuous stats-style
// reader (DailySummary + ToolBreakdown, the queries behind `hookwise
// stats`) running against the same DB for the entire write window. Under
// WAL, readers must neither fail nor block the writer fleet.
func TestChaos_FleetLoadWithConcurrentReader(t *testing.T) {
	elapsed := runFleetLoad(t, true)
	t.Logf("fleet load with concurrent reader completed in %v", elapsed)
}
