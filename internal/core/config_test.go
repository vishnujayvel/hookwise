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

// transcript_backup was a dead config block (parse-only, never read; the
// "transcript backup" feature was never ported to Go). After removal it must
// be reported as an unknown section so a config still carrying it is flagged
// honestly rather than silently accepted as a no-op.
func TestValidateConfig_TranscriptBackupIsUnknownSection(t *testing.T) {
	raw := map[string]interface{}{
		"version":           1,
		"transcript_backup": map[string]interface{}{"enabled": true},
	}
	result := ValidateConfig(raw)
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "Unknown config section") && e.Path == "transcript_backup" {
			found = true
		}
	}
	if !found {
		t.Error("expected transcript_backup to be reported as an unknown config section after removal")
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

func TestValidateConfig_NegativeSnapshotSettings(t *testing.T) {
	for _, key := range []string{"snapshot_interval_minutes", "snapshot_retention"} {
		raw := map[string]interface{}{
			"analytics": map[string]interface{}{key: -1},
		}
		if ValidateConfig(raw).Valid {
			t.Errorf("expected invalid config for negative %s", key)
		}
	}
	// 0 is valid: interval 0 = use default, retention 0 = keep all.
	raw := map[string]interface{}{
		"analytics": map[string]interface{}{
			"snapshot_interval_minutes": 0,
			"snapshot_retention":        0,
		},
	}
	if !ValidateConfig(raw).Valid {
		t.Error("expected zero snapshot settings to be valid (sentinel values)")
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

func TestValidateConfig_EmptySubstringGuardValue(t *testing.T) {
	validGuard := func(condKey, condVal string) map[string]interface{} {
		return map[string]interface{}{
			"match":  "Bash",
			"action": "warn",
			"reason": "x",
			condKey:  condVal,
		}
	}

	errorsForPath := func(raw map[string]interface{}, path string) []ValidationError {
		result := ValidateConfig(raw)
		var out []ValidationError
		for _, e := range result.Errors {
			if e.Path == path {
				out = append(out, e)
			}
		}
		return out
	}

	// Positive cases: each substring operator with empty value must produce an error.
	for _, op := range []string{"contains", "starts_with", "ends_with"} {
		for _, condKey := range []string{"when", "unless"} {
			expr := `command ` + op + ` ""`
			path := "guards[0]." + condKey
			raw := map[string]interface{}{
				"guards": []interface{}{validGuard(condKey, expr)},
			}
			errs := errorsForPath(raw, path)
			if len(errs) == 0 {
				t.Errorf("operator=%q condKey=%q: expected ValidationError at path %q, got none", op, condKey, path)
				continue
			}
			msg := errs[0].Message
			if !strings.Contains(msg, "empty") && !strings.Contains(msg, "match-all") {
				t.Errorf("operator=%q condKey=%q: expected message to mention 'empty' or 'match-all', got %q", op, condKey, msg)
			}
			if errs[0].Suggestion == "" {
				t.Errorf("operator=%q condKey=%q: expected non-empty Suggestion", op, condKey)
			}
		}
	}

	// Negative cases: must NOT produce empty-substring error.
	negativeCases := []struct {
		label   string
		condKey string
		expr    string
	}{
		{"non-empty contains", "when", `command contains "rm"`},
		{"equals with empty value", "when", `command == ""`},
		{"matches with empty value", "when", `command matches ""`},
	}
	for _, tc := range negativeCases {
		path := "guards[0]." + tc.condKey
		raw := map[string]interface{}{
			"guards": []interface{}{validGuard(tc.condKey, tc.expr)},
		}
		errs := errorsForPath(raw, path)
		if len(errs) > 0 {
			t.Errorf("case=%q: expected no empty-substring error at path %q, got %v", tc.label, path, errs)
		}
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

func TestGetDefaultConfig_FeedDefaults(t *testing.T) {
	cfg := GetDefaultConfig()
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
analytics:
  enabled: false
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
	if cfg.Analytics.Enabled {
		t.Error("expected analytics.enabled=false from global config")
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
}

// LoadGlobalConfig sources the singleton daemon's config from the global file
// ONLY (#89). It must never observe a project hookwise.yaml, so its result is
// deterministic regardless of which directory cold-started the daemon.
func TestLoadGlobalConfig_ReadsGlobalOnly(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", globalDir)

	globalConfig := `
version: 1
feeds:
  weather:
    enabled: true
    interval_seconds: 900
  news:
    enabled: false
`
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Feeds.Weather.Enabled {
		t.Error("expected weather enabled from global config")
	}
	if cfg.Feeds.Weather.IntervalSeconds != 900 {
		t.Errorf("expected weather interval 900, got %d", cfg.Feeds.Weather.IntervalSeconds)
	}
	if cfg.Feeds.News.Enabled {
		t.Error("expected news disabled from global config")
	}
}

// A project hookwise.yaml in the working directory must NOT influence
// LoadGlobalConfig — that is the whole point of the global-only source.
func TestLoadGlobalConfig_IgnoresProjectOverlayInCwd(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", globalDir)
	globalConfig := `
version: 1
feeds:
  weather:
    enabled: true
`
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(globalConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	// A project config in cwd that would flip weather off under LoadConfig.
	projectDir := t.TempDir()
	projectConfig := `
feeds:
  weather:
    enabled: false
`
	if err := os.WriteFile(filepath.Join(projectDir, "hookwise.yaml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(projectDir)

	// Sanity: LoadConfig(cwd) DOES see the project override (overlay is real).
	proj, err := LoadConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if proj.Feeds.Weather.Enabled {
		t.Fatal("precondition failed: project overlay should disable weather under LoadConfig")
	}

	// LoadGlobalConfig must ignore the project overlay entirely.
	global, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if !global.Feeds.Weather.Enabled {
		t.Error("LoadGlobalConfig must ignore the project overlay; weather should stay enabled")
	}
}

func TestLoadGlobalConfig_NoFileReturnsDefaults(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", globalDir)

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(cfg, GetDefaultConfig()) {
		t.Error("expected defaults when no global config file exists")
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
analytics:
  db_path: "${HOOKWISE_CUSTOM_LOG}"
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

	// Global log_level preserved (project didn't override)
	if cfg.Settings.LogLevel != "warn" {
		t.Errorf("expected log_level=warn from global, got %q", cfg.Settings.LogLevel)
	}
	// Handler timeout from global
	if cfg.Settings.HandlerTimeoutSeconds != 30 {
		t.Errorf("expected handler_timeout=30 from global, got %d", cfg.Settings.HandlerTimeoutSeconds)
	}
	// Env var interpolated
	if cfg.Analytics.DBPath != "/var/log/hookwise.log" {
		t.Errorf("expected interpolated analytics db_path, got %q", cfg.Analytics.DBPath)
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

func TestLoadConfig_FeedsConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, "global"))

	projectConfig := `
version: 1
feeds:
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
	if cfg.Feeds.Project.IntervalSeconds != defaults.Feeds.Project.IntervalSeconds {
		t.Errorf("expected default project interval=%d, got %d", defaults.Feeds.Project.IntervalSeconds, cfg.Feeds.Project.IntervalSeconds)
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
}

func TestValidateConfig_MalformedGuardCondition(t *testing.T) {
	validGuard := func(condKey, condVal string) map[string]interface{} {
		return map[string]interface{}{
			"match":  "Bash",
			"action": "warn",
			"reason": "x",
			condKey:  condVal,
		}
	}

	errorsForPath := func(raw map[string]interface{}, path string) []ValidationError {
		result := ValidateConfig(raw)
		var out []ValidationError
		for _, e := range result.Errors {
			if e.Path == path {
				out = append(out, e)
			}
		}
		return out
	}

	// Positive cases: malformed expressions must produce a "malformed" error.
	positiveCases := []struct {
		label   string
		condKey string
		expr    string
	}{
		{"operator typo when", "when", `command containss "x"`},
		{"operator typo unless", "unless", `command containss "x"`},
		{"missing field path when", "when", `contains "x"`},
		{"garbage expression when", "when", `this is not a condition`},
	}
	for _, tc := range positiveCases {
		path := "guards[0]." + tc.condKey
		raw := map[string]interface{}{
			"guards": []interface{}{validGuard(tc.condKey, tc.expr)},
		}
		errs := errorsForPath(raw, path)
		if len(errs) == 0 {
			t.Errorf("case=%q: expected ValidationError at path %q, got none", tc.label, path)
			continue
		}
		msg := errs[0].Message
		if !strings.Contains(msg, "malformed") {
			t.Errorf("case=%q: expected message to mention 'malformed', got %q", tc.label, msg)
		}
		if errs[0].Suggestion == "" {
			t.Errorf("case=%q: expected non-empty Suggestion", tc.label)
		}
	}

	// Negative cases: valid and absent conditions must NOT produce a "malformed" error.
	negativeCases := []struct {
		label   string
		condKey string
		expr    string
	}{
		{"valid contains", "when", `command contains "rm"`},
		{"valid equals", "when", `tool_input.command == "git"`},
	}
	for _, tc := range negativeCases {
		path := "guards[0]." + tc.condKey
		raw := map[string]interface{}{
			"guards": []interface{}{validGuard(tc.condKey, tc.expr)},
		}
		errs := errorsForPath(raw, path)
		for _, e := range errs {
			if strings.Contains(e.Message, "malformed") {
				t.Errorf("case=%q: expected no 'malformed' error, got %q", tc.label, e.Message)
			}
		}
	}

	// Guard with no "when" key at all — must not produce a malformed error.
	{
		noCondGuard := map[string]interface{}{
			"match":  "Bash",
			"action": "warn",
			"reason": "x",
		}
		raw := map[string]interface{}{
			"guards": []interface{}{noCondGuard},
		}
		for _, e := range ValidateConfig(raw).Errors {
			if strings.Contains(e.Message, "malformed") {
				t.Errorf("guard with no condition key: expected no 'malformed' error, got %q at %q", e.Message, e.Path)
			}
		}
	}

	// Guard with empty string "when" — not malformed (means "no condition").
	{
		emptyCondGuard := validGuard("when", "")
		raw := map[string]interface{}{
			"guards": []interface{}{emptyCondGuard},
		}
		for _, e := range ValidateConfig(raw).Errors {
			if e.Path == "guards[0].when" && strings.Contains(e.Message, "malformed") {
				t.Errorf("empty-string condition: expected no 'malformed' error, got %q", e.Message)
			}
		}
	}

	// The existing empty-substring case: "command contains """ should be flagged
	// with "empty"/"match-all" message, NOT "malformed".
	{
		path := "guards[0].when"
		raw := map[string]interface{}{
			"guards": []interface{}{validGuard("when", `command contains ""`)},
		}
		errs := errorsForPath(raw, path)
		if len(errs) == 0 {
			t.Errorf("empty-substring case: expected at least one error at %q", path)
		} else {
			msg := errs[0].Message
			if strings.Contains(msg, "malformed") {
				t.Errorf("empty-substring case: message should not contain 'malformed', got %q", msg)
			}
			if !strings.Contains(msg, "empty") && !strings.Contains(msg, "match-all") {
				t.Errorf("empty-substring case: expected message to mention 'empty' or 'match-all', got %q", msg)
			}
		}
	}
}

func TestValidateConfig_InvalidRegexGuardCondition(t *testing.T) {
	validGuard := func(condKey, condVal string) map[string]interface{} {
		return map[string]interface{}{
			"match":  "Bash",
			"action": "warn",
			"reason": "x",
			condKey:  condVal,
		}
	}

	errorsForPath := func(raw map[string]interface{}, path string) []ValidationError {
		result := ValidateConfig(raw)
		var out []ValidationError
		for _, e := range result.Errors {
			if e.Path == path {
				out = append(out, e)
			}
		}
		return out
	}

	// Positive cases: matches operator with invalid regex must produce an error
	// whose Message mentions "regex" or "regular expression".
	positiveCases := []struct {
		label   string
		condKey string
		expr    string
	}{
		{"unclosed char class when", "when", `command matches "["`},
		{"unclosed group when", "when", `command matches "("`},
		{"unclosed group unless", "unless", `command matches "("`},
		{"bare repetition when", "when", `command matches "*"`},
	}
	for _, tc := range positiveCases {
		path := "guards[0]." + tc.condKey
		raw := map[string]interface{}{
			"guards": []interface{}{validGuard(tc.condKey, tc.expr)},
		}
		errs := errorsForPath(raw, path)
		if len(errs) == 0 {
			t.Errorf("case=%q: expected ValidationError at path %q, got none", tc.label, path)
			continue
		}
		msg := errs[0].Message
		if !strings.Contains(msg, "regex") && !strings.Contains(msg, "regular expression") {
			t.Errorf("case=%q: expected message to mention 'regex' or 'regular expression', got %q", tc.label, msg)
		}
		if errs[0].Suggestion == "" {
			t.Errorf("case=%q: expected non-empty Suggestion", tc.label)
		}
	}

	// Negative cases: must NOT produce a regex error at the guard path.
	negativeCases := []struct {
		label   string
		condKey string
		expr    string
	}{
		{"valid regex rm.*", "when", `command matches "rm.*"`},
		{"valid regex .*", "when", `command matches ".*"`},
		{"contains with literal [", "when", `command contains "["`},
		{"equals with literal [", "when", `command == "["`},
	}
	for _, tc := range negativeCases {
		path := "guards[0]." + tc.condKey
		raw := map[string]interface{}{
			"guards": []interface{}{validGuard(tc.condKey, tc.expr)},
		}
		errs := errorsForPath(raw, path)
		for _, e := range errs {
			if strings.Contains(e.Message, "regex") || strings.Contains(e.Message, "regular expression") {
				t.Errorf("case=%q: expected no regex error at path %q, got %q", tc.label, path, e.Message)
			}
		}
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

// =============================================================================
// Unit Tests: ValidateConfig — invalid glob in guard match
// =============================================================================

func TestValidateConfig_InvalidGlobMatch(t *testing.T) {
	makeRaw := func(matchVal string) map[string]interface{} {
		return map[string]interface{}{
			"guards": []interface{}{
				map[string]interface{}{
					"match":  matchVal,
					"action": "warn",
					"reason": "x",
				},
			},
		}
	}

	errorsForPath := func(raw map[string]interface{}, path string) []ValidationError {
		result := ValidateConfig(raw)
		var out []ValidationError
		for _, e := range result.Errors {
			if e.Path == path {
				out = append(out, e)
			}
		}
		return out
	}

	const path = "guards[0].match"

	// Positive cases: invalid glob patterns that contain glob metacharacters must
	// produce a ValidationError at guards[0].match mentioning "glob".
	positiveCases := []struct {
		label string
		match string
	}{
		{"unclosed char class", "Bash["},
		{"unclosed char class with content", "Edit[0-9"},
	}
	for _, tc := range positiveCases {
		raw := makeRaw(tc.match)
		errs := errorsForPath(raw, path)
		var globErrs []ValidationError
		for _, e := range errs {
			if strings.Contains(e.Message, "glob") {
				globErrs = append(globErrs, e)
			}
		}
		if len(globErrs) == 0 {
			t.Errorf("case=%q match=%q: expected ValidationError mentioning 'glob' at %q, got none (all errors at path: %v)", tc.label, tc.match, path, errs)
		}
	}

	// Negative cases: valid patterns (plain exact or valid globs) must NOT produce
	// a ValidationError mentioning "glob" at guards[0].match.
	negativeCases := []struct {
		label string
		match string
	}{
		{"plain exact", "Bash"},
		{"pipe in value (no glob metachar)", "Edit|Write"},
		{"wildcard star", "*"},
		{"prefix glob", "tool_*"},
		{"valid brace group", "Bash{a,b}"},
	}
	for _, tc := range negativeCases {
		raw := makeRaw(tc.match)
		errs := errorsForPath(raw, path)
		for _, e := range errs {
			if strings.Contains(e.Message, "glob") {
				t.Errorf("case=%q match=%q: expected no 'glob' error, got %q", tc.label, tc.match, e.Message)
			}
		}
	}
}

func TestValidateConfig_InvalidHandlerEvents(t *testing.T) {
	makeRaw := func(eventsVal interface{}) map[string]interface{} {
		return map[string]interface{}{
			"handlers": []interface{}{
				map[string]interface{}{
					"name":   "h",
					"type":   "builtin",
					"events": eventsVal,
				},
			},
		}
	}

	errorsForPath := func(raw map[string]interface{}, path string) []ValidationError {
		result := ValidateConfig(raw)
		var out []ValidationError
		for _, e := range result.Errors {
			if e.Path == path {
				out = append(out, e)
			}
		}
		return out
	}

	const path = "handlers[0].events"

	// Positive cases: invalid event names must produce a ValidationError at
	// handlers[0].events whose Message contains the bad token.
	positiveCases := []struct {
		label     string
		eventsVal interface{}
		badToken  string
	}{
		{"typo in slice", []interface{}{"PreToolUze"}, "PreToolUze"},
		{"dead wildcard", []interface{}{"*"}, "*"},
		{"scalar string typo", "PreToolUze", "PreToolUze"},
	}
	for _, tc := range positiveCases {
		raw := makeRaw(tc.eventsVal)
		errs := errorsForPath(raw, path)
		var found bool
		for _, e := range errs {
			if strings.Contains(e.Message, tc.badToken) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("case=%q: expected ValidationError at %q mentioning %q, got errors: %v", tc.label, path, tc.badToken, errs)
		}
	}

	// Mixed case: one valid event + one invalid; must flag the invalid one (Foo).
	{
		raw := makeRaw([]interface{}{"PreToolUse", "Foo"})
		errs := errorsForPath(raw, path)
		var found bool
		for _, e := range errs {
			if strings.Contains(e.Message, "Foo") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("mixed case: expected ValidationError at %q mentioning %q, got errors: %v", path, "Foo", errs)
		}
	}

	// Negative cases: valid event names must NOT produce any error at handlers[0].events.
	negativeCases := []struct {
		label     string
		eventsVal interface{}
	}{
		{"single valid slice", []interface{}{"PreToolUse"}},
		{"multiple valid slice", []interface{}{"PreToolUse", "Stop", "SessionEnd"}},
		{"scalar valid string", "Stop"},
	}
	for _, tc := range negativeCases {
		raw := makeRaw(tc.eventsVal)
		errs := errorsForPath(raw, path)
		for _, e := range errs {
			if strings.Contains(e.Message, "not a valid event") {
				t.Errorf("case=%q: expected no invalid-event error at %q, got %q", tc.label, path, e.Message)
			}
		}
	}
}
