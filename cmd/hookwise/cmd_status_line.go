package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/bridge"
	"github.com/vishnujayvel/hookwise/internal/core"
	"github.com/vishnujayvel/hookwise/internal/notifications"
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

	// Auto-start the feed daemon if not running (Task 6.1).
	// Uses marker file caching to avoid socket dial on every invocation.
	configPath := filepath.Join(projectDir, core.ProjectConfigFile)
	ensureDaemonWithCache(configPath)

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

	// Surface warning count (architecture-v2).
	warnSegment := renderWarningSegment(core.GetStateDir())
	if warnSegment != "" {
		segments = append(segments, warnSegment)
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

// renderWarningSegment reads unexpired warnings and returns a status-line
// segment showing the count and most recent source. Returns "" if no warnings.
func renderWarningSegment(stateDir string) string {
	warnings := core.ReadWarnings(stateDir)
	if len(warnings) == 0 {
		return ""
	}
	// Most recent warning is last in the slice.
	mostRecentSource := warnings[len(warnings)-1].Source
	return fmt.Sprintf("%s\u26a0 %d %s%s", ansiYellow, len(warnings), mostRecentSource, ansiReset)
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
// Returns nil if the feed is missing, malformed, stale (past TTL), or has source: "placeholder".
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
	// TTL freshness check — stale cache entries are treated as absent.
	if !bridge.IsEnvelopeFresh(envelope) {
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
			if eventStart, err := core.ParseTimeFlex(startStr); err == nil {
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
		"wrong_approach":        "break tasks into steps",
		"misunderstood_request": "be more specific",
		"stale_context":         "try a fresh session",
		"tool_error":            "check tool setup",
		"scope_creep":           "define done upfront",
		"repeated_errors":       "read error output first",
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

