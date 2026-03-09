package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// --- Action and Phase Constants ---

const (
	ActionAllow   = "allow"
	ActionBlock   = "block"
	ActionWarn    = "warn"
	ActionConfirm = "confirm"

	PhaseGuard      = "guard"
	PhaseContext     = "context"
	PhaseSideEffect = "side_effect"
)

// Package-level lookup maps (static data, allocated once).
var (
	guardEventsSet = map[string]bool{
		EventPreToolUse:        true,
		EventUserPromptSubmit:  true,
		EventPermissionRequest: true,
	}
	contextEventsSet = map[string]bool{
		EventSessionStart:  true,
		EventSubagentStart: true,
	}
	phaseOrderMap = map[string]int{
		PhaseGuard:      0,
		PhaseContext:    1,
		PhaseSideEffect: 2,
	}
)

// --- Phase Inference ---

// inferPhase determines the execution phase of a handler based on its config.
// Returns the explicit phase if set, otherwise infers from event types:
//   - guard: PreToolUse, UserPromptSubmit, PermissionRequest (non-inline)
//   - context: SessionStart, SubagentStart
//   - side_effect: everything else
func inferPhase(handler CustomHandlerConfig) string {
	if handler.Phase != "" {
		return handler.Phase
	}

	isGuardEvent := false
	isContextEvent := false
	for _, e := range handler.Events {
		if guardEventsSet[e] {
			isGuardEvent = true
		}
		if contextEventsSet[e] {
			isContextEvent = true
		}
	}

	if isGuardEvent && handler.Type != "inline" {
		return PhaseGuard
	}
	if isContextEvent {
		return PhaseContext
	}
	return PhaseSideEffect
}

// --- Handler Resolution ---

// ResolveHandlers converts CustomHandlerConfig entries from a HooksConfig into
// ResolvedHandler instances. It infers the phase if not explicitly set and
// computes the timeout in milliseconds.
func ResolveHandlers(config HooksConfig) []ResolvedHandler {
	resolved := make([]ResolvedHandler, 0, len(config.Handlers))
	defaultTimeoutMs := config.Settings.HandlerTimeoutSeconds * 1000
	if defaultTimeoutMs <= 0 {
		defaultTimeoutMs = DefaultHandlerTimeout * 1000
	}

	for _, h := range config.Handlers {
		events := make([]string, len(h.Events))
		copy(events, h.Events)

		timeoutMs := defaultTimeoutMs
		if h.Timeout > 0 {
			timeoutMs = h.Timeout * 1000
		}

		resolved = append(resolved, ResolvedHandler{
			Name:        h.Name,
			HandlerType: h.Type,
			Events:      events,
			Module:      h.Module,
			Command:     h.Command,
			Action:      h.Action,
			Timeout:     timeoutMs,
			Phase:       inferPhase(h),
			ConfigRaw:   nil,
		})
	}

	return resolved
}

// GetHandlersForEvent filters handlers by event type and sorts them by phase:
// guard (0) < context (1) < side_effect (2).
func GetHandlersForEvent(handlers []ResolvedHandler, eventType string) []ResolvedHandler {
	var matching []ResolvedHandler
	for i := range handlers {
		if handlers[i].HasEvent(eventType) {
			matching = append(matching, handlers[i])
		}
	}

	// Stable sort by phase order
	for i := 1; i < len(matching); i++ {
		for j := i; j > 0 && phaseOrderMap[matching[j].Phase] < phaseOrderMap[matching[j-1].Phase]; j-- {
			matching[j], matching[j-1] = matching[j-1], matching[j]
		}
	}

	return matching
}

// --- Handler Execution ---

// ExecuteHandler executes a single handler with error boundary.
// Returns a HandlerResult; on any error, returns an empty result (fail-open).
func ExecuteHandler(ctx context.Context, handler ResolvedHandler, payload HookPayload) HandlerResult {
	defer func() {
		if r := recover(); r != nil {
			Logger().Error("panic in handler execution", "handler", handler.Name, "recovered", fmt.Sprintf("%v", r))
		}
	}()

	switch handler.HandlerType {
	case "builtin":
		return executeBuiltinHandler(ctx, handler, payload)
	case "script":
		return executeScriptHandler(ctx, handler, payload)
	case "inline":
		return executeInlineHandler(ctx, handler)
	default:
		Logger().Error("unknown handler type", "type", handler.HandlerType, "handler", handler.Name)
		return emptyHandlerResult()
	}
}

// executeBuiltinHandler handles builtin-type handlers.
// If the handler has an action object, return it as the result.
func executeBuiltinHandler(ctx context.Context, handler ResolvedHandler, payload HookPayload) HandlerResult {
	Logger().Debug("executing builtin handler", "handler", handler.Name, "module", handler.Module)

	if handler.Action != nil {
		return actionToResult(handler.Action)
	}
	return emptyHandlerResult()
}

// executeScriptHandler runs a script handler via os/exec, piping payload JSON to stdin.
// Timeout is enforced via context.WithTimeout.
//
// Exit codes:
//   - 0: success, parse stdout as HandlerResult
//   - 2: block only if stdout has valid block JSON (decision: "block")
//   - Other: error, log and skip
func executeScriptHandler(ctx context.Context, handler ResolvedHandler, payload HookPayload) HandlerResult {
	command := handler.Command
	if command == "" {
		Logger().Error("script handler has no command", "handler", handler.Name)
		return emptyHandlerResult()
	}

	Logger().Debug("executing script handler", "handler", handler.Name, "command", command, "timeout", handler.Timeout)

	// Prepare payload JSON for stdin
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		Logger().Error("failed to marshal payload for script handler", "handler", handler.Name, "error", err)
		return emptyHandlerResult()
	}

	// Split command into parts
	parts := strings.Fields(command)
	if len(parts) == 0 {
		Logger().Error("script handler command is empty after splitting", "handler", handler.Name)
		return emptyHandlerResult()
	}

	// Set up timeout
	timeoutMs := handler.Timeout
	if timeoutMs <= 0 {
		timeoutMs = DefaultHandlerTimeout * 1000
	}
	handlerCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(handlerCtx, parts[0], parts[1:]...)
	cmd.Stdin = bytes.NewReader(payloadJSON)
	// Ensure the process group is killed on cancel so child processes (e.g., sleep) are also terminated
	cmd.Cancel = func() error {
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 100 * time.Millisecond

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	// Check for timeout (context deadline or process killed by signal)
	if handlerCtx.Err() == context.DeadlineExceeded {
		Logger().Error("handler timed out", "handler", handler.Name, "timeout_ms", timeoutMs)
		return emptyHandlerResult()
	}

	// Get exit code
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			// Process killed by signal (e.g., from timeout) — treat as timeout/error
			if exitCode == -1 {
				Logger().Error("handler killed by signal", "handler", handler.Name)
				return emptyHandlerResult()
			}
		} else {
			Logger().Error("handler execution error", "handler", handler.Name, "error", err)
			return emptyHandlerResult()
		}
	}

	// Non-zero exit (not 0 or 2) -> error
	if exitCode != 0 && exitCode != 2 {
		Logger().Error("handler exited with unexpected code", "handler", handler.Name, "exitCode", exitCode, "stderr", stderr.String())
		return emptyHandlerResult()
	}

	// Parse stdout
	stdoutStr := strings.TrimSpace(stdout.String())
	if stdoutStr == "" {
		return emptyHandlerResult()
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(stdoutStr), &parsed); err != nil {
		// Non-JSON stdout
		if exitCode == 2 {
			Logger().Error("handler exited 2 but stdout is not valid JSON", "handler", handler.Name, "stdout", stdoutStr)
		}
		return emptyHandlerResult()
	}

	// Exit code 2 is only a block if stdout has decision: "block"
	if exitCode == 2 {
		decision, _ := parsed["decision"].(string)
		if decision != "block" {
			Logger().Error("handler exited 2 but stdout is not a block", "handler", handler.Name, "stdout", stdoutStr)
			return emptyHandlerResult()
		}
	}

	return actionToResult(parsed)
}

// executeInlineHandler evaluates a handler's static action object.
func executeInlineHandler(ctx context.Context, handler ResolvedHandler) HandlerResult {
	Logger().Debug("executing inline handler", "handler", handler.Name)

	if handler.Action == nil {
		return emptyHandlerResult()
	}
	return actionToResult(handler.Action)
}

// --- Helper functions ---

func emptyHandlerResult() HandlerResult {
	return HandlerResult{}
}

func actionToResult(action map[string]interface{}) HandlerResult {
	result := HandlerResult{}
	if v, ok := action["decision"].(string); ok {
		result.Decision = &v
	}
	if v, ok := action["reason"].(string); ok {
		result.Reason = &v
	}
	if v, ok := action["additionalContext"].(string); ok {
		result.AdditionalContext = &v
	}
	if v, ok := action["output"].(map[string]interface{}); ok {
		result.Output = v
	}
	return result
}

