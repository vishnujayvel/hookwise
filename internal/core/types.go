// Package core contains the shared types, dispatcher, config, and guard engine.
package core

// All 13 event types supported by Claude Code hooks.
const (
	EventUserPromptSubmit = "UserPromptSubmit"
	EventPreToolUse       = "PreToolUse"
	EventPostToolUse      = "PostToolUse"
	EventPostToolUseFailure = "PostToolUseFailure"
	EventNotification     = "Notification"
	EventStop             = "Stop"
	EventSubagentStart    = "SubagentStart"
	EventSubagentStop     = "SubagentStop"
	EventPreCompact       = "PreCompact"
	EventSessionStart     = "SessionStart"
	EventSessionEnd       = "SessionEnd"
	EventPermissionRequest = "PermissionRequest"
	EventSetup            = "Setup"
)

// EventTypes is the canonical list of all supported event types.
var EventTypes = []string{
	EventUserPromptSubmit,
	EventPreToolUse,
	EventPostToolUse,
	EventPostToolUseFailure,
	EventNotification,
	EventStop,
	EventSubagentStart,
	EventSubagentStop,
	EventPreCompact,
	EventSessionStart,
	EventSessionEnd,
	EventPermissionRequest,
	EventSetup,
}

// IsEventType returns true if the value is a valid event type.
func IsEventType(value string) bool {
	for _, et := range EventTypes {
		if et == value {
			return true
		}
	}
	return false
}

// --- Validation Types ---

type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors"`
}

type ValidationError struct {
	Path       string `json:"path"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// --- Coaching Types ---
//
// Runtime coaching types (Mode, AlertLevel, CoachingCache, etc.) have been
// extracted to internal/coaching/types.go.  Config types (CoachingConfig,
// MetacognitionConfig, BuilderTrapConfig, etc.) remain in config_types.go
// to avoid circular imports.

// --- Cost Types ---

type CostEstimate struct {
	EstimatedTokens int     `json:"estimatedTokens"`
	EstimatedCostUSD float64 `json:"estimatedCostUsd"`
	Model           string  `json:"model"`
}

// --- Agent Types ---

type FileConflict struct {
	FilePath      string   `json:"filePath"`
	Agents        []string `json:"agents"`
	OverlapPeriod struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"overlapPeriod"`
}

// --- Testing Types ---

type TestScenario struct {
	ToolName  string                 `json:"toolName"`
	ToolInput map[string]interface{} `json:"toolInput,omitempty"`
	Expected  string                 `json:"expected"` // "block", "allow", "warn", "confirm"
}

type ScenarioResult struct {
	Scenario    TestScenario `json:"scenario"`
	GuardResult GuardResult  `json:"guardResult"`
	Passed      bool         `json:"passed"`
}
