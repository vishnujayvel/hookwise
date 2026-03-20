package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/core"
	"github.com/vishnujayvel/hookwise/internal/feeds"
)

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

func newDaemonRunCmd() *cobra.Command {
	var configPath string
	var socketPath string

	cmd := &cobra.Command{
		Use:    "run",
		Short:  "Run the daemon in the foreground (internal)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				cwd, _ := os.Getwd()
				configPath = filepath.Join(cwd, core.ProjectConfigFile)
			}

			config, err := core.LoadConfig(filepath.Dir(configPath))
			if err != nil {
				// Fail-open: use defaults.
				config = core.GetDefaultConfig()
			}

			registry := feeds.NewRegistry()
			feeds.RegisterBuiltins(registry)

			daemon := feeds.NewDaemon(config.Daemon, config.Feeds, registry)
			if socketPath != "" {
				daemon.SetSocketPath(socketPath)
			}
			if err := daemon.Start(); err != nil {
				return fmt.Errorf("daemon: %w", err)
			}

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
// TUI launcher (Bug #14 — duplicate terminal tabs)
// ---------------------------------------------------------------------------

// tuiPIDPath returns the path to the TUI PID file.
func tuiPIDPath() string {
	return filepath.Join(core.DefaultStateDir, "tui.pid")
}

// isTUIRunning checks if a TUI process is already running by reading the PID file,
// checking if the process exists, and verifying it's actually hookwise-tui
// (not a stale PID reused by an unrelated process).
func isTUIRunning() bool {
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
	// Verify the PID belongs to hookwise-tui, not a stale PID reused by
	// an unrelated process. Uses `ps` which works on macOS and Linux.
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	comm := strings.TrimSpace(string(out))
	return strings.Contains(comm, "hookwise-tui") || strings.Contains(comm, "python") || strings.Contains(comm, "Python")
}

// acquireTUILaunchLock atomically creates a lock file to prevent concurrent
// TUI launches (TOCTOU race between isTUIRunning check and TUI PID write).
// Returns a cleanup function and true on success, or nil and false if another
// dispatch already holds the lock.
func acquireTUILaunchLock() (unlock func(), ok bool) {
	lockPath := filepath.Join(core.DefaultStateDir, "tui.launch.lock")
	_ = os.MkdirAll(filepath.Dir(lockPath), core.DefaultDirMode)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, false
	}
	f.Close()
	return func() { os.Remove(lockPath) }, true
}

// launchTUIIfNeeded launches the TUI if it's not already running.
// Called synchronously from dispatch on SessionStart events.
func launchTUIIfNeeded(launchMethod string) {
	defer func() {
		if r := recover(); r != nil {
			core.Logger().Error("panic in TUI launcher", "recovered", fmt.Sprintf("%v", r))
		}
	}()

	if isTUIRunning() {
		core.Logger().Debug("TUI already running, skipping launch")
		return
	}

	// Atomic lock prevents TOCTOU race: two concurrent SessionStart dispatches
	// could both pass isTUIRunning() before either TUI writes its PID file.
	unlock, ok := acquireTUILaunchLock()
	if !ok {
		core.Logger().Debug("another dispatch is launching TUI, skipping")
		return
	}
	defer unlock()

	// Re-check after acquiring lock (double-check pattern)
	if isTUIRunning() {
		core.Logger().Debug("TUI started between check and lock, skipping")
		return
	}

	// Find hookwise-tui executable
	tuiCmd, err := exec.LookPath("hookwise-tui")
	if err != nil {
		core.Logger().Debug("hookwise-tui not found in PATH, skipping auto-launch")
		return
	}

	var cmd *exec.Cmd
	switch launchMethod {
	case "newWindow":
		// macOS: open in a new Terminal window
		cmd = exec.Command("open", "-a", "Terminal", tuiCmd)
	default:
		// background: launch directly as a background process
		cmd = exec.Command(tuiCmd)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		core.Logger().Warn("failed to launch TUI", "method", launchMethod, "error", err)
		return
	}

	core.Logger().Info("TUI launched", "method", launchMethod, "pid", cmd.Process.Pid)
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
