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

// --- Config Types ---

// DispatchConfig holds settings for the dispatch pipeline.
type DispatchConfig struct {
	TimeoutMs int `yaml:"timeout_ms" json:"timeoutMs"`
}

// HooksConfig is the top-level configuration structure.
type HooksConfig struct {
	Version          int                    `yaml:"version" json:"version"`
	Guards           []GuardRuleConfig      `yaml:"guards" json:"guards"`
	Coaching         CoachingConfig         `yaml:"coaching" json:"coaching"`
	Analytics        AnalyticsConfig        `yaml:"analytics" json:"analytics"`
	Greeting         GreetingConfig         `yaml:"greeting" json:"greeting"`
	Sounds           SoundsConfig           `yaml:"sounds" json:"sounds"`
	StatusLine       StatusLineConfig       `yaml:"status_line" json:"statusLine"`
	CostTracking     CostTrackingConfig     `yaml:"cost_tracking" json:"costTracking"`
	TranscriptBackup TranscriptConfig       `yaml:"transcript_backup" json:"transcriptBackup"`
	Handlers         []CustomHandlerConfig  `yaml:"handlers" json:"handlers"`
	Settings         SettingsConfig         `yaml:"settings" json:"settings"`
	Includes         []string               `yaml:"includes" json:"includes"`
	Feeds            FeedsConfig            `yaml:"feeds" json:"feeds"`
	Daemon           DaemonConfig           `yaml:"daemon" json:"daemon"`
	TUI              TUIConfig              `yaml:"tui" json:"tui"`
	Dispatch         DispatchConfig         `yaml:"dispatch" json:"dispatch"`
}

type CoachingConfig struct {
	Metacognition MetacognitionConfig `yaml:"metacognition" json:"metacognition"`
	BuilderTrap   BuilderTrapConfig   `yaml:"builder_trap" json:"builderTrap"`
	Communication CommunicationConfig `yaml:"communication" json:"communication"`
}

type MetacognitionConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	IntervalSeconds int    `yaml:"interval_seconds" json:"intervalSeconds"`
	PromptsFile     string `yaml:"prompts_file,omitempty" json:"promptsFile,omitempty"`
}

type BuilderTrapConfig struct {
	Enabled         bool                `yaml:"enabled" json:"enabled"`
	Thresholds      BuilderTrapThresholds `yaml:"thresholds" json:"thresholds"`
	ToolingPatterns []string            `yaml:"tooling_patterns" json:"toolingPatterns"`
	PracticeTools   []string            `yaml:"practice_tools" json:"practiceTools"`
}

type BuilderTrapThresholds struct {
	Yellow int `yaml:"yellow" json:"yellow"`
	Orange int `yaml:"orange" json:"orange"`
	Red    int `yaml:"red" json:"red"`
}

type CommunicationConfig struct {
	Enabled   bool     `yaml:"enabled" json:"enabled"`
	Frequency int      `yaml:"frequency" json:"frequency"`
	MinLength int      `yaml:"min_length" json:"minLength"`
	Rules     []string `yaml:"rules" json:"rules"`
	Tone      string   `yaml:"tone" json:"tone"` // "gentle", "direct", "silent"
}

type AnalyticsConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	DBPath  string `yaml:"db_path,omitempty" json:"dbPath,omitempty"`
}

type GreetingConfig struct {
	Enabled    bool                         `yaml:"enabled" json:"enabled"`
	QuotesFile string                       `yaml:"quotes_file,omitempty" json:"quotesFile,omitempty"`
	Categories map[string]QuoteCategoryConfig `yaml:"categories,omitempty" json:"categories,omitempty"`
}

type QuoteCategoryConfig struct {
	Weight int          `yaml:"weight" json:"weight"`
	Quotes []QuoteEntry `yaml:"quotes" json:"quotes"`
}

type QuoteEntry struct {
	Text   string `yaml:"text" json:"text"`
	Author string `yaml:"author,omitempty" json:"author,omitempty"`
}

type SoundsConfig struct {
	Enabled      bool   `yaml:"enabled" json:"enabled"`
	Notification string `yaml:"notification,omitempty" json:"notification,omitempty"`
	Completion   string `yaml:"completion,omitempty" json:"completion,omitempty"`
}

type StatusLineConfig struct {
	Enabled   bool            `yaml:"enabled" json:"enabled"`
	Segments  []SegmentConfig `yaml:"segments" json:"segments"`
	Delimiter string          `yaml:"delimiter" json:"delimiter"`
	CachePath string          `yaml:"cache_path" json:"cachePath"`
}

type CostTrackingConfig struct {
	Enabled     bool               `yaml:"enabled" json:"enabled"`
	Rates       map[string]float64 `yaml:"rates" json:"rates"`
	DailyBudget float64            `yaml:"daily_budget" json:"dailyBudget"`
	Enforcement string             `yaml:"enforcement" json:"enforcement"` // "warn" or "enforce"
}

type TranscriptConfig struct {
	Enabled   bool   `yaml:"enabled" json:"enabled"`
	BackupDir string `yaml:"backup_dir" json:"backupDir"`
	MaxSizeMB int    `yaml:"max_size_mb" json:"maxSizeMb"`
}

type SettingsConfig struct {
	LogLevel              string `yaml:"log_level" json:"logLevel"`       // "debug", "info", "warn", "error"
	HandlerTimeoutSeconds int    `yaml:"handler_timeout_seconds" json:"handlerTimeoutSeconds"`
	StateDir              string `yaml:"state_dir" json:"stateDir"`
}

// --- Guard Types ---

type GuardRuleConfig struct {
	Match  string `yaml:"match" json:"match"`
	Action string `yaml:"action" json:"action"` // "block", "warn", "confirm"
	Reason string `yaml:"reason" json:"reason"`
	When   string `yaml:"when,omitempty" json:"when,omitempty"`
	Unless string `yaml:"unless,omitempty" json:"unless,omitempty"`
}

type GuardResult struct {
	Action      string           `json:"action"` // "allow", "block", "warn", "confirm"
	Reason      string           `json:"reason,omitempty"`
	MatchedRule *GuardRuleConfig `json:"matchedRule,omitempty"`
}

type ParsedCondition struct {
	FieldPath string `json:"fieldPath"`
	Operator  string `json:"operator"` // "contains", "starts_with", "ends_with", "==", "equals", "matches"
	Value     string `json:"value"`
}

// --- Handler Types ---

type ResolvedHandler struct {
	Name        string
	HandlerType string   // "builtin", "script", "inline"
	Events      []string // event types this handler listens for
	Module      string
	Command     string
	Action      map[string]interface{}
	Timeout     int    // milliseconds
	Phase       string // "guard", "context", "side_effect"
	ConfigRaw   map[string]interface{}
}

// HasEvent returns true if the handler listens for the given event type.
func (h *ResolvedHandler) HasEvent(eventType string) bool {
	for _, e := range h.Events {
		if e == eventType {
			return true
		}
	}
	return false
}

type CustomHandlerConfig struct {
	Name    string   `yaml:"name" json:"name"`
	Type    string   `yaml:"type" json:"type"` // "builtin", "script", "inline"
	Events  []string `yaml:"events" json:"events"`
	Phase   string   `yaml:"phase,omitempty" json:"phase,omitempty"`
	Timeout int      `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Module  string   `yaml:"module,omitempty" json:"module,omitempty"`
	Command string   `yaml:"command,omitempty" json:"command,omitempty"`
	Action  map[string]interface{} `yaml:"action,omitempty" json:"action,omitempty"`
}

// --- Segment Config ---

type SegmentConfig struct {
	Builtin string              `yaml:"builtin,omitempty" json:"builtin,omitempty"`
	Custom  *CustomSegmentConfig `yaml:"custom,omitempty" json:"custom,omitempty"`
}

// UnmarshalYAML allows SegmentConfig to accept both a plain string
// (e.g., "- session") and a full struct (e.g., {builtin: "session"}).
func (s *SegmentConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try plain string first.
	var str string
	if err := unmarshal(&str); err == nil {
		s.Builtin = str
		return nil
	}
	// Fall back to struct.
	type raw SegmentConfig
	return unmarshal((*raw)(s))
}

type CustomSegmentConfig struct {
	Command string `yaml:"command" json:"command"`
	Label   string `yaml:"label,omitempty" json:"label,omitempty"`
	Timeout int    `yaml:"timeout,omitempty" json:"timeout,omitempty"`
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

// --- Analytics Types ---

type AIClassification string

const (
	AIClassHighProbability AIClassification = "high_probability_ai"
	AIClassLikelyAI       AIClassification = "likely_ai"
	AIClassMixedVerified  AIClassification = "mixed_verified"
	AIClassHumanAuthored  AIClassification = "human_authored"
)

type AIConfidenceScore struct {
	Score          float64          `json:"score"`
	Classification AIClassification `json:"classification"`
}

type AnalyticsEvent struct {
	SessionID       string  `json:"sessionId"`
	EventType       string  `json:"eventType"`
	ToolName        string  `json:"toolName,omitempty"`
	Timestamp       string  `json:"timestamp"`
	FilePath        string  `json:"filePath,omitempty"`
	LinesAdded      int     `json:"linesAdded,omitempty"`
	LinesRemoved    int     `json:"linesRemoved,omitempty"`
	AIConfidenceScore *float64 `json:"aiConfidenceScore,omitempty"`
}

type SessionSummary struct {
	TotalToolCalls     int     `json:"totalToolCalls"`
	FileEditsCount     int     `json:"fileEditsCount"`
	AIAuthoredLines    int     `json:"aiAuthoredLines"`
	HumanVerifiedLines int     `json:"humanVerifiedLines"`
	Classification     string  `json:"classification,omitempty"`
	EstimatedCostUSD   float64 `json:"estimatedCostUsd,omitempty"`
}

type AuthorshipSummary struct {
	TotalEntries            int                        `json:"totalEntries"`
	TotalLinesChanged       int                        `json:"totalLinesChanged"`
	WeightedAIScore         float64                    `json:"weightedAIScore"`
	ClassificationBreakdown map[AIClassification]int   `json:"classificationBreakdown"`
}

type StatsOptions struct {
	SessionID string `json:"sessionId,omitempty"`
	Days      int    `json:"days,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
}

// --- Coaching Types ---
//
// Runtime coaching types (Mode, AlertLevel, CoachingCache, etc.) have been
// extracted to internal/coaching/types.go.  Config types (CoachingConfig,
// MetacognitionConfig, BuilderTrapConfig, etc.) remain here to avoid
// circular imports.

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

// --- Feed Platform Types ---

type CacheEntry struct {
	UpdatedAt  string `json:"updated_at"`
	TTLSeconds int    `json:"ttl_seconds"`
}

type FeedDefinition struct {
	Name            string `json:"name"`
	IntervalSeconds int    `json:"intervalSeconds"`
	Enabled         bool   `json:"enabled"`
}

type ProjectFeedConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	IntervalSeconds int    `yaml:"interval_seconds" json:"intervalSeconds"`
	ShowBranch      bool   `yaml:"show_branch" json:"showBranch"`
	ShowLastCommit  bool   `yaml:"show_last_commit" json:"showLastCommit"`
}

type CalendarFeedConfig struct {
	Enabled          bool     `yaml:"enabled" json:"enabled"`
	IntervalSeconds  int      `yaml:"interval_seconds" json:"intervalSeconds"`
	LookaheadMinutes int      `yaml:"lookahead_minutes" json:"lookaheadMinutes"`
	Calendars        []string `yaml:"calendars" json:"calendars"`
	CredentialsPath  string   `yaml:"credentials_path" json:"credentialsPath"`
	TokenPath        string   `yaml:"token_path" json:"tokenPath"`
}

type NewsFeedConfig struct {
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	Source           string `yaml:"source" json:"source"` // "hackernews" or "rss"
	RSSUrl           string `yaml:"rss_url" json:"rssUrl"`
	IntervalSeconds  int    `yaml:"interval_seconds" json:"intervalSeconds"`
	MaxStories       int    `yaml:"max_stories" json:"maxStories"`
	RotationMinutes  int    `yaml:"rotation_minutes" json:"rotationMinutes"`
}

type CustomFeedConfig struct {
	Name            string `yaml:"name" json:"name"`
	Command         string `yaml:"command" json:"command"`
	IntervalSeconds int    `yaml:"interval_seconds" json:"intervalSeconds"`
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	TimeoutSeconds  int    `yaml:"timeout_seconds" json:"timeoutSeconds"`
}

type InsightsFeedConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	IntervalSeconds int    `yaml:"interval_seconds" json:"intervalSeconds"`
	StalenessDays   int    `yaml:"staleness_days" json:"stalenessDays"`
	UsageDataPath   string `yaml:"usage_data_path" json:"usageDataPath"`
}

type WeatherFeedConfig struct {
	Enabled         bool    `yaml:"enabled" json:"enabled"`
	IntervalSeconds int     `yaml:"interval_seconds" json:"intervalSeconds"`
	Latitude        float64 `yaml:"latitude" json:"latitude"`
	Longitude       float64 `yaml:"longitude" json:"longitude"`
	TemperatureUnit string  `yaml:"temperature_unit" json:"temperatureUnit"` // "fahrenheit" or "celsius"
}

type MemoriesFeedConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	IntervalSeconds int    `yaml:"interval_seconds" json:"intervalSeconds"`
	DBPath          string `yaml:"db_path" json:"dbPath"`
}

type FeedsConfig struct {
	Project  ProjectFeedConfig  `yaml:"project" json:"project"`
	Calendar CalendarFeedConfig `yaml:"calendar" json:"calendar"`
	News     NewsFeedConfig     `yaml:"news" json:"news"`
	Insights InsightsFeedConfig `yaml:"insights" json:"insights"`
	Weather  WeatherFeedConfig  `yaml:"weather" json:"weather"`
	Memories MemoriesFeedConfig `yaml:"memories" json:"memories"`
	Custom   []CustomFeedConfig `yaml:"custom" json:"custom"`
}

type DaemonConfig struct {
	AutoStart                bool   `yaml:"auto_start" json:"autoStart"`
	InactivityTimeoutMinutes int    `yaml:"inactivity_timeout_minutes" json:"inactivityTimeoutMinutes"`
	LogFile                  string `yaml:"log_file" json:"logFile"`
}

type TUIConfig struct {
	AutoLaunch   bool   `yaml:"auto_launch" json:"autoLaunch"`
	LaunchMethod string `yaml:"launch_method" json:"launchMethod"` // "newWindow" or "background"
}
