package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
)

func newSnapshotCmd() *cobra.Command {
	var (
		dataDir      string
		snapshotsDir string
		retention    int
	)

	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Take a point-in-time snapshot of the analytics database",
		Long: "Writes a consistent VACUUM INTO copy of the analytics SQLite " +
			"database to ~/.hookwise/snapshots/ and prunes older snapshots " +
			"beyond the configured retention.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshot(cmd, dataDir, snapshotsDir, retention)
		},
	}

	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Path to the analytics SQLite DB file (defaults to config / ~/.hookwise/analytics.db)")
	cmd.Flags().StringVar(&snapshotsDir, "snapshots-dir", "", "Directory to write snapshots to (defaults to ~/.hookwise/snapshots)")
	cmd.Flags().IntVar(&retention, "retention", -1, "Number of snapshots to keep (defaults to config snapshot_retention)")
	return cmd
}

func runSnapshot(cmd *cobra.Command, dataDir, snapshotsDir string, retention int) error {
	// Load config the same way other commands do, falling back to defaults
	// (ARCH-1: never hard-fail on a missing/broken config).
	cwd, _ := os.Getwd()
	config, err := core.LoadConfig(cwd)
	if err != nil {
		config = core.GetDefaultConfig()
	}

	// Resolve the DB path: explicit flag wins, then config, then default.
	dbPath := dataDir
	if dbPath == "" {
		dbPath = config.Analytics.DBPath
	}

	// Resolve the snapshots dir: explicit flag wins, else the default.
	if snapshotsDir == "" {
		snapshotsDir = analytics.DefaultSnapshotsDir()
	}

	// Resolve retention: explicit non-negative flag wins, else config.
	keep := retention
	if keep < 0 {
		keep = config.Analytics.SnapshotRetention
	}

	db, err := analytics.Open(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open analytics DB: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	path, err := db.Snapshot(ctx, snapshotsDir)
	if err != nil {
		return fmt.Errorf("snapshot failed: %w", err)
	}

	pruned, err := analytics.PruneSnapshots(snapshotsDir, keep)
	if err != nil {
		return fmt.Errorf("prune failed: %w", err)
	}

	remaining, err := analytics.ListSnapshots(snapshotsDir)
	if err != nil {
		return fmt.Errorf("list snapshots failed: %w", err)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "snapshot written: %s\n", path)
	fmt.Fprintf(w, "pruned: %d\n", len(pruned))
	fmt.Fprintf(w, "retained: %d\n", len(remaining))
	return nil
}
