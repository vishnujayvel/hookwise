package main

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestLauncher builds a tuiLauncher backed by a real marker file in dir, a
// real clock, and a spawn closure that the test controls. isRunning defaults to
// always-false (simulating the boot gap where neither the PID file nor the
// comm-filtered detection has caught the TUI yet).
func newTestLauncher(dir string, spawn func(string) error) *tuiLauncher {
	return &tuiLauncher{
		stateDir:  dir,
		cooldown:  tuiLaunchCooldown,
		now:       time.Now,
		isRunning: func() bool { return false },
		spawn:     spawn,
	}
}

// Test 1 — the core singleton property: rapid SessionStarts across the boot
// window (isRunning false throughout) must yield EXACTLY ONE spawn. This is the
// regression test for the launch-race that piled up Terminal windows.
func Test_launchIfNeeded_FiveSequential_ExactlyOneSpawn(t *testing.T) {
	dir := t.TempDir()
	var spawns int32
	l := newTestLauncher(dir, func(string) error {
		atomic.AddInt32(&spawns, 1)
		return nil
	})

	for i := 0; i < 5; i++ {
		_, err := l.launchIfNeeded("newWindow")
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), atomic.LoadInt32(&spawns), "5 rapid launches must spawn exactly once")
	assert.FileExists(t, filepath.Join(dir, "tui.launch.marker"), "marker should persist to suppress relaunch")
}

// Test 2 — concurrent dispatches at the same instant must still spawn once
// (O_EXCL marker claim serializes them).
func Test_launchIfNeeded_Concurrent_ExactlyOneSpawn(t *testing.T) {
	dir := t.TempDir()
	var spawns int32
	l := newTestLauncher(dir, func(string) error {
		atomic.AddInt32(&spawns, 1)
		return nil
	})

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = l.launchIfNeeded("newWindow")
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), atomic.LoadInt32(&spawns), "%d concurrent launches must spawn exactly once", n)
}

// Test 3 — once the cooldown has elapsed (marker stale) and the TUI still isn't
// running, a relaunch is allowed. Elapsed time is simulated by back-dating the
// marker's mtime (the marker timestamp IS its mtime — no content parsing).
func Test_launchIfNeeded_CooldownExpiry_AllowsSecond(t *testing.T) {
	dir := t.TempDir()
	var spawns int32
	l := newTestLauncher(dir, func(string) error {
		atomic.AddInt32(&spawns, 1)
		return nil
	})

	_, err := l.launchIfNeeded("newWindow")
	require.NoError(t, err)
	require.Equal(t, int32(1), atomic.LoadInt32(&spawns))

	// Age the marker well past the cooldown.
	marker := filepath.Join(dir, "tui.launch.marker")
	old := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(marker, old, old))

	_, err = l.launchIfNeeded("newWindow")
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&spawns), "stale marker must allow a fresh launch")
}

// Test 4 — a spawn failure must remove the marker so the very next dispatch can
// retry immediately (no cooldown blackhole on e.g. a transient PATH miss).
func Test_launchIfNeeded_SpawnFailure_RemovesMarker_AllowsRetry(t *testing.T) {
	dir := t.TempDir()
	var attempts int32
	l := newTestLauncher(dir, func(string) error {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			return assert.AnError
		}
		return nil
	})

	spawned, err := l.launchIfNeeded("newWindow")
	require.Error(t, err)
	assert.False(t, spawned)
	assert.NoFileExists(t, filepath.Join(dir, "tui.launch.marker"), "failed spawn must unclaim the marker")

	spawned, err = l.launchIfNeeded("newWindow")
	require.NoError(t, err)
	assert.True(t, spawned, "retry after a failed spawn must be allowed immediately")
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
}

// Test 5 — when the TUI is already running, never spawn.
func Test_launchIfNeeded_AlreadyRunning_NoSpawn(t *testing.T) {
	dir := t.TempDir()
	var spawns int32
	l := newTestLauncher(dir, func(string) error {
		atomic.AddInt32(&spawns, 1)
		return nil
	})
	l.isRunning = func() bool { return true }

	spawned, err := l.launchIfNeeded("newWindow")
	require.NoError(t, err)
	assert.False(t, spawned)
	assert.Equal(t, int32(0), atomic.LoadInt32(&spawns))
}

// Test 6 — THE Grok-BLOCKER-#1 guard. `pgrep -f hookwise-tui` matches the
// `open -a Terminal …/hookwise-tui` launcher and stray grep/sh/find that carry
// the token in argv. Filtering on comm (argv[0] basename) must REJECT those and
// keep only the real TUI (comm hookwise-tui / Python). A false positive here
// would suppress a legitimate launch.
func Test_filterTUICandidates_RejectsLaunchersKeepsTUI(t *testing.T) {
	cands := []tuiCandidate{
		{pid: 101, comm: "open"},               // the launcher — token is in argv, not comm
		{pid: 102, comm: "grep"},               // a stray `grep hookwise-tui`
		{pid: 103, comm: "sh"},                 // `sh -c '… hookwise-tui'`
		{pid: 104, comm: "find"},               // `find . -name hookwise-tui`
		{pid: 105, comm: "/usr/bin/cat"},       // `cat …/hookwise-tui`
		{pid: 201, comm: "hookwise-tui"},       // real TUI wrapper
		{pid: 202, comm: "Python"},             // real TUI under python interpreter
		{pid: 203, comm: "/path/.venv/bin/python3.13"},
	}

	got := filterTUICandidates(cands)

	assert.ElementsMatch(t, []int{201, 202, 203}, got,
		"only real TUI processes survive the comm filter; launchers/search tools are rejected")
}

// Test 7 — a marker I/O error (here: the marker's parent is a FILE, so O_EXCL
// create fails with ENOTDIR, not EEXIST) must FAIL TOWARD SUPPRESS: do not
// spawn. Pile-up is the bug; under-launch is the safe failure.
func Test_launchIfNeeded_MarkerIOError_Suppresses(t *testing.T) {
	base := t.TempDir()
	notADir := filepath.Join(base, "iam-a-file")
	require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o644))

	var spawns int32
	l := newTestLauncher(notADir, func(string) error { // stateDir's parent path is a file
		atomic.AddInt32(&spawns, 1)
		return nil
	})

	spawned, err := l.launchIfNeeded("newWindow")
	require.NoError(t, err)
	assert.False(t, spawned)
	assert.Equal(t, int32(0), atomic.LoadInt32(&spawns), "marker I/O error must suppress, never spawn")
}

// isTUIComm unit coverage — the decisive allowlist.
func Test_isTUIComm(t *testing.T) {
	for _, c := range []struct {
		comm string
		want bool
	}{
		{"hookwise-tui", true},
		{"/path/.venv/bin/hookwise-tui", true},
		{"Python", true},
		{"python3.13", true},
		{"open", false},
		{"grep", false},
		{"sh", false},
		{"Terminal", false},
		{"", false},
	} {
		assert.Equalf(t, c.want, isTUIComm(c.comm), "isTUIComm(%q)", c.comm)
	}
}
