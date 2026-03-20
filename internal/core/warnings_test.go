package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWarningCollector_AddAndCount(t *testing.T) {
	wc := NewWarningCollector()
	assert.Equal(t, 0, wc.Count())

	wc.Add("config", "missing field")
	assert.Equal(t, 1, wc.Count())

	wc.Add("dispatch", "panic: nil pointer")
	assert.Equal(t, 2, wc.Count())

	warnings := wc.Warnings()
	require.Len(t, warnings, 2)
	assert.Equal(t, "config", warnings[0].Source)
	assert.Equal(t, "missing field", warnings[0].Message)
	assert.Equal(t, "dispatch", warnings[1].Source)
	assert.Equal(t, "panic: nil pointer", warnings[1].Message)

	// Verify timestamps are valid RFC3339
	for _, w := range warnings {
		_, err := time.Parse(time.RFC3339, w.Timestamp)
		assert.NoError(t, err, "timestamp should be valid RFC3339")
	}
}

func TestWarningCollector_ConcurrentAdd(t *testing.T) {
	wc := NewWarningCollector()
	const goroutines = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			wc.Add("goroutine", "warning from goroutine")
		}(i)
	}
	wg.Wait()

	assert.Equal(t, goroutines, wc.Count())
	warnings := wc.Warnings()
	assert.Len(t, warnings, goroutines)
}

func TestWarningCollector_FlushCreatesJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", tmpDir)

	wc := NewWarningCollector()
	wc.Add("config", "bad yaml")
	wc.Add("dispatch", "timeout exceeded")

	err := wc.Flush()
	require.NoError(t, err)

	// Verify the file was created
	filePath := filepath.Join(tmpDir, "state", "warnings.json")
	data, err := os.ReadFile(filePath)
	require.NoError(t, err)

	var warnings []Warning
	err = json.Unmarshal(data, &warnings)
	require.NoError(t, err)
	assert.Len(t, warnings, 2)
	assert.Equal(t, "config", warnings[0].Source)
	assert.Equal(t, "bad yaml", warnings[0].Message)
	assert.Equal(t, "dispatch", warnings[1].Source)
	assert.Equal(t, "timeout exceeded", warnings[1].Message)
}

func TestWarningCollector_ReadWarnings_TTLFilter(t *testing.T) {
	tmpDir := t.TempDir()

	// Create warnings with mixed timestamps
	now := time.Now().UTC()
	old := now.Add(-time.Duration(DefaultWarningTTL+60) * time.Second) // expired
	recent := now.Add(-10 * time.Second)                                // still valid

	warnings := []Warning{
		{Source: "old", Message: "expired warning", Timestamp: old.Format(time.RFC3339)},
		{Source: "recent", Message: "fresh warning", Timestamp: recent.Format(time.RFC3339)},
	}

	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o700))

	data, err := json.Marshal(warnings)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "warnings.json"), data, 0o600))

	result := ReadWarnings(tmpDir)
	require.Len(t, result, 1)
	assert.Equal(t, "recent", result[0].Source)
	assert.Equal(t, "fresh warning", result[0].Message)
}

func TestWarningCollector_ReadWarnings_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()

	// No file exists → empty slice
	result := ReadWarnings(tmpDir)
	assert.Empty(t, result)
	assert.NotNil(t, result, "should return empty slice, not nil")
}

func TestWarningCollector_WarningsReturnsCopy(t *testing.T) {
	wc := NewWarningCollector()
	wc.Add("test", "original")

	warnings := wc.Warnings()
	warnings[0].Message = "mutated"

	// Original should be unchanged
	assert.Equal(t, "original", wc.Warnings()[0].Message)
}

func TestFormatWarningAge(t *testing.T) {
	tests := []struct {
		name     string
		ts       time.Time
		expected string
	}{
		{"seconds", time.Now().Add(-30 * time.Second), "30s"},
		{"minutes", time.Now().Add(-5 * time.Minute), "5m"},
		{"hours", time.Now().Add(-2 * time.Hour), "2h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatWarningAge(tt.ts.Format(time.RFC3339))
			assert.Equal(t, tt.expected, result)
		})
	}

	// Invalid timestamp
	assert.Equal(t, "unknown", FormatWarningAge("not-a-time"))
}
