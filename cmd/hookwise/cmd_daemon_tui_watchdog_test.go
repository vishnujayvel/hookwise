package main

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test 1 — the core reaper policy: among grace-passed procs, kill runaways and
// every healthy DUPLICATE, keeping exactly the newest healthy one.
func Test_reapTUIs_KillsRunawayAndStaleDuplicate_KeepsNewest(t *testing.T) {
	procs := []tuiProc{
		{PID: 1, CPUPct: 90, AgeSecs: 100}, // runaway
		{PID: 2, CPUPct: 5, AgeSecs: 20},   // healthy, newest
		{PID: 3, CPUPct: 5, AgeSecs: 40},   // healthy, older duplicate
	}
	got := reapTUIs(procs, 80.0, 15.0)
	assert.ElementsMatch(t, []int{1, 3}, got, "kill runaway + older duplicate, keep newest healthy (PID 2)")
}

// Test 2 — never kill the sole healthy TUI.
func Test_reapTUIs_SingleHealthy_NoKill(t *testing.T) {
	procs := []tuiProc{{PID: 7, CPUPct: 5, AgeSecs: 100}}
	got := reapTUIs(procs, 80.0, 15.0)
	assert.Empty(t, got)
}

// Test 3 — boot grace: a process younger than bootGrace is never touched, even
// at runaway CPU (protects boot spikes AND the singleton guard's fresh launch,
// killing the guard/reaper oscillation).
func Test_reapTUIs_BootGraceProtectsYoungProc(t *testing.T) {
	procs := []tuiProc{
		{PID: 1, CPUPct: 95, AgeSecs: 5},  // young runaway — grace-protected
		{PID: 2, CPUPct: 5, AgeSecs: 100}, // sole grace-passed healthy
	}
	got := reapTUIs(procs, 80.0, 15.0)
	assert.Empty(t, got, "young proc protected by boot grace; the one old healthy is the sole keeper")
}

// Test 4 — all grace-passed procs are runaway → kill them all.
func Test_reapTUIs_AllRunaway_KillsAll(t *testing.T) {
	procs := []tuiProc{
		{PID: 1, CPUPct: 90, AgeSecs: 100},
		{PID: 2, CPUPct: 95, AgeSecs: 100},
	}
	got := reapTUIs(procs, 80.0, 15.0)
	assert.ElementsMatch(t, []int{1, 2}, got)
}

// Test 5 — sweep() drives reapTUIs over an injected lister and routes targets to
// an injected killer, with ZERO real kills. Asserts exactly the expected PIDs.
func Test_tuiReaper_sweep_KillsExpected_NoRealKills(t *testing.T) {
	procs := []tuiProc{
		{PID: 1, CPUPct: 90, AgeSecs: 100},
		{PID: 2, CPUPct: 5, AgeSecs: 20},
		{PID: 3, CPUPct: 5, AgeSecs: 40},
	}
	var mu sync.Mutex
	var killedArgs []int
	r := &tuiReaper{
		list: func() ([]tuiProc, error) { return procs, nil },
		kill: func(p tuiProc) error {
			mu.Lock()
			killedArgs = append(killedArgs, p.PID)
			mu.Unlock()
			return nil
		},
		runawayCPU: 80.0,
		bootGrace:  15.0,
	}

	killed, err := r.sweep()
	require.NoError(t, err)
	assert.ElementsMatch(t, []int{1, 3}, killed)
	assert.ElementsMatch(t, []int{1, 3}, killedArgs, "killer invoked for exactly the targets")
}

// Test 6 — PID-recycle defence. reVerifyTUI must reject a PID that no longer
// looks like the TUI process we listed (recycled into another command, or now
// substantially younger = a different process reusing the PID), and must keep a
// still-alive same process (age grew).
func Test_reVerifyTUI_RejectsRecycledPID(t *testing.T) {
	listed := tuiProc{PID: 999, CPUPct: 5, AgeSecs: 100}

	// same process, a few seconds later (age grew) → still valid
	assert.True(t, reVerifyTUI(listed, func(int) (string, float64, bool) {
		return "Python", 103, true
	}), "same TUI, age grew → keep")

	// recycled into a non-TUI command → reject
	assert.False(t, reVerifyTUI(listed, func(int) (string, float64, bool) {
		return "bash", 1, true
	}), "PID recycled into bash → reject")

	// recycled into another python but far younger than at list time → reject
	assert.False(t, reVerifyTUI(listed, func(int) (string, float64, bool) {
		return "Python", 0.5, true
	}), "PID reused by a fresh python → reject (age dropped)")

	// process gone → reject
	assert.False(t, reVerifyTUI(listed, func(int) (string, float64, bool) {
		return "", 0, false
	}), "process gone → reject")
}

// Test 7 — etime parser across all three documented ps formats.
func Test_parseEtime(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float64
	}{
		{"00:42", 42},
		{"01:02:03", 3723},
		{"2-03:04:05", 183845},
		{"  10:00  ", 600},
	} {
		got, err := parseEtime(c.in)
		require.NoErrorf(t, err, "parseEtime(%q)", c.in)
		assert.InDeltaf(t, c.want, got, 0.001, "parseEtime(%q)", c.in)
	}
	_, err := parseEtime("garbage")
	assert.Error(t, err)
}

// Test 8 — a lister error aborts the sweep without any kills; the daemon
// survives (fail-open).
func Test_tuiReaper_sweep_ListError_NoKills(t *testing.T) {
	var killCalls int
	r := &tuiReaper{
		list:       func() ([]tuiProc, error) { return nil, errors.New("pgrep blew up") },
		kill:       func(tuiProc) error { killCalls++; return nil },
		runawayCPU: 80.0,
		bootGrace:  15.0,
	}
	killed, err := r.sweep()
	require.Error(t, err)
	assert.Empty(t, killed)
	assert.Zero(t, killCalls, "no kills attempted when the lister fails")
}
