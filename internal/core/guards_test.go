package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// ParseCondition tests
// =============================================================================

func TestParseCondition_ValidExpressions(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		expected *ParsedCondition
	}{
		{
			name: "contains with double quotes",
			expr: `tool_input.command contains "force push"`,
			expected: &ParsedCondition{
				FieldPath: "tool_input.command",
				Operator:  "contains",
				Value:     "force push",
			},
		},
		{
			name: "starts_with with single quotes",
			expr: `tool_input.path starts_with '/etc'`,
			expected: &ParsedCondition{
				FieldPath: "tool_input.path",
				Operator:  "starts_with",
				Value:     "/etc",
			},
		},
		{
			name: "ends_with with unquoted value",
			expr: `tool_input.file ends_with .env`,
			expected: &ParsedCondition{
				FieldPath: "tool_input.file",
				Operator:  "ends_with",
				Value:     ".env",
			},
		},
		{
			name: "== operator",
			expr: `tool_input.mode == "production"`,
			expected: &ParsedCondition{
				FieldPath: "tool_input.mode",
				Operator:  "==",
				Value:     "production",
			},
		},
		{
			name: "equals operator",
			expr: `tool_input.env equals staging`,
			expected: &ParsedCondition{
				FieldPath: "tool_input.env",
				Operator:  "equals",
				Value:     "staging",
			},
		},
		{
			name: "matches operator with regex",
			expr: `tool_input.command matches "rm\s+-rf"`,
			expected: &ParsedCondition{
				FieldPath: "tool_input.command",
				Operator:  "matches",
				Value:     `rm\s+-rf`,
			},
		},
		{
			name: "deeply nested field path",
			expr: `tool_input.nested.deep.value contains hello`,
			expected: &ParsedCondition{
				FieldPath: "tool_input.nested.deep.value",
				Operator:  "contains",
				Value:     "hello",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCondition(tt.expr)
			require.NotNil(t, result, "expected non-nil ParsedCondition")
			assert.Equal(t, tt.expected.FieldPath, result.FieldPath)
			assert.Equal(t, tt.expected.Operator, result.Operator)
			assert.Equal(t, tt.expected.Value, result.Value)
		})
	}
}

func TestParseCondition_Invalid(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
		{"missing operator", "tool_input.command force push"},
		{"unknown operator", `tool_input.command like "test"`},
		{"no value", "tool_input.command contains"},
		{"just a word", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCondition(tt.expr)
			assert.Nil(t, result, "expected nil for invalid expression %q", tt.expr)
		})
	}
}

// =============================================================================
// ResolveFieldPath tests
// =============================================================================

func TestResolveFieldPath_Simple(t *testing.T) {
	data := map[string]interface{}{
		"command": "git push --force",
	}
	result := ResolveFieldPath(data, "command")
	require.NotNil(t, result)
	assert.Equal(t, "git push --force", *result)
}

func TestResolveFieldPath_Nested(t *testing.T) {
	data := map[string]interface{}{
		"tool_input": map[string]interface{}{
			"command": "rm -rf /",
		},
	}
	result := ResolveFieldPath(data, "tool_input.command")
	require.NotNil(t, result)
	assert.Equal(t, "rm -rf /", *result)
}

func TestResolveFieldPath_DeeplyNested(t *testing.T) {
	data := map[string]interface{}{
		"tool_input": map[string]interface{}{
			"nested": map[string]interface{}{
				"deep": "found it",
			},
		},
	}
	result := ResolveFieldPath(data, "tool_input.nested.deep")
	require.NotNil(t, result)
	assert.Equal(t, "found it", *result)
}

func TestResolveFieldPath_MissingIntermediateKey(t *testing.T) {
	data := map[string]interface{}{
		"tool_input": map[string]interface{}{
			"command": "ls",
		},
	}
	// "nonexistent" key doesn't exist — must return nil, not panic
	result := ResolveFieldPath(data, "tool_input.nonexistent.deep")
	assert.Nil(t, result)
}

func TestResolveFieldPath_MissingTopLevel(t *testing.T) {
	data := map[string]interface{}{
		"other": "value",
	}
	result := ResolveFieldPath(data, "missing_key")
	assert.Nil(t, result)
}

func TestResolveFieldPath_NilData(t *testing.T) {
	result := ResolveFieldPath(nil, "tool_input.command")
	assert.Nil(t, result)
}

func TestResolveFieldPath_EmptyPath(t *testing.T) {
	data := map[string]interface{}{"key": "val"}
	result := ResolveFieldPath(data, "")
	assert.Nil(t, result)
}

func TestResolveFieldPath_NumericValue(t *testing.T) {
	data := map[string]interface{}{
		"count": 42,
	}
	result := ResolveFieldPath(data, "count")
	require.NotNil(t, result)
	assert.Equal(t, "42", *result)
}

func TestResolveFieldPath_BooleanValue(t *testing.T) {
	data := map[string]interface{}{
		"enabled": true,
	}
	result := ResolveFieldPath(data, "enabled")
	require.NotNil(t, result)
	assert.Equal(t, "true", *result)
}

func TestResolveFieldPath_NilValue(t *testing.T) {
	data := map[string]interface{}{
		"nothing": nil,
	}
	result := ResolveFieldPath(data, "nothing")
	assert.Nil(t, result)
}

// =============================================================================
// EvaluateCondition tests — 6 operators
// =============================================================================

func TestEvaluateCondition_Contains(t *testing.T) {
	data := map[string]interface{}{
		"command": "git push --force origin main",
	}
	result := EvaluateCondition(`command contains "force"`, data)
	require.NotNil(t, result)
	assert.True(t, *result)
}

func TestEvaluateCondition_ContainsCaseInsensitive(t *testing.T) {
	data := map[string]interface{}{
		"command": "git push --FORCE origin main",
	}
	result := EvaluateCondition(`command contains "force"`, data)
	require.NotNil(t, result)
	assert.True(t, *result, "contains should be case-insensitive")
}

func TestEvaluateCondition_ContainsNoMatch(t *testing.T) {
	data := map[string]interface{}{
		"command": "git pull",
	}
	result := EvaluateCondition(`command contains "force"`, data)
	require.NotNil(t, result)
	assert.False(t, *result)
}

func TestEvaluateCondition_StartsWith(t *testing.T) {
	data := map[string]interface{}{
		"path": "/etc/passwd",
	}
	result := EvaluateCondition(`path starts_with "/etc"`, data)
	require.NotNil(t, result)
	assert.True(t, *result)
}

func TestEvaluateCondition_StartsWithCaseInsensitive(t *testing.T) {
	data := map[string]interface{}{
		"path": "/ETC/passwd",
	}
	result := EvaluateCondition(`path starts_with "/etc"`, data)
	require.NotNil(t, result)
	assert.True(t, *result, "starts_with should be case-insensitive")
}

func TestEvaluateCondition_StartsWithNoMatch(t *testing.T) {
	data := map[string]interface{}{
		"path": "/home/user",
	}
	result := EvaluateCondition(`path starts_with "/etc"`, data)
	require.NotNil(t, result)
	assert.False(t, *result)
}

func TestEvaluateCondition_EndsWith(t *testing.T) {
	data := map[string]interface{}{
		"file": "config.env",
	}
	result := EvaluateCondition(`file ends_with ".env"`, data)
	require.NotNil(t, result)
	assert.True(t, *result)
}

func TestEvaluateCondition_EndsWithCaseInsensitive(t *testing.T) {
	data := map[string]interface{}{
		"file": "CONFIG.ENV",
	}
	result := EvaluateCondition(`file ends_with ".env"`, data)
	require.NotNil(t, result)
	assert.True(t, *result, "ends_with should be case-insensitive")
}

func TestEvaluateCondition_EndsWithNoMatch(t *testing.T) {
	data := map[string]interface{}{
		"file": "config.yaml",
	}
	result := EvaluateCondition(`file ends_with ".env"`, data)
	require.NotNil(t, result)
	assert.False(t, *result)
}

func TestEvaluateCondition_EndsWithMidstringNoMatch(t *testing.T) {
	// Distinguishes ends_with from contains: ".env" appears in the middle but not at the end
	data := map[string]interface{}{
		"file": "config.env.bak",
	}
	result := EvaluateCondition(`file ends_with ".env"`, data)
	require.NotNil(t, result)
	assert.False(t, *result, "ends_with must not match when substring appears in the middle")
}

func TestEvaluateCondition_DoubleEquals(t *testing.T) {
	data := map[string]interface{}{
		"mode": "production",
	}
	result := EvaluateCondition(`mode == "production"`, data)
	require.NotNil(t, result)
	assert.True(t, *result)
}

func TestEvaluateCondition_DoubleEqualsCaseInsensitive(t *testing.T) {
	data := map[string]interface{}{
		"mode": "PRODUCTION",
	}
	result := EvaluateCondition(`mode == "production"`, data)
	require.NotNil(t, result)
	assert.True(t, *result, "== should be case-insensitive")
}

func TestEvaluateCondition_DoubleEqualsNoMatch(t *testing.T) {
	data := map[string]interface{}{
		"mode": "staging",
	}
	result := EvaluateCondition(`mode == "production"`, data)
	require.NotNil(t, result)
	assert.False(t, *result)
}

func TestEvaluateCondition_Equals(t *testing.T) {
	data := map[string]interface{}{
		"env": "staging",
	}
	result := EvaluateCondition(`env equals "staging"`, data)
	require.NotNil(t, result)
	assert.True(t, *result)
}

func TestEvaluateCondition_EqualsCaseInsensitive(t *testing.T) {
	data := map[string]interface{}{
		"env": "STAGING",
	}
	result := EvaluateCondition(`env equals "staging"`, data)
	require.NotNil(t, result)
	assert.True(t, *result, "equals should be case-insensitive")
}

func TestEvaluateCondition_MatchesRegex(t *testing.T) {
	data := map[string]interface{}{
		"command": "rm -rf /tmp",
	}
	result := EvaluateCondition(`command matches "rm\s+-rf"`, data)
	require.NotNil(t, result)
	assert.True(t, *result)
}

func TestEvaluateCondition_MatchesCaseSensitive(t *testing.T) {
	data := map[string]interface{}{
		"command": "RM -RF /tmp",
	}
	// matches is case-SENSITIVE — uppercase should NOT match lowercase regex
	result := EvaluateCondition(`command matches "rm\s+-rf"`, data)
	require.NotNil(t, result)
	assert.False(t, *result, "matches should be case-sensitive")
}

func TestEvaluateCondition_MatchesRegexNoMatch(t *testing.T) {
	data := map[string]interface{}{
		"command": "ls -la",
	}
	result := EvaluateCondition(`command matches "rm\s+-rf"`, data)
	require.NotNil(t, result)
	assert.False(t, *result)
}

func TestEvaluateCondition_MatchesInvalidRegex(t *testing.T) {
	data := map[string]interface{}{
		"command": "test",
	}
	// Invalid regex — should return false (fail-open), not panic
	result := EvaluateCondition(`command matches "["`, data)
	require.NotNil(t, result)
	assert.False(t, *result, "invalid regex should fail-open to false")
}

func TestEvaluateCondition_MissingField(t *testing.T) {
	data := map[string]interface{}{
		"other": "value",
	}
	result := EvaluateCondition(`command contains "force"`, data)
	require.NotNil(t, result)
	assert.False(t, *result, "missing field should return false")
}

func TestEvaluateCondition_MalformedExpression(t *testing.T) {
	data := map[string]interface{}{
		"command": "test",
	}
	result := EvaluateCondition("this is not valid", data)
	assert.Nil(t, result, "malformed expression should return nil")
}

func TestEvaluateCondition_NestedFieldPath(t *testing.T) {
	data := map[string]interface{}{
		"tool_input": map[string]interface{}{
			"command": "git push --force",
		},
	}
	result := EvaluateCondition(`tool_input.command contains "force"`, data)
	require.NotNil(t, result)
	assert.True(t, *result)
}

func TestEvaluateCondition_NilData(t *testing.T) {
	result := EvaluateCondition(`command contains "test"`, nil)
	require.NotNil(t, result)
	assert.False(t, *result, "nil data should return false")
}

// =============================================================================
// matchToolName / Glob tests
// =============================================================================

func TestMatchToolName_ExactMatch(t *testing.T) {
	assert.True(t, matchToolName("Bash", "Bash"))
	assert.False(t, matchToolName("Bash", "bash"))
	assert.False(t, matchToolName("Bash", "Write"))
}

func TestMatchToolName_GlobWildcard(t *testing.T) {
	assert.True(t, matchToolName("mcp__gmail__send_email", "mcp__gmail__*"))
	assert.True(t, matchToolName("mcp__gmail__read_email", "mcp__gmail__*"))
	assert.False(t, matchToolName("mcp__slack__send_message", "mcp__gmail__*"))
}

func TestMatchToolName_GlobQuestion(t *testing.T) {
	assert.True(t, matchToolName("Bas_", "Bas?"))
	assert.True(t, matchToolName("Bash", "Bas?"))
	assert.False(t, matchToolName("Ba", "Bas?"))
}

func TestMatchToolName_GlobBraceExpansion(t *testing.T) {
	assert.True(t, matchToolName("Read", "{Read,Write}"))
	assert.True(t, matchToolName("Write", "{Read,Write}"))
	assert.False(t, matchToolName("Bash", "{Read,Write}"))
}

func TestMatchToolName_GlobCharClass(t *testing.T) {
	assert.True(t, matchToolName("Ba1h", "Ba[0-9]h"))
	assert.False(t, matchToolName("Bash", "Ba[0-9]h"))
}

func TestMatchToolName_WildcardMatchesAll(t *testing.T) {
	assert.True(t, matchToolName("Bash", "*"))
	assert.True(t, matchToolName("anything", "*"))
}

func TestMatchToolName_NoGlobCharsNoMatch(t *testing.T) {
	// No glob chars, not exact match → false
	assert.False(t, matchToolName("Bash", "NotBash"))
}

func TestMatchToolName_InvalidGlobPattern(t *testing.T) {
	// Invalid glob pattern — should return false (fail-open), not panic
	assert.False(t, matchToolName("Bash", "[invalid"))
}

// =============================================================================
// Evaluate (guard engine) tests
// =============================================================================

func TestEvaluate_ExactMatch(t *testing.T) {
	rules := []GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "no bash allowed"},
	}
	result := Evaluate("Bash", nil, rules)
	assert.Equal(t, "block", result.Action)
	assert.Equal(t, "no bash allowed", result.Reason)
	require.NotNil(t, result.MatchedRule)
	assert.Equal(t, "Bash", result.MatchedRule.Match)
}

func TestEvaluate_GlobMatch(t *testing.T) {
	rules := []GuardRuleConfig{
		{Match: "mcp__gmail__*", Action: "confirm", Reason: "gmail access"},
	}
	result := Evaluate("mcp__gmail__send_email", map[string]interface{}{}, rules)
	assert.Equal(t, "confirm", result.Action)
	assert.Equal(t, "gmail access", result.Reason)
}

func TestEvaluate_NoMatchReturnsAllow(t *testing.T) {
	rules := []GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "no bash"},
	}
	result := Evaluate("Read", nil, rules)
	assert.Equal(t, "allow", result.Action)
	assert.Empty(t, result.Reason)
	assert.Nil(t, result.MatchedRule)
}

func TestEvaluate_EmptyRulesReturnsAllow(t *testing.T) {
	result := Evaluate("Bash", nil, nil)
	assert.Equal(t, "allow", result.Action)
}

func TestEvaluate_EmptyToolName(t *testing.T) {
	rules := []GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "no bash"},
	}
	result := Evaluate("", nil, rules)
	assert.Equal(t, "allow", result.Action)
}

func TestEvaluate_FirstMatchWins_GlobBeforeExact(t *testing.T) {
	// Broad glob rule first, specific exact match second — glob should win
	rules := []GuardRuleConfig{
		{Match: "*", Action: "warn", Reason: "glob catches all"},
		{Match: "Bash", Action: "block", Reason: "specific bash block"},
	}
	result := Evaluate("Bash", nil, rules)
	assert.Equal(t, "warn", result.Action, "first-match-wins: glob before exact")
	assert.Equal(t, "glob catches all", result.Reason)
}

func TestEvaluate_FirstMatchWins_ExactBeforeGlob(t *testing.T) {
	// Exact match first, broad glob second — exact should win
	rules := []GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "specific bash block"},
		{Match: "*", Action: "warn", Reason: "glob catches all"},
	}
	result := Evaluate("Bash", nil, rules)
	assert.Equal(t, "block", result.Action, "first-match-wins: exact before glob")
	assert.Equal(t, "specific bash block", result.Reason)
}

func TestEvaluate_WhenConditionTrue(t *testing.T) {
	rules := []GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "no force push",
			When:   `command contains "force"`,
		},
	}
	payload := map[string]interface{}{
		"command": "git push --force",
	}
	result := Evaluate("Bash", payload, rules)
	assert.Equal(t, "block", result.Action)
}

func TestEvaluate_WhenConditionFalse(t *testing.T) {
	rules := []GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "no force push",
			When:   `command contains "force"`,
		},
	}
	payload := map[string]interface{}{
		"command": "git pull",
	}
	result := Evaluate("Bash", payload, rules)
	assert.Equal(t, "allow", result.Action, "when condition false should skip rule")
}

func TestEvaluate_UnlessConditionTrue(t *testing.T) {
	rules := []GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "bash blocked",
			Unless: `command starts_with "ls"`,
		},
	}
	payload := map[string]interface{}{
		"command": "ls -la",
	}
	result := Evaluate("Bash", payload, rules)
	assert.Equal(t, "allow", result.Action, "unless condition true should skip rule")
}

func TestEvaluate_UnlessConditionFalse(t *testing.T) {
	rules := []GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "bash blocked",
			Unless: `command starts_with "ls"`,
		},
	}
	payload := map[string]interface{}{
		"command": "rm -rf /",
	}
	result := Evaluate("Bash", payload, rules)
	assert.Equal(t, "block", result.Action, "unless condition false should apply rule")
}

func TestEvaluate_WhenAndUnlessBothPresent_BothSatisfied(t *testing.T) {
	// when=true AND unless=true → unless skips the rule
	rules := []GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "blocked",
			When:   `command contains "push"`,
			Unless: `command contains "dry-run"`,
		},
	}
	payload := map[string]interface{}{
		"command": "git push --dry-run",
	}
	result := Evaluate("Bash", payload, rules)
	assert.Equal(t, "allow", result.Action, "unless=true should override when=true")
}

func TestEvaluate_WhenAndUnlessBothPresent_WhenTrue_UnlessFalse(t *testing.T) {
	// when=true AND unless=false → rule applies
	rules := []GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "blocked",
			When:   `command contains "push"`,
			Unless: `command contains "dry-run"`,
		},
	}
	payload := map[string]interface{}{
		"command": "git push --force",
	}
	result := Evaluate("Bash", payload, rules)
	assert.Equal(t, "block", result.Action, "when=true, unless=false → rule applies")
}

func TestEvaluate_NilPayloadNoConditions(t *testing.T) {
	rules := []GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "no bash"},
	}
	result := Evaluate("Bash", nil, rules)
	assert.Equal(t, "block", result.Action)
}

func TestEvaluate_NilPayloadWithWhenCondition(t *testing.T) {
	// When condition with nil payload → field not found → condition false → skip
	rules := []GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "blocked",
			When:   `command contains "force"`,
		},
	}
	result := Evaluate("Bash", nil, rules)
	assert.Equal(t, "allow", result.Action, "nil payload with when condition should skip rule")
}

func TestEvaluate_MultipleRulesSecondMatches(t *testing.T) {
	rules := []GuardRuleConfig{
		{Match: "Write", Action: "warn", Reason: "careful with writes"},
		{Match: "Bash", Action: "block", Reason: "no bash"},
	}
	result := Evaluate("Bash", nil, rules)
	assert.Equal(t, "block", result.Action)
	assert.Equal(t, "no bash", result.Reason)
}

func TestEvaluate_MatchedRuleCopy(t *testing.T) {
	// Ensure the matched rule is a copy, not a reference to the slice element
	rules := []GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "original"},
	}
	result := Evaluate("Bash", nil, rules)
	require.NotNil(t, result.MatchedRule)
	rules[0].Reason = "modified"
	assert.Equal(t, "original", result.MatchedRule.Reason, "matched rule should be a copy")
}

func TestEvaluate_WarnAction(t *testing.T) {
	rules := []GuardRuleConfig{
		{Match: "Write", Action: "warn", Reason: "be careful"},
	}
	result := Evaluate("Write", nil, rules)
	assert.Equal(t, "warn", result.Action)
}

func TestEvaluate_ConfirmAction(t *testing.T) {
	rules := []GuardRuleConfig{
		{Match: "mcp__gmail__*", Action: "confirm", Reason: "confirm gmail"},
	}
	result := Evaluate("mcp__gmail__send_email", nil, rules)
	assert.Equal(t, "confirm", result.Action)
}

func TestEvaluate_MalformedWhenCondition(t *testing.T) {
	// Malformed when condition → EvaluateCondition returns nil → skip rule (fail-open)
	rules := []GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "blocked",
			When:   "this is not valid",
		},
	}
	result := Evaluate("Bash", map[string]interface{}{"command": "test"}, rules)
	assert.Equal(t, "allow", result.Action, "malformed when should skip rule")
}

func TestEvaluate_MalformedUnlessCondition(t *testing.T) {
	// Malformed unless condition → EvaluateCondition returns nil → unless is not true → rule applies
	rules := []GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "blocked",
			Unless: "this is not valid",
		},
	}
	result := Evaluate("Bash", map[string]interface{}{"command": "test"}, rules)
	assert.Equal(t, "block", result.Action, "malformed unless should let rule apply")
}

// =============================================================================
// Edge cases & integration scenarios
// =============================================================================

func TestEvaluate_GlobShadowsSpecificExact(t *testing.T) {
	// This is the canonical first-match-wins test from the spec:
	// broad glob before specific exact match — glob wins
	rules := []GuardRuleConfig{
		{Match: "mcp__*", Action: "confirm", Reason: "confirm all MCP"},
		{Match: "mcp__gmail__send_email", Action: "block", Reason: "block gmail send"},
	}
	result := Evaluate("mcp__gmail__send_email", nil, rules)
	assert.Equal(t, "confirm", result.Action, "glob should shadow exact match")
	assert.Equal(t, "confirm all MCP", result.Reason)
}

func TestEvaluate_ComplexScenario_FirewallStyle(t *testing.T) {
	// Realistic firewall-style rules
	rules := []GuardRuleConfig{
		// Rule 0: block rm -rf
		{
			Match:  "Bash",
			Action: "block",
			Reason: "destructive command",
			When:   `command contains "rm -rf"`,
		},
		// Rule 1: warn on all bash
		{
			Match:  "Bash",
			Action: "warn",
			Reason: "bash usage",
		},
		// Rule 2: confirm all MCP
		{
			Match:  "mcp__*",
			Action: "confirm",
			Reason: "external tool",
		},
	}

	// Test rm -rf → block (rule 0)
	r1 := Evaluate("Bash", map[string]interface{}{"command": "rm -rf /tmp"}, rules)
	assert.Equal(t, "block", r1.Action, "rm -rf should be blocked")

	// Test safe bash → warn (rule 1, rule 0 skipped due to when)
	r2 := Evaluate("Bash", map[string]interface{}{"command": "ls -la"}, rules)
	assert.Equal(t, "warn", r2.Action, "safe bash should warn")

	// Test MCP tool → confirm (rule 2)
	r3 := Evaluate("mcp__slack__send_message", nil, rules)
	assert.Equal(t, "confirm", r3.Action, "MCP tool should need confirm")

	// Test unknown tool → allow
	r4 := Evaluate("Read", nil, rules)
	assert.Equal(t, "allow", r4.Action, "unknown tool should be allowed")
}

func TestEvaluate_EmptyPayloadMap(t *testing.T) {
	rules := []GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "blocked"},
	}
	result := Evaluate("Bash", map[string]interface{}{}, rules)
	assert.Equal(t, "block", result.Action)
}

func TestEvaluateCondition_EqualsAndDoubleEqualsAreSynonyms(t *testing.T) {
	data := map[string]interface{}{
		"mode": "production",
	}
	r1 := EvaluateCondition(`mode == "production"`, data)
	r2 := EvaluateCondition(`mode equals "production"`, data)
	require.NotNil(t, r1)
	require.NotNil(t, r2)
	assert.Equal(t, *r1, *r2, "== and equals should produce identical results")
}

func TestHasGlobChars(t *testing.T) {
	assert.True(t, hasGlobChars("mcp__*"))
	assert.True(t, hasGlobChars("Bas?"))
	assert.True(t, hasGlobChars("[abc]"))
	assert.True(t, hasGlobChars("{Read,Write}"))
	assert.False(t, hasGlobChars("Bash"))
	assert.False(t, hasGlobChars("mcp__gmail__send_email"))
}
