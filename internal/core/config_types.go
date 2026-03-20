package core

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

type DaemonConfig struct {
	AutoStart                bool   `yaml:"auto_start" json:"autoStart"`
	InactivityTimeoutMinutes int    `yaml:"inactivity_timeout_minutes" json:"inactivityTimeoutMinutes"`
	LogFile                  string `yaml:"log_file" json:"logFile"`
}

type TUIConfig struct {
	AutoLaunch   bool   `yaml:"auto_launch" json:"autoLaunch"`
	LaunchMethod string `yaml:"launch_method" json:"launchMethod"` // "newWindow" or "background"
}
