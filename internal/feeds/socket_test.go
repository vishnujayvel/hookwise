package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// socketTempDir creates a short temp directory suitable for unix socket paths.
// macOS /var/folders paths can exceed the 104-byte unix socket limit, so we
// use /tmp directly.
func socketTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hw-sock-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// unixClient returns an HTTP client that dials the given unix socket.
func unixClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 2 * time.Second,
	}
}

func TestSocketServer_StartAndHealth(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "d.sock")
	startedAt := time.Now().Add(-2 * time.Hour) // fake 2h ago for deterministic uptime

	srv := NewSocketServer(socketPath, func() {}, startedAt)
	require.NoError(t, srv.Start())
	t.Cleanup(func() {
		_ = srv.Shutdown(context.Background())
	})

	client := unixClient(socketPath)
	resp, err := client.Get("http://unix/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var health map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &health))

	assert.Equal(t, "ok", health["status"])
	assert.NotZero(t, health["pid"])
	assert.NotEmpty(t, health["uptime"])

	// Uptime should contain "h" since we set startedAt to 2h ago.
	uptime, ok := health["uptime"].(string)
	require.True(t, ok)
	assert.Contains(t, uptime, "h")
}

func TestSocketServer_Shutdown(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "d.sock")
	var shutdownCalled atomic.Bool

	srv := NewSocketServer(socketPath, func() {
		shutdownCalled.Store(true)
	}, time.Now())
	require.NoError(t, srv.Start())

	client := unixClient(socketPath)
	resp, err := client.Post("http://unix/shutdown", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &result))
	assert.Equal(t, "shutting_down", result["status"])

	// Wait briefly for the async shutdownFn goroutine to fire.
	require.Eventually(t, func() bool {
		return shutdownCalled.Load()
	}, 2*time.Second, 10*time.Millisecond, "shutdownFn should have been called")

	// Clean up the server.
	_ = srv.Shutdown(context.Background())
}

func TestSocketServer_StaleSocketCleanup(t *testing.T) {
	tmpDir := socketTempDir(t)
	socketPath := filepath.Join(tmpDir, "d.sock")

	// Create a regular file at the socket path (simulates a stale socket).
	require.NoError(t, os.WriteFile(socketPath, []byte("stale"), 0o600))

	srv := NewSocketServer(socketPath, func() {}, time.Now())
	require.NoError(t, srv.Start())
	t.Cleanup(func() {
		_ = srv.Shutdown(context.Background())
	})

	// Verify the server is actually listening by hitting /health.
	client := unixClient(socketPath)
	resp, err := client.Get("http://unix/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSocketServer_SocketPermissions(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "d.sock")

	srv := NewSocketServer(socketPath, func() {}, time.Now())
	require.NoError(t, srv.Start())
	t.Cleanup(func() {
		_ = srv.Shutdown(context.Background())
	})

	info, err := os.Stat(socketPath)
	require.NoError(t, err)

	// On unix systems, socket files may have different mode bits depending on
	// the OS, but the permission bits should be 0600.
	perm := info.Mode().Perm()
	assert.Equal(t, os.FileMode(0o600), perm,
		fmt.Sprintf("expected socket permissions 0600, got %o", perm))
}

func TestSocketServer_DoubleStart(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "d.sock")

	srv1 := NewSocketServer(socketPath, func() {}, time.Now())
	require.NoError(t, srv1.Start())
	t.Cleanup(func() {
		_ = srv1.Shutdown(context.Background())
	})

	// Second server on same path should fail because the first is still alive
	// (dial test succeeds -> socket is not stale).
	srv2 := NewSocketServer(socketPath, func() {}, time.Now())
	err := srv2.Start()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in use")
}

func TestSocketServer_ShutdownRemovesSocketFile(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "d.sock")

	srv := NewSocketServer(socketPath, func() {}, time.Now())
	require.NoError(t, srv.Start())

	// Verify socket file exists.
	_, err := os.Stat(socketPath)
	require.NoError(t, err)

	// Shutdown should remove the socket file.
	require.NoError(t, srv.Shutdown(context.Background()))

	_, err = os.Stat(socketPath)
	assert.True(t, os.IsNotExist(err), "socket file should be removed after shutdown")
}

func TestSocketServer_ShutdownOnlyPOST(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "d.sock")

	srv := NewSocketServer(socketPath, func() {}, time.Now())
	require.NoError(t, srv.Start())
	t.Cleanup(func() {
		_ = srv.Shutdown(context.Background())
	})

	client := unixClient(socketPath)

	// GET /shutdown should be rejected.
	resp, err := client.Get("http://unix/shutdown")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}
