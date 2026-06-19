package notifications

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vishnujayvel/hookwise/internal/analytics"
)

// Producer name constants used in the notifications table.
const (
	ProducerBudget = "budget"
)

// Notification type constants.
const (
	TypeBudgetThreshold = "budget_threshold"
)

// ---------------------------------------------------------------------------
// Orchestrator
// ---------------------------------------------------------------------------

// RunAll runs every notification producer best-effort. A failure in one does
// NOT stop the others; all errors are joined and returned (callers log + ignore
// per ARCH-1). costState may be nil (producers no-op on nil).
func RunAll(ctx context.Context, ns *NotificationService, db *analytics.DB, costState *analytics.CostState, budgetThreshold float64) error {
	var errs []error
	if err := CheckBudget(ctx, ns, costState, budgetThreshold); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// ---------------------------------------------------------------------------
// Budget threshold notifications (R12.1)
// ---------------------------------------------------------------------------

// CheckBudget checks if the current daily cost exceeds the given threshold
// and creates a budget notification if so.
//
// It avoids duplicate notifications by checking whether a budget_threshold
// notification was already created today.
func CheckBudget(ctx context.Context, ns *NotificationService, costState *analytics.CostState, threshold float64) error {
	if costState == nil || threshold <= 0 {
		return nil
	}

	if costState.TotalToday < threshold {
		return nil
	}

	// Check for an existing notification today to avoid duplicates.
	today := time.Now().UTC().Format("2006-01-02")
	alreadyNotified, err := hasNotificationToday(ctx, ns, ProducerBudget, TypeBudgetThreshold, today)
	if err != nil {
		return fmt.Errorf("notifications: check budget: %w", err)
	}
	if alreadyNotified {
		return nil
	}

	content := fmt.Sprintf(
		"Daily cost threshold exceeded: $%.2f spent today (threshold: $%.2f)",
		costState.TotalToday, threshold,
	)

	return ns.Create(ctx, ProducerBudget, TypeBudgetThreshold, content)
}

// ---------------------------------------------------------------------------
// Deduplication helpers
// ---------------------------------------------------------------------------

// hasNotificationToday checks if a notification with the given producer and
// type was already created today.
func hasNotificationToday(ctx context.Context, ns *NotificationService, producer, notifType, today string) (bool, error) {
	row := ns.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications
		 WHERE producer = ? AND notification_type = ? AND created_at LIKE ?`,
		producer, notifType, today+"%",
	)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
