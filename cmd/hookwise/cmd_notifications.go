package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
	"github.com/vishnujayvel/hookwise/internal/notifications"
)

func newNotificationsCmd() *cobra.Command {
	var (
		dataDir string
		limit   int
	)

	cmd := &cobra.Command{
		Use:   "notifications",
		Short: "Display notification history",
		Long: `Shows recent notifications from the budget producer.
Notifications are stored in the SQLite analytics database and
surfaced via the status line or this command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotifications(cmd, dataDir, limit)
		},
	}

	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Path to the analytics SQLite DB file (defaults to config analytics.db_path / ~/.hookwise/analytics.db)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of notifications to show")

	return cmd
}

func runNotifications(cmd *cobra.Command, dataDir string, limit int) error {
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
	ns := notifications.NewNotificationService(db)

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "hookwise notifications")
	fmt.Fprintln(w, strings.Repeat("-", 50))

	notifs, err := ns.List(ctx, limit)
	if err != nil {
		return fmt.Errorf("failed to list notifications: %w", err)
	}

	if len(notifs) == 0 {
		fmt.Fprintln(w, "No notifications.")
		return nil
	}

	for _, n := range notifs {
		ts := n.CreatedAt.Format("2006-01-02 15:04")
		surfaced := " "
		if n.SurfacedAt != nil {
			surfaced = "*"
		}

		fmt.Fprintf(w, "%s [%s] %-8s %-22s %s\n",
			surfaced, ts, n.Producer, n.Type, n.Content)
	}

	fmt.Fprintln(w, strings.Repeat("-", 50))
	fmt.Fprintf(w, "%d notification(s) shown.\n", len(notifs))

	// Mark all unsurfaced notifications as surfaced now that they've been displayed.
	unsurfaced, err := ns.Unsurfaced(ctx)
	if err != nil {
		return fmt.Errorf("failed to query unsurfaced: %w", err)
	}
	for _, n := range unsurfaced {
		_ = ns.MarkSurfaced(ctx, n.ID)
	}

	return nil
}
