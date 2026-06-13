package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
)

func newLogCmd() *cobra.Command {
	var (
		snapshotsDir string
		limit        int
	)

	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show analytics snapshot history",
		Long: `Displays recent analytics snapshots from ~/.hookwise/snapshots (newest first).

Each snapshot is a consistent point-in-time copy of the analytics database,
created periodically by the daemon via VACUUM INTO. Use hookwise diff to
compare two snapshots.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLog(cmd, snapshotsDir, limit)
		},
	}

	cmd.Flags().StringVar(&snapshotsDir, "snapshots-dir", "", "Path to snapshots directory (defaults to ~/.hookwise/snapshots)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum number of snapshots to show (0 = all)")
	return cmd
}

func runLog(cmd *cobra.Command, snapshotsDir string, limit int) error {
	if snapshotsDir == "" {
		snapshotsDir = analytics.DefaultSnapshotsDir()
	}

	infos, err := analytics.ListSnapshotInfos(snapshotsDir, limit)
	if err != nil {
		return fmt.Errorf("log: %w", err)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "hookwise log")
	fmt.Fprintln(w, strings.Repeat("-", 40))

	if len(infos) == 0 {
		fmt.Fprintln(w, "No snapshots found.")
		return nil
	}

	for _, info := range infos {
		dateStr := info.Time.Format("2006-01-02 15:04:05")
		sizeStr := humanizeBytes(info.SizeBytes)
		sessions := info.RowCounts["sessions"]
		events := info.RowCounts["events"]
		fmt.Fprintf(w, "%s  %s  %s  %d sessions, %d events\n",
			info.Name, dateStr, sizeStr, sessions, events)
	}

	return nil
}

// humanizeBytes formats a byte count as a human-readable string (B, KB, MB).
func humanizeBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
