// Package bridge provides the JSON cache bridge between the Go daemon's
// per-feed cache files and the Python TUI's single merged cache file.
//
// The Go daemon (internal/feeds) writes one JSON file per feed producer
// (e.g. pulse.json, weather.json) into a cache directory. The Python TUI
// reads a single merged file at ~/.hookwise/state/status-line-cache.json.
//
// This package bridges those two worlds:
//   - CollectFeedCache reads all per-feed JSON files and merges them
//   - WriteTUICache writes the merged result to status-line-cache.json
//   - ValidateCacheFormat checks that entries conform to the expected schema
package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// CollectFeedCache reads all *.json files from cacheDir and merges them into
// a single map keyed by feed name (the filename stem, e.g. "pulse" from
// "pulse.json"). Each value is the parsed JSON content of that file.
//
// Files that cannot be read or contain invalid JSON are silently skipped.
// An empty or nonexistent directory returns an empty (non-nil) map and no error.
func CollectFeedCache(cacheDir string) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, fmt.Errorf("bridge: read cache dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}

		feedName := strings.TrimSuffix(name, ".json")
		filePath := filepath.Join(cacheDir, name)

		data, err := os.ReadFile(filePath)
		if err != nil {
			continue // skip unreadable files
		}

		var parsed interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			continue // skip invalid JSON
		}

		result[feedName] = parsed
	}

	return result, nil
}

// DefaultTTLSeconds is the default TTL for feed cache entries when the
// producer does not specify one.
const DefaultTTLSeconds = 300

// WriteTUICache collects all per-feed JSON files from cacheDir, flattens them
// into the Python TUI-compatible format, and writes the result to
// ~/.hookwise/state/status-line-cache.json using atomic file writes.
//
// The Python TUI expects each feed entry to be a flat object with
// "updated_at" and "ttl_seconds" at the top level, plus the feed-specific
// data fields spread at the top level (not nested under "data").
//
// This is the primary function called by the dispatch process to bridge
// the Go daemon's per-feed cache with the Python TUI (R9.1, R9.3).
func WriteTUICache(cacheDir string) error {
	merged, err := CollectFeedCache(cacheDir)
	if err != nil {
		return fmt.Errorf("bridge: collect feed cache: %w", err)
	}

	flattened := FlattenForTUI(merged)
	outPath := filepath.Join(core.GetStateDir(), "state", "status-line-cache.json")
	return core.AtomicWriteJSON(outPath, flattened)
}

// WriteTUICacheTo is like WriteTUICache but writes to a specified output path
// instead of the default. This variant is used in tests.
func WriteTUICacheTo(cacheDir, outPath string) error {
	merged, err := CollectFeedCache(cacheDir)
	if err != nil {
		return fmt.Errorf("bridge: collect feed cache: %w", err)
	}

	flattened := FlattenForTUI(merged)
	return core.AtomicWriteJSON(outPath, flattened)
}

// FlattenForTUI converts Go-envelope feed entries ({type, timestamp, data: {...}})
// into the flat format expected by the Python TUI ({...data fields, updated_at, ttl_seconds}).
//
// For each feed entry:
//   - Spreads the contents of "data" to the top level
//   - Renames "timestamp" to "updated_at"
//   - Adds "ttl_seconds" (default 300) if not present
//   - Preserves entries that are already in flat format
func FlattenForTUI(merged map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(merged))

	for key, value := range merged {
		entry, ok := value.(map[string]interface{})
		if !ok {
			result[key] = value
			continue
		}

		// Check if this is a Go-envelope format (has "data" sub-object).
		dataObj, hasData := entry["data"]
		timestamp, hasTimestamp := entry["timestamp"]

		if hasData && hasTimestamp {
			// Flatten: spread data fields to top level.
			flat := make(map[string]interface{})

			// First, spread the "data" fields.
			if dataMap, ok := dataObj.(map[string]interface{}); ok {
				for k, v := range dataMap {
					flat[k] = v
				}
			}

			// Add updated_at (renamed from timestamp).
			flat["updated_at"] = timestamp

			// Add ttl_seconds if not already present.
			if _, hasTTL := flat["ttl_seconds"]; !hasTTL {
				flat["ttl_seconds"] = DefaultTTLSeconds
			}

			result[key] = flat
		} else {
			// Already flat or unknown format — pass through.
			result[key] = value
		}
	}

	return result
}

// ValidateGoEnvelopeFormat checks that data conforms to the Go-internal envelope
// format ({type, timestamp, data}). Used to validate raw collector output before
// flattening.
func ValidateGoEnvelopeFormat(data map[string]interface{}) error {
	for key, value := range data {
		entry, ok := value.(map[string]interface{})
		if !ok {
			return fmt.Errorf("bridge: entry %q is not a JSON object", key)
		}

		if _, ok := entry["type"]; !ok {
			return fmt.Errorf("bridge: entry %q missing required field \"type\"", key)
		}
		if _, ok := entry["timestamp"]; !ok {
			return fmt.Errorf("bridge: entry %q missing required field \"timestamp\"", key)
		}
		if _, ok := entry["data"]; !ok {
			return fmt.Errorf("bridge: entry %q missing required field \"data\"", key)
		}

		if _, ok := entry["type"].(string); !ok {
			return fmt.Errorf("bridge: entry %q field \"type\" is not a string", key)
		}
		if _, ok := entry["timestamp"].(string); !ok {
			return fmt.Errorf("bridge: entry %q field \"timestamp\" is not a string", key)
		}
	}

	return nil
}

// ValidateCacheFormat checks that data conforms to the flat format expected by
// the Python TUI. Each top-level entry must be a JSON object containing at
// least "updated_at" (string) and "ttl_seconds" (number) fields.
//
// Returns nil if all entries are valid. Returns an error describing the first
// invalid entry found.
func ValidateCacheFormat(data map[string]interface{}) error {
	for key, value := range data {
		entry, ok := value.(map[string]interface{})
		if !ok {
			return fmt.Errorf("bridge: entry %q is not a JSON object", key)
		}

		if _, ok := entry["updated_at"]; !ok {
			return fmt.Errorf("bridge: entry %q missing required field \"updated_at\"", key)
		}
		if _, ok := entry["updated_at"].(string); !ok {
			return fmt.Errorf("bridge: entry %q field \"updated_at\" is not a string", key)
		}
		ttl, hasTTL := entry["ttl_seconds"]
		if !hasTTL {
			return fmt.Errorf("bridge: entry %q missing required field \"ttl_seconds\"", key)
		}
		switch ttl.(type) {
		case int, float64, json.Number:
			// valid numeric types
		default:
			return fmt.Errorf("bridge: entry %q field \"ttl_seconds\" is not a number (got %T)", key, ttl)
		}
	}

	return nil
}
