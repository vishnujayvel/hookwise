package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTUIEnv marks a child copy of the test binary as a fake TUI process: it
// sleeps instead of running the test suite (helper-process pattern, as in
// os/exec's own tests). Copying the test binary — not /bin/sleep — matters on
// macOS, where byte-copying a signed platform binary invalidates its signature
// and the kernel SIGKILLs it at exec; the ad-hoc-signed test binary copies fine.
const fakeTUIEnv = "HOOKWISE_TEST_FAKE_TUI"

func TestMain(m *testing.M) {
	if os.Getenv(fakeTUIEnv) == "1" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeTUIBinary copies the running test binary into dir as "hookwise-tui" so a
// spawned process passes isTUIComm (comm basename contains "hookwise-tui")
// without needing a real Python TUI installed.
func fakeTUIBinary(t *testing.T, dir string) string {
	t.Helper()
	self, err := os.Executable()
	require.NoError(t, err)
	data, err := os.ReadFile(self)
	require.NoError(t, err)
	fake := filepath.Join(dir, "hookwise-tui")
	require.NoError(t, os.WriteFile(fake, data, 0o755))
	return fake
}

// ---------------------------------------------------------------------------
// Registration + flag parsing
// ---------------------------------------------------------------------------

func Test_TUICmd_Registered(t *testing.T) {
	root := newRootCmd()
	for _, c := range root.Commands() {
		if c.Name() == "tui" {
			assert.NotNil(t, c.Flags().Lookup("launch-method"),
				"tui command must expose --launch-method")
			return
		}
	}
	t.Fatal("tui command not registered on root command")
}

func Test_TUICmd_InvalidLaunchMethod_Errors(t *testing.T) {
	t.Setenv("HOOKWISE_STATE_DIR", t.TempDir())
	_, err := executeCommand("tui", "--launch-method", "split")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid --launch-method "split"`)
}

// ---------------------------------------------------------------------------
// Launch-method resolution (pure)
// ---------------------------------------------------------------------------

func Test_resolveTUILaunchMethod(t *testing.T) {
	cases := []struct {
		name         string
		flagMethod   string
		configMethod string
		want         string
		wantErr      bool
	}{
		{"flag wins over config", "background", "newWindow", "background", false},
		{"invalid flag errors", "split", "newWindow", "", true},
		{"empty flag falls to config", "", "background", "background", false},
		{"empty flag and config falls to newWindow", "", "", "newWindow", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveTUILaunchMethod(tc.flagMethod, tc.configMethod)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Already-running path: temp state dir + PID file pointing at a live process
// whose comm passes the TUI comm filter -> report PID, exit 0, no spawn.
// ---------------------------------------------------------------------------

func Test_runTUI_AlreadyRunning_ReportsPID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmp)

	fake := fakeTUIBinary(t, tmp)
	proc := exec.Command(fake)
	proc.Env = append(os.Environ(), fakeTUIEnv+"=1")
	require.NoError(t, proc.Start())
	t.Cleanup(func() {
		_ = proc.Process.Kill()
		_, _ = proc.Process.Wait()
	})

	pid := proc.Process.Pid
	require.NoError(t, os.WriteFile(
		filepath.Join(tmp, "tui.pid"), []byte(strconv.Itoa(pid)), 0o600))

	var buf bytes.Buffer
	err := runTUI(&buf, "")
	require.NoError(t, err, "already-running must exit 0")
	assert.Contains(t, buf.String(), fmt.Sprintf("TUI is already running (PID %d)", pid))
	assert.NoFileExists(t, filepath.Join(tmp, "tui.launch.marker"),
		"already-running must not claim the launch marker")
}

// ---------------------------------------------------------------------------
// Missing-binary path: PATH scrubbed -> actionable error, nonzero, no panic.
// (Scrubbing PATH also disables pgrep/ps, so a real TUI on the host machine
// cannot leak into the detection and flake the test.)
// ---------------------------------------------------------------------------

func Test_runTUI_MissingBinary_Errors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmp)
	t.Setenv("PATH", tmp) // empty dir: no hookwise-tui, no pgrep, no ps

	var buf bytes.Buffer
	err := runTUI(&buf, "")
	require.Error(t, err, "missing hookwise-tui must be a nonzero-exit error")
	assert.Contains(t, err.Error(), "hookwise-tui not found in PATH")
	assert.Contains(t, err.Error(), "uv tool install", "error must be actionable")
}

// ---------------------------------------------------------------------------
// Happy path: fake hookwise-tui on PATH, nothing running -> delegates to the
// existing singleton launch path (marker claimed in the temp state dir).
// ---------------------------------------------------------------------------

func Test_runTUI_Launches_ViaSingletonGuard(t *testing.T) {
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	t.Setenv("HOOKWISE_STATE_DIR", tmp)
	fakeTUIBinary(t, binDir)
	t.Setenv("PATH", binDir) // only the fake TUI; pgrep/ps absent -> no host leak
	// realTUISpawn passes the inherited env to the child, so mark fake mode
	// here — the spawned copy must sleep, not recursively run the test suite.
	t.Setenv(fakeTUIEnv, "1")

	var buf bytes.Buffer
	err := runTUI(&buf, "background")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "TUI launching (method: background)")
	assert.FileExists(t, filepath.Join(tmp, "tui.launch.marker"),
		"launch must go through the singleton mtime-marker guard")
}
