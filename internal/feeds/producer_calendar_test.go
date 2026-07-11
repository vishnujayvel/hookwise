package feeds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// TestWriteBackToken_AtomicAndSecure pins the contract of writeBackToken (which
// had no coverage): the refreshed OAuth token is persisted in the Python
// google-auth format, with 0600 permissions (a credential file must never be
// world-readable), and via an atomic write that leaves no partial/temp files.
// The 0600 assertion is a real security-regression guard.
func TestWriteBackToken_AtomicAndSecure(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")

	tok := &oauth2.Token{
		AccessToken:  "access-xyz",
		RefreshToken: "refresh-abc",
		Expiry:       time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	cfg := &oauth2.Config{
		ClientID:     "cid",
		ClientSecret: "secret",
		Endpoint:     oauth2.Endpoint{TokenURL: "https://oauth2.example/token"},
	}

	writeBackToken(tokenPath, tok, cfg)

	// Credential file must be 0600.
	info, err := os.Stat(tokenPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "token file must be 0600")

	// Round-trips as the Python google-auth token format with the right fields.
	data, err := os.ReadFile(tokenPath)
	require.NoError(t, err)
	var ptf pythonTokenFile
	require.NoError(t, json.Unmarshal(data, &ptf))
	assert.Equal(t, "access-xyz", ptf.Token)
	assert.Equal(t, "refresh-abc", ptf.RefreshToken)
	assert.Equal(t, "cid", ptf.ClientID)
	assert.Equal(t, "secret", ptf.ClientSecret)
	assert.Equal(t, "https://oauth2.example/token", ptf.TokenURI)
	assert.Equal(t, "2030-01-02T03:04:05Z", ptf.Expiry)

	// Atomic write must leave no partial/temp files behind — only the token file.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "only the token file should remain (no .tmp-* leftovers)")
}

// calendarMockServer handles both the OAuth token endpoint and the Calendar
// events endpoint on a single httptest.Server.
//
// Routes:
//
//	POST /token             → fresh access token JSON (forces refresh when expiry is past)
//	GET  /calendars/*/events → minimal events.list response with one event
type calendarMockServer struct {
	tokenCalls   int // counts how many token refresh calls were made
	eventCalls   int // counts how many events list calls were made
	eventSummary string
	// tokenError, when non-empty, makes POST /token fail with HTTP 400 and
	// this RFC 6749 error code (e.g. "invalid_grant" = revoked refresh token).
	// Set before starting the server; never mutated afterwards.
	tokenError string
}

func (m *calendarMockServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/token"):
		m.tokenCalls++
		w.Header().Set("Content-Type", "application/json")
		if m.tokenError != "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":%q}`, m.tokenError)
			return
		}
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
			"kind": "calendar#events",
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
// Empty TokenPath fallback honors HOOKWISE_STATE_DIR at call time.
// ---------------------------------------------------------------------------

// TestCalendarProducer_TokenPathFallback_HonorsStateDirOverride verifies that
// when cfg.TokenPath is empty, Produce falls back to
// $HOOKWISE_STATE_DIR/calendar-token.json (resolved at call time), not the
// frozen core.DefaultCalendarTokenPath package var. Proven end-to-end: a
// token file written only at the override path is found and used to fetch
// events.
func TestCalendarProducer_TokenPathFallback_HonorsStateDirOverride(t *testing.T) {
	mock := &calendarMockServer{eventSummary: "Override Meeting"}
	srv := httptest.NewServer(mock)
	defer srv.Close()

	overrideDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", overrideDir)

	// Write the token file only at the override location — the frozen
	// ~/.hookwise/calendar-token.json path is never touched by this test.
	tokenPath := filepath.Join(overrideDir, "calendar-token.json")
	pastExpiry := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	tok := map[string]interface{}{
		"token":         "expired-access-token",
		"refresh_token": "valid-refresh-token",
		"token_uri":     srv.URL + "/token",
		"client_id":     "test-client-id",
		"client_secret": "test-client-secret",
		"expiry":        pastExpiry,
	}
	data, err := json.Marshal(tok)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tokenPath, data, 0600))

	p := newCalendarProducerForTest(srv.URL + "/")
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			Enabled:          true,
			TokenPath:        "", // empty — must fall back to core.GetStateDir()
			Calendars:        []string{"primary"},
			LookaheadMinutes: 120,
		},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: must not error")

	env := result.(map[string]interface{})
	data2 := env["data"].(map[string]interface{})
	events, ok := data2["events"].([]interface{})
	require.True(t, ok, "data.events must be a slice")
	assert.Greater(t, len(events), 0,
		"empty TokenPath must fall back to $HOOKWISE_STATE_DIR/calendar-token.json at call time; "+
			"an empty events result means the fallback missed the override file")
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

// ---------------------------------------------------------------------------
// Auth-dead detection (hw-b15m): classify 401/invalid_grant, drop the cached
// service so the token file is re-read, keep the fail-open fallback but never
// re-stamp its timestamp.
// ---------------------------------------------------------------------------

// TestIsAuthDeadError_Classification pins which failures count as a dead
// credential (mark auth-dead, drop client) vs transient (retry next poll).
func TestIsAuthDeadError_Classification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"invalid_grant retrieve error", &oauth2.RetrieveError{ErrorCode: "invalid_grant"}, true},
		{"invalid_grant wrapped in url.Error (real transport shape)",
			&url.Error{Op: "Get", URL: "https://example.com", Err: &oauth2.RetrieveError{ErrorCode: "invalid_grant"}}, true},
		{"401 from token endpoint", &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusUnauthorized}}, true},
		{"503 from token endpoint is transient", &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusServiceUnavailable}}, false},
		{"401 from calendar API", &googleapi.Error{Code: http.StatusUnauthorized}, true},
		{"403 from calendar API is not auth-dead", &googleapi.Error{Code: http.StatusForbidden}, false},
		{"500 from calendar API is transient", &googleapi.Error{Code: http.StatusInternalServerError}, false},
		{"network error is transient", errors.New("dial tcp: connection refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isAuthDeadError(tc.err))
		})
	}
}

// TestCalendarProducer_RefreshFailure_InvalidGrant_MarksAuthDead exercises the
// refresh-FAILURE path end-to-end: an expired access token forces a refresh,
// the token endpoint rejects it with invalid_grant (revoked refresh token),
// and the producer must (a) stay fail-open (no error, fallback envelope) and
// (b) flag itself auth-dead for the feed-health path (AuthReporter).
func TestCalendarProducer_RefreshFailure_InvalidGrant_MarksAuthDead(t *testing.T) {
	mock := &calendarMockServer{tokenError: "invalid_grant"}
	srv := httptest.NewServer(mock)
	defer srv.Close()

	tokenPath := writeTempTokenFile(t, srv.URL+"/token")

	p := newCalendarProducerForTest(srv.URL + "/")
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			Enabled:   true,
			TokenPath: tokenPath,
			Calendars: []string{"primary"},
		},
	})

	assert.False(t, p.AuthDead(), "producer must not start auth-dead")

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: auth failure must not propagate as error")

	env := result.(map[string]interface{})
	data := env["data"].(map[string]interface{})
	events := data["events"].([]interface{})
	assert.Empty(t, events, "no prior success: fallback must be the empty envelope")

	assert.True(t, p.AuthDead(),
		"invalid_grant on refresh must mark the producer auth-dead")
	assert.GreaterOrEqual(t, mock.tokenCalls, 1, "refresh must have been attempted")
}

// TestCalendarProducer_TokenRotation_PickedUpWithoutRestart is the re-auth
// regression test: before the fix, ensureClient short-circuited on the cached
// service forever, so fixing the token file on disk was invisible until a
// daemon restart. After an auth-dead failure the cached service is dropped,
// the token file is re-read on the next poll, and a rotated (valid) token
// recovers the feed — and clears the auth-dead flag.
func TestCalendarProducer_TokenRotation_PickedUpWithoutRestart(t *testing.T) {
	badMock := &calendarMockServer{tokenError: "invalid_grant"}
	srvBad := httptest.NewServer(badMock)
	defer srvBad.Close()

	goodMock := &calendarMockServer{eventSummary: "Post-Rotation Meeting"}
	srvGood := httptest.NewServer(goodMock)
	defer srvGood.Close()

	// Token file initially points its token_uri at the failing endpoint —
	// this is the on-disk state while the refresh token is revoked.
	tokenPath := writeTempTokenFile(t, srvBad.URL+"/token")

	// Events always go to the good server; only the credential is dead.
	p := newCalendarProducerForTest(srvGood.URL + "/")
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			Enabled:          true,
			TokenPath:        tokenPath,
			Calendars:        []string{"primary"},
			LookaheadMinutes: 120,
		},
	})

	// Poll 1: refresh fails with invalid_grant → auth-dead, fallback.
	_, err := p.Produce(context.Background())
	require.NoError(t, err)
	require.True(t, p.AuthDead(), "poll 1 must mark auth-dead")

	// User re-authenticates: the token file is rewritten in place with a
	// working credential (token_uri now points at the good endpoint).
	pastExpiry := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	rotated := map[string]interface{}{
		"token":         "expired-access-token",
		"refresh_token": "fresh-valid-refresh-token",
		"token_uri":     srvGood.URL + "/token",
		"client_id":     "test-client-id",
		"client_secret": "test-client-secret",
		"expiry":        pastExpiry,
	}
	data, err := json.Marshal(rotated)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tokenPath, data, 0600))

	// Poll 2: NO restart, same producer instance. The rotated token file
	// must be picked up because the dead client was dropped.
	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	env := result.(map[string]interface{})
	events := env["data"].(map[string]interface{})["events"].([]interface{})
	require.NotEmpty(t, events,
		"rotated token file must be re-read on the next poll without a daemon restart")
	assert.Equal(t, "Post-Rotation Meeting", events[0].(map[string]interface{})["name"])
	assert.False(t, p.AuthDead(), "successful poll must clear the auth-dead flag")
	assert.GreaterOrEqual(t, goodMock.tokenCalls, 1, "refresh must have hit the rotated token_uri")
}

// TestCalendarProducer_FallbackTimestampFrozen_AcrossFailingPolls verifies the
// frozen-timestamp contract: once polls start failing, the fallback envelope
// keeps the LAST SUCCESSFUL poll's timestamp verbatim across >=3 failing
// polls. The daemon rewrites the cache file every poll (err is nil, ARCH-1),
// so a re-stamped timestamp would keep the segment TTL-fresh forever; frozen,
// TTL expiry eventually hides it.
func TestCalendarProducer_FallbackTimestampFrozen_AcrossFailingPolls(t *testing.T) {
	mock := &calendarMockServer{eventSummary: "Freeze Check Meeting"}
	srv := httptest.NewServer(mock)

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

	// Poll 1: success — this stamps the envelope.
	result1, err := p.Produce(context.Background())
	require.NoError(t, err)
	env1 := result1.(map[string]interface{})
	ts1 := env1["timestamp"].(string)
	require.NotEmpty(t, ts1)
	events1 := env1["data"].(map[string]interface{})["events"].([]interface{})
	require.NotEmpty(t, events1, "poll 1 must succeed")

	// Kill the API: every subsequent poll fails (transient network error).
	srv.Close()

	// Envelope timestamps are RFC3339 at second precision — cross a second
	// boundary so a re-stamped envelope would provably differ.
	time.Sleep(1100 * time.Millisecond)

	for i := 2; i <= 4; i++ {
		result, err := p.Produce(context.Background())
		require.NoError(t, err, "ARCH-1: failing poll %d must not error", i)
		env := result.(map[string]interface{})
		assert.Equal(t, ts1, env["timestamp"],
			"failing poll %d must return the last successful envelope with its timestamp FROZEN", i)
		events := env["data"].(map[string]interface{})["events"].([]interface{})
		assert.NotEmpty(t, events, "failing poll %d must keep serving cached events (fail-open)", i)
	}

	assert.False(t, p.AuthDead(), "network failure is transient, not auth-dead")
}

// TestCalendarProducer_NoPriorSuccess_EmptyFallbackTimestampFrozen covers the
// daemon-restarted-with-broken-auth case: with no cached success, the empty
// fallback envelope must also keep a frozen timestamp across failing polls,
// so a permanently-broken producer cannot render an eternally-fresh "Free".
func TestCalendarProducer_NoPriorSuccess_EmptyFallbackTimestampFrozen(t *testing.T) {
	p := newCalendarProducerForTest("http://unused.invalid/")
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			Enabled:   true,
			TokenPath: "/nonexistent/path/calendar-token.json",
			Calendars: []string{"primary"},
		},
	})

	result1, err := p.Produce(context.Background())
	require.NoError(t, err)
	ts1 := result1.(map[string]interface{})["timestamp"].(string)
	require.NotEmpty(t, ts1)

	// Cross a second boundary so a re-stamp would provably differ.
	time.Sleep(1100 * time.Millisecond)

	for i := 2; i <= 3; i++ {
		result, err := p.Produce(context.Background())
		require.NoError(t, err, "ARCH-1: failing poll %d must not error", i)
		assert.Equal(t, ts1, result.(map[string]interface{})["timestamp"],
			"empty fallback envelope must not be re-stamped on failing poll %d", i)
	}
}

// ---------------------------------------------------------------------------
// Timezone + malformed-date fixtures (hw-8d9k): every earlier calendar test
// used uniform time.UTC, leaving parseGoogleEventTime's non-UTC-offset
// handling and its malformed-date silent-drop path unpinned. Google returns
// event times in the calendar's own timezone (e.g. +05:30), so these are the
// timestamps the producer actually sees in the wild.
// ---------------------------------------------------------------------------

// TestParseGoogleEventTime_OffsetsAndMalformed pins parseGoogleEventTime
// directly: non-UTC offsets must parse to the correct instant with the offset
// preserved, and malformed values must yield zero-time without claiming
// all-day.
func TestParseGoogleEventTime_OffsetsAndMalformed(t *testing.T) {
	cases := []struct {
		name       string
		edt        *calendar.EventDateTime
		wantZero   bool
		wantAllDay bool
		wantUTC    time.Time // instant the parsed value must equal (when !wantZero)
		wantOffset int       // expected zone offset in seconds (when !wantZero)
	}{
		{
			name:       "dateTime with +05:30 offset",
			edt:        &calendar.EventDateTime{DateTime: "2026-06-15T15:30:00+05:30"},
			wantUTC:    time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
			wantOffset: 5*3600 + 30*60,
		},
		{
			name:       "dateTime with -08:00 offset",
			edt:        &calendar.EventDateTime{DateTime: "2026-06-15T02:00:00-08:00"},
			wantUTC:    time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
			wantOffset: -8 * 3600,
		},
		{
			name:       "dateTime with Z suffix",
			edt:        &calendar.EventDateTime{DateTime: "2026-06-15T10:00:00Z"},
			wantUTC:    time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
			wantOffset: 0,
		},
		{
			name:       "all-day date parses as UTC midnight",
			edt:        &calendar.EventDateTime{Date: "2026-06-15"},
			wantAllDay: true,
			wantUTC:    time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
			wantOffset: 0,
		},
		{
			name:     "nil EventDateTime",
			edt:      nil,
			wantZero: true,
		},
		{
			name:     "empty EventDateTime",
			edt:      &calendar.EventDateTime{},
			wantZero: true,
		},
		{
			name:     "garbage dateTime",
			edt:      &calendar.EventDateTime{DateTime: "not-a-timestamp"},
			wantZero: true,
		},
		{
			name:     "non-RFC3339 dateTime (space separator)",
			edt:      &calendar.EventDateTime{DateTime: "2026-06-15 10:00:00"},
			wantZero: true,
		},
		{
			name:     "out-of-range dateTime components",
			edt:      &calendar.EventDateTime{DateTime: "2026-13-45T99:99:99Z"},
			wantZero: true,
		},
		{
			name:     "garbage all-day date must not claim all-day",
			edt:      &calendar.EventDateTime{Date: "not-a-date"},
			wantZero: true,
		},
		{
			name:     "impossible calendar date (Feb 30)",
			edt:      &calendar.EventDateTime{Date: "2026-02-30"},
			wantZero: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, allDay := parseGoogleEventTime(tc.edt)
			if tc.wantZero {
				assert.True(t, got.IsZero(), "malformed/empty input must yield zero-time")
				assert.False(t, allDay, "malformed input must never claim all-day")
				return
			}
			assert.True(t, got.Equal(tc.wantUTC),
				"parsed instant %v must equal %v", got, tc.wantUTC)
			_, off := got.Zone()
			assert.Equal(t, tc.wantOffset, off, "source offset must be preserved")
			assert.Equal(t, tc.wantAllDay, allDay)
		})
	}
}

// TestCalendarProducer_OffsetEvents_WindowFiltering runs the producer
// end-to-end against events whose timestamps carry a +05:30 offset while the
// producer's "now" is time.Now().UTC(). is_current and next_event selection
// compare instants, so an in-progress offset event must still be flagged
// current and the upcoming offset event must still win next_event.
func TestCalendarProducer_OffsetEvents_WindowFiltering(t *testing.T) {
	ist := time.FixedZone("UTC+05:30", 5*3600+30*60)
	now := time.Now()

	// In-progress event (started 10m ago, ends in 20m) expressed in +05:30.
	currentStart := now.Add(-10 * time.Minute).In(ist).Format(time.RFC3339)
	currentEnd := now.Add(20 * time.Minute).In(ist).Format(time.RFC3339)
	// Upcoming event (starts in 45m) expressed in +05:30.
	upcomingStart := now.Add(45 * time.Minute).In(ist).Format(time.RFC3339)
	upcomingEnd := now.Add(75 * time.Minute).In(ist).Format(time.RFC3339)

	apiResp := fakeCalendarAPIResponse([]map[string]interface{}{
		{
			"summary": "IST Current Meeting",
			"start":   map[string]string{"dateTime": currentStart},
			"end":     map[string]string{"dateTime": currentEnd},
		},
		{
			"summary": "IST Next Meeting",
			"start":   map[string]string{"dateTime": upcomingStart},
			"end":     map[string]string{"dateTime": upcomingEnd},
		},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, apiResp)
	}))
	defer srv.Close()

	tokenPath := writeFakeToken(t, t.TempDir())
	p := &CalendarProducer{baseURL: srv.URL}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{TokenPath: tokenPath},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	events := data["events"].([]interface{})
	require.Len(t, events, 2)

	first := events[0].(map[string]interface{})
	assert.True(t, first["is_current"].(bool),
		"offset event spanning UTC-now must be flagged current")
	assert.True(t, strings.HasSuffix(first["start"].(string), "+05:30"),
		"formatted start must keep the source offset, got %q", first["start"])

	second := events[1].(map[string]interface{})
	assert.False(t, second["is_current"].(bool))

	nextEvent := data["next_event"].(map[string]interface{})
	assert.Equal(t, "IST Next Meeting", nextEvent["name"],
		"upcoming offset event must be selected as next_event")

	// The stored absolute start must round-trip to the correct instant.
	parsed, err := time.Parse(time.RFC3339, nextEvent["start"].(string))
	require.NoError(t, err)
	assert.WithinDuration(t, now.Add(45*time.Minute), parsed, 2*time.Second,
		"next_event start must be the same instant regardless of offset")
}

// TestCalendarProducer_MalformedDates_SilentDrop pins the silent-drop
// contract for unparseable event times: the event still appears in the events
// list but with empty start/end strings, is never flagged current, and is
// skipped for next_event selection in favor of the first valid event
// (ARCH-1: no error, no crash).
func TestCalendarProducer_MalformedDates_SilentDrop(t *testing.T) {
	now := time.Now()
	validStart := now.Add(30 * time.Minute).UTC().Format(time.RFC3339)
	validEnd := now.Add(60 * time.Minute).UTC().Format(time.RFC3339)

	apiResp := fakeCalendarAPIResponse([]map[string]interface{}{
		{
			"summary": "Broken Meeting",
			"start":   map[string]string{"dateTime": "not-a-timestamp"},
			"end":     map[string]string{"dateTime": "2026-13-45T99:99:99Z"},
		},
		{
			"summary": "Valid Meeting",
			"start":   map[string]string{"dateTime": validStart},
			"end":     map[string]string{"dateTime": validEnd},
		},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, apiResp)
	}))
	defer srv.Close()

	tokenPath := writeFakeToken(t, t.TempDir())
	p := &CalendarProducer{baseURL: srv.URL}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{TokenPath: tokenPath},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: malformed event times must not error")

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	events := data["events"].([]interface{})
	require.Len(t, events, 2, "malformed event stays in the list, not removed")

	broken := events[0].(map[string]interface{})
	assert.Equal(t, "Broken Meeting", broken["name"])
	assert.Equal(t, "", broken["start"], "zero-time start renders as empty string")
	assert.Equal(t, "", broken["end"], "zero-time end renders as empty string")
	assert.False(t, broken["all_day"].(bool), "malformed dateTime must not claim all-day")
	assert.False(t, broken["is_current"].(bool), "zero-time event can never be current")

	nextEvent := data["next_event"].(map[string]interface{})
	assert.Equal(t, "Valid Meeting", nextEvent["name"],
		"next_event selection must skip the malformed event")
}

// TestCalendarProducer_OnlyMalformedDates_NoNextEvent covers the degenerate
// window where every event has unparseable times: the list is served as-is
// (fail-open) but nothing qualifies as next_event.
func TestCalendarProducer_OnlyMalformedDates_NoNextEvent(t *testing.T) {
	apiResp := fakeCalendarAPIResponse([]map[string]interface{}{
		{
			"summary": "Only Broken Meeting",
			"start":   map[string]string{"dateTime": "garbage"},
			"end":     map[string]string{"dateTime": "garbage"},
		},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, apiResp)
	}))
	defer srv.Close()

	tokenPath := writeFakeToken(t, t.TempDir())
	p := &CalendarProducer{baseURL: srv.URL}
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{TokenPath: tokenPath},
	})

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: malformed-only window must not error")

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	events := data["events"].([]interface{})
	require.Len(t, events, 1)
	assert.Equal(t, "", events[0].(map[string]interface{})["start"])
	assert.Nil(t, data["next_event"],
		"a zero-time event must never be promoted to next_event")
}
