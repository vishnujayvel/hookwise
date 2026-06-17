package feeds

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
