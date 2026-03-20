package notifications

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vishnujayvel/hookwise/internal/analytics"
)

// Producer name constants used in the notifications table.
const (
	ProducerBudget     = "budget"
	ProducerGuard      = "guard"
	ProducerCoaching   = "coaching"
)

// Notification type constants.
const (
	TypeBudgetThreshold     = "budget_threshold"
	TypeGuardEffectiveness  = "guard_effectiveness"
	TypeCoachingPrompt      = "coaching_prompt"
)

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
// Guard effectiveness notifications (R12.2)
// ---------------------------------------------------------------------------

// CheckGuardEffectiveness queries for tools that have been blocked frequently
// (5 or more times today) and creates a notification for each one that
// hasn't already been notified about today.
func CheckGuardEffectiveness(ctx context.Context, ns *NotificationService, db *analytics.DB) error {
	if db == nil {
		return nil
	}

	today := time.Now().UTC().Format("2006-01-02")

	// Query events table for tools that were blocked today via the
	// analytics DB method (SQL lives in the analytics package).
	summaries, err := db.GuardBlockSummaries(ctx, today)
	if err != nil {
		return fmt.Errorf("notifications: check guard effectiveness: %w", err)
	}

	for _, s := range summaries {
		if s.BlockCount < 5 {
			continue
		}

		// Check for existing notification today for this tool.
		content := fmt.Sprintf(
			"Guard rule for %q triggered %d times today -- consider reviewing the rule's effectiveness",
			s.ToolName, s.BlockCount,
		)

		// Deduplicate: check if we already notified about this tool today.
		alreadyNotified, err := hasNotificationTodayWithContent(ctx, ns, ProducerGuard, TypeGuardEffectiveness, today, s.ToolName)
		if err != nil {
			return err
		}
		if alreadyNotified {
			continue
		}

		if err := ns.Create(ctx, ProducerGuard, TypeGuardEffectiveness, content); err != nil {
			return err
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Coaching prompt notifications (R12.3)
// ---------------------------------------------------------------------------

// CheckCoaching checks coaching state and creates a notification if the
// alert level has changed to something actionable (yellow, orange, or red).
//
// It avoids duplicate notifications by checking whether a coaching notification
// with the same alert level was already created today.
func CheckCoaching(ctx context.Context, ns *NotificationService, coachState *analytics.CoachingState) error {
	if coachState == nil {
		return nil
	}

	// Only notify on elevated alert levels.
	alertLevel := coachState.AlertLevel
	if alertLevel == "none" || alertLevel == "" {
		return nil
	}

	today := time.Now().UTC().Format("2006-01-02")

	// Deduplicate: check if we already sent a coaching notification
	// with this alert level today.
	alreadyNotified, err := hasNotificationTodayWithContent(ctx, ns, ProducerCoaching, TypeCoachingPrompt, today, alertLevel)
	if err != nil {
		return fmt.Errorf("notifications: check coaching: %w", err)
	}
	if alreadyNotified {
		return nil
	}

	content := formatCoachingContent(coachState)
	return ns.Create(ctx, ProducerCoaching, TypeCoachingPrompt, content)
}

// formatCoachingContent builds a human-readable coaching notification.
func formatCoachingContent(state *analytics.CoachingState) string {
	switch state.AlertLevel {
	case "yellow":
		return fmt.Sprintf(
			"Coaching alert (yellow): %.0f minutes of tooling detected in %s mode. Consider taking a step back.",
			state.ToolingMinutes, state.CurrentMode,
		)
	case "orange":
		return fmt.Sprintf(
			"Coaching alert (orange): %.0f minutes of heavy tooling in %s mode. Time for a review pause.",
			state.ToolingMinutes, state.CurrentMode,
		)
	case "red":
		return fmt.Sprintf(
			"Coaching alert (red): %.0f minutes of continuous tooling in %s mode. Strongly recommend stopping to review.",
			state.ToolingMinutes, state.CurrentMode,
		)
	default:
		return fmt.Sprintf(
			"Coaching alert (%s): current mode is %s with %.0f tooling minutes.",
			state.AlertLevel, state.CurrentMode, state.ToolingMinutes,
		)
	}
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

// hasNotificationTodayWithContent checks if a notification with the given
// producer, type, and content substring was already created today.
func hasNotificationTodayWithContent(ctx context.Context, ns *NotificationService, producer, notifType, today, contentSubstr string) (bool, error) {
	escaped := escapeLIKE(contentSubstr)
	row := ns.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications
		 WHERE producer = ? AND notification_type = ? AND created_at LIKE ? AND content LIKE ?`,
		producer, notifType, today+"%", "%"+escaped+"%",
	)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// escapeLIKE escapes SQL LIKE wildcard characters (% and _) in a string.
func escapeLIKE(s string) string {
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}
