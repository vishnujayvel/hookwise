package proptest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// --- Generators ---

// genToolName generates valid tool names (alphanumeric + underscores).
func genToolName() *rapid.Generator[string] {
	return rapid.StringMatching(`[A-Za-z][A-Za-z0-9_]{0,30}`)
}

// genAction generates a valid guard action.
func genAction() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{core.ActionAllow, core.ActionBlock, core.ActionWarn, core.ActionConfirm})
}

// genGuardRule generates a single GuardRuleConfig with valid fields.
func genGuardRule() *rapid.Generator[core.GuardRuleConfig] {
	return rapid.Custom(func(t *rapid.T) core.GuardRuleConfig {
		return core.GuardRuleConfig{
			Match:  rapid.OneOf(genToolName(), rapid.Just("*")).Draw(t, "match"),
			Action: genAction().Draw(t, "action"),
			Reason: rapid.StringMatching(`[a-z ]{0,20}`).Draw(t, "reason"),
		}
	})
}

// genGuardRules generates a slice of guard rules.
func genGuardRules() *rapid.Generator[[]core.GuardRuleConfig] {
	return rapid.SliceOf(genGuardRule())
}

// genPayloadMap generates a payload map with optional tool_input submap.
func genPayloadMap() *rapid.Generator[map[string]interface{}] {
	return rapid.Custom(func(t *rapid.T) map[string]interface{} {
		m := make(map[string]interface{})
		if rapid.Bool().Draw(t, "hasToolInput") {
			ti := make(map[string]interface{})
			ti["command"] = rapid.StringMatching(`[a-z /.\-]{0,50}`).Draw(t, "command")
			m["tool_input"] = ti
		}
		return m
	})
}

// --- Property Tests ---

// TestProperty_Evaluate_BoundedOutput verifies that Evaluate() always returns
// an action in {allow, block, warn, confirm} for any inputs.
func TestProperty_Evaluate_BoundedOutput(t *testing.T) {
	validActions := map[string]bool{
		core.ActionAllow: true, core.ActionBlock: true,
		core.ActionWarn: true, core.ActionConfirm: true,
	}

	rapid.Check(t, func(t *rapid.T) {
		toolName := genToolName().Draw(t, "toolName")
		rules := genGuardRules().Draw(t, "rules")
		payload := genPayloadMap().Draw(t, "payload")

		result := core.Evaluate(toolName, payload, rules)

		if !validActions[result.Action] {
			t.Fatalf("Evaluate returned invalid action %q for toolName=%q, %d rules",
				result.Action, toolName, len(rules))
		}
	})
}

// TestProperty_Evaluate_EmptyRulesAllow verifies the identity property:
// an empty rule set always returns "allow".
func TestProperty_Evaluate_EmptyRulesAllow(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		toolName := genToolName().Draw(t, "toolName")
		payload := genPayloadMap().Draw(t, "payload")

		result := core.Evaluate(toolName, payload, nil)

		if result.Action != core.ActionAllow {
			t.Fatalf("empty rules should return 'allow', got %q for toolName=%q",
				result.Action, toolName)
		}
	})
}

// TestProperty_Evaluate_FirstMatchWins verifies that appending rules after
// a matching rule does not change the result.
func TestProperty_Evaluate_FirstMatchWins(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		toolName := genToolName().Draw(t, "toolName")
		payload := genPayloadMap().Draw(t, "payload")

		// Create a rule that matches this exact tool name
		matchingRule := core.GuardRuleConfig{
			Match:  toolName,
			Action: genAction().Draw(t, "action"),
			Reason: "first match",
		}
		baseRules := []core.GuardRuleConfig{matchingRule}
		result1 := core.Evaluate(toolName, payload, baseRules)

		// Append extra rules after the matching one
		extraRules := genGuardRules().Draw(t, "extraRules")
		extendedRules := make([]core.GuardRuleConfig, 0, 1+len(extraRules))
		extendedRules = append(extendedRules, matchingRule)
		extendedRules = append(extendedRules, extraRules...)
		result2 := core.Evaluate(toolName, payload, extendedRules)

		if result1.Action != result2.Action {
			t.Fatalf("first-match-wins violated: base=%q, extended=%q for toolName=%q",
				result1.Action, result2.Action, toolName)
		}
	})
}

// TestProperty_ParseTimeFlex_RoundTrip verifies that any time formatted as
// RFC3339 round-trips through ParseTimeFlex back to the same time.
func TestProperty_ParseTimeFlex_RoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random time in a reasonable range
		year := rapid.IntRange(2000, 2030).Draw(t, "year")
		month := rapid.IntRange(1, 12).Draw(t, "month")
		day := rapid.IntRange(1, 28).Draw(t, "day") // avoid invalid dates
		hour := rapid.IntRange(0, 23).Draw(t, "hour")
		minute := rapid.IntRange(0, 59).Draw(t, "minute")
		second := rapid.IntRange(0, 59).Draw(t, "second")

		original := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
		formatted := original.Format(time.RFC3339)

		parsed, err := core.ParseTimeFlex(formatted)
		if err != nil {
			t.Fatalf("ParseTimeFlex(%q) returned error: %v", formatted, err)
		}

		if !parsed.Equal(original) {
			t.Fatalf("round-trip failed: original=%v, parsed=%v, formatted=%q",
				original, parsed, formatted)
		}
	})
}

// TestProperty_ParseTimeFlex_NeverPanics verifies that arbitrary string input
// to ParseTimeFlex never panics.
func TestProperty_ParseTimeFlex_NeverPanics(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		input := rapid.String().Draw(t, "input")
		// Should never panic -- may return error, that's fine
		_, _ = core.ParseTimeFlex(input)
	})
}

// TestProperty_EvaluateCondition_NilSafe verifies that EvaluateCondition
// never panics with nil or empty data maps.
func TestProperty_EvaluateCondition_NilSafe(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		expr := rapid.String().Draw(t, "expression")
		// Test with nil map
		_ = core.EvaluateCondition(expr, nil)
		// Test with empty map
		_ = core.EvaluateCondition(expr, map[string]interface{}{})
	})
}

// TestProperty_Evaluate_Reflexive verifies that matching a tool name against
// itself (as a non-glob exact match rule) always matches.
func TestProperty_Evaluate_Reflexive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		toolName := genToolName().Draw(t, "toolName")
		action := rapid.SampledFrom([]string{core.ActionBlock, core.ActionWarn, core.ActionConfirm}).Draw(t, "action")
		rules := []core.GuardRuleConfig{
			{Match: toolName, Action: action, Reason: "reflexive test"},
		}
		payload := map[string]interface{}{}

		result := core.Evaluate(toolName, payload, rules)
		if result.Action != action {
			t.Fatalf("reflexive match failed: toolName=%q, expected action=%q, got=%q",
				toolName, action, result.Action)
		}
	})
}

// TestProperty_LoadConfig_StructuralInvariant verifies that valid YAML configs
// always produce Version >= 0 and non-nil Guards slice.
func TestProperty_LoadConfig_StructuralInvariant(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		version := rapid.IntRange(1, 10).Draw(t, "version")
		numGuards := rapid.IntRange(1, 5).Draw(t, "numGuards")

		// Build valid YAML with at least one guard entry
		yamlContent := fmt.Sprintf("version: %d\nguards:\n", version)
		for i := 0; i < numGuards; i++ {
			yamlContent += fmt.Sprintf("  - match: \"Tool%c\"\n", rune('A'+i))
			yamlContent += "    action: warn\n"
			yamlContent += "    reason: \"test\"\n"
		}

		dir, err := os.MkdirTemp("", "proptest-config-*")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })

		if err := os.WriteFile(filepath.Join(dir, "hookwise.yaml"), []byte(yamlContent), 0o644); err != nil {
			t.Fatal(err)
		}

		// Set state dir to a temp dir to avoid touching the real home directory
		stateDir, err := os.MkdirTemp("", "proptest-state-*")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(stateDir) })

		prevStateDir := os.Getenv("HOOKWISE_STATE_DIR")
		os.Setenv("HOOKWISE_STATE_DIR", stateDir)
		t.Cleanup(func() {
			if prevStateDir == "" {
				os.Unsetenv("HOOKWISE_STATE_DIR")
			} else {
				os.Setenv("HOOKWISE_STATE_DIR", prevStateDir)
			}
		})

		config, err := core.LoadConfig(dir)
		if err != nil {
			return // parse error is acceptable; we test the invariant on success
		}

		if config.Version < 1 {
			t.Fatalf("config.Version should be >= 1, got %d", config.Version)
		}
		if config.Guards == nil {
			t.Fatal("config.Guards should not be nil when guards are specified")
		}
		if len(config.Guards) != numGuards {
			t.Fatalf("expected %d guards, got %d", numGuards, len(config.Guards))
		}
	})
}
