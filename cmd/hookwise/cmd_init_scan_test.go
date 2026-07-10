package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupInitScanEnv isolates an init run: a temp working directory (so
// hookwise.yaml lands there), a temp state dir, and a temp Claude dir that
// DefaultSettingsPaths resolves via HOOKWISE_CLAUDE_DIR. Returns the Claude
// dir and the state dir.
func setupInitScanEnv(t *testing.T) (claudeDir, stateDir string) {
	t.Helper()
	tmpDir := t.TempDir()

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stateDir = filepath.Join(tmpDir, ".hookwise")
	t.Setenv("HOOKWISE_STATE_DIR", stateDir)

	claudeDir = filepath.Join(tmpDir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)

	return claudeDir, stateDir
}

// settingsWithHooks is a realistic settings.json with hooks on two events,
// including a network-dependent hot-path hook so AllFindings produces a
// non-SCAN finding to render.
const settingsWithHooks = `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "",
        "hooks": [
          {"type": "command", "command": "guardian check"},
          {"type": "command", "command": "curl -s https://example.com/audit"}
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {"type": "command", "command": "logger post-bash"}
        ]
      }
    ]
  }
}
`

func TestInitScansExistingHooks(t *testing.T) {
	claudeDir, stateDir := setupInitScanEnv(t)
	settingsPath := filepath.Join(claudeDir, "settings.json")
	require.NoError(t, os.WriteFile(settingsPath, []byte(settingsWithHooks), 0o600))

	output, err := executeCommand("init")
	require.NoError(t, err, "init failed: %s", output)

	// Inventory + findings render before the config-created line.
	assert.Contains(t, output, "Scanning existing Claude Code hooks...")
	assert.Contains(t, output, "3 hooks across 2 events")
	assert.Contains(t, output, "PreToolUse:")
	assert.Contains(t, output, "PostToolUse:")
	// The curl hook on a hot path must surface a rendered finding.
	assert.Contains(t, output, "hook-network")
	assert.NotContains(t, output, "No existing Claude Code hooks found")

	// The scan happens before the template write.
	scanIdx := indexOf(t, output, "Scanning existing Claude Code hooks...")
	createdIdx := indexOf(t, output, "Created")
	assert.Less(t, scanIdx, createdIdx, "scan output must precede config creation")

	// Audit report exists and round-trips.
	auditPath := filepath.Join(stateDir, "hook-audit.json")
	data, err := os.ReadFile(auditPath)
	require.NoError(t, err, "hook-audit.json must be written")

	var report struct {
		GeneratedAt  string `json:"generated_at"`
		ScannedPaths []string
		Hooks        []struct {
			Event      string `json:"event"`
			Command    string `json:"command"`
			SourceFile string `json:"source_file"`
		} `json:"hooks"`
		Findings []struct {
			Level string
			Code  string
		} `json:"findings"`
	}
	require.NoError(t, json.Unmarshal(data, &report))
	assert.NotEmpty(t, report.GeneratedAt)
	assert.Len(t, report.Hooks, 3)
	assert.NotEmpty(t, report.Findings)
	for _, h := range report.Hooks {
		assert.Equal(t, settingsPath, h.SourceFile)
	}
	// No leftover temp file from the atomic write.
	tmps, err := filepath.Glob(filepath.Join(stateDir, ".tmp-*"))
	require.NoError(t, err)
	assert.Empty(t, tmps)
}

func TestInitScanEmptyPath(t *testing.T) {
	_, stateDir := setupInitScanEnv(t)
	// No settings files exist at all.

	output, err := executeCommand("init")
	require.NoError(t, err, "init failed: %s", output)

	assert.Contains(t, output, "No existing Claude Code hooks found")
	assert.NotContains(t, output, "hooks across")
	assert.Contains(t, output, "Created")

	// The audit is still written, recording the clean scan.
	data, err := os.ReadFile(filepath.Join(stateDir, "hook-audit.json"))
	require.NoError(t, err)
	var report struct {
		Hooks    []any `json:"hooks"`
		Findings []any `json:"findings"`
	}
	require.NoError(t, json.Unmarshal(data, &report))
	assert.Empty(t, report.Hooks)
}

func TestInitNeverMutatesSettings(t *testing.T) {
	claudeDir, _ := setupInitScanEnv(t)
	settingsPath := filepath.Join(claudeDir, "settings.json")
	localPath := filepath.Join(claudeDir, "settings.local.json")
	require.NoError(t, os.WriteFile(settingsPath, []byte(settingsWithHooks), 0o600))
	localBytes := []byte(`{"hooks": {"Stop": [{"matcher": "", "hooks": [{"type": "command", "command": "say done"}]}]}}` + "\n")
	require.NoError(t, os.WriteFile(localPath, localBytes, 0o600))

	before, err := os.ReadFile(settingsPath)
	require.NoError(t, err)
	beforeLocal, err := os.ReadFile(localPath)
	require.NoError(t, err)

	output, err := executeCommand("init")
	require.NoError(t, err, "init failed: %s", output)

	after, err := os.ReadFile(settingsPath)
	require.NoError(t, err)
	afterLocal, err := os.ReadFile(localPath)
	require.NoError(t, err)

	assert.Equal(t, before, after, "init must leave settings.json byte-identical")
	assert.Equal(t, beforeLocal, afterLocal, "init must leave settings.local.json byte-identical")
}

func TestInitScanReportsParseErrors(t *testing.T) {
	claudeDir, stateDir := setupInitScanEnv(t)
	settingsPath := filepath.Join(claudeDir, "settings.json")
	malformed := []byte(`{"hooks": not-json`)
	require.NoError(t, os.WriteFile(settingsPath, malformed, 0o600))

	output, err := executeCommand("init")
	require.NoError(t, err, "init must not fail on malformed settings: %s", output)

	assert.Contains(t, output, "could not be parsed")
	assert.Contains(t, output, "Created")

	// Malformed input is recorded in the audit and never rewritten on disk.
	data, err := os.ReadFile(filepath.Join(stateDir, "hook-audit.json"))
	require.NoError(t, err)
	var report struct {
		ParseErrors []string `json:"parse_errors"`
	}
	require.NoError(t, json.Unmarshal(data, &report))
	assert.Len(t, report.ParseErrors, 1)

	after, err := os.ReadFile(settingsPath)
	require.NoError(t, err)
	assert.Equal(t, malformed, after)
}

func TestInitSkipsScanWhenConfigExists(t *testing.T) {
	claudeDir, stateDir := setupInitScanEnv(t)
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settingsWithHooks), 0o600))

	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "hookwise.yaml"), []byte("version: 1\n"), 0o644))

	output, err := executeCommand("init")
	require.NoError(t, err)

	assert.Contains(t, output, "already exists")
	assert.NotContains(t, output, "Scanning existing Claude Code hooks")
	_, statErr := os.Stat(filepath.Join(stateDir, "hook-audit.json"))
	assert.True(t, os.IsNotExist(statErr), "no audit should be written when init short-circuits")
}

// indexOf returns the byte index of substr in s, failing the test if absent.
func indexOf(t *testing.T, s, substr string) int {
	t.Helper()
	idx := strings.Index(s, substr)
	require.GreaterOrEqual(t, idx, 0, "expected output to contain %q", substr)
	return idx
}
