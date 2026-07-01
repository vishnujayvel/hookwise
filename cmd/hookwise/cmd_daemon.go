package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/bridge"
	"github.com/vishnujayvel/hookwise/internal/core"
	"github.com/vishnujayvel/hookwise/internal/feeds"
)

// registerCustomFeeds registers user-defined custom feed producers (#124) from
// config so the daemon polls them alongside the built-ins. Entries with an
// empty name or command are skipped (a malformed entry must not register a
// no-op producer). Enabled/interval gating happens in the daemon poll loop via
// config.Feeds.Custom (mirroring how built-ins are gated), so producers are
// registered here regardless of their Enabled flag. Returns the number
// registered.
func registerCustomFeeds(registry *feeds.Registry, customs []core.CustomFeedConfig) int {
	count := 0
	for _, c := range customs {
		if c.Name == "" || c.Command == "" {
			continue
		}
		registry.Register(feeds.NewCustomProducer(
			c.Name, c.Command, time.Duration(c.TimeoutSeconds)*time.Second,
		))
		count++
	}
	return count
}

func newDaemonCmd() *cobra.Command {
	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the hookwise feed daemon",
	}

	daemonCmd.AddCommand(
		newDaemonStartCmd(),
		newDaemonStopCmd(),
		newDaemonRunCmd(),
	)

	return daemonCmd
}

func newDaemonStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the feed daemon (connect-or-start)",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			socketPath := filepath.Join(core.GetStateDir(), "daemon.sock")
			client := feeds.NewDaemonClient(socketPath)

			cwd, _ := os.Getwd()
			configPath := filepath.Join(cwd, core.ProjectConfigFile)

			if err := client.EnsureDaemon(configPath); err != nil {
				fmt.Fprintf(w, "daemon: failed to start: %v\n", err)
				return nil // Fail-open (ARCH-1)
			}

			// Report health.
			health, err := client.Health()
			if err != nil {
				fmt.Fprintln(w, "daemon: started (health check unavailable)")
				return nil
			}

			fmt.Fprintf(w, "daemon: running (pid: %v, uptime: %v)\n",
				health["pid"], health["uptime"])
			return nil
		},
	}
}

func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the feed daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			socketPath := filepath.Join(core.GetStateDir(), "daemon.sock")
			client := feeds.NewDaemonClient(socketPath)

			if !client.IsRunning() {
				fmt.Fprintln(w, "daemon: not running")
				return nil
			}

			if err := client.Shutdown(); err != nil {
				fmt.Fprintf(w, "daemon: shutdown error: %v\n", err)
				return nil
			}

			fmt.Fprintln(w, "daemon: stopped")
			return nil
		},
	}
}

// resolveDaemonConfig loads the canonical daemon-process config. The daemon is a
// singleton — one socket, one cache, one process per state dir — so its entire
// config (feeds, daemon, analytics snapshots) MUST come from the global config
// file only, independent of which project directory cold-started it (#89).
// Honoring a per-project hookwise.yaml here let whichever project won the
// cold-start race freeze the feed config for every other project/session. Fails
// open to defaults on error (ARCH-1).
func resolveDaemonConfig() core.HooksConfig {
	config, err := core.LoadGlobalConfig()
	if err != nil {
		return core.GetDefaultConfig()
	}
	return config
}

func newDaemonRunCmd() *cobra.Command {
	// configPath is retained only so the daemon still accepts the legacy
	// --config flag that older spawn paths may pass; it is intentionally NOT used
	// to source config anymore (#89 — see resolveDaemonConfig).
	var configPath string
	var socketPath string

	cmd := &cobra.Command{
		Use:    "run",
		Short:  "Run the daemon in the foreground (internal)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			config := resolveDaemonConfig()

			registry := feeds.NewRegistry()
			feeds.RegisterBuiltins(registry)
			registerCustomFeeds(registry, config.Feeds.Custom)

			daemon := feeds.NewDaemon(config.Daemon, config.Feeds, registry)
			if socketPath != "" {
				daemon.SetSocketPath(socketPath)
			}

			// Wire the merged TUI cache regeneration. This lives in the cmd
			// layer (not internal/feeds) because internal/bridge's test package
			// imports internal/feeds, so a feeds→bridge import would cycle. After
			// each producer writes its per-feed file, regenerate the single
			// merged status-line-cache.json the Python TUI reads (audit #5).
			daemon.SetPostPollHook(func(cacheDir string) {
				outPath := filepath.Join(cacheDir, bridge.TUICacheFileName)
				if err := bridge.WriteTUICacheTo(cacheDir, outPath); err != nil {
					core.Logger().Error("daemon: TUI cache write error", "error", err)
				}
			})

			if err := daemon.Start(); err != nil {
				return fmt.Errorf("daemon: %w", err)
			}

			// Schedule periodic analytics snapshots from the cmd layer.
			// This deliberately lives here (package main) rather than inside
			// internal/feeds, because ARCH-3 forbids the feeds/daemon package
			// from importing internal/analytics. The cmd layer may import both.
			// The scheduler stops when the daemon's stop channel closes.
			startSnapshotScheduler(config.Analytics, daemon.StopCh())

			// Start the TUI watchdog: a daemon-side backstop that reaps
			// duplicate / runaway TUIs the singleton launch guard cannot fix
			// retroactively (older binaries, manual launches, wedged render
			// loops). Signals processes only (ARCH-3 untouched); stops when the
			// daemon's stop channel closes.
			startTUIWatchdog(daemon.StopCh())

			// Block until the daemon stops.
			// The daemon handles SIGTERM/SIGINT and /shutdown internally.
			<-daemon.StopCh()
			return daemon.Stop()
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Config file path")
	cmd.Flags().StringVar(&socketPath, "socket", "", "Socket file path")
	return cmd
}

// ---------------------------------------------------------------------------
// Periodic analytics snapshots (VACUUM INTO, hourly by default)
// ---------------------------------------------------------------------------

// startSnapshotScheduler launches a background goroutine that takes an
// analytics snapshot every cfg.SnapshotIntervalMinutes and prunes older
// snapshots beyond cfg.SnapshotRetention. It is a no-op unless both analytics
// and snapshots are enabled.
//
// Architecture notes:
//   - ARCH-3: this scheduling lives in cmd/hookwise (package main), NOT in
//     internal/feeds, because the arch lint forbids the feeds/daemon package
//     from importing internal/analytics. The cmd layer is permitted to import
//     both, so the scheduler bridges them here.
//   - ARCH-7: the side effect is non-blocking (runs in its own goroutine) and
//     each tick is wrapped in a recover() so a snapshot panic can never crash
//     the daemon.
//
// The goroutine exits when stopCh is closed (daemon shutdown).
func startSnapshotScheduler(cfg core.AnalyticsConfig, stopCh <-chan struct{}) {
	if !cfg.Enabled || !cfg.SnapshotEnabled {
		return
	}

	interval := time.Duration(cfg.SnapshotIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = time.Duration(core.DefaultSnapshotIntervalMinutes) * time.Minute
	}

	dbPath := cfg.DBPath
	retention := cfg.SnapshotRetention

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				runScheduledSnapshot(dbPath, retention)
			}
		}
	}()
}

// runScheduledSnapshot performs a single snapshot + prune cycle, recovering
// from any panic so the daemon stays alive (ARCH-7). All failures are logged,
// never propagated.
func runScheduledSnapshot(dbPath string, retention int) {
	defer func() {
		if r := recover(); r != nil {
			core.Logger().Error("panic in snapshot scheduler", "recovered", fmt.Sprintf("%v", r))
		}
	}()

	db, err := analytics.Open(dbPath)
	if err != nil {
		core.Logger().Warn("snapshot scheduler: failed to open analytics DB", "error", err)
		return
	}
	defer db.Close()

	path, err := db.Snapshot(context.Background(), analytics.DefaultSnapshotsDir())
	if err != nil {
		core.Logger().Warn("snapshot scheduler: snapshot failed", "error", err)
		return
	}

	pruned, err := analytics.PruneSnapshots(analytics.DefaultSnapshotsDir(), retention)
	if err != nil {
		core.Logger().Warn("snapshot scheduler: prune failed", "snapshot", path, "error", err)
		return
	}

	core.Logger().Info("analytics snapshot taken", "path", path, "pruned", len(pruned))
}

// ---------------------------------------------------------------------------
// TUI launcher (Bug #14 — duplicate terminal tabs)
// ---------------------------------------------------------------------------

// tuiPIDPath returns the path to the TUI PID file. It reads core.GetStateDir()
// at call time so it honors HOOKWISE_STATE_DIR (#116); the package-level
// core.DefaultStateDir var is frozen at init and ignores the override.
func tuiPIDPath() string {
	return filepath.Join(core.GetStateDir(), "tui.pid")
}

// tuiLaunchCooldown is how long a recorded launch suppresses re-launch. It only
// has to bridge the open->Terminal->python-exec handoff (~1-3s), because once
// python has exec'd, listTUIProcs() detects the TUI by comm well before the
// python TUI writes its PID file on mount. 30s is generous, self-healing slack.
const tuiLaunchCooldown = 30 * time.Second

// isTUIRunning reports whether a TUI is already up. It first trusts the PID file
// (written by the python TUI on mount), then falls back to comm-filtered process
// detection so a TUI that has exec'd but not yet written its PID is still seen.
func isTUIRunning() bool {
	if tuiPIDFileAlive() {
		return true
	}
	return len(listTUIProcs()) > 0
}

// tuiPIDFileAlive checks the PID file points at a live hookwise-tui process.
func tuiPIDFileAlive() bool {
	data, err := os.ReadFile(tuiPIDPath())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	// Check if process exists (signal 0 = existence check)
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	// Verify the PID belongs to the TUI, not a stale PID reused by an
	// unrelated process. Uses `ps` which works on macOS and Linux.
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	return isTUIComm(strings.TrimSpace(string(out)))
}

// tuiCandidate is one `pgrep -f hookwise-tui` hit with its comm (argv[0]).
type tuiCandidate struct {
	pid  int
	comm string
}

// isTUIComm reports whether a process command (argv[0] basename, from `ps -o
// comm=`) belongs to a real TUI process. `pgrep -f hookwise-tui` matches the
// full command line, so the transient `open -a Terminal …/hookwise-tui`
// launcher and stray `grep`/`sh`/`find …hookwise-tui` all match — but their
// comm is open/grep/sh/find, which does NOT contain the token. Filtering on
// comm (not the full cmdline) rejects those launchers and keeps only the TUI,
// whose comm is the hookwise-tui wrapper or the python interpreter running it.
func isTUIComm(comm string) bool {
	comm = strings.TrimSpace(comm)
	if comm == "" {
		return false
	}
	base := comm
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	lower := strings.ToLower(base)
	return strings.Contains(lower, "hookwise-tui") || strings.Contains(lower, "python")
}

// filterTUICandidates keeps only the PIDs whose comm is a real TUI process
// (pure — the testable heart of the comm filter).
func filterTUICandidates(cands []tuiCandidate) []int {
	var out []int
	for _, c := range cands {
		if isTUIComm(c.comm) {
			out = append(out, c.pid)
		}
	}
	return out
}

// listTUIProcs returns the PIDs of live TUI processes via `pgrep -f hookwise-tui`
// filtered by comm. Shared by the launch guard and the watchdog (PR2).
func listTUIProcs() []int {
	out, err := exec.Command("pgrep", "-f", "hookwise-tui").Output()
	if err != nil {
		return nil
	}
	var cands []tuiCandidate
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			continue
		}
		commOut, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
		if err != nil {
			continue
		}
		cands = append(cands, tuiCandidate{pid: pid, comm: strings.TrimSpace(string(commOut))})
	}
	return filterTUICandidates(cands)
}

// tuiLauncher carries the dependencies of the singleton launch guard so the
// decision logic is unit-testable without opening Terminal or touching
// ~/.hookwise. Production wires real deps in launchTUIIfNeeded.
type tuiLauncher struct {
	stateDir  string
	cooldown  time.Duration
	now       func() time.Time
	isRunning func() bool
	spawn     func(method string) error
}

func (l *tuiLauncher) markerPath() string {
	return filepath.Join(l.stateDir, "tui.launch.marker")
}

func (l *tuiLauncher) removeMarker() {
	_ = os.Remove(l.markerPath())
}

// claimMarker atomically claims the launch window. The marker's MTIME is its
// timestamp (no content is parsed — this avoids the empty-file read race). It
// returns true iff this caller won the claim and should proceed to spawn:
//
//   - absent marker  -> O_EXCL create wins -> true
//   - fresh marker   -> a launch is in flight -> false (suppress)
//   - stale marker   -> remove + re-claim via O_EXCL (exactly one winner)
//   - any I/O error  -> false (FAIL TOWARD SUPPRESS; pile-up is the bug we fix)
func (l *tuiLauncher) claimMarker() bool {
	path := l.markerPath()
	_ = core.EnsureDir(filepath.Dir(path), core.DefaultDirMode)
	for attempt := 0; attempt < 3; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return true
		}
		if !errors.Is(err, fs.ErrExist) {
			return false // I/O error (ENOTDIR, ENOSPC, EPERM, …) -> suppress
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			// Vanished between Open and Stat (another dispatch removed it) — retry.
			continue
		}
		if l.now().Sub(info.ModTime()) < l.cooldown {
			return false // fresh -> a launch is in flight -> suppress
		}
		// Stale -> drop it and retry the O_EXCL create so exactly one wins.
		_ = os.Remove(path)
	}
	return false
}

// launchIfNeeded is the testable core of the singleton guard.
func (l *tuiLauncher) launchIfNeeded(method string) (spawned bool, err error) {
	if l.isRunning() {
		return false, nil
	}
	if !l.claimMarker() {
		return false, nil
	}
	// Double-check: a TUI may have appeared between the first isRunning check
	// and winning the claim. If so, release the marker and skip.
	if l.isRunning() {
		l.removeMarker()
		return false, nil
	}
	if err := l.spawn(method); err != nil {
		l.removeMarker() // unclaim so the next dispatch can retry immediately
		return false, err
	}
	return true, nil
}

// realTUISpawn finds and starts the hookwise-tui process (the only impure,
// untested part — exec wiring identical to the previous implementation).
func realTUISpawn(method string) error {
	tuiCmd, err := exec.LookPath("hookwise-tui")
	if err != nil {
		core.Logger().Debug("hookwise-tui not found in PATH, skipping auto-launch")
		return err
	}

	var cmd *exec.Cmd
	switch method {
	case "newWindow":
		cmd = exec.Command("open", "-a", "Terminal", tuiCmd) // macOS: new Terminal window
	default:
		cmd = exec.Command(tuiCmd) // background process
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		core.Logger().Warn("failed to launch TUI", "method", method, "error", err)
		return err
	}
	core.Logger().Info("TUI launched", "method", method, "pid", cmd.Process.Pid)
	return nil
}

// launchTUIIfNeeded launches the TUI if it's not already running.
// Called synchronously from dispatch on SessionStart events.
func launchTUIIfNeeded(launchMethod string) {
	defer func() {
		if r := recover(); r != nil {
			core.Logger().Error("panic in TUI launcher", "recovered", fmt.Sprintf("%v", r))
		}
	}()

	l := &tuiLauncher{
		// core.GetStateDir() (not the frozen core.DefaultStateDir) so the launch
		// marker honors HOOKWISE_STATE_DIR, matching tuiPIDPath (#116).
		stateDir:  core.GetStateDir(),
		cooldown:  tuiLaunchCooldown,
		now:       time.Now,
		isRunning: isTUIRunning,
		spawn:     realTUISpawn,
	}
	if _, err := l.launchIfNeeded(launchMethod); err != nil {
		core.Logger().Debug("TUI launch suppressed/failed (fail-open)", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Auto-start helper for status-line (Task 6.1)
// ---------------------------------------------------------------------------

// ensureDaemonWithCache calls EnsureDaemon only if the alive marker is stale
// or missing. The marker file is touched on success, caching the result for 60s.
// Returns silently on any error (ARCH-1 fail-open).
func ensureDaemonWithCache(configPath string) {
	stateDir := core.GetStateDir()
	socketPath := filepath.Join(stateDir, "daemon.sock")
	markerPath := filepath.Join(stateDir, "daemon-alive.marker")

	// Check marker file freshness.
	if info, err := os.Stat(markerPath); err == nil {
		if time.Since(info.ModTime()) < 60*time.Second {
			// Marker is fresh — verify the socket is still reachable to
			// handle daemon crash during marker validity window.
			client := feeds.NewDaemonClient(socketPath)
			if client.IsRunning() {
				return // Recent check and daemon still reachable — skip.
			}
			// Daemon crashed — fall through to re-start.
		}
	}

	client := feeds.NewDaemonClient(socketPath)
	if err := client.EnsureDaemon(configPath); err != nil {
		core.Logger().Debug("feeds: auto-start failed (fail-open)", "error", err)
		return
	}

	// Touch the marker file.
	if err := core.EnsureDir(filepath.Dir(markerPath), core.DefaultDirMode); err != nil {
		return
	}
	os.WriteFile(markerPath, []byte("ok"), 0o600)
}
