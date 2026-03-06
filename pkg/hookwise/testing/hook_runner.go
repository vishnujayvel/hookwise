package hwtesting

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
)

// HookRunner invokes the hookwise binary as a subprocess for integration testing.
// It wraps os/exec to dispatch events and capture output, providing assertion
// methods on the result for ergonomic test code.
type HookRunner struct {
	binaryPath string
	configDir  string
}

// NewHookRunner creates a HookRunner pointing at the hookwise binary.
// binaryPath is the path to the compiled hookwise executable.
func NewHookRunner(binaryPath string) *HookRunner {
	return &HookRunner{
		binaryPath: binaryPath,
	}
}

// WithConfigDir sets the project directory (--project-dir) for config resolution.
// Returns the receiver for chaining.
func (hr *HookRunner) WithConfigDir(configDir string) *HookRunner {
	hr.configDir = configDir
	return hr
}

// Run dispatches an event by invoking `hookwise dispatch <eventType>` as a
// subprocess. The payload is JSON-encoded and piped to stdin.
// Returns a HookResult containing stdout, stderr, and exit code.
func (hr *HookRunner) Run(eventType string, payload map[string]interface{}) *HookResult {
	args := []string{"dispatch", eventType}
	if hr.configDir != "" {
		args = append(args, "--project-dir", hr.configDir)
	}

	cmd := exec.Command(hr.binaryPath, args...)

	// Encode payload as JSON for stdin
	if payload != nil {
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return &HookResult{
				Stderr:   "hwtesting: failed to marshal payload: " + err.Error(),
				ExitCode: -1,
			}
		}
		cmd.Stdin = bytes.NewReader(payloadJSON)
	} else {
		// Send empty JSON object if no payload
		cmd.Stdin = bytes.NewReader([]byte("{}"))
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Command failed to start (binary not found, etc.)
			return &HookResult{
				Stderr:   "hwtesting: failed to run binary: " + err.Error(),
				ExitCode: -1,
			}
		}
	}

	return &HookResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

// HookResult contains the output of a hookwise dispatch subprocess invocation.
type HookResult struct {
	// Stdout is the captured standard output from the hookwise binary.
	Stdout string
	// Stderr is the captured standard error from the hookwise binary.
	Stderr string
	// ExitCode is the process exit code (0 = success, 2 = block).
	// -1 indicates the binary could not be started.
	ExitCode int
}

// IsAllowed returns true if the hookwise dispatch did not output a deny/block
// decision. A result is considered "allowed" when stdout does not contain a
// permissionDecision of "deny" and does not contain a decision of "block".
func (r *HookResult) IsAllowed() bool {
	if r.Stdout == "" {
		return true
	}
	// Parse stdout to check for deny/block decisions
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
		// Non-JSON stdout is not a deny
		return true
	}

	// Check hookSpecificOutput.permissionDecision
	if hso, ok := parsed["hookSpecificOutput"].(map[string]interface{}); ok {
		if decision, ok := hso["permissionDecision"].(string); ok {
			if decision == "deny" {
				return false
			}
		}
	}

	// Check top-level decision (from handler-based guards)
	if decision, ok := parsed["decision"].(string); ok {
		if decision == "block" {
			return false
		}
	}

	return true
}

// IsBlocked returns true if a deny or block decision was output.
// This is the logical inverse of IsAllowed.
func (r *HookResult) IsBlocked() bool {
	return !r.IsAllowed()
}

// HasContext returns true if the stdout contains an additionalContext field
// whose value includes the given substring.
func (r *HookResult) HasContext(substring string) bool {
	if r.Stdout == "" {
		return false
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
		// Check raw stdout as fallback
		return strings.Contains(r.Stdout, substring)
	}

	// Check hookSpecificOutput.additionalContext
	if hso, ok := parsed["hookSpecificOutput"].(map[string]interface{}); ok {
		if ctx, ok := hso["additionalContext"].(string); ok {
			return strings.Contains(ctx, substring)
		}
	}

	// Check top-level additionalContext
	if ctx, ok := parsed["additionalContext"].(string); ok {
		return strings.Contains(ctx, substring)
	}

	return false
}

// HasDecisionReason returns true if the stdout contains a permission decision
// reason that includes the given substring.
func (r *HookResult) HasDecisionReason(substring string) bool {
	if r.Stdout == "" {
		return false
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(r.Stdout), &parsed); err != nil {
		return false
	}

	// Check hookSpecificOutput.permissionDecisionReason
	if hso, ok := parsed["hookSpecificOutput"].(map[string]interface{}); ok {
		if reason, ok := hso["permissionDecisionReason"].(string); ok {
			return strings.Contains(reason, substring)
		}
	}

	// Check top-level reason
	if reason, ok := parsed["reason"].(string); ok {
		return strings.Contains(reason, substring)
	}

	return false
}
