package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// ---------------------------------------------------------------------------
// WeatherProducer envelope structure tests
// ---------------------------------------------------------------------------

func TestWeatherProducer_EnvelopeStructure_WithAPI(t *testing.T) {
	// Set up a mock Open-Meteo API server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"current": map[string]interface{}{
				"temperature_2m": 72.0,
				"wind_speed_10m": 5.3,
				"weather_code":   0,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := &WeatherProducer{
		client: srv.Client(),
	}
	p.SetFeedsConfig(core.FeedsConfig{
		Weather: core.WeatherFeedConfig{
			Latitude:  37.7749,
			Longitude: -122.4194,
		},
	})

	// Override the URL by pointing the client at our test server.
	// WeatherProducer builds the URL internally, so we need a transport-level redirect.
	p.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: Produce must not return error")

	data := assertValidEnvelope(t, result, "weather")

	// Verify expected data keys.
	assert.Equal(t, 72.0, data["temperature"])
	assert.Equal(t, "fahrenheit", data["temperatureUnit"])
	assert.Equal(t, 5.3, data["windSpeed"])
	assert.NotNil(t, data["emoji"])
	assert.NotNil(t, data["description"])
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWeatherProducer_EnvelopeNoSourceKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"current": map[string]interface{}{
				"temperature_2m": 20.0,
				"wind_speed_10m": 3.0,
				"weather_code":   1,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := &WeatherProducer{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = srv.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		},
	}

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := assertValidEnvelope(t, result, "weather")
	_, hasSource := data["source"]
	assert.False(t, hasSource, "data must NOT contain 'source' key (Bug #29)")
}

// ---------------------------------------------------------------------------
// WeatherProducer error/fallback path tests
// ---------------------------------------------------------------------------

func TestWeatherProducer_FallbackResult_NoCachedData(t *testing.T) {
	p := &WeatherProducer{}
	result := p.fallbackResult("test reason")

	data := assertValidEnvelope(t, result, "weather")

	// No cached data: temperature and weatherCode should be nil.
	assert.Nil(t, data["temperature"], "fallback temperature must be nil, not 0")
	assert.Nil(t, data["weatherCode"], "fallback weatherCode must be nil, not 0")
	assert.Equal(t, "fahrenheit", data["temperatureUnit"])
	assert.Equal(t, "Unavailable", data["description"])
}

func TestWeatherProducer_FallbackResult_CelsiusUnit(t *testing.T) {
	p := &WeatherProducer{}
	p.SetFeedsConfig(core.FeedsConfig{
		Weather: core.WeatherFeedConfig{
			TemperatureUnit: "celsius",
		},
	})
	result := p.fallbackResult("test")

	data := assertValidEnvelope(t, result, "weather")
	assert.Equal(t, "celsius", data["temperatureUnit"])
}

func TestWeatherProducer_APIError_FailOpen(t *testing.T) {
	// Server returns 500 Internal Server Error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := &WeatherProducer{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = srv.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		},
	}

	result, err := p.Produce(context.Background())

	// ARCH-1: must not return error.
	require.NoError(t, err, "ARCH-1: API error must fail-open")
	assertValidEnvelope(t, result, "weather")
}

func TestWeatherProducer_APIUnreachable_FailOpen(t *testing.T) {
	// Client pointed at a server that immediately closes connections.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close connection without writing anything.
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer srv.Close()

	p := &WeatherProducer{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = srv.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		},
	}

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: unreachable API must fail-open")
	assertValidEnvelope(t, result, "weather")
}

func TestWeatherProducer_InvalidJSON_FailOpen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "not valid json at all")
	}))
	defer srv.Close()

	p := &WeatherProducer{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = srv.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		},
	}

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: invalid JSON must fail-open")
	assertValidEnvelope(t, result, "weather")
}

func TestWeatherProducer_FallbackUsesCachedResult(t *testing.T) {
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call succeeds.
			resp := map[string]interface{}{
				"current": map[string]interface{}{
					"temperature_2m": 68.0,
					"wind_speed_10m": 4.0,
					"weather_code":   0,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		} else {
			// Second call fails.
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	p := &WeatherProducer{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = srv.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		},
	}

	// First call: should succeed and cache the result.
	result1, err := p.Produce(context.Background())
	require.NoError(t, err)
	data1 := assertValidEnvelope(t, result1, "weather")
	assert.Equal(t, 68.0, data1["temperature"])

	// Second call: should fail-open and return cached result.
	result2, err := p.Produce(context.Background())
	require.NoError(t, err)
	data2 := assertValidEnvelope(t, result2, "weather")
	assert.Equal(t, 68.0, data2["temperature"], "fallback should return cached temperature")
}

func TestWeatherProducer_TestFixture_FieldConsistency(t *testing.T) {
	fixture := WeatherTestFixture()
	fixtureData := assertValidEnvelope(t, fixture, "weather")

	// Compare keys against a real fallback (which has all the same fields).
	p := &WeatherProducer{}
	fallback := p.fallbackResult("test")
	fallbackData := fallback["data"].(map[string]interface{})

	for key := range fallbackData {
		_, ok := fixtureData[key]
		assert.True(t, ok, "fixture data missing key %q", key)
	}
	for key := range fixtureData {
		_, ok := fallbackData[key]
		assert.True(t, ok, "fixture data has extra key %q", key)
	}
}
