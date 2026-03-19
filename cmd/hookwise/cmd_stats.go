package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
)

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
