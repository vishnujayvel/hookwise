package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// ---------------------------------------------------------------------------
// TUI watchdog / reaper (PR 2) — converge the system to <=1 healthy TUI.
//
// Even a race-proof singleton launch guard (PR 1) cannot retroactively fix
// duplicates from an older guard-less binary, a manual launch, or a TUI wedged
// in a CPU-hogging render loop. The watchdog is the daemon-side backstop that
// reaps duplicate / runaway TUIs. It signals processes only — no DB/cache
// writes (ARCH-3) — and never propagates failures (ARCH-7).
// ---------------------------------------------------------------------------

const (
	// tuiWatchInterval is how often the watchdog sweeps for duplicate/runaway TUIs.
	tuiWatchInterval = 30 * time.Second
	// tuiRunawayCPU is the %CPU above which a TUI is treated as a runaway and
	// killed. High enough that a momentarily-busy TUI (boot/import/render) is not
	// reaped; only a genuinely pegged render loop.
	tuiRunawayCPU = 80.0
	// tuiBootGraceSecs protects a just-started TUI from reaping: boot CPU spikes
	// are not runaways, and the singleton guard's fresh launch must not be killed
	// (which would oscillate with the guard).
	tuiBootGraceSecs = 15.0
)

// tuiProc is a single live TUI process with the stats the reaper policy needs.
type tuiProc struct {
	PID     int
	CPUPct  float64
	AgeSecs float64 // process age in seconds (now - start)
}

// reapTUIs is the pure reaper policy. Given the live TUI processes it returns
// the PIDs that should be killed:
//
//   - procs younger than bootGraceSecs are never touched (boot-spike / fresh-launch
//     protection);
//   - among the rest, any proc above runawayCPU is killed;
//   - among the remaining healthy procs, exactly one — the NEWEST (smallest age,
//     most likely the window the user is looking at) — is kept; the older
//     duplicates are killed.
//
// If all grace-passed procs are runaway, all are killed (with auto_launch off
// nothing respawns; with it on the guard relaunches one cleanly).
func reapTUIs(procs []tuiProc, runawayCPU, bootGraceSecs float64) []int {
	var kill []int
	var healthy []tuiProc
	for _, p := range procs {
		if p.AgeSecs < bootGraceSecs {
			continue // boot grace — never touch
		}
		if p.CPUPct > runawayCPU {
			kill = append(kill, p.PID)
			continue
		}
		healthy = append(healthy, p)
	}
	if len(healthy) > 1 {
		newest := 0
		for i := 1; i < len(healthy); i++ {
			if healthy[i].AgeSecs < healthy[newest].AgeSecs {
				newest = i
			}
		}
		for i, p := range healthy {
			if i != newest {
				kill = append(kill, p.PID)
			}
		}
	}
	return kill
}

// reVerifyTUI re-checks, immediately before signalling, that a PID is still the
// same TUI process we listed — defeating PID recycle (the PID dies between
// list() and kill() and is reused by an unrelated process). lookup returns the
// current (comm, age, ok) for the PID. A recycled PID is rejected because its
// comm is no longer a TUI, or because a fresh process reusing the PID is far
// younger than the one we listed.
func reVerifyTUI(p tuiProc, lookup func(pid int) (comm string, ageSecs float64, ok bool)) bool {
	comm, age, ok := lookup(p.PID)
	if !ok {
		return false // process gone
	}
	if !isTUIComm(comm) {
		return false // recycled into a non-TUI command
	}
	// The same process only ages forward between list and kill; a PID reused by
	// a fresh process is substantially younger. Allow small slack for sampling.
	if age+2.0 < p.AgeSecs {
		return false
	}
	return true
}

// tuiReaper carries the watchdog's injectable I/O so sweep() is unit-testable
// with no real processes. Production wires real list/kill in startTUIWatchdog.
type tuiReaper struct {
	list       func() ([]tuiProc, error)
	kill       func(p tuiProc) error // re-verifies identity, then signals
	runawayCPU float64
	bootGrace  float64
}

// sweep lists TUI processes, computes the reaper policy, and kills the targets.
// A lister error aborts the sweep with no kills. Individual kill failures (e.g.
// a re-verify reject) are skipped; only successfully-signalled PIDs are returned.
func (r *tuiReaper) sweep() (killed []int, err error) {
	procs, err := r.list()
	if err != nil {
		return nil, err
	}
	byPID := make(map[int]tuiProc, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	for _, pid := range reapTUIs(procs, r.runawayCPU, r.bootGrace) {
		if err := r.kill(byPID[pid]); err == nil {
			killed = append(killed, pid)
		}
	}
	return killed, nil
}

// parseEtime parses the well-defined `ps -o etime=` format `[[DD-]HH:]MM:SS`
// into seconds (locale-stable, unlike `lstart`).
func parseEtime(s string) (float64, error) {
	s = strings.TrimSpace(s)
	var days float64
	if i := strings.Index(s, "-"); i >= 0 {
		d, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, fmt.Errorf("etime days %q: %w", s, err)
		}
		days = float64(d)
		s = s[i+1:]
	}
	parts := strings.Split(s, ":")
	atof := func(x string) (float64, error) { return strconv.ParseFloat(strings.TrimSpace(x), 64) }
	var h, m, sec float64
	var err error
	switch len(parts) {
	case 2:
		if m, err = atof(parts[0]); err != nil {
			return 0, err
		}
		if sec, err = atof(parts[1]); err != nil {
			return 0, err
		}
	case 3:
		if h, err = atof(parts[0]); err != nil {
			return 0, err
		}
		if m, err = atof(parts[1]); err != nil {
			return 0, err
		}
		if sec, err = atof(parts[2]); err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("bad etime %q", s)
	}
	return days*86400 + h*3600 + m*60 + sec, nil
}

// psTUILookup returns the current (comm, ageSecs, ok) for a PID, used by the
// real killer's re-verify. Impure (runs `ps`); not unit-tested.
func psTUILookup(pid int) (string, float64, bool) {
	commOut, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return "", 0, false
	}
	etimeOut, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "etime=").Output()
	if err != nil {
		return "", 0, false
	}
	age, err := parseEtime(string(etimeOut))
	if err != nil {
		return "", 0, false
	}
	return strings.TrimSpace(string(commOut)), age, true
}

// listTUIProcsWithStats is the real lister: the comm-filtered TUI PIDs from PR 1
// enriched with %cpu + age. Impure; not unit-tested (the policy that consumes it
// is reapTUIs, which is).
func listTUIProcsWithStats() ([]tuiProc, error) {
	pids := listTUIProcs()
	var procs []tuiProc
	for _, pid := range pids {
		out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "%cpu=,etime=").Output()
		if err != nil {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(string(out)))
		if len(fields) < 2 {
			continue
		}
		cpu, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		age, err := parseEtime(fields[1])
		if err != nil {
			continue
		}
		procs = append(procs, tuiProc{PID: pid, CPUPct: cpu, AgeSecs: age})
	}
	return procs, nil
}

// realKillTUI re-verifies the PID is still the listed TUI, then sends SIGTERM and
// escalates to SIGKILL if it survives a short grace period.
func realKillTUI(p tuiProc) error {
	if !reVerifyTUI(p, psTUILookup) {
		return errors.New("re-verify failed; skipping kill (PID recycled or gone)")
	}
	proc, err := os.FindProcess(p.PID)
	if err != nil {
		return err
	}
	_ = proc.Signal(syscall.SIGTERM)
	time.Sleep(500 * time.Millisecond)
	if reVerifyTUI(p, psTUILookup) {
		_ = proc.Signal(syscall.SIGKILL)
	}
	core.Logger().Info("watchdog reaped TUI", "pid", p.PID, "cpu", p.CPUPct, "ageSecs", p.AgeSecs)
	return nil
}

// startTUIWatchdog launches the daemon-side reaper goroutine. It sweeps every
// tuiWatchInterval and exits when stopCh closes. Mirrors startSnapshotScheduler:
// the side effect is non-blocking and each tick recovers from panic (ARCH-7).
func startTUIWatchdog(stopCh <-chan struct{}) {
	r := &tuiReaper{
		list:       listTUIProcsWithStats,
		kill:       realKillTUI,
		runawayCPU: tuiRunawayCPU,
		bootGrace:  tuiBootGraceSecs,
	}
	go func() {
		ticker := time.NewTicker(tuiWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				runWatchdogSweep(r)
			}
		}
	}()
}

// runWatchdogSweep performs one sweep, recovering from any panic so the daemon
// stays alive (ARCH-7). Failures are logged, never propagated.
func runWatchdogSweep(r *tuiReaper) {
	defer func() {
		if rec := recover(); rec != nil {
			core.Logger().Error("panic in TUI watchdog", "recovered", fmt.Sprintf("%v", rec))
		}
	}()
	killed, err := r.sweep()
	if err != nil {
		core.Logger().Debug("TUI watchdog sweep error (fail-open)", "error", err)
		return
	}
	if len(killed) > 0 {
		core.Logger().Info("TUI watchdog reaped duplicates/runaways", "pids", killed)
	}
}
