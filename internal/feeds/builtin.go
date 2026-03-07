package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
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
			"recent_activity": "No recent activity (placeholder)",
			"source":          "placeholder",
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
			"name":        "unknown (placeholder)",
			"branch":      "main",
			"last_commit": "n/a",
			"dirty":       false,
			"source":      "placeholder",
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
			"source":     "placeholder",
		},
	}, nil
}

// WeatherProducer fetches live weather data from the Open-Meteo API.
// It implements ConfigAware to receive latitude/longitude from feed config.
type WeatherProducer struct {
	mu         sync.Mutex
	feedsCfg   core.FeedsConfig
	lastResult map[string]interface{} // cached last successful result for fallback
	client     *http.Client
}

func (p *WeatherProducer) Name() string { return "weather" }

// SetFeedsConfig receives the feed configuration (ConfigAware interface).
func (p *WeatherProducer) SetFeedsConfig(cfg core.FeedsConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.feedsCfg = cfg
}

// openMeteoResponse is the JSON structure returned by the Open-Meteo API.
type openMeteoResponse struct {
	Current struct {
		Temperature2m float64 `json:"temperature_2m"`
		WindSpeed10m  float64 `json:"wind_speed_10m"`
		WeatherCode   int     `json:"weather_code"`
	} `json:"current"`
}

// wmoCodeToEmoji maps WMO weather codes to emoji strings.
func wmoCodeToEmoji(code int) string {
	switch {
	case code == 0:
		return "\u2600\ufe0f" // sunny ☀️
	case code >= 1 && code <= 3:
		return "\u26c5" // partly cloudy ⛅
	case code >= 45 && code <= 48:
		return "\U0001f32b\ufe0f" // fog 🌫️
	case code >= 51 && code <= 67:
		return "\U0001f327\ufe0f" // rain 🌧️
	case code >= 71 && code <= 77:
		return "\u2744\ufe0f" // snow ❄️
	case code >= 80 && code <= 82:
		return "\U0001f326\ufe0f" // sun behind rain cloud 🌦️
	case code >= 95 && code <= 99:
		return "\u26c8\ufe0f" // thunderstorm ⛈️
	default:
		return "\U0001f324\ufe0f" // sun behind small cloud 🌤️
	}
}

// wmoCodeToDescription maps WMO weather codes to human-readable descriptions.
func wmoCodeToDescription(code int) string {
	switch {
	case code == 0:
		return "Clear"
	case code >= 1 && code <= 3:
		return "Partly Cloudy"
	case code >= 45 && code <= 48:
		return "Fog"
	case code >= 51 && code <= 55:
		return "Drizzle"
	case code >= 56 && code <= 57:
		return "Freezing Drizzle"
	case code >= 61 && code <= 65:
		return "Rain"
	case code >= 66 && code <= 67:
		return "Freezing Rain"
	case code >= 71 && code <= 75:
		return "Snow"
	case code >= 77 && code <= 77:
		return "Snow Grains"
	case code >= 80 && code <= 82:
		return "Showers"
	case code >= 95 && code <= 99:
		return "Thunderstorm"
	default:
		return "Unknown"
	}
}

func (p *WeatherProducer) Produce(ctx context.Context) (interface{}, error) {
	p.mu.Lock()
	cfg := p.feedsCfg.Weather
	p.mu.Unlock()

	// Default coordinates (San Francisco) if not configured.
	lat := cfg.Latitude
	lon := cfg.Longitude
	if lat == 0 && lon == 0 {
		lat = 37.7749
		lon = -122.4194
	}

	tempUnit := "fahrenheit"
	if cfg.TemperatureUnit == "celsius" {
		tempUnit = "celsius"
	}

	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,wind_speed_10m,weather_code&temperature_unit=%s",
		lat, lon, tempUnit,
	)

	if p.client == nil {
		p.client = &http.Client{Timeout: 5 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return p.fallbackResult("request error"), nil
	}

	resp, err := p.client.Do(req)
	if err != nil {
		core.Logger().Warn("weather: API request failed, using cached data", "error", err)
		return p.fallbackResult("api error"), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		core.Logger().Warn("weather: API returned non-200", "status", resp.StatusCode)
		return p.fallbackResult("bad status"), nil
	}

	var apiResp openMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		core.Logger().Warn("weather: failed to decode API response", "error", err)
		return p.fallbackResult("decode error"), nil
	}

	code := apiResp.Current.WeatherCode

	result := map[string]interface{}{
		"type":      "weather",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"temperature":     apiResp.Current.Temperature2m,
			"temperatureUnit": tempUnit,
			"windSpeed":       apiResp.Current.WindSpeed10m,
			"weatherCode":     code,
			"emoji":           wmoCodeToEmoji(code),
			"description":     wmoCodeToDescription(code),
		},
	}

	// Cache for fallback on future failures.
	p.mu.Lock()
	p.lastResult = result
	p.mu.Unlock()

	return result, nil
}

// fallbackResult returns the last cached result if available, or a placeholder.
func (p *WeatherProducer) fallbackResult(reason string) map[string]interface{} {
	p.mu.Lock()
	last := p.lastResult
	p.mu.Unlock()

	if last != nil {
		core.Logger().Debug("weather: using cached fallback", "reason", reason)
		return last
	}

	// No cached data — return a safe placeholder with correct field names.
	return map[string]interface{}{
		"type":      "weather",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"temperature":     0,
			"temperatureUnit": "fahrenheit",
			"windSpeed":       0,
			"weatherCode":     0,
			"emoji":           "\U0001f324\ufe0f",
			"description":     "Unavailable",
		},
	}
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
			"source":           "placeholder",
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
			"source":          "placeholder",
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
			"source":            "placeholder",
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
