package hwtesting

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// GuardTester — NewGuardTesterFromRules
// =============================================================================

func TestNewGuardTesterFromRules_Basic(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{Match: "Bash", Action: "block", Reason: "no bash allowed"},
	})
	require.NotNil(t, gt)
	assert.Equal(t, 1, gt.Rules())
}

func TestNewGuardTesterFromRules_Empty(t *testing.T) {
	gt := NewGuardTesterFromRules(nil)
	require.NotNil(t, gt)
	assert.Equal(t, 0, gt.Rules())
}

// =============================================================================
// GuardTester — Evaluate
// =============================================================================

func TestEvaluate_ExactMatch_Block(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{Match: "Bash", Action: "block", Reason: "no bash allowed"},
	})

	result := gt.Evaluate("Bash", nil)
	assert.Equal(t, "block", result.Action)
	assert.Equal(t, "no bash allowed", result.Reason)
	assert.Equal(t, "Bash", result.RuleName)
	assert.True(t, result.Passed, "Passed should be true for standalone Evaluate")
}

func TestEvaluate_NoMatch_Allow(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{Match: "Bash", Action: "block", Reason: "no bash"},
	})

	result := gt.Evaluate("Read", nil)
	assert.Equal(t, "allow", result.Action)
	assert.Empty(t, result.Reason)
	assert.Empty(t, result.RuleName)
	assert.True(t, result.Passed)
}

func TestEvaluate_GlobMatch(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{Match: "mcp__gmail__*", Action: "confirm", Reason: "gmail access requires confirmation"},
	})

	result := gt.Evaluate("mcp__gmail__send_email", nil)
	assert.Equal(t, "confirm", result.Action)
	assert.Equal(t, "gmail access requires confirmation", result.Reason)
	assert.Equal(t, "mcp__gmail__*", result.RuleName)
}

func TestEvaluate_WhenCondition_Matches(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "no force push",
			When:   `command contains "force"`,
		},
	})

	payload := map[string]interface{}{
		"command": "git push --force origin main",
	}
	result := gt.Evaluate("Bash", payload)
	assert.Equal(t, "block", result.Action)
}

func TestEvaluate_WhenCondition_NoMatch(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "no force push",
			When:   `command contains "force"`,
		},
	})

	payload := map[string]interface{}{
		"command": "git pull origin main",
	}
	result := gt.Evaluate("Bash", payload)
	assert.Equal(t, "allow", result.Action, "when condition not met should allow")
}

func TestEvaluate_UnlessCondition_Skips(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "bash blocked",
			Unless: `command starts_with "ls"`,
		},
	})

	payload := map[string]interface{}{
		"command": "ls -la",
	}
	result := gt.Evaluate("Bash", payload)
	assert.Equal(t, "allow", result.Action, "unless condition true should skip rule")
}

func TestEvaluate_UnlessCondition_Applies(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "bash blocked",
			Unless: `command starts_with "ls"`,
		},
	})

	payload := map[string]interface{}{
		"command": "rm -rf /",
	}
	result := gt.Evaluate("Bash", payload)
	assert.Equal(t, "block", result.Action, "unless condition false should apply rule")
}

func TestEvaluate_WarnAction(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{Match: "Write", Action: "warn", Reason: "be careful with writes"},
	})

	result := gt.Evaluate("Write", nil)
	assert.Equal(t, "warn", result.Action)
	assert.Equal(t, "be careful with writes", result.Reason)
}

func TestEvaluate_FirstMatchWins(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{Match: "*", Action: "warn", Reason: "catch all"},
		{Match: "Bash", Action: "block", Reason: "specific block"},
	})

	result := gt.Evaluate("Bash", nil)
	assert.Equal(t, "warn", result.Action, "first-match-wins: glob before exact")
	assert.Equal(t, "catch all", result.Reason)
}

func TestEvaluate_EmptyRules_Allow(t *testing.T) {
	gt := NewGuardTesterFromRules(nil)

	result := gt.Evaluate("Bash", nil)
	assert.Equal(t, "allow", result.Action)
}

// =============================================================================
// GuardTester — EvaluateAll
// =============================================================================

func TestEvaluateAll_AllPass(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{Match: "Bash", Action: "block", Reason: "no bash"},
		{Match: "mcp__gmail__*", Action: "confirm", Reason: "gmail needs confirm"},
	})

	scenarios := []TestScenario{
		{Name: "bash blocked", ToolName: "Bash", Expected: "block"},
		{Name: "gmail confirm", ToolName: "mcp__gmail__send_email", Expected: "confirm"},
		{Name: "read allowed", ToolName: "Read", Expected: "allow"},
	}

	results := gt.EvaluateAll(scenarios)
	require.Len(t, results, 3)

	assert.True(t, results[0].Passed, "scenario %q should pass", scenarios[0].Name)
	assert.Equal(t, "block", results[0].Action)
	assert.Equal(t, "no bash", results[0].Reason)

	assert.True(t, results[1].Passed, "scenario %q should pass", scenarios[1].Name)
	assert.Equal(t, "confirm", results[1].Action)

	assert.True(t, results[2].Passed, "scenario %q should pass", scenarios[2].Name)
	assert.Equal(t, "allow", results[2].Action)
}

func TestEvaluateAll_SomeFail(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{Match: "Bash", Action: "block", Reason: "no bash"},
	})

	scenarios := []TestScenario{
		{Name: "correct expectation", ToolName: "Bash", Expected: "block"},
		{Name: "wrong expectation", ToolName: "Bash", Expected: "allow"}, // will fail
		{Name: "correct allow", ToolName: "Read", Expected: "allow"},
	}

	results := gt.EvaluateAll(scenarios)
	require.Len(t, results, 3)

	assert.True(t, results[0].Passed)
	assert.False(t, results[1].Passed, "expecting allow but got block should fail")
	assert.True(t, results[2].Passed)
}

func TestEvaluateAll_WithPayload(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "destructive command",
			When:   `command contains "rm -rf"`,
		},
		{
			Match:  "Bash",
			Action: "warn",
			Reason: "bash usage",
		},
	})

	scenarios := []TestScenario{
		{
			Name:     "destructive blocked",
			ToolName: "Bash",
			Payload:  map[string]interface{}{"command": "rm -rf /tmp"},
			Expected: "block",
		},
		{
			Name:     "safe bash warns",
			ToolName: "Bash",
			Payload:  map[string]interface{}{"command": "ls -la"},
			Expected: "warn",
		},
		{
			Name:     "other tool allowed",
			ToolName: "Read",
			Expected: "allow",
		},
	}

	results := gt.EvaluateAll(scenarios)
	require.Len(t, results, 3)

	for i, r := range results {
		assert.True(t, r.Passed, "scenario %q should pass (got %s, expected %s)",
			scenarios[i].Name, r.Action, scenarios[i].Expected)
	}
}

func TestEvaluateAll_Empty(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{Match: "Bash", Action: "block", Reason: "no bash"},
	})

	results := gt.EvaluateAll(nil)
	assert.Len(t, results, 0)
}

// =============================================================================
// GuardTester — NewGuardTester (file-based)
// =============================================================================

func TestNewGuardTester_FromFile(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "hookwise.yaml")

	configContent := `version: 1
guards:
  - match: "Bash"
    action: block
    reason: "no bash"
  - match: "mcp__*"
    action: confirm
    reason: "external tool"
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	gt, err := NewGuardTester(configPath)
	require.NoError(t, err)
	require.NotNil(t, gt)
	assert.Equal(t, 2, gt.Rules())

	// Verify rules work
	result := gt.Evaluate("Bash", nil)
	assert.Equal(t, "block", result.Action)

	result = gt.Evaluate("mcp__slack__send", nil)
	assert.Equal(t, "confirm", result.Action)
}

func TestNewGuardTester_FromDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "hookwise.yaml")

	configContent := `version: 1
guards:
  - match: "Write"
    action: warn
    reason: "careful"
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	// Pass directory instead of file
	gt, err := NewGuardTester(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, gt)
	assert.Equal(t, 1, gt.Rules())
}

func TestNewGuardTester_FileNotFound(t *testing.T) {
	gt, err := NewGuardTester("/nonexistent/path/hookwise.yaml")
	assert.Error(t, err)
	assert.Nil(t, gt)
}

func TestNewGuardTester_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "hookwise.yaml")

	err := os.WriteFile(configPath, []byte("{{{{invalid yaml"), 0o644)
	require.NoError(t, err)

	gt, err := NewGuardTester(configPath)
	assert.Error(t, err)
	assert.Nil(t, gt)
}

func TestNewGuardTester_NoGuards(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "hookwise.yaml")

	configContent := `version: 1
analytics:
  enabled: true
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	gt, err := NewGuardTester(configPath)
	require.NoError(t, err)
	require.NotNil(t, gt)
	assert.Equal(t, 0, gt.Rules())

	// With no guards, everything is allowed
	result := gt.Evaluate("Bash", nil)
	assert.Equal(t, "allow", result.Action)
}

// =============================================================================
// HookResult — Assertion Methods
// =============================================================================

func TestHookResult_IsAllowed_EmptyStdout(t *testing.T) {
	r := &HookResult{Stdout: "", ExitCode: 0}
	assert.True(t, r.IsAllowed())
	assert.False(t, r.IsBlocked())
}

func TestHookResult_IsAllowed_NonJSON(t *testing.T) {
	r := &HookResult{Stdout: "some non-json output", ExitCode: 0}
	assert.True(t, r.IsAllowed())
	assert.False(t, r.IsBlocked())
}

func TestHookResult_IsBlocked_PermissionDeny(t *testing.T) {
	stdout := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"no bash allowed"}}`
	r := &HookResult{Stdout: stdout, ExitCode: 0}
	assert.True(t, r.IsBlocked())
	assert.False(t, r.IsAllowed())
}

func TestHookResult_IsBlocked_HandlerBlock(t *testing.T) {
	stdout := `{"decision":"block","reason":"Blocked by guard rule"}`
	r := &HookResult{Stdout: stdout, ExitCode: 0}
	assert.True(t, r.IsBlocked())
	assert.False(t, r.IsAllowed())
}

func TestHookResult_IsAllowed_PermissionAsk(t *testing.T) {
	// "ask" is not "deny" — it's allowed (it's a confirm prompt)
	stdout := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"confirm gmail"}}`
	r := &HookResult{Stdout: stdout, ExitCode: 0}
	assert.True(t, r.IsAllowed())
	assert.False(t, r.IsBlocked())
}

func TestHookResult_IsAllowed_ContextOnly(t *testing.T) {
	stdout := `{"hookSpecificOutput":{"additionalContext":"Guard warning: be careful"}}`
	r := &HookResult{Stdout: stdout, ExitCode: 0}
	assert.True(t, r.IsAllowed())
	assert.False(t, r.IsBlocked())
}

func TestHookResult_HasContext_Found(t *testing.T) {
	stdout := `{"hookSpecificOutput":{"additionalContext":"Guard warning: be careful with writes"}}`
	r := &HookResult{Stdout: stdout, ExitCode: 0}
	assert.True(t, r.HasContext("be careful"))
	assert.True(t, r.HasContext("Guard warning"))
	assert.False(t, r.HasContext("not present"))
}

func TestHookResult_HasContext_EmptyStdout(t *testing.T) {
	r := &HookResult{Stdout: "", ExitCode: 0}
	assert.False(t, r.HasContext("anything"))
}

func TestHookResult_HasContext_TopLevel(t *testing.T) {
	stdout := `{"additionalContext":"some context here"}`
	r := &HookResult{Stdout: stdout, ExitCode: 0}
	assert.True(t, r.HasContext("some context"))
}

func TestHookResult_HasContext_NoContextField(t *testing.T) {
	stdout := `{"hookSpecificOutput":{"permissionDecision":"deny"}}`
	r := &HookResult{Stdout: stdout, ExitCode: 0}
	assert.False(t, r.HasContext("anything"))
}

func TestHookResult_HasDecisionReason_PermissionDecision(t *testing.T) {
	stdout := `{"hookSpecificOutput":{"permissionDecision":"deny","permissionDecisionReason":"no bash allowed"}}`
	r := &HookResult{Stdout: stdout, ExitCode: 0}
	assert.True(t, r.HasDecisionReason("no bash"))
	assert.False(t, r.HasDecisionReason("something else"))
}

func TestHookResult_HasDecisionReason_TopLevelReason(t *testing.T) {
	stdout := `{"decision":"block","reason":"Blocked by guard rule"}`
	r := &HookResult{Stdout: stdout, ExitCode: 0}
	assert.True(t, r.HasDecisionReason("Blocked by guard"))
}

func TestHookResult_HasDecisionReason_Empty(t *testing.T) {
	r := &HookResult{Stdout: "", ExitCode: 0}
	assert.False(t, r.HasDecisionReason("anything"))
}

// =============================================================================
// HookRunner — Construction
// =============================================================================

func TestNewHookRunner(t *testing.T) {
	hr := NewHookRunner("/usr/local/bin/hookwise")
	require.NotNil(t, hr)
	assert.Equal(t, "/usr/local/bin/hookwise", hr.binaryPath)
	assert.Empty(t, hr.configDir)
}

func TestHookRunner_WithConfigDir(t *testing.T) {
	hr := NewHookRunner("/usr/local/bin/hookwise").WithConfigDir("/tmp/myproject")
	assert.Equal(t, "/tmp/myproject", hr.configDir)
}

// =============================================================================
// HookRunner — Run (with non-existent binary)
// =============================================================================

func TestHookRunner_Run_BinaryNotFound(t *testing.T) {
	hr := NewHookRunner("/nonexistent/hookwise")
	result := hr.Run("PreToolUse", map[string]interface{}{
		"session_id": "test",
		"tool_name":  "Bash",
	})

	require.NotNil(t, result)
	assert.Equal(t, -1, result.ExitCode)
	assert.Contains(t, result.Stderr, "hwtesting: failed to run binary")
}

func TestHookRunner_Run_NilPayload(t *testing.T) {
	hr := NewHookRunner("/nonexistent/hookwise")
	result := hr.Run("PreToolUse", nil)

	require.NotNil(t, result)
	// Binary not found, but nil payload handling should not crash
	assert.Equal(t, -1, result.ExitCode)
}

// =============================================================================
// Integration: GuardTester with realistic firewall-style rules
// =============================================================================

func TestGuardTester_FirewallScenarios(t *testing.T) {
	gt := NewGuardTesterFromRules([]GuardRule{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "destructive command",
			When:   `command contains "rm -rf"`,
		},
		{
			Match:  "Bash",
			Action: "block",
			Reason: "no force push",
			When:   `command contains "force"`,
		},
		{
			Match:  "Bash",
			Action: "warn",
			Reason: "bash usage",
			Unless: `command starts_with "ls"`,
		},
		{
			Match:  "mcp__*",
			Action: "confirm",
			Reason: "external tool access",
		},
	})

	scenarios := []TestScenario{
		{
			Name:     "rm -rf blocked",
			ToolName: "Bash",
			Payload:  map[string]interface{}{"command": "rm -rf /tmp"},
			Expected: "block",
		},
		{
			Name:     "force push blocked",
			ToolName: "Bash",
			Payload:  map[string]interface{}{"command": "git push --force"},
			Expected: "block",
		},
		{
			Name:     "safe bash warns",
			ToolName: "Bash",
			Payload:  map[string]interface{}{"command": "cat /etc/passwd"},
			Expected: "warn",
		},
		{
			Name:     "ls exempted from warn",
			ToolName: "Bash",
			Payload:  map[string]interface{}{"command": "ls -la /home"},
			Expected: "allow",
		},
		{
			Name:     "gmail confirm",
			ToolName: "mcp__gmail__send_email",
			Expected: "confirm",
		},
		{
			Name:     "slack confirm",
			ToolName: "mcp__slack__send_message",
			Expected: "confirm",
		},
		{
			Name:     "Read allowed",
			ToolName: "Read",
			Expected: "allow",
		},
		{
			Name:     "Write allowed (no rule)",
			ToolName: "Write",
			Expected: "allow",
		},
	}

	results := gt.EvaluateAll(scenarios)
	require.Len(t, results, len(scenarios))

	for i, r := range results {
		assert.True(t, r.Passed, "scenario %q: expected %s, got %s (reason: %s)",
			scenarios[i].Name, scenarios[i].Expected, r.Action, r.Reason)
	}
}
