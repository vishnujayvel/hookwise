// Package notifications provides a notification service for hookwise.
//
// Notifications are created by producers (budget, guard effectiveness,
// coaching) and stored in the Dolt notifications table. They can be
// queried for display via the CLI or surfaced in the status line.
package notifications

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// Notification represents a single notification row from the Dolt table.
type Notification struct {
	ID         int
	Producer   string
	Type       string
	Content    string
	CreatedAt  time.Time
	SurfacedAt *time.Time
	ActedOn    bool
	Branch     string
}

// NotificationService manages creating and querying notifications via Dolt.
type NotificationService struct {
	db *analytics.DB
}

// NewNotificationService creates a new NotificationService backed by the given DB.
func NewNotificationService(db *analytics.DB) *NotificationService {
	return &NotificationService{db: db}
}

// Create inserts a new notification into the notifications table.
func (ns *NotificationService) Create(ctx context.Context, producer, notifType, content string) error {
	_, err := ns.db.Exec(ctx,
		`INSERT INTO notifications (producer, notification_type, content, created_at)
		 VALUES (?, ?, ?, ?)`,
		producer, notifType, content, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("notifications: create: %w", err)
	}
	return nil
}

// List returns the most recent notifications, up to limit.
// Results are ordered by creation time descending (newest first).
func (ns *NotificationService) List(ctx context.Context, limit int) ([]Notification, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := ns.db.Query(ctx,
		`SELECT id, producer, notification_type, content, created_at, surfaced_at, acted_on, branch
		 FROM notifications
		 ORDER BY id DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("notifications: list: %w", err)
	}
	defer rows.Close()

	return scanNotifications(rows)
}

// Unsurfaced returns notifications that haven't been shown to the user yet
// (surfaced_at IS NULL). Results are ordered by creation time ascending
// (oldest first so they can be shown in chronological order).
func (ns *NotificationService) Unsurfaced(ctx context.Context) ([]Notification, error) {
	rows, err := ns.db.Query(ctx,
		`SELECT id, producer, notification_type, content, created_at, surfaced_at, acted_on, branch
		 FROM notifications
		 WHERE surfaced_at IS NULL
		 ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("notifications: unsurfaced: %w", err)
	}
	defer rows.Close()

	return scanNotifications(rows)
}

// MarkSurfaced marks a notification as surfaced by setting surfaced_at to now.
func (ns *NotificationService) MarkSurfaced(ctx context.Context, id int) error {
	_, err := ns.db.Exec(ctx,
		`UPDATE notifications SET surfaced_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("notifications: mark surfaced: %w", err)
	}
	return nil
}

// scanNotifications scans rows into a slice of Notification.
func scanNotifications(rows *sql.Rows) ([]Notification, error) {
	var result []Notification
	for rows.Next() {
		var n Notification
		var createdAt string
		var surfacedAt sql.NullString
		var actedOn int
		var branch sql.NullString

		if err := rows.Scan(&n.ID, &n.Producer, &n.Type, &n.Content,
			&createdAt, &surfacedAt, &actedOn, &branch); err != nil {
			return nil, fmt.Errorf("notifications: scan: %w", err)
		}

		n.CreatedAt = parseTime(createdAt)
		if surfacedAt.Valid && surfacedAt.String != "" {
			t := parseTime(surfacedAt.String)
			n.SurfacedAt = &t
		}
		n.ActedOn = actedOn != 0
		if branch.Valid {
			n.Branch = branch.String
		}

		result = append(result, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notifications: rows: %w", err)
	}
	return result, nil
}

// parseTime delegates to core.ParseTimeFlex, returning zero time on failure.
func parseTime(s string) time.Time {
	t, _ := core.ParseTimeFlex(s)
	return t
}
