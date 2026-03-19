package coaching

// Service is the future home of coaching evaluation logic:
//
//   - AlertLevel computation from tooling minutes and builder's trap thresholds
//   - MetacognitionResult selection based on mode, interval, and rapid-acceptance triggers
//   - GrammarResult evaluation for communication coaching
//
// The evaluation functions do not exist yet; the notification-creation helpers
// (CheckCoaching, formatCoachingContent) remain in internal/notifications
// because they are notification producers, not domain evaluators.
type Service struct {
	// Fields will be added as evaluation logic is implemented.
}
