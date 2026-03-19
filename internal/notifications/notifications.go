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
	ID         int        `json:"id"`
	Producer   string     `json:"producer"`
	Type       string     `json:"type"`
	Content    string     `json:"content"`
	CreatedAt  time.Time  `json:"created_at"`
	SurfacedAt *time.Time `json:"surfaced_at,omitempty"`
	ActedOn    bool       `json:"acted_on"`
	Branch     string     `json:"branch,omitempty"`
	TTLSeconds int        `json:"ttl_seconds"`
	Expired    bool       `json:"expired"`
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

// CreateWithTTL inserts a new notification with a custom TTL (in seconds).
func (ns *NotificationService) CreateWithTTL(ctx context.Context, producer, notifType, content string, ttlSeconds int) error {
	_, err := ns.db.Exec(ctx,
		`INSERT INTO notifications (producer, notification_type, content, created_at, ttl_seconds)
		 VALUES (?, ?, ?, ?, ?)`,
		producer, notifType, content, time.Now().UTC().Format(time.RFC3339), ttlSeconds,
	)
	if err != nil {
		return fmt.Errorf("notifications: create with ttl: %w", err)
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
		`SELECT id, producer, notification_type, content, created_at, surfaced_at, acted_on, branch, ttl_seconds
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
		`SELECT id, producer, notification_type, content, created_at, surfaced_at, acted_on, branch, ttl_seconds
		 FROM notifications
		 WHERE surfaced_at IS NULL
		 ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("notifications: unsurfaced: %w", err)
	}
	defer rows.Close()

	all, err := scanNotifications(rows)
	if err != nil {
		return nil, err
	}

	// Filter out expired notifications in Go (avoids Dolt SQL date math
	// issues on TEXT-stored timestamps).
	var active []Notification
	for _, n := range all {
		if !n.Expired {
			active = append(active, n)
		}
	}
	return active, nil
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
		var ttlSeconds sql.NullInt64

		if err := rows.Scan(&n.ID, &n.Producer, &n.Type, &n.Content,
			&createdAt, &surfacedAt, &actedOn, &branch, &ttlSeconds); err != nil {
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

		// TTL: default to 86400 (24h) if NULL.
		if ttlSeconds.Valid {
			n.TTLSeconds = int(ttlSeconds.Int64)
		} else {
			n.TTLSeconds = 86400
		}

		// Compute Expired at read time.
		if n.TTLSeconds > 0 && !n.CreatedAt.IsZero() {
			n.Expired = time.Since(n.CreatedAt) > time.Duration(n.TTLSeconds)*time.Second
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
