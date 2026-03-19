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
		return "\u2600\ufe0f" // sunny
	case code >= 1 && code <= 3:
		return "\u26c5" // partly cloudy
	case code >= 45 && code <= 48:
		return "\U0001f32b\ufe0f" // fog
	case code >= 51 && code <= 67:
		return "\U0001f327\ufe0f" // rain
	case code >= 71 && code <= 77:
		return "\u2744\ufe0f" // snow
	case code >= 80 && code <= 82:
		return "\U0001f326\ufe0f" // sun behind rain cloud
	case code >= 95 && code <= 99:
		return "\u26c8\ufe0f" // thunderstorm
	default:
		return "\U0001f324\ufe0f" // sun behind small cloud
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
	case code == 77:
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

	result := NewEnvelope("weather", map[string]interface{}{
		"temperature":     apiResp.Current.Temperature2m,
		"temperatureUnit": tempUnit,
		"windSpeed":       apiResp.Current.WindSpeed10m,
		"weatherCode":     code,
		"emoji":           wmoCodeToEmoji(code),
		"description":     wmoCodeToDescription(code),
	})

	// Cache for fallback on future failures.
	p.mu.Lock()
	p.lastResult = result
	p.mu.Unlock()

	return result, nil
}

// fallbackResult returns the last cached result if available, or a placeholder.
// When no cached data exists, temperature and weatherCode are nil so the Python
// TUI renders "--" instead of a plausible-looking 0°F.
func (p *WeatherProducer) fallbackResult(reason string) map[string]interface{} {
	p.mu.Lock()
	last := p.lastResult
	unit := "fahrenheit"
	if p.feedsCfg.Weather.TemperatureUnit == "celsius" {
		unit = "celsius"
	}
	p.mu.Unlock()

	if last != nil {
		core.Logger().Debug("weather: using cached fallback", "reason", reason)
		return last
	}

	// No cached data — return placeholder with nil numerics so consumers
	// show "--" instead of interpreting 0 as a real reading.
	return NewEnvelope("weather", map[string]interface{}{
		"temperature":     nil,
		"temperatureUnit": unit,
		"windSpeed":       0,
		"weatherCode":     nil,
		"emoji":           "\U0001f324\ufe0f",
		"description":     "Unavailable",
	})
}

// WeatherTestFixture returns a deterministic weather envelope for use in
// cross-package tests (e.g., bridge_test.go). This is a shared fixture so
// that bridge tests bind to the producer's actual field names rather than
// hardcoding keys that can silently drift.
func WeatherTestFixture() map[string]interface{} {
	return NewEnvelopeAt("weather", map[string]interface{}{
		"temperature":     float64(72),
		"temperatureUnit": "fahrenheit",
		"windSpeed":       float64(5.3),
		"weatherCode":     float64(0),
		"emoji":           "\u2600\ufe0f",
		"description":     "Clear",
	}, time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC))
}
