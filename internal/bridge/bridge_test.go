package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/feeds"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeFeedFile writes a JSON file to dir/<name>.json using MarshalIndent
// (matching the format core.AtomicWriteJSON produces).
func writeFeedFile(t *testing.T, dir, name string, data interface{}) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	content, err := json.MarshalIndent(data, "", "  ")
	require.NoError(t, err)
	content = append(content, '\n')
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".json"), content, 0o600))
}

// makeFeedEntry builds a feed entry in the Go-envelope format produced by
// built-in and custom producers.
func makeFeedEntry(feedType string, feedData map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type":      feedType,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      feedData,
	}
}

// makeTUIEntry builds a flat TUI-format entry.
func makeTUIEntry(feedData map[string]interface{}) map[string]interface{} {
	entry := map[string]interface{}{
		"updated_at":  time.Now().UTC().Format(time.RFC3339),
		"ttl_seconds": float64(DefaultTTLSeconds),
	}
	for k, v := range feedData {
		entry[k] = v
	}
	return entry
}

// ---------------------------------------------------------------------------
// Test 1: CollectFeedCache merges multiple per-feed JSON files
// ---------------------------------------------------------------------------

func TestCollectFeedCache_MultipleFeeds(t *testing.T) {
	dir := t.TempDir()

	pulse := makeFeedEntry("pulse", map[string]interface{}{
		"session_count":   float64(3),
		"active_sessions": float64(1),
	})
	weather := makeFeedEntry("weather", map[string]interface{}{
		"temperature": float64(72),
		"unit":        "F",
		"condition":   "sunny",
	})
	calendar := makeFeedEntry("calendar", map[string]interface{}{
		"events":     []interface{}{},
		"next_event": nil,
	})

	writeFeedFile(t, dir, "pulse", pulse)
	writeFeedFile(t, dir, "weather", weather)
	writeFeedFile(t, dir, "calendar", calendar)

	merged, err := CollectFeedCache(dir)
	require.NoError(t, err)
	assert.Len(t, merged, 3)

	// Verify each feed is keyed by its filename stem.
	assert.Contains(t, merged, "pulse")
	assert.Contains(t, merged, "weather")
	assert.Contains(t, merged, "calendar")

	// Raw collected data is in Go-envelope format.
	weatherEntry, ok := merged["weather"].(map[string]interface{})
	require.True(t, ok, "weather entry should be a map")
	weatherData, ok := weatherEntry["data"].(map[string]interface{})
	require.True(t, ok, "weather data should be a map")
	assert.Equal(t, float64(72), weatherData["temperature"])
	assert.Equal(t, "F", weatherData["unit"])
}

// ---------------------------------------------------------------------------
// Test 2: WriteTUICacheTo writes flattened cache to status-line-cache.json
// ---------------------------------------------------------------------------

func TestWriteTUICacheTo_WritesFlattenedFile(t *testing.T) {
	cacheDir := t.TempDir()
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "status-line-cache.json")

	writeFeedFile(t, cacheDir, "pulse", makeFeedEntry("pulse", map[string]interface{}{
		"session_count": float64(5),
	}))
	writeFeedFile(t, cacheDir, "news", makeFeedEntry("news", map[string]interface{}{
		"stories": []interface{}{},
		"source":  "placeholder",
	}))

	require.NoError(t, WriteTUICacheTo(cacheDir, outPath))

	raw, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &parsed))
	assert.Len(t, parsed, 2)
	assert.Contains(t, parsed, "pulse")
	assert.Contains(t, parsed, "news")

	// Output should be in flattened TUI format (not Go envelope).
	pulseEntry, ok := parsed["pulse"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, pulseEntry["updated_at"])
	assert.Equal(t, float64(DefaultTTLSeconds), pulseEntry["ttl_seconds"])
	assert.Equal(t, float64(5), pulseEntry["session_count"])
	// Envelope fields should NOT be present in flat format.
	assert.NotContains(t, pulseEntry, "type")
	assert.NotContains(t, pulseEntry, "data")
}

// ---------------------------------------------------------------------------
// Test 3: ValidateGoEnvelopeFormat — valid entries pass
// ---------------------------------------------------------------------------

func TestValidateGoEnvelopeFormat_ValidEntries(t *testing.T) {
	data := map[string]interface{}{
		"pulse": makeFeedEntry("pulse", map[string]interface{}{
			"session_count": float64(3),
		}),
		"weather": makeFeedEntry("weather", map[string]interface{}{
			"temperature": float64(72),
		}),
	}

	err := ValidateGoEnvelopeFormat(data)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Test 4: ValidateGoEnvelopeFormat — missing "type" field
// ---------------------------------------------------------------------------

func TestValidateGoEnvelopeFormat_MissingType(t *testing.T) {
	data := map[string]interface{}{
		"bad": map[string]interface{}{
			"timestamp": "2026-03-06T10:00:00Z",
			"data":      map[string]interface{}{},
		},
	}

	err := ValidateGoEnvelopeFormat(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required field \"type\"")
}

// ---------------------------------------------------------------------------
// Test 5: ValidateGoEnvelopeFormat — missing "timestamp" field
// ---------------------------------------------------------------------------

func TestValidateGoEnvelopeFormat_MissingTimestamp(t *testing.T) {
	data := map[string]interface{}{
		"bad": map[string]interface{}{
			"type": "bad",
			"data": map[string]interface{}{},
		},
	}

	err := ValidateGoEnvelopeFormat(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required field \"timestamp\"")
}

// ---------------------------------------------------------------------------
// Test 6: ValidateGoEnvelopeFormat — missing "data" field
// ---------------------------------------------------------------------------

func TestValidateGoEnvelopeFormat_MissingData(t *testing.T) {
	data := map[string]interface{}{
		"bad": map[string]interface{}{
			"type":      "bad",
			"timestamp": "2026-03-06T10:00:00Z",
		},
	}

	err := ValidateGoEnvelopeFormat(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required field \"data\"")
}

// ---------------------------------------------------------------------------
// Test 7: ValidateGoEnvelopeFormat — entry is not a JSON object
// ---------------------------------------------------------------------------

func TestValidateGoEnvelopeFormat_EntryNotObject(t *testing.T) {
	data := map[string]interface{}{
		"bad": "just a string",
	}

	err := ValidateGoEnvelopeFormat(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a JSON object")
}

// ---------------------------------------------------------------------------
// Test 8: ValidateGoEnvelopeFormat — "type" field is not a string
// ---------------------------------------------------------------------------

func TestValidateGoEnvelopeFormat_TypeNotString(t *testing.T) {
	data := map[string]interface{}{
		"bad": map[string]interface{}{
			"type":      42,
			"timestamp": "2026-03-06T10:00:00Z",
			"data":      map[string]interface{}{},
		},
	}

	err := ValidateGoEnvelopeFormat(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "\"type\" is not a string")
}

// ---------------------------------------------------------------------------
// Test 9: ValidateGoEnvelopeFormat — empty map is valid
// ---------------------------------------------------------------------------

func TestValidateGoEnvelopeFormat_EmptyMap(t *testing.T) {
	err := ValidateGoEnvelopeFormat(map[string]interface{}{})
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Test 10: CollectFeedCache — empty directory returns empty map
// ---------------------------------------------------------------------------

func TestCollectFeedCache_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	merged, err := CollectFeedCache(dir)
	require.NoError(t, err)
	assert.NotNil(t, merged)
	assert.Empty(t, merged)
}

// ---------------------------------------------------------------------------
// Test 11: CollectFeedCache — nonexistent directory returns empty map
// ---------------------------------------------------------------------------

func TestCollectFeedCache_NonexistentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")

	merged, err := CollectFeedCache(dir)
	require.NoError(t, err)
	assert.NotNil(t, merged)
	assert.Empty(t, merged)
}

// ---------------------------------------------------------------------------
// Test 12: CollectFeedCache — skips invalid JSON files
// ---------------------------------------------------------------------------

func TestCollectFeedCache_SkipsInvalidJSON(t *testing.T) {
	dir := t.TempDir()

	writeFeedFile(t, dir, "pulse", makeFeedEntry("pulse", map[string]interface{}{
		"session_count": float64(1),
	}))

	// Write an invalid JSON file.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "corrupt.json"),
		[]byte("not valid json {{{"),
		0o600,
	))

	merged, err := CollectFeedCache(dir)
	require.NoError(t, err)
	assert.Len(t, merged, 1)
	assert.Contains(t, merged, "pulse")
	assert.NotContains(t, merged, "corrupt")
}

// ---------------------------------------------------------------------------
// Test 13: CollectFeedCache — skips non-JSON files
// ---------------------------------------------------------------------------

func TestCollectFeedCache_SkipsNonJSONFiles(t *testing.T) {
	dir := t.TempDir()

	writeFeedFile(t, dir, "weather", makeFeedEntry("weather", map[string]interface{}{
		"temperature": float64(65),
	}))

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "readme.txt"),
		[]byte("not a feed file"),
		0o600,
	))

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".tmp-abc123"),
		[]byte("{}"),
		0o600,
	))

	merged, err := CollectFeedCache(dir)
	require.NoError(t, err)
	assert.Len(t, merged, 1)
	assert.Contains(t, merged, "weather")
}

// ---------------------------------------------------------------------------
// Test 14: CollectFeedCache — skips subdirectories
// ---------------------------------------------------------------------------

func TestCollectFeedCache_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()

	writeFeedFile(t, dir, "pulse", makeFeedEntry("pulse", map[string]interface{}{}))

	// Create a subdirectory that happens to end in .json (edge case).
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subdir.json"), 0o700))

	merged, err := CollectFeedCache(dir)
	require.NoError(t, err)
	assert.Len(t, merged, 1)
	assert.Contains(t, merged, "pulse")
}

// ---------------------------------------------------------------------------
// Test 15: WriteTUICacheTo — empty cache dir writes valid empty JSON
// ---------------------------------------------------------------------------

func TestWriteTUICacheTo_EmptyCache(t *testing.T) {
	cacheDir := t.TempDir()
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "status-line-cache.json")

	require.NoError(t, WriteTUICacheTo(cacheDir, outPath))

	raw, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &parsed))
	assert.Empty(t, parsed)
}

// ---------------------------------------------------------------------------
// Test 16: WriteTUICacheTo — creates parent directories
// ---------------------------------------------------------------------------

func TestWriteTUICacheTo_CreatesParentDirs(t *testing.T) {
	cacheDir := t.TempDir()
	outPath := filepath.Join(t.TempDir(), "nested", "deep", "status-line-cache.json")

	writeFeedFile(t, cacheDir, "pulse", makeFeedEntry("pulse", map[string]interface{}{
		"session_count": float64(1),
	}))

	require.NoError(t, WriteTUICacheTo(cacheDir, outPath))

	_, err := os.Stat(outPath)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Test 17: FlattenForTUI — Go envelope to TUI flat format
// ---------------------------------------------------------------------------

func TestFlattenForTUI_EnvelopeToFlat(t *testing.T) {
	ts := "2026-03-06T10:00:00Z"
	merged := map[string]interface{}{
		"pulse": map[string]interface{}{
			"type":      "pulse",
			"timestamp": ts,
			"data": map[string]interface{}{
				"session_count":   float64(3),
				"active_sessions": float64(1),
			},
		},
		"weather": map[string]interface{}{
			"type":      "weather",
			"timestamp": ts,
			"data": map[string]interface{}{
				"temperature": float64(72),
				"unit":        "F",
			},
		},
	}

	flat := FlattenForTUI(merged)
	assert.Len(t, flat, 2)

	// Pulse should be flattened.
	pulseEntry, ok := flat["pulse"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, ts, pulseEntry["updated_at"])
	assert.Equal(t, DefaultTTLSeconds, pulseEntry["ttl_seconds"])
	assert.Equal(t, float64(3), pulseEntry["session_count"])
	assert.Equal(t, float64(1), pulseEntry["active_sessions"])
	assert.NotContains(t, pulseEntry, "type")
	assert.NotContains(t, pulseEntry, "data")
	assert.NotContains(t, pulseEntry, "timestamp")

	// Weather should be flattened.
	weatherEntry, ok := flat["weather"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, ts, weatherEntry["updated_at"])
	assert.Equal(t, float64(72), weatherEntry["temperature"])
	assert.Equal(t, "F", weatherEntry["unit"])
}

// ---------------------------------------------------------------------------
// Test 18: FlattenForTUI — already-flat entries pass through
// ---------------------------------------------------------------------------

func TestFlattenForTUI_AlreadyFlatPassThrough(t *testing.T) {
	merged := map[string]interface{}{
		"custom": map[string]interface{}{
			"updated_at":  "2026-03-06T10:00:00Z",
			"ttl_seconds": float64(600),
			"value":       "something",
		},
	}

	flat := FlattenForTUI(merged)
	entry, ok := flat["custom"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "2026-03-06T10:00:00Z", entry["updated_at"])
	assert.Equal(t, float64(600), entry["ttl_seconds"])
	assert.Equal(t, "something", entry["value"])
}

// ---------------------------------------------------------------------------
// Test 19: FlattenForTUI — non-object values pass through
// ---------------------------------------------------------------------------

func TestFlattenForTUI_NonObjectPassThrough(t *testing.T) {
	merged := map[string]interface{}{
		"scalar": "just a string",
	}

	flat := FlattenForTUI(merged)
	assert.Equal(t, "just a string", flat["scalar"])
}

// ---------------------------------------------------------------------------
// Test 20: FlattenForTUI — empty map produces empty map
// ---------------------------------------------------------------------------

func TestFlattenForTUI_EmptyMap(t *testing.T) {
	flat := FlattenForTUI(map[string]interface{}{})
	assert.NotNil(t, flat)
	assert.Empty(t, flat)
}

// ---------------------------------------------------------------------------
// Test 21: Full roundtrip — collect, validate envelope, flatten, validate TUI, write, re-read
// ---------------------------------------------------------------------------

func TestFullRoundtrip_CollectFlattenValidateWrite(t *testing.T) {
	cacheDir := t.TempDir()
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "status-line-cache.json")

	// Write all 8 built-in feed types.
	feeds := []struct {
		name string
		data map[string]interface{}
	}{
		{"pulse", map[string]interface{}{"session_count": float64(2)}},
		{"project", map[string]interface{}{"name": "hookwise", "branch": "main"}},
		{"news", map[string]interface{}{"stories": []interface{}{}, "source": "hn"}},
		{"calendar", map[string]interface{}{"events": []interface{}{}}},
		{"weather", map[string]interface{}{"temperature": float64(68)}},
		{"practice", map[string]interface{}{"total_sessions": float64(10)}},
		{"memories", map[string]interface{}{"recent_memories": []interface{}{}}},
		{"insights", map[string]interface{}{"productivity_score": float64(85)}},
	}

	for _, f := range feeds {
		writeFeedFile(t, cacheDir, f.name, makeFeedEntry(f.name, f.data))
	}

	// Collect — raw data is Go-envelope format.
	merged, err := CollectFeedCache(cacheDir)
	require.NoError(t, err)
	assert.Len(t, merged, 8)

	// Validate Go-envelope format.
	require.NoError(t, ValidateGoEnvelopeFormat(merged))

	// Flatten for TUI.
	flattened := FlattenForTUI(merged)
	assert.Len(t, flattened, 8)

	// Validate TUI flat format.
	require.NoError(t, ValidateCacheFormat(flattened))

	// Write via WriteTUICacheTo (which does collect + flatten internally).
	require.NoError(t, WriteTUICacheTo(cacheDir, outPath))

	// Re-read and verify flat format.
	raw, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var reread map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &reread))
	assert.Len(t, reread, 8)

	for _, f := range feeds {
		assert.Contains(t, reread, f.name)
		entry, ok := reread[f.name].(map[string]interface{})
		require.True(t, ok, "entry %q should be a map", f.name)
		assert.NotEmpty(t, entry["updated_at"])
		assert.Equal(t, float64(DefaultTTLSeconds), entry["ttl_seconds"])
		// Data fields should be at top level.
		for k, v := range f.data {
			assert.Equal(t, v, entry[k], "field %q mismatch in %s", k, f.name)
		}
		// Envelope fields should NOT be present.
		assert.NotContains(t, entry, "type")
		assert.NotContains(t, entry, "timestamp")
		assert.NotContains(t, entry, "data")
	}
}

// ---------------------------------------------------------------------------
// Test 22: ValidateGoEnvelopeFormat — "timestamp" field is not a string
// ---------------------------------------------------------------------------

func TestValidateGoEnvelopeFormat_TimestampNotString(t *testing.T) {
	data := map[string]interface{}{
		"bad": map[string]interface{}{
			"type":      "bad",
			"timestamp": 12345,
			"data":      map[string]interface{}{},
		},
	}

	err := ValidateGoEnvelopeFormat(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "\"timestamp\" is not a string")
}

// ---------------------------------------------------------------------------
// Test 23: CollectFeedCache — atomic write compatible
// ---------------------------------------------------------------------------

func TestCollectFeedCache_AtomicWriteCompatible(t *testing.T) {
	dir := t.TempDir()

	feedData := makeFeedEntry("pulse", map[string]interface{}{
		"session_count": float64(7),
	})

	require.NoError(t, os.MkdirAll(dir, 0o700))
	content, err := json.MarshalIndent(feedData, "", "  ")
	require.NoError(t, err)
	content = append(content, '\n')
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pulse.json"), content, 0o600))

	merged, err := CollectFeedCache(dir)
	require.NoError(t, err)
	assert.Len(t, merged, 1)

	pulseEntry, ok := merged["pulse"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "pulse", pulseEntry["type"])

	pulseData, ok := pulseEntry["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(7), pulseData["session_count"])
}

// ---------------------------------------------------------------------------
// Test 24: ValidateCacheFormat (TUI format) — valid entries pass
// ---------------------------------------------------------------------------

func TestValidateCacheFormat_TUIFormat_ValidEntries(t *testing.T) {
	data := map[string]interface{}{
		"pulse": makeTUIEntry(map[string]interface{}{
			"session_count": float64(3),
		}),
		"weather": makeTUIEntry(map[string]interface{}{
			"temperature": float64(72),
		}),
	}

	err := ValidateCacheFormat(data)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Test 25: ValidateCacheFormat (TUI format) — missing updated_at
// ---------------------------------------------------------------------------

func TestValidateCacheFormat_TUIFormat_MissingUpdatedAt(t *testing.T) {
	data := map[string]interface{}{
		"bad": map[string]interface{}{
			"ttl_seconds": float64(300),
			"value":       "something",
		},
	}

	err := ValidateCacheFormat(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required field \"updated_at\"")
}

// ---------------------------------------------------------------------------
// Test 26: ValidateCacheFormat (TUI format) — missing ttl_seconds
// ---------------------------------------------------------------------------

func TestValidateCacheFormat_TUIFormat_MissingTTL(t *testing.T) {
	data := map[string]interface{}{
		"bad": map[string]interface{}{
			"updated_at": "2026-03-06T10:00:00Z",
			"value":      "something",
		},
	}

	err := ValidateCacheFormat(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required field \"ttl_seconds\"")
}

// ---------------------------------------------------------------------------
// Test 27: ValidateCacheFormat (TUI format) — updated_at not string
// ---------------------------------------------------------------------------

func TestValidateCacheFormat_TUIFormat_UpdatedAtNotString(t *testing.T) {
	data := map[string]interface{}{
		"bad": map[string]interface{}{
			"updated_at":  12345,
			"ttl_seconds": float64(300),
		},
	}

	err := ValidateCacheFormat(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "\"updated_at\" is not a string")
}

// ---------------------------------------------------------------------------
// Test 28: ValidateCacheFormat (TUI format) — entry not object
// ---------------------------------------------------------------------------

func TestValidateCacheFormat_TUIFormat_EntryNotObject(t *testing.T) {
	data := map[string]interface{}{
		"bad": "just a string",
	}

	err := ValidateCacheFormat(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a JSON object")
}

// ---------------------------------------------------------------------------
// Test 29: ValidateCacheFormat (TUI format) — empty map valid
// ---------------------------------------------------------------------------

func TestValidateCacheFormat_TUIFormat_EmptyMap(t *testing.T) {
	err := ValidateCacheFormat(map[string]interface{}{})
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Test 30: Weather producer output -> FlattenForTUI -> Python TUI field names (BP5)
// ---------------------------------------------------------------------------

func TestFlattenForTUI_WeatherFieldNamesForPythonTUI(t *testing.T) {
	// Use the shared fixture from the feeds package so that field name
	// renames in WeatherProducer surface as test failures here too.
	fixture := feeds.WeatherTestFixture()
	merged := map[string]interface{}{
		"weather": fixture,
	}

	flat := FlattenForTUI(merged)

	weatherEntry, ok := flat["weather"].(map[string]interface{})
	require.True(t, ok, "weather entry should be a map after flattening")

	// Extract expected values from the fixture's data envelope.
	fixtureData := fixture["data"].(map[string]interface{})

	// These are the EXACT field names the Python TUI expects in
	// tui/hookwise_tui/tabs/status.py:_render_segment("weather", ...).
	assert.Equal(t, fixtureData["temperature"], weatherEntry["temperature"],
		"Python TUI reads entry.get('temperature')")
	assert.Equal(t, fixtureData["temperatureUnit"], weatherEntry["temperatureUnit"],
		"Python TUI reads entry.get('temperatureUnit') to decide F vs C")
	assert.Equal(t, fixtureData["windSpeed"], weatherEntry["windSpeed"],
		"Python TUI reads entry.get('windSpeed') for wind indicator")
	assert.Equal(t, fixtureData["emoji"], weatherEntry["emoji"],
		"Python TUI reads entry.get('emoji') for weather icon")

	// Verify envelope fields are stripped.
	assert.NotContains(t, weatherEntry, "type")
	assert.NotContains(t, weatherEntry, "timestamp")
	assert.NotContains(t, weatherEntry, "data")

	// Verify TUI metadata is added.
	assert.Equal(t, fixture["timestamp"], weatherEntry["updated_at"])
	assert.Equal(t, DefaultTTLSeconds, weatherEntry["ttl_seconds"])

	// Also verify additional fields are present.
	assert.Equal(t, fixtureData["weatherCode"], weatherEntry["weatherCode"])
	assert.Equal(t, fixtureData["description"], weatherEntry["description"])
}

// ---------------------------------------------------------------------------
// Test 31: FlattenForTUI preserves ttl_seconds from data if present
// ---------------------------------------------------------------------------

func TestFlattenForTUI_PreservesTTLFromData(t *testing.T) {
	merged := map[string]interface{}{
		"custom": map[string]interface{}{
			"type":      "custom",
			"timestamp": "2026-03-06T10:00:00Z",
			"data": map[string]interface{}{
				"value":       "test",
				"ttl_seconds": float64(600),
			},
		},
	}

	flat := FlattenForTUI(merged)
	entry, ok := flat["custom"].(map[string]interface{})
	require.True(t, ok)
	// Should use the ttl_seconds from data, not the default.
	assert.Equal(t, float64(600), entry["ttl_seconds"])
}
