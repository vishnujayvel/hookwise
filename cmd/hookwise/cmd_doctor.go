package main

import (
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
	"gopkg.in/yaml.v3"
)

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
