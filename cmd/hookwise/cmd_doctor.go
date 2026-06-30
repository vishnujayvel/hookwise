package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

	// Load config once for the cost-honesty (Check 8) and feed-health (Check 5)
	// checks below; both consult it and doctor is a one-shot diagnostic.
	var doctorCfg *core.HooksConfig
	if loadedCfg, loadErr := core.LoadConfig(cwd); loadErr == nil {
		doctorCfg = &loadedCfg
	}
	costEnabled := doctorCfg != nil && doctorCfg.CostTracking.Enabled

	// Check 8: Cost-tracking honesty. DailySummary aggregates per-session
	// estimated_cost_usd from the SAME UTC day `hookwise stats` and the
	// status-line read, so doctor agrees with what the user sees there. If
	// sessions were recorded today but $0 was computed WHILE cost tracking is
	// enabled, the cost writer is likely dead -- the exact failure that kept
	// stats/status-line silently at $0. When cost tracking is disabled (the
	// default), $0 is expected, not a malfunction. Mirrors the feed
	// zero-liveness / disabled-subsystem honesty principle.
	warnings += checkCostHonesty(w, dbPath, costEnabled)

	// Check 4: Daemon liveness via socket dial (replaces PID file check).
	// Use GetStateDir() to respect HOOKWISE_STATE_DIR env override.
	socketPath := filepath.Join(core.GetStateDir(), "daemon.sock")
	client := feeds.NewDaemonClient(socketPath)
	daemonUp := client.IsRunning()
	if daemonUp {
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

	// Resolve the feed config doctor reports against: the daemon's runtime view
	// is authoritative (#1), falling back to the on-disk global config when the
	// daemon is down or unreachable.
	effectiveFeeds, feedSource := resolveEffectiveFeeds(client, daemonUp)
	switch feedSource {
	case "global-after-error":
		fmt.Fprintln(w, "INFO  feed-config: daemon feed query failed; reporting against on-disk global config")
	case "global":
		fmt.Fprintln(w, "INFO  feed-config: daemon not running; reporting against on-disk global config")
	}

	// Drift: the daemon reads config once at startup. If it is up but its runtime
	// feed config no longer matches the on-disk global config, the user edited the
	// config without restarting — warn so the change isn't silently un-applied.
	if feedSource == "daemon" {
		warnings += checkFeedConfigDrift(w, effectiveFeeds)
	}

	// Back-compat: a project-level feeds: block is ignored by the singleton daemon
	// (#89); tell the user to migrate it to the global config.
	warnings += checkProjectFeedsIgnored(w, cwd)

	// Check 5: Feed health against the daemon's effective feed config. doctorCfg
	// (the project config) is still used for the status-line segment cross-check.
	warnings += checkFeedHealth(w, filepath.Join(stateDir, "state"), effectiveFeeds, doctorCfg)

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
// It seeds the builtin half of feedsFromConfig's enumeration.
var knownBuiltinFeeds = []string{"project", "news", "calendar", "weather", "memories", "insights"}

// feedsFromConfig builds the doctor-side effective feed map from a config. It is
// the fallback used when the daemon is down: it mirrors what the daemon's
// EffectiveFeeds would report for the same config because it resolves
// enabled/interval through the SAME shared feeds.FeedEnabled /
// feeds.EffectiveIntervalSeconds helpers the daemon uses — so doctor can never
// report a feed enabled/stale differently from what the daemon actually polls
// (#1). A nil config yields an empty map (all caches treated as orphans).
func feedsFromConfig(cfg *core.HooksConfig) map[string]feeds.FeedStatus {
	out := map[string]feeds.FeedStatus{}
	if cfg == nil {
		return out
	}
	names := append([]string{}, knownBuiltinFeeds...)
	for _, c := range cfg.Feeds.Custom {
		names = append(names, c.Name)
	}
	for _, name := range names {
		out[name] = feeds.FeedStatus{
			Name:            name,
			Enabled:         feeds.FeedEnabled(cfg.Feeds, name),
			IntervalSeconds: feeds.EffectiveIntervalSeconds(cfg.Feeds, name),
		}
	}
	return out
}

// resolveEffectiveFeeds returns the feed config doctor should report against,
// plus a source tag. The daemon's runtime view (GET /feeds) is authoritative
// (#1); when the daemon is down or the query fails, fall back to the on-disk
// global config — what the daemon WOULD poll on next start — labeling it so the
// user knows it is best-effort. Always fail-open (ARCH-1): never returns an
// error, only an empty map in the worst case.
//
// source is one of: "daemon" (authoritative), "global" (daemon down),
// "global-after-error" (daemon was up but the /feeds query failed).
func resolveEffectiveFeeds(client *feeds.DaemonClient, daemonUp bool) (m map[string]feeds.FeedStatus, source string) {
	if daemonUp {
		if list, err := client.EffectiveFeeds(); err == nil {
			out := make(map[string]feeds.FeedStatus, len(list))
			for _, fs := range list {
				out[fs.Name] = fs
			}
			return out, "daemon"
		}
		// Daemon was up but the query failed — fall through to on-disk, labeled.
		gcfg, err := core.LoadGlobalConfig()
		if err != nil {
			return map[string]feeds.FeedStatus{}, "global-after-error"
		}
		return feedsFromConfig(&gcfg), "global-after-error"
	}
	gcfg, err := core.LoadGlobalConfig()
	if err != nil {
		return map[string]feeds.FeedStatus{}, "global"
	}
	return feedsFromConfig(&gcfg), "global"
}

// checkFeedConfigDrift warns when the daemon is polling a feed config that no
// longer matches the on-disk global config — i.e. the user edited the global
// config but did not restart the daemon (the daemon reads config once at
// startup). Without this, a post-edit global config LOOKS applied while the
// daemon keeps polling the old set. Only meaningful when daemonFeeds came from
// the daemon socket. Returns 1 if it warned.
//
// The comparison is symmetric over the UNION of feed names: a feed present on
// only one side (a custom feed added to or removed from the on-disk config
// without a restart) is drift too, not just an enabled/interval change on a feed
// both sides know about.
func checkFeedConfigDrift(w io.Writer, daemonFeeds map[string]feeds.FeedStatus) int {
	gcfg, err := core.LoadGlobalConfig()
	if err != nil {
		return 0 // fail-open: can't compare, don't warn
	}
	onDisk := feedsFromConfig(&gcfg)

	drifted := map[string]bool{}
	for name, d := range daemonFeeds {
		o, ok := onDisk[name]
		if !ok || d.Enabled != o.Enabled || d.IntervalSeconds != o.IntervalSeconds {
			drifted[name] = true
		}
	}
	for name := range onDisk {
		if _, ok := daemonFeeds[name]; !ok {
			drifted[name] = true
		}
	}
	if len(drifted) == 0 {
		return 0
	}

	names := make([]string, 0, len(drifted))
	for name := range drifted {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintf(w, "WARN  feed-config: daemon is polling a stale feed config (differs for: %s) — restart the daemon (hookwise daemon stop) to apply changes to %s\n",
		strings.Join(names, ", "), filepath.Join(core.GetStateDir(), "config.yaml"))
	return 1
}

// checkProjectFeedsIgnored warns when the project hookwise.yaml in cwd carries a
// feeds: block. Since the daemon is a singleton that sources feed config from
// the global config only (#89), a project-level feeds: block is silently
// ignored for polling — so the user must be told to move those keys to the
// global config, or their per-project feed settings vanish. Returns 1 if warned.
func checkProjectFeedsIgnored(w io.Writer, cwd string) int {
	keys := projectFeedKeys(cwd)
	if len(keys) == 0 {
		return 0
	}
	fmt.Fprintf(w, "WARN  config: feed settings in this project's %s are ignored — the daemon uses %s (move these feed keys there: %s)\n",
		core.ProjectConfigFile, filepath.Join(core.GetStateDir(), "config.yaml"), strings.Join(keys, ", "))
	return 1
}

// projectFeedKeys returns the sorted top-level keys under the feeds: block of
// cwd/hookwise.yaml, or nil if there is no project config or no feeds: block.
func projectFeedKeys(cwd string) []string {
	data, err := os.ReadFile(filepath.Join(cwd, core.ProjectConfigFile))
	if err != nil {
		return nil
	}
	var raw map[string]interface{}
	if yaml.Unmarshal(data, &raw) != nil {
		return nil
	}
	feedsRaw, ok := raw["feeds"].(map[string]interface{})
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(feedsRaw))
	for k := range feedsRaw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// checkFeedHealth reads feed cache files and reports placeholder/stale feeds
// against `effective` — the feed config the daemon is actually polling with
// (#1), resolved by the caller from the daemon socket (or the on-disk global
// config as a fallback). `cfg` is the PROJECT config, used only for the
// status-line segment cross-reference. Returns the number of warnings emitted.
func checkFeedHealth(w io.Writer, cacheDir string, effective map[string]feeds.FeedStatus, cfg *core.HooksConfig) int {
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
		// Skip orphan cache files that the daemon is not polling (no producer in
		// its effective config). Only feeds the daemon actually runs produce
		// actionable diagnostics.
		fs, known := effective[feedName]
		if !known {
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

		// A disabled feed is not expected to be polled, so a leftover cache that
		// is placeholder/empty/zero-lived/stale is not a malfunction — reporting
		// it as WARN ("stale data 304h ago") is misleading. Downgrade those
		// outcomes to a benign INFO "disabled" line. A disabled feed that
		// nonetheless carries fresh, real data still falls through to the healthy
		// "OK" path below, so an actively-polled-elsewhere feed is not mislabelled.
		enabled := fs.Enabled
		reportDisabled := func() {
			fmt.Fprintf(w, "INFO  feed:%s: disabled (cache not refreshed)\n", feedName)
			feedStatuses[feedName] = "disabled"
		}

		// Check placeholder.
		if src, ok := dataMap["source"].(string); ok && src == "placeholder" {
			if !enabled {
				reportDisabled()
				continue
			}
			fmt.Fprintf(w, "WARN  feed:%s: placeholder data (no real source configured)\n", feedName)
			warnings++
			feedStatuses[feedName] = "placeholder"
			continue
		}

		// Honesty check: a fresh cache with an empty data object means the
		// producer ran but emitted nothing — the segment renders blank. Report
		// it distinctly rather than as "OK" (issue #99: cache-fresh != data-present).
		if len(dataMap) == 0 {
			if !enabled {
				reportDisabled()
				continue
			}
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
			if !enabled {
				reportDisabled()
				continue
			}
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

		// Check staleness against the daemon's effective poll interval.
		interval := fs.IntervalSeconds
		if interval > 0 {
			staleThreshold := time.Duration(2*interval) * time.Second
			if age > staleThreshold {
				if !enabled {
					reportDisabled()
					continue
				}
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
// but the computed cost is still $0 AND cost tracking is enabled -- the
// signature of a dead cost writer (sessions present but the cost value absent).
// When cost tracking is disabled (costEnabled=false, the default), a $0 total is
// expected, not a malfunction, and is reported as a benign INFO instead. It
// reads DailySummary for the current UTC day, the same source `hookwise stats`
// and the status-line use, so doctor stays consistent with what the user sees
// there. Returns the number of warnings emitted (0 or 1). Absent or unopenable
// DBs are silent no-ops -- the analytics check (Check 3) already owns those.
func checkCostHonesty(w io.Writer, dbPath string, costEnabled bool) int {
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
		// A $0 total is only a "dead writer" signal when cost tracking is ON.
		// With cost tracking disabled (the default), $0 is the expected state,
		// not a malfunction — report it as benign INFO rather than a misleading
		// "may be dead" WARN. Same disabled-subsystem honesty principle as the
		// feed-health checks (a disabled feed isn't "stale", it's off).
		if !costEnabled {
			fmt.Fprintf(w, "INFO  cost: tracking disabled (%d session(s) today, $0.00)\n", summary.TotalSessions)
			return 0
		}
		fmt.Fprintf(w, "WARN  cost: %d session(s) today but $0.00 computed "+
			"(cost tracking may be dead -- see 'hookwise stats')\n", summary.TotalSessions)
		return 1
	default:
		fmt.Fprintf(w, "PASS  cost: $%.2f across %d session(s) today\n",
			summary.EstimatedCostUSD, summary.TotalSessions)
		return 0
	}
}
