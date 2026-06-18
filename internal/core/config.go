package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gobwas/glob"
	"gopkg.in/yaml.v3"
)

// envVarPattern matches ${VAR_NAME} patterns in strings.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// knownSections lists all valid top-level YAML config keys (snake_case).
var knownSections = map[string]bool{
	"version":           true,
	"guards":            true,
	"analytics":         true,
	"greeting":          true,
	"sounds":            true,
	"status_line":       true,
	"cost_tracking":     true,
	"handlers":          true,
	"settings":          true,
	"includes":          true,
	"feeds":             true,
	"daemon":            true,
	"tui":               true,
	"dispatch":          true,
}

// --- Default Config ---

// GetDefaultConfig returns sensible defaults matching the TypeScript implementation.
func GetDefaultConfig() HooksConfig {
	home := HomeDir()
	return HooksConfig{
		Version: 1,
		Guards:  []GuardRuleConfig{},
		Analytics: AnalyticsConfig{
			Enabled:                 true,
			SnapshotEnabled:         true,
			SnapshotIntervalMinutes: DefaultSnapshotIntervalMinutes,
			SnapshotRetention:       DefaultSnapshotRetention,
		},
		Greeting: GreetingConfig{Enabled: false},
		Sounds:   SoundsConfig{Enabled: false},
		StatusLine: StatusLineConfig{
			Enabled:   false,
			Segments:  []SegmentConfig{},
			Delimiter: DefaultStatusDelimiter,
			CachePath: DefaultCachePath,
		},
		CostTracking: CostTrackingConfig{
			Enabled:     false,
			Rates:       map[string]float64{},
			DailyBudget: 10,
			Enforcement: "warn",
		},
		Handlers: []CustomHandlerConfig{},
		Settings: SettingsConfig{
			LogLevel:              "info",
			HandlerTimeoutSeconds: DefaultHandlerTimeout,
			StateDir:              DefaultStateDir,
		},
		Includes: []string{},
		Feeds: FeedsConfig{
			Project: ProjectFeedConfig{
				Enabled:         true,
				IntervalSeconds: 60,
				ShowBranch:      true,
				ShowLastCommit:  true,
			},
			Calendar: CalendarFeedConfig{
				Enabled:          false,
				IntervalSeconds:  300,
				LookaheadMinutes: 120,
				Calendars:        []string{"primary"},
				CredentialsPath:  DefaultCalendarCredentialsPath,
				TokenPath:        DefaultCalendarTokenPath,
			},
			News: NewsFeedConfig{
				Enabled:         false,
				Source:          "hackernews",
				IntervalSeconds: 1800,
				MaxStories:      5,
				RotationMinutes: 30,
			},
			Insights: InsightsFeedConfig{
				Enabled:         true,
				IntervalSeconds: 120,
				StalenessDays:   30,
				UsageDataPath:   filepath.Join(home, ".claude", "usage-data"),
			},
			Weather: WeatherFeedConfig{
				Enabled:         false,
				IntervalSeconds: 600,
				Latitude:        37.7749,
				Longitude:       -122.4194,
				TemperatureUnit: "fahrenheit",
			},
			Memories: MemoriesFeedConfig{
				Enabled:         false,
				IntervalSeconds: 3600,
				DBPath:          DefaultDBPath,
			},
			Custom: []CustomFeedConfig{},
		},
		Daemon: DaemonConfig{
			AutoStart:                true,
			InactivityTimeoutMinutes: 120,
			LogFile:                  DefaultDaemonLogPath,
		},
		TUI: TUIConfig{
			AutoLaunch:   false,
			LaunchMethod: "newWindow",
		},
		Dispatch: DispatchConfig{
			TimeoutMs: DefaultDispatchTimeoutMs,
		},
	}
}

// --- YAML Parsing ---

// parseYAML parses a YAML byte slice into a map. Returns an error on malformed YAML.
func parseYAML(data []byte, filePath string) (map[string]interface{}, error) {
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, NewConfigError(fmt.Sprintf("failed to parse YAML in %s: %v", filePath, err))
	}
	if raw == nil {
		return map[string]interface{}{}, nil
	}
	return raw, nil
}

// readYAMLFile reads and parses a YAML config file.
// Returns (nil, nil) if the file does not exist.
func readYAMLFile(filePath string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, NewConfigError(fmt.Sprintf("failed to read config file %s: %v", filePath, err))
	}
	return parseYAML(data, filePath)
}

// --- Deep Merge ---

// DeepMerge deep-merges source into target. Source values win.
// For nested maps: merge recursively.
// For slices/arrays: source REPLACES target (no concatenation).
// For scalars: source wins.
func DeepMerge(target, source map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(target))
	for k, v := range target {
		result[k] = v
	}

	for key, sourceValue := range source {
		targetValue, exists := result[key]
		if !exists {
			result[key] = sourceValue
			continue
		}

		sourceMap, sourceIsMap := sourceValue.(map[string]interface{})
		targetMap, targetIsMap := targetValue.(map[string]interface{})

		if sourceIsMap && targetIsMap {
			// Both are maps: recurse
			result[key] = DeepMerge(targetMap, sourceMap)
		} else {
			// Arrays, primitives, nil: source replaces target
			result[key] = sourceValue
		}
	}

	return result
}

// --- Environment Variable Interpolation ---

// InterpolateEnvVars recursively substitutes ${VAR_NAME} patterns in string values.
// If the environment variable is NOT defined, the literal ${VAR_NAME} is preserved.
func InterpolateEnvVars(data interface{}) interface{} {
	switch v := data.(type) {
	case string:
		return envVarPattern.ReplaceAllStringFunc(v, func(match string) string {
			// Extract variable name from ${VAR_NAME}
			varName := match[2 : len(match)-1]
			if val, ok := os.LookupEnv(varName); ok {
				return val
			}
			// Undefined: leave the literal ${VAR_NAME}
			return match
		})
	case map[string]interface{}:
		result := make(map[string]interface{}, len(v))
		for key, val := range v {
			result[key] = InterpolateEnvVars(val)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = InterpolateEnvVars(item)
		}
		return result
	default:
		return data
	}
}

// --- Include Resolution ---

// ResolveIncludes resolves `includes` directives in a config map.
// Include paths are resolved relative to configDir.
// Included files are deep-merged into the config (included values override).
// The `includes` key from included files is stripped to prevent cycles.
func ResolveIncludes(config map[string]interface{}, configDir string) (map[string]interface{}, error) {
	includesRaw, ok := config["includes"]
	if !ok {
		return config, nil
	}

	includes, ok := includesRaw.([]interface{})
	if !ok {
		return config, nil
	}
	if len(includes) == 0 {
		return config, nil
	}

	merged := make(map[string]interface{}, len(config))
	for k, v := range config {
		merged[k] = v
	}

	for _, inc := range includes {
		includePath, ok := inc.(string)
		if !ok {
			continue
		}

		// Resolve relative paths against configDir
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(configDir, includePath)
		}

		includeRaw, err := readYAMLFile(includePath)
		if err != nil {
			// Log but continue — included file errors are non-fatal
			Logger().Warn("failed to load include", "path", includePath, "error", err)
			continue
		}
		if includeRaw == nil {
			// File doesn't exist — skip silently
			continue
		}

		// Strip the included file's own includes to prevent cycles
		delete(includeRaw, "includes")

		merged = DeepMerge(merged, includeRaw)
	}

	return merged, nil
}

// --- Config Loading ---

// LoadConfig loads hookwise configuration with the full resolution pipeline:
//  1. Read global config (~/.hookwise/config.yaml)
//  2. Read project config (<projectDir>/hookwise.yaml)
//  3. Deep-merge: project values override global values
//  4. Resolve includes (relative to project dir)
//  5. Interpolate environment variables
//  6. Unmarshal into HooksConfig struct (with defaults backfill)
func LoadConfig(projectDir string) (HooksConfig, error) {
	projectConfigPath := filepath.Join(projectDir, ProjectConfigFile)
	globalConfigPath := filepath.Join(GetStateDir(), "config.yaml")

	// Step 1: Read raw YAML files
	globalRaw, err := readYAMLFile(globalConfigPath)
	if err != nil {
		return HooksConfig{}, fmt.Errorf("reading global config: %w", err)
	}

	projectRaw, err := readYAMLFile(projectConfigPath)
	if err != nil {
		return HooksConfig{}, fmt.Errorf("reading project config: %w", err)
	}

	// If neither exists, return defaults
	if globalRaw == nil && projectRaw == nil {
		return GetDefaultConfig(), nil
	}

	// Step 2: Deep merge global + project (project wins)
	var merged map[string]interface{}
	if globalRaw != nil {
		merged = globalRaw
	} else {
		merged = map[string]interface{}{}
	}
	if projectRaw != nil {
		merged = DeepMerge(merged, projectRaw)
	}

	// Step 3: Resolve includes (relative to project dir if project config exists)
	includeBaseDir := projectDir
	if projectRaw == nil {
		includeBaseDir = filepath.Dir(globalConfigPath)
	}
	merged, err = ResolveIncludes(merged, includeBaseDir)
	if err != nil {
		return HooksConfig{}, fmt.Errorf("resolving includes: %w", err)
	}

	// Step 4: Interpolate environment variables
	interpolated := InterpolateEnvVars(merged)
	merged, ok := interpolated.(map[string]interface{})
	if !ok {
		return HooksConfig{}, fmt.Errorf("env var interpolation returned unexpected type %T", interpolated)
	}

	// Step 5: Marshal back to YAML and unmarshal into struct.
	// This leverages Go's yaml tags for snake_case -> struct field mapping.
	yamlBytes, err := yaml.Marshal(merged)
	if err != nil {
		return HooksConfig{}, fmt.Errorf("re-marshaling config: %w", err)
	}

	// Start with defaults so missing fields get sensible values
	config := GetDefaultConfig()
	if err := yaml.Unmarshal(yamlBytes, &config); err != nil {
		return HooksConfig{}, fmt.Errorf("unmarshaling config: %w", err)
	}

	return config, nil
}

// --- Config Validation ---

// ValidateConfig validates a raw config map (after YAML parsing).
// It reports errors with JSON path and suggestion for each problem found.
func ValidateConfig(raw map[string]interface{}) ValidationResult {
	var errors []ValidationError

	// Check for unknown top-level keys
	for key := range raw {
		if !knownSections[key] {
			errors = append(errors, ValidationError{
				Path:       key,
				Message:    fmt.Sprintf("Unknown config section: %q", key),
				Suggestion: fmt.Sprintf("Known sections: %s", strings.Join(knownSectionsList(), ", ")),
			})
		}
	}

	// Validate version
	if v, ok := raw["version"]; ok {
		switch ver := v.(type) {
		case int:
			if ver < 1 {
				errors = append(errors, ValidationError{
					Path:       "version",
					Message:    "version must be a positive number",
					Suggestion: "Set version: 1",
				})
			}
		case float64:
			if ver < 1 {
				errors = append(errors, ValidationError{
					Path:       "version",
					Message:    "version must be a positive number",
					Suggestion: "Set version: 1",
				})
			}
		default:
			errors = append(errors, ValidationError{
				Path:       "version",
				Message:    "version must be a positive number",
				Suggestion: "Set version: 1",
			})
		}
	}

	// Validate guards
	if g, ok := raw["guards"]; ok {
		guards, isSlice := g.([]interface{})
		if !isSlice {
			errors = append(errors, ValidationError{
				Path:       "guards",
				Message:    "guards must be an array",
				Suggestion: "Use: guards: [{match: '...', action: 'block', reason: '...'}]",
			})
		} else {
			for i, item := range guards {
				guard, isMap := item.(map[string]interface{})
				if !isMap {
					errors = append(errors, ValidationError{
						Path:    fmt.Sprintf("guards[%d]", i),
						Message: "guard rule must be an object",
					})
					continue
				}
				if match, ok := guard["match"]; !ok || match == nil || fmt.Sprintf("%v", match) == "" {
					errors = append(errors, ValidationError{
						Path:       fmt.Sprintf("guards[%d].match", i),
						Message:    "guard rule must have a 'match' string",
						Suggestion: "Add match: 'tool_name:Bash' or similar glob pattern",
					})
				}
				if match, ok := guard["match"]; ok && match != nil {
					matchStr := fmt.Sprintf("%v", match)
					if matchStr != "" && hasGlobChars(matchStr) {
						if _, gErr := glob.Compile(matchStr); gErr != nil {
							errors = append(errors, ValidationError{
								Path:       fmt.Sprintf("guards[%d].match", i),
								Message:    fmt.Sprintf("guard match %q is an invalid glob pattern: %v (the rule will never match any tool at runtime)", matchStr, gErr),
								Suggestion: "Fix the glob pattern (e.g. close '[' and '{' groups), or use a plain tool name for an exact match",
							})
						}
					}
				}
				action, actionOk := guard["action"]
				if !actionOk || !isValidGuardAction(fmt.Sprintf("%v", action)) {
					errors = append(errors, ValidationError{
						Path:       fmt.Sprintf("guards[%d].action", i),
						Message:    "guard rule action must be 'block', 'warn', or 'confirm'",
						Suggestion: "Set action: 'block' | 'warn' | 'confirm'",
					})
				}
				if reason, ok := guard["reason"]; !ok || reason == nil || fmt.Sprintf("%v", reason) == "" {
					errors = append(errors, ValidationError{
						Path:    fmt.Sprintf("guards[%d].reason", i),
						Message: "guard rule must have a 'reason' string",
					})
				}
				for _, condKey := range []string{"when", "unless"} {
					condVal, hasKey := guard[condKey]
					if !hasKey || condVal == nil {
						continue
					}
					expr := strings.TrimSpace(fmt.Sprintf("%v", condVal))
					if expr == "" {
						// empty/absent condition means "no condition" — not an error
						continue
					}
					parsed := ParseCondition(expr)
					if parsed == nil {
						errors = append(errors, ValidationError{
							Path:       fmt.Sprintf("guards[%d].%s", i, condKey),
							Message:    "guard condition is malformed and will be silently ignored at runtime (the rule will not match as intended)",
							Suggestion: "Use the form: field_path operator value (operators: contains, starts_with, ends_with, ==, equals, matches)",
						})
						continue
					}
					substringOps := map[string]bool{"contains": true, "starts_with": true, "ends_with": true}
					if substringOps[parsed.Operator] && parsed.Value == "" {
						errors = append(errors, ValidationError{
							Path:       fmt.Sprintf("guards[%d].%s", i, condKey),
							Message:    fmt.Sprintf("guard condition uses %q with an empty value; an empty substring is treated as a non-match and the rule will never fire (likely an accidental empty value)", parsed.Operator),
							Suggestion: "Provide a non-empty substring value, or use a different operator if a catch-all is truly intended",
						})
					}
					if parsed.Operator == "matches" {
						if _, reErr := regexp.Compile(parsed.Value); reErr != nil {
							errors = append(errors, ValidationError{
								Path:       fmt.Sprintf("guards[%d].%s", i, condKey),
								Message:    fmt.Sprintf("guard condition has an invalid regular expression %q: %v (the rule will never match at runtime)", parsed.Value, reErr),
								Suggestion: "Fix the regex pattern, or use contains/starts_with/ends_with for a literal substring match",
							})
						}
					}
				}
			}
		}
	}

	// Validate handlers
	if h, ok := raw["handlers"]; ok {
		handlers, isSlice := h.([]interface{})
		if !isSlice {
			errors = append(errors, ValidationError{
				Path:       "handlers",
				Message:    "handlers must be an array",
				Suggestion: "Use: handlers: [{name: '...', type: 'builtin', events: ['PreToolUse']}]",
			})
		} else {
			for i, item := range handlers {
				handler, isMap := item.(map[string]interface{})
				if !isMap {
					errors = append(errors, ValidationError{
						Path:    fmt.Sprintf("handlers[%d]", i),
						Message: "handler must be an object",
					})
					continue
				}
				if name, ok := handler["name"]; !ok || name == nil || fmt.Sprintf("%v", name) == "" {
					errors = append(errors, ValidationError{
						Path:    fmt.Sprintf("handlers[%d].name", i),
						Message: "handler must have a 'name' string",
					})
				}
				hType, typeOk := handler["type"]
				if !typeOk || !isValidHandlerType(fmt.Sprintf("%v", hType)) {
					errors = append(errors, ValidationError{
						Path:       fmt.Sprintf("handlers[%d].type", i),
						Message:    "handler type must be 'builtin', 'script', or 'inline'",
						Suggestion: "Set type: 'builtin' | 'script' | 'inline'",
					})
				}
				if _, ok := handler["events"]; !ok {
					errors = append(errors, ValidationError{
						Path:       fmt.Sprintf("handlers[%d].events", i),
						Message:    "handler must specify events",
						Suggestion: "Set events to one or more of: " + strings.Join(EventTypes, ", "),
					})
				}
				if ev, ok := handler["events"]; ok && ev != nil {
					var eventVals []string
					switch t := ev.(type) {
					case string:
						eventVals = []string{t}
					case []interface{}:
						for _, e := range t {
							eventVals = append(eventVals, fmt.Sprintf("%v", e))
						}
					}
					for _, e := range eventVals {
						if !IsEventType(e) {
							errors = append(errors, ValidationError{
								Path:       fmt.Sprintf("handlers[%d].events", i),
								Message:    fmt.Sprintf("handler lists %q, which is not a valid event (the handler will never fire for it)", e),
								Suggestion: "Use one of: " + strings.Join(EventTypes, ", "),
							})
						}
					}
				}
			}
		}
	}

	// Validate settings
	if s, ok := raw["settings"]; ok {
		if settings, isMap := s.(map[string]interface{}); isMap {
			if logLevel, ok := settings["log_level"]; ok {
				lvl := fmt.Sprintf("%v", logLevel)
				if !isValidLogLevel(lvl) {
					errors = append(errors, ValidationError{
						Path:       "settings.log_level",
						Message:    fmt.Sprintf("Invalid log level: %q", lvl),
						Suggestion: "Use one of: debug, info, warn, error",
					})
				}
			}
		}
	}

	// Validate includes
	if inc, ok := raw["includes"]; ok {
		if _, isSlice := inc.([]interface{}); !isSlice {
			errors = append(errors, ValidationError{
				Path:       "includes",
				Message:    "includes must be an array of file paths",
				Suggestion: "Use: includes: ['./recipes/safety.yaml']",
			})
		}
	}

	// Validate daemon
	if d, ok := raw["daemon"]; ok {
		if daemon, isMap := d.(map[string]interface{}); isMap {
			if timeout, ok := daemon["inactivity_timeout_minutes"]; ok {
				if !isPositiveNumber(timeout) {
					errors = append(errors, ValidationError{
						Path:       "daemon.inactivity_timeout_minutes",
						Message:    "inactivity_timeout_minutes must be a positive number",
						Suggestion: "Set inactivity_timeout_minutes: 120",
					})
				}
			}
		}
	}

	// Validate tui
	if t, ok := raw["tui"]; ok {
		if tui, isMap := t.(map[string]interface{}); isMap {
			if lm, ok := tui["launch_method"]; ok {
				method := fmt.Sprintf("%v", lm)
				if method != "newWindow" && method != "background" {
					errors = append(errors, ValidationError{
						Path:       "tui.launch_method",
						Message:    fmt.Sprintf("Invalid launch method: %q", method),
						Suggestion: "Use one of: newWindow, background",
					})
				}
			}
		}
	}

	// Validate feeds
	if f, ok := raw["feeds"]; ok {
		if feeds, isMap := f.(map[string]interface{}); isMap {
			validateFeeds(feeds, &errors)
		}
	}

	// Validate analytics snapshot settings
	if a, ok := raw["analytics"]; ok {
		if analytics, isMap := a.(map[string]interface{}); isMap {
			if iv, ok := analytics["snapshot_interval_minutes"]; ok && !isPositiveOrZeroNumber(iv) {
				errors = append(errors, ValidationError{
					Path:       "analytics.snapshot_interval_minutes",
					Message:    "snapshot_interval_minutes must be a non-negative number",
					Suggestion: "Set snapshot_interval_minutes: 60 (hourly); 0 uses the default",
				})
			}
			if ret, ok := analytics["snapshot_retention"]; ok && !isPositiveOrZeroNumber(ret) {
				errors = append(errors, ValidationError{
					Path:       "analytics.snapshot_retention",
					Message:    "snapshot_retention must be a non-negative number",
					Suggestion: "Set snapshot_retention: 24; 0 keeps all snapshots",
				})
			}
		}
	}

	return ValidationResult{
		Valid:  len(errors) == 0,
		Errors: errors,
	}
}

// --- Validation Helpers ---

func validateFeeds(feeds map[string]interface{}, errors *[]ValidationError) {
	// Validate feeds.news.source
	if n, ok := feeds["news"]; ok {
		if news, isMap := n.(map[string]interface{}); isMap {
			if source, ok := news["source"]; ok {
				s := fmt.Sprintf("%v", source)
				if s != "hackernews" && s != "rss" {
					*errors = append(*errors, ValidationError{
						Path:       "feeds.news.source",
						Message:    fmt.Sprintf("Invalid news source: %q", s),
						Suggestion: "Use one of: hackernews, rss",
					})
				}
			}
		}
	}

	// Validate feeds.weather
	if w, ok := feeds["weather"]; ok {
		if weather, isMap := w.(map[string]interface{}); isMap {
			if lat, ok := weather["latitude"]; ok {
				if latVal, isNum := toFloat64(lat); isNum {
					if latVal < -90 || latVal > 90 {
						*errors = append(*errors, ValidationError{
							Path:       "feeds.weather.latitude",
							Message:    "latitude must be a number between -90 and 90",
							Suggestion: "Set latitude: 37.7749",
						})
					}
				}
			}
			if lon, ok := weather["longitude"]; ok {
				if lonVal, isNum := toFloat64(lon); isNum {
					if lonVal < -180 || lonVal > 180 {
						*errors = append(*errors, ValidationError{
							Path:       "feeds.weather.longitude",
							Message:    "longitude must be a number between -180 and 180",
							Suggestion: "Set longitude: -122.4194",
						})
					}
				}
			}
			if tu, ok := weather["temperature_unit"]; ok {
				unit := fmt.Sprintf("%v", tu)
				if unit != "fahrenheit" && unit != "celsius" {
					*errors = append(*errors, ValidationError{
						Path:       "feeds.weather.temperature_unit",
						Message:    fmt.Sprintf("Invalid temperature_unit: %q", unit),
						Suggestion: "Use one of: fahrenheit, celsius",
					})
				}
			}
		}
	}
}

func isValidGuardAction(action string) bool {
	return action == "block" || action == "warn" || action == "confirm"
}

func isValidHandlerType(t string) bool {
	return t == "builtin" || t == "script" || t == "inline"
}

func isValidLogLevel(level string) bool {
	return level == "debug" || level == "info" || level == "warn" || level == "error"
}

func isPositiveNumber(v interface{}) bool {
	switch n := v.(type) {
	case int:
		return n > 0
	case float64:
		return n > 0
	default:
		return false
	}
}

func isPositiveOrZeroNumber(v interface{}) bool {
	switch n := v.(type) {
	case int:
		return n >= 0
	case float64:
		return n >= 0
	default:
		return false
	}
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}

func knownSectionsList() []string {
	sections := make([]string, 0, len(knownSections))
	for k := range knownSections {
		sections = append(sections, k)
	}
	return sections
}
