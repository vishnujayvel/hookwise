package feeds

import (
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
	staggerOffset time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
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

// SetStaggerOffset overrides the stagger delay between feed goroutine starts
// (used in tests to speed up execution).
func (d *Daemon) SetStaggerOffset(offset time.Duration) {
	d.staggerOffset = offset
}

// Start writes the PID file and launches feed goroutines.
// Returns an error if the daemon is already running (PID file exists and
// process is alive).
func (d *Daemon) Start() error {
	// Ensure parent directory exists.
	if err := core.EnsureDir(filepath.Dir(d.pidFile), core.DefaultDirMode); err != nil {
		return fmt.Errorf("feeds: ensure pid dir: %w", err)
	}

	// Attempt to create PID file exclusively to prevent double-start.
	f, err := os.OpenFile(d.pidFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			// PID file exists — check if the process is still alive.
			if d.isProcessAlive() {
				return fmt.Errorf("feeds: daemon already running (pid file %s)", d.pidFile)
			}
			// Stale PID file — remove and retry.
			os.Remove(d.pidFile)
			f, err = os.OpenFile(d.pidFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return fmt.Errorf("feeds: create pid file after stale cleanup: %w", err)
			}
		} else {
			return fmt.Errorf("feeds: create pid file: %w", err)
		}
	}

	pid := os.Getpid()
	if _, err := fmt.Fprintf(f, "%d\n", pid); err != nil {
		f.Close()
		os.Remove(d.pidFile)
		return fmt.Errorf("feeds: write pid: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(d.pidFile)
		return fmt.Errorf("feeds: close pid file: %w", err)
	}

	d.stopCh = make(chan struct{})

	// Install signal handler for graceful shutdown.
	d.installSignalHandler()

	// Launch feed goroutines.
	d.runAllFeeds()

	return nil
}

// Stop signals all feed goroutines to stop, waits for them to finish,
// and removes the PID file.
func (d *Daemon) Stop() error {
	if d.stopCh != nil {
		close(d.stopCh)
	}
	d.wg.Wait()
	return d.removePIDFile()
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
