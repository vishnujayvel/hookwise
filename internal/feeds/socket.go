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
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// SocketServer manages a unix domain socket HTTP server for daemon IPC.
type SocketServer struct {
	socketPath   string
	listener     net.Listener
	server       *http.Server
	shutdownFn   func() // triggers daemon shutdown
	shutdownOnce sync.Once
	startedAt    time.Time
	mu           sync.Mutex
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

// Start binds the unix socket, sets permissions to 0600, registers HTTP
// routes, and begins serving. Before binding, it performs stale socket
// detection: if a socket file exists but cannot be connected to, it is
// removed to allow a fresh bind (SOCKET-3).
func (s *SocketServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure parent directory exists.
	if err := core.EnsureDir(filepath.Dir(s.socketPath), core.DefaultDirMode); err != nil {
		return fmt.Errorf("feeds: ensure socket dir: %w", err)
	}

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

	// Set socket file permissions to 0600 (SOCKET-2).
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		listener.Close()
		os.Remove(s.socketPath)
		return fmt.Errorf("feeds: chmod socket: %w", err)
	}

	// Register routes.
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/shutdown", s.handleShutdown)

	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
	}

	// Serve in a background goroutine.
	go func() {
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

	// Remove the socket file.
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		if firstErr == nil {
			firstErr = fmt.Errorf("feeds: remove socket: %w", err)
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
			go s.shutdownFn()
		})
	}
}
