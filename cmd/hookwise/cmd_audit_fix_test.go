package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// executeCommandWithIn is executeCommand plus a scripted stdin, for the
// interactive --fix prompts.
func executeCommandWithIn(in io.Reader, args ...string) (string, error) {
	rootCmd := newRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetIn(in)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return buf.String(), err
}

// forceInteractive stubs the terminal check for the duration of one test.
func forceInteractive(t *testing.T, interactive bool) {
	t.Helper()
	orig := auditStdinIsInteractive
	auditStdinIsInteractive = func() bool { return interactive }
	t.Cleanup(func() { auditStdinIsInteractive = orig })
}

// duplicateSettings holds one exact duplicate pair on PreToolUse. Single-line
// array so the expected post-fix content is an exact literal.
const duplicateSettings = `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "echo a"}, {"type": "command", "command": "echo a"}]}
    ]
  }
}`

const duplicateSettingsFixed = `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "echo a"}]}
    ]
  }
}`

func TestAuditFixDryRunMutatesNothing(t *testing.T) {
	claudeDir := t.TempDir()
	path := writeAuditSettings(t, claudeDir, duplicateSettings)
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)

	output, err := executeCommand("audit", "--fix", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, output, "eligible for removal")
	assert.Contains(t, output, "Dry run: no changes made.")

	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, duplicateSettings, string(after), "--dry-run must be byte-identical no-op")

	backups, globErr := filepath.Glob(path + ".bak-*")
	require.NoError(t, globErr)
	assert.Empty(t, backups, "--dry-run must not write backups")
}

func TestAuditFixNonInteractiveStdinRefusesToApply(t *testing.T) {
	claudeDir := t.TempDir()
	path := writeAuditSettings(t, claudeDir, duplicateSettings)
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)
	forceInteractive(t, false)

	output, err := executeCommand("audit", "--fix")
	require.NoError(t, err, "non-interactive refusal must exit 0")
	assert.Contains(t, output, "Refusing to apply")
	assert.Contains(t, output, "interactive terminal")

	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, duplicateSettings, string(after))
}

func TestAuditFixInteractiveAcceptRemovesDuplicate(t *testing.T) {
	claudeDir := t.TempDir()
	path := writeAuditSettings(t, claudeDir, duplicateSettings)
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)
	forceInteractive(t, true)

	output, err := executeCommandWithIn(strings.NewReader("y\n"), "audit", "--fix")
	require.NoError(t, err)
	assert.Contains(t, output, "Remove [1/1]")
	assert.Contains(t, output, "OK")
	assert.Contains(t, output, "removed 1 duplicate")

	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, duplicateSettingsFixed, string(after))

	backups, globErr := filepath.Glob(path + ".bak-*")
	require.NoError(t, globErr)
	require.Len(t, backups, 1, "apply must leave a timestamped backup")
	bak, readBakErr := os.ReadFile(backups[0])
	require.NoError(t, readBakErr)
	assert.Equal(t, duplicateSettings, string(bak))
}

func TestAuditFixDeclineByDefaultLeavesFileUntouched(t *testing.T) {
	claudeDir := t.TempDir()
	path := writeAuditSettings(t, claudeDir, duplicateSettings)
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)
	forceInteractive(t, true)

	// Bare Enter (empty answer) must default to No; EOF after it ends input.
	output, err := executeCommandWithIn(strings.NewReader("\n"), "audit", "--fix")
	require.NoError(t, err)
	assert.Contains(t, output, "No removals accepted")

	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, duplicateSettings, string(after))

	backups, globErr := filepath.Glob(path + ".bak-*")
	require.NoError(t, globErr)
	assert.Empty(t, backups, "a fully-declined run must not write backups")
}

func TestAuditFixSecondRunReportsNothingToFix(t *testing.T) {
	claudeDir := t.TempDir()
	writeAuditSettings(t, claudeDir, duplicateSettings)
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)
	forceInteractive(t, true)

	_, err := executeCommandWithIn(strings.NewReader("y\n"), "audit", "--fix")
	require.NoError(t, err)

	output, err := executeCommandWithIn(strings.NewReader(""), "audit", "--fix")
	require.NoError(t, err)
	assert.Contains(t, output, "Nothing to fix")
}

func TestAuditFixNothingToFixOnHealthySettings(t *testing.T) {
	claudeDir := t.TempDir()
	writeAuditSettings(t, claudeDir, healthySettings)
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)

	output, err := executeCommand("audit", "--fix", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, output, "Nothing to fix")
}

func TestAuditFixRejectsJSONCombination(t *testing.T) {
	claudeDir := t.TempDir()
	writeAuditSettings(t, claudeDir, healthySettings)
	t.Setenv("HOOKWISE_CLAUDE_DIR", claudeDir)

	_, err := executeCommand("audit", "--fix", "--json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--json cannot be combined")
}
