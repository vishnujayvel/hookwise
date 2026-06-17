package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
	"github.com/vishnujayvel/hookwise/internal/feeds"
	"github.com/vishnujayvel/hookwise/internal/hooks"
	"gopkg.in/yaml.v3"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run system health checks",
		Long:  "Checks hookwise.yaml validity, analytics DB connectivity, state directory, and daemon status.",
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

	// Check 3: analytics DB health. Open the SQLite DB and run a trivial query
	// so a corrupt or unwritable analytics.db surfaces here — ARCH-1 fail-open
	// hides analytics write failures everywhere else.
	// Resolve the analytics DB under the active state dir so doctor checks the
	// SAME DB the dispatch writer uses (and honors HOOKWISE_STATE_DIR), rather
	// than the home-relative DefaultDBPath which ignores the env override.
	dbPath := filepath.Join(core.GetStateDir(), "analytics.db")
	if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
		fmt.Fprintf(w, "WARN  analytics: %s not yet created (created on first dispatch)\n", dbPath)
		warnings++
	} else if db, err := analytics.Open(dbPath); err != nil {
		fmt.Fprintf(w, "FAIL  analytics: cannot open %s: %v\n", dbPath, err)
		allOK = false
	} else {
		var one int
		if qErr := db.QueryRow(context.Background(), "SELECT 1").Scan(&one); qErr != nil {
			fmt.Fprintf(w, "FAIL  analytics: %s not queryable: %v\n", dbPath, qErr)
			allOK = false
		} else {
			fmt.Fprintf(w, "PASS  analytics: %s\n", dbPath)
		}
		_ = db.Close()
	}

	// Legacy Dolt archive note (informational; safe to delete once confirmed).
	doltDir := analytics.DefaultDoltDir()
	if info, e := os.Stat(doltDir); e == nil && info.IsDir() {
		fmt.Fprintf(w, "INFO  legacy-dolt: %s present (archived; safe to remove)\n", doltDir)
	}

	// Check 8: Cost-tracking honesty. DailySummary aggregates per-session
	// estimated_cost_usd from the SAME UTC day `hookwise stats` and the
	// status-line read, so doctor agrees with what the user sees there. If
	// sessions were recorded today but $0 was computed, the cost writer is
	// likely dead -- the exact failure that kept stats/status-line silently at
	// $0. Mirrors the feed zero-liveness principle (cache-fresh != data-present).
	warnings += checkCostHonesty(w, dbPath)

	// Check 4: Daemon liveness via socket dial (replaces PID file check).
	// Use GetStateDir() to respect HOOKWISE_STATE_DIR env override.
	socketPath := filepath.Join(core.GetStateDir(), "daemon.sock")
	client := feeds.NewDaemonClient(socketPath)
	if client.IsRunning() {
		health, healthErr := client.Health()
		if healthErr == nil {
			fmt.Fprintf(w, "PASS  daemon: running (pid: %v, uptime: %v)\n",
				health["pid"], health["uptime"])
		} else {
			fmt.Fprintln(w, "PASS  daemon: running (health check unavailable)")
		}
	} else {
		fmt.Fprintln(w, "INFO  daemon: not running (start with 'hookwise daemon start')")
	}

	// Check 5: Feed health.
	var feedCfg *core.HooksConfig
	if loadedCfg, loadErr := core.LoadConfig(cwd); loadErr == nil {
		feedCfg = &loadedCfg
	}
	warnings += checkFeedHealth(w, filepath.Join(stateDir, "state"), feedCfg)

	// Check 7: Hook safety — scan Claude Code settings for hook sprawl, missing
	// binaries, network-dependent hot-path hooks, and duplicate/overlapping
	// guards (issues #33-36).
	hookWarnings, hookFails := checkHookSafety(w)
	warnings += hookWarnings
	if hookFails > 0 {
		allOK = false
	}

	// Check 6: Recent warnings.
	recentWarnings := core.ReadWarnings(stateDir)
	if len(recentWarnings) == 0 {
		fmt.Fprintln(w, "PASS  warnings: none")
	} else {
		for _, rw := range recentWarnings {
			age := core.FormatWarningAge(rw.Timestamp)
			fmt.Fprintf(w, "WARN  warning: [%s] %s (%s ago)\n", rw.Source, rw.Message, age)
		}
		warnings += len(recentWarnings)
	}

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

// checkHookSafety scans Claude Code settings for hook-safety issues (sprawl,
// missing binaries, network-dependent hot-path hooks, duplicate/overlapping
// guards) and renders the findings. Returns (warnings, fails) so the caller can
// fold them into the doctor summary.
func checkHookSafety(w io.Writer) (warnings, fails int) {
	inv, err := hooks.Scan(hooks.DefaultSettingsPaths())
	if err != nil {
		// Scan currently never returns a non-nil error (malformed files are
		// recorded in inv.ParseErrors), but handle it defensively.
		fmt.Fprintf(w, "WARN  hooks: settings scan failed: %v\n", err)
		warnings++
	}
	// Surface any settings files that could not be parsed.
	for _, pe := range inv.ParseErrors {
		fmt.Fprintf(w, "WARN  hook-settings: %s could not be parsed: %v\n", pe.File, pe.Err)
		warnings++
	}
	for _, f := range hooks.AllFindings(inv, nil) {
		renderHookFinding(w, f)
		switch f.Level {
		case hooks.LevelWarn, hooks.LevelInfo:
			warnings++
		case hooks.LevelFail:
			fails++
		}
	}
	return warnings, fails
}

// renderHookFinding prints a finding in the doctor's house style:
//
//	LEVEL  code: message
//	        <8-space breakdown for SCAN>
//	       → <remediation for WARN/FAIL/INFO>
//	       • <bullet detail>
func renderHookFinding(w io.Writer, f hooks.Finding) {
	fmt.Fprintf(w, "%-5s %s: %s\n", f.Level, f.Code, f.Message)
	for _, d := range f.Details {
		switch {
		case f.Level == hooks.LevelScan:
			fmt.Fprintf(w, "        %s\n", d)
		case strings.HasPrefix(d, "• "):
			fmt.Fprintf(w, "       %s\n", d)
		default:
			fmt.Fprintf(w, "       → %s\n", d)
		}
	}
}

// knownBuiltinFeeds is the canonical list of feed names that have a Go producer.
// Mirrors the cases in getFeedInterval's switch statement.
var knownBuiltinFeeds = []string{"project", "news", "calendar", "weather", "memories", "insights"}

// isKnownFeed returns true if feedName corresponds to a built-in producer or to
// a custom feed declared in cfg. Unknown orphan files (written by old versions or
// by the Python TUI) return false and should be silently skipped.
func isKnownFeed(feedName string, cfg *core.HooksConfig) bool {
	for _, b := range knownBuiltinFeeds {
		if feedName == b {
			return true
		}
	}
	if cfg != nil {
		for _, c := range cfg.Feeds.Custom {
			if feedName == c.Name {
				return true
			}
		}
	}
	return false
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
		// Skip orphan cache files that have no Go producer and are not in the
		// config's custom feeds. Only known feeds produce actionable diagnostics.
		if !isKnownFeed(feedName, cfg) {
			continue
		}

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

		// Honesty check: a fresh cache with an empty data object means the
		// producer ran but emitted nothing — the segment renders blank. Report
		// it distinctly rather than as "OK" (issue #99: cache-fresh != data-present).
		if len(dataMap) == 0 {
			fmt.Fprintf(w, "WARN  feed:%s: cache fresh but no data\n", feedName)
			warnings++
			feedStatuses[feedName] = "empty"
			continue
		}

		// Zero-liveness check: some producers emit a fully-keyed but all-zero
		// envelope when their data source is empty (issue #98). The segment
		// renders blank even though len(dataMap) > 0, so the empty-map guard
		// above won't catch it. Detect feed-specific zero-liveness conditions.
		if reason := feedZeroLivenessReason(feedName, dataMap); reason != "" {
			fmt.Fprintf(w, "WARN  feed:%s: %s\n", feedName, reason)
			warnings++
			feedStatuses[feedName] = "empty"
			continue
		}

		// Parse timestamp once for staleness and reporting.
		tsStr := envelope["timestamp"].(string)
		ts, parseErr := core.ParseTimeFlex(tsStr)
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
			// Removed builtins (e.g. session, #129) are not feed-backed — a stray
			// config entry must not produce a misleading "feed:<name>" warning.
			if _, removed := removedSegments[name]; removed {
				continue
			}
			// cost is computed from analytics, not feed-backed.
			if name == "cost" {
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

// feedZeroLivenessReason reports whether a fresh, non-empty feed envelope
// actually contains zero live data (cache-fresh != data-present, issue #98).
// Some producers (e.g. insights) emit a fully-keyed but all-zero envelope when
// their data source is empty, which would otherwise pass as OK. Returns a
// human-readable reason when the feed should be flagged, or "" when it looks live.
func feedZeroLivenessReason(feedName string, dataMap map[string]interface{}) string {
	switch feedName {
	case "insights":
		if n, ok := dataMap["total_sessions"].(float64); ok && n == 0 {
			return "cache fresh but no sessions recorded"
		}
	}
	return ""
}

// checkCostHonesty reports a doctor warning when sessions were recorded today
// but the computed cost is still $0 -- the signature of a dead cost writer
// (sessions present but the cost value absent). It reads DailySummary for the
// current UTC day, the same source `hookwise stats` and the status-line use, so
// doctor stays consistent with what the user sees there. Returns the number of
// warnings emitted (0 or 1). Absent or unopenable DBs are silent no-ops -- the
// analytics check (Check 3) already owns reporting those.
func checkCostHonesty(w io.Writer, dbPath string) int {
	if _, err := os.Stat(dbPath); err != nil {
		return 0
	}
	db, err := analytics.Open(dbPath)
	if err != nil {
		return 0
	}
	defer db.Close()

	today := time.Now().UTC().Format("2006-01-02")
	summary, err := analytics.NewAnalytics(db).DailySummary(context.Background(), today)
	if err != nil {
		return 0
	}

	switch {
	case summary.TotalSessions == 0:
		fmt.Fprintln(w, "INFO  cost: no sessions recorded today yet")
		return 0
	case summary.EstimatedCostUSD == 0:
		fmt.Fprintf(w, "WARN  cost: %d session(s) today but $0.00 computed "+
			"(cost tracking may be dead -- see 'hookwise stats')\n", summary.TotalSessions)
		return 1
	default:
		fmt.Fprintf(w, "PASS  cost: $%.2f across %d session(s) today\n",
			summary.EstimatedCostUSD, summary.TotalSessions)
		return 0
	}
}

// getFeedInterval returns the configured interval_seconds for a feed name.
func getFeedInterval(cfg *core.HooksConfig, feedName string) int {
	if cfg == nil {
		return 0
	}
	switch feedName {
	case "project":
		return cfg.Feeds.Project.IntervalSeconds
	case "calendar":
		return cfg.Feeds.Calendar.IntervalSeconds
	case "news":
		return cfg.Feeds.News.IntervalSeconds
	case "insights":
		return cfg.Feeds.Insights.IntervalSeconds
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
