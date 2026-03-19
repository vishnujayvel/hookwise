package feeds

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

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
		basePath = filepath.Join(core.HomeDir(), ".claude", "usage-data")
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
		ts, err := core.ParseTimeFlex(startTime)
		if err != nil {
			continue
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
			ts, err := core.ParseTimeFlex(startTime)
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
		ts, err := core.ParseTimeFlex(st)
		if err != nil {
			continue
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

	return NewEnvelopeAt("insights", map[string]interface{}{
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
	}, now), nil
}

// zeroedEnvelope returns an insights envelope with all fields zeroed.
// Used when no data is available (ARCH-1: fail-open).
func (p *InsightsProducer) zeroedEnvelope(stalenessDays int) map[string]interface{} {
	return NewEnvelope("insights", map[string]interface{}{
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
	})
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
	return NewEnvelopeAt("insights", map[string]interface{}{
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
	}, time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC))
}
