package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// resolveAnalyticsDBPath picks the analytics DB path the way reader commands
// must agree with the writer (dispatch): an explicit --data-dir flag wins,
// otherwise the loaded config's Analytics.DBPath is used. The returned value
// may be empty, in which case analytics.Open falls back to DefaultDBPath().
// See #105: stats previously ignored config and read the default DB, showing
// $0 whenever a custom analytics.db_path was set.
func resolveAnalyticsDBPath(flagDataDir string, config core.HooksConfig) string {
	if flagDataDir != "" {
		return flagDataDir
	}
	return config.Analytics.DBPath
}

func newStatsCmd() *cobra.Command {
	var dataDir string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show analytics dashboard for today",
		Long:  "Opens the analytics database and displays today's daily summary and tool breakdown.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStats(cmd, dataDir)
		},
	}

	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Path to the analytics SQLite DB file (defaults to config analytics.db_path / ~/.hookwise/analytics.db)")
	return cmd
}

func runStats(cmd *cobra.Command, dataDir string) error {
	// Load config the same way the writer (dispatch) does so reader and writer
	// agree on the DB path (ARCH-1: never hard-fail on a missing/broken config).
	cwd, _ := os.Getwd()
	config, err := core.LoadConfig(cwd)
	if err != nil {
		config = core.GetDefaultConfig()
	}

	db, err := analytics.Open(resolveAnalyticsDBPath(dataDir, config))
	if err != nil {
		return fmt.Errorf("failed to open analytics DB: %w", err)
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
