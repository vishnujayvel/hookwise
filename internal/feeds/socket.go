package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// SocketServer manages a unix domain socket HTTP server for daemon IPC.
type SocketServer struct {
	socketPath   string
	listener     net.Listener
	server       *http.Server
	shutdownFn   func()              // triggers daemon shutdown
	feedsFn      func() []FeedStatus // returns the daemon's effective feed config
	shutdownOnce sync.Once
	startedAt    time.Time
	mu           sync.Mutex
	boundInfo    os.FileInfo // socket file identity captured at bind time
}

// SetFeedsProvider wires the function that GET /feeds calls to report the
// daemon's effective feed config (#1). Must be called before Start. If unset,
// GET /feeds returns an empty list.
func (s *SocketServer) SetFeedsProvider(fn func() []FeedStatus) {
	s.feedsFn = fn
}

// NewSocketServer creates a new SocketServer.
// shutdownFn is called asynchronously when the /shutdown endpoint is hit.
// startedAt is used to compute uptime for the /health endpoint.
func NewSocketServer(socketPath string, shutdownFn func(), startedAt time.Time) *SocketServer {
	return &SocketServer{
		socketPath: socketPath,
		shutdownFn: shutdownFn,
		startedAt:  startedAt,
	}
}

// acquireArbitrationLock takes an exclusive flock on a sidecar lock file
// next to the socket. bind(2)+listen(2) via net.Listen is not atomic: a
// socket that is bound but not yet listening refuses connections and is
// indistinguishable from a stale one, so two racing daemons could each
// classify the other's live socket as stale and both "win" the bind. The
// flock spans the whole stat/dial/remove/bind sequence so arbitration is
// serialized across processes. The lock file is deliberately never removed:
// unlinking a lock file while another process may be flocking it reopens
// the race on a fresh inode.
//
// Acquisition is non-blocking with a retry deadline rather than a bare
// LOCK_EX: a hung-but-alive holder (SIGSTOP, wedged filesystem) would
// otherwise stall Start and Shutdown indefinitely — Daemon.Stop budgets
// 10s for the whole shutdown. A dead holder is not a concern; the OS
// releases the flock on process exit.
func (s *SocketServer) acquireArbitrationLock(timeout time.Duration) (*os.File, error) {
	f, err := os.OpenFile(s.socketPath+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			f.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("timed out after %s waiting for holder of %s", timeout, s.socketPath+".lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Start binds the unix socket, sets permissions to 0600, registers HTTP
// routes, and begins serving. Before binding, it performs stale socket
// detection: if a socket file exists but cannot be connected to, it is
// removed to allow a fresh bind (SOCKET-3). The detect-remove-bind sequence
// runs under a cross-process flock so concurrent starts cannot misclassify
// a bound-but-not-yet-listening socket as stale.
func (s *SocketServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure parent directory exists.
	if err := core.EnsureDir(filepath.Dir(s.socketPath), core.DefaultDirMode); err != nil {
		return fmt.Errorf("feeds: ensure socket dir: %w", err)
	}

	// 5s bound: a healthy holder's critical section is at most one failed
	// 500ms dial plus a few syscalls, so even a queue of racers drains fast.
	lock, err := s.acquireArbitrationLock(5 * time.Second)
	if err != nil {
		return fmt.Errorf("feeds: acquire socket arbitration lock: %w", err)
	}
	defer lock.Close() // closing the fd releases the flock

	// Stale socket detection (SOCKET-3): if the file exists, try to connect.
	if _, err := os.Stat(s.socketPath); err == nil {
		conn, dialErr := net.DialTimeout("unix", s.socketPath, 500*time.Millisecond)
		if dialErr != nil {
			// Cannot connect — stale socket file. Remove it.
			if removeErr := os.Remove(s.socketPath); removeErr != nil {
				return fmt.Errorf("feeds: remove stale socket: %w", removeErr)
			}
		} else {
			// Socket is alive — another server is running.
			conn.Close()
			return fmt.Errorf("feeds: socket already in use: %s", s.socketPath)
		}
	}

	// Bind the unix socket (SOCKET-1: socket bind is single-instance authority).
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("feeds: bind socket: %w", err)
	}
	s.listener = listener

	// Go's UnixListener unlinks the path unconditionally on Close — if another
	// daemon has replaced the file by then, that would delete the successor's
	// live socket. Disable it; Shutdown owns removal with an identity check.
	if ul, ok := listener.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}

	// Capture the bound socket file's identity so Shutdown only ever removes
	// the file this instance created, never a successor's. On stat failure
	// boundInfo stays nil and Shutdown skips removal — the file is then
	// reclaimed by the next Start's stale detection.
	if fi, statErr := os.Stat(s.socketPath); statErr == nil {
		s.boundInfo = fi
	} else {
		core.Logger().Warn("feeds: could not capture bound socket identity; shutdown will not remove the socket file", "path", s.socketPath, "error", statErr)
	}

	// Set socket file permissions to 0600 (SOCKET-2).
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		listener.Close()
		os.Remove(s.socketPath)
		return fmt.Errorf("feeds: chmod socket: %w", err)
	}

	// Register routes.
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/feeds", s.handleFeeds)
	mux.HandleFunc("/shutdown", s.handleShutdown)

	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
	}

	// Serve in a background goroutine (ARCH-7: panic recovery).
	go func() {
		defer func() {
			if r := recover(); r != nil {
				core.Logger().Error("feeds: socket serve goroutine panic recovered", "recovered", fmt.Sprintf("%v", r))
			}
		}()
		// Serve returns http.ErrServerClosed on graceful shutdown — that's expected.
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "hookwise: socket server error: %v\n", err)
		}
	}()

	return nil
}

// Shutdown gracefully shuts down the HTTP server and removes the socket file.
func (s *SocketServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var firstErr error

	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("feeds: shutdown server: %w", err)
		}
	}

	// Remove the socket file — only under the arbitration flock, and only if
	// it is still the file this instance bound. If another daemon has since
	// replaced it, removing by path would delete the other daemon's live
	// socket. If the lock cannot be acquired, skip removal entirely: an
	// unlocked check-and-remove is the same TOCTOU this lock exists to close,
	// and leaving a stale file for the next Start's stale detection is
	// strictly safer.
	if lock, lockErr := s.acquireArbitrationLock(2 * time.Second); lockErr == nil {
		defer lock.Close()
		if fi, statErr := os.Stat(s.socketPath); statErr == nil {
			if s.boundInfo != nil && os.SameFile(s.boundInfo, fi) {
				if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
					if firstErr == nil {
						firstErr = fmt.Errorf("feeds: remove socket: %w", err)
					}
				}
			}
		}
	}

	return firstErr
}

// handleHealth responds to GET /health with daemon status information.
func (s *SocketServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	uptime := time.Since(s.startedAt).Round(time.Second).String()

	resp := map[string]interface{}{
		"status": "ok",
		"pid":    os.Getpid(),
		"uptime": uptime,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleFeeds responds to GET /feeds with the daemon's effective feed config
// (#1): the per-feed enabled flag + interval the daemon is actually polling
// with. doctor queries this so it reports against the daemon's real state
// rather than re-deriving from possibly-divergent on-disk config.
func (s *SocketServer) handleFeeds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	statuses := []FeedStatus{}
	if s.feedsFn != nil {
		if got := s.feedsFn(); got != nil {
			statuses = got
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"feeds": statuses})
}

// handleShutdown responds to POST /shutdown and triggers the shutdown function
// asynchronously.
func (s *SocketServer) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := map[string]interface{}{
		"status": "shutting_down",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	// Trigger shutdown asynchronously so the response is sent first.
	// sync.Once ensures idempotency if multiple /shutdown requests arrive.
	if s.shutdownFn != nil {
		s.shutdownOnce.Do(func() {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						core.Logger().Error("feeds: shutdown goroutine panic recovered", "recovered", fmt.Sprintf("%v", r))
					}
				}()
				s.shutdownFn()
			}()
		})
	}
}
