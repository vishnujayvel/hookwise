package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// DaemonClient provides daemon connectivity from CLI commands.
type DaemonClient struct {
	socketPath     string
	readyTimeout   time.Duration // 3s default
	connectTimeout time.Duration // 100ms per attempt
	httpClient     *http.Client  // configured for unix socket transport
}

// NewDaemonClient creates a client with default timeouts, configured for
// communication with the daemon over a unix socket.
func NewDaemonClient(socketPath string) *DaemonClient {
	return &DaemonClient{
		socketPath:     socketPath,
		readyTimeout:   3 * time.Second,
		connectTimeout: 100 * time.Millisecond,
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
				DisableKeepAlives: true, // Prevent connection pool leaks on unix sockets.
			},
			Timeout: 2 * time.Second,
		},
	}
}

// EnsureDaemon implements connect-or-start:
//  1. Dial socket with connectTimeout
//  2. If connected, return nil (daemon already running)
//  3. If not, add random jitter (SPAWN-1: 0-200ms), spawn hookwise daemon run
//     with Setsid:true (SPAWN-2), redirect stdout/stderr to DefaultDaemonLogPath
//  4. Poll socket every 100ms for up to readyTimeout
//  5. Return error if timeout (caller fail-opens per ARCH-1)
func (c *DaemonClient) EnsureDaemon(configPath string) error {
	// Step 1-2: Try to connect to existing daemon.
	if c.IsRunning() {
		return nil
	}

	// Step 3: Daemon not running — spawn it.

	// SPAWN-1: Random jitter (0-200ms) to prevent thundering herd.
	jitter := time.Duration(rand.Intn(200)) * time.Millisecond
	time.Sleep(jitter)

	// Re-check after jitter — another process may have started the daemon.
	if c.IsRunning() {
		return nil
	}

	if err := c.spawnDaemon(configPath); err != nil {
		return fmt.Errorf("feeds: spawn daemon: %w", err)
	}

	// Step 4: Poll socket every 100ms for up to readyTimeout.
	deadline := time.Now().Add(c.readyTimeout)
	for time.Now().Before(deadline) {
		if c.IsRunning() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Step 5: Timeout — return error (caller fail-opens per ARCH-1).
	return fmt.Errorf("feeds: daemon did not become ready within %v", c.readyTimeout)
}

// spawnDaemon starts the daemon process in the background.
// SPAWN-2: Setsid:true detaches the daemon from the parent process group.
// SPAWN-3: Config path passed via --config flag to daemon run command.
func (c *DaemonClient) spawnDaemon(configPath string) error {
	// Ensure state directory exists (for daemon log and socket).
	if err := core.EnsureDir(core.DefaultStateDir, core.DefaultDirMode); err != nil {
		return fmt.Errorf("ensure state dir: %w", err)
	}

	// Find our own binary for re-exec.
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	args := []string{"daemon", "run"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	if c.socketPath != core.DefaultSocketPath {
		args = append(args, "--socket", c.socketPath)
	}

	cmd := exec.Command(self, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // SPAWN-2

	// Redirect stdout/stderr to daemon log file.
	logFile, err := os.OpenFile(core.DefaultDaemonLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open daemon log: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start daemon: %w", err)
	}

	// Release the child process — we don't wait for it.
	if err := cmd.Process.Release(); err != nil {
		core.Logger().Debug("feeds: daemon process release", "error", err)
	}
	logFile.Close()

	return nil
}

// Shutdown sends POST /shutdown to the daemon and polls for socket
// disappearance for up to 5 seconds.
func (c *DaemonClient) Shutdown() error {
	resp, err := c.httpClient.Post("http://unix/shutdown", "application/json", nil)
	if err != nil {
		return fmt.Errorf("feeds: shutdown request: %w", err)
	}
	resp.Body.Close()

	// Surface unexpected HTTP status codes.
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("feeds: shutdown returned HTTP %d", resp.StatusCode)
	}

	// Poll for socket disappearance up to 5 seconds.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !c.IsRunning() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("feeds: daemon still running after 5s shutdown wait")
}

// IsRunning checks whether the daemon is reachable by dialing the socket.
func (c *DaemonClient) IsRunning() bool {
	conn, err := net.DialTimeout("unix", c.socketPath, c.connectTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Health sends GET /health to the daemon and returns the parsed JSON response.
func (c *DaemonClient) Health() (map[string]interface{}, error) {
	resp, err := c.httpClient.Get("http://unix/health")
	if err != nil {
		return nil, fmt.Errorf("feeds: health request: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("feeds: decode health response: %w", err)
	}

	return result, nil
}
