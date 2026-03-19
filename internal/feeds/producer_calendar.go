package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// defaultCalendarLookaheadMinutes is the fallback when LookaheadMinutes is not configured.
const defaultCalendarLookaheadMinutes = 120

// CalendarProducer fetches upcoming events from Google Calendar API.
// It implements ConfigAware to receive token/credentials paths from feed config.
type CalendarProducer struct {
	mu         sync.Mutex
	feedsCfg   core.FeedsConfig
	lastResult map[string]interface{}
	baseURL    string // override endpoint for testing; empty = default Google API

	// Cached across polls — initialized on first successful token load.
	client   *http.Client
	oauthCfg *oauth2.Config
	origTok  *oauth2.Token
	svc      *calendar.Service
}

func (p *CalendarProducer) Name() string { return "calendar" }

// SetFeedsConfig receives the feed configuration (ConfigAware interface).
func (p *CalendarProducer) SetFeedsConfig(cfg core.FeedsConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.feedsCfg = cfg
}

// pythonTokenFile maps the JSON format written by Python's google-auth library.
type pythonTokenFile struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	TokenURI     string `json:"token_uri"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Expiry       string `json:"expiry"`
}

// loadCalendarOAuthClient reads a Python google-auth token file and returns
// an HTTP client with automatic token refresh.
func loadCalendarOAuthClient(ctx context.Context, tokenPath string) (*http.Client, *oauth2.Token, *oauth2.Config, error) {
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read token: %w", err)
	}

	var ptf pythonTokenFile
	if err := json.Unmarshal(data, &ptf); err != nil {
		return nil, nil, nil, fmt.Errorf("parse token: %w", err)
	}

	if ptf.ClientID == "" || ptf.ClientSecret == "" {
		return nil, nil, nil, fmt.Errorf("token file missing client_id or client_secret")
	}

	tokenURI := ptf.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}

	cfg := &oauth2.Config{
		ClientID:     ptf.ClientID,
		ClientSecret: ptf.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenURI,
		},
		Scopes: []string{"https://www.googleapis.com/auth/calendar.readonly"},
	}

	tok := &oauth2.Token{
		AccessToken:  ptf.Token,
		RefreshToken: ptf.RefreshToken,
		TokenType:    "Bearer",
	}

	// Parse expiry if present.
	if ptf.Expiry != "" {
		for _, layout := range []string{
			time.RFC3339,
			"2006-01-02T15:04:05.999999999Z07:00",
			"2006-01-02T15:04:05Z",
			"2006-01-02T15:04:05.999999Z",
		} {
			if t, err := time.Parse(layout, ptf.Expiry); err == nil {
				tok.Expiry = t
				break
			}
		}
	}

	client := cfg.Client(ctx, tok)
	return client, tok, cfg, nil
}

// writeBackToken writes the refreshed token back to disk in Python google-auth format
// so that the Python calendar-feed.py script stays compatible.
func writeBackToken(tokenPath string, tok *oauth2.Token, cfg *oauth2.Config) {
	ptf := pythonTokenFile{
		Token:        tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		TokenURI:     cfg.Endpoint.TokenURL,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
	}
	if !tok.Expiry.IsZero() {
		ptf.Expiry = tok.Expiry.UTC().Format(time.RFC3339)
	}

	data, err := json.MarshalIndent(ptf, "", "  ")
	if err != nil {
		core.Logger().Debug("calendar: failed to marshal token for write-back", "error", err)
		return
	}
	if err := os.WriteFile(tokenPath, data, 0600); err != nil {
		core.Logger().Debug("calendar: failed to write-back token", "error", err)
	}
}

// ensureClient initializes the OAuth client and Calendar service on first use.
// Subsequent calls reuse the cached client (oauth2 handles token refresh internally).
func (p *CalendarProducer) ensureClient(ctx context.Context, tokenPath string) error {
	if p.svc != nil {
		return nil
	}

	client, tok, cfg, err := loadCalendarOAuthClient(ctx, tokenPath)
	if err != nil {
		return err
	}

	opts := []option.ClientOption{option.WithHTTPClient(client)}
	if p.baseURL != "" {
		opts = append(opts, option.WithEndpoint(p.baseURL))
	}
	svc, err := calendar.NewService(ctx, opts...)
	if err != nil {
		return err
	}

	p.client = client
	p.oauthCfg = cfg
	p.origTok = tok
	p.svc = svc
	return nil
}

func (p *CalendarProducer) Produce(ctx context.Context) (interface{}, error) {
	p.mu.Lock()
	cfg := p.feedsCfg.Calendar
	p.mu.Unlock()

	// Add timeout to prevent blocking the poll cycle.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Resolve token path.
	tokenPath := cfg.TokenPath
	if tokenPath == "" {
		tokenPath = core.DefaultCalendarTokenPath
	}

	lookahead := cfg.LookaheadMinutes
	if lookahead <= 0 {
		lookahead = defaultCalendarLookaheadMinutes
	}

	// Initialize OAuth client and service on first call; reuse thereafter.
	if err := p.ensureClient(ctx, tokenPath); err != nil {
		core.Logger().Debug("calendar: token load failed, returning empty", "error", err)
		return p.fallbackResult("no token"), nil
	}

	now := time.Now().UTC()
	timeMin := now.Format(time.RFC3339)
	timeMax := now.Add(time.Duration(lookahead) * time.Minute).Format(time.RFC3339)

	// Use configured calendars or default to "primary".
	calendarID := "primary"
	if len(cfg.Calendars) > 0 {
		calendarID = cfg.Calendars[0]
	}

	eventsCall := p.svc.Events.List(calendarID).
		TimeMin(timeMin).
		TimeMax(timeMax).
		SingleEvents(true).
		OrderBy("startTime").
		MaxResults(20)

	events, err := eventsCall.Do()
	if err != nil {
		core.Logger().Warn("calendar: API request failed", "error", err)
		return p.fallbackResult("api error"), nil
	}

	// Write back token if it was refreshed.
	// oauth2 transport refreshes lazily; after a successful API call we can
	// check if the token changed by requesting it from the transport.
	if ts, ok := p.client.Transport.(*oauth2.Transport); ok {
		newTok, err := ts.Source.Token()
		if err == nil && newTok.AccessToken != p.origTok.AccessToken {
			writeBackToken(tokenPath, newTok, p.oauthCfg)
			p.origTok = newTok
		}
	}

	// Parse events into our envelope format.
	var eventList []interface{}
	var nextEvent map[string]interface{}

	for _, item := range events.Items {
		start, allDay := parseGoogleEventTime(item.Start)
		end, _ := parseGoogleEventTime(item.End)

		isCurrent := !start.IsZero() && !end.IsZero() && !now.Before(start) && now.Before(end)

		ev := map[string]interface{}{
			"name":       item.Summary,
			"start":      formatEventTime(start, allDay),
			"end":        formatEventTime(end, allDay),
			"all_day":    allDay,
			"is_current": isCurrent,
		}
		eventList = append(eventList, ev)

		// First upcoming non-current event becomes next_event.
		// Store absolute start time — relative time is computed at render time
		// so the display stays accurate regardless of cache age.
		if nextEvent == nil && !isCurrent && !start.IsZero() && start.After(now) {
			nextEvent = map[string]interface{}{
				"name":  item.Summary,
				"start": formatEventTime(start, allDay),
			}
		}
	}

	if eventList == nil {
		eventList = []interface{}{}
	}

	result := NewEnvelopeAt("calendar", map[string]interface{}{
		"events":     eventList,
		"next_event": nextEvent,
	}, now)

	p.mu.Lock()
	p.lastResult = result
	p.mu.Unlock()

	return result, nil
}

// fallbackResult returns cached data or an empty envelope (ARCH-1: fail-open).
func (p *CalendarProducer) fallbackResult(reason string) map[string]interface{} {
	p.mu.Lock()
	last := p.lastResult
	p.mu.Unlock()

	if last != nil {
		core.Logger().Debug("calendar: using cached fallback", "reason", reason)
		return last
	}

	return NewEnvelope("calendar", map[string]interface{}{
		"events":     []interface{}{},
		"next_event": nil,
	})
}

// parseGoogleEventTime extracts a time.Time from a Google Calendar EventDateTime.
// Returns the parsed time and whether it's an all-day event (Date vs DateTime).
func parseGoogleEventTime(edt *calendar.EventDateTime) (time.Time, bool) {
	if edt == nil {
		return time.Time{}, false
	}
	if edt.DateTime != "" {
		t, err := time.Parse(time.RFC3339, edt.DateTime)
		if err != nil {
			return time.Time{}, false
		}
		return t, false
	}
	if edt.Date != "" {
		t, err := time.Parse("2006-01-02", edt.Date)
		if err != nil {
			return time.Time{}, false // Parse failed, don't claim it's all-day
		}
		return t, true
	}
	return time.Time{}, false
}

// formatEventTime formats a time for the event list.
func formatEventTime(t time.Time, allDay bool) string {
	if t.IsZero() {
		return ""
	}
	if allDay {
		return t.Format("2006-01-02")
	}
	return t.Format(time.RFC3339)
}

// relativeTimeString returns a human-friendly relative time like "in 15m", "in 2h", "tomorrow 3pm".
func relativeTimeString(now, eventStart time.Time) string {
	diff := eventStart.Sub(now)
	if diff <= 0 || diff < time.Minute {
		return "now"
	}

	totalMinutes := int(diff.Minutes())
	if totalMinutes < 60 {
		return fmt.Sprintf("in %dm", totalMinutes)
	}

	// Check calendar-day boundary before raw hours, so "tomorrow 3pm" beats "in 17h".
	nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	eventDate := time.Date(eventStart.Year(), eventStart.Month(), eventStart.Day(), 0, 0, 0, 0, eventStart.Location())
	dayDiff := int(eventDate.Sub(nowDate).Hours() / 24)

	if dayDiff == 0 {
		// Same calendar day — use hours/minutes format.
		hours := totalMinutes / 60
		minutes := totalMinutes % 60
		if minutes < 5 {
			return fmt.Sprintf("in %dh", hours)
		}
		return fmt.Sprintf("in %dh %dm", hours, minutes)
	}

	timeLabel := strings.ToLower(eventStart.Format("3:04pm"))
	if dayDiff == 1 {
		return "tomorrow " + timeLabel
	}

	return eventStart.Format("Mon") + " " + timeLabel
}

// CalendarTestFixture returns a deterministic calendar envelope for use in
// cross-package tests. Shared fixture so tests bind to actual field names.
func CalendarTestFixture() map[string]interface{} {
	return NewEnvelopeAt("calendar", map[string]interface{}{
		"events": []interface{}{
			map[string]interface{}{
				"name":       "Standup",
				"start":      "2026-03-07T10:30:00Z",
				"end":        "2026-03-07T11:00:00Z",
				"all_day":    false,
				"is_current": false,
			},
		},
		"next_event": map[string]interface{}{
			"name":  "Standup",
			"start": "2026-03-07T10:30:00Z",
		},
	}, time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC))
}
