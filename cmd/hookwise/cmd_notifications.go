package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
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
		Long: `Shows recent notifications from budget, guard effectiveness, and coaching
producers. Notifications are stored in the Dolt analytics database and
surfaced via the status line or this command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotifications(cmd, dataDir, limit)
		},
	}

	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Dolt data directory (defaults to ~/.hookwise/dolt)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of notifications to show")

	return cmd
}

func runNotifications(cmd *cobra.Command, dataDir string, limit int) error {
	db, err := analytics.Open(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open Dolt DB: %w", err)
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
