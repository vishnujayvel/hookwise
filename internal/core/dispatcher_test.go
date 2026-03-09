package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// =============================================================================
// Helper: create a minimal config with guards
// =============================================================================

func configWithGuards(guards []GuardRuleConfig) HooksConfig {
	cfg := GetDefaultConfig()
	cfg.Guards = guards
	cfg.Analytics.Enabled = false // disable for dispatch tests
	return cfg
}

func configWithHandlers(handlers []CustomHandlerConfig) HooksConfig {
	cfg := GetDefaultConfig()
	cfg.Handlers = handlers
	cfg.Analytics.Enabled = false
	return cfg
}

func emptyConfig() HooksConfig {
	cfg := GetDefaultConfig()
	cfg.Guards = nil
	cfg.Handlers = nil
	cfg.Analytics.Enabled = false
	return cfg
}

func preToolUsePayload(toolName string, toolInput map[string]interface{}) HookPayload {
	return HookPayload{
		SessionID: "test-session-123",
		ToolName:  toolName,
		ToolInput: toolInput,
	}
}

// parseHookOutput parses the hookSpecificOutput JSON from a dispatch result.
func parseHookOutput(t *testing.T, stdout *string) map[string]interface{} {
	t.Helper()
	require.NotNil(t, stdout, "expected non-nil stdout")
	var result map[string]interface{}
	err := json.Unmarshal([]byte(*stdout), &result)
	require.NoError(t, err, "failed to parse stdout JSON: %s", *stdout)
	hookOutput, ok := result["hookSpecificOutput"].(map[string]interface{})
	require.True(t, ok, "expected hookSpecificOutput in output")
	return hookOutput
}

// =============================================================================
// Unit Tests: Dispatch — Guard Block
// =============================================================================

func TestDispatch_PreToolUse_BlockGuard_DenyJSON(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "no bash allowed"},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout)

	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "PreToolUse", hookOutput["hookEventName"])
	assert.Equal(t, "deny", hookOutput["permissionDecision"])
	assert.Equal(t, "no bash allowed", hookOutput["permissionDecisionReason"])
}

func TestDispatch_PreToolUse_BlockGuard_ExactJSONFormat(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "reason"},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	require.NotNil(t, result.Stdout)
	expected := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"reason"}}`
	assert.Equal(t, expected, *result.Stdout)
}

func TestDispatch_PreToolUse_BlockGuard_DefaultReason(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: ""},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	require.NotNil(t, result.Stdout)
	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "deny", hookOutput["permissionDecision"])
	assert.Equal(t, "Blocked by guard rule", hookOutput["permissionDecisionReason"])
}

func TestDispatch_PreToolUse_BlockGuard_GlobMatch(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "mcp__gmail__*", Action: "block", Reason: "no gmail"},
	})
	payload := preToolUsePayload("mcp__gmail__send_email", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	require.NotNil(t, result.Stdout)
	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "deny", hookOutput["permissionDecision"])
}

func TestDispatch_PreToolUse_BlockGuard_WithCondition(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "no force push",
			When:   `tool_input.command contains "force"`,
		},
	})
	payload := preToolUsePayload("Bash", map[string]interface{}{
		"command": "git push --force origin main",
	})

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	require.NotNil(t, result.Stdout)
	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "deny", hookOutput["permissionDecision"])
	assert.Equal(t, "no force push", hookOutput["permissionDecisionReason"])
}

func TestDispatch_PreToolUse_BlockGuard_ConditionFalse_Allow(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "no force push",
			When:   `tool_input.command contains "force"`,
		},
	})
	payload := preToolUsePayload("Bash", map[string]interface{}{
		"command": "git pull",
	})

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	// When condition is false, the guard doesn't match -> allow (no stdout)
	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout)
}

// =============================================================================
// Unit Tests: Dispatch — Guard Confirm
// =============================================================================

func TestDispatch_PreToolUse_ConfirmGuard_AskJSON(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "mcp__gmail__*", Action: "confirm", Reason: "confirm gmail access"},
	})
	payload := preToolUsePayload("mcp__gmail__read_email", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout)

	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "PreToolUse", hookOutput["hookEventName"])
	assert.Equal(t, "ask", hookOutput["permissionDecision"])
	assert.Equal(t, "confirm gmail access", hookOutput["permissionDecisionReason"])
}

func TestDispatch_PreToolUse_ConfirmGuard_ExactJSONFormat(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Write", Action: "confirm", Reason: "confirm write"},
	})
	payload := preToolUsePayload("Write", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	require.NotNil(t, result.Stdout)
	expected := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"confirm write"}}`
	assert.Equal(t, expected, *result.Stdout)
}

func TestDispatch_PreToolUse_ConfirmGuard_DefaultReason(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Write", Action: "confirm", Reason: ""},
	})
	payload := preToolUsePayload("Write", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	require.NotNil(t, result.Stdout)
	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "ask", hookOutput["permissionDecision"])
	assert.Equal(t, "Requires confirmation", hookOutput["permissionDecisionReason"])
}

// =============================================================================
// Unit Tests: Dispatch — Guard Warn (context injection)
// =============================================================================

func TestDispatch_PreToolUse_WarnGuard_AdditionalContext(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "warn", Reason: "bash is dangerous"},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout)

	hookOutput := parseHookOutput(t, result.Stdout)
	ctx, ok := hookOutput["additionalContext"].(string)
	require.True(t, ok, "expected additionalContext string")
	assert.Contains(t, ctx, "bash is dangerous")
}

func TestDispatch_PreToolUse_WarnGuard_DoesNotBlock(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "warn", Reason: "be careful"},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	// Warn should NOT produce a deny/ask decision
	require.NotNil(t, result.Stdout)
	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Nil(t, hookOutput["permissionDecision"])
}

// =============================================================================
// Unit Tests: Dispatch — No Matching Guard
// =============================================================================

func TestDispatch_PreToolUse_NoMatchingGuard_Allow(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Write", Action: "block", Reason: "no writes"},
	})
	payload := preToolUsePayload("Read", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout, "no guard match should produce no stdout")
}

func TestDispatch_PreToolUse_EmptyGuards_Allow(t *testing.T) {
	config := configWithGuards(nil)
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout)
}

// =============================================================================
// Unit Tests: Dispatch — Non-PreToolUse Events
// =============================================================================

func TestDispatch_PostToolUse_NoGuardEvaluation(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "should not trigger"},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPostToolUse, payload, config)

	// Guards only evaluate on PreToolUse — PostToolUse should not block
	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout)
}

func TestDispatch_SessionStart_NoGuardEvaluation(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "*", Action: "block", Reason: "block everything"},
	})
	payload := HookPayload{SessionID: "test-session"}

	result := Dispatch(context.Background(), EventSessionStart, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout)
}

func TestDispatch_Notification_NoGuardEvaluation(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "*", Action: "block", Reason: "block everything"},
	})
	payload := HookPayload{SessionID: "test-session"}

	result := Dispatch(context.Background(), EventNotification, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout)
}

func TestDispatch_Stop_NoGuardEvaluation(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "*", Action: "block", Reason: "block all"},
	})
	payload := HookPayload{SessionID: "test-session"}

	result := Dispatch(context.Background(), EventStop, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout)
}

// =============================================================================
// Unit Tests: Dispatch — Unrecognized Event Type
// =============================================================================

func TestDispatch_UnrecognizedEventType_ExitZeroNoStdout(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "*", Action: "block", Reason: "block all"},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), "NonExistentEvent", payload, config)

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout, "unrecognized event type should produce zero stdout")
}

func TestDispatch_EmptyEventType_ExitZeroNoStdout(t *testing.T) {
	config := emptyConfig()
	payload := HookPayload{SessionID: "test-session"}

	result := Dispatch(context.Background(), "", payload, config)

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout)
}

// =============================================================================
// Unit Tests: SafeDispatch — Panic Recovery
// =============================================================================

func TestSafeDispatch_PanicRecovery_ExitZero(t *testing.T) {
	result := SafeDispatch(func() DispatchResult {
		panic("something went wrong")
	})

	assert.Equal(t, 0, result.ExitCode, "panic should be recovered and exit 0")
	assert.Nil(t, result.Stdout, "panic recovery should produce no stdout")
}

func TestSafeDispatch_NormalReturn(t *testing.T) {
	stdout := "test output"
	result := SafeDispatch(func() DispatchResult {
		return DispatchResult{Stdout: &stdout, ExitCode: 0}
	})

	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout)
	assert.Equal(t, "test output", *result.Stdout)
}

func TestSafeDispatch_NilStringPanic(t *testing.T) {
	result := SafeDispatch(func() DispatchResult {
		var s *string
		_ = *s // nil pointer dereference
		return DispatchResult{ExitCode: 0}
	})

	assert.Equal(t, 0, result.ExitCode)
}

// =============================================================================
// Unit Tests: Context Injection Merge
// =============================================================================

func TestDispatch_ContextInjection_MergesAdditionalContext(t *testing.T) {
	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:   "context-handler-1",
			Type:   "inline",
			Events: []string{EventSessionStart},
			Phase:  "context",
			Action: map[string]interface{}{
				"additionalContext": "Hello from handler 1",
			},
		},
		{
			Name:   "context-handler-2",
			Type:   "inline",
			Events: []string{EventSessionStart},
			Phase:  "context",
			Action: map[string]interface{}{
				"additionalContext": "Hello from handler 2",
			},
		},
	})
	payload := HookPayload{SessionID: "test-session"}

	result := Dispatch(context.Background(), EventSessionStart, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout)

	hookOutput := parseHookOutput(t, result.Stdout)
	ctx, ok := hookOutput["additionalContext"].(string)
	require.True(t, ok, "expected additionalContext string")
	assert.Contains(t, ctx, "Hello from handler 1")
	assert.Contains(t, ctx, "Hello from handler 2")
}

func TestDispatch_WarnGuard_MergedWithContextHandler(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "warn", Reason: "guard warning"},
	})
	config.Handlers = []CustomHandlerConfig{
		{
			Name:   "context-handler",
			Type:   "inline",
			Events: []string{EventPreToolUse},
			Phase:  "context",
			Action: map[string]interface{}{
				"additionalContext": "handler context",
			},
		},
	}
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	require.NotNil(t, result.Stdout)
	hookOutput := parseHookOutput(t, result.Stdout)
	ctx, ok := hookOutput["additionalContext"].(string)
	require.True(t, ok)
	assert.Contains(t, ctx, "guard warning")
	assert.Contains(t, ctx, "handler context")
}

func TestDispatch_NoContextHandlers_NoContextOutput(t *testing.T) {
	config := emptyConfig()
	payload := HookPayload{SessionID: "test-session"}

	result := Dispatch(context.Background(), EventSessionStart, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout, "no context handlers should produce no stdout")
}

// =============================================================================
// Unit Tests: Side Effects — Non-Blocking
// =============================================================================

func TestDispatch_SideEffects_DontBlockResponse(t *testing.T) {
	// Create an inline handler with phase side_effect
	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:   "side-effect-handler",
			Type:   "inline",
			Events: []string{EventPostToolUse},
			Phase:  "side_effect",
			Action: map[string]interface{}{
				"additionalContext": "side effect context",
			},
		},
	})
	payload := HookPayload{SessionID: "test-session", ToolName: "Bash"}

	start := time.Now()
	result := Dispatch(context.Background(), EventPostToolUse, payload, config)
	elapsed := time.Since(start)

	// Side effects should not block — dispatch should return very quickly
	assert.Equal(t, 0, result.ExitCode)
	// The side effect handler's output should NOT appear in stdout
	// (side effects don't contribute to output)
	assert.Nil(t, result.Stdout, "side effects should not produce stdout")
	assert.Less(t, elapsed, 2*time.Second, "side effects should not block dispatch")
}

func TestFireSideEffectsSync_RunsHandlers(t *testing.T) {
	handlers := []ResolvedHandler{
		{
			Name:        "test-side-effect",
			HandlerType: "inline",
			Events:      []string{EventPostToolUse},
			Phase:       "side_effect",
			Action: map[string]interface{}{
				"additionalContext": "ran",
			},
		},
	}

	// Verify the function completes without panic.
	// FireSideEffectsSync discards return values, so reaching this point
	// without panic is the assertion.
	require.NotPanics(t, func() {
		FireSideEffectsSync(context.Background(), handlers, HookPayload{SessionID: "test"})
	})
}

func TestFireSideEffects_PanicRecovery(t *testing.T) {
	// Test that panicking side-effect handlers don't crash the process
	handlers := []ResolvedHandler{
		{
			Name:        "panicking-handler",
			HandlerType: "unknown_type_that_causes_log",
			Events:      []string{EventPostToolUse},
			Phase:       "side_effect",
		},
	}

	// Should not panic
	FireSideEffectsSync(context.Background(), handlers, HookPayload{SessionID: "test"})
}

// =============================================================================
// Unit Tests: Handler Resolution
// =============================================================================

func TestResolveHandlers_InfersGuardPhase(t *testing.T) {
	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:   "guard-handler",
			Type:   "script",
			Events: []string{EventPreToolUse},
		},
	})

	handlers := ResolveHandlers(config)
	require.Len(t, handlers, 1)
	assert.Equal(t, "guard", handlers[0].Phase, "PreToolUse script handler should infer guard phase")
}

func TestResolveHandlers_InfersContextPhase(t *testing.T) {
	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:   "greeting-handler",
			Type:   "inline",
			Events: []string{EventSessionStart},
		},
	})

	handlers := ResolveHandlers(config)
	require.Len(t, handlers, 1)
	assert.Equal(t, "context", handlers[0].Phase, "SessionStart handler should infer context phase")
}

func TestResolveHandlers_InfersSideEffectPhase(t *testing.T) {
	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:   "analytics-handler",
			Type:   "script",
			Events: []string{EventPostToolUse},
		},
	})

	handlers := ResolveHandlers(config)
	require.Len(t, handlers, 1)
	assert.Equal(t, "side_effect", handlers[0].Phase, "PostToolUse handler should infer side_effect phase")
}

func TestResolveHandlers_ExplicitPhaseOverridesInference(t *testing.T) {
	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:   "custom-handler",
			Type:   "script",
			Events: []string{EventPreToolUse},
			Phase:  "side_effect",
		},
	})

	handlers := ResolveHandlers(config)
	require.Len(t, handlers, 1)
	assert.Equal(t, "side_effect", handlers[0].Phase, "explicit phase should override inference")
}

func TestResolveHandlers_InlinePreToolUse_InfersSideEffect(t *testing.T) {
	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:   "inline-pretooluse",
			Type:   "inline",
			Events: []string{EventPreToolUse},
		},
	})

	handlers := ResolveHandlers(config)
	require.Len(t, handlers, 1)
	// Inline handlers on PreToolUse should NOT be guard phase (per TS logic)
	assert.Equal(t, "side_effect", handlers[0].Phase, "inline PreToolUse should infer side_effect, not guard")
}

func TestResolveHandlers_DefaultTimeout(t *testing.T) {
	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:   "handler-no-timeout",
			Type:   "script",
			Events: []string{EventPostToolUse},
		},
	})

	handlers := ResolveHandlers(config)
	require.Len(t, handlers, 1)
	assert.Equal(t, config.Settings.HandlerTimeoutSeconds*1000, handlers[0].Timeout)
}

func TestResolveHandlers_CustomTimeout(t *testing.T) {
	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:    "handler-custom-timeout",
			Type:    "script",
			Events:  []string{EventPostToolUse},
			Timeout: 5,
		},
	})

	handlers := ResolveHandlers(config)
	require.Len(t, handlers, 1)
	assert.Equal(t, 5000, handlers[0].Timeout, "custom timeout should be converted to milliseconds")
}

func TestGetHandlersForEvent_FiltersByEvent(t *testing.T) {
	handlers := []ResolvedHandler{
		{Name: "h1", Events: []string{EventPreToolUse}, Phase: "guard"},
		{Name: "h2", Events: []string{EventPostToolUse}, Phase: "side_effect"},
		{Name: "h3", Events: []string{EventPreToolUse}, Phase: "context"},
	}

	result := GetHandlersForEvent(handlers, EventPreToolUse)
	assert.Len(t, result, 2)
	// Should be sorted by phase: guard before context
	assert.Equal(t, "guard", result[0].Phase)
	assert.Equal(t, "context", result[1].Phase)
}

func TestGetHandlersForEvent_SortsByPhase(t *testing.T) {
	handlers := []ResolvedHandler{
		{Name: "side", Events: []string{EventPreToolUse}, Phase: "side_effect"},
		{Name: "context", Events: []string{EventPreToolUse}, Phase: "context"},
		{Name: "guard", Events: []string{EventPreToolUse}, Phase: "guard"},
	}

	result := GetHandlersForEvent(handlers, EventPreToolUse)
	require.Len(t, result, 3)
	assert.Equal(t, "guard", result[0].Phase)
	assert.Equal(t, "context", result[1].Phase)
	assert.Equal(t, "side_effect", result[2].Phase)
}

func TestGetHandlersForEvent_NoMatch(t *testing.T) {
	handlers := []ResolvedHandler{
		{Name: "h1", Events: []string{EventPreToolUse}, Phase: "guard"},
	}

	result := GetHandlersForEvent(handlers, EventPostToolUse)
	assert.Empty(t, result)
}

// =============================================================================
// Unit Tests: Handler Execution
// =============================================================================

func TestExecuteHandler_InlineWithAction(t *testing.T) {
	handler := ResolvedHandler{
		Name:        "inline-test",
		HandlerType: "inline",
		Action: map[string]interface{}{
			"decision":          "block",
			"reason":            "inline block",
			"additionalContext": "some context",
		},
	}

	result := ExecuteHandler(context.Background(), handler, HookPayload{SessionID: "test"})

	require.NotNil(t, result.Decision)
	assert.Equal(t, "block", *result.Decision)
	require.NotNil(t, result.Reason)
	assert.Equal(t, "inline block", *result.Reason)
	require.NotNil(t, result.AdditionalContext)
	assert.Equal(t, "some context", *result.AdditionalContext)
}

func TestExecuteHandler_InlineNoAction(t *testing.T) {
	handler := ResolvedHandler{
		Name:        "inline-empty",
		HandlerType: "inline",
		Action:      nil,
	}

	result := ExecuteHandler(context.Background(), handler, HookPayload{SessionID: "test"})

	assert.Nil(t, result.Decision)
	assert.Nil(t, result.Reason)
	assert.Nil(t, result.AdditionalContext)
}

func TestExecuteHandler_BuiltinWithAction(t *testing.T) {
	handler := ResolvedHandler{
		Name:        "builtin-test",
		HandlerType: "builtin",
		Module:      "test-module",
		Action: map[string]interface{}{
			"additionalContext": "builtin context",
		},
	}

	result := ExecuteHandler(context.Background(), handler, HookPayload{SessionID: "test"})

	require.NotNil(t, result.AdditionalContext)
	assert.Equal(t, "builtin context", *result.AdditionalContext)
}

func TestExecuteHandler_UnknownType(t *testing.T) {
	handler := ResolvedHandler{
		Name:        "unknown-handler",
		HandlerType: "unknown",
	}

	result := ExecuteHandler(context.Background(), handler, HookPayload{SessionID: "test"})

	assert.Nil(t, result.Decision)
}

// =============================================================================
// Unit Tests: payloadToMap
// =============================================================================

func TestPayloadToMap_Basic(t *testing.T) {
	payload := HookPayload{
		SessionID: "session-123",
		ToolName:  "Bash",
		ToolInput: map[string]interface{}{
			"command": "ls -la",
		},
	}

	m := payloadToMap(payload)

	assert.Equal(t, "session-123", m["session_id"])
	assert.Equal(t, "Bash", m["tool_name"])
	toolInput, ok := m["tool_input"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ls -la", toolInput["command"])
}

func TestPayloadToMap_EmptyPayload(t *testing.T) {
	m := payloadToMap(HookPayload{})
	assert.NotNil(t, m)
}

// =============================================================================
// Unit Tests: mergeContext
// =============================================================================

func TestMergeContext_BothPresent(t *testing.T) {
	result := mergeContext("warn", "handler")
	assert.Equal(t, "warn\n\nhandler", result)
}

func TestMergeContext_OnlyWarn(t *testing.T) {
	result := mergeContext("warn", "")
	assert.Equal(t, "warn", result)
}

func TestMergeContext_OnlyHandler(t *testing.T) {
	result := mergeContext("", "handler")
	assert.Equal(t, "handler", result)
}

func TestMergeContext_NeitherPresent(t *testing.T) {
	result := mergeContext("", "")
	assert.Empty(t, result)
}

// =============================================================================
// Unit Tests: buildPermissionJSON
// =============================================================================

func TestBuildPermissionJSON_Deny(t *testing.T) {
	result := buildPermissionJSON("deny", "blocked")
	expected := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"blocked"}}`
	assert.Equal(t, expected, result)
}

func TestBuildPermissionJSON_Ask(t *testing.T) {
	result := buildPermissionJSON("ask", "needs confirmation")
	expected := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"needs confirmation"}}`
	assert.Equal(t, expected, result)
}

func TestBuildPermissionJSON_KeyOrder(t *testing.T) {
	result := buildPermissionJSON("deny", "reason")

	// Verify key order in the JSON
	hookIdx := strings.Index(result, "hookEventName")
	decisionIdx := strings.Index(result, "permissionDecision\"")
	reasonIdx := strings.Index(result, "permissionDecisionReason")

	assert.True(t, hookIdx < decisionIdx, "hookEventName must come before permissionDecision")
	assert.True(t, decisionIdx < reasonIdx, "permissionDecision must come before permissionDecisionReason")
}

// =============================================================================
// Unit Tests: buildContextJSON
// =============================================================================

func TestBuildContextJSON(t *testing.T) {
	result := buildContextJSON("some context")

	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)

	hookOutput, ok := parsed["hookSpecificOutput"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "some context", hookOutput["additionalContext"])
}

// =============================================================================
// Integration Tests: Full Dispatch Pipeline
// =============================================================================

func TestIntegration_PreToolUse_BlockGuard_FullPipeline(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "no bash in production"},
	})
	payload := preToolUsePayload("Bash", map[string]interface{}{
		"command": "rm -rf /",
	})

	result := SafeDispatch(func() DispatchResult {
		return Dispatch(context.Background(), EventPreToolUse, payload, config)
	})

	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout)

	var output map[string]interface{}
	err := json.Unmarshal([]byte(*result.Stdout), &output)
	require.NoError(t, err)

	hookOutput := output["hookSpecificOutput"].(map[string]interface{})
	assert.Equal(t, "PreToolUse", hookOutput["hookEventName"])
	assert.Equal(t, "deny", hookOutput["permissionDecision"])
	assert.Equal(t, "no bash in production", hookOutput["permissionDecisionReason"])
}

func TestIntegration_PreToolUse_ConfirmGuard_FullPipeline(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "mcp__*", Action: "confirm", Reason: "MCP access requires approval"},
	})
	payload := preToolUsePayload("mcp__slack__send_message", nil)

	result := SafeDispatch(func() DispatchResult {
		return Dispatch(context.Background(), EventPreToolUse, payload, config)
	})

	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout)

	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "ask", hookOutput["permissionDecision"])
}

func TestIntegration_PreToolUse_WarnGuard_FullPipeline(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Write", Action: "warn", Reason: "be careful with writes"},
	})
	payload := preToolUsePayload("Write", nil)

	result := SafeDispatch(func() DispatchResult {
		return Dispatch(context.Background(), EventPreToolUse, payload, config)
	})

	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout)

	hookOutput := parseHookOutput(t, result.Stdout)
	ctx := hookOutput["additionalContext"].(string)
	assert.Contains(t, ctx, "be careful with writes")
}

func TestIntegration_UnrecognizedEvent_FullPipeline(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "*", Action: "block", Reason: "block all"},
	})
	payload := preToolUsePayload("Bash", nil)

	result := SafeDispatch(func() DispatchResult {
		return Dispatch(context.Background(), "TotallyFakeEvent", payload, config)
	})

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout)
}

func TestIntegration_PanicRecovery_FullPipeline(t *testing.T) {
	result := SafeDispatch(func() DispatchResult {
		panic("catastrophic failure")
	})

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout)
}

func TestIntegration_FirstMatchWins_BlockBeforeWarn(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "blocked"},
		{Match: "Bash", Action: "warn", Reason: "warned"},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	require.NotNil(t, result.Stdout)
	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "deny", hookOutput["permissionDecision"])
	assert.Equal(t, "blocked", hookOutput["permissionDecisionReason"])
}

func TestIntegration_FirstMatchWins_WarnBeforeBlock(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "warn", Reason: "warned"},
		{Match: "Bash", Action: "block", Reason: "blocked"},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	// Warn doesn't short-circuit like block/confirm, but since it's first-match-wins,
	// the warn rule is the match and block never fires
	require.NotNil(t, result.Stdout)
	hookOutput := parseHookOutput(t, result.Stdout)
	// Should have additionalContext, NOT permissionDecision
	assert.Contains(t, hookOutput["additionalContext"], "warned")
	assert.Nil(t, hookOutput["permissionDecision"])
}

func TestIntegration_ConditionGuard_WhenMatches(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "no rm -rf",
			When:   `tool_input.command contains "rm -rf"`,
		},
	})
	payload := preToolUsePayload("Bash", map[string]interface{}{
		"command": "rm -rf /important",
	})

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	require.NotNil(t, result.Stdout)
	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "deny", hookOutput["permissionDecision"])
}

func TestIntegration_ConditionGuard_WhenDoesNotMatch(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "no rm -rf",
			When:   `tool_input.command contains "rm -rf"`,
		},
	})
	payload := preToolUsePayload("Bash", map[string]interface{}{
		"command": "ls -la",
	})

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	assert.Nil(t, result.Stdout, "when condition not met should not block")
}

func TestIntegration_ConditionGuard_UnlessExcludes(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "blocked",
			Unless: `tool_input.command starts_with "ls"`,
		},
	})
	payload := preToolUsePayload("Bash", map[string]interface{}{
		"command": "ls -la",
	})

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	assert.Nil(t, result.Stdout, "unless condition should prevent block")
}

func TestIntegration_MultipleGuards_FirstBlockWins(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "bash blocked"},
		{Match: "Write", Action: "confirm", Reason: "write needs confirm"},
		{Match: "*", Action: "warn", Reason: "general warning"},
	})

	// Test Bash -> block
	r1 := Dispatch(context.Background(), EventPreToolUse, preToolUsePayload("Bash", nil), config)
	require.NotNil(t, r1.Stdout)
	ho1 := parseHookOutput(t, r1.Stdout)
	assert.Equal(t, "deny", ho1["permissionDecision"])

	// Test Write -> confirm
	r2 := Dispatch(context.Background(), EventPreToolUse, preToolUsePayload("Write", nil), config)
	require.NotNil(t, r2.Stdout)
	ho2 := parseHookOutput(t, r2.Stdout)
	assert.Equal(t, "ask", ho2["permissionDecision"])

	// Test Read -> warn (matches *)
	r3 := Dispatch(context.Background(), EventPreToolUse, preToolUsePayload("Read", nil), config)
	require.NotNil(t, r3.Stdout)
	ho3 := parseHookOutput(t, r3.Stdout)
	assert.Contains(t, ho3["additionalContext"], "general warning")
}

func TestIntegration_HandlerBasedGuard_Blocks(t *testing.T) {
	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:   "inline-guard",
			Type:   "inline",
			Events: []string{EventPreToolUse},
			Phase:  "guard",
			Action: map[string]interface{}{
				"decision": "block",
				"reason":   "inline guard blocked",
			},
		},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	require.NotNil(t, result.Stdout)
	// Handler-based guard block produces different JSON format (decision/reason, not hookSpecificOutput)
	var output map[string]interface{}
	err := json.Unmarshal([]byte(*result.Stdout), &output)
	require.NoError(t, err)
	assert.Equal(t, "block", output["decision"])
	assert.Equal(t, "inline guard blocked", output["reason"])
}

func TestIntegration_DeclarativeGuardBeforeHandlerGuard(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "declarative block"},
	})
	config.Handlers = []CustomHandlerConfig{
		{
			Name:   "handler-guard",
			Type:   "inline",
			Events: []string{EventPreToolUse},
			Phase:  "guard",
			Action: map[string]interface{}{
				"decision": "block",
				"reason":   "handler block",
			},
		},
	}
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	// Declarative guards (Phase 1a) run before handler guards (Phase 1b)
	require.NotNil(t, result.Stdout)
	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "deny", hookOutput["permissionDecision"])
	assert.Equal(t, "declarative block", hookOutput["permissionDecisionReason"])
}

func TestIntegration_AllEventTypes_ExitZero(t *testing.T) {
	config := emptyConfig()
	payload := HookPayload{SessionID: "test"}

	for _, eventType := range EventTypes {
		t.Run(eventType, func(t *testing.T) {
			result := Dispatch(context.Background(), eventType, payload, config)
			assert.Equal(t, 0, result.ExitCode, "event %s should exit 0", eventType)
		})
	}
}

func TestIntegration_EmptyConfig_AllEventsAllow(t *testing.T) {
	config := emptyConfig()
	payload := preToolUsePayload("Bash", map[string]interface{}{
		"command": "rm -rf /",
	})

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout, "empty config should produce no stdout")
}

// =============================================================================
// Integration Tests: Script Handler Execution
// =============================================================================

func TestIntegration_ScriptHandler_ExitZero_ParsesResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	// Create a temporary script that outputs valid JSON
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "handler.sh")
	scriptContent := `#!/bin/sh
echo '{"additionalContext":"hello from script"}'
`
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)

	handler := ResolvedHandler{
		Name:        "test-script",
		HandlerType: "script",
		Command:     scriptPath,
		Timeout:     5000,
		Phase:       "context",
	}

	result := ExecuteHandler(context.Background(), handler, HookPayload{SessionID: "test"})

	require.NotNil(t, result.AdditionalContext)
	assert.Equal(t, "hello from script", *result.AdditionalContext)
}

func TestIntegration_ScriptHandler_ExitTwo_BlockJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "blocker.sh")
	scriptContent := `#!/bin/sh
echo '{"decision":"block","reason":"script blocked"}'
exit 2
`
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)

	handler := ResolvedHandler{
		Name:        "test-blocker",
		HandlerType: "script",
		Command:     scriptPath,
		Timeout:     5000,
		Phase:       "guard",
	}

	result := ExecuteHandler(context.Background(), handler, HookPayload{SessionID: "test"})

	require.NotNil(t, result.Decision)
	assert.Equal(t, "block", *result.Decision)
	require.NotNil(t, result.Reason)
	assert.Equal(t, "script blocked", *result.Reason)
}

func TestIntegration_ScriptHandler_ExitTwo_NotBlock_Ignored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "bad-exit2.sh")
	scriptContent := `#!/bin/sh
echo '{"decision":"warn","reason":"not a block"}'
exit 2
`
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)

	handler := ResolvedHandler{
		Name:        "bad-exit2",
		HandlerType: "script",
		Command:     scriptPath,
		Timeout:     5000,
		Phase:       "guard",
	}

	result := ExecuteHandler(context.Background(), handler, HookPayload{SessionID: "test"})

	// Exit 2 without decision:"block" should be treated as error
	assert.Nil(t, result.Decision)
}

func TestIntegration_ScriptHandler_ExitOne_Ignored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "exit1.sh")
	scriptContent := `#!/bin/sh
echo '{"decision":"block","reason":"should not matter"}'
exit 1
`
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)

	handler := ResolvedHandler{
		Name:        "exit1-handler",
		HandlerType: "script",
		Command:     scriptPath,
		Timeout:     5000,
		Phase:       "guard",
	}

	result := ExecuteHandler(context.Background(), handler, HookPayload{SessionID: "test"})

	// Exit 1 is treated as error, not a valid handler result
	assert.Nil(t, result.Decision)
}

func TestIntegration_ScriptHandler_EmptyStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "empty.sh")
	scriptContent := `#!/bin/sh
exit 0
`
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)

	handler := ResolvedHandler{
		Name:        "empty-stdout",
		HandlerType: "script",
		Command:     scriptPath,
		Timeout:     5000,
	}

	result := ExecuteHandler(context.Background(), handler, HookPayload{SessionID: "test"})

	assert.Nil(t, result.Decision)
	assert.Nil(t, result.Reason)
}

func TestIntegration_ScriptHandler_InvalidJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "invalid-json.sh")
	scriptContent := `#!/bin/sh
echo 'not json at all'
`
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)

	handler := ResolvedHandler{
		Name:        "invalid-json",
		HandlerType: "script",
		Command:     scriptPath,
		Timeout:     5000,
	}

	result := ExecuteHandler(context.Background(), handler, HookPayload{SessionID: "test"})

	assert.Nil(t, result.Decision)
}

func TestIntegration_ScriptHandler_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	_, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep command not found")
	}

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "slow.sh")
	scriptContent := `#!/bin/sh
sleep 10
echo '{"decision":"block","reason":"should never reach here"}'
`
	err = os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)

	handler := ResolvedHandler{
		Name:        "slow-handler",
		HandlerType: "script",
		Command:     scriptPath,
		Timeout:     200, // 200ms timeout
		Phase:       "guard",
	}

	start := time.Now()
	result := ExecuteHandler(context.Background(), handler, HookPayload{SessionID: "test"})
	elapsed := time.Since(start)

	assert.Nil(t, result.Decision, "timed-out handler should return empty result")
	assert.Less(t, elapsed, 5*time.Second, "handler should timeout well before 5s")
}

func TestIntegration_ScriptHandler_ReceivesPayloadOnStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "stdin-capture.txt")
	scriptPath := filepath.Join(tmpDir, "stdin-reader.sh")
	scriptContent := fmt.Sprintf(`#!/bin/sh
cat > %s
echo '{"additionalContext":"captured"}'
`, outputPath)
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)

	handler := ResolvedHandler{
		Name:        "stdin-reader",
		HandlerType: "script",
		Command:     scriptPath,
		Timeout:     5000,
	}

	payload := HookPayload{
		SessionID: "capture-test",
		ToolName:  "Bash",
		ToolInput: map[string]interface{}{
			"command": "echo hello",
		},
	}

	result := ExecuteHandler(context.Background(), handler, payload)
	require.NotNil(t, result.AdditionalContext)

	// Verify the script received the payload on stdin
	capturedData, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	var captured map[string]interface{}
	err = json.Unmarshal(capturedData, &captured)
	require.NoError(t, err)
	assert.Equal(t, "capture-test", captured["session_id"])
	assert.Equal(t, "Bash", captured["tool_name"])
}

func TestIntegration_ScriptHandler_NoCommand(t *testing.T) {
	handler := ResolvedHandler{
		Name:        "no-command",
		HandlerType: "script",
		Command:     "",
		Timeout:     5000,
	}

	result := ExecuteHandler(context.Background(), handler, HookPayload{SessionID: "test"})
	assert.Nil(t, result.Decision)
}

func TestIntegration_ScriptHandler_NonexistentCommand(t *testing.T) {
	handler := ResolvedHandler{
		Name:        "nonexistent",
		HandlerType: "script",
		Command:     "/nonexistent/path/to/handler",
		Timeout:     5000,
	}

	result := ExecuteHandler(context.Background(), handler, HookPayload{SessionID: "test"})
	assert.Nil(t, result.Decision)
}

// =============================================================================
// Integration Tests: Full Dispatch with Script Handlers
// =============================================================================

func TestIntegration_FullDispatch_ScriptGuard_Blocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "guard.sh")
	scriptContent := `#!/bin/sh
echo '{"decision":"block","reason":"script guard blocked"}'
`
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)

	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:    "script-guard",
			Type:    "script",
			Events:  []string{EventPreToolUse},
			Phase:   "guard",
			Command: scriptPath,
			Timeout: 5,
		},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	require.NotNil(t, result.Stdout)
	var output map[string]interface{}
	err = json.Unmarshal([]byte(*result.Stdout), &output)
	require.NoError(t, err)
	assert.Equal(t, "block", output["decision"])
}

func TestIntegration_FullDispatch_ScriptContextHandler(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "context.sh")
	scriptContent := `#!/bin/sh
echo '{"additionalContext":"context from script"}'
`
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)

	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:    "script-context",
			Type:    "script",
			Events:  []string{EventSessionStart},
			Phase:   "context",
			Command: scriptPath,
			Timeout: 5,
		},
	})
	payload := HookPayload{SessionID: "test-session"}

	result := Dispatch(context.Background(), EventSessionStart, payload, config)

	require.NotNil(t, result.Stdout)
	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "context from script", hookOutput["additionalContext"])
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestDispatch_AllPhases_ExecuteInOrder(t *testing.T) {
	// Setup handlers in all three phases
	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:   "side-effect",
			Type:   "inline",
			Events: []string{EventPreToolUse},
			Phase:  "side_effect",
		},
		{
			Name:   "context",
			Type:   "inline",
			Events: []string{EventPreToolUse},
			Phase:  "context",
			Action: map[string]interface{}{
				"additionalContext": "phase 2 context",
			},
		},
		{
			Name:   "guard",
			Type:   "inline",
			Events: []string{EventPreToolUse},
			Phase:  "guard",
			Action: nil, // guard that does NOT block
		},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	// Guard didn't block, context should be present
	require.NotNil(t, result.Stdout)
	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "phase 2 context", hookOutput["additionalContext"])
}

func TestDispatch_SpecialCharactersInReason(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: `reason with "quotes" and <brackets> & ampersand`},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(context.Background(), EventPreToolUse, payload, config)

	require.NotNil(t, result.Stdout)
	// Verify the JSON is valid and contains the special chars
	var output map[string]interface{}
	err := json.Unmarshal([]byte(*result.Stdout), &output)
	require.NoError(t, err)
	hookOutput := output["hookSpecificOutput"].(map[string]interface{})
	assert.Equal(t, `reason with "quotes" and <brackets> & ampersand`, hookOutput["permissionDecisionReason"])
}

func TestDispatch_EmptyPayload(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "blocked"},
	})
	payload := HookPayload{} // completely empty

	// Should not panic even with empty payload
	result := Dispatch(context.Background(), EventPreToolUse, payload, config)
	assert.Equal(t, 0, result.ExitCode)
}

func TestDispatch_MultipleGuardRules_ComplexScenario(t *testing.T) {
	config := configWithGuards([]GuardRuleConfig{
		{
			Match:  "Bash",
			Action: "block",
			Reason: "rm -rf blocked",
			When:   `tool_input.command contains "rm -rf"`,
		},
		{
			Match:  "Bash",
			Action: "warn",
			Reason: "bash usage noted",
		},
		{
			Match:  "mcp__*",
			Action: "confirm",
			Reason: "external tool",
		},
	})

	// rm -rf -> block
	r1 := Dispatch(context.Background(), EventPreToolUse, preToolUsePayload("Bash", map[string]interface{}{
		"command": "rm -rf /tmp",
	}), config)
	require.NotNil(t, r1.Stdout)
	ho1 := parseHookOutput(t, r1.Stdout)
	assert.Equal(t, "deny", ho1["permissionDecision"])

	// safe bash -> warn
	r2 := Dispatch(context.Background(), EventPreToolUse, preToolUsePayload("Bash", map[string]interface{}{
		"command": "ls -la",
	}), config)
	require.NotNil(t, r2.Stdout)
	ho2 := parseHookOutput(t, r2.Stdout)
	assert.Contains(t, ho2["additionalContext"], "bash usage noted")

	// MCP tool -> confirm
	r3 := Dispatch(context.Background(), EventPreToolUse, preToolUsePayload("mcp__slack__send", nil), config)
	require.NotNil(t, r3.Stdout)
	ho3 := parseHookOutput(t, r3.Stdout)
	assert.Equal(t, "ask", ho3["permissionDecision"])
}

// =============================================================================
// inferPhase tests
// =============================================================================

func TestInferPhase_ExplicitPhase(t *testing.T) {
	h := CustomHandlerConfig{Phase: "context", Events: []string{EventPreToolUse}, Type: "script"}
	assert.Equal(t, "context", inferPhase(h))
}

func TestInferPhase_PreToolUseScript_Guard(t *testing.T) {
	h := CustomHandlerConfig{Events: []string{EventPreToolUse}, Type: "script"}
	assert.Equal(t, "guard", inferPhase(h))
}

func TestInferPhase_UserPromptSubmitScript_Guard(t *testing.T) {
	h := CustomHandlerConfig{Events: []string{EventUserPromptSubmit}, Type: "script"}
	assert.Equal(t, "guard", inferPhase(h))
}

func TestInferPhase_PermissionRequestScript_Guard(t *testing.T) {
	h := CustomHandlerConfig{Events: []string{EventPermissionRequest}, Type: "builtin"}
	assert.Equal(t, "guard", inferPhase(h))
}

func TestInferPhase_PreToolUseInline_SideEffect(t *testing.T) {
	h := CustomHandlerConfig{Events: []string{EventPreToolUse}, Type: "inline"}
	assert.Equal(t, "side_effect", inferPhase(h))
}

func TestInferPhase_SessionStart_Context(t *testing.T) {
	h := CustomHandlerConfig{Events: []string{EventSessionStart}, Type: "inline"}
	assert.Equal(t, "context", inferPhase(h))
}

func TestInferPhase_SubagentStart_Context(t *testing.T) {
	h := CustomHandlerConfig{Events: []string{EventSubagentStart}, Type: "script"}
	// SubagentStart is both a guard event (no) and context event (yes)
	// Since it's not in guardEvents, it should infer context
	assert.Equal(t, "context", inferPhase(h))
}

func TestInferPhase_PostToolUse_SideEffect(t *testing.T) {
	h := CustomHandlerConfig{Events: []string{EventPostToolUse}, Type: "script"}
	assert.Equal(t, "side_effect", inferPhase(h))
}

func TestInferPhase_SessionEnd_SideEffect(t *testing.T) {
	h := CustomHandlerConfig{Events: []string{EventSessionEnd}, Type: "script"}
	assert.Equal(t, "side_effect", inferPhase(h))
}

// =============================================================================
// Task 5.1: Dispatch Respects Context Cancellation
// =============================================================================

func TestDispatch_CancelledContext_ReturnsExitZero(t *testing.T) {
	// A pre-cancelled context should cause Dispatch to return exit 0 immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "should not evaluate"},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(ctx, EventPreToolUse, payload, config)

	assert.Equal(t, 0, result.ExitCode, "cancelled context should fail-open with exit 0")
	assert.Nil(t, result.Stdout, "cancelled context should produce no stdout")
}

func TestDispatch_DeadlineExceeded_ReturnsExitZero(t *testing.T) {
	// A context past its deadline should fail-open
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Microsecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond) // ensure deadline has passed

	config := emptyConfig()
	payload := preToolUsePayload("Read", nil)

	result := Dispatch(ctx, EventPreToolUse, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout)
}

func TestDispatch_GuardBlockSurvives_WhenContextValid(t *testing.T) {
	// Guard decisions computed before timeout should still be returned.
	// Use a generous timeout so the guard runs normally.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "no bash"},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(ctx, EventPreToolUse, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout, "guard block should produce stdout")
	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "deny", hookOutput["permissionDecision"])
	assert.Equal(t, "no bash", hookOutput["permissionDecisionReason"])
}

func TestDispatch_ContextPropagatedToGuardHandlers(t *testing.T) {
	// Verify that a cancelled context stops guard handler execution
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	// Create a context that cancels after 50ms
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Script handler that sleeps 10s — should be cancelled by context
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "slow-guard.sh")
	err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 10\necho '{\"decision\":\"block\",\"reason\":\"too slow\"}'\n"), 0o755)
	require.NoError(t, err)

	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:    "slow-guard",
			Type:    "script",
			Events:  []string{EventPreToolUse},
			Command: scriptPath,
			Timeout: 30, // 30s handler timeout — but dispatch ctx is 50ms
		},
	})
	payload := preToolUsePayload("Bash", nil)

	start := time.Now()
	result := Dispatch(ctx, EventPreToolUse, payload, config)
	elapsed := time.Since(start)

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout, "cancelled handler should not produce block decision")
	assert.Less(t, elapsed, 2*time.Second, "dispatch should exit well before handler would complete")
}

func TestDispatch_ContextPropagatedToContextPhase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	// Context handler that would sleep — dispatch ctx cancels first
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "slow-ctx.sh")
	err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 10\necho '{\"additionalContext\":\"late context\"}'\n"), 0o755)
	require.NoError(t, err)

	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:    "slow-context",
			Type:    "script",
			Events:  []string{EventSessionStart},
			Phase:   "context",
			Command: scriptPath,
			Timeout: 30,
		},
	})
	payload := HookPayload{SessionID: "test-session-123"}

	start := time.Now()
	result := Dispatch(ctx, EventSessionStart, payload, config)
	elapsed := time.Since(start)

	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Stdout, "timed out context handler should not produce output")
	assert.Less(t, elapsed, 2*time.Second)
}

// =============================================================================
// Task 5.2: DispatchConfig Defaults and Edge Cases
// =============================================================================

func TestDispatchConfig_ZeroTimeout_UsesDefault(t *testing.T) {
	// Zero value is preserved in struct; CLI resolves to default
	cfg := DispatchConfig{TimeoutMs: 0}
	assert.Equal(t, 0, cfg.TimeoutMs, "zero value should be preserved in struct")
	assert.Equal(t, 500, DefaultDispatchTimeoutMs, "default constant should be 500ms")
}

func TestDispatchConfig_CustomTimeout_Respected(t *testing.T) {
	cfg := DispatchConfig{TimeoutMs: 1000}
	assert.Equal(t, 1000, cfg.TimeoutMs)
}

func TestDispatchConfig_NegativeTimeout_NoTimeout(t *testing.T) {
	// Negative timeout means no timeout (opt-out)
	cfg := DispatchConfig{TimeoutMs: -1}
	assert.Less(t, cfg.TimeoutMs, 0, "negative timeout signals no timeout")
}

func TestDispatchConfig_InHooksConfig(t *testing.T) {
	// Verify DispatchConfig is part of HooksConfig and default config sets the timeout
	cfg := GetDefaultConfig()
	assert.Equal(t, DefaultDispatchTimeoutMs, cfg.Dispatch.TimeoutMs, "default config should set dispatch timeout")
}

func TestDispatchConfig_YAMLUnmarshal(t *testing.T) {
	yamlStr := `
version: 1
dispatch:
  timeout_ms: 750
`
	var cfg HooksConfig
	err := yaml.Unmarshal([]byte(yamlStr), &cfg)
	require.NoError(t, err)
	assert.Equal(t, 750, cfg.Dispatch.TimeoutMs)
}

func TestDispatchConfig_YAMLUnmarshal_Absent(t *testing.T) {
	yamlStr := `
version: 1
`
	var cfg HooksConfig
	err := yaml.Unmarshal([]byte(yamlStr), &cfg)
	require.NoError(t, err)
	assert.Equal(t, 0, cfg.Dispatch.TimeoutMs, "absent dispatch section should leave zero value")
}

// =============================================================================
// Task 5.3: Dispatch-Level Timeout with Slow Script Handlers
// =============================================================================

func TestDispatch_TimeoutKillsSlowGuardHandler(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	_, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep command not found")
	}

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "very-slow-guard.sh")
	err = os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 30\necho '{\"decision\":\"block\",\"reason\":\"never\"}'\n"), 0o755)
	require.NoError(t, err)

	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:    "very-slow-guard",
			Type:    "script",
			Events:  []string{EventPreToolUse},
			Command: scriptPath,
			Timeout: 60, // 60s handler timeout
		},
	})
	payload := preToolUsePayload("Bash", nil)

	// Dispatch context with 100ms timeout — should override the 60s handler timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	result := Dispatch(ctx, EventPreToolUse, payload, config)
	elapsed := time.Since(start)

	assert.Equal(t, 0, result.ExitCode, "timeout should fail-open")
	assert.Nil(t, result.Stdout, "timed-out handler should not produce output")
	assert.Less(t, elapsed, 2*time.Second, "dispatch should terminate within 2s")
}

func TestDispatch_TimeoutDoesNotAffectFastDispatch(t *testing.T) {
	// A fast dispatch with a timeout should work normally
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	config := configWithGuards([]GuardRuleConfig{
		{Match: "Bash", Action: "block", Reason: "blocked"},
	})
	payload := preToolUsePayload("Bash", nil)

	result := Dispatch(ctx, EventPreToolUse, payload, config)

	assert.Equal(t, 0, result.ExitCode)
	require.NotNil(t, result.Stdout, "fast dispatch should still produce guard result")
	hookOutput := parseHookOutput(t, result.Stdout)
	assert.Equal(t, "deny", hookOutput["permissionDecision"])
}

func TestDispatch_TimeoutWithSideEffects(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script tests require unix shell")
	}

	tmpDir := t.TempDir()
	markerPath := filepath.Join(tmpDir, "side-effect-marker")
	scriptPath := filepath.Join(tmpDir, "slow-side-effect.sh")
	// Side effect writes a marker after 5s sleep — should be killed by context
	err := os.WriteFile(scriptPath, []byte(fmt.Sprintf("#!/bin/sh\nsleep 5\ntouch %s\n", markerPath)), 0o755)
	require.NoError(t, err)

	config := configWithHandlers([]CustomHandlerConfig{
		{
			Name:    "slow-side-effect",
			Type:    "script",
			Events:  []string{EventPostToolUse},
			Phase:   "side_effect",
			Command: scriptPath,
			Timeout: 30,
		},
	})
	payload := HookPayload{SessionID: "test-session-123", ToolName: "Bash"}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	// Use FireSideEffectsSync to wait for completion (or cancellation)
	handlers := GetHandlersForEvent(ResolveHandlers(config), EventPostToolUse)
	FireSideEffectsSync(ctx, handlers, payload)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 2*time.Second, "side effect should be cancelled by context")
	// Marker file should NOT exist since the script was killed
	_, err = os.Stat(markerPath)
	assert.True(t, os.IsNotExist(err), "side effect marker should not exist — script was killed")
}
