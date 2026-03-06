package analytics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Batch 7 — Coaching + Cost State Persistence tests
// ---------------------------------------------------------------------------

// --- Coaching State Tests ---

// Test 1: ReadCoachingState returns defaults when no row exists
func TestReadCoachingState_DefaultsWhenEmpty(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	state, err := db.ReadCoachingState(ctx)
	require.NoError(t, err)
	require.NotNil(t, state)

	assert.Equal(t, "coding", state.CurrentMode)
	assert.Equal(t, "none", state.AlertLevel)
	assert.Equal(t, 0, state.PracticeCount)
	assert.Equal(t, float64(0), state.ToolingMinutes)
	assert.Nil(t, state.LastPromptAt)
	assert.Nil(t, state.ModeStartedAt)
	assert.Nil(t, state.PromptHistory)
	assert.Nil(t, state.LastLargeChange)
	// TodayDate should be today's date.
	assert.Equal(t, time.Now().Format("2006-01-02"), state.TodayDate)
}

// Test 2: WriteCoachingState + ReadCoachingState roundtrip
func TestCoachingState_WriteReadRoundtrip(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	original := &CoachingState{
		LastPromptAt:   &now,
		PromptHistory:  []string{"prompt-a", "prompt-b"},
		CurrentMode:    "reviewing",
		ModeStartedAt:  &now,
		ToolingMinutes: 42.5,
		AlertLevel:     "warning",
		TodayDate:      "2026-03-06",
		PracticeCount:  7,
		LastLargeChange: map[string]interface{}{
			"file":  "main.go",
			"lines": float64(150),
		},
	}

	require.NoError(t, db.WriteCoachingState(ctx, original))

	loaded, err := db.ReadCoachingState(ctx)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, original.CurrentMode, loaded.CurrentMode)
	assert.Equal(t, original.AlertLevel, loaded.AlertLevel)
	assert.Equal(t, original.TodayDate, loaded.TodayDate)
	assert.Equal(t, original.PracticeCount, loaded.PracticeCount)
	assert.InDelta(t, original.ToolingMinutes, loaded.ToolingMinutes, 0.01)
	assert.Equal(t, original.PromptHistory, loaded.PromptHistory)

	// Time fields: compare truncated to second.
	require.NotNil(t, loaded.LastPromptAt)
	assert.True(t, now.Equal(loaded.LastPromptAt.Truncate(time.Second)))
	require.NotNil(t, loaded.ModeStartedAt)
	assert.True(t, now.Equal(loaded.ModeStartedAt.Truncate(time.Second)))

	// LastLargeChange
	require.NotNil(t, loaded.LastLargeChange)
	assert.Equal(t, "main.go", loaded.LastLargeChange["file"])
	assert.Equal(t, float64(150), loaded.LastLargeChange["lines"])
}

// Test 3: Coaching state with all fields populated including JSON fields
func TestCoachingState_AllFieldsPopulated(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	promptAt := time.Date(2026, 3, 6, 10, 30, 0, 0, time.UTC)
	modeAt := time.Date(2026, 3, 6, 8, 0, 0, 0, time.UTC)

	state := &CoachingState{
		LastPromptAt:   &promptAt,
		PromptHistory:  []string{"p1", "p2", "p3", "p4", "p5"},
		CurrentMode:    "deep-focus",
		ModeStartedAt:  &modeAt,
		ToolingMinutes: 120.75,
		AlertLevel:     "critical",
		TodayDate:      "2026-03-06",
		PracticeCount:  15,
		LastLargeChange: map[string]interface{}{
			"file":       "state.go",
			"lines":      float64(300),
			"confidence": float64(0.95),
			"nested": map[string]interface{}{
				"key": "value",
			},
		},
	}

	require.NoError(t, db.WriteCoachingState(ctx, state))

	loaded, err := db.ReadCoachingState(ctx)
	require.NoError(t, err)

	assert.Len(t, loaded.PromptHistory, 5)
	assert.Equal(t, "deep-focus", loaded.CurrentMode)
	assert.Equal(t, "critical", loaded.AlertLevel)
	assert.Equal(t, 15, loaded.PracticeCount)
	assert.InDelta(t, 120.75, loaded.ToolingMinutes, 0.01)

	// Verify nested JSON survived the roundtrip.
	require.NotNil(t, loaded.LastLargeChange)
	nested, ok := loaded.LastLargeChange["nested"].(map[string]interface{})
	require.True(t, ok, "nested should be a map")
	assert.Equal(t, "value", nested["key"])
}

// Test 4: WriteCoachingState with nil optional fields
func TestCoachingState_NilOptionalFields(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	state := &CoachingState{
		CurrentMode:   "coding",
		AlertLevel:    "none",
		TodayDate:     "2026-03-06",
		PracticeCount: 0,
		// LastPromptAt, ModeStartedAt, PromptHistory, LastLargeChange all nil
	}

	require.NoError(t, db.WriteCoachingState(ctx, state))

	loaded, err := db.ReadCoachingState(ctx)
	require.NoError(t, err)

	assert.Nil(t, loaded.LastPromptAt)
	assert.Nil(t, loaded.ModeStartedAt)
	// PromptHistory should be nil or empty (JSON null decodes to nil slice)
	assert.Empty(t, loaded.PromptHistory)
	// LastLargeChange should be nil or empty
	assert.Empty(t, loaded.LastLargeChange)
}

// Test 5: Overwriting coaching state replaces old values
func TestCoachingState_OverwriteReplaces(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	state1 := &CoachingState{
		CurrentMode:    "coding",
		AlertLevel:     "none",
		TodayDate:      "2026-03-05",
		PracticeCount:  3,
		ToolingMinutes: 10,
	}
	require.NoError(t, db.WriteCoachingState(ctx, state1))

	state2 := &CoachingState{
		CurrentMode:    "reviewing",
		AlertLevel:     "warning",
		TodayDate:      "2026-03-06",
		PracticeCount:  5,
		ToolingMinutes: 45.5,
	}
	require.NoError(t, db.WriteCoachingState(ctx, state2))

	loaded, err := db.ReadCoachingState(ctx)
	require.NoError(t, err)

	assert.Equal(t, "reviewing", loaded.CurrentMode)
	assert.Equal(t, "warning", loaded.AlertLevel)
	assert.Equal(t, "2026-03-06", loaded.TodayDate)
	assert.Equal(t, 5, loaded.PracticeCount)
	assert.InDelta(t, 45.5, loaded.ToolingMinutes, 0.01)
}

// --- Cost State Tests ---

// Test 6: ReadCostState returns defaults when no row exists
func TestReadCostState_DefaultsWhenEmpty(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	state, err := db.ReadCostState(ctx)
	require.NoError(t, err)
	require.NotNil(t, state)

	assert.Equal(t, time.Now().Format("2006-01-02"), state.Today)
	assert.Equal(t, float64(0), state.TotalToday)
	assert.NotNil(t, state.DailyCosts)
	assert.NotNil(t, state.SessionCosts)
	assert.Empty(t, state.DailyCosts)
	assert.Empty(t, state.SessionCosts)
}

// Test 7: WriteCostState + ReadCostState roundtrip
func TestCostState_WriteReadRoundtrip(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	today := time.Now().Format("2006-01-02")

	original := &CostState{
		DailyCosts: map[string]float64{
			"2026-03-05": 1.25,
			"2026-03-06": 3.50,
		},
		SessionCosts: map[string]float64{
			"sess-001": 0.75,
			"sess-002": 2.10,
		},
		Today:      today,
		TotalToday: 3.50,
	}

	require.NoError(t, db.WriteCostState(ctx, original))

	loaded, err := db.ReadCostState(ctx)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, today, loaded.Today)
	assert.InDelta(t, 3.50, loaded.TotalToday, 0.01)
	assert.InDelta(t, 1.25, loaded.DailyCosts["2026-03-05"], 0.01)
	assert.InDelta(t, 3.50, loaded.DailyCosts["2026-03-06"], 0.01)
	assert.InDelta(t, 0.75, loaded.SessionCosts["sess-001"], 0.01)
	assert.InDelta(t, 2.10, loaded.SessionCosts["sess-002"], 0.01)
}

// Test 8: Cost state date boundary reset (today changes -> TotalToday resets)
func TestCostState_DateBoundaryReset(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	// Write with yesterday's date and a non-zero TotalToday.
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	state := &CostState{
		DailyCosts: map[string]float64{
			yesterday: 5.00,
		},
		SessionCosts: map[string]float64{
			"old-sess": 5.00,
		},
		Today:      yesterday,
		TotalToday: 5.00,
	}
	require.NoError(t, db.WriteCostState(ctx, state))

	// Reading should detect the date boundary and reset TotalToday.
	loaded, err := db.ReadCostState(ctx)
	require.NoError(t, err)

	assert.Equal(t, time.Now().Format("2006-01-02"), loaded.Today)
	assert.Equal(t, float64(0), loaded.TotalToday, "TotalToday should be reset on date boundary")
	// DailyCosts and SessionCosts are preserved.
	assert.InDelta(t, 5.00, loaded.DailyCosts[yesterday], 0.01)
	assert.InDelta(t, 5.00, loaded.SessionCosts["old-sess"], 0.01)
}

// Test 9: Cost state with empty maps
func TestCostState_EmptyMaps(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	today := time.Now().Format("2006-01-02")

	state := &CostState{
		DailyCosts:   make(map[string]float64),
		SessionCosts: make(map[string]float64),
		Today:        today,
		TotalToday:   0,
	}
	require.NoError(t, db.WriteCostState(ctx, state))

	loaded, err := db.ReadCostState(ctx)
	require.NoError(t, err)

	assert.NotNil(t, loaded.DailyCosts)
	assert.NotNil(t, loaded.SessionCosts)
	assert.Empty(t, loaded.DailyCosts)
	assert.Empty(t, loaded.SessionCosts)
}

// --- Feed Cache Tests ---

// Test 10: ReadFeedCache returns nil for missing key
func TestReadFeedCache_MissingKey(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	entry, err := db.ReadFeedCache(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, entry)
}

// Test 11: WriteFeedCache + ReadFeedCache roundtrip
func TestFeedCache_WriteReadRoundtrip(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	entry := FeedCacheEntry{
		Key: "weather",
		Data: map[string]interface{}{
			"temperature": float64(72),
			"unit":        "F",
			"forecast":    []interface{}{"sunny", "cloudy"},
		},
		UpdatedAt:  now,
		TTLSeconds: 3600, // 1 hour
	}

	require.NoError(t, db.WriteFeedCache(ctx, entry))

	loaded, err := db.ReadFeedCache(ctx, "weather")
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, "weather", loaded.Key)
	assert.Equal(t, 3600, loaded.TTLSeconds)

	// Verify data content.
	dataMap, ok := loaded.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(72), dataMap["temperature"])
	assert.Equal(t, "F", dataMap["unit"])
}

// Test 12: Feed cache TTL freshness (expired entry returns nil)
func TestFeedCache_TTLExpired(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	// Write an entry with updated_at far in the past and a short TTL.
	pastTime := time.Now().Add(-2 * time.Hour)
	entry := FeedCacheEntry{
		Key:        "stale-data",
		Data:       map[string]interface{}{"value": "old"},
		UpdatedAt:  pastTime,
		TTLSeconds: 60, // 60 seconds TTL, but updated 2 hours ago
	}
	require.NoError(t, db.WriteFeedCache(ctx, entry))

	// Reading should return nil because the entry has expired.
	loaded, err := db.ReadFeedCache(ctx, "stale-data")
	require.NoError(t, err)
	assert.Nil(t, loaded, "expired cache entry should return nil")
}

// Test 13: Feed cache entry with TTL=0 does not expire
func TestFeedCache_ZeroTTLNeverExpires(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()

	pastTime := time.Now().Add(-48 * time.Hour)
	entry := FeedCacheEntry{
		Key:        "permanent",
		Data:       map[string]interface{}{"keep": true},
		UpdatedAt:  pastTime,
		TTLSeconds: 0,
	}
	require.NoError(t, db.WriteFeedCache(ctx, entry))

	loaded, err := db.ReadFeedCache(ctx, "permanent")
	require.NoError(t, err)
	require.NotNil(t, loaded, "zero-TTL entry should never expire")
	assert.Equal(t, "permanent", loaded.Key)
}

// Test 14: ReadAllFeedCache with multiple entries
func TestReadAllFeedCache_MultipleEntries(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	entries := []FeedCacheEntry{
		{Key: "weather", Data: map[string]interface{}{"temp": float64(72)}, UpdatedAt: now, TTLSeconds: 300},
		{Key: "costs", Data: map[string]interface{}{"total": float64(5.5)}, UpdatedAt: now, TTLSeconds: 600},
		{Key: "session", Data: map[string]interface{}{"active": true}, UpdatedAt: now, TTLSeconds: 120},
	}

	for _, e := range entries {
		require.NoError(t, db.WriteFeedCache(ctx, e))
	}

	all, err := db.ReadAllFeedCache(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// Verify each key exists and has the right data shape.
	weatherData, ok := all["weather"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(72), weatherData["temp"])

	costsData, ok := all["costs"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(5.5), costsData["total"])

	sessionData, ok := all["session"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, sessionData["active"])
}

// Test 15: WriteFeedCacheJSON creates JSON file at correct path
func TestWriteFeedCacheJSON_CreatesFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hookwise-feed-json-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	outPath := filepath.Join(tmpDir, "status-line-cache.json")

	cacheData := map[string]interface{}{
		"weather": map[string]interface{}{"temp": float64(72)},
		"costs":   map[string]interface{}{"total": float64(5.5)},
	}

	require.NoError(t, WriteFeedCacheJSONTo(outPath, cacheData))

	// Verify file exists.
	info, err := os.Stat(outPath)
	require.NoError(t, err)
	assert.True(t, info.Size() > 0)
}

// Test 16: WriteFeedCacheJSON file content matches expected format
func TestWriteFeedCacheJSON_ContentFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hookwise-feed-json-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	outPath := filepath.Join(tmpDir, "status-line-cache.json")

	cacheData := map[string]interface{}{
		"weather": map[string]interface{}{
			"temperature": float64(72),
			"unit":        "F",
		},
		"session_info": map[string]interface{}{
			"active":   true,
			"duration": float64(300),
		},
	}

	require.NoError(t, WriteFeedCacheJSONTo(outPath, cacheData))

	// Read the file back and verify structure.
	raw, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &parsed))

	// Top-level keys should be the cache keys.
	assert.Contains(t, parsed, "weather")
	assert.Contains(t, parsed, "session_info")

	// Verify nested structure.
	weather, ok := parsed["weather"].(map[string]interface{})
	require.True(t, ok, "weather should be a map")
	assert.Equal(t, float64(72), weather["temperature"])
	assert.Equal(t, "F", weather["unit"])

	session, ok := parsed["session_info"].(map[string]interface{})
	require.True(t, ok, "session_info should be a map")
	assert.Equal(t, true, session["active"])
	assert.Equal(t, float64(300), session["duration"])
}

// Test 17: WriteFeedCacheJSON with empty map creates valid JSON
func TestWriteFeedCacheJSON_EmptyMap(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hookwise-feed-json-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	outPath := filepath.Join(tmpDir, "status-line-cache.json")

	require.NoError(t, WriteFeedCacheJSONTo(outPath, map[string]interface{}{}))

	raw, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &parsed))
	assert.Empty(t, parsed)
}

// Test 18: Feed cache overwrite replaces existing entry
func TestFeedCache_OverwriteReplaces(t *testing.T) {
	db, cleanup := testOpen(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	// Write initial entry.
	entry1 := FeedCacheEntry{
		Key:        "data",
		Data:       map[string]interface{}{"version": float64(1)},
		UpdatedAt:  now,
		TTLSeconds: 3600,
	}
	require.NoError(t, db.WriteFeedCache(ctx, entry1))

	// Overwrite with new data.
	entry2 := FeedCacheEntry{
		Key:        "data",
		Data:       map[string]interface{}{"version": float64(2), "extra": "field"},
		UpdatedAt:  now,
		TTLSeconds: 7200,
	}
	require.NoError(t, db.WriteFeedCache(ctx, entry2))

	loaded, err := db.ReadFeedCache(ctx, "data")
	require.NoError(t, err)
	require.NotNil(t, loaded)

	dataMap, ok := loaded.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), dataMap["version"])
	assert.Equal(t, "field", dataMap["extra"])
	assert.Equal(t, 7200, loaded.TTLSeconds)
}
