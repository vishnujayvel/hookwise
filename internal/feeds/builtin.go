package feeds

import (
	"context"
	"time"
)

// ---------------------------------------------------------------------------
// Built-in feed producers (stub implementations).
// Each returns placeholder data; actual API/data-fetching logic is deferred.
// ---------------------------------------------------------------------------

// PulseProducer returns session count and recent activity summary.
type PulseProducer struct{}

func (p *PulseProducer) Name() string { return "pulse" }
func (p *PulseProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      "pulse",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"session_count":   0,
			"active_sessions": 0,
			"recent_activity": "No recent activity",
		},
	}, nil
}

// ProjectProducer returns project directory info.
type ProjectProducer struct{}

func (p *ProjectProducer) Name() string { return "project" }
func (p *ProjectProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      "project",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"name":        "unknown",
			"branch":      "main",
			"last_commit": "n/a",
			"dirty":       false,
		},
	}, nil
}

// NewsProducer returns placeholder news items.
type NewsProducer struct{}

func (p *NewsProducer) Name() string { return "news" }
func (p *NewsProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      "news",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"stories": []interface{}{
				map[string]interface{}{
					"title": "Placeholder story",
					"url":   "https://example.com",
					"score": 0,
				},
			},
			"source": "placeholder",
		},
	}, nil
}

// CalendarProducer returns placeholder calendar entries.
type CalendarProducer struct{}

func (p *CalendarProducer) Name() string { return "calendar" }
func (p *CalendarProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      "calendar",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"events":     []interface{}{},
			"next_event": nil,
		},
	}, nil
}

// WeatherProducer returns placeholder weather data.
type WeatherProducer struct{}

func (p *WeatherProducer) Name() string { return "weather" }
func (p *WeatherProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      "weather",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"temperature":  0,
			"unit":         "F",
			"condition":    "unknown",
			"humidity":     0,
			"wind_speed":   0,
		},
	}, nil
}

// PracticeProducer returns practice session summary.
type PracticeProducer struct{}

func (p *PracticeProducer) Name() string { return "practice" }
func (p *PracticeProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      "practice",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"total_sessions":   0,
			"streak_days":      0,
			"last_practice_at": nil,
			"focus_area":       "none",
		},
	}, nil
}

// MemoriesProducer returns placeholder memories.
type MemoriesProducer struct{}

func (p *MemoriesProducer) Name() string { return "memories" }
func (p *MemoriesProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      "memories",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"recent_memories": []interface{}{},
			"total_count":     0,
		},
	}, nil
}

// InsightsProducer returns placeholder insights.
type InsightsProducer struct{}

func (p *InsightsProducer) Name() string { return "insights" }
func (p *InsightsProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      "insights",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"productivity_score": 0,
			"suggestions":       []interface{}{},
			"staleness_days":    0,
		},
	}, nil
}

// RegisterBuiltins registers all 8 built-in feed producers with the registry.
func RegisterBuiltins(r *Registry) {
	r.Register(&PulseProducer{})
	r.Register(&ProjectProducer{})
	r.Register(&NewsProducer{})
	r.Register(&CalendarProducer{})
	r.Register(&WeatherProducer{})
	r.Register(&PracticeProducer{})
	r.Register(&MemoriesProducer{})
	r.Register(&InsightsProducer{})
}
