package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// ---------------------------------------------------------------------------
// Built-in feed producers (stub implementations).
// Each returns placeholder data; actual API/data-fetching logic is deferred.
// ---------------------------------------------------------------------------

// PulseProducer returns session count and recent activity summary.
type PulseProducer struct{}

func (p *PulseProducer) Name() string { return "pulse" }
func (p *PulseProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      "pulse",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"session_count":   0,
			"active_sessions": 0,
			"recent_activity": "No recent activity (placeholder)",
			"source":          "placeholder",
		},
	}, nil
}

// ProjectProducer returns project directory info by running git commands.
// It implements ConfigAware to receive feed configuration.
type ProjectProducer struct {
	mu       sync.Mutex
	feedsCfg core.FeedsConfig
}

func (p *ProjectProducer) Name() string { return "project" }

// SetFeedsConfig receives the feed configuration (ConfigAware interface).
func (p *ProjectProducer) SetFeedsConfig(cfg core.FeedsConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.feedsCfg = cfg
}

func (p *ProjectProducer) Produce(ctx context.Context) (interface{}, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return p.fallbackResult()
	}

	// Detect git repo root.
	repoRoot, err := p.runGit(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		// Not a git repo or git not installed — fail-open (ARCH-1).
		return p.fallbackResult()
	}

	repoName := filepath.Base(repoRoot)

	// Get current branch.
	branchName, err := p.runGit(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		branchName = ""
	}

	// Get short commit hash.
	commitHash, err := p.runGit(ctx, cwd, "rev-parse", "--short", "HEAD")
	if err != nil {
		commitHash = ""
	}

	// Detect dirty state.
	porcelain, err := p.runGit(ctx, cwd, "status", "--porcelain")
	isDirty := err == nil && porcelain != ""

	return map[string]interface{}{
		"type":      "project",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"name":        repoName,
			"branch":      branchName,
			"last_commit": commitHash,
			"dirty":       isDirty,
		},
	}, nil
}

// runGit executes a git command in the given directory and returns trimmed stdout.
func (p *ProjectProducer) runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// fallbackResult returns a valid envelope with empty fields when git is unavailable (ARCH-1: fail-open).
func (p *ProjectProducer) fallbackResult() (map[string]interface{}, error) {
	return map[string]interface{}{
		"type":      "project",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"name":        "",
			"branch":      "",
			"last_commit": "",
			"dirty":       false,
		},
	}, nil
}

// ProjectTestFixture returns a deterministic project envelope for use in
// cross-package tests. Shared fixture so tests bind to actual field names.
func ProjectTestFixture() map[string]interface{} {
	return map[string]interface{}{
		"type":      "project",
		"timestamp": "2026-03-07T10:00:00Z",
		"data": map[string]interface{}{
			"name":        "hookwise",
			"branch":      "main",
			"last_commit": "abc1234",
			"dirty":       false,
		},
	}
}

// NewsProducer returns placeholder news items.
type NewsProducer struct{}

func (p *NewsProducer) Name() string { return "news" }
func (p *NewsProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      "news",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"stories": []interface{}{
				map[string]interface{}{
					"title": "Placeholder story",
					"url":   "https://example.com",
					"score": 0,
				},
			},
			"source": "placeholder",
		},
	}, nil
}

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

	result := map[string]interface{}{
		"type":      "calendar",
		"timestamp": now.Format(time.RFC3339),
		"data": map[string]interface{}{
			"events":     eventList,
			"next_event": nextEvent,
		},
	}

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

	return map[string]interface{}{
		"type":      "calendar",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"events":     []interface{}{},
			"next_event": nil,
		},
	}
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
	return map[string]interface{}{
		"type":      "calendar",
		"timestamp": "2026-03-07T10:00:00Z",
		"data": map[string]interface{}{
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
		},
	}
}

// WeatherProducer fetches live weather data from the Open-Meteo API.
// It implements ConfigAware to receive latitude/longitude from feed config.
type WeatherProducer struct {
	mu         sync.Mutex
	feedsCfg   core.FeedsConfig
	lastResult map[string]interface{} // cached last successful result for fallback
	client     *http.Client
}

func (p *WeatherProducer) Name() string { return "weather" }

// SetFeedsConfig receives the feed configuration (ConfigAware interface).
func (p *WeatherProducer) SetFeedsConfig(cfg core.FeedsConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.feedsCfg = cfg
}

// openMeteoResponse is the JSON structure returned by the Open-Meteo API.
type openMeteoResponse struct {
	Current struct {
		Temperature2m float64 `json:"temperature_2m"`
		WindSpeed10m  float64 `json:"wind_speed_10m"`
		WeatherCode   int     `json:"weather_code"`
	} `json:"current"`
}

// wmoCodeToEmoji maps WMO weather codes to emoji strings.
func wmoCodeToEmoji(code int) string {
	switch {
	case code == 0:
		return "\u2600\ufe0f" // sunny ☀️
	case code >= 1 && code <= 3:
		return "\u26c5" // partly cloudy ⛅
	case code >= 45 && code <= 48:
		return "\U0001f32b\ufe0f" // fog 🌫️
	case code >= 51 && code <= 67:
		return "\U0001f327\ufe0f" // rain 🌧️
	case code >= 71 && code <= 77:
		return "\u2744\ufe0f" // snow ❄️
	case code >= 80 && code <= 82:
		return "\U0001f326\ufe0f" // sun behind rain cloud 🌦️
	case code >= 95 && code <= 99:
		return "\u26c8\ufe0f" // thunderstorm ⛈️
	default:
		return "\U0001f324\ufe0f" // sun behind small cloud 🌤️
	}
}

// wmoCodeToDescription maps WMO weather codes to human-readable descriptions.
func wmoCodeToDescription(code int) string {
	switch {
	case code == 0:
		return "Clear"
	case code >= 1 && code <= 3:
		return "Partly Cloudy"
	case code >= 45 && code <= 48:
		return "Fog"
	case code >= 51 && code <= 55:
		return "Drizzle"
	case code >= 56 && code <= 57:
		return "Freezing Drizzle"
	case code >= 61 && code <= 65:
		return "Rain"
	case code >= 66 && code <= 67:
		return "Freezing Rain"
	case code >= 71 && code <= 75:
		return "Snow"
	case code >= 77 && code <= 77:
		return "Snow Grains"
	case code >= 80 && code <= 82:
		return "Showers"
	case code >= 95 && code <= 99:
		return "Thunderstorm"
	default:
		return "Unknown"
	}
}

func (p *WeatherProducer) Produce(ctx context.Context) (interface{}, error) {
	p.mu.Lock()
	cfg := p.feedsCfg.Weather
	p.mu.Unlock()

	// Default coordinates (San Francisco) if not configured.
	lat := cfg.Latitude
	lon := cfg.Longitude
	if lat == 0 && lon == 0 {
		lat = 37.7749
		lon = -122.4194
	}

	tempUnit := "fahrenheit"
	if cfg.TemperatureUnit == "celsius" {
		tempUnit = "celsius"
	}

	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,wind_speed_10m,weather_code&temperature_unit=%s",
		lat, lon, tempUnit,
	)

	if p.client == nil {
		p.client = &http.Client{Timeout: 5 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return p.fallbackResult("request error"), nil
	}

	resp, err := p.client.Do(req)
	if err != nil {
		core.Logger().Warn("weather: API request failed, using cached data", "error", err)
		return p.fallbackResult("api error"), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		core.Logger().Warn("weather: API returned non-200", "status", resp.StatusCode)
		return p.fallbackResult("bad status"), nil
	}

	var apiResp openMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		core.Logger().Warn("weather: failed to decode API response", "error", err)
		return p.fallbackResult("decode error"), nil
	}

	code := apiResp.Current.WeatherCode

	result := map[string]interface{}{
		"type":      "weather",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"temperature":     apiResp.Current.Temperature2m,
			"temperatureUnit": tempUnit,
			"windSpeed":       apiResp.Current.WindSpeed10m,
			"weatherCode":     code,
			"emoji":           wmoCodeToEmoji(code),
			"description":     wmoCodeToDescription(code),
		},
	}

	// Cache for fallback on future failures.
	p.mu.Lock()
	p.lastResult = result
	p.mu.Unlock()

	return result, nil
}

// fallbackResult returns the last cached result if available, or a placeholder.
// When no cached data exists, temperature and weatherCode are nil so the Python
// TUI renders "--" instead of a plausible-looking 0°F.
func (p *WeatherProducer) fallbackResult(reason string) map[string]interface{} {
	p.mu.Lock()
	last := p.lastResult
	unit := "fahrenheit"
	if p.feedsCfg.Weather.TemperatureUnit == "celsius" {
		unit = "celsius"
	}
	p.mu.Unlock()

	if last != nil {
		core.Logger().Debug("weather: using cached fallback", "reason", reason)
		return last
	}

	// No cached data — return placeholder with nil numerics so consumers
	// show "--" instead of interpreting 0 as a real reading.
	return map[string]interface{}{
		"type":      "weather",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"temperature":     nil,
			"temperatureUnit": unit,
			"windSpeed":       0,
			"weatherCode":     nil,
			"emoji":           "\U0001f324\ufe0f",
			"description":     "Unavailable",
		},
	}
}

// WeatherTestFixture returns a deterministic weather envelope for use in
// cross-package tests (e.g., bridge_test.go). This is a shared fixture so
// that bridge tests bind to the producer's actual field names rather than
// hardcoding keys that can silently drift.
func WeatherTestFixture() map[string]interface{} {
	return map[string]interface{}{
		"type":      "weather",
		"timestamp": "2026-03-07T10:00:00Z",
		"data": map[string]interface{}{
			"temperature":     float64(72),
			"temperatureUnit": "fahrenheit",
			"windSpeed":       float64(5.3),
			"weatherCode":     float64(0),
			"emoji":           "\u2600\ufe0f",
			"description":     "Clear",
		},
	}
}

// PracticeProducer returns practice session summary.
type PracticeProducer struct{}

func (p *PracticeProducer) Name() string { return "practice" }
func (p *PracticeProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      "practice",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"total_sessions":   0,
			"streak_days":      0,
			"last_practice_at": nil,
			"focus_area":       "none",
			"source":           "placeholder",
		},
	}, nil
}

// MemoriesProducer returns placeholder memories.
type MemoriesProducer struct{}

func (p *MemoriesProducer) Name() string { return "memories" }
func (p *MemoriesProducer) Produce(_ context.Context) (interface{}, error) {
	return map[string]interface{}{
		"type":      "memories",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"recent_memories": []interface{}{},
			"total_count":     0,
			"source":          "placeholder",
		},
	}, nil
}

// InsightsProducer aggregates Claude Code usage data from ~/.claude/usage-data/
// (session-meta + facets) and returns analytics metrics.
// It implements ConfigAware to receive staleness_days and usage_data_path from feed config.
type InsightsProducer struct {
	mu       sync.Mutex
	feedsCfg core.FeedsConfig
}

func (p *InsightsProducer) Name() string { return "insights" }

// SetFeedsConfig receives the feed configuration (ConfigAware interface).
func (p *InsightsProducer) SetFeedsConfig(cfg core.FeedsConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.feedsCfg = cfg
}

func (p *InsightsProducer) Produce(_ context.Context) (interface{}, error) {
	p.mu.Lock()
	cfg := p.feedsCfg.Insights
	p.mu.Unlock()

	// Resolve usage data path.
	basePath := cfg.UsageDataPath
	if basePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			// ARCH-1: fail-open, return zeroed envelope.
			return p.zeroedEnvelope(0), nil
		}
		basePath = filepath.Join(home, ".claude", "usage-data")
	}

	stalenessDays := cfg.StalenessDays
	if stalenessDays <= 0 {
		stalenessDays = 30
	}

	sessionMetaDir := filepath.Join(basePath, "session-meta")
	facetsDir := filepath.Join(basePath, "facets")

	now := time.Now().UTC()
	cutoff := now.Add(-time.Duration(stalenessDays) * 24 * time.Hour)

	// Read and filter sessions within staleness window.
	allSessions := readJSONFiles(sessionMetaDir)
	var validSessions []map[string]interface{}
	for _, s := range allSessions {
		startTime, ok := s["start_time"].(string)
		if !ok || startTime == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, startTime)
		if err != nil {
			// Try RFC3339Nano and other common ISO formats.
			ts, err = time.Parse("2006-01-02T15:04:05.999999999Z07:00", startTime)
			if err != nil {
				continue
			}
		}
		if !ts.Before(cutoff) {
			validSessions = append(validSessions, s)
		}
	}

	// ARCH-1: if no valid sessions, return zeroed fields (not error).
	if len(validSessions) == 0 {
		return p.zeroedEnvelope(stalenessDays), nil
	}

	// Build set of valid session IDs for facets matching.
	validIDs := make(map[string]struct{}, len(validSessions))
	for _, s := range validSessions {
		if id, ok := s["session_id"].(string); ok && id != "" {
			validIDs[id] = struct{}{}
		}
	}

	// Accumulators.
	var totalMessages, totalLines int
	var totalDuration float64
	toolCounts := make(map[string]int)
	hourCounts := make([]int, 24)
	activeDates := make(map[string]struct{})

	// Track the most recent session.
	var recentSession map[string]interface{}
	var recentStartTime time.Time

	_, localOffset := time.Now().Zone()

	for _, session := range validSessions {
		totalMessages += toInt(session["user_message_count"])
		totalLines += toInt(session["lines_added"])
		totalDuration += toFloat(session["duration_minutes"])

		// Tool counts.
		if tools, ok := session["tool_counts"].(map[string]interface{}); ok {
			for name, count := range tools {
				if v := toInt(count); v > 0 {
					toolCounts[name] += v
				}
			}
		}

		// Message hours for peak hour calculation.
		if hours, ok := session["message_hours"].([]interface{}); ok {
			for _, h := range hours {
				if hi := toInt(h); hi >= 0 && hi < 24 {
					hourCounts[hi]++
				}
			}
		}

		// Days active — unique local dates.
		if startTime, ok := session["start_time"].(string); ok && len(startTime) >= 10 {
			ts, err := time.Parse(time.RFC3339, startTime)
			if err != nil {
				ts, err = time.Parse("2006-01-02T15:04:05.999999999Z07:00", startTime)
			}
			if err == nil {
				localTime := ts.In(time.Now().Location())
				dateStr := localTime.Format("2006-01-02")
				activeDates[dateStr] = struct{}{}

				// Track most recent session.
				if ts.After(recentStartTime) {
					recentStartTime = ts
					recentSession = session
				}
			}
		}
	}

	// Read facets for friction data.
	frictionCounts := make(map[string]int)
	allFacets := readJSONFiles(facetsDir)
	var recentFrictionCount int
	var recentOutcome string

	for _, facet := range allFacets {
		sid, ok := facet["session_id"].(string)
		if !ok || sid == "" {
			continue
		}
		if _, valid := validIDs[sid]; !valid {
			continue
		}

		if friction, ok := facet["friction_counts"].(map[string]interface{}); ok {
			for cat, count := range friction {
				if v := toInt(count); v > 0 {
					frictionCounts[cat] += v
				}
			}

			// Track friction for the most recent session.
			if recentSession != nil && sid == recentSession["session_id"] {
				for _, count := range friction {
					recentFrictionCount += toInt(count)
				}
			}
		}

		if recentSession != nil && sid == recentSession["session_id"] {
			if outcome, ok := facet["outcome"].(string); ok {
				recentOutcome = outcome
			}
		}
	}

	// Derived metrics.
	avgDuration := totalDuration / float64(len(validSessions))
	avgDuration = math.Round(avgDuration*10) / 10

	// Top 10 tools sorted by count descending.
	type toolEntry struct {
		Name  string
		Count int
	}
	var toolList []toolEntry
	for name, count := range toolCounts {
		toolList = append(toolList, toolEntry{Name: name, Count: count})
	}
	sort.Slice(toolList, func(i, j int) bool {
		if toolList[i].Count != toolList[j].Count {
			return toolList[i].Count > toolList[j].Count
		}
		return toolList[i].Name < toolList[j].Name
	})
	if len(toolList) > 10 {
		toolList = toolList[:10]
	}
	topTools := make([]map[string]interface{}, 0, len(toolList))
	for _, t := range toolList {
		topTools = append(topTools, map[string]interface{}{
			"name":  t.Name,
			"count": t.Count,
		})
	}

	// Peak hour: convert UTC peak hour to local timezone.
	peakHourUTC := 0
	maxHourCount := 0
	for h := 0; h < 24; h++ {
		if hourCounts[h] > maxHourCount {
			maxHourCount = hourCounts[h]
			peakHourUTC = h
		}
	}
	localOffsetHours := localOffset / 3600
	peakHour := ((peakHourUTC + localOffsetHours) % 24 + 24) % 24

	// Friction total.
	frictionTotal := 0
	for _, count := range frictionCounts {
		frictionTotal += count
	}

	// Convert frictionCounts to interface map for JSON.
	frictionCountsOut := make(map[string]interface{}, len(frictionCounts))
	for k, v := range frictionCounts {
		frictionCountsOut[k] = v
	}

	// Recent session metrics (last 7 days).
	recentCutoff := now.Add(-7 * 24 * time.Hour)
	var recentMessages int
	recentActiveDates := make(map[string]struct{})
	for _, s := range validSessions {
		st, ok := s["start_time"].(string)
		if !ok {
			continue
		}
		ts, err := time.Parse(time.RFC3339, st)
		if err != nil {
			ts, err = time.Parse("2006-01-02T15:04:05.999999999Z07:00", st)
			if err != nil {
				continue
			}
		}
		if ts.Before(recentCutoff) {
			continue
		}
		recentMessages += toInt(s["user_message_count"])
		localTime := ts.In(time.Now().Location())
		dateStr := localTime.Format("2006-01-02")
		recentActiveDates[dateStr] = struct{}{}
	}
	recentDaysActive := len(recentActiveDates)
	var recentMsgsPerDay int
	if recentDaysActive > 0 {
		recentMsgsPerDay = int(math.Round(float64(recentMessages) / float64(recentDaysActive)))
	}

	// Build recent_session sub-object.
	recentSessionData := map[string]interface{}{
		"id":               "",
		"duration_minutes": 0,
		"lines_added":      0,
		"friction_count":   recentFrictionCount,
		"outcome":          recentOutcome,
		"tool_errors":      0,
	}
	if recentSession != nil {
		if id, ok := recentSession["session_id"].(string); ok {
			recentSessionData["id"] = id
		}
		recentSessionData["duration_minutes"] = toFloat(recentSession["duration_minutes"])
		recentSessionData["lines_added"] = toInt(recentSession["lines_added"])
		recentSessionData["tool_errors"] = toInt(recentSession["tool_errors"])
	}

	return map[string]interface{}{
		"type":      "insights",
		"timestamp": now.Format(time.RFC3339),
		"data": map[string]interface{}{
			"total_sessions":       len(validSessions),
			"total_messages":       totalMessages,
			"total_lines_added":    totalLines,
			"avg_duration_minutes": avgDuration,
			"top_tools":           topTools,
			"friction_counts":     frictionCountsOut,
			"friction_total":      frictionTotal,
			"peak_hour":           peakHour,
			"days_active":         len(activeDates),
			"staleness_days":      stalenessDays,
			"recent_msgs_per_day":  recentMsgsPerDay,
			"recent_messages":      recentMessages,
			"recent_days_active":   recentDaysActive,
			"recent_session":       recentSessionData,
		},
	}, nil
}

// zeroedEnvelope returns an insights envelope with all fields zeroed.
// Used when no data is available (ARCH-1: fail-open).
func (p *InsightsProducer) zeroedEnvelope(stalenessDays int) map[string]interface{} {
	return map[string]interface{}{
		"type":      "insights",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]interface{}{
			"total_sessions":       0,
			"total_messages":       0,
			"total_lines_added":    0,
			"avg_duration_minutes": float64(0),
			"top_tools":           []map[string]interface{}{},
			"friction_counts":     map[string]interface{}{},
			"friction_total":      0,
			"peak_hour":           0,
			"days_active":         0,
			"staleness_days":      stalenessDays,
			"recent_msgs_per_day":  0,
			"recent_messages":      0,
			"recent_days_active":   0,
			"recent_session": map[string]interface{}{
				"id":               "",
				"duration_minutes": 0,
				"lines_added":      0,
				"friction_count":   0,
				"outcome":          "",
				"tool_errors":      0,
			},
		},
	}
}

// readJSONFiles reads all .json files in a directory and returns their parsed
// contents. Malformed files and non-object JSON are silently skipped (fail-open).
// Returns an empty slice if the directory does not exist.
func readJSONFiles(dirPath string) []map[string]interface{} {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}

	var results []map[string]interface{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dirPath, entry.Name()))
		if err != nil {
			continue
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			continue
		}
		results = append(results, parsed)
	}
	return results
}

// toInt defensively converts an interface{} to int (supports float64 from JSON).
func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

// toFloat defensively converts an interface{} to float64.
func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// InsightsTestFixture returns a deterministic insights envelope for use in
// cross-package tests (e.g., bridge_test.go). This is a shared fixture so
// that bridge tests bind to the producer's actual field names rather than
// hardcoding keys that can silently drift.
func InsightsTestFixture() map[string]interface{} {
	return map[string]interface{}{
		"type":      "insights",
		"timestamp": "2026-03-07T10:00:00Z",
		"data": map[string]interface{}{
			"total_sessions":       5,
			"total_messages":       42,
			"total_lines_added":    320,
			"avg_duration_minutes": 15.3,
			"top_tools": []map[string]interface{}{
				{"name": "Read", "count": 25},
				{"name": "Edit", "count": 18},
			},
			"friction_counts":    map[string]interface{}{"permission_denied": 3},
			"friction_total":     3,
			"peak_hour":          14,
			"days_active":        4,
			"staleness_days":     30,
			"recent_msgs_per_day": 8,
			"recent_messages":     16,
			"recent_days_active":  2,
			"recent_session": map[string]interface{}{
				"id":               "session-001",
				"duration_minutes": float64(22),
				"lines_added":      80,
				"friction_count":   1,
				"outcome":          "success",
				"tool_errors":      0,
			},
		},
	}
}

// RegisterBuiltins registers all 8 built-in feed producers with the registry.
func RegisterBuiltins(r *Registry) {
	r.Register(&PulseProducer{})
	r.Register(&ProjectProducer{})
	r.Register(&NewsProducer{})
	r.Register(&CalendarProducer{})
	r.Register(&WeatherProducer{})
	r.Register(&PracticeProducer{})
	r.Register(&MemoriesProducer{})
	r.Register(&InsightsProducer{})
}
