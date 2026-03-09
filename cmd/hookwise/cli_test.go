package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vishnujayvel/hookwise/internal/analytics"
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

	// Should produce output (either segment data or fallback "hookwise" label).
	if strings.TrimSpace(output) == "" {
		t.Errorf("status-line should produce output, got empty")
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
	emptyCache := map[string]interface{}{}

	// Segments with no data should return empty (omitted from output).
	noDataSegments := []string{"session", "cost", "project", "calendar", "weather"}
	for _, name := range noDataSegments {
		result := renderBuiltinSegment(name, emptyCache, nil)
		if result != "" {
			t.Errorf("builtin segment %q with no data should return empty, got: %s", name, stripANSI(result))
		}
	}

	// Unknown segment should still render something (fallback).
	unknown := renderBuiltinSegment("unknown_segment", emptyCache, nil)
	if unknown == "" {
		t.Error("unknown builtin segment should still render")
	}
}

// ---------------------------------------------------------------------------
// 14. Status-line with feed cache renders real data
// ---------------------------------------------------------------------------

func TestStatusLineWithFeedCache(t *testing.T) {
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

	// Create the feed cache directory (same as filepath.Dir(core.DefaultCachePath)).
	cacheDir := filepath.Join(stateDir, "state")
	os.MkdirAll(cacheDir, 0o700)

	// Write weather feed cache with real data.
	weatherEnvelope := map[string]interface{}{
		"type":      "weather",
		"timestamp": "2026-03-07T10:00:00Z",
		"data": map[string]interface{}{
			"temperature":     72.0,
			"temperatureUnit": "fahrenheit",
			"windSpeed":       5.3,
			"weatherCode":     0,
			"emoji":           "\u2600\ufe0f",
			"description":     "Clear",
		},
	}
	writeJSONFile(t, filepath.Join(cacheDir, "weather.json"), weatherEnvelope)

	// Write project feed cache with real data.
	projectEnvelope := map[string]interface{}{
		"type":      "project",
		"timestamp": "2026-03-07T10:00:00Z",
		"data": map[string]interface{}{
			"name":   "hookwise",
			"branch": "main",
		},
	}
	writeJSONFile(t, filepath.Join(cacheDir, "project.json"), projectEnvelope)

	configYAML := `version: 1
status_line:
  enabled: true
  segments:
    - builtin: weather
    - builtin: project
`
	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	os.WriteFile(configPath, []byte(configYAML), 0o644)

	output, err := executeCommand("status-line")
	if err != nil {
		t.Fatalf("status-line failed: %v\noutput: %s", err, output)
	}

	stripped := stripANSI(output)

	// Weather should show temperature and description from cache.
	if !strings.Contains(stripped, "72") {
		t.Errorf("weather segment should show temperature 72, got: %s", stripped)
	}
	if !strings.Contains(stripped, "Clear") {
		t.Errorf("weather segment should show description 'Clear', got: %s", stripped)
	}

	// Project should show name and branch from cache.
	if !strings.Contains(stripped, "hookwise") {
		t.Errorf("project segment should show project name, got: %s", stripped)
	}
	if !strings.Contains(stripped, "(main)") {
		t.Errorf("project segment should show branch, got: %s", stripped)
	}
}

// ---------------------------------------------------------------------------
// 15. Status-line with missing cache renders fallbacks
// ---------------------------------------------------------------------------

func TestStatusLineWithMissingCache(t *testing.T) {
	tmpDir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	// Point state dir to a nonexistent path so no cache is found.
	stateDir := filepath.Join(tmpDir, ".hookwise-no-cache")
	t.Setenv("HOOKWISE_STATE_DIR", stateDir)

	configYAML := `version: 1
status_line:
  enabled: true
  segments:
    - builtin: weather
    - builtin: project
    - builtin: calendar
`
	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	os.WriteFile(configPath, []byte(configYAML), 0o644)

	output, err := executeCommand("status-line")
	if err != nil {
		t.Fatalf("status-line failed: %v\noutput: %s", err, output)
	}

	stripped := stripANSI(output)

	// With no cache, all segments return empty and the fallback "hookwise" label shows.
	if !strings.Contains(stripped, "hookwise") {
		t.Errorf("status-line with no cache should show fallback 'hookwise', got: %s", stripped)
	}
	// No segment should show "--" anymore — they should be omitted entirely.
	if strings.Contains(stripped, ": --") {
		t.Errorf("status-line should not contain '--' placeholders, got: %s", stripped)
	}
}

// ---------------------------------------------------------------------------
// 16. Weather segment renders from cache
// ---------------------------------------------------------------------------

func TestStatusLineWeatherSegment(t *testing.T) {
	feedCache := map[string]interface{}{
		"weather": map[string]interface{}{
			"type":      "weather",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"temperature":     55.0,
				"temperatureUnit": "fahrenheit",
				"emoji":           "\U0001f327\ufe0f",
				"description":     "Rain",
			},
		},
	}

	result := renderBuiltinSegment("weather", feedCache, nil)
	stripped := stripANSI(result)

	if !strings.Contains(stripped, "55") {
		t.Errorf("weather should show temperature 55, got: %s", stripped)
	}
	if !strings.Contains(stripped, "F") {
		t.Errorf("weather should show unit F, got: %s", stripped)
	}
	if !strings.Contains(stripped, "Rain") {
		t.Errorf("weather should show description Rain, got: %s", stripped)
	}
}

// ---------------------------------------------------------------------------
// 16b. Weather segment with real zero temperature (not placeholder)
// ---------------------------------------------------------------------------

func TestStatusLineWeatherZeroTemp(t *testing.T) {
	feedCache := map[string]interface{}{
		"weather": map[string]interface{}{
			"type":      "weather",
			"timestamp": "2026-01-15T08:00:00Z",
			"data": map[string]interface{}{
				"temperature":     0.0,
				"temperatureUnit": "fahrenheit",
				"emoji":           "\u2744\ufe0f",
				"description":     "Snow",
			},
		},
	}

	result := renderBuiltinSegment("weather", feedCache, nil)
	stripped := stripANSI(result)

	// 0°F is a real temperature — must render "0", not "--"
	if strings.Contains(stripped, "--") {
		t.Errorf("real zero temperature should NOT show '--', got: %s", stripped)
	}
	if !strings.Contains(stripped, "0") {
		t.Errorf("weather should show temperature 0, got: %s", stripped)
	}
	if !strings.Contains(stripped, "F") {
		t.Errorf("weather should show unit F, got: %s", stripped)
	}
}

// ---------------------------------------------------------------------------
// 16c. Weather segment with celsius unit
// ---------------------------------------------------------------------------

func TestStatusLineWeatherCelsius(t *testing.T) {
	feedCache := map[string]interface{}{
		"weather": map[string]interface{}{
			"type":      "weather",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"temperature":     22.0,
				"temperatureUnit": "celsius",
				"emoji":           "\u2600\ufe0f",
				"description":     "Clear",
			},
		},
	}

	result := renderBuiltinSegment("weather", feedCache, nil)
	stripped := stripANSI(result)

	if !strings.Contains(stripped, "22") {
		t.Errorf("weather should show temperature 22, got: %s", stripped)
	}
	if !strings.Contains(stripped, "C") {
		t.Errorf("weather should show unit C for celsius, got: %s", stripped)
	}
	if strings.Contains(stripped, "F") {
		t.Errorf("celsius weather should NOT show F, got: %s", stripped)
	}
}

// ---------------------------------------------------------------------------
// 17. Project segment renders from cache
// ---------------------------------------------------------------------------

func TestStatusLineProjectSegment(t *testing.T) {
	feedCache := map[string]interface{}{
		"project": map[string]interface{}{
			"type":      "project",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"name":   "myapp",
				"branch": "feature/xyz",
			},
		},
	}

	result := renderBuiltinSegment("project", feedCache, nil)
	stripped := stripANSI(result)

	if !strings.Contains(stripped, "myapp") {
		t.Errorf("project should show name myapp, got: %s", stripped)
	}
	if !strings.Contains(stripped, "(feature/xyz)") {
		t.Errorf("project should show branch, got: %s", stripped)
	}
}

// ---------------------------------------------------------------------------
// 17b. Calendar segment renders from cache
// ---------------------------------------------------------------------------

func TestStatusLineCalendarSegment(t *testing.T) {
	// Use absolute start time — renderer computes relative time dynamically.
	eventStart := time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)
	feedCache := map[string]interface{}{
		"calendar": map[string]interface{}{
			"type":      "calendar",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"events": []interface{}{
					map[string]interface{}{
						"name":       "Standup",
						"start":      eventStart,
						"end":        "2026-03-07T11:00:00Z",
						"all_day":    false,
						"is_current": false,
					},
				},
				"next_event": map[string]interface{}{
					"name":  "Standup",
					"start": eventStart,
				},
			},
		},
	}

	result := renderBuiltinSegment("calendar", feedCache, nil)
	stripped := stripANSI(result)

	if !strings.Contains(stripped, "\U0001f4c5") {
		t.Errorf("calendar should show 📅 emoji, got: %s", stripped)
	}
	if !strings.Contains(stripped, "Standup") {
		t.Errorf("calendar should show event name 'Standup', got: %s", stripped)
	}
	if !strings.Contains(stripped, "in ") {
		t.Errorf("calendar should show dynamic relative time, got: %s", stripped)
	}
}

func TestStatusLineCalendarSegmentEmpty(t *testing.T) {
	// Calendar with no next_event should return empty.
	feedCache := map[string]interface{}{
		"calendar": map[string]interface{}{
			"type":      "calendar",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"events":     []interface{}{},
				"next_event": nil,
			},
		},
	}

	result := renderBuiltinSegment("calendar", feedCache, nil)
	if result != "" {
		t.Errorf("calendar with nil next_event should return empty, got: %s", stripANSI(result))
	}
}

func TestStatusLineCalendarSegmentStringEvent(t *testing.T) {
	// next_event as plain string (alternate format).
	feedCache := map[string]interface{}{
		"calendar": map[string]interface{}{
			"type":      "calendar",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"next_event": "Sprint Review in 1h",
			},
		},
	}

	result := renderBuiltinSegment("calendar", feedCache, nil)
	stripped := stripANSI(result)
	if !strings.Contains(stripped, "Sprint Review in 1h") {
		t.Errorf("calendar should render string event, got: %s", stripped)
	}
}

// ---------------------------------------------------------------------------
// 18. Placeholder fallback renders "--" not "0"
// ---------------------------------------------------------------------------

func TestStatusLinePlaceholderFallback(t *testing.T) {
	// Feed with source: "placeholder" should be treated as no data.
	feedCache := map[string]interface{}{
		"weather": map[string]interface{}{
			"type":      "weather",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"temperature":     0.0,
				"temperatureUnit": "fahrenheit",
				"emoji":           "",
				"description":     "",
				"source":          "placeholder",
			},
		},
		"project": map[string]interface{}{
			"type":      "project",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"name":   "unknown (placeholder)",
				"branch": "main",
				"source": "placeholder",
			},
		},
		"calendar": map[string]interface{}{
			"type":      "calendar",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"next_event": nil,
				"source":     "placeholder",
			},
		},
	}

	for _, name := range []string{"weather", "project", "calendar"} {
		result := renderBuiltinSegment(name, feedCache, nil)
		// Placeholder feeds should produce empty output (segment omitted).
		if result != "" {
			t.Errorf("placeholder %q should return empty, got: %s", name, stripANSI(result))
		}
	}
}

// ---------------------------------------------------------------------------
// 21. Session segment with daily summary
// ---------------------------------------------------------------------------

func TestStatusLineSessionSegment(t *testing.T) {
	summary := &analytics.DailySummaryResult{TotalSessions: 3}
	result := renderBuiltinSegment("session", nil, summary)
	stripped := stripANSI(result)
	if !strings.Contains(stripped, "session: 3") {
		t.Errorf("session segment should show count, got: %s", stripped)
	}
}

func TestStatusLineSessionSegmentNoSummary(t *testing.T) {
	result := renderBuiltinSegment("session", nil, nil)
	if result != "" {
		t.Errorf("session segment without summary should return empty, got: %s", stripANSI(result))
	}
}

// ---------------------------------------------------------------------------
// 22. Cost segment with daily summary
// ---------------------------------------------------------------------------

func TestStatusLineCostSegment(t *testing.T) {
	summary := &analytics.DailySummaryResult{EstimatedCostUSD: 1.50}
	result := renderBuiltinSegment("cost", nil, summary)
	stripped := stripANSI(result)
	if !strings.Contains(stripped, "cost: $1.50") {
		t.Errorf("cost segment should show dollar amount, got: %s", stripped)
	}
}

func TestStatusLineCostSegmentZero(t *testing.T) {
	summary := &analytics.DailySummaryResult{EstimatedCostUSD: 0}
	result := renderBuiltinSegment("cost", nil, summary)
	if result != "" {
		t.Errorf("cost segment with zero should return empty, got: %s", stripANSI(result))
	}
}

// writeJSONFile marshals v to JSON and writes it to path.
func writeJSONFile(t *testing.T, path string, v interface{}) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal JSON: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

// ---------------------------------------------------------------------------
// 23. recordAnalytics writes session data to Dolt
// ---------------------------------------------------------------------------

func TestRecordAnalytics_SessionStart(t *testing.T) {
	tmpDir := t.TempDir()

	payload := core.HookPayload{SessionID: "test-session-001"}
	recordAnalytics(context.Background(), core.EventSessionStart, payload, tmpDir)

	// Verify session was recorded.
	db, err := analytics.Open(tmpDir)
	if err != nil {
		t.Fatalf("failed to open analytics DB: %v", err)
	}
	defer db.Close()

	a := analytics.NewAnalytics(db)
	today := time.Now().UTC().Format("2006-01-02")
	summary, err := a.DailySummary(context.Background(), today)
	if err != nil {
		t.Fatalf("daily summary failed: %v", err)
	}
	if summary.TotalSessions < 1 {
		t.Errorf("expected at least 1 session after recordAnalytics, got %d", summary.TotalSessions)
	}
}

func TestRecordAnalytics_PostToolUse(t *testing.T) {
	tmpDir := t.TempDir()

	// First create a session.
	payload := core.HookPayload{SessionID: "test-session-002"}
	recordAnalytics(context.Background(), core.EventSessionStart, payload, tmpDir)

	// Record a tool use event.
	payload.ToolName = "Bash"
	recordAnalytics(context.Background(), core.EventPostToolUse, payload, tmpDir)

	db, err := analytics.Open(tmpDir)
	if err != nil {
		t.Fatalf("failed to open analytics DB: %v", err)
	}
	defer db.Close()

	a := analytics.NewAnalytics(db)
	today := time.Now().UTC().Format("2006-01-02")
	summary, err := a.DailySummary(context.Background(), today)
	if err != nil {
		t.Fatalf("daily summary failed: %v", err)
	}
	if summary.TotalEvents < 1 {
		t.Errorf("expected at least 1 event after PostToolUse, got %d", summary.TotalEvents)
	}
}

// ---------------------------------------------------------------------------
// 24. Insights segment renders from cache
// ---------------------------------------------------------------------------

func TestStatusLineInsightsSegment(t *testing.T) {
	feedCache := map[string]interface{}{
		"insights": map[string]interface{}{
			"type":      "insights",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"total_sessions":       72,
				"total_messages":       486,
				"total_lines_added":    12800,
				"avg_duration_minutes": 225.0,
				"top_tools": []interface{}{
					map[string]interface{}{"name": "Bash", "count": 509},
					map[string]interface{}{"name": "Read", "count": 323},
					map[string]interface{}{"name": "Edit", "count": 228},
				},
				"friction_counts":    map[string]interface{}{"wrong_approach": 32, "misunderstood_request": 14},
				"friction_total":     46,
				"peak_hour":          7,
				"days_active":        17,
				"staleness_days":     30,
				"recent_msgs_per_day": 55,
				"recent_messages":     110,
				"recent_days_active":  2,
				"recent_session": map[string]interface{}{
					"id":               "s1",
					"duration_minutes": 45.0,
					"lines_added":      200,
					"friction_count":   3,
					"outcome":          "success",
					"tool_errors":      0,
				},
			},
		},
	}

	// Test insights segment.
	result := renderBuiltinSegment("insights", feedCache, nil)
	stripped := stripANSI(result)
	if !strings.Contains(stripped, "72 sessions") {
		t.Errorf("insights should show 72 sessions, got: %s", stripped)
	}
	if !strings.Contains(stripped, "17d") {
		t.Errorf("insights should show 17d, got: %s", stripped)
	}
	if !strings.Contains(stripped, "12.8k") {
		t.Errorf("insights should show 12.8k lines, got: %s", stripped)
	}
}

func TestStatusLineInsightsFrictionSegment(t *testing.T) {
	// Friction with recent session friction > 0.
	feedCache := map[string]interface{}{
		"insights": map[string]interface{}{
			"type":      "insights",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"total_sessions":  5,
				"friction_total":  5,
				"friction_counts": map[string]interface{}{"wrong_approach": 3, "misunderstood_request": 2},
				"recent_session": map[string]interface{}{
					"friction_count": 3,
				},
			},
		},
	}

	result := renderBuiltinSegment("insights_friction", feedCache, nil)
	stripped := stripANSI(result)
	if !strings.Contains(stripped, "3 friction") {
		t.Errorf("friction should show 3, got: %s", stripped)
	}
	if !strings.Contains(stripped, "wrong approach") {
		t.Errorf("friction should show top category, got: %s", stripped)
	}
	if !strings.Contains(stripped, "break tasks into steps") {
		t.Errorf("friction should show tip, got: %s", stripped)
	}
}

func TestStatusLineInsightsFrictionClean(t *testing.T) {
	// Clean session (zero recent, non-zero total).
	feedCache := map[string]interface{}{
		"insights": map[string]interface{}{
			"type":      "insights",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"total_sessions":  10,
				"friction_total":  12,
				"friction_counts": map[string]interface{}{},
				"recent_session": map[string]interface{}{
					"friction_count": 0,
				},
			},
		},
	}

	result := renderBuiltinSegment("insights_friction", feedCache, nil)
	stripped := stripANSI(result)
	if !strings.Contains(stripped, "Clean session") {
		t.Errorf("should show clean session, got: %s", stripped)
	}
	if !strings.Contains(stripped, "12 in 30d") {
		t.Errorf("should show historical friction count, got: %s", stripped)
	}
}

func TestStatusLineInsightsPaceSegment(t *testing.T) {
	feedCache := map[string]interface{}{
		"insights": map[string]interface{}{
			"type":      "insights",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"total_messages":    470,
				"total_lines_added": 5400,
				"total_sessions":    42,
				"days_active":       10,
			},
		},
	}

	result := renderBuiltinSegment("insights_pace", feedCache, nil)
	stripped := stripANSI(result)
	if !strings.Contains(stripped, "47 msgs/day") {
		t.Errorf("pace should show 47 msgs/day, got: %s", stripped)
	}
	if !strings.Contains(stripped, "5.4k") {
		t.Errorf("pace should show 5.4k lines, got: %s", stripped)
	}
	if !strings.Contains(stripped, "42 sessions") {
		t.Errorf("pace should show 42 sessions, got: %s", stripped)
	}
}

func TestStatusLineInsightsTrendSegment(t *testing.T) {
	feedCache := map[string]interface{}{
		"insights": map[string]interface{}{
			"type":      "insights",
			"timestamp": "2026-03-07T10:00:00Z",
			"data": map[string]interface{}{
				"top_tools": []interface{}{
					map[string]interface{}{"name": "Bash", "count": 50},
					map[string]interface{}{"name": "Read", "count": 30},
					map[string]interface{}{"name": "Edit", "count": 20},
				},
				"peak_hour": 14,
			},
		},
	}

	result := renderBuiltinSegment("insights_trend", feedCache, nil)
	stripped := stripANSI(result)
	if !strings.Contains(stripped, "Top: Bash, Read") {
		t.Errorf("trend should show top 2 tools, got: %s", stripped)
	}
	if !strings.Contains(stripped, "Peak: afternoon") {
		t.Errorf("trend should show peak afternoon, got: %s", stripped)
	}
}

func TestStatusLineInsightsTrendPeakHour(t *testing.T) {
	tests := []struct {
		hour  int
		label string
	}{
		{8, "morning"},
		{14, "afternoon"},
		{20, "evening"},
		{3, "night"},
	}
	for _, tt := range tests {
		feedCache := map[string]interface{}{
			"insights": map[string]interface{}{
				"type":      "insights",
				"timestamp": "2026-03-07T10:00:00Z",
				"data": map[string]interface{}{
					"top_tools": []interface{}{
						map[string]interface{}{"name": "Read", "count": 10},
					},
					"peak_hour": tt.hour,
				},
			},
		}
		result := renderBuiltinSegment("insights_trend", feedCache, nil)
		stripped := stripANSI(result)
		if !strings.Contains(stripped, "Peak: "+tt.label) {
			t.Errorf("hour %d should map to %s, got: %s", tt.hour, tt.label, stripped)
		}
	}
}

func TestStatusLineInsightsNoData(t *testing.T) {
	// Insights segments with no cache data should handle gracefully.
	emptyCache := map[string]interface{}{}

	result := renderBuiltinSegment("insights", emptyCache, nil)
	if result != "" {
		t.Errorf("insights with no data should return empty, got: %s", stripANSI(result))
	}

	// Friction/pace/trend also return empty string with no data.
	assert := func(name string) {
		r := renderBuiltinSegment(name, emptyCache, nil)
		if r != "" {
			t.Errorf("%s with no data should return empty, got: %s", name, r)
		}
	}
	assert("insights_friction")
	assert("insights_pace")
	assert("insights_trend")
}

// ---------------------------------------------------------------------------
// 25. Multi-line status output with insights
// ---------------------------------------------------------------------------

func TestStatusLineMultiLine(t *testing.T) {
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

	// Create insights cache with real data.
	cacheDir := filepath.Join(stateDir, "state")
	os.MkdirAll(cacheDir, 0o700)

	insightsEnvelope := map[string]interface{}{
		"type":      "insights",
		"timestamp": "2026-03-08T10:00:00Z",
		"data": map[string]interface{}{
			"total_sessions":       72,
			"total_messages":       486,
			"total_lines_added":    12800,
			"avg_duration_minutes": 225.0,
			"top_tools": []interface{}{
				map[string]interface{}{"name": "Bash", "count": 509},
				map[string]interface{}{"name": "Read", "count": 323},
			},
			"friction_counts":    map[string]interface{}{"wrong_approach": 5},
			"friction_total":     5,
			"peak_hour":          14,
			"days_active":        17,
			"staleness_days":     30,
			"recent_msgs_per_day": 55,
			"recent_messages":     110,
			"recent_days_active":  2,
			"recent_session": map[string]interface{}{
				"friction_count": 0,
			},
		},
	}
	writeJSONFile(t, filepath.Join(cacheDir, "insights.json"), insightsEnvelope)

	configYAML := `version: 1
status_line:
  enabled: true
  segments:
    - session
    - cost
`
	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	os.WriteFile(configPath, []byte(configYAML), 0o644)

	output, err := executeCommand("status-line")
	if err != nil {
		t.Fatalf("status-line failed: %v\noutput: %s", err, output)
	}

	stripped := stripANSI(output)
	lines := strings.Split(strings.TrimSpace(stripped), "\n")

	// Should have at least 2 lines: main segment line + insights summary lines.
	if len(lines) < 2 {
		t.Errorf("expected at least 2 lines for multi-line output, got %d:\n%s", len(lines), stripped)
	}

	// Check for insights summary content in lines 2+.
	if !strings.Contains(stripped, "72 sessions") {
		t.Errorf("multi-line output should contain '72 sessions', got:\n%s", stripped)
	}
	if !strings.Contains(stripped, "Bash(509)") {
		t.Errorf("multi-line output should contain tool breakdown, got:\n%s", stripped)
	}
}

// ---------------------------------------------------------------------------
// 26. formatLargeNumber helper
// ---------------------------------------------------------------------------

func TestFormatLargeNumber(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{340, "340"},
		{999, "999"},
		{1000, "1k"},
		{5400, "5.4k"},
		{12800, "12.8k"},
		{28000, "28k"},
	}
	for _, tt := range tests {
		result := formatLargeNumber(tt.input)
		if result != tt.expected {
			t.Errorf("formatLargeNumber(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// 27. Doctor feed health: placeholder feeds
// ---------------------------------------------------------------------------

func TestDoctorFeedHealthPlaceholder(t *testing.T) {
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
	cacheDir := filepath.Join(stateDir, "state")
	os.MkdirAll(cacheDir, 0o700)

	// Create a valid config.
	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	os.WriteFile(configPath, []byte("version: 1\nguards: []\n"), 0o644)

	// Write a placeholder feed.
	writeJSONFile(t, filepath.Join(cacheDir, "practice.json"), map[string]interface{}{
		"type":      "practice",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"source":         "placeholder",
			"total_sessions": 0,
		},
	})

	// Write a healthy feed.
	writeJSONFile(t, filepath.Join(cacheDir, "weather.json"), map[string]interface{}{
		"type":      "weather",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"temperature": 72.0,
			"emoji":       "☀️",
		},
	})

	output, err := executeCommand("doctor")
	if err != nil {
		t.Fatalf("doctor failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(output, "WARN  feed:practice: placeholder") {
		t.Errorf("doctor should warn about placeholder practice feed, got:\n%s", output)
	}
	if !strings.Contains(output, "INFO  feed:weather: OK") {
		t.Errorf("doctor should show OK for weather feed, got:\n%s", output)
	}
	if !strings.Contains(output, "warning(s)") {
		t.Errorf("doctor should show warning count in summary, got:\n%s", output)
	}
	// Placeholders are non-blocking — should still pass.
	if !strings.Contains(output, "All critical checks passed") {
		t.Errorf("doctor should still pass with placeholder warnings, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// 28. Doctor feed health: stale feeds
// ---------------------------------------------------------------------------

func TestDoctorFeedHealthStale(t *testing.T) {
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
	cacheDir := filepath.Join(stateDir, "state")
	os.MkdirAll(cacheDir, 0o700)

	// Config with weather interval = 60 seconds.
	configYAML := `version: 1
feeds:
  weather:
    enabled: true
    interval_seconds: 60
`
	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	os.WriteFile(configPath, []byte(configYAML), 0o644)

	// Write a stale weather feed (timestamp 5 minutes ago, interval is 60s, threshold is 120s).
	staleTime := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	writeJSONFile(t, filepath.Join(cacheDir, "weather.json"), map[string]interface{}{
		"type":      "weather",
		"timestamp": staleTime,
		"data": map[string]interface{}{
			"temperature": 72.0,
			"emoji":       "☀️",
		},
	})

	output, err := executeCommand("doctor")
	if err != nil {
		t.Fatalf("doctor failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(output, "WARN  feed:weather: stale data") {
		t.Errorf("doctor should warn about stale weather feed, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// 29. Doctor feed health: segment coverage
// ---------------------------------------------------------------------------

func TestDoctorFeedHealthSegmentCoverage(t *testing.T) {
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
	cacheDir := filepath.Join(stateDir, "state")
	os.MkdirAll(cacheDir, 0o700)

	// Config with 3 feed-backed segments, only 1 has real data.
	configYAML := `version: 1
status_line:
  enabled: true
  segments:
    - session
    - weather
    - project
    - calendar
`
	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	os.WriteFile(configPath, []byte(configYAML), 0o644)

	// Only weather has real data.
	writeJSONFile(t, filepath.Join(cacheDir, "weather.json"), map[string]interface{}{
		"type":      "weather",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      map[string]interface{}{"temperature": 72.0},
	})
	// Project has placeholder.
	writeJSONFile(t, filepath.Join(cacheDir, "project.json"), map[string]interface{}{
		"type":      "project",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      map[string]interface{}{"source": "placeholder", "name": "unknown"},
	})
	// Calendar missing entirely.

	output, err := executeCommand("doctor")
	if err != nil {
		t.Fatalf("doctor failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(output, "WARN  status-line: 1/3 feed-backed segments have real data") {
		t.Errorf("doctor should report segment coverage, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// 30. Doctor feed health: no state files (graceful)
// ---------------------------------------------------------------------------

func TestDoctorFeedHealthNoStateFiles(t *testing.T) {
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
	// state/ subdir does NOT exist — no feed files.

	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	os.WriteFile(configPath, []byte("version: 1\nguards: []\n"), 0o644)

	output, err := executeCommand("doctor")
	if err != nil {
		t.Fatalf("doctor failed: %v\noutput: %s", err, output)
	}

	// Should not crash, should not show feed warnings.
	if strings.Contains(output, "feed:") {
		t.Errorf("doctor with no state files should not show feed checks, got:\n%s", output)
	}
	if !strings.Contains(output, "All critical checks passed.") {
		t.Errorf("doctor should pass cleanly with no state files, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// 31. Doctor feed health: all segments healthy
// ---------------------------------------------------------------------------

func TestDoctorFeedHealthAllHealthy(t *testing.T) {
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
	cacheDir := filepath.Join(stateDir, "state")
	os.MkdirAll(cacheDir, 0o700)

	configYAML := `version: 1
status_line:
  enabled: true
  segments:
    - weather
    - project
`
	configPath := filepath.Join(tmpDir, core.ProjectConfigFile)
	os.WriteFile(configPath, []byte(configYAML), 0o644)

	now := time.Now().UTC().Format(time.RFC3339)
	writeJSONFile(t, filepath.Join(cacheDir, "weather.json"), map[string]interface{}{
		"type": "weather", "timestamp": now,
		"data": map[string]interface{}{"temperature": 72.0},
	})
	writeJSONFile(t, filepath.Join(cacheDir, "project.json"), map[string]interface{}{
		"type": "project", "timestamp": now,
		"data": map[string]interface{}{"name": "myapp", "branch": "main"},
	})

	output, err := executeCommand("doctor")
	if err != nil {
		t.Fatalf("doctor failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(output, "INFO  status-line: all 2 feed-backed segments have real data") {
		t.Errorf("doctor should report all segments healthy, got:\n%s", output)
	}
	// No warnings — summary should NOT have "warning(s)".
	if strings.Contains(output, "warning(s)") {
		t.Errorf("doctor with all healthy feeds should not show warnings, got:\n%s", output)
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
