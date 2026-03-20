package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultWarningTTL is the number of seconds before a warning expires.
const DefaultWarningTTL = 300

// Warning represents a single warning captured during dispatch or config loading.
type Warning struct {
	Source    string `json:"source"`
	Message  string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// WarningCollector accumulates warnings in a goroutine-safe manner.
type WarningCollector struct {
	mu       sync.Mutex
	warnings []Warning
}

// NewWarningCollector creates a new empty WarningCollector.
func NewWarningCollector() *WarningCollector {
	return &WarningCollector{}
}

// Add appends a warning with the current RFC3339 timestamp.
// Safe for concurrent use.
func (wc *WarningCollector) Add(source, message string) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.warnings = append(wc.warnings, Warning{
		Source:    source,
		Message:   message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// Warnings returns a copy of the accumulated warnings.
func (wc *WarningCollector) Warnings() []Warning {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	out := make([]Warning, len(wc.warnings))
	copy(out, wc.warnings)
	return out
}

// Count returns the number of accumulated warnings.
func (wc *WarningCollector) Count() int {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	return len(wc.warnings)
}

// Flush writes all accumulated warnings to ~/.hookwise/state/warnings.json
// using AtomicWriteJSON. The state directory is resolved via GetStateDir().
func (wc *WarningCollector) Flush() error {
	wc.mu.Lock()
	warnings := make([]Warning, len(wc.warnings))
	copy(warnings, wc.warnings)
	wc.mu.Unlock()

	stateDir := filepath.Join(GetStateDir(), "state")
	filePath := filepath.Join(stateDir, "warnings.json")
	return AtomicWriteJSON(filePath, warnings)
}

// ReadWarnings reads warnings.json from the given state directory and returns
// unexpired warnings (those within DefaultWarningTTL seconds of now).
// Returns an empty slice if the file doesn't exist or can't be parsed.
func ReadWarnings(stateDir string) []Warning {
	filePath := filepath.Join(stateDir, "state", "warnings.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return []Warning{}
	}

	var warnings []Warning
	if err := json.Unmarshal(data, &warnings); err != nil {
		return []Warning{}
	}

	now := time.Now().UTC()
	var active []Warning
	for _, w := range warnings {
		ts, err := ParseTimeFlex(w.Timestamp)
		if err != nil {
			continue
		}
		age := now.Sub(ts)
		if age.Seconds() <= float64(DefaultWarningTTL) {
			active = append(active, w)
		}
	}

	if active == nil {
		return []Warning{}
	}
	return active
}

// warningsFilePath returns the path to warnings.json for a given state dir.
// Exported for testing convenience.
func warningsFilePath(stateDir string) string {
	return filepath.Join(stateDir, "state", "warnings.json")
}

// FormatWarningAge returns a human-readable age string for a warning timestamp.
func FormatWarningAge(timestamp string) string {
	ts, err := ParseTimeFlex(timestamp)
	if err != nil {
		return "unknown"
	}
	age := time.Since(ts).Truncate(time.Second)
	if age < time.Minute {
		return fmt.Sprintf("%ds", int(age.Seconds()))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	return fmt.Sprintf("%dh", int(age.Hours()))
}
