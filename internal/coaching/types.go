// Package coaching is the domain package for builder's trap detection,
// metacognition prompts, and communication coaching.
//
// Runtime types live here; config types (CoachingConfig, MetacognitionConfig,
// BuilderTrapConfig, etc.) remain in internal/core to avoid circular imports.
package coaching

// ---------------------------------------------------------------------------
// Mode — the developer's current working mode.
// ---------------------------------------------------------------------------

// Mode classifies what the developer is doing right now.
type Mode string

const (
	ModeCoding   Mode = "coding"
	ModeTooling  Mode = "tooling"
	ModePractice Mode = "practice"
	ModePrep     Mode = "prep"
	ModeNeutral  Mode = "neutral"
)

// String implements fmt.Stringer.
func (m Mode) String() string { return string(m) }

// ---------------------------------------------------------------------------
// AlertLevel — builder's trap escalation level.
// ---------------------------------------------------------------------------

// AlertLevel represents the severity of a builder's trap alert.
type AlertLevel string

const (
	AlertNone   AlertLevel = "none"
	AlertYellow AlertLevel = "yellow"
	AlertOrange AlertLevel = "orange"
	AlertRed    AlertLevel = "red"
)

// String implements fmt.Stringer.
func (a AlertLevel) String() string { return string(a) }

// ---------------------------------------------------------------------------
// Runtime structs
// ---------------------------------------------------------------------------

// LargeChangeRecord captures a single large-change event that may trigger
// a metacognition prompt.
type LargeChangeRecord struct {
	Timestamp             string `json:"timestamp"`
	ToolName              string `json:"toolName"`
	LinesChanged          int    `json:"linesChanged"`
	AcceptedWithinSeconds int    `json:"acceptedWithinSeconds"`
}

// CoachingCache is the in-memory snapshot of the coaching state, suitable
// for JSON round-tripping to the feed cache.
type CoachingCache struct {
	LastPromptAt    string             `json:"lastPromptAt"`
	PromptHistory   []string           `json:"promptHistory"`
	CurrentMode     Mode               `json:"currentMode"`
	ModeStartedAt   string             `json:"modeStartedAt"`
	ToolingMinutes  float64            `json:"toolingMinutes"`
	AlertLevel      AlertLevel         `json:"alertLevel"`
	TodayDate       string             `json:"todayDate"`
	PracticeCount   int                `json:"practiceCount"`
	LastLargeChange *LargeChangeRecord `json:"lastLargeChange"`
}

// MetacognitionResult is the output of a metacognition evaluation pass.
type MetacognitionResult struct {
	ShouldEmit  bool   `json:"shouldEmit"`
	PromptText  string `json:"promptText,omitempty"`
	PromptID    string `json:"promptId,omitempty"`
	Category    string `json:"category,omitempty"`
	TriggerType string `json:"triggerType,omitempty"` // "interval", "rapid_acceptance", "mode_change", "builder_trap"
}

// GrammarResult is the output of a communication coaching grammar check.
type GrammarResult struct {
	ShouldCorrect    bool           `json:"shouldCorrect"`
	Issues           []GrammarIssue `json:"issues"`
	CorrectedText    string         `json:"correctedText,omitempty"`
	ImprovementScore float64        `json:"improvementScore,omitempty"`
}

// GrammarIssue describes a single grammar or style issue found.
type GrammarIssue struct {
	Rule       string `json:"rule"`
	Original   string `json:"original"`
	Suggestion string `json:"suggestion"`
	Position   int    `json:"position"`
}
