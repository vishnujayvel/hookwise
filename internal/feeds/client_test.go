package feeds

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// DaemonClient tests
// ---------------------------------------------------------------------------

func TestDaemonClient_IsRunning_WhenRunning(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "d.sock")

	srv := NewSocketServer(socketPath, func() {}, time.Now())
	require.NoError(t, srv.Start())
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	client := NewDaemonClient(socketPath)
	assert.True(t, client.IsRunning(), "IsRunning should return true when socket server is listening")
}

func TestDaemonClient_IsRunning_WhenNotRunning(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "nonexistent.sock")

	client := NewDaemonClient(socketPath)
	assert.False(t, client.IsRunning(), "IsRunning should return false when no server is listening")
}

func TestDaemonClient_Health(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "d.sock")

	startedAt := time.Now().Add(-30 * time.Second)
	srv := NewSocketServer(socketPath, func() {}, startedAt)
	require.NoError(t, srv.Start())
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	client := NewDaemonClient(socketPath)
	health, err := client.Health()
	require.NoError(t, err)

	assert.Equal(t, "ok", health["status"])
	assert.NotZero(t, health["pid"])
	assert.NotEmpty(t, health["uptime"])
}

func TestDaemonClient_Health_WhenNotRunning(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "nonexistent.sock")

	client := NewDaemonClient(socketPath)
	_, err := client.Health()
	assert.Error(t, err, "Health should return error when daemon is not running")
}

func TestDaemonClient_Shutdown(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "d.sock")

	shutdownCh := make(chan struct{}, 1)
	var srv *SocketServer
	srv = NewSocketServer(socketPath, func() {
		shutdownCh <- struct{}{}
		// Actually shut down the server so the socket disappears,
		// allowing the client's post-shutdown poll to succeed.
		_ = srv.Shutdown(context.Background())
	}, time.Now())
	require.NoError(t, srv.Start())
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	client := NewDaemonClient(socketPath)
	err := client.Shutdown()
	require.NoError(t, err)

	// Verify the shutdown function was triggered.
	select {
	case <-shutdownCh:
		// OK
	case <-time.After(2 * time.Second):
		require.FailNow(t, "shutdown function was not called within timeout")
	}
}

func TestDaemonClient_Shutdown_WhenNotRunning(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "nonexistent.sock")

	client := NewDaemonClient(socketPath)
	err := client.Shutdown()
	assert.Error(t, err, "Shutdown should return error when daemon is not running")
}

func TestDaemonClient_EnsureDaemon_AlreadyRunning(t *testing.T) {
	socketPath := filepath.Join(socketTempDir(t), "d.sock")

	srv := NewSocketServer(socketPath, func() {}, time.Now())
	require.NoError(t, srv.Start())
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	client := NewDaemonClient(socketPath)
	err := client.EnsureDaemon("some-config-path.yaml")
	assert.NoError(t, err, "EnsureDaemon should succeed without spawning when daemon is already running")
}

func TestDaemonClient_HealthResponseFormat(t *testing.T) {
	// Verify that the Health response contains the expected keys with correct types.
	socketPath := filepath.Join(socketTempDir(t), "d.sock")

	srv := NewSocketServer(socketPath, func() {}, time.Now())
	require.NoError(t, srv.Start())
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	client := NewDaemonClient(socketPath)

	// Use the raw HTTP client to verify JSON format.
	resp, err := client.httpClient.Get("http://unix/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var health map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &health))

	// Verify all expected keys exist.
	assert.Contains(t, health, "status")
	assert.Contains(t, health, "pid")
	assert.Contains(t, health, "uptime")
}

func TestNewDaemonClient_DefaultTimeouts(t *testing.T) {
	socketPath := "/tmp/test.sock"
	client := NewDaemonClient(socketPath)

	assert.Equal(t, socketPath, client.socketPath)
	assert.Equal(t, 3*time.Second, client.readyTimeout)
	assert.Equal(t, 100*time.Millisecond, client.connectTimeout)
	assert.NotNil(t, client.httpClient)
}

func TestDaemonClient_UnixSocketTransport(t *testing.T) {
	// Verify the HTTP client is configured with unix socket transport.
	socketPath := filepath.Join(socketTempDir(t), "d.sock")

	srv := NewSocketServer(socketPath, func() {}, time.Now())
	require.NoError(t, srv.Start())
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	client := NewDaemonClient(socketPath)

	// The transport should be able to connect to the unix socket.
	// Verify by making a request through it.
	resp, err := client.httpClient.Get("http://unix/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, 200, resp.StatusCode)
}

func TestDaemonClient_IsRunning_DialTimeout(t *testing.T) {
	// Create a socket that accepts connections but doesn't respond.
	// This tests that IsRunning doesn't hang forever.
	socketPath := filepath.Join(socketTempDir(t), "slow.sock")

	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })

	client := NewDaemonClient(socketPath)
	// Should still return true because the socket accepts connections.
	assert.True(t, client.IsRunning())
}
