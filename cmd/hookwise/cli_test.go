package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// ---------------------------------------------------------------------------
// Helper: execute a cobra command and capture output
// ---------------------------------------------------------------------------

func executeCommand(args ...string) (string, error) {
	rootCmd := newRootCmd()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return buf.String(), err
}

// ---------------------------------------------------------------------------
// 1. Version output format
// ---------------------------------------------------------------------------

func TestVersionOutput(t *testing.T) {
	output, err := executeCommand("--version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify the exact version format: "hookwise version X (commit: Y, built: Z)"
	expected := fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, buildDate)
	if !strings.Contains(output, expected) {
		t.Errorf("version output should contain %q, got: %s", expected, output)
	}
}

// ---------------------------------------------------------------------------
// 2. Init creates hookwise.yaml
// ---------------------------------------------------------------------------

func TestInitCreatesConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Change to temp directory for the init command.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	// Override state dir so we don't touch real home.
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))

	output, err := executeCommand("init")
	if err != nil {
		t.Fatalf("init failed: %v\noutput: %s", err, output)
	}

	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatalf("init did not create %s", configPath)
	}

	// Verify the file content is valid YAML.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("could not read created config: %v", err)
	}
	if !strings.Contains(string(data), "version: 1") {
		t.Error("created config should contain 'version: 1'")
	}

	if !strings.Contains(output, "Created") {
		t.Error("init output should say 'Created'")
	}
	if !strings.Contains(output, "initialized successfully") {
		t.Error("init output should say 'initialized successfully'")
	}
}

// ---------------------------------------------------------------------------
// 3. Init does not overwrite existing file
// ---------------------------------------------------------------------------

func TestInitDoesNotOverwrite(t *testing.T) {
	tmpDir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))

	// Pre-create a config file with known content.
	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	sentinel := "# existing config -- do not overwrite\nversion: 1\n"
	if err := os.WriteFile(configPath, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("init")
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Verify it was NOT overwritten.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != sentinel {
		t.Error("init should not have overwritten the existing config file")
	}

	if !strings.Contains(output, "already exists") {
		t.Errorf("output should mention 'already exists', got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// 4. Doctor with no config reports issues
// ---------------------------------------------------------------------------

func TestDoctorNoConfig(t *testing.T) {
	tmpDir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise-nonexistent"))

	output, err := executeCommand("doctor")
	if err != nil {
		t.Fatalf("doctor failed: %v\noutput: %s", err, output)
	}

	// Should report FAIL for config.
	if !strings.Contains(output, "FAIL") {
		t.Errorf("doctor should report FAIL when no config exists, got: %s", output)
	}
	if !strings.Contains(output, "not found") {
		t.Errorf("doctor should mention 'not found' for missing config, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// 5. Doctor with valid config passes
// ---------------------------------------------------------------------------

func TestDoctorWithValidConfig(t *testing.T) {
	tmpDir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	stateDir := filepath.Join(tmpDir, ".hookwise")
	t.Setenv("HOOKWISE_STATE_DIR", stateDir)
	os.MkdirAll(stateDir, 0o700)

	// Create a valid config.
	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	os.WriteFile(configPath, []byte("version: 1\nguards: []\n"), 0o644)

	output, err := executeCommand("doctor")
	if err != nil {
		t.Fatalf("doctor failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(output, "PASS  config") {
		t.Errorf("doctor should report PASS for valid config, got: %s", output)
	}
	if !strings.Contains(output, "PASS  state-dir") {
		t.Errorf("doctor should report PASS for state dir, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// 6. Status-line disabled shows message
// ---------------------------------------------------------------------------

func TestStatusLineDisabled(t *testing.T) {
	tmpDir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))

	// Config with status_line disabled.
	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	os.WriteFile(configPath, []byte("version: 1\nstatus_line:\n  enabled: false\n"), 0o644)

	output, err := executeCommand("status-line")
	if err != nil {
		t.Fatalf("status-line failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(output, "disabled") {
		t.Errorf("status-line should show 'disabled' message, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// 7. Status-line enabled renders segments
// ---------------------------------------------------------------------------

func TestStatusLineEnabled(t *testing.T) {
	tmpDir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))

	// Config with status_line enabled and some segments.
	configYAML := `version: 1
status_line:
  enabled: true
  segments:
    - builtin: session
    - builtin: cost
`
	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	os.WriteFile(configPath, []byte(configYAML), 0o644)

	output, err := executeCommand("status-line")
	if err != nil {
		t.Fatalf("status-line failed: %v\noutput: %s", err, output)
	}

	// Should contain rendered segments.
	if !strings.Contains(output, "session") {
		t.Errorf("status-line should contain 'session' segment, got: %s", output)
	}
	if !strings.Contains(output, "cost") {
		t.Errorf("status-line should contain 'cost' segment, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// 8. Test command with no guards
// ---------------------------------------------------------------------------

func TestGuardTestNoGuards(t *testing.T) {
	tmpDir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))

	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	os.WriteFile(configPath, []byte("version: 1\nguards: []\n"), 0o644)

	output, err := executeCommand("test")
	if err != nil {
		t.Fatalf("test command failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(output, "No guard rules") {
		t.Errorf("test should report no guard rules, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// 9. Test command with guard rules
// ---------------------------------------------------------------------------

func TestGuardTestWithRules(t *testing.T) {
	tmpDir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))

	configYAML := `version: 1
guards:
  - match: "Bash"
    action: block
    when: 'tool_input.command contains "rm -rf"'
    reason: "Dangerous command blocked"
  - match: "Write"
    action: warn
    reason: "Write operations need review"
`
	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	os.WriteFile(configPath, []byte(configYAML), 0o644)

	output, err := executeCommand("test")
	if err != nil {
		t.Fatalf("test command failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(output, "PASS") {
		t.Errorf("test should report at least one PASS, got: %s", output)
	}
	if !strings.Contains(output, "Results:") {
		t.Errorf("test should show Results summary, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// 10. Diff requires two arguments
// ---------------------------------------------------------------------------

func TestDiffRequiresArgs(t *testing.T) {
	_, err := executeCommand("diff")
	if err == nil {
		t.Error("diff with no args should fail")
	}
}

// ---------------------------------------------------------------------------
// 11. Log with default limit
// ---------------------------------------------------------------------------

func TestLogDefaultLimit(t *testing.T) {
	// This test verifies the log command exists and accepts --limit.
	// We can't test actual Dolt output without a database, so we just
	// verify the flag is recognized.
	rootCmd := newRootCmd()
	logCmd, _, err := rootCmd.Find([]string{"log"})
	if err != nil {
		t.Fatalf("log command not found: %v", err)
	}

	limitFlag := logCmd.Flags().Lookup("limit")
	if limitFlag == nil {
		t.Fatal("log command should have --limit flag")
	}
	if limitFlag.DefValue != "10" {
		t.Errorf("default limit should be 10, got %s", limitFlag.DefValue)
	}
}

// ---------------------------------------------------------------------------
// 12. BuildTestPayload covers conditions
// ---------------------------------------------------------------------------

func TestBuildTestPayload(t *testing.T) {
	rule := core.GuardRuleConfig{
		Match:  "Bash",
		Action: "block",
		When:   `tool_input.command contains "rm -rf"`,
		Reason: "test",
	}

	payload := buildTestPayload(rule)

	// Payload should have tool_input.command that contains "rm -rf".
	ti, ok := payload["tool_input"].(map[string]interface{})
	if !ok {
		t.Fatal("payload should have tool_input map")
	}
	cmd, ok := ti["command"].(string)
	if !ok {
		t.Fatal("payload should have tool_input.command string")
	}
	if !strings.Contains(cmd, "rm -rf") {
		t.Errorf("payload command should contain 'rm -rf', got: %s", cmd)
	}
}

// ---------------------------------------------------------------------------
// 13. Render builtin segments
// ---------------------------------------------------------------------------

func TestRenderBuiltinSegments(t *testing.T) {
	names := []string{"session", "cost", "project", "calendar", "pulse", "weather"}
	for _, name := range names {
		result := renderBuiltinSegment(name)
		if result == "" {
			t.Errorf("builtin segment %q rendered empty", name)
		}
		// After stripping ANSI codes, segment name should appear.
		stripped := stripANSI(result)
		if !strings.Contains(stripped, name) && name != "cost" {
			t.Errorf("builtin segment %q should contain its name, got: %s", name, stripped)
		}
	}

	// Unknown segment should still render something.
	unknown := renderBuiltinSegment("unknown_segment")
	if unknown == "" {
		t.Error("unknown builtin segment should still render")
	}
}

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	result := strings.Builder{}
	inEscape := false
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}
