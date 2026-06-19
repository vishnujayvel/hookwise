package feeds

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// TestWeatherProducer_FallbackNoCache_NilNumerics pins the documented contract of
// fallbackResult (producer_weather.go): when the Open-Meteo API is unavailable
// AND no prior result is cached, every NUMERIC reading must be nil so consumers
// render "--" rather than interpreting a literal 0 as a real measurement.
// windSpeed was the one numeric field set to 0, breaking the symmetry with
// temperature/weatherCode and the function's own "nil numerics" comment -- a
// cross-boundary 0-vs-nil bug (the Go->JSON->Python class, cf. weather bug #29):
// the TUI would show "0 mph" wind while the rest of the panel reads unavailable.
func TestWeatherProducer_FallbackNoCache_NilNumerics(t *testing.T) {
	p := &WeatherProducer{} // no cached lastResult, zero config

	env := p.fallbackResult("test: api down")
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok, "envelope must carry a data map")

	assert.Nil(t, data["temperature"], "temperature must be nil when unavailable")
	assert.Nil(t, data["weatherCode"], "weatherCode must be nil when unavailable")
	assert.Nil(t, data["windSpeed"],
		"windSpeed must be nil when unavailable, not 0 (a literal 0 reads as a real 0 mph reading)")

	// The unit label is descriptive, not a numeric reading — it stays populated.
	assert.Equal(t, "fahrenheit", data["temperatureUnit"])
	assert.Equal(t, "Unavailable", data["description"])
}

// TestWeatherProducer_UnconfiguredCoords_NoSilentSF pins audit NICE-TO-HAVE #15:
// when weather is enabled but no coordinates are configured (lat==0 && lon==0),
// the producer must emit an actionable "set your location" envelope rather than
// silently fetching San Francisco weather (which reads as a real reading the user
// never asked for). Numerics are nil so the TUI renders "--", and the description
// is distinct from the generic API-down "Unavailable" so the user knows to act.
// Produce must short-circuit BEFORE any HTTP call: the failing client below would
// error if reached, proving no network fetch happens for unset coordinates.
func TestWeatherProducer_UnconfiguredCoords_NoSilentSF(t *testing.T) {
	p := &WeatherProducer{
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatalf("Produce must not hit the network when coordinates are unset")
			return nil, nil
		})},
	}
	// feedsCfg left zero-valued: Latitude==0, Longitude==0.

	env, err := p.Produce(context.Background())
	require.NoError(t, err)
	data, ok := env.(map[string]interface{})["data"].(map[string]interface{})
	require.True(t, ok, "envelope must carry a data map")

	assert.Nil(t, data["temperature"], "temperature must be nil when location is unset")
	assert.Nil(t, data["windSpeed"], "windSpeed must be nil when location is unset")
	assert.Nil(t, data["weatherCode"], "weatherCode must be nil when location is unset")
	assert.Equal(t, "Set location", data["description"],
		"unset coordinates must surface an actionable message, not silent SF weather")
}

// roundTripFunc adapts a function to http.RoundTripper for hermetic client tests.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestGetDefaultConfig_WeatherCoordsUnset guards that the default config no longer
// bakes in San Francisco coordinates — a fresh user who enables weather without
// setting coords hits the honest "set location" path, not silent SF weather.
func TestGetDefaultConfig_WeatherCoordsUnset(t *testing.T) {
	cfg := core.GetDefaultConfig()
	assert.Zero(t, cfg.Feeds.Weather.Latitude, "default weather latitude must be unset (not SF)")
	assert.Zero(t, cfg.Feeds.Weather.Longitude, "default weather longitude must be unset (not SF)")
}

// TestWmoCodeToEmoji pins the WMO weather-code → emoji mapping across every
// standard Open-Meteo code. These functions are otherwise reached only via a live
// HTTP call, so they had zero coverage. Codes 85/86 (snow showers) are the bug
// this locks down: they previously fell through to the default sunny-ish icon.
//
// Rather than hardcode emoji byte sequences (variation-selector fragile), each
// code is asserted to share the emoji of a reference code in its expected bucket
// — both sides come from the same function, so the comparison is byte-exact.
func TestWmoCodeToEmoji(t *testing.T) {
	const (
		refSunny   = 0
		refPartly  = 1
		refFog     = 45
		refRain    = 51
		refSnow    = 71
		refShowers = 80
		refThunder = 95
		refDefault = 100
	)
	cases := []struct {
		code int
		ref  int
	}{
		{0, refSunny},
		{1, refPartly}, {2, refPartly}, {3, refPartly},
		{45, refFog}, {48, refFog},
		{51, refRain}, {53, refRain}, {55, refRain},
		{56, refRain}, {57, refRain},
		{61, refRain}, {63, refRain}, {65, refRain},
		{66, refRain}, {67, refRain},
		{71, refSnow}, {73, refSnow}, {75, refSnow}, {77, refSnow},
		{80, refShowers}, {81, refShowers}, {82, refShowers},
		{85, refSnow}, {86, refSnow}, // snow showers — regression guard
		{95, refThunder}, {96, refThunder}, {99, refThunder},
		{-1, refDefault}, {1000, refDefault}, // out of range → default
	}
	for _, tc := range cases {
		assert.Equalf(t, wmoCodeToEmoji(tc.ref), wmoCodeToEmoji(tc.code),
			"wmoCodeToEmoji(%d) should share the bucket of code %d", tc.code, tc.ref)
	}

	// The buckets must be genuinely distinct — otherwise the reference-equality
	// checks above could pass trivially if everything returned one emoji. Spot-
	// check that snow showers do NOT collapse into the default (the actual bug).
	assert.NotEqual(t, wmoCodeToEmoji(refDefault), wmoCodeToEmoji(85),
		"snow showers (85) must not render as the default sunny-ish icon")
	assert.NotEqual(t, wmoCodeToEmoji(refSunny), wmoCodeToEmoji(refSnow),
		"distinct buckets must yield distinct emoji")
}

// TestWmoCodeToDescription pins the WMO weather-code → label mapping across every
// standard Open-Meteo code. Codes 85/86 (snow showers) previously returned
// "Unknown"; this guards the fix.
func TestWmoCodeToDescription(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{0, "Clear"},
		{1, "Partly Cloudy"}, {2, "Partly Cloudy"}, {3, "Partly Cloudy"},
		{45, "Fog"}, {48, "Fog"},
		{51, "Drizzle"}, {53, "Drizzle"}, {55, "Drizzle"},
		{56, "Freezing Drizzle"}, {57, "Freezing Drizzle"},
		{61, "Rain"}, {63, "Rain"}, {65, "Rain"},
		{66, "Freezing Rain"}, {67, "Freezing Rain"},
		{71, "Snow"}, {73, "Snow"}, {75, "Snow"},
		{77, "Snow Grains"},
		{80, "Showers"}, {81, "Showers"}, {82, "Showers"},
		{85, "Snow Showers"}, {86, "Snow Showers"}, // regression guard
		{95, "Thunderstorm"}, {96, "Thunderstorm"}, {99, "Thunderstorm"},
		{-1, "Unknown"}, {100, "Unknown"}, // out of range → Unknown
	}
	for _, tc := range cases {
		assert.Equalf(t, tc.want, wmoCodeToDescription(tc.code),
			"wmoCodeToDescription(%d)", tc.code)
	}
}
