package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
)

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
