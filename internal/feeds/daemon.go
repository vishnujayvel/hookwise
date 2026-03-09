package feeds

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// DaemonStatus reports the state of the daemon.
type DaemonStatus struct {
	Running   bool
	PID       int
	FeedCount int
}

// Daemon manages background feed polling.
type Daemon struct {
	config        core.DaemonConfig
	feeds         core.FeedsConfig
	registry      *Registry
	pidFile       string
	cacheDir      string // directory for JSON cache files
	socketPath    string
	server        *SocketServer
	startedAt     time.Time
	staggerOffset time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup

	// Idle timeout fields (IDLE-1).
	idleTimer       *time.Timer
	idleMu          sync.Mutex
	idleTimeoutOverride time.Duration // test-only: overrides InactivityTimeoutMinutes
}

// NewDaemon creates a new daemon with the given configuration and registry.
// pidFile and cacheDir can be overridden for testing; pass empty strings to
// use the defaults from core.DefaultPIDPath and core.DefaultCachePath.
func NewDaemon(config core.DaemonConfig, feeds core.FeedsConfig, registry *Registry) *Daemon {
	return &Daemon{
		config:        config,
		feeds:         feeds,
		registry:      registry,
		pidFile:       core.DefaultPIDPath,
		cacheDir:      filepath.Dir(core.DefaultCachePath),
		socketPath:    core.DefaultSocketPath,
		staggerOffset: defaultStaggerOffset,
	}
}

// SetPIDFile overrides the PID file path (used in tests).
func (d *Daemon) SetPIDFile(path string) {
	d.pidFile = path
}

// SetCacheDir overrides the cache directory path (used in tests).
func (d *Daemon) SetCacheDir(path string) {
	d.cacheDir = path
}

// SetSocketPath overrides the socket file path (used in tests).
func (d *Daemon) SetSocketPath(path string) {
	d.socketPath = path
}

// SetStaggerOffset overrides the stagger delay between feed goroutine starts
// (used in tests to speed up execution).
func (d *Daemon) SetStaggerOffset(offset time.Duration) {
	d.staggerOffset = offset
}

// SetIdleTimeout overrides the idle timeout duration (used in tests).
// Must be called before Start().
func (d *Daemon) SetIdleTimeout(timeout time.Duration) {
	d.idleTimeoutOverride = timeout
}

// Start binds the unix socket (single-instance authority per SOCKET-1),
// writes a PID file for debugging visibility, installs signal handlers,
// and launches feed goroutines.
func (d *Daemon) Start() error {
	// MIGRATE-1: Detect and shut down old PID-only daemon before socket bind.
	d.migrateOldDaemon()

	d.startedAt = time.Now()

	// Create SocketServer — socket bind IS the single-instance check (SOCKET-1).
	// The shutdownFn closure reads d.stopCh at invocation time (not capture time),
	// so stopCh can be set after NewSocketServer but before Start.
	d.server = NewSocketServer(d.socketPath, func() {
		// shutdownFn: close stopCh to trigger graceful shutdown.
		select {
		case <-d.stopCh:
			// Already closed.
		default:
			close(d.stopCh)
		}
	}, d.startedAt)

	if err := d.server.Start(); err != nil {
		return fmt.Errorf("feeds: daemon already running: %w", err)
	}

	// Only initialize stopCh after successful socket bind so a failed second
	// Start() call doesn't clobber the running daemon's channel.
	d.stopCh = make(chan struct{})

	// Write PID file for debugging visibility only (not for liveness).
	if err := d.writePIDFile(); err != nil {
		// Non-fatal: PID file is informational. Log and continue.
		core.Logger().Error("feeds: failed to write pid file", "error", err)
	}

	// Install signal handler for graceful shutdown.
	d.installSignalHandler()

	// Start idle monitor if configured.
	d.startIdleMonitor()

	// Launch feed goroutines.
	d.runAllFeeds()

	return nil
}

// Stop gracefully shuts down the daemon: stops the socket server, signals
// feed goroutines to stop, waits for completion, and cleans up the PID file.
func (d *Daemon) Stop() error {
	// Close stopCh to signal all goroutines to exit.
	if d.stopCh != nil {
		select {
		case <-d.stopCh:
			// Already closed (e.g., by signal handler or /shutdown endpoint).
		default:
			close(d.stopCh)
		}
	}

	// Stop the idle timer if running.
	d.idleMu.Lock()
	if d.idleTimer != nil {
		d.idleTimer.Stop()
	}
	d.idleMu.Unlock()

	// Shut down the socket server with a timeout.
	if d.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := d.server.Shutdown(ctx); err != nil {
			core.Logger().Error("feeds: socket server shutdown error", "error", err)
		}
	}

	// Wait for feed goroutines with a timeout.
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines finished.
	case <-time.After(10 * time.Second):
		core.Logger().Error("feeds: timed out waiting for feed goroutines to stop")
	}

	// Remove PID file (socket file is already removed by SocketServer.Shutdown).
	return d.removePIDFile()
}

// StopCh returns the stop channel, which is closed when the daemon is shutting down.
// Used by the CLI `daemon run` command to block until shutdown.
func (d *Daemon) StopCh() <-chan struct{} {
	return d.stopCh
}

// IsRunning checks whether the daemon process identified by the PID file is
// still alive.
func (d *Daemon) IsRunning() bool {
	return d.isProcessAlive()
}

// Status returns the current daemon status.
func (d *Daemon) Status() DaemonStatus {
	pid := d.readPID()
	alive := false
	if pid > 0 {
		alive = isProcessAliveByPID(pid)
	}
	return DaemonStatus{
		Running:   alive,
		PID:       pid,
		FeedCount: len(d.registry.All()),
	}
}

// StopByPIDFile sends SIGTERM to the process identified in the given PID file.
func StopByPIDFile(pidFile string) error {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("feeds: read pid file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("feeds: parse pid: %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("feeds: find process %d: %w", pid, err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// If the process doesn't exist, clean up the PID file.
		os.Remove(pidFile)
		return fmt.Errorf("feeds: send SIGTERM to %d: %w", pid, err)
	}

	return nil
}

// idleTimeout returns the effective idle timeout duration, preferring
// the test override if set.
func (d *Daemon) idleTimeout() time.Duration {
	if d.idleTimeoutOverride > 0 {
		return d.idleTimeoutOverride
	}
	return time.Duration(d.config.InactivityTimeoutMinutes) * time.Minute
}

// resetIdleTimer resets the idle timeout timer. Called after each producer
// poll completes (IDLE-1: idle timer resets on producer poll completion).
func (d *Daemon) resetIdleTimer() {
	d.idleMu.Lock()
	defer d.idleMu.Unlock()
	if d.idleTimer != nil {
		d.idleTimer.Reset(d.idleTimeout())
	}
}

// startIdleMonitor starts the idle timeout goroutine if InactivityTimeoutMinutes
// is configured (or idleTimeoutOverride is set). When the timer fires without
// being reset, the daemon shuts down.
func (d *Daemon) startIdleMonitor() {
	if d.config.InactivityTimeoutMinutes <= 0 && d.idleTimeoutOverride <= 0 {
		return
	}

	timeout := d.idleTimeout()

	d.idleMu.Lock()
	d.idleTimer = time.NewTimer(timeout)
	d.idleMu.Unlock()

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		select {
		case <-d.idleTimer.C:
			core.Logger().Info("feeds: idle timeout reached, shutting down",
				"timeout_minutes", d.config.InactivityTimeoutMinutes)
			// Trigger graceful shutdown.
			select {
			case <-d.stopCh:
				// Already closed.
			default:
				close(d.stopCh)
			}
		case <-d.stopCh:
			// Daemon is shutting down for another reason.
		}
	}()
}

// migrateOldDaemon detects and shuts down an old PID-only daemon (MIGRATE-1).
// An old-style daemon is identified by: PID file exists AND socket file does NOT exist.
// This runs BEFORE socket bind, so it doesn't interfere with new startup.
func (d *Daemon) migrateOldDaemon() {
	// Check if PID file exists.
	_, pidErr := os.Stat(d.pidFile)
	if pidErr != nil {
		return // No PID file — nothing to migrate.
	}

	// Check if socket file exists.
	_, sockErr := os.Stat(d.socketPath)
	if sockErr == nil {
		return // Socket exists — this is a new-style daemon, not an old one.
	}

	// Old-style daemon detected: PID file exists, socket does not.
	core.Logger().Info("feeds: detected old-style daemon, attempting migration")

	pid := d.readPID()
	if pid <= 0 {
		// Invalid PID file — just clean up.
		os.Remove(d.pidFile)
		return
	}

	// Verify the process is alive.
	if !isProcessAliveByPID(pid) {
		// Stale PID file — clean up.
		core.Logger().Info("feeds: old daemon not running, removing stale PID file", "pid", pid)
		os.Remove(d.pidFile)
		return
	}

	// Send SIGTERM and wait for the old daemon to exit.
	core.Logger().Info("feeds: sending SIGTERM to old daemon", "pid", pid)
	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(d.pidFile)
		return
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		core.Logger().Error("feeds: failed to send SIGTERM to old daemon", "pid", pid, "error", err)
		os.Remove(d.pidFile)
		return
	}

	// Wait up to 3 seconds for the process to exit.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !isProcessAliveByPID(pid) {
			core.Logger().Info("feeds: old daemon exited", "pid", pid)
			os.Remove(d.pidFile)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Process didn't exit in time — remove PID file anyway and proceed.
	core.Logger().Error("feeds: old daemon did not exit within timeout", "pid", pid)
	os.Remove(d.pidFile)
}

// installSignalHandler listens for SIGTERM/SIGINT and triggers graceful shutdown.
func (d *Daemon) installSignalHandler() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		select {
		case <-sigCh:
			// Received signal — trigger shutdown.
			if d.stopCh != nil {
				select {
				case <-d.stopCh:
					// Already closed.
				default:
					close(d.stopCh)
				}
			}
		case <-d.stopCh:
			// Daemon is stopping normally.
		}
		signal.Stop(sigCh)
	}()
}

// writePIDFile writes the current process PID to the PID file.
// This is for debugging visibility only — the socket bind is the
// authoritative single-instance check (SOCKET-1).
func (d *Daemon) writePIDFile() error {
	if err := core.EnsureDir(filepath.Dir(d.pidFile), core.DefaultDirMode); err != nil {
		return fmt.Errorf("feeds: ensure pid dir: %w", err)
	}

	pid := os.Getpid()
	return os.WriteFile(d.pidFile, []byte(fmt.Sprintf("%d\n", pid)), 0o600)
}

// readPID reads the PID from the PID file, returning 0 on any error.
func (d *Daemon) readPID() int {
	data, err := os.ReadFile(d.pidFile)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// isProcessAlive checks whether the process in the PID file is still running.
func (d *Daemon) isProcessAlive() bool {
	pid := d.readPID()
	if pid <= 0 {
		return false
	}
	return isProcessAliveByPID(pid)
}

// isProcessAliveByPID checks whether a process with the given PID is still running
// by sending signal 0.
func isProcessAliveByPID(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks existence without actually sending a signal.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// removePIDFile removes the PID file, ignoring "not exist" errors.
func (d *Daemon) removePIDFile() error {
	err := os.Remove(d.pidFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("feeds: remove pid file: %w", err)
	}
	return nil
}
