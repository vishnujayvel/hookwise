package feeds

import "context"

// InsightsSource supplies aggregated usage metrics for the insights feed.
// Implemented in the cmd layer over the analytics DB (ARCH-3: feeds must not
// import analytics; the concrete impl is injected from package main).
type InsightsSource interface {
	InsightsSummary(ctx context.Context, stalenessDays int) (InsightsSummary, error)
}

// InsightsToolCount is a single tool-usage entry returned by InsightsSource.
type InsightsToolCount struct {
	Name  string
	Count int
}

// InsightsRecentSession holds the most-recent session metrics available from
// the analytics layer.
type InsightsRecentSession struct {
	ID          string
	DurationMin float64
	LinesAdded  int
}

// InsightsSummary is the feeds-side DTO for aggregated usage metrics.
// It is intentionally a separate copy from any analytics DTO so that feeds
// does not import internal/analytics (ARCH-3).
type InsightsSummary struct {
	TotalSessions    int
	TotalLinesAdded  int
	AvgDurationMin   float64
	TopTools         []InsightsToolCount
	PeakHourUTC      int
	DaysActive       int
	RecentDaysActive int
	Recent           InsightsRecentSession
}

// MapInsightsSummary converts a DB-backed InsightsSummary into the insights
// feed's envelope `data` map. The shape is byte-for-byte the same keys/types the
// existing producer emits, so a later PR can swap the data source without
// changing the feed contract. Fields with no analytics source (messages,
// friction, outcome, tool_errors, recent_msgs_per_day) map to the same zero
// defaults as zeroedEnvelope.
func MapInsightsSummary(s InsightsSummary, stalenessDays int) map[string]interface{} {
	topTools := make([]map[string]interface{}, 0, len(s.TopTools))
	for _, t := range s.TopTools {
		topTools = append(topTools, map[string]interface{}{
			"name":  t.Name,
			"count": t.Count,
		})
	}

	return map[string]interface{}{
		"total_sessions":       s.TotalSessions,
		"total_messages":       0,
		"total_lines_added":    s.TotalLinesAdded,
		"avg_duration_minutes": s.AvgDurationMin,
		"top_tools":            topTools,
		"friction_counts":      map[string]interface{}{},
		"friction_total":       0,
		"peak_hour":            s.PeakHourUTC,
		"days_active":          s.DaysActive,
		"staleness_days":       stalenessDays,
		"recent_msgs_per_day":  0,
		"recent_messages":      0,
		"recent_days_active":   s.RecentDaysActive,
		"recent_session": map[string]interface{}{
			"id":               s.Recent.ID,
			"duration_minutes": s.Recent.DurationMin,
			"lines_added":      s.Recent.LinesAdded,
			"friction_count":   0,
			"outcome":          "",
			"tool_errors":      0,
		},
	}
}
