package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// calendarMockServer handles both the OAuth token endpoint and the Calendar
// events endpoint on a single httptest.Server.
//
// Routes:
//
//	POST /token             → fresh access token JSON (forces refresh when expiry is past)
//	GET  /calendars/*/events → minimal events.list response with one event
type calendarMockServer struct {
	tokenCalls  int // counts how many token refresh calls were made
	eventCalls  int // counts how many events list calls were made
	eventSummary string
}

func (m *calendarMockServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/token"):
		m.tokenCalls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"fresh-token-%d","token_type":"Bearer","expires_in":3600}`, m.tokenCalls)

	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/calendars/"):
		m.eventCalls++
		// Return a minimal Google Calendar events.list response.
		// start.dateTime must be in the future relative to timeMin param so the event
		// appears in the result window. The producer applies its own timeMin/timeMax
		// query params, but the mock ignores them and always returns the event.
		futureStart := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339)
		futureEnd := time.Now().UTC().Add(60 * time.Minute).Format(time.RFC3339)
		summary := m.eventSummary
		if summary == "" {
			summary = "Mock Standup"
		}
		resp := map[string]interface{}{
			"kind":  "calendar#events",
			"items": []interface{}{
				map[string]interface{}{
					"kind":    "calendar#event",
					"summary": summary,
					"start":   map[string]interface{}{"dateTime": futureStart},
					"end":     map[string]interface{}{"dateTime": futureEnd},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck

	default:
		http.NotFound(w, r)
	}
}

// writeTempTokenFile creates a Python-google-auth-format token JSON file in a
// temp directory. Setting expiry in the past forces oauth2 to perform a token
// refresh on first use, which is the operation that failed with the old code.
func writeTempTokenFile(t *testing.T, tokenURI string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "calendar-token.json")

	// Expiry 1 hour in the past so oauth2 treats the token as expired and
	// immediately attempts a refresh POST to tokenURI.
	pastExpiry := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)

	tok := map[string]interface{}{
		"token":         "expired-access-token",
		"refresh_token": "valid-refresh-token",
		"token_uri":     tokenURI,
		"client_id":     "test-client-id",
		"client_secret": "test-client-secret",
		"expiry":        pastExpiry,
	}
	data, err := json.Marshal(tok)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
	return path
}

// newCalendarProducerForTest builds a CalendarProducer wired to the given mock
// server. The caller must set feedsCfg.Calendar.TokenPath before calling Produce.
func newCalendarProducerForTest(baseURL string) *CalendarProducer {
	return &CalendarProducer{
		baseURL: baseURL,
	}
}

// ---------------------------------------------------------------------------
// Primary regression test: two-context poll. Before the fix, the second poll
// fails because the cached token source is bound to the (now-cancelled) ctx1.
// After the fix, the token source uses Background and the second poll succeeds.
// ---------------------------------------------------------------------------

func TestCalendarProducer_TwoContextPoll_SecondPollSucceeds(t *testing.T) {
	mock := &calendarMockServer{eventSummary: "Test Meeting"}
	srv := httptest.NewServer(mock)
	defer srv.Close()

	tokenPath := writeTempTokenFile(t, srv.URL+"/token")

	p := newCalendarProducerForTest(srv.URL + "/")
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			Enabled:          true,
			TokenPath:        tokenPath,
			Calendars:        []string{"primary"},
			LookaheadMinutes: 120,
		},
	})

	// --- Poll 1: use ctx1, then cancel it to simulate per-poll context dying. ---
	ctx1, cancel1 := context.WithCancel(context.Background())

	result1, err1 := p.Produce(ctx1)
	require.NoError(t, err1, "poll 1 must not error (ARCH-1)")

	env1 := result1.(map[string]interface{})
	assert.Equal(t, "calendar", env1["type"], "poll 1: envelope type must be 'calendar'")
	data1 := env1["data"].(map[string]interface{})
	events1, ok := data1["events"].([]interface{})
	require.True(t, ok, "poll 1: data.events must be a slice")
	assert.Greater(t, len(events1), 0, "poll 1: must return at least one event")

	// Cancel ctx1 — this is the action that caused the bug. The old code bound
	// the oauth2 token source to ctx1; after this cancel the refresh would fail.
	cancel1()

	// --- Poll 2: fresh context. Before the fix this returns the empty fallback.
	// After the fix the service and token source survive on Background. ---
	ctx2 := context.Background()

	result2, err2 := p.Produce(ctx2)
	require.NoError(t, err2, "poll 2 must not error (ARCH-1)")

	env2 := result2.(map[string]interface{})
	assert.Equal(t, "calendar", env2["type"], "poll 2: envelope type must be 'calendar'")
	data2 := env2["data"].(map[string]interface{})
	events2, ok := data2["events"].([]interface{})
	require.True(t, ok, "poll 2: data.events must be a slice")

	// The critical assertion: before the fix events2 is empty (fallback path).
	assert.Greater(t, len(events2), 0,
		"poll 2: must still return events after ctx1 was cancelled; "+
			"got empty result which means the bug (context-canceled on token refresh) is still present")

	// Sanity: event mock server was called twice.
	assert.GreaterOrEqual(t, mock.eventCalls, 2,
		"events endpoint must have been called for both polls")
}

// ---------------------------------------------------------------------------
// ARCH-1 test: missing token file → fail-open fallback, no error returned.
// ---------------------------------------------------------------------------

func TestCalendarProducer_MissingTokenFile_FailOpen(t *testing.T) {
	p := newCalendarProducerForTest("http://unused.invalid/")
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			Enabled:   true,
			TokenPath: "/nonexistent/path/calendar-token.json",
			Calendars: []string{"primary"},
		},
	})

	result, err := p.Produce(context.Background())

	// ARCH-1: must not return an error.
	require.NoError(t, err, "ARCH-1: missing token must not propagate as error")
	require.NotNil(t, result, "ARCH-1: result must not be nil")

	// The result must be the canonical calendar envelope.
	env, ok := result.(map[string]interface{})
	require.True(t, ok, "result must be map[string]interface{}")
	assert.Equal(t, "calendar", env["type"], "envelope type must be 'calendar'")

	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok, "envelope data must be a map")

	// Fallback envelope has empty events slice (not nil).
	events, ok := data["events"].([]interface{})
	require.True(t, ok, "fallback data.events must be []interface{}")
	assert.Empty(t, events, "fallback must have no events")

	// next_event must be present (nil value is ok) — key existence guards TUI rendering.
	_, hasNextEvent := data["next_event"]
	assert.True(t, hasNextEvent, "fallback envelope must contain 'next_event' key")
}

// ---------------------------------------------------------------------------
// Token refresh is actually attempted: verify the mock OAuth server is hit.
// ---------------------------------------------------------------------------

func TestCalendarProducer_TokenRefresh_IsAttemptedWhenExpired(t *testing.T) {
	mock := &calendarMockServer{eventSummary: "Refresh Check Meeting"}
	srv := httptest.NewServer(mock)
	defer srv.Close()

	// Expired token forces a refresh on first Produce call.
	tokenPath := writeTempTokenFile(t, srv.URL+"/token")

	p := newCalendarProducerForTest(srv.URL + "/")
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			Enabled:          true,
			TokenPath:        tokenPath,
			Calendars:        []string{"primary"},
			LookaheadMinutes: 60,
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	env := result.(map[string]interface{})
	assert.Equal(t, "calendar", env["type"])

	// The mock OAuth server must have been hit for the refresh.
	assert.GreaterOrEqual(t, mock.tokenCalls, 1,
		"oauth token endpoint must be called when token is expired")

	// And we must still get events back (not a fallback).
	data := env["data"].(map[string]interface{})
	events := data["events"].([]interface{})
	assert.Greater(t, len(events), 0, "must get events despite needing a token refresh")

	first := events[0].(map[string]interface{})
	assert.Equal(t, "Refresh Check Meeting", first["name"])
}
