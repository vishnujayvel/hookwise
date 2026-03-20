package core

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
