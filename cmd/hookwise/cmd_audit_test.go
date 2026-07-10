package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/hooks"
)

// writeAuditSettings writes a settings.json into dir and returns its path.
func writeAuditSettings(t *testing.T, dir, content string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	path := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// healthySettings has two hooks whose binary ("echo") is always on PATH, so
// the scan produces no WARN/FAIL findings.
const healthySettings = `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "", "hooks": [{"type": "command", "command": "echo pre"}]}
    ],
    "PostToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "echo post"}]}
    ]
  }
}`

func TestAuditTextRendersInventoryAndPerformance(t *testing.T) {
	claudeDir := t.TempDir()
	writeAuditSettings(t, claudeDir, healthySettings)
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)

	output, err := executeCommand("audit")

	require.NoError(t, err, "PASS must exit 0")
	assert.Contains(t, output, "SCAN  hooks: 2 hooks across 2 events")
	assert.Contains(t, output, "PreToolUse:")
	assert.Contains(t, output, "PostToolUse:")
	assert.Contains(t, output, "PERFORMANCE")
	assert.Contains(t, output, "latency tracking not enabled")
	assert.Contains(t, output, "Result: PASS")
}

func TestAuditJSONIsSchemaVersionedAndOmitsPerformance(t *testing.T) {
	claudeDir := t.TempDir()
	writeAuditSettings(t, claudeDir, healthySettings)
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)

	output, err := executeCommand("audit", "--json")
	require.NoError(t, err)

	// Assert on a raw map so we catch keys that must NOT exist (performance)
	// and the presence of schema_version regardless of struct tags.
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &raw), "output must be valid JSON")
	assert.EqualValues(t, 1, raw["schema_version"])
	assert.NotContains(t, raw, "performance",
		"latency data does not exist yet — JSON must omit it, never fabricate")

	var report auditReport
	require.NoError(t, json.Unmarshal([]byte(output), &report))
	assert.Equal(t, 2, report.Inventory.TotalHooks)
	assert.Equal(t, map[string]int{"PreToolUse": 1, "PostToolUse": 1}, report.Inventory.ByEvent)
	assert.Equal(t, "PASS", report.Summary.Result)
	require.NotEmpty(t, report.Findings)
	assert.Equal(t, hooks.LevelScan, report.Findings[0].Level)
}

func TestAuditMalformedSettingsFailFindingNoPanic(t *testing.T) {
	claudeDir := t.TempDir()
	writeAuditSettings(t, claudeDir, `{"hooks": {`) // truncated JSON
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)

	output, err := executeCommand("audit", "--json")

	// Malformed settings must be reported as a FAIL finding and exit 1,
	// never crash (ARCH-1 fail-open: report, don't panic).
	require.Error(t, err, "FAIL must exit non-zero")

	var report auditReport
	require.NoError(t, json.Unmarshal([]byte(output), &report), "JSON must still be emitted on FAIL")
	assert.Equal(t, "FAIL", report.Summary.Result)
	require.NotEmpty(t, report.Findings)
	assert.Equal(t, hooks.LevelFail, report.Findings[0].Level)
	assert.Equal(t, "hook-settings", report.Findings[0].Code)
	assert.Contains(t, report.Findings[0].Message, "could not be parsed")
}

func TestAuditExitCodeFailOnMissingBinary(t *testing.T) {
	claudeDir := t.TempDir()
	writeAuditSettings(t, claudeDir, `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "", "hooks": [{"type": "command", "command": "hookwise-definitely-not-installed-xyz run"}]}
    ]
  }
}`)
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)

	output, err := executeCommand("audit")

	require.Error(t, err)
	assert.ErrorIs(t, err, errAuditFailed)
	assert.Contains(t, output, "hook-binary")
	assert.Contains(t, output, "Result: FAIL")
}

func TestAuditWarnStillExitsZero(t *testing.T) {
	claudeDir := t.TempDir()
	// A network-dependent runner on a hot-path event yields WARN hook-network.
	writeAuditSettings(t, claudeDir, `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "", "hooks": [{"type": "command", "command": "curl https://example.com/hook"}]}
    ]
  }
}`)
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)

	output, err := executeCommand("audit")

	require.NoError(t, err, "WARN must exit 0 — only FAIL is exit 1")
	assert.Contains(t, output, "hook-network")
	assert.Contains(t, output, "Result: WARN")
}

func TestAuditProjectDirScansProjectSettingsPair(t *testing.T) {
	// User-level dir has one hook; the project pair has different ones. With
	// --project-dir only the project settings must be scanned.
	userDir := t.TempDir()
	writeAuditSettings(t, userDir, `{
  "hooks": {"SessionStart": [{"matcher": "", "hooks": [{"type": "command", "command": "echo user-level"}]}]}
}`)
	t.Setenv("HOOKWISE_CLAUDE_DIR", userDir)

	projectDir := t.TempDir()
	projectClaude := filepath.Join(projectDir, ".claude")
	writeAuditSettings(t, projectClaude, `{
  "hooks": {"PreToolUse": [{"matcher": "", "hooks": [{"type": "command", "command": "echo project-shared"}]}]}
}`)
	require.NoError(t, os.WriteFile(filepath.Join(projectClaude, "settings.local.json"), []byte(`{
  "hooks": {"PostToolUse": [{"matcher": "", "hooks": [{"type": "command", "command": "echo project-local"}]}]}
}`), 0o600))

	output, err := executeCommand("audit", "--project-dir", projectDir, "--json")
	require.NoError(t, err)

	var report auditReport
	require.NoError(t, json.Unmarshal([]byte(output), &report))
	assert.Equal(t, hooks.ProjectSettingsPaths(projectDir), report.SettingsPaths)
	assert.Equal(t, 2, report.Inventory.TotalHooks,
		"both project settings.json and settings.local.json hooks are scanned")
	assert.NotContains(t, output, "user-level", "user settings must not leak into a project scan")

	commands := []string{}
	for _, h := range report.Inventory.Hooks {
		commands = append(commands, h.Command)
	}
	assert.ElementsMatch(t, []string{"echo project-shared", "echo project-local"}, commands)
}

func TestAuditEmptySettingsIsPass(t *testing.T) {
	claudeDir := t.TempDir() // no settings files at all
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)

	output, err := executeCommand("audit")

	require.NoError(t, err)
	assert.Contains(t, output, "0 hooks across 0 events")
	assert.Contains(t, output, "Result: PASS")
}

func TestAuditSummarize(t *testing.T) {
	tests := []struct {
		name     string
		findings []hooks.Finding
		want     auditSummary
	}{
		{
			name:     "no findings is PASS",
			findings: nil,
			want:     auditSummary{Result: "PASS"},
		},
		{
			name:     "scan-only is PASS",
			findings: []hooks.Finding{{Level: hooks.LevelScan}},
			want:     auditSummary{Result: "PASS"},
		},
		{
			name:     "info counts as warning",
			findings: []hooks.Finding{{Level: hooks.LevelInfo}},
			want:     auditSummary{Result: "WARN", Warnings: 1},
		},
		{
			name:     "warn is WARN",
			findings: []hooks.Finding{{Level: hooks.LevelScan}, {Level: hooks.LevelWarn}},
			want:     auditSummary{Result: "WARN", Warnings: 1},
		},
		{
			name: "any fail dominates",
			findings: []hooks.Finding{
				{Level: hooks.LevelWarn},
				{Level: hooks.LevelFail},
				{Level: hooks.LevelInfo},
			},
			want: auditSummary{Result: "FAIL", Warnings: 2, Failures: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, summarize(tt.findings))
		})
	}
}
