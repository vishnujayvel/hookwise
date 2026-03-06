package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Dispatch is the main three-phase execution engine for hook events.
//
// Phase 1a: Declarative Guards (PreToolUse only) — evaluate config.Guards[]
// Phase 1b: Handler-based guards (phase: "guard")
// Phase 2:  Context Injection — collect additionalContext
// Phase 3:  Side Effects — fire goroutines (non-blocking)
//
// Output is a DispatchResult with JSON on stdout for block/confirm/context.
// Unrecognized event types return ExitCode 0 with no stdout.
// Follows fail-open guarantee (ARCH-1).
func Dispatch(eventType string, payload HookPayload, config HooksConfig) DispatchResult {
	// Unrecognized event type -> exit 0, no stdout
	if !IsEventType(eventType) {
		return DispatchResult{ExitCode: 0}
	}

	// Resolve and filter handlers for this event
	allHandlers := ResolveHandlers(config)
	handlers := GetHandlersForEvent(allHandlers, eventType)

	// Track warn context from declarative guards
	var warnContext string

	// Build payload as map for guard evaluation
	payloadAsMap := payloadToMap(payload)

	// Phase 1a: Declarative guard rules (config.Guards[])
	// Evaluated on PreToolUse events only — first match wins
	if eventType == EventPreToolUse && len(config.Guards) > 0 {
		guardResult := func() *DispatchResult {
			defer func() {
				if r := recover(); r != nil {
					Logger().Error("panic in declarative guard evaluation", "recovered", fmt.Sprintf("%v", r))
				}
			}()

			guardEval := Evaluate(payload.ToolName, payloadAsMap, config.Guards)

			switch guardEval.Action {
			case "block":
				reason := guardEval.Reason
				if reason == "" {
					reason = "Blocked by guard rule"
				}
				stdout := buildPermissionJSON("deny", reason)
				return &DispatchResult{Stdout: &stdout, ExitCode: 0}

			case "confirm":
				reason := guardEval.Reason
				if reason == "" {
					reason = "Requires confirmation"
				}
				stdout := buildPermissionJSON("ask", reason)
				return &DispatchResult{Stdout: &stdout, ExitCode: 0}

			case "warn":
				if guardEval.Reason != "" {
					warnContext = "Guard warning: " + guardEval.Reason
				}
			}
			return nil
		}()

		if guardResult != nil {
			return *guardResult
		}
	}

	// Phase 1b: Handler-based guards (handlers with phase: "guard")
	guardPhaseResult := executeGuardPhase(handlers, payload)
	if guardPhaseResult != nil {
		return *guardPhaseResult
	}

	// Phase 2: Context Injection
	contextOutput := executeContextPhase(handlers, payload)

	// Phase 3: Side Effects (non-blocking goroutines)
	fireSideEffects(handlers, payload)

	// Merge warn context with Phase 2 context output
	finalContext := mergeContext(warnContext, contextOutput)

	// Build final result
	if finalContext != "" {
		stdout := buildContextJSON(finalContext)
		return DispatchResult{Stdout: &stdout, ExitCode: 0}
	}

	return DispatchResult{ExitCode: 0}
}

// --- Phase 1b: Guard Handlers ---

// executeGuardPhase runs guard-phase handlers synchronously.
// On first block decision, short-circuits and returns the block result.
// Returns nil if no guard blocked.
func executeGuardPhase(handlers []ResolvedHandler, payload HookPayload) *DispatchResult {
	for i := range handlers {
		if handlers[i].Phase != "guard" {
			continue
		}

		result := func() *DispatchResult {
			defer func() {
				if r := recover(); r != nil {
					Logger().Error("panic in guard handler", "handler", handlers[i].Name, "recovered", fmt.Sprintf("%v", r))
				}
			}()

			hr := ExecuteHandler(handlers[i], payload)

			if hr.Decision != nil && *hr.Decision == "block" {
				reason := "Blocked by guard rule"
				if hr.Reason != nil {
					reason = *hr.Reason
				}
				stdout := buildGuardBlockJSON(reason)
				return &DispatchResult{Stdout: &stdout, ExitCode: 0}
			}
			return nil
		}()

		if result != nil {
			return result
		}
	}
	return nil
}

// --- Phase 2: Context Injection ---

// executeContextPhase runs context-phase handlers and collects additionalContext strings.
// Returns the combined context string or empty string if none.
func executeContextPhase(handlers []ResolvedHandler, payload HookPayload) string {
	var parts []string

	for i := range handlers {
		if handlers[i].Phase != "context" {
			continue
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					Logger().Error("panic in context handler", "handler", handlers[i].Name, "recovered", fmt.Sprintf("%v", r))
				}
			}()

			hr := ExecuteHandler(handlers[i], payload)
			if hr.AdditionalContext != nil && *hr.AdditionalContext != "" {
				parts = append(parts, *hr.AdditionalContext)
			}
		}()
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// --- Phase 3: Side Effects ---

// fireSideEffects launches side-effect handlers in goroutines.
// Each goroutine has its own defer/recover boundary (ARCH-7).
// Side effects do NOT block the response.
func fireSideEffects(handlers []ResolvedHandler, payload HookPayload) {
	var wg sync.WaitGroup

	for i := range handlers {
		if handlers[i].Phase != "side_effect" {
			continue
		}

		wg.Add(1)
		go func(h ResolvedHandler) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					Logger().Error("panic in side-effect handler", "handler", h.Name, "recovered", fmt.Sprintf("%v", r))
				}
			}()
			ExecuteHandler(h, payload)
		}(handlers[i])
	}

	// Do NOT wait — side effects are non-blocking.
	// The goroutines will run and complete on their own.
	// We use WaitGroup only if callers need synchronization (e.g., tests).
}

// FireSideEffectsSync is like fireSideEffects but waits for completion.
// Used in tests to verify side effects ran without blocking dispatch.
func FireSideEffectsSync(handlers []ResolvedHandler, payload HookPayload) {
	var wg sync.WaitGroup

	for i := range handlers {
		if handlers[i].Phase != "side_effect" {
			continue
		}

		wg.Add(1)
		go func(h ResolvedHandler) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					Logger().Error("panic in side-effect handler", "handler", h.Name, "recovered", fmt.Sprintf("%v", r))
				}
			}()
			ExecuteHandler(h, payload)
		}(handlers[i])
	}

	wg.Wait()
}

// --- Output Construction ---

// buildPermissionJSON builds the exact JSON output for block/confirm guard decisions.
// Keys are in the exact order required by the spec.
func buildPermissionJSON(decision, reason string) string {
	// Use a struct with ordered fields to guarantee key order
	type hookOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	}
	type output struct {
		HookSpecificOutput hookOutput `json:"hookSpecificOutput"`
	}

	data := output{
		HookSpecificOutput: hookOutput{
			HookEventName:            EventPreToolUse,
			PermissionDecision:       decision,
			PermissionDecisionReason: reason,
		},
	}

	b, err := json.Marshal(data)
	if err != nil {
		// Should never happen — fallback
		Logger().Error("failed to marshal permission JSON", "error", err)
		return fmt.Sprintf(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"%s","permissionDecisionReason":"%s"}}`, decision, reason)
	}
	return string(b)
}

// buildContextJSON builds the JSON output for additionalContext injection.
func buildContextJSON(contextStr string) string {
	type hookOutput struct {
		AdditionalContext string `json:"additionalContext"`
	}
	type output struct {
		HookSpecificOutput hookOutput `json:"hookSpecificOutput"`
	}

	data := output{
		HookSpecificOutput: hookOutput{
			AdditionalContext: contextStr,
		},
	}

	b, err := json.Marshal(data)
	if err != nil {
		Logger().Error("failed to marshal context JSON", "error", err)
		return ""
	}
	return string(b)
}

// buildGuardBlockJSON builds JSON output for a handler-based guard block.
func buildGuardBlockJSON(reason string) string {
	type blockOutput struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	data := blockOutput{Decision: "block", Reason: reason}
	b, err := json.Marshal(data)
	if err != nil {
		return `{"decision":"block","reason":"Blocked by guard rule"}`
	}
	return string(b)
}

// --- Payload Conversion ---

// payloadToMap converts a HookPayload to map[string]interface{} for guard evaluation.
// This allows field paths like "tool_input.command" to resolve correctly.
func payloadToMap(payload HookPayload) map[string]interface{} {
	// Marshal then unmarshal to get a clean map representation
	// This handles the struct tags correctly
	data, err := json.Marshal(payload)
	if err != nil {
		return map[string]interface{}{}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]interface{}{}
	}

	// Merge in Extra fields (these are captured by custom UnmarshalJSON if present)
	for k, v := range payload.Extra {
		if _, exists := result[k]; !exists {
			result[k] = v
		}
	}

	return result
}

// mergeContext combines warn context (from guard rules) with handler context.
// Warn context takes precedence (appears first).
func mergeContext(warnContext, handlerContext string) string {
	if warnContext == "" && handlerContext == "" {
		return ""
	}
	if warnContext != "" && handlerContext != "" {
		return warnContext + "\n\n" + handlerContext
	}
	if warnContext != "" {
		return warnContext
	}
	return handlerContext
}
