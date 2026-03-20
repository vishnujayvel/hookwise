package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
)

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
