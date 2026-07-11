package feeds

// Pinned-behavior tests (scout hw-xthx blind spot #8, bead hw-rnqs).
//
// These tests PIN the calendar producer's CURRENT behavior for two edge
// cases. They are NOT an endorsement of that behavior — whether each case
// should be accepted or quarantined is a product/policy decision that has
// been explicitly deferred (pinned-pending-policy). If a deliberate policy
// change later alters either behavior, update these tests alongside it.
//
//  1. A genuine HTTP 200 with items:[] is treated as a SUCCESS and
//     overwrites the cached last-good result. A transient upstream glitch
//     that returns an empty-but-200 body therefore makes the TUI render
//     "Free" while the user is actually busy, and destroys the good cache
//     that the fail-open fallback would otherwise have kept serving.
//  2. The events request is capped at MaxResults(20) and NextPageToken is
//     never followed, so a calendar with more than 20 events in the
//     lookahead window is silently truncated to the first 20.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// pinnedCalendarMock emulates the Google Calendar API closely enough to pin
// truncation semantics: it holds a pool of totalEvents events, honors the
// maxResults query param the way the real API does (returns at most
// maxResults items and sets nextPageToken when more remain), and can be
// flipped to return a genuine 200 with items:[]. All mutable state is
// mutex-guarded because httptest handlers run on server goroutines.
type pinnedCalendarMock struct {
	mu             sync.Mutex
	eventCalls     int
	lastMaxResults string // maxResults query param seen on the last events call
	pageTokenSeen  bool   // true if any events call carried a pageToken param
	emptyItems     bool   // when true, events endpoint returns 200 with items:[]
	totalEvents    int    // size of the emulated busy calendar
}

func (m *pinnedCalendarMock) setEmptyItems(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emptyItems = v
}

func (m *pinnedCalendarMock) snapshot() (eventCalls int, lastMaxResults string, pageTokenSeen bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.eventCalls, m.lastMaxResults, m.pageTokenSeen
}

func (m *pinnedCalendarMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/token"):
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"pinned-fresh-token","token_type":"Bearer","expires_in":3600}`)

	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/calendars/"):
		m.mu.Lock()
		m.eventCalls++
		m.lastMaxResults = r.URL.Query().Get("maxResults")
		if r.URL.Query().Get("pageToken") != "" {
			m.pageTokenSeen = true
		}
		empty := m.emptyItems
		total := m.totalEvents
		m.mu.Unlock()

		resp := map[string]interface{}{
			"kind":  "calendar#events",
			"items": []interface{}{},
		}
		if !empty {
			maxResults := total
			if s := r.URL.Query().Get("maxResults"); s != "" {
				if n, err := strconv.Atoi(s); err == nil && n < maxResults {
					maxResults = n
				}
			}
			items := make([]interface{}, 0, maxResults)
			now := time.Now().UTC()
			for i := 0; i < maxResults; i++ {
				start := now.Add(time.Duration(i+1) * time.Minute).Format(time.RFC3339)
				end := now.Add(time.Duration(i+6) * time.Minute).Format(time.RFC3339)
				items = append(items, map[string]interface{}{
					"kind":    "calendar#event",
					"summary": fmt.Sprintf("Pinned Event %d", i+1),
					"start":   map[string]interface{}{"dateTime": start},
					"end":     map[string]interface{}{"dateTime": end},
				})
			}
			resp["items"] = items
			if maxResults < total {
				resp["nextPageToken"] = "pinned-next-page"
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck

	default:
		http.NotFound(w, r)
	}
}

func newPinnedCalendarProducer(t *testing.T, srvURL string) *CalendarProducer {
	t.Helper()
	tokenPath := writeTempTokenFile(t, srvURL+"/token")
	p := newCalendarProducerForTest(srvURL + "/")
	p.SetFeedsConfig(core.FeedsConfig{
		Calendar: core.CalendarFeedConfig{
			Enabled:          true,
			TokenPath:        tokenPath,
			Calendars:        []string{"primary"},
			LookaheadMinutes: 120,
		},
	})
	return p
}

func calendarEvents(t *testing.T, result interface{}) []interface{} {
	t.Helper()
	env, ok := result.(map[string]interface{})
	require.True(t, ok, "result must be map[string]interface{}")
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok, "envelope data must be a map")
	events, ok := data["events"].([]interface{})
	require.True(t, ok, "data.events must be []interface{}")
	return events
}

// TestCalendarProducer_Pinned_Genuine200EmptyItems_OverwritesGoodCache pins
// CURRENT behavior (pinned-pending-policy, hw-rnqs): a genuine HTTP 200 whose
// body has items:[] is indistinguishable from "the calendar really is free",
// so the producer accepts it as a success and OVERWRITES the cached last-good
// result. Consequences pinned here:
//
//   - the poll immediately renders zero events ("Free") even if the previous
//     poll saw real events, and
//   - a subsequent FAILING poll serves the empty envelope from cache — the
//     good data is gone, the fail-open fallback cannot resurrect it.
//
// Whether an empty-200 should instead be quarantined (e.g. kept out of the
// cache until confirmed by a second poll) is a policy decision deferred to
// Vishnu. Do NOT "fix" this by changing production code without that call.
func TestCalendarProducer_Pinned_Genuine200EmptyItems_OverwritesGoodCache(t *testing.T) {
	mock := &pinnedCalendarMock{totalEvents: 1}
	srv := httptest.NewServer(mock)

	p := newPinnedCalendarProducer(t, srv.URL)

	// Poll 1: success with one real event — this is the "good cache".
	result1, err := p.Produce(context.Background())
	require.NoError(t, err)
	require.Len(t, calendarEvents(t, result1), 1, "poll 1 must cache one real event")

	// Poll 2: genuine 200 with items:[]. Current behavior: accepted as
	// success, cache overwritten, renders as "Free".
	mock.setEmptyItems(true)
	result2, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: empty-200 poll must not error")
	env2 := result2.(map[string]interface{})
	assert.Empty(t, calendarEvents(t, result2),
		"PINNED: a genuine 200 with items:[] replaces the good result with an empty one")
	assert.Nil(t, env2["data"].(map[string]interface{})["next_event"],
		"PINNED: empty-200 poll clears next_event")

	// Poll 3: the API dies (transient network failure). The fail-open
	// fallback serves the cached envelope — which is now the EMPTY one from
	// poll 2, not the good one from poll 1. This is the destructive part of
	// the overwrite: fallback can only serve what the empty-200 left behind.
	srv.Close()
	result3, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: failing poll must not error")
	assert.Empty(t, calendarEvents(t, result3),
		"PINNED: after an empty-200, the fallback cache holds zero events — poll 1's good data is unrecoverable")
	assert.Equal(t, env2["timestamp"], result3.(map[string]interface{})["timestamp"],
		"fallback must serve poll 2's envelope verbatim (frozen timestamp)")
}

// TestCalendarProducer_Pinned_BusyCalendarTruncatedAt20_NoPagination pins
// CURRENT behavior (pinned-pending-policy, hw-rnqs): the events request is
// issued with maxResults=20 and the response's nextPageToken is never
// followed. Against an emulated busy calendar with 25 events in the
// lookahead window, the envelope silently contains only the first 20 —
// events 21+ are invisible to the TUI, including a potential next_event.
//
// Whether the producer should paginate (or raise MaxResults) is a policy
// decision deferred to Vishnu. Do NOT "fix" this by changing production code
// without that call. This test fails if someone adds pagination or changes
// the MaxResults cap — update it alongside that deliberate change.
func TestCalendarProducer_Pinned_BusyCalendarTruncatedAt20_NoPagination(t *testing.T) {
	mock := &pinnedCalendarMock{totalEvents: 25}
	srv := httptest.NewServer(mock)
	defer srv.Close()

	p := newPinnedCalendarProducer(t, srv.URL)

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	events := calendarEvents(t, result)
	assert.Len(t, events, 20,
		"PINNED: 25 upcoming events must be truncated to exactly 20 (MaxResults cap)")
	// The truncation keeps the FIRST 20 by start time; event 21+ never appears.
	first := events[0].(map[string]interface{})
	last := events[19].(map[string]interface{})
	assert.Equal(t, "Pinned Event 1", first["name"])
	assert.Equal(t, "Pinned Event 20", last["name"])

	eventCalls, lastMaxResults, pageTokenSeen := mock.snapshot()
	assert.Equal(t, "20", lastMaxResults,
		"PINNED: the request must carry maxResults=20")
	assert.Equal(t, 1, eventCalls,
		"PINNED: exactly one events call — nextPageToken must NOT be followed")
	assert.False(t, pageTokenSeen,
		"PINNED: no request may carry a pageToken param (no pagination)")
}
