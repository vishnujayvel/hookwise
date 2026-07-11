package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishnujayvel/hookwise/internal/bridge"
)

// calendarEnvelopeAt builds a Go-envelope cache entry for the calendar feed
// with the given embedded timestamp. Freshness is derived from this timestamp
// (bridge.IsEnvelopeFresh), never from file mtime, so tests age entries by
// moving the timestamp — mirroring how a dead daemon leaves frozen content.
func calendarEnvelopeAt(ts time.Time, data map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type":      "calendar",
		"timestamp": ts.UTC().Format(time.RFC3339),
		"data":      data,
	}
}

// upcomingCalendarData returns calendar feed data with a next_event starting
// in the future, matching the schema pinned by feeds.CalendarTestFixture.
func upcomingCalendarData(eventStart time.Time) map[string]interface{} {
	start := eventStart.UTC().Format(time.RFC3339)
	return map[string]interface{}{
		"events": []interface{}{
			map[string]interface{}{
				"name":       "Standup",
				"start":      start,
				"end":        eventStart.Add(30 * time.Minute).UTC().Format(time.RFC3339),
				"all_day":    false,
				"is_current": false,
			},
		},
		"next_event": map[string]interface{}{
			"name":  "Standup",
			"start": start,
		},
	}
}

// ---------------------------------------------------------------------------
// feedData: missing / malformed cache entries are treated as absent
// ---------------------------------------------------------------------------

func TestFeedData_MissingOrMalformedReturnsNil(t *testing.T) {
	freshData := upcomingCalendarData(time.Now().Add(time.Hour))

	tests := []struct {
		name  string
		cache map[string]interface{}
	}{
		{"nil cache", nil},
		{"feed absent", map[string]interface{}{}},
		{"envelope not a map", map[string]interface{}{"calendar": "not-a-map"}},
		{"missing timestamp", map[string]interface{}{
			"calendar": map[string]interface{}{"type": "calendar", "data": freshData},
		}},
		{"timestamp not a string", map[string]interface{}{
			"calendar": map[string]interface{}{"type": "calendar", "timestamp": 12345.0, "data": freshData},
		}},
		{"unparseable timestamp", map[string]interface{}{
			"calendar": map[string]interface{}{"type": "calendar", "timestamp": "not-a-time", "data": freshData},
		}},
		{"missing data key", map[string]interface{}{
			"calendar": map[string]interface{}{
				"type":      "calendar",
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			},
		}},
		{"data not a map", map[string]interface{}{
			"calendar": map[string]interface{}{
				"type":      "calendar",
				"timestamp": time.Now().UTC().Format(time.RFC3339),
				"data":      []interface{}{"wrong shape"},
			},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Nil(t, feedData(tc.cache, "calendar"))
		})
	}
}

// ---------------------------------------------------------------------------
// feedData: TTL freshness gate — stale entries are treated as absent
// ---------------------------------------------------------------------------

func TestFeedData_FreshnessGate(t *testing.T) {
	now := time.Now()

	withTTL := func(data map[string]interface{}, ttl float64) map[string]interface{} {
		// ttl_seconds arrives as float64 after a JSON cache round-trip.
		data["ttl_seconds"] = ttl
		return data
	}

	tests := []struct {
		name     string
		envelope map[string]interface{}
		wantData bool
	}{
		{
			name:     "fresh envelope within default TTL",
			envelope: calendarEnvelopeAt(now, upcomingCalendarData(now.Add(time.Hour))),
			wantData: true,
		},
		{
			name: "stale envelope past default TTL",
			envelope: calendarEnvelopeAt(
				now.Add(-time.Duration(bridge.DefaultTTLSeconds+60)*time.Second),
				upcomingCalendarData(now.Add(time.Hour)),
			),
			wantData: false,
		},
		{
			name: "custom ttl_seconds keeps older entry fresh",
			envelope: calendarEnvelopeAt(
				now.Add(-400*time.Second),
				withTTL(upcomingCalendarData(now.Add(time.Hour)), 600),
			),
			wantData: true,
		},
		{
			name: "custom ttl_seconds expires entry before default TTL",
			envelope: calendarEnvelopeAt(
				now.Add(-200*time.Second),
				withTTL(upcomingCalendarData(now.Add(time.Hour)), 120),
			),
			wantData: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cache := map[string]interface{}{"calendar": tc.envelope}
			data := feedData(cache, "calendar")
			if tc.wantData {
				require.NotNil(t, data, "fresh envelope must yield its data map")
				assert.Contains(t, data, "next_event")
			} else {
				assert.Nil(t, data, "stale envelope must be treated as absent")
			}
		})
	}
}

func TestFeedData_PlaceholderSourceReturnsNil(t *testing.T) {
	cache := map[string]interface{}{
		"calendar": calendarEnvelopeAt(time.Now(), map[string]interface{}{
			"source":     "placeholder",
			"next_event": map[string]interface{}{"name": "Standup"},
		}),
	}
	assert.Nil(t, feedData(cache, "calendar"), "placeholder data must be treated as absent")
}

// ---------------------------------------------------------------------------
// renderCalendarSegment: fresh/stale/missing cache
// ---------------------------------------------------------------------------

func TestRenderCalendarSegment_FreshCacheRenders(t *testing.T) {
	// Presence detector: proves the assertions below can distinguish
	// rendered-vs-omitted, so the stale/missing empty results are meaningful.
	cache := map[string]interface{}{
		"calendar": calendarEnvelopeAt(time.Now(), upcomingCalendarData(time.Now().Add(45*time.Minute))),
	}

	got := stripANSI(renderCalendarSegment(cache))
	require.NotEmpty(t, got, "fresh calendar envelope must render a segment")
	assert.Contains(t, got, "\U0001f4c5")
	assert.Contains(t, got, "Standup")
	assert.Contains(t, got, "in ", "relative time must be computed from the absolute start")
}

func TestRenderCalendarSegment_StaleCacheOmitted(t *testing.T) {
	// Same payload as the fresh case — only the embedded timestamp differs.
	// A dead daemon leaves exactly this shape behind: valid event data with
	// an aging timestamp. Past TTL the segment must vanish, not linger.
	cache := map[string]interface{}{
		"calendar": calendarEnvelopeAt(
			time.Now().Add(-time.Duration(bridge.DefaultTTLSeconds+60)*time.Second),
			upcomingCalendarData(time.Now().Add(45*time.Minute)),
		),
	}

	assert.Empty(t, renderCalendarSegment(cache), "stale calendar envelope must render nothing")
}

func TestRenderCalendarSegment_MissingCacheOmitted(t *testing.T) {
	assert.Empty(t, renderCalendarSegment(nil), "nil cache must render nothing")
	assert.Empty(t, renderCalendarSegment(map[string]interface{}{}), "absent feed must render nothing")
}

func TestRenderCalendarSegment_NextEventVariants(t *testing.T) {
	fresh := func(nextEvent interface{}) map[string]interface{} {
		return map[string]interface{}{
			"calendar": calendarEnvelopeAt(time.Now(), map[string]interface{}{
				"next_event": nextEvent,
			}),
		}
	}

	t.Run("static time fallback when start missing", func(t *testing.T) {
		got := stripANSI(renderCalendarSegment(fresh(map[string]interface{}{
			"name": "1:1",
			"time": "3pm",
		})))
		assert.Contains(t, got, "1:1 3pm")
	})

	t.Run("name-only map renders name", func(t *testing.T) {
		got := stripANSI(renderCalendarSegment(fresh(map[string]interface{}{
			"name": "Focus block",
		})))
		assert.Contains(t, got, "Focus block")
	})

	t.Run("empty map omitted", func(t *testing.T) {
		assert.Empty(t, renderCalendarSegment(fresh(map[string]interface{}{})))
	})

	t.Run("empty string omitted", func(t *testing.T) {
		assert.Empty(t, renderCalendarSegment(fresh("")))
	})

	t.Run("unexpected type omitted", func(t *testing.T) {
		assert.Empty(t, renderCalendarSegment(fresh(42.0)))
	})
}
