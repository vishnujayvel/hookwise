package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// =============================================================================
// Unit Tests: YAML Parsing
// =============================================================================

func TestParseYAML_ValidBasic(t *testing.T) {
	data := []byte("version: 1\nanalytics:\n  enabled: true\n")
	raw, err := parseYAML(data, "test.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw["version"] != 1 {
		t.Errorf("expected version=1, got %v", raw["version"])
	}
	analytics, ok := raw["analytics"].(map[string]interface{})
	if !ok {
		t.Fatal("expected analytics to be a map")
	}
	if analytics["enabled"] != true {
		t.Errorf("expected analytics.enabled=true, got %v", analytics["enabled"])
	}
}

func TestParseYAML_EmptyDocument(t *testing.T) {
	data := []byte("")
	raw, err := parseYAML(data, "empty.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(raw) != 0 {
		t.Errorf("expected empty map, got %v", raw)
	}
}

func TestParseYAML_NullDocument(t *testing.T) {
	data := []byte("---\n")
	raw, err := parseYAML(data, "null.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(raw) != 0 {
		t.Errorf("expected empty map for null document, got %v", raw)
	}
}

func TestParseYAML_InvalidSyntax(t *testing.T) {
	data := []byte("version: [\n  invalid yaml")
	_, err := parseYAML(data, "invalid.yaml")
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if _, ok := err.(*ConfigError); !ok {
		t.Errorf("expected ConfigError, got %T", err)
	}
}

func TestParseYAML_SnakeCaseKeys(t *testing.T) {
	data := []byte(`
status_line:
  enabled: true
  cache_path: "/tmp/cache.json"
cost_tracking:
  daily_budget: 25.0
`)
	raw, err := parseYAML(data, "snake.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := raw["status_line"]; !ok {
		t.Error("expected status_line key (snake_case)")
	}
	if _, ok := raw["cost_tracking"]; !ok {
		t.Error("expected cost_tracking key (snake_case)")
	}
}

func TestParseYAML_GuardsArray(t *testing.T) {
	data := []byte(`
guards:
  - match: "Bash"
    action: block
    reason: "blocked"
  - match: "Write"
    action: warn
    reason: "warned"
`)
	raw, err := parseYAML(data, "guards.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	guards, ok := raw["guards"].([]interface{})
	if !ok {
		t.Fatal("expected guards to be a slice")
	}
	if len(guards) != 2 {
		t.Fatalf("expected 2 guards, got %d", len(guards))
	}
	g0 := guards[0].(map[string]interface{})
	if g0["match"] != "Bash" {
		t.Errorf("expected first guard match=Bash, got %v", g0["match"])
	}
}

func TestParseYAML_NestedCoaching(t *testing.T) {
	data := []byte(`
coaching:
  metacognition:
    enabled: true
    interval_seconds: 600
  builder_trap:
    enabled: true
    thresholds:
      yellow: 20
      orange: 40
      red: 80
`)
	raw, err := parseYAML(data, "coaching.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	coaching := raw["coaching"].(map[string]interface{})
	metacog := coaching["metacognition"].(map[string]interface{})
	if metacog["interval_seconds"] != 600 {
		t.Errorf("expected interval_seconds=600, got %v", metacog["interval_seconds"])
	}
}

// =============================================================================
// Unit Tests: Deep Merge
// =============================================================================

func TestDeepMerge_SourceOverridesTarget(t *testing.T) {
	target := map[string]interface{}{"version": 1, "analytics": map[string]interface{}{"enabled": false}}
	source := map[string]interface{}{"analytics": map[string]interface{}{"enabled": true}}
	result := DeepMerge(target, source)

	analytics := result["analytics"].(map[string]interface{})
	if analytics["enabled"] != true {
		t.Errorf("expected source to override target: got enabled=%v", analytics["enabled"])
	}
	if result["version"] != 1 {
		t.Errorf("expected version from target to be preserved: got %v", result["version"])
	}
}

func TestDeepMerge_ArraysReplace(t *testing.T) {
	target := map[string]interface{}{
		"guards": []interface{}{
			map[string]interface{}{"match": "Bash", "action": "block"},
			map[string]interface{}{"match": "Write", "action": "warn"},
		},
	}
	source := map[string]interface{}{
		"guards": []interface{}{
			map[string]interface{}{"match": "Read", "action": "confirm"},
		},
	}
	result := DeepMerge(target, source)

	guards := result["guards"].([]interface{})
	if len(guards) != 1 {
		t.Fatalf("expected arrays to REPLACE (1 element), got %d elements", len(guards))
	}
	g0 := guards[0].(map[string]interface{})
	if g0["match"] != "Read" {
		t.Errorf("expected replacement guard match=Read, got %v", g0["match"])
	}
}

func TestDeepMerge_ArraysReplaceNotConcatenate(t *testing.T) {
	target := map[string]interface{}{
		"includes": []interface{}{"a.yaml", "b.yaml"},
	}
	source := map[string]interface{}{
		"includes": []interface{}{"c.yaml"},
	}
	result := DeepMerge(target, source)

	includes := result["includes"].([]interface{})
	if len(includes) != 1 || includes[0] != "c.yaml" {
		t.Errorf("expected arrays to replace, got %v", includes)
	}
}

func TestDeepMerge_NestedObjects(t *testing.T) {
	target := map[string]interface{}{
		"coaching": map[string]interface{}{
			"metacognition": map[string]interface{}{
				"enabled":          false,
				"interval_seconds": 300,
			},
			"builder_trap": map[string]interface{}{
				"enabled": false,
			},
		},
	}
	source := map[string]interface{}{
		"coaching": map[string]interface{}{
			"metacognition": map[string]interface{}{
				"enabled": true,
			},
		},
	}
	result := DeepMerge(target, source)

	coaching := result["coaching"].(map[string]interface{})
	metacog := coaching["metacognition"].(map[string]interface{})
	// Source overrides enabled
	if metacog["enabled"] != true {
		t.Error("expected enabled=true from source")
	}
	// Target's interval_seconds preserved
	if metacog["interval_seconds"] != 300 {
		t.Errorf("expected interval_seconds=300 preserved from target, got %v", metacog["interval_seconds"])
	}
	// builder_trap preserved from target
	bt := coaching["builder_trap"].(map[string]interface{})
	if bt["enabled"] != false {
		t.Error("expected builder_trap.enabled=false preserved from target")
	}
}

func TestDeepMerge_NewKeysAdded(t *testing.T) {
	target := map[string]interface{}{"version": 1}
	source := map[string]interface{}{"analytics": map[string]interface{}{"enabled": true}}
	result := DeepMerge(target, source)

	if _, ok := result["analytics"]; !ok {
		t.Error("expected new key 'analytics' from source to be added")
	}
}

func TestDeepMerge_EmptyTarget(t *testing.T) {
	target := map[string]interface{}{}
	source := map[string]interface{}{"version": 1, "analytics": map[string]interface{}{"enabled": true}}
	result := DeepMerge(target, source)

	if result["version"] != 1 {
		t.Errorf("expected version=1, got %v", result["version"])
	}
}

func TestDeepMerge_EmptySource(t *testing.T) {
	target := map[string]interface{}{"version": 1}
	source := map[string]interface{}{}
	result := DeepMerge(target, source)

	if result["version"] != 1 {
		t.Errorf("expected version=1 preserved, got %v", result["version"])
	}
}

func TestDeepMerge_ScalarOverridesMap(t *testing.T) {
	target := map[string]interface{}{
		"analytics": map[string]interface{}{"enabled": true},
	}
	source := map[string]interface{}{
		"analytics": "disabled",
	}
	result := DeepMerge(target, source)
	if result["analytics"] != "disabled" {
		t.Errorf("expected scalar to replace map, got %v", result["analytics"])
	}
}

func TestDeepMerge_NilSource(t *testing.T) {
	target := map[string]interface{}{"version": 1}
	source := map[string]interface{}{"version": nil}
	result := DeepMerge(target, source)
	if result["version"] != nil {
		t.Errorf("expected nil from source to override, got %v", result["version"])
	}
}

func TestDeepMerge_DoesNotMutateInputs(t *testing.T) {
	target := map[string]interface{}{"version": 1, "coaching": map[string]interface{}{"enabled": false}}
	source := map[string]interface{}{"coaching": map[string]interface{}{"enabled": true}}

	_ = DeepMerge(target, source)

	// Original target should not be mutated
	coaching := target["coaching"].(map[string]interface{})
	if coaching["enabled"] != false {
		t.Error("DeepMerge mutated the original target")
	}
}

// =============================================================================
// Unit Tests: Env Var Interpolation
// =============================================================================

func TestInterpolateEnvVars_DefinedVar(t *testing.T) {
	t.Setenv("HOOKWISE_TEST_VAR", "hello-world")
	result := InterpolateEnvVars("prefix-${HOOKWISE_TEST_VAR}-suffix")
	if result != "prefix-hello-world-suffix" {
		t.Errorf("expected substitution, got %v", result)
	}
}

func TestInterpolateEnvVars_UndefinedLeavesLiteral(t *testing.T) {
	os.Unsetenv("HOOKWISE_UNDEFINED_VAR_12345")
	result := InterpolateEnvVars("${HOOKWISE_UNDEFINED_VAR_12345}")
	if result != "${HOOKWISE_UNDEFINED_VAR_12345}" {
		t.Errorf("expected undefined var to stay literal, got %v", result)
	}
}

func TestInterpolateEnvVars_MultipleVars(t *testing.T) {
	t.Setenv("HOOKWISE_A", "alpha")
	t.Setenv("HOOKWISE_B", "beta")
	result := InterpolateEnvVars("${HOOKWISE_A} and ${HOOKWISE_B}")
	if result != "alpha and beta" {
		t.Errorf("expected both vars substituted, got %v", result)
	}
}

func TestInterpolateEnvVars_MixedDefinedUndefined(t *testing.T) {
	t.Setenv("HOOKWISE_DEFINED_X", "value")
	os.Unsetenv("HOOKWISE_NOEXIST_Y")
	result := InterpolateEnvVars("${HOOKWISE_DEFINED_X} and ${HOOKWISE_NOEXIST_Y}")
	if result != "value and ${HOOKWISE_NOEXIST_Y}" {
		t.Errorf("expected mixed result, got %v", result)
	}
}

func TestInterpolateEnvVars_InMap(t *testing.T) {
	t.Setenv("HOOKWISE_DB", "/tmp/db.sqlite")
	input := map[string]interface{}{
		"analytics": map[string]interface{}{
			"db_path": "${HOOKWISE_DB}",
		},
	}
	result := InterpolateEnvVars(input)
	m := result.(map[string]interface{})
	analytics := m["analytics"].(map[string]interface{})
	if analytics["db_path"] != "/tmp/db.sqlite" {
		t.Errorf("expected interpolation in nested map, got %v", analytics["db_path"])
	}
}

func TestInterpolateEnvVars_InSlice(t *testing.T) {
	t.Setenv("HOOKWISE_ITEM", "resolved")
	input := []interface{}{"${HOOKWISE_ITEM}", "literal"}
	result := InterpolateEnvVars(input)
	arr := result.([]interface{})
	if arr[0] != "resolved" {
		t.Errorf("expected interpolation in slice, got %v", arr[0])
	}
	if arr[1] != "literal" {
		t.Errorf("expected literal preserved, got %v", arr[1])
	}
}

func TestInterpolateEnvVars_NonStringPassthrough(t *testing.T) {
	if InterpolateEnvVars(42) != 42 {
		t.Error("expected int to pass through")
	}
	if InterpolateEnvVars(true) != true {
		t.Error("expected bool to pass through")
	}
	if InterpolateEnvVars(nil) != nil {
		t.Error("expected nil to pass through")
	}
}

func TestInterpolateEnvVars_EmptyStringVar(t *testing.T) {
	t.Setenv("HOOKWISE_EMPTY", "")
	result := InterpolateEnvVars("${HOOKWISE_EMPTY}")
	// A defined empty string should replace the pattern with empty string
	if result != "" {
		t.Errorf("expected empty string for defined-but-empty env var, got %q", result)
	}
}

func TestInterpolateEnvVars_NoPattern(t *testing.T) {
	result := InterpolateEnvVars("no interpolation needed")
	if result != "no interpolation needed" {
		t.Errorf("expected unchanged string, got %v", result)
	}
}

// =============================================================================
// Unit Tests: Validation
// =============================================================================

func TestValidateConfig_ValidMinimal(t *testing.T) {
	raw := map[string]interface{}{
		"version": 1,
	}
	result := ValidateConfig(raw)
	if !result.Valid {
		t.Errorf("expected valid config, got errors: %v", result.Errors)
	}
}

func TestValidateConfig_UnknownSection(t *testing.T) {
	raw := map[string]interface{}{
		"version":         1,
		"unknown_section": true,
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid config for unknown section")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "Unknown config section") && e.Path == "unknown_section" {
			found = true
			if e.Suggestion == "" {
				t.Error("expected suggestion for unknown section")
			}
		}
	}
	if !found {
		t.Error("expected error about unknown_section")
	}
}

func TestValidateConfig_InvalidVersion(t *testing.T) {
	raw := map[string]interface{}{
		"version": "not-a-number",
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid config for non-numeric version")
	}
}

func TestValidateConfig_NegativeVersion(t *testing.T) {
	raw := map[string]interface{}{
		"version": -1,
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid config for negative version")
	}
}

func TestValidateConfig_InvalidGuards(t *testing.T) {
	raw := map[string]interface{}{
		"guards": "not-an-array",
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid for non-array guards")
	}
}

func TestValidateConfig_GuardMissingFields(t *testing.T) {
	raw := map[string]interface{}{
		"guards": []interface{}{
			map[string]interface{}{},
		},
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid for guard missing match/action/reason")
	}
	if len(result.Errors) < 3 {
		t.Errorf("expected at least 3 errors for missing guard fields, got %d", len(result.Errors))
	}
}

func TestValidateConfig_ValidGuards(t *testing.T) {
	raw := map[string]interface{}{
		"guards": []interface{}{
			map[string]interface{}{
				"match":  "Bash",
				"action": "block",
				"reason": "safety",
			},
		},
	}
	result := ValidateConfig(raw)
	if !result.Valid {
		t.Errorf("expected valid guards, got errors: %v", result.Errors)
	}
}

func TestValidateConfig_InvalidHandler(t *testing.T) {
	raw := map[string]interface{}{
		"handlers": []interface{}{
			map[string]interface{}{
				"name": "test",
				"type": "invalid_type",
			},
		},
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid for bad handler type")
	}
}

func TestValidateConfig_InvalidLogLevel(t *testing.T) {
	raw := map[string]interface{}{
		"settings": map[string]interface{}{
			"log_level": "verbose",
		},
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid for bad log level")
	}
}

func TestValidateConfig_ValidLogLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		raw := map[string]interface{}{
			"settings": map[string]interface{}{
				"log_level": level,
			},
		}
		result := ValidateConfig(raw)
		if !result.Valid {
			t.Errorf("expected valid for log_level=%q, got errors: %v", level, result.Errors)
		}
	}
}

func TestValidateConfig_IncludesNotArray(t *testing.T) {
	raw := map[string]interface{}{
		"includes": "not-an-array",
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid for non-array includes")
	}
}

func TestValidateConfig_InvalidNewsSource(t *testing.T) {
	raw := map[string]interface{}{
		"feeds": map[string]interface{}{
			"news": map[string]interface{}{
				"source": "reddit",
			},
		},
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid for bad news source")
	}
}

func TestValidateConfig_InvalidTUILaunchMethod(t *testing.T) {
	raw := map[string]interface{}{
		"tui": map[string]interface{}{
			"launch_method": "split",
		},
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid for bad launch method")
	}
}

func TestValidateConfig_InvalidWeatherLatitude(t *testing.T) {
	raw := map[string]interface{}{
		"feeds": map[string]interface{}{
			"weather": map[string]interface{}{
				"latitude": 200.0,
			},
		},
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid for out-of-range latitude")
	}
}

func TestValidateConfig_InvalidWeatherTempUnit(t *testing.T) {
	raw := map[string]interface{}{
		"feeds": map[string]interface{}{
			"weather": map[string]interface{}{
				"temperature_unit": "kelvin",
			},
		},
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid for unsupported temperature unit")
	}
}

func TestValidateConfig_MultipleErrors(t *testing.T) {
	raw := map[string]interface{}{
		"unknown_a": true,
		"unknown_b": true,
		"version":   "bad",
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid")
	}
	// At least 3 errors: 2 unknown sections + 1 bad version
	if len(result.Errors) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(result.Errors), result.Errors)
	}
}

func TestValidateConfig_EmptyConfig(t *testing.T) {
	raw := map[string]interface{}{}
	result := ValidateConfig(raw)
	if !result.Valid {
		t.Errorf("expected valid for empty config, got errors: %v", result.Errors)
	}
}

func TestValidateConfig_InvalidDaemonTimeout(t *testing.T) {
	raw := map[string]interface{}{
		"daemon": map[string]interface{}{
			"inactivity_timeout_minutes": -5,
		},
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid for negative daemon timeout")
	}
}

func TestValidateConfig_InvalidCoachingInterval(t *testing.T) {
	raw := map[string]interface{}{
		"coaching": map[string]interface{}{
			"metacognition": map[string]interface{}{
				"interval_seconds": "not-a-number",
			},
		},
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid for non-numeric interval")
	}
}

func TestValidateConfig_GuardInvalidAction(t *testing.T) {
	raw := map[string]interface{}{
		"guards": []interface{}{
			map[string]interface{}{
				"match":  "Bash",
				"action": "destroy",
				"reason": "bad action",
			},
		},
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid for bad guard action")
	}
}

// =============================================================================
// Unit Tests: GetDefaultConfig
// =============================================================================

func TestGetDefaultConfig_Version(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.Version != 1 {
		t.Errorf("expected default version=1, got %d", cfg.Version)
	}
}

func TestGetDefaultConfig_EmptyGuards(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.Guards == nil || len(cfg.Guards) != 0 {
		t.Error("expected empty guards slice")
	}
}

func TestGetDefaultConfig_AnalyticsEnabled(t *testing.T) {
	cfg := GetDefaultConfig()
	if !cfg.Analytics.Enabled {
		t.Error("expected analytics.enabled=true by default")
	}
}

func TestGetDefaultConfig_SettingsLogLevel(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.Settings.LogLevel != "info" {
		t.Errorf("expected settings.logLevel=info, got %q", cfg.Settings.LogLevel)
	}
}

func TestGetDefaultConfig_HandlerTimeout(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.Settings.HandlerTimeoutSeconds != DefaultHandlerTimeout {
		t.Errorf("expected handler timeout=%d, got %d", DefaultHandlerTimeout, cfg.Settings.HandlerTimeoutSeconds)
	}
}

func TestGetDefaultConfig_StatusLineDelimiter(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.StatusLine.Delimiter != DefaultStatusDelimiter {
		t.Errorf("expected delimiter=%q, got %q", DefaultStatusDelimiter, cfg.StatusLine.Delimiter)
	}
}

func TestGetDefaultConfig_CoachingDefaults(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.Coaching.Metacognition.Enabled {
		t.Error("expected metacognition disabled by default")
	}
	if cfg.Coaching.Metacognition.IntervalSeconds != 300 {
		t.Errorf("expected metacognition interval=300, got %d", cfg.Coaching.Metacognition.IntervalSeconds)
	}
	if cfg.Coaching.BuilderTrap.Thresholds.Yellow != 30 {
		t.Errorf("expected builderTrap yellow=30, got %d", cfg.Coaching.BuilderTrap.Thresholds.Yellow)
	}
	if cfg.Coaching.Communication.Tone != "gentle" {
		t.Errorf("expected communication tone=gentle, got %q", cfg.Coaching.Communication.Tone)
	}
}

func TestGetDefaultConfig_FeedDefaults(t *testing.T) {
	cfg := GetDefaultConfig()
	if !cfg.Feeds.Pulse.Enabled {
		t.Error("expected pulse feed enabled by default")
	}
	if cfg.Feeds.Pulse.IntervalSeconds != 30 {
		t.Errorf("expected pulse interval=30, got %d", cfg.Feeds.Pulse.IntervalSeconds)
	}
	if !cfg.Feeds.Project.Enabled {
		t.Error("expected project feed enabled by default")
	}
	if cfg.Feeds.Calendar.Enabled {
		t.Error("expected calendar feed disabled by default")
	}
	if cfg.Feeds.Weather.TemperatureUnit != "fahrenheit" {
		t.Errorf("expected weather unit=fahrenheit, got %q", cfg.Feeds.Weather.TemperatureUnit)
	}
}

func TestGetDefaultConfig_CostTracking(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.CostTracking.Enabled {
		t.Error("expected cost tracking disabled by default")
	}
	if cfg.CostTracking.DailyBudget != 10 {
		t.Errorf("expected daily budget=10, got %f", cfg.CostTracking.DailyBudget)
	}
	if cfg.CostTracking.Enforcement != "warn" {
		t.Errorf("expected enforcement=warn, got %q", cfg.CostTracking.Enforcement)
	}
}

func TestGetDefaultConfig_TranscriptBackup(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.TranscriptBackup.Enabled {
		t.Error("expected transcript backup disabled by default")
	}
	if cfg.TranscriptBackup.MaxSizeMB != 100 {
		t.Errorf("expected max size=100, got %d", cfg.TranscriptBackup.MaxSizeMB)
	}
}

func TestGetDefaultConfig_Daemon(t *testing.T) {
	cfg := GetDefaultConfig()
	if !cfg.Daemon.AutoStart {
		t.Error("expected daemon auto_start=true by default")
	}
	if cfg.Daemon.InactivityTimeoutMinutes != 120 {
		t.Errorf("expected inactivity timeout=120, got %d", cfg.Daemon.InactivityTimeoutMinutes)
	}
}

func TestGetDefaultConfig_TUI(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.TUI.AutoLaunch {
		t.Error("expected tui auto_launch=false by default")
	}
	if cfg.TUI.LaunchMethod != "newWindow" {
		t.Errorf("expected tui launch_method=newWindow, got %q", cfg.TUI.LaunchMethod)
	}
}

// =============================================================================
// Unit Tests: Include Resolution
// =============================================================================

func TestResolveIncludes_NoIncludes(t *testing.T) {
	config := map[string]interface{}{"version": 1}
	result, err := ResolveIncludes(config, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["version"] != 1 {
		t.Error("expected config unchanged when no includes")
	}
}

func TestResolveIncludes_EmptyIncludes(t *testing.T) {
	config := map[string]interface{}{
		"version":  1,
		"includes": []interface{}{},
	}
	result, err := ResolveIncludes(config, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["version"] != 1 {
		t.Error("expected config unchanged with empty includes")
	}
}

func TestResolveIncludes_MissingFileSkipped(t *testing.T) {
	config := map[string]interface{}{
		"version":  1,
		"includes": []interface{}{"nonexistent.yaml"},
	}
	result, err := ResolveIncludes(config, "/tmp/no-such-dir-12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["version"] != 1 {
		t.Error("expected config unchanged when include file missing")
	}
}

// =============================================================================
// Unit Tests: readYAMLFile
// =============================================================================

func TestReadYAMLFile_NonExistent(t *testing.T) {
	raw, err := readYAMLFile("/tmp/hookwise-nonexistent-12345.yaml")
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if raw != nil {
		t.Error("expected nil for non-existent file")
	}
}

// =============================================================================
// Integration Tests: LoadConfig with Temp Directories
// =============================================================================

func TestLoadConfig_NoConfigFiles(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defaults := GetDefaultConfig()
	if cfg.Version != defaults.Version {
		t.Errorf("expected default version=%d, got %d", defaults.Version, cfg.Version)
	}
	if cfg.Analytics.Enabled != defaults.Analytics.Enabled {
		t.Error("expected default analytics.enabled")
	}
}

func TestLoadConfig_ProjectConfigOnly(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	projectConfig := `
version: 1
analytics:
  enabled: false
  db_path: "/custom/db.sqlite"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Analytics.Enabled {
		t.Error("expected analytics.enabled=false from project config")
	}
	if cfg.Analytics.DBPath != "/custom/db.sqlite" {
		t.Errorf("expected custom db_path, got %q", cfg.Analytics.DBPath)
	}
	// Defaults should fill in missing fields
	if cfg.Settings.LogLevel != "info" {
		t.Errorf("expected default log_level=info, got %q", cfg.Settings.LogLevel)
	}
}

func TestLoadConfig_GlobalConfigOnly(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOOKWISE_STATE_DIR", globalDir)

	globalConfig := `
version: 1
coaching:
  metacognition:
    enabled: true
    interval_seconds: 600
`
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Coaching.Metacognition.Enabled {
		t.Error("expected metacognition.enabled=true from global config")
	}
	if cfg.Coaching.Metacognition.IntervalSeconds != 600 {
		t.Errorf("expected interval_seconds=600, got %d", cfg.Coaching.Metacognition.IntervalSeconds)
	}
}

func TestLoadConfig_ProjectOverridesGlobal(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOOKWISE_STATE_DIR", globalDir)

	globalConfig := `
version: 1
analytics:
  enabled: true
  db_path: "/global/db.sqlite"
coaching:
  metacognition:
    enabled: false
    interval_seconds: 300
`
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projectConfig := `
analytics:
  enabled: false
coaching:
  metacognition:
    enabled: true
`
	if err := os.WriteFile(filepath.Join(projectDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Project overrides analytics.enabled
	if cfg.Analytics.Enabled {
		t.Error("expected analytics.enabled=false from project override")
	}
	// Global's db_path preserved (project didn't override it)
	if cfg.Analytics.DBPath != "/global/db.sqlite" {
		t.Errorf("expected global db_path preserved, got %q", cfg.Analytics.DBPath)
	}
	// Project overrides metacognition.enabled
	if !cfg.Coaching.Metacognition.Enabled {
		t.Error("expected metacognition.enabled=true from project override")
	}
	// Global interval_seconds preserved
	if cfg.Coaching.Metacognition.IntervalSeconds != 300 {
		t.Errorf("expected interval_seconds=300 from global, got %d", cfg.Coaching.Metacognition.IntervalSeconds)
	}
}

func TestLoadConfig_GuardsArrayReplaces(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOOKWISE_STATE_DIR", globalDir)

	globalConfig := `
version: 1
guards:
  - match: "Bash"
    action: block
    reason: "global block"
  - match: "Write"
    action: warn
    reason: "global warn"
`
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projectConfig := `
guards:
  - match: "Read"
    action: confirm
    reason: "project confirm"
`
	if err := os.WriteFile(filepath.Join(projectDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Arrays REPLACE, not concatenate
	if len(cfg.Guards) != 1 {
		t.Fatalf("expected 1 guard (project replaces global), got %d", len(cfg.Guards))
	}
	if cfg.Guards[0].Match != "Read" {
		t.Errorf("expected project guard match=Read, got %q", cfg.Guards[0].Match)
	}
}

func TestLoadConfig_EnvVarInterpolation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))
	t.Setenv("HOOKWISE_CUSTOM_DB", "/env/path/db.sqlite")

	projectConfig := `
version: 1
analytics:
  enabled: true
  db_path: "${HOOKWISE_CUSTOM_DB}"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Analytics.DBPath != "/env/path/db.sqlite" {
		t.Errorf("expected interpolated db_path, got %q", cfg.Analytics.DBPath)
	}
}

func TestLoadConfig_UndefinedEnvVarLeavesLiteral(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))
	os.Unsetenv("HOOKWISE_NONEXISTENT_VAR_XYZ")

	projectConfig := `
version: 1
analytics:
  db_path: "${HOOKWISE_NONEXISTENT_VAR_XYZ}"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Analytics.DBPath != "${HOOKWISE_NONEXISTENT_VAR_XYZ}" {
		t.Errorf("expected literal ${HOOKWISE_NONEXISTENT_VAR_XYZ}, got %q", cfg.Analytics.DBPath)
	}
}

func TestLoadConfig_IncludesResolution(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	// Create included config file
	includeConfig := `
sounds:
  enabled: true
  notification: "ding"
`
	includesDir := filepath.Join(tmpDir, "includes")
	if err := os.MkdirAll(includesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(includesDir, "sounds.yaml"), []byte(includeConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	projectConfig := `
version: 1
includes:
  - includes/sounds.yaml
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Sounds.Enabled {
		t.Error("expected sounds.enabled=true from included file")
	}
	if cfg.Sounds.Notification != "ding" {
		t.Errorf("expected sounds.notification=ding, got %q", cfg.Sounds.Notification)
	}
}

func TestLoadConfig_IncludesWithAbsolutePath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	// Create included config file with absolute path
	includeDir := filepath.Join(tmpDir, "abs-includes")
	if err := os.MkdirAll(includeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	includeConfig := `
greeting:
  enabled: true
`
	includePath := filepath.Join(includeDir, "greeting.yaml")
	if err := os.WriteFile(includePath, []byte(includeConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	projectConfig := "version: 1\nincludes:\n  - " + includePath + "\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Greeting.Enabled {
		t.Error("expected greeting.enabled=true from absolute include")
	}
}

func TestLoadConfig_FullPipelineIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOOKWISE_STATE_DIR", globalDir)
	t.Setenv("HOOKWISE_CUSTOM_LOG", "/var/log/hookwise.log")

	// Global config with base settings
	globalConfig := `
version: 1
settings:
  log_level: warn
  handler_timeout_seconds: 30
coaching:
  metacognition:
    enabled: false
    interval_seconds: 300
`
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create include file
	includesDir := filepath.Join(tmpDir, "project", "includes")
	if err := os.MkdirAll(includesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	includeConfig := `
sounds:
  enabled: true
`
	if err := os.WriteFile(filepath.Join(includesDir, "extras.yaml"), []byte(includeConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	// Project config overrides with env vars and includes
	projectDir := filepath.Join(tmpDir, "project")
	projectConfig := `
coaching:
  metacognition:
    enabled: true
daemon:
  log_file: "${HOOKWISE_CUSTOM_LOG}"
includes:
  - includes/extras.yaml
guards:
  - match: "Bash"
    action: block
    reason: "no bash"
`
	if err := os.WriteFile(filepath.Join(projectDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Project overrides global: metacognition enabled
	if !cfg.Coaching.Metacognition.Enabled {
		t.Error("expected metacognition enabled from project override")
	}
	// Global setting preserved: interval_seconds
	if cfg.Coaching.Metacognition.IntervalSeconds != 300 {
		t.Errorf("expected global interval preserved, got %d", cfg.Coaching.Metacognition.IntervalSeconds)
	}
	// Global log_level preserved (project didn't override)
	if cfg.Settings.LogLevel != "warn" {
		t.Errorf("expected log_level=warn from global, got %q", cfg.Settings.LogLevel)
	}
	// Handler timeout from global
	if cfg.Settings.HandlerTimeoutSeconds != 30 {
		t.Errorf("expected handler_timeout=30 from global, got %d", cfg.Settings.HandlerTimeoutSeconds)
	}
	// Env var interpolated
	if cfg.Daemon.LogFile != "/var/log/hookwise.log" {
		t.Errorf("expected interpolated daemon log_file, got %q", cfg.Daemon.LogFile)
	}
	// Include resolved
	if !cfg.Sounds.Enabled {
		t.Error("expected sounds.enabled=true from include")
	}
	// Guards from project only
	if len(cfg.Guards) != 1 {
		t.Fatalf("expected 1 guard, got %d", len(cfg.Guards))
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	invalidYAML := `
version: [
  broken yaml
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(invalidYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadConfig_StatusLineSnakeCase(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	projectConfig := `
version: 1
status_line:
  enabled: true
  delimiter: " :: "
  cache_path: "/tmp/cache.json"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.StatusLine.Enabled {
		t.Error("expected status_line.enabled=true")
	}
	if cfg.StatusLine.Delimiter != " :: " {
		t.Errorf("expected delimiter=' :: ', got %q", cfg.StatusLine.Delimiter)
	}
	if cfg.StatusLine.CachePath != "/tmp/cache.json" {
		t.Errorf("expected cache_path=/tmp/cache.json, got %q", cfg.StatusLine.CachePath)
	}
}

func TestLoadConfig_CostTrackingSnakeCase(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	projectConfig := `
version: 1
cost_tracking:
  enabled: true
  daily_budget: 25.50
  enforcement: enforce
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.CostTracking.Enabled {
		t.Error("expected cost_tracking.enabled=true")
	}
	if cfg.CostTracking.DailyBudget != 25.50 {
		t.Errorf("expected daily_budget=25.50, got %f", cfg.CostTracking.DailyBudget)
	}
	if cfg.CostTracking.Enforcement != "enforce" {
		t.Errorf("expected enforcement=enforce, got %q", cfg.CostTracking.Enforcement)
	}
}

func TestLoadConfig_TranscriptBackupSnakeCase(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	projectConfig := `
version: 1
transcript_backup:
  enabled: true
  backup_dir: "/custom/transcripts"
  max_size_mb: 200
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.TranscriptBackup.Enabled {
		t.Error("expected transcript_backup.enabled=true")
	}
	if cfg.TranscriptBackup.BackupDir != "/custom/transcripts" {
		t.Errorf("expected backup_dir=/custom/transcripts, got %q", cfg.TranscriptBackup.BackupDir)
	}
	if cfg.TranscriptBackup.MaxSizeMB != 200 {
		t.Errorf("expected max_size_mb=200, got %d", cfg.TranscriptBackup.MaxSizeMB)
	}
}

func TestLoadConfig_FeedsConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	projectConfig := `
version: 1
feeds:
  pulse:
    enabled: true
    interval_seconds: 15
    thresholds:
      green: 0
      yellow: 15
      orange: 30
      red: 60
      skull: 90
  calendar:
    enabled: true
    lookahead_minutes: 60
  weather:
    enabled: true
    latitude: 40.7128
    longitude: -74.006
    temperature_unit: celsius
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Feeds.Pulse.IntervalSeconds != 15 {
		t.Errorf("expected pulse interval=15, got %d", cfg.Feeds.Pulse.IntervalSeconds)
	}
	if cfg.Feeds.Pulse.Thresholds.Skull != 90 {
		t.Errorf("expected skull=90, got %d", cfg.Feeds.Pulse.Thresholds.Skull)
	}
	if !cfg.Feeds.Calendar.Enabled {
		t.Error("expected calendar enabled")
	}
	if cfg.Feeds.Calendar.LookaheadMinutes != 60 {
		t.Errorf("expected lookahead=60, got %d", cfg.Feeds.Calendar.LookaheadMinutes)
	}
	if cfg.Feeds.Weather.Latitude != 40.7128 {
		t.Errorf("expected latitude=40.7128, got %f", cfg.Feeds.Weather.Latitude)
	}
	if cfg.Feeds.Weather.TemperatureUnit != "celsius" {
		t.Errorf("expected celsius, got %q", cfg.Feeds.Weather.TemperatureUnit)
	}
}

func TestLoadConfig_HandlersConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	projectConfig := `
version: 1
handlers:
  - name: my-handler
    type: script
    events:
      - PreToolUse
      - PostToolUse
    command: "./scripts/handler.sh"
    timeout: 5
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Handlers) != 1 {
		t.Fatalf("expected 1 handler, got %d", len(cfg.Handlers))
	}
	h := cfg.Handlers[0]
	if h.Name != "my-handler" {
		t.Errorf("expected name=my-handler, got %q", h.Name)
	}
	if h.Type != "script" {
		t.Errorf("expected type=script, got %q", h.Type)
	}
	if len(h.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(h.Events))
	}
	if h.Command != "./scripts/handler.sh" {
		t.Errorf("expected command=./scripts/handler.sh, got %q", h.Command)
	}
	if h.Timeout != 5 {
		t.Errorf("expected timeout=5, got %d", h.Timeout)
	}
}

func TestLoadConfig_IncludeStripsNestedIncludes(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	// Included file references another include (should be stripped to prevent cycles)
	innerInclude := `
greeting:
  enabled: true
includes:
  - should-not-resolve.yaml
`
	if err := os.WriteFile(filepath.Join(tmpDir, "inner.yaml"), []byte(innerInclude), 0o644); err != nil {
		t.Fatal(err)
	}

	projectConfig := `
version: 1
includes:
  - inner.yaml
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Greeting.Enabled {
		t.Error("expected greeting from inner include")
	}
}

// =============================================================================
// Integration Tests: Validation with Real YAML
// =============================================================================

func TestValidateConfig_IntegrationValidYAML(t *testing.T) {
	data := []byte(`
version: 1
guards:
  - match: "Bash"
    action: block
    reason: "no bash allowed"
settings:
  log_level: debug
tui:
  auto_launch: true
  launch_method: newWindow
`)
	raw, err := parseYAML(data, "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	result := ValidateConfig(raw)
	if !result.Valid {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
}

func TestValidateConfig_IntegrationInvalidYAML(t *testing.T) {
	data := []byte(`
version: "bad"
unknown_key: true
guards: "not-an-array"
settings:
  log_level: "verbose"
`)
	raw, err := parseYAML(data, "test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	result := ValidateConfig(raw)
	if result.Valid {
		t.Error("expected invalid config")
	}
	// Should have at least 4 errors: version, unknown_key, guards, log_level
	if len(result.Errors) < 4 {
		t.Errorf("expected at least 4 errors, got %d: %v", len(result.Errors), result.Errors)
	}
}

// =============================================================================
// Integration Tests: Full resolve pipeline with temp directories
// =============================================================================

func TestIntegration_DeepMergePreservesUnrelatedSections(t *testing.T) {
	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOOKWISE_STATE_DIR", globalDir)

	globalConfig := `
version: 1
sounds:
  enabled: true
  notification: "bell"
greeting:
  enabled: true
`
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tmpDir, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projectConfig := `
sounds:
  enabled: false
`
	if err := os.WriteFile(filepath.Join(projectDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Project overrides sounds.enabled
	if cfg.Sounds.Enabled {
		t.Error("expected sounds disabled by project override")
	}
	// Global notification preserved (deep merge)
	if cfg.Sounds.Notification != "bell" {
		t.Errorf("expected notification=bell from global, got %q", cfg.Sounds.Notification)
	}
	// Global greeting preserved (project didn't touch it)
	if !cfg.Greeting.Enabled {
		t.Error("expected greeting.enabled=true from global")
	}
}

func TestIntegration_MultipleIncludes(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	// Create two include files
	includesDir := filepath.Join(tmpDir, "includes")
	if err := os.MkdirAll(includesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	include1 := `
sounds:
  enabled: true
`
	include2 := `
greeting:
  enabled: true
`
	if err := os.WriteFile(filepath.Join(includesDir, "one.yaml"), []byte(include1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(includesDir, "two.yaml"), []byte(include2), 0o644); err != nil {
		t.Fatal(err)
	}

	projectConfig := `
version: 1
includes:
  - includes/one.yaml
  - includes/two.yaml
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Sounds.Enabled {
		t.Error("expected sounds from first include")
	}
	if !cfg.Greeting.Enabled {
		t.Error("expected greeting from second include")
	}
}

func TestIntegration_IncludeOverridesBaseConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	includeConfig := `
analytics:
  enabled: false
`
	if err := os.WriteFile(filepath.Join(tmpDir, "override.yaml"), []byte(includeConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	projectConfig := `
version: 1
analytics:
  enabled: true
includes:
  - override.yaml
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Include overrides the base config value
	if cfg.Analytics.Enabled {
		t.Error("expected include to override analytics.enabled to false")
	}
}

func TestIntegration_DefaultsBackfillMissingFields(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	// Minimal config — most fields missing
	projectConfig := `
version: 1
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defaults := GetDefaultConfig()

	// Verify many defaults are filled in
	if cfg.Analytics.Enabled != defaults.Analytics.Enabled {
		t.Error("expected default analytics.enabled to be backfilled")
	}
	if cfg.Settings.LogLevel != defaults.Settings.LogLevel {
		t.Errorf("expected default log_level=%q, got %q", defaults.Settings.LogLevel, cfg.Settings.LogLevel)
	}
	if cfg.Coaching.Communication.Tone != defaults.Coaching.Communication.Tone {
		t.Errorf("expected default tone=%q, got %q", defaults.Coaching.Communication.Tone, cfg.Coaching.Communication.Tone)
	}
	if cfg.Feeds.Pulse.IntervalSeconds != defaults.Feeds.Pulse.IntervalSeconds {
		t.Errorf("expected default pulse interval=%d, got %d", defaults.Feeds.Pulse.IntervalSeconds, cfg.Feeds.Pulse.IntervalSeconds)
	}
	if cfg.TUI.LaunchMethod != defaults.TUI.LaunchMethod {
		t.Errorf("expected default launch method=%q, got %q", defaults.TUI.LaunchMethod, cfg.TUI.LaunchMethod)
	}
}

func TestIntegration_DaemonConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	projectConfig := `
version: 1
daemon:
  auto_start: false
  inactivity_timeout_minutes: 60
  log_file: "/custom/daemon.log"
`
	if err := os.WriteFile(filepath.Join(tmpDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Daemon.AutoStart {
		t.Error("expected auto_start=false")
	}
	if cfg.Daemon.InactivityTimeoutMinutes != 60 {
		t.Errorf("expected timeout=60, got %d", cfg.Daemon.InactivityTimeoutMinutes)
	}
	if cfg.Daemon.LogFile != "/custom/daemon.log" {
		t.Errorf("expected custom log file, got %q", cfg.Daemon.LogFile)
	}
}

// =============================================================================
// Test helpers
// =============================================================================

func TestKnownSectionsList(t *testing.T) {
	sections := knownSectionsList()
	if len(sections) != len(knownSections) {
		t.Errorf("expected %d sections, got %d", len(knownSections), len(sections))
	}
	for _, s := range sections {
		if !knownSections[s] {
			t.Errorf("unexpected section in list: %q", s)
		}
	}
}

func TestIsValidGuardAction(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"block", true},
		{"warn", true},
		{"confirm", true},
		{"allow", false},
		{"", false},
		{"BLOCK", false},
	}
	for _, tt := range tests {
		if got := isValidGuardAction(tt.input); got != tt.want {
			t.Errorf("isValidGuardAction(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsValidHandlerType(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"builtin", true},
		{"script", true},
		{"inline", true},
		{"external", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isValidHandlerType(tt.input); got != tt.want {
			t.Errorf("isValidHandlerType(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsValidLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"debug", true},
		{"info", true},
		{"warn", true},
		{"error", true},
		{"verbose", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isValidLogLevel(tt.input); got != tt.want {
			t.Errorf("isValidLogLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDeepMerge_MultiLevelNesting(t *testing.T) {
	target := map[string]interface{}{
		"a": map[string]interface{}{
			"b": map[string]interface{}{
				"c": "target-value",
				"d": "only-in-target",
			},
		},
	}
	source := map[string]interface{}{
		"a": map[string]interface{}{
			"b": map[string]interface{}{
				"c": "source-value",
				"e": "only-in-source",
			},
		},
	}
	result := DeepMerge(target, source)
	a := result["a"].(map[string]interface{})
	b := a["b"].(map[string]interface{})
	if b["c"] != "source-value" {
		t.Errorf("expected source override, got %v", b["c"])
	}
	if b["d"] != "only-in-target" {
		t.Errorf("expected target preserved, got %v", b["d"])
	}
	if b["e"] != "only-in-source" {
		t.Errorf("expected source addition, got %v", b["e"])
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		input interface{}
		want  float64
		ok    bool
	}{
		{1.5, 1.5, true},
		{42, 42.0, true},
		{"string", 0, false},
		{nil, 0, false},
	}
	for _, tt := range tests {
		got, ok := toFloat64(tt.input)
		if ok != tt.ok || got != tt.want {
			t.Errorf("toFloat64(%v) = (%v, %v), want (%v, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

// Verify default config struct has correct field types via reflection
func TestGetDefaultConfig_StructIntegrity(t *testing.T) {
	cfg := GetDefaultConfig()

	// Ensure slices are non-nil (not just empty)
	if cfg.Guards == nil {
		t.Error("Guards should be non-nil empty slice")
	}
	if cfg.Handlers == nil {
		t.Error("Handlers should be non-nil empty slice")
	}
	if cfg.Includes == nil {
		t.Error("Includes should be non-nil empty slice")
	}
	if cfg.Feeds.Custom == nil {
		t.Error("Feeds.Custom should be non-nil empty slice")
	}
	if cfg.CostTracking.Rates == nil {
		t.Error("CostTracking.Rates should be non-nil empty map")
	}

	// Verify types via reflection
	v := reflect.TypeOf(cfg)
	if v.Kind() != reflect.Struct {
		t.Errorf("expected struct, got %v", v.Kind())
	}
}
