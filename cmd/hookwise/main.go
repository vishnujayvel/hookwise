package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/bridge"
	"github.com/vishnujayvel/hookwise/internal/core"
	"github.com/vishnujayvel/hookwise/internal/migration"
	"github.com/vishnujayvel/hookwise/internal/notifications"
	"gopkg.in/yaml.v3"
)

// Injected at build time via -ldflags.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	rootCmd := newRootCmd()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// newRootCmd creates the root command with all subcommands attached.
// Extracted from main() so tests can invoke commands without exec-ing a binary.
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "hookwise",
		Short:   "Config-driven hook framework for Claude Code",
		Long:    "Hookwise provides guards, analytics, coaching, feeds, and an interactive TUI for Claude Code sessions.",
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, buildDate),
	}

	rootCmd.AddCommand(
		newDispatchCmd(),
		newInitCmd(),
		newDoctorCmd(),
		newStatsCmd(),
		newDiffCmd(),
		newLogCmd(),
		newStatusLineCmd(),
		newTestCmd(),
		newUpgradeCmd(),
		newNotificationsCmd(),
	)

	return rootCmd
}

// newDispatchCmd handles "hookwise dispatch <EventType>".
// Reads JSON from stdin, runs the three-phase dispatch pipeline, writes result to stdout.
func newDispatchCmd() *cobra.Command {
	var projectDir string

	cmd := &cobra.Command{
		Use:   "dispatch <EventType>",
		Short: "Dispatch a hook event (called by Claude Code)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eventType := args[0]

			var tuiLaunchMethod string // set inside SafeDispatch, used after

			result := core.SafeDispatch(func() core.DispatchResult {
				// Read payload from stdin
				payload := core.ReadStdinPayload()

				// Resolve project directory
				dir := projectDir
				if dir == "" {
					var err error
					dir, err = os.Getwd()
					if err != nil {
						dir = "."
					}
				}

				// Load config
				config, err := core.LoadConfig(dir)
				if err != nil {
					core.Logger().Error("failed to load config", "error", err)
					// Malformed config -> exit 0 silently (fail-open)
					return core.DispatchResult{ExitCode: 0}
				}

				// Run the three-phase dispatch engine
				dispatchResult := core.Dispatch(eventType, payload, config)

				// Analytics recording (non-blocking, ARCH-7).
				if config.Analytics.Enabled && payload.SessionID != "" {
					go recordAnalytics(eventType, payload, config.Analytics.DBPath)
				}

				// Signal TUI launch intent (executed synchronously after SafeDispatch)
				if eventType == core.EventSessionStart && config.TUI.AutoLaunch {
					tuiLaunchMethod = config.TUI.LaunchMethod
				}

				return dispatchResult
			})

			if result.Stdout != nil {
				fmt.Print(*result.Stdout)
			}

			// Launch TUI synchronously — cmd.Start() returns immediately after
			// fork, so this doesn't block. Running it outside SafeDispatch
			// ensures the launch completes before os.Exit().
			if tuiLaunchMethod != "" {
				launchTUIIfNeeded(tuiLaunchMethod)
			}

			// Brief grace period for side-effect goroutines (analytics, coaching)
			// to finish before the process exits.
			time.Sleep(50 * time.Millisecond)
			os.Exit(result.ExitCode)
			return nil
		},
	}

	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory (defaults to cwd)")

	return cmd
}

// recordAnalytics writes session/event data to Dolt in a background goroutine.
// Fail-open: any error is logged but never surfaces to the user (ARCH-1).
func recordAnalytics(eventType string, payload core.HookPayload, dataDir string) {
	defer func() {
		if r := recover(); r != nil {
			core.Logger().Error("panic in analytics recording", "recovered", fmt.Sprintf("%v", r))
		}
	}()

	db, err := analytics.Open(dataDir)
	if err != nil {
		core.Logger().Error("analytics: failed to open DB", "error", err)
		return
	}
	defer db.Close()

	ctx := context.Background()
	a := analytics.NewAnalytics(db)
	now := time.Now()

	switch eventType {
	case core.EventSessionStart:
		if err := a.StartSession(ctx, payload.SessionID, now); err != nil {
			core.Logger().Error("analytics: start session", "error", err)
		}

	case core.EventPostToolUse:
		event := analytics.EventRecord{
			EventType: eventType,
			ToolName:  payload.ToolName,
			Timestamp: now,
		}
		if err := a.RecordEvent(ctx, payload.SessionID, event); err != nil {
			core.Logger().Error("analytics: record event", "error", err)
		}

	case core.EventSessionEnd, core.EventStop:
		if err := a.EndSession(ctx, payload.SessionID, now, analytics.SessionStats{}); err != nil {
			core.Logger().Error("analytics: end session", "error", err)
		}
	}

	// Commit to Dolt so data is visible across connections (ARCH-2).
	if _, err := db.CommitDispatch(ctx, eventType, payload.SessionID); err != nil {
		core.Logger().Error("analytics: commit", "error", err)
	}
}

// ---------------------------------------------------------------------------
// init command (R7.1)
// ---------------------------------------------------------------------------

// defaultYAMLTemplate is the minimal hookwise.yaml created by `hookwise init`.
const defaultYAMLTemplate = `# hookwise.yaml -- generated by hookwise init
# Docs: https://github.com/hookwise/hookwise
version: 1

guards: []

analytics:
  enabled: true

status_line:
  enabled: false

tui:
  auto_launch: false
  launch_method: "newWindow"

# feeds:
#   weather:
#     enabled: true
#     interval_seconds: 900
#     latitude: 37.7749
#     longitude: -122.4194
#     temperature_unit: "fahrenheit" # or "celsius"
`

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize hookwise configuration",
		Long:  "Creates a default hookwise.yaml in the current directory and ensures the ~/.hookwise/ state directory exists.",
		RunE:  runInit,
	}
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}

	configPath := filepath.Join(cwd, core.ProjectConfigFile)

	// Check if hookwise.yaml already exists.
	if _, err := os.Stat(configPath); err == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "hookwise.yaml already exists at %s -- skipping.\n", configPath)
		return nil
	}

	// Write default config.
	if err := os.WriteFile(configPath, []byte(defaultYAMLTemplate), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", configPath)

	// Ensure state directory exists.
	stateDir := core.GetStateDir()
	if err := core.EnsureDir(stateDir, core.DefaultDirMode); err != nil {
		return fmt.Errorf("failed to create state directory %s: %w", stateDir, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Ensured state directory %s\n", stateDir)

	fmt.Fprintln(cmd.OutOrStdout(), "hookwise initialized successfully.")
	return nil
}

// ---------------------------------------------------------------------------
// doctor command (R7.2)
// ---------------------------------------------------------------------------

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run system health checks",
		Long:  "Checks hookwise.yaml validity, Dolt DB connectivity, state directory, and daemon status.",
		RunE:  runDoctor,
	}
}

func runDoctor(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "hookwise doctor")
	fmt.Fprintln(w, strings.Repeat("-", 40))

	allOK := true
	warnings := 0

	// Check 1: hookwise.yaml exists and is valid.
	cwd, _ := os.Getwd()
	configPath := filepath.Join(cwd, core.ProjectConfigFile)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Fprintf(w, "FAIL  config: %s not found\n", core.ProjectConfigFile)
		allOK = false
	} else {
		// Try to load and validate.
		data, readErr := os.ReadFile(configPath)
		if readErr != nil {
			fmt.Fprintf(w, "FAIL  config: cannot read %s: %v\n", configPath, readErr)
			allOK = false
		} else {
			var raw map[string]interface{}
			if parseErr := yaml.Unmarshal(data, &raw); parseErr != nil {
				fmt.Fprintf(w, "FAIL  config: invalid YAML: %v\n", parseErr)
				allOK = false
			} else {
				vr := core.ValidateConfig(raw)
				if vr.Valid {
					fmt.Fprintln(w, "PASS  config: hookwise.yaml is valid")
				} else {
					fmt.Fprintf(w, "WARN  config: %d validation issue(s)\n", len(vr.Errors))
					for _, e := range vr.Errors {
						fmt.Fprintf(w, "       - %s: %s\n", e.Path, e.Message)
					}
					warnings++
				}
			}
		}
	}

	// Check 2: State directory.
	stateDir := core.GetStateDir()
	if info, err := os.Stat(stateDir); err != nil || !info.IsDir() {
		fmt.Fprintf(w, "FAIL  state-dir: %s does not exist\n", stateDir)
		allOK = false
	} else {
		fmt.Fprintf(w, "PASS  state-dir: %s\n", stateDir)
	}

	// Check 3: Dolt DB.
	doltDir := analytics.DefaultDoltDir()
	if info, err := os.Stat(doltDir); err != nil || !info.IsDir() {
		fmt.Fprintf(w, "WARN  dolt-db: %s not found (will be created on first dispatch)\n", doltDir)
		warnings++
	} else {
		fmt.Fprintf(w, "PASS  dolt-db: %s\n", doltDir)
	}

	// Check 4: Daemon PID file.
	pidPath := core.DefaultPIDPath
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		fmt.Fprintln(w, "INFO  daemon: no PID file (daemon not running)")
	} else {
		pidData, err := os.ReadFile(pidPath)
		if err == nil {
			fmt.Fprintf(w, "PASS  daemon: PID file exists (pid: %s)\n", strings.TrimSpace(string(pidData)))
		} else {
			fmt.Fprintf(w, "WARN  daemon: PID file exists but unreadable: %v\n", err)
			warnings++
		}
	}

	// Check 5: Feed health.
	var feedCfg *core.HooksConfig
	if loadedCfg, loadErr := core.LoadConfig(cwd); loadErr == nil {
		feedCfg = &loadedCfg
	}
	warnings += checkFeedHealth(w, filepath.Join(stateDir, "state"), feedCfg)

	fmt.Fprintln(w, strings.Repeat("-", 40))
	if allOK {
		if warnings > 0 {
			fmt.Fprintf(w, "All critical checks passed. %d warning(s).\n", warnings)
		} else {
			fmt.Fprintln(w, "All critical checks passed.")
		}
	} else {
		fmt.Fprintln(w, "Some checks failed. Run 'hookwise init' to fix.")
	}

	return nil
}

// checkFeedHealth reads feed cache files and reports placeholder/stale feeds.
// Returns the number of warnings emitted.
func checkFeedHealth(w io.Writer, cacheDir string, cfg *core.HooksConfig) int {
	warnings := 0

	feedFiles, err := filepath.Glob(filepath.Join(cacheDir, "*.json"))
	if err != nil || len(feedFiles) == 0 {
		return 0
	}

	feedStatuses := make(map[string]string) // feed name → "ok" | "placeholder" | "stale"

	for _, f := range feedFiles {
		base := filepath.Base(f)
		// Skip non-feed files.
		if base == "status-line-cache.json" {
			continue
		}
		feedName := strings.TrimSuffix(base, ".json")

		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}

		var envelope map[string]interface{}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			continue
		}

		// Must have standard feed envelope format: type + timestamp + data.
		if _, hasType := envelope["type"].(string); !hasType {
			continue
		}
		if _, hasTS := envelope["timestamp"].(string); !hasTS {
			continue
		}
		dataRaw, ok := envelope["data"]
		if !ok {
			continue
		}
		dataMap, ok := dataRaw.(map[string]interface{})
		if !ok {
			continue
		}

		// Check placeholder.
		if src, ok := dataMap["source"].(string); ok && src == "placeholder" {
			fmt.Fprintf(w, "WARN  feed:%s: placeholder data (no real source configured)\n", feedName)
			warnings++
			feedStatuses[feedName] = "placeholder"
			continue
		}

		// Parse timestamp once for staleness and reporting.
		tsStr := envelope["timestamp"].(string)
		ts, parseErr := time.Parse(time.RFC3339, tsStr)
		if parseErr != nil {
			fmt.Fprintf(w, "INFO  feed:%s: OK\n", feedName)
			feedStatuses[feedName] = "ok"
			continue
		}

		age := time.Since(ts).Truncate(time.Second)

		// Check staleness.
		interval := getFeedInterval(cfg, feedName)
		if interval > 0 {
			staleThreshold := time.Duration(2*interval) * time.Second
			if age > staleThreshold {
				fmt.Fprintf(w, "WARN  feed:%s: stale data (last updated %s ago)\n", feedName, age)
				warnings++
				feedStatuses[feedName] = "stale"
				continue
			}
		}

		// Feed is healthy.
		fmt.Fprintf(w, "INFO  feed:%s: OK (last updated %s ago)\n", feedName, age)
		feedStatuses[feedName] = "ok"
	}

	// Cross-reference with status_line segments.
	if cfg != nil && cfg.StatusLine.Enabled && len(cfg.StatusLine.Segments) > 0 {
		realCount := 0
		feedSegments := 0
		for _, seg := range cfg.StatusLine.Segments {
			name := seg.Builtin
			if name == "" {
				continue
			}
			// session and cost are computed from analytics, not feed-backed.
			if name == "session" || name == "cost" {
				continue
			}
			feedSegments++
			feedKey := name
			if strings.HasPrefix(feedKey, "insights_") {
				feedKey = "insights"
			}
			if status, ok := feedStatuses[feedKey]; ok && status == "ok" {
				realCount++
			}
		}
		if feedSegments > 0 {
			if realCount < feedSegments {
				fmt.Fprintf(w, "WARN  status-line: %d/%d feed-backed segments have real data\n", realCount, feedSegments)
				warnings++
			} else {
				fmt.Fprintf(w, "INFO  status-line: all %d feed-backed segments have real data\n", feedSegments)
			}
		}
	}

	return warnings
}

// getFeedInterval returns the configured interval_seconds for a feed name.
func getFeedInterval(cfg *core.HooksConfig, feedName string) int {
	if cfg == nil {
		return 0
	}
	switch feedName {
	case "pulse":
		return cfg.Feeds.Pulse.IntervalSeconds
	case "project":
		return cfg.Feeds.Project.IntervalSeconds
	case "calendar":
		return cfg.Feeds.Calendar.IntervalSeconds
	case "news":
		return cfg.Feeds.News.IntervalSeconds
	case "insights":
		return cfg.Feeds.Insights.IntervalSeconds
	case "practice":
		return cfg.Feeds.Practice.IntervalSeconds
	case "weather":
		return cfg.Feeds.Weather.IntervalSeconds
	case "memories":
		return cfg.Feeds.Memories.IntervalSeconds
	default:
		// Check custom feeds.
		for _, cf := range cfg.Feeds.Custom {
			if cf.Name == feedName {
				return cf.IntervalSeconds
			}
		}
		return 0
	}
}

// ---------------------------------------------------------------------------
// status-line command (R7.3)
// ---------------------------------------------------------------------------

// ANSI color helpers.
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiGray   = "\033[90m"
)

func newStatusLineCmd() *cobra.Command {
	var projectDir string

	cmd := &cobra.Command{
		Use:   "status-line",
		Short: "Render the status line",
		Long:  "Loads config and feed cache, then renders a single ANSI-colored status line to stdout.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusLine(cmd, projectDir)
		},
	}

	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory (defaults to cwd)")
	return cmd
}

func runStatusLine(cmd *cobra.Command, projectDir string) error {
	if projectDir == "" {
		var err error
		projectDir, err = os.Getwd()
		if err != nil {
			projectDir = "."
		}
	}

	config, err := core.LoadConfig(projectDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !config.StatusLine.Enabled {
		fmt.Fprintln(cmd.OutOrStdout(), ansiGray+"(status line disabled)"+ansiReset)
		return nil
	}

	// Load feed cache from daemon's per-feed JSON files (ARCH-3: read JSON only).
	// Use GetStateDir() to respect HOOKWISE_STATE_DIR env override.
	cacheDir := filepath.Join(core.GetStateDir(), "state")
	feedCache, _ := bridge.CollectFeedCache(cacheDir)

	// Load today's daily summary from Dolt for session/cost segments.
	// Fail-open: nil summary → segments fall back to "--".
	var dailySummary *analytics.DailySummaryResult
	if db, err := analytics.Open(config.Analytics.DBPath); err == nil {
		defer db.Close()
		today := time.Now().UTC().Format("2006-01-02")
		a := analytics.NewAnalytics(db)
		if s, err := a.DailySummary(context.Background(), today); err == nil {
			dailySummary = s
		}
	}

	delimiter := config.StatusLine.Delimiter
	if delimiter == "" {
		delimiter = core.DefaultStatusDelimiter
	}

	var segments []string

	for _, seg := range config.StatusLine.Segments {
		rendered := renderSegment(seg, feedCache, dailySummary)
		if rendered != "" {
			segments = append(segments, rendered)
		}
	}

	if len(segments) == 0 {
		segments = append(segments, ansiGreen+"hookwise"+ansiReset)
	}

	// Surface unsurfaced notifications (R12.5).
	notifSegment := renderNotificationSegment(config.Analytics.DBPath)
	if notifSegment != "" {
		segments = append(segments, notifSegment)
	}

	line := strings.Join(segments, ansiGray+delimiter+ansiReset)
	fmt.Fprintln(cmd.OutOrStdout(), line)

	// Render insights summary lines (lines 2-4 of the rich status line).
	// These are additional lines below the main segment bar, only shown
	// when insights feed cache has real data.
	var insightsBuf bytes.Buffer
	renderInsightsSummaryLines(io.MultiWriter(cmd.OutOrStdout(), &insightsBuf), feedCache)

	// Write ANSI-stripped full output to cache file for TUI preview (Bug #20).
	fullOutput := line
	if insightsBuf.Len() > 0 {
		fullOutput += "\n" + strings.TrimRight(insightsBuf.String(), "\n")
	}
	if err := writeStatusLineCache(fullOutput); err != nil {
		core.Logger().Warn("failed to write status-line cache", "error", err)
	}

	return nil
}

// renderNotificationSegment queries the Dolt DB for unsurfaced notifications
// and returns a status-line segment summarising them. If the DB cannot be
// opened or there are no pending notifications, returns "".
func renderNotificationSegment(dbPath string) string {
	db, err := analytics.Open(dbPath)
	if err != nil {
		return ""
	}
	defer db.Close()

	ctx := context.Background()
	ns := notifications.NewNotificationService(db)

	unsurfaced, err := ns.Unsurfaced(ctx)
	if err != nil || len(unsurfaced) == 0 {
		return ""
	}

	// Show count and the most recent notification's content (truncated).
	latest := unsurfaced[len(unsurfaced)-1]
	content := latest.Content
	if len(content) > 40 {
		content = content[:37] + "..."
	}

	segment := fmt.Sprintf("%s%d notif%s%s %s%s%s",
		ansiYellow, len(unsurfaced), pluralS(len(unsurfaced)), ansiReset,
		ansiGray, content, ansiReset)

	// Mark as surfaced (write side-effect during render; intentional since
	// this is the only moment we know the user saw the notification).
	for _, n := range unsurfaced {
		if err := ns.MarkSurfaced(ctx, n.ID); err != nil {
			core.Logger().Warn("failed to mark notification surfaced", "id", n.ID, "error", err)
		}
	}

	return segment
}

// pluralS returns "s" when count != 1, "" otherwise.
func pluralS(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// renderSegment renders a single status-line segment with ANSI colors.
func renderSegment(seg core.SegmentConfig, feedCache map[string]interface{}, summary *analytics.DailySummaryResult) string {
	if seg.Builtin != "" {
		return renderBuiltinSegment(seg.Builtin, feedCache, summary)
	}
	if seg.Custom != nil && seg.Custom.Command != "" {
		label := seg.Custom.Label
		if label == "" {
			label = "custom"
		}
		return ansiCyan + label + ansiReset
	}
	return ""
}

// renderBuiltinSegment renders a known builtin segment by name, reading
// real data from the feed cache and Dolt daily summary when available.
func renderBuiltinSegment(name string, feedCache map[string]interface{}, summary *analytics.DailySummaryResult) string {
	switch name {
	case "session":
		if summary != nil && summary.TotalSessions > 0 {
			return ansiBold + ansiGreen + fmt.Sprintf("session: %d", summary.TotalSessions) + ansiReset
		}
		return "" // No data — omit segment instead of showing "--"
	case "cost":
		if summary != nil && summary.EstimatedCostUSD > 0 {
			return ansiYellow + fmt.Sprintf("cost: $%.2f", summary.EstimatedCostUSD) + ansiReset
		}
		return "" // No data — omit segment instead of showing "--"
	case "project":
		return renderProjectSegment(feedCache)
	case "calendar":
		return renderCalendarSegment(feedCache)
	case "pulse":
		return renderPulseSegment(feedCache)
	case "weather":
		return renderWeatherSegment(feedCache)
	case "insights":
		return renderInsightsSegment(feedCache)
	case "insights_friction":
		return renderInsightsFrictionSegment(feedCache)
	case "insights_pace":
		return renderInsightsPaceSegment(feedCache)
	case "insights_trend":
		return renderInsightsTrendSegment(feedCache)
	default:
		return ansiGray + name + ansiReset
	}
}

// feedData extracts the "data" sub-object from a feed cache envelope.
// Returns nil if the feed is missing, malformed, or has source: "placeholder".
func feedData(feedCache map[string]interface{}, feedName string) map[string]interface{} {
	if feedCache == nil {
		return nil
	}
	raw, ok := feedCache[feedName]
	if !ok {
		return nil
	}
	envelope, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	dataRaw, ok := envelope["data"]
	if !ok {
		return nil
	}
	data, ok := dataRaw.(map[string]interface{})
	if !ok {
		return nil
	}
	// Check for placeholder source.
	if src, ok := data["source"]; ok {
		if srcStr, ok := src.(string); ok && srcStr == "placeholder" {
			return nil
		}
	}
	return data
}

// renderWeatherSegment renders the weather segment from feed cache data.
func renderWeatherSegment(feedCache map[string]interface{}) string {
	data := feedData(feedCache, "weather")
	if data == nil {
		return ""
	}

	// Temperature may be nil (placeholder/fallback).
	temp := data["temperature"]
	if temp == nil {
		return ""
	}

	// Format temperature as integer.
	var tempStr string
	switch v := temp.(type) {
	case float64:
		tempStr = strconv.FormatFloat(v, 'f', 0, 64)
	case int:
		tempStr = strconv.Itoa(v)
	default:
		tempStr = fmt.Sprintf("%v", v)
	}

	// Determine unit symbol.
	unit := "F"
	if u, ok := data["temperatureUnit"]; ok {
		if us, ok := u.(string); ok && us == "celsius" {
			unit = "C"
		}
	}

	emoji := ""
	if e, ok := data["emoji"].(string); ok {
		emoji = e + " "
	}

	desc := ""
	if d, ok := data["description"].(string); ok {
		desc = " " + d
	}

	return ansiCyan + emoji + tempStr + "\u00b0" + unit + desc + ansiReset
}

// renderProjectSegment renders the project segment from feed cache data.
func renderProjectSegment(feedCache map[string]interface{}) string {
	data := feedData(feedCache, "project")
	if data == nil {
		return ""
	}

	name := "project"
	if n, ok := data["name"].(string); ok && n != "" {
		name = n
	}

	branch := ""
	if b, ok := data["branch"].(string); ok && b != "" {
		branch = " (" + b + ")"
	}

	return ansiCyan + name + branch + ansiReset
}

// renderCalendarSegment renders the calendar segment from feed cache data.
// Relative time ("in 15m") is computed at render time from the absolute start
// time, so the display stays accurate regardless of cache age.
func renderCalendarSegment(feedCache map[string]interface{}) string {
	data := feedData(feedCache, "calendar")
	if data == nil {
		return ""
	}

	nextEvent := data["next_event"]
	if nextEvent == nil {
		return ""
	}

	// next_event can be a string or a map with name/start/time fields.
	switch ev := nextEvent.(type) {
	case string:
		if ev == "" {
			return ""
		}
		return ansiCyan + "\U0001f4c5 " + ev + ansiReset
	case map[string]interface{}:
		name, _ := ev["name"].(string)

		// Prefer absolute "start" (compute relative time dynamically).
		// Fall back to static "time" for backward compatibility.
		var timeLabel string
		if startStr, ok := ev["start"].(string); ok && startStr != "" {
			if eventStart, err := time.Parse(time.RFC3339, startStr); err == nil {
				timeLabel = calendarRelativeTime(time.Now(), eventStart)
			}
		}
		if timeLabel == "" {
			timeLabel, _ = ev["time"].(string)
		}

		if name == "" && timeLabel == "" {
			return ""
		}
		label := name
		if timeLabel != "" {
			if label != "" {
				label += " " + timeLabel
			} else {
				label = timeLabel
			}
		}
		return ansiCyan + "\U0001f4c5 " + label + ansiReset
	default:
		return ""
	}
}

// calendarRelativeTime returns a human-friendly relative time for calendar events.
func calendarRelativeTime(now, eventStart time.Time) string {
	diff := eventStart.Sub(now)
	if diff <= 0 || diff < time.Minute {
		return "now"
	}

	totalMinutes := int(diff.Minutes())
	if totalMinutes < 60 {
		return fmt.Sprintf("in %dm", totalMinutes)
	}

	// Check calendar-day boundary before raw hours.
	nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	eventDate := time.Date(eventStart.Year(), eventStart.Month(), eventStart.Day(), 0, 0, 0, 0, eventStart.Location())
	dayDiff := int(eventDate.Sub(nowDate).Hours() / 24)

	if dayDiff == 0 {
		hours := totalMinutes / 60
		minutes := totalMinutes % 60
		if minutes < 5 {
			return fmt.Sprintf("in %dh", hours)
		}
		return fmt.Sprintf("in %dh %dm", hours, minutes)
	}

	timeLabel := strings.ToLower(eventStart.Format("3:04pm"))
	if dayDiff == 1 {
		return "tomorrow " + timeLabel
	}
	return eventStart.Format("Mon") + " " + timeLabel
}

// renderPulseSegment renders the pulse segment from feed cache data.
func renderPulseSegment(feedCache map[string]interface{}) string {
	data := feedData(feedCache, "pulse")
	if data == nil {
		return ""
	}

	sessionCount, ok := data["session_count"]
	if !ok {
		return ""
	}

	var count int
	switch v := sessionCount.(type) {
	case float64:
		count = int(v)
	case int:
		count = v
	default:
		return ""
	}

	suffix := "sessions"
	if count == 1 {
		suffix = "session"
	}
	return ansiGreen + fmt.Sprintf("pulse: %d %s", count, suffix) + ansiReset
}

// renderInsightsSegment renders a compact one-line insights summary.
func renderInsightsSegment(feedCache map[string]interface{}) string {
	data := feedData(feedCache, "insights")
	if data == nil {
		return ""
	}
	sessions := toInt(data["total_sessions"])
	if sessions == 0 {
		return ""
	}
	lines := toInt(data["total_lines_added"])
	days := toInt(data["days_active"])
	return ansiCyan + fmt.Sprintf("%d sessions / %dd | %s lines", sessions, days, formatLargeNumber(lines)) + ansiReset
}

// renderInsightsFrictionSegment renders friction status from insights feed cache.
func renderInsightsFrictionSegment(feedCache map[string]interface{}) string {
	data := feedData(feedCache, "insights")
	if data == nil {
		return ""
	}
	if toInt(data["total_sessions"]) == 0 {
		return "" // No usage data — don't show "✅ No friction" for empty envelopes
	}

	// Check recent session friction
	recentFriction := 0
	if rs, ok := data["recent_session"].(map[string]interface{}); ok {
		recentFriction = toInt(rs["friction_count"])
	}

	frictionTotal := toInt(data["friction_total"])
	frictionCounts, _ := data["friction_counts"].(map[string]interface{})

	if recentFriction > 0 {
		tip := topFrictionTip(frictionCounts)
		if tip != "" {
			return ansiYellow + fmt.Sprintf("\u26a0\ufe0f %d friction this session \u00b7 %s", recentFriction, tip) + ansiReset
		}
		return ansiYellow + fmt.Sprintf("\u26a0\ufe0f %d friction this session", recentFriction) + ansiReset
	}
	if frictionTotal > 0 {
		return ansiGreen + fmt.Sprintf("\u2705 Clean session \u00b7 %d in 30d", frictionTotal) + ansiReset
	}
	return ansiGreen + "\u2705 No friction detected" + ansiReset
}

// renderInsightsPaceSegment renders productivity pace from insights feed cache.
func renderInsightsPaceSegment(feedCache map[string]interface{}) string {
	data := feedData(feedCache, "insights")
	if data == nil {
		return ""
	}
	if toInt(data["total_sessions"]) == 0 {
		return "" // No usage data — don't show "0 msgs/day | 0 lines | 0 sessions"
	}
	totalMessages := toInt(data["total_messages"])
	daysActive := toInt(data["days_active"])
	if daysActive == 0 {
		daysActive = 1
	}
	linesAdded := toInt(data["total_lines_added"])
	sessions := toInt(data["total_sessions"])

	msgsPerDay := totalMessages / daysActive
	formattedLines := formatLargeNumber(linesAdded)

	return ansiCyan + fmt.Sprintf("\U0001f4ca %d msgs/day | %s+ lines | %d sessions", msgsPerDay, formattedLines, sessions) + ansiReset
}

// renderInsightsTrendSegment renders tool trends and peak hour from insights feed cache.
func renderInsightsTrendSegment(feedCache map[string]interface{}) string {
	data := feedData(feedCache, "insights")
	if data == nil {
		return ""
	}
	topToolsRaw, ok := data["top_tools"].([]interface{})
	if !ok || len(topToolsRaw) == 0 {
		return ""
	}

	// Extract tool names (top 2)
	var toolNames []string
	for i, t := range topToolsRaw {
		if i >= 2 {
			break
		}
		if tm, ok := t.(map[string]interface{}); ok {
			if name, ok := tm["name"].(string); ok {
				toolNames = append(toolNames, name)
			}
		}
	}

	peakHour := toInt(data["peak_hour"])
	peakLabel := hourToLabel(peakHour)

	if len(toolNames) == 0 {
		return ""
	}

	return ansiCyan + fmt.Sprintf("\U0001f527 Top: %s | Peak: %s", strings.Join(toolNames, ", "), peakLabel) + ansiReset
}

// renderInsightsSummaryLines outputs 2-3 additional status lines with aggregated
// insights data. Each line is independent — if data is missing, that line is omitted.
func renderInsightsSummaryLines(w io.Writer, feedCache map[string]interface{}) {
	data := feedData(feedCache, "insights")
	if data == nil {
		return
	}

	sessions := toInt(data["total_sessions"])
	if sessions == 0 {
		return
	}

	// Line 2: Session overview — green for sessions, cyan for lines, gray for avg
	days := toInt(data["days_active"])
	lines := toInt(data["total_lines_added"])
	avgDur := 0.0
	if v, ok := data["avg_duration_minutes"].(float64); ok {
		avgDur = v
	}
	fmt.Fprintf(w, "%s%d sessions%s %s/ %dd active%s %s|%s %s%s lines%s %s|%s %savg %.0fm/session%s\n",
		ansiGreen, sessions, ansiReset,
		ansiGray, days, ansiReset,
		ansiGray, ansiReset,
		ansiCyan, formatLargeNumber(lines), ansiReset,
		ansiGray, ansiReset,
		ansiGray, avgDur, ansiReset)

	// Line 3: Top tools + peak hour — cyan for tools, yellow for peak
	if topToolsRaw, ok := data["top_tools"].([]interface{}); ok && len(topToolsRaw) > 0 {
		var parts []string
		for i, t := range topToolsRaw {
			if i >= 5 {
				break
			}
			if tm, ok := t.(map[string]interface{}); ok {
				name, _ := tm["name"].(string)
				count := toInt(tm["count"])
				if name != "" {
					parts = append(parts, fmt.Sprintf("%s%s%s%s(%d)%s",
						ansiCyan, name, ansiReset, ansiGray, count, ansiReset))
				}
			}
		}
		peakHour := toInt(data["peak_hour"])
		if len(parts) > 0 {
			fmt.Fprintf(w, "%stop:%s %s %s|%s %speak: %s%s%s%s\n",
				ansiGray, ansiReset,
				strings.Join(parts, " "),
				ansiGray, ansiReset,
				ansiGray, ansiReset,
				ansiYellow, hourToLabel(peakHour), ansiReset)
		}
	}

	// Line 4: Friction summary — yellow for total, gray for categories
	frictionTotal := toInt(data["friction_total"])
	if frictionTotal > 0 {
		frictionParts := ""
		if fc, ok := data["friction_counts"].(map[string]interface{}); ok && len(fc) > 0 {
			type catCount struct {
				name  string
				count int
			}
			var sorted []catCount
			for cat, v := range fc {
				count := toInt(v)
				if count > 0 {
					sorted = append(sorted, catCount{
						name:  strings.ReplaceAll(cat, "_", " "),
						count: count,
					})
				}
			}
			sort.Slice(sorted, func(i, j int) bool {
				return sorted[i].count > sorted[j].count
			})
			if len(sorted) > 5 {
				sorted = sorted[:5]
			}
			var cats []string
			for _, c := range sorted {
				cats = append(cats, fmt.Sprintf("%s:%d", c.name, c.count))
			}
			if len(cats) > 0 {
				frictionParts = " " + ansiGray + "(" + strings.Join(cats, " ") + ")" + ansiReset
			}
		}
		fmt.Fprintf(w, "%sfriction:%s %s%d total%s%s\n",
			ansiYellow, ansiReset,
			ansiYellow, frictionTotal, ansiReset, frictionParts)
	}
}

// toInt extracts an integer from an interface{} value.
// Handles float64 (from JSON), int, and nil.
func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case nil:
		return 0
	default:
		return 0
	}
}

// formatLargeNumber formats a number with k suffix for thousands.
func formatLargeNumber(n int) string {
	if n >= 1000 {
		k := float64(n) / 1000
		if k == float64(int(k)) {
			return fmt.Sprintf("%dk", int(k))
		}
		return fmt.Sprintf("%.1fk", k)
	}
	return fmt.Sprintf("%d", n)
}

// hourToLabel maps an hour (0-23) to a time-of-day label.
func hourToLabel(hour int) string {
	switch {
	case hour >= 6 && hour < 12:
		return "morning"
	case hour >= 12 && hour < 18:
		return "afternoon"
	case hour >= 18 && hour < 24:
		return "evening"
	default:
		return "night"
	}
}

// topFrictionTip returns an actionable tip for the most common friction category.
func topFrictionTip(frictionCounts map[string]interface{}) string {
	if len(frictionCounts) == 0 {
		return ""
	}

	frictionTips := map[string]string{
		"wrong_approach":       "break tasks into steps",
		"misunderstood_request": "be more specific",
		"stale_context":        "try a fresh session",
		"tool_error":           "check tool setup",
		"scope_creep":          "define done upfront",
		"repeated_errors":      "read error output first",
	}

	var topCat string
	var topCount int
	for cat, v := range frictionCounts {
		count := toInt(v)
		if count > topCount {
			topCat = cat
			topCount = count
		}
	}
	if topCat == "" {
		return ""
	}

	humanName := strings.ReplaceAll(topCat, "_", " ")
	if tip, ok := frictionTips[topCat]; ok {
		return humanName + " \u2192 " + tip
	}
	return humanName
}

// ---------------------------------------------------------------------------
// test command (R7.4) -- guard test runner
// ---------------------------------------------------------------------------

func newTestCmd() *cobra.Command {
	var projectDir string

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Evaluate guard test scenarios",
		Long:  "Loads config, creates synthetic test payloads for each guard rule, evaluates them, and reports pass/fail.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTest(cmd, projectDir)
		},
	}

	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory (defaults to cwd)")
	return cmd
}

func runTest(cmd *cobra.Command, projectDir string) error {
	if projectDir == "" {
		var err error
		projectDir, err = os.Getwd()
		if err != nil {
			projectDir = "."
		}
	}

	config, err := core.LoadConfig(projectDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "hookwise test -- guard rule evaluation")
	fmt.Fprintln(w, strings.Repeat("-", 40))

	if len(config.Guards) == 0 {
		fmt.Fprintln(w, "No guard rules defined. Nothing to test.")
		return nil
	}

	passed := 0
	failed := 0

	for i, rule := range config.Guards {
		// Create a synthetic payload that should trigger this rule.
		payload := buildTestPayload(rule)
		result := core.Evaluate(rule.Match, payload, config.Guards)

		// The rule should match and produce its expected action.
		expectAction := rule.Action
		actualAction := result.Action

		if actualAction == expectAction {
			fmt.Fprintf(w, "PASS  [%d] match=%q action=%s\n", i+1, rule.Match, actualAction)
			passed++
		} else {
			fmt.Fprintf(w, "FAIL  [%d] match=%q expected=%s got=%s\n", i+1, rule.Match, expectAction, actualAction)
			failed++
		}
	}

	fmt.Fprintln(w, strings.Repeat("-", 40))
	fmt.Fprintf(w, "Results: %d passed, %d failed, %d total\n", passed, failed, passed+failed)

	if failed > 0 {
		return fmt.Errorf("%d guard test(s) failed", failed)
	}
	return nil
}

// buildTestPayload creates a synthetic payload designed to trigger the given rule.
// If the rule has a "when" condition, we try to populate the payload so the condition is satisfied.
func buildTestPayload(rule core.GuardRuleConfig) map[string]interface{} {
	payload := map[string]interface{}{
		"session_id": "test-session",
		"tool_name":  rule.Match,
	}

	// Parse the "when" condition to build a payload that satisfies it.
	if rule.When != "" {
		parsed := core.ParseCondition(rule.When)
		if parsed != nil {
			setNestedField(payload, parsed.FieldPath, buildMatchValue(parsed))
		}
	}

	return payload
}

// setNestedField sets a value at a dot-separated path in a map.
func setNestedField(m map[string]interface{}, path string, value interface{}) {
	parts := strings.Split(path, ".")
	current := m
	for i := 0; i < len(parts)-1; i++ {
		child, ok := current[parts[i]].(map[string]interface{})
		if !ok {
			child = make(map[string]interface{})
			current[parts[i]] = child
		}
		current = child
	}
	current[parts[len(parts)-1]] = value
}

// buildMatchValue creates a string value that satisfies the given parsed condition.
func buildMatchValue(cond *core.ParsedCondition) string {
	switch cond.Operator {
	case "contains":
		return "prefix_" + cond.Value + "_suffix"
	case "starts_with":
		return cond.Value + "_rest"
	case "ends_with":
		return "start_" + cond.Value
	case "==", "equals":
		return cond.Value
	case "matches":
		return cond.Value // best effort: use the regex pattern as a literal
	default:
		return cond.Value
	}
}

// ---------------------------------------------------------------------------
// stats command (R7.5)
// ---------------------------------------------------------------------------

func newStatsCmd() *cobra.Command {
	var dataDir string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show analytics dashboard for today",
		Long:  "Opens the Dolt database and displays today's daily summary and tool breakdown.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStats(cmd, dataDir)
		},
	}

	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Dolt data directory (defaults to ~/.hookwise/dolt)")
	return cmd
}

func runStats(cmd *cobra.Command, dataDir string) error {
	db, err := analytics.Open(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open Dolt DB: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	today := time.Now().UTC().Format("2006-01-02")
	a := analytics.NewAnalytics(db)

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "hookwise stats -- %s\n", today)
	fmt.Fprintln(w, strings.Repeat("-", 40))

	// Daily summary.
	summary, err := a.DailySummary(ctx, today)
	if err != nil {
		return fmt.Errorf("daily summary: %w", err)
	}

	fmt.Fprintf(w, "Sessions:       %d\n", summary.TotalSessions)
	fmt.Fprintf(w, "Events:         %d\n", summary.TotalEvents)
	fmt.Fprintf(w, "Tool calls:     %d\n", summary.TotalToolCalls)
	fmt.Fprintf(w, "File edits:     %d\n", summary.TotalFileEdits)
	fmt.Fprintf(w, "AI lines:       %d\n", summary.AIAuthoredLines)
	fmt.Fprintf(w, "Human lines:    %d\n", summary.HumanVerifiedLines)
	fmt.Fprintf(w, "Est. cost:      $%.2f\n", summary.EstimatedCostUSD)

	// Tool breakdown.
	breakdown, err := a.ToolBreakdown(ctx, today)
	if err != nil {
		return fmt.Errorf("tool breakdown: %w", err)
	}

	if len(breakdown) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Tool breakdown:")
		for _, entry := range breakdown {
			fmt.Fprintf(w, "  %-20s %4d (%5.1f%%)\n", entry.ToolName, entry.Count, entry.Percentage)
		}
	} else {
		fmt.Fprintln(w, "\nNo tool usage recorded today.")
	}

	return nil
}

// ---------------------------------------------------------------------------
// diff command
// ---------------------------------------------------------------------------

func newDiffCmd() *cobra.Command {
	var dataDir string

	cmd := &cobra.Command{
		Use:   "diff <from-ref> <to-ref>",
		Short: "Show Dolt data changes between commits",
		Long:  "Compares two Dolt refs (commit hashes, branches, tags) and shows table-level differences.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd, args[0], args[1], dataDir)
		},
	}

	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Dolt data directory (defaults to ~/.hookwise/dolt)")
	return cmd
}

func runDiff(cmd *cobra.Command, fromRef, toRef, dataDir string) error {
	db, err := analytics.Open(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open Dolt DB: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	entries, err := db.Diff(ctx, fromRef, toRef)
	if err != nil {
		return fmt.Errorf("diff failed: %w", err)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "hookwise diff %s..%s\n", fromRef, toRef)
	fmt.Fprintln(w, strings.Repeat("-", 40))

	if len(entries) == 0 {
		fmt.Fprintln(w, "No differences found.")
		return nil
	}

	for _, e := range entries {
		dataChange, _ := e.RowData["data_change"].(bool)
		schemaChange, _ := e.RowData["schema_change"].(bool)
		var changes []string
		if dataChange {
			changes = append(changes, "data")
		}
		if schemaChange {
			changes = append(changes, "schema")
		}
		changeStr := strings.Join(changes, ", ")
		if changeStr == "" {
			changeStr = "-"
		}
		fmt.Fprintf(w, "  %-8s  %-25s  changes: %s\n", e.DiffType, e.TableName, changeStr)
	}

	return nil
}

// ---------------------------------------------------------------------------
// log command
// ---------------------------------------------------------------------------

func newLogCmd() *cobra.Command {
	var (
		dataDir string
		limit   int
	)

	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show Dolt commit history",
		Long:  "Displays recent Dolt commits from the hookwise analytics database.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLog(cmd, dataDir, limit)
		},
	}

	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Dolt data directory (defaults to ~/.hookwise/dolt)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum number of commits to show")
	return cmd
}

func runLog(cmd *cobra.Command, dataDir string, limit int) error {
	db, err := analytics.Open(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open Dolt DB: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	entries, err := db.Log(ctx, limit)
	if err != nil {
		return fmt.Errorf("log query failed: %w", err)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "hookwise log")
	fmt.Fprintln(w, strings.Repeat("-", 40))

	if len(entries) == 0 {
		fmt.Fprintln(w, "No commits found.")
		return nil
	}

	for _, e := range entries {
		dateStr := e.Date.Format("2006-01-02 15:04:05")
		fmt.Fprintf(w, "%s  %s  %s\n", e.CommitHash[:min(7, len(e.CommitHash))], dateStr, e.Message)
	}

	return nil
}

// ---------------------------------------------------------------------------
// upgrade command (Batch 10 — Data Migration)
// ---------------------------------------------------------------------------

func newUpgradeCmd() *cobra.Command {
	var (
		dryRun     bool
		dataDir    string
		projectDir string
	)

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Migrate data from TypeScript hookwise installation",
		Long: `Detects an existing TypeScript hookwise installation (~/.hookwise/analytics.db
and ~/.hookwise/state/cost-state.json), imports the data into the Go Dolt
database, and validates config parity.

Use --dry-run to preview what would be migrated without making changes.
Original files are never modified (non-destructive).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectDir == "" {
				var err error
				projectDir, err = os.Getwd()
				if err != nil {
					projectDir = "."
				}
			}

			result := migration.Run(migration.MigrationOpts{
				DryRun:      dryRun,
				DoltDataDir: dataDir,
				ProjectDir:  projectDir,
				Writer:      cmd.OutOrStdout(),
			})

			if len(result.Errors) > 0 {
				return fmt.Errorf("migration completed with %d error(s)", len(result.Errors))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview migration without making changes")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Dolt data directory (defaults to ~/.hookwise/dolt)")
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory for config validation (defaults to cwd)")

	return cmd
}

// ---------------------------------------------------------------------------
// TUI launcher (Bug #14 — duplicate terminal tabs)
// ---------------------------------------------------------------------------

// tuiPIDPath returns the path to the TUI PID file.
func tuiPIDPath() string {
	return filepath.Join(core.DefaultStateDir, "tui.pid")
}

// isTUIRunning checks if a TUI process is already running by reading the PID file,
// checking if the process exists, and verifying it's actually hookwise-tui
// (not a stale PID reused by an unrelated process).
func isTUIRunning() bool {
	data, err := os.ReadFile(tuiPIDPath())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	// Check if process exists (signal 0 = existence check)
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	// Verify the PID belongs to hookwise-tui, not a stale PID reused by
	// an unrelated process. Uses `ps` which works on macOS and Linux.
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	comm := strings.TrimSpace(string(out))
	return strings.Contains(comm, "hookwise-tui") || strings.Contains(comm, "python") || strings.Contains(comm, "Python")
}

// acquireTUILaunchLock atomically creates a lock file to prevent concurrent
// TUI launches (TOCTOU race between isTUIRunning check and TUI PID write).
// Returns a cleanup function and true on success, or nil and false if another
// dispatch already holds the lock.
func acquireTUILaunchLock() (unlock func(), ok bool) {
	lockPath := filepath.Join(core.DefaultStateDir, "tui.launch.lock")
	_ = os.MkdirAll(filepath.Dir(lockPath), core.DefaultDirMode)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, false
	}
	f.Close()
	return func() { os.Remove(lockPath) }, true
}

// launchTUIIfNeeded launches the TUI if it's not already running.
// Called synchronously from dispatch on SessionStart events.
func launchTUIIfNeeded(launchMethod string) {
	defer func() {
		if r := recover(); r != nil {
			core.Logger().Error("panic in TUI launcher", "recovered", fmt.Sprintf("%v", r))
		}
	}()

	if isTUIRunning() {
		core.Logger().Debug("TUI already running, skipping launch")
		return
	}

	// Atomic lock prevents TOCTOU race: two concurrent SessionStart dispatches
	// could both pass isTUIRunning() before either TUI writes its PID file.
	unlock, ok := acquireTUILaunchLock()
	if !ok {
		core.Logger().Debug("another dispatch is launching TUI, skipping")
		return
	}
	defer unlock()

	// Re-check after acquiring lock (double-check pattern)
	if isTUIRunning() {
		core.Logger().Debug("TUI started between check and lock, skipping")
		return
	}

	// Find hookwise-tui executable
	tuiCmd, err := exec.LookPath("hookwise-tui")
	if err != nil {
		core.Logger().Debug("hookwise-tui not found in PATH, skipping auto-launch")
		return
	}

	var cmd *exec.Cmd
	switch launchMethod {
	case "newWindow":
		// macOS: open in a new Terminal window
		cmd = exec.Command("open", "-a", "Terminal", tuiCmd)
	default:
		// background: launch directly as a background process
		cmd = exec.Command(tuiCmd)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		core.Logger().Warn("failed to launch TUI", "method", launchMethod, "error", err)
		return
	}

	core.Logger().Info("TUI launched", "method", launchMethod, "pid", cmd.Process.Pid)
}

// ---------------------------------------------------------------------------
// Status line cache writer (Bug #20 — TUI preview out of sync)
// ---------------------------------------------------------------------------

// ansiRegex matches ANSI escape sequences for stripping.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// writeStatusLineCache writes the ANSI-stripped status line output to the cache file
// so the TUI can display a live preview.
func writeStatusLineCache(line string) error {
	cachePath := core.LastStatusOutputPath
	cacheDir := filepath.Dir(cachePath)

	if err := os.MkdirAll(cacheDir, core.DefaultDirMode); err != nil {
		return fmt.Errorf("mkdir %s: %w", cacheDir, err)
	}

	stripped := ansiRegex.ReplaceAllString(line, "")
	return os.WriteFile(cachePath, []byte(stripped+"\n"), 0o644)
}

// ---------------------------------------------------------------------------
// notifications command (Batch 14 — Notification Platform)
// ---------------------------------------------------------------------------

func newNotificationsCmd() *cobra.Command {
	var (
		dataDir string
		limit   int
	)

	cmd := &cobra.Command{
		Use:   "notifications",
		Short: "Display notification history",
		Long: `Shows recent notifications from budget, guard effectiveness, and coaching
producers. Notifications are stored in the Dolt analytics database and
surfaced via the status line or this command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotifications(cmd, dataDir, limit)
		},
	}

	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Dolt data directory (defaults to ~/.hookwise/dolt)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of notifications to show")

	return cmd
}

func runNotifications(cmd *cobra.Command, dataDir string, limit int) error {
	db, err := analytics.Open(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open Dolt DB: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	ns := notifications.NewNotificationService(db)

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "hookwise notifications")
	fmt.Fprintln(w, strings.Repeat("-", 50))

	notifs, err := ns.List(ctx, limit)
	if err != nil {
		return fmt.Errorf("failed to list notifications: %w", err)
	}

	if len(notifs) == 0 {
		fmt.Fprintln(w, "No notifications.")
		return nil
	}

	for _, n := range notifs {
		ts := n.CreatedAt.Format("2006-01-02 15:04")
		surfaced := " "
		if n.SurfacedAt != nil {
			surfaced = "*"
		}

		fmt.Fprintf(w, "%s [%s] %-8s %-22s %s\n",
			surfaced, ts, n.Producer, n.Type, n.Content)
	}

	fmt.Fprintln(w, strings.Repeat("-", 50))
	fmt.Fprintf(w, "%d notification(s) shown.\n", len(notifs))

	// Mark all unsurfaced notifications as surfaced now that they've been displayed.
	unsurfaced, err := ns.Unsurfaced(ctx)
	if err != nil {
		return fmt.Errorf("failed to query unsurfaced: %w", err)
	}
	for _, n := range unsurfaced {
		_ = ns.MarkSurfaced(ctx, n.ID)
	}

	return nil
}
