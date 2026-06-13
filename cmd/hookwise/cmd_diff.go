package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
)

func newDiffCmd() *cobra.Command {
	var snapshotsDir string

	cmd := &cobra.Command{
		Use:   "diff <from-ref> <to-ref>",
		Short: "Show row-count changes between two analytics snapshots",
		Long: `Compares two analytics snapshots and shows per-table row-count deltas.

Refs accept:
  latest          — the newest snapshot
  prev            — the second-newest snapshot
  20060102T150405Z — exact snapshot name (without .db extension)
  20060102        — prefix match; if multiple match, picks the newest

Examples:
  hookwise diff prev latest
  hookwise diff 20060101 20060102`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd, args[0], args[1], snapshotsDir)
		},
	}

	cmd.Flags().StringVar(&snapshotsDir, "snapshots-dir", "", "Path to snapshots directory (defaults to ~/.hookwise/snapshots)")
	return cmd
}

func runDiff(cmd *cobra.Command, fromRef, toRef, snapshotsDir string) error {
	if snapshotsDir == "" {
		snapshotsDir = analytics.DefaultSnapshotsDir()
	}

	diff, err := analytics.DiffSnapshots(snapshotsDir, fromRef, toRef)
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "hookwise diff %s..%s\n", diff.From.Name, diff.To.Name)
	fmt.Fprintln(w, strings.Repeat("-", 40))

	changed := false
	for _, td := range diff.Tables {
		if td.Delta == 0 {
			continue
		}
		changed = true
		sign := "+"
		if td.Delta < 0 {
			sign = ""
		}
		fmt.Fprintf(w, "  %-30s  %d -> %d  (%s%d)\n",
			td.Table, td.FromRows, td.ToRows, sign, td.Delta)
	}

	if !changed {
		fmt.Fprintln(w, "No row-count changes.")
	}

	return nil
}
