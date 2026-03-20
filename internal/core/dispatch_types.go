package core

// HookPayload is the JSON payload piped to stdin by Claude Code for each hook invocation.
type HookPayload struct {
	SessionID string                 `json:"session_id"`
	ToolName  string                 `json:"tool_name,omitempty"`
	ToolInput map[string]interface{} `json:"tool_input,omitempty"`
	Extra     map[string]interface{} `json:"-"` // captures unknown fields
}

// IsValidPayload checks that the payload has the required session_id field.
func (p *HookPayload) IsValidPayload() bool {
	return p.SessionID != ""
}

// DispatchResult is the output of the dispatch pipeline.
type DispatchResult struct {
	Stdout   *string `json:"stdout"`
	Stderr   *string `json:"stderr"`
	ExitCode int     `json:"exitCode"` // 0 or 2
}

// HandlerResult is the output of a single handler execution.
type HandlerResult struct {
	Decision          *string                `json:"decision"`          // "block", "warn", "confirm", or nil
	Reason            *string                `json:"reason"`
	AdditionalContext *string                `json:"additionalContext"`
	Output            map[string]interface{} `json:"output"`
}
