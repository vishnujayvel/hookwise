package transcript_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/pricing"
	"github.com/vishnujayvel/hookwise/internal/transcript"
)

// testdataPath returns the absolute path to a fixture file.
func testdataPath(name string) string {
	return filepath.Join("testdata", name)
}

// assistantLine builds a minimal assistant JSONL line as a string.
func assistantLine(model string, input, output, cacheRead, cacheWrite int64) string {
	type usageJSON struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	}
	type msgJSON struct {
		ID    string    `json:"id"`
		Model string    `json:"model"`
		Role  string    `json:"role"`
		Usage usageJSON `json:"usage"`
	}
	type lineJSON struct {
		Type      string  `json:"type"`
		Message   msgJSON `json:"message"`
		Timestamp string  `json:"timestamp"`
	}
	line := lineJSON{
		Type: "assistant",
		Message: msgJSON{
			ID:    "msg_test",
			Model: model,
			Role:  "assistant",
			Usage: usageJSON{
				InputTokens:              input,
				OutputTokens:             output,
				CacheReadInputTokens:     cacheRead,
				CacheCreationInputTokens: cacheWrite,
			},
		},
		Timestamp: "2026-06-07T00:00:00Z",
	}
	b, _ := json.Marshal(line)
	return string(b)
}

// TestSingleAssistantMessage verifies a single assistant message is parsed into the correct per-model usage.
func TestSingleAssistantMessage(t *testing.T) {
	result, err := transcript.SumUsage(testdataPath("single.jsonl"))
	require.NoError(t, err)
	require.Len(t, result, 1)

	u, ok := result["claude-opus-4-8"]
	require.True(t, ok, "expected key 'claude-opus-4-8' in result")
	assert.Equal(t, int64(1234), u.InputTokens)
	assert.Equal(t, int64(567), u.OutputTokens)
	assert.Equal(t, int64(89), u.CacheReadTokens)
	assert.Equal(t, int64(10), u.CacheCreationTokens)
}

// TestMultipleMessagesSameModel verifies usage is accumulated when the same model appears multiple times.
func TestMultipleMessagesSameModel(t *testing.T) {
	// Use multi_model fixture but filter to just opus entries: 1000+500=1500 input, 200+100=300 output, etc.
	result, err := transcript.SumUsage(testdataPath("multi_model.jsonl"))
	require.NoError(t, err)

	u, ok := result["claude-opus-4-8"]
	require.True(t, ok, "expected key 'claude-opus-4-8'")
	assert.Equal(t, int64(1000+500), u.InputTokens)
	assert.Equal(t, int64(200+100), u.OutputTokens)
	assert.Equal(t, int64(50+20), u.CacheReadTokens)
	assert.Equal(t, int64(5+3), u.CacheCreationTokens)
}

// TestMultipleModels verifies that different models produce separate map entries each summed correctly.
func TestMultipleModels(t *testing.T) {
	result, err := transcript.SumUsage(testdataPath("multi_model.jsonl"))
	require.NoError(t, err)
	require.Len(t, result, 2, "expected exactly 2 model entries")

	opus, ok := result["claude-opus-4-8"]
	require.True(t, ok, "expected key 'claude-opus-4-8'")
	assert.Equal(t, int64(1500), opus.InputTokens)
	assert.Equal(t, int64(300), opus.OutputTokens)
	assert.Equal(t, int64(70), opus.CacheReadTokens)
	assert.Equal(t, int64(8), opus.CacheCreationTokens)

	sonnet, ok := result["claude-sonnet-4-6"]
	require.True(t, ok, "expected key 'claude-sonnet-4-6'")
	assert.Equal(t, int64(1000), sonnet.InputTokens)
	assert.Equal(t, int64(350), sonnet.OutputTokens)
	assert.Equal(t, int64(10), sonnet.CacheReadTokens)
	assert.Equal(t, int64(1), sonnet.CacheCreationTokens)
}

// TestNonAssistantLinesIgnored verifies user/system/summary lines with no usage are skipped.
func TestNonAssistantLinesIgnored(t *testing.T) {
	result, err := transcript.SumUsage(testdataPath("non_assistant_only.jsonl"))
	require.NoError(t, err)
	assert.Empty(t, result)
}

// TestMalformedLinesSkipped verifies garbled/non-JSON lines are skipped and valid lines still counted.
func TestMalformedLinesSkipped(t *testing.T) {
	result, err := transcript.SumUsage(testdataPath("with_malformed_lines.jsonl"))
	require.NoError(t, err)
	require.Len(t, result, 1, "expected exactly 1 model")

	u, ok := result["claude-haiku-3-5"]
	require.True(t, ok)
	// 100+200=300, 50+75=125, 5+10=15, 2+3=5
	assert.Equal(t, int64(300), u.InputTokens)
	assert.Equal(t, int64(125), u.OutputTokens)
	assert.Equal(t, int64(15), u.CacheReadTokens)
	assert.Equal(t, int64(5), u.CacheCreationTokens)
}

// TestAssistantLineMissingUsageSkipped verifies that assistant lines without a usage block are skipped without panic.
func TestAssistantLineMissingUsageSkipped(t *testing.T) {
	result, err := transcript.SumUsage(testdataPath("missing_usage.jsonl"))
	require.NoError(t, err)
	// Only the second message (with usage) should be counted.
	u, ok := result["claude-opus-4-8"]
	require.True(t, ok)
	assert.Equal(t, int64(500), u.InputTokens)
	assert.Equal(t, int64(100), u.OutputTokens)
}

// TestMissingFile verifies that a missing file path returns an error.
func TestMissingFile(t *testing.T) {
	_, err := transcript.SumUsage(testdataPath("does_not_exist.jsonl"))
	assert.Error(t, err)
}

// TestEmptyFile verifies an empty file returns an empty map and nil error.
func TestEmptyFile(t *testing.T) {
	result, err := transcript.SumUsage(testdataPath("empty.jsonl"))
	require.NoError(t, err)
	assert.Empty(t, result)
}

// TestVeryLongLine verifies that a line exceeding the default 64KB Scanner buffer is parsed correctly.
// The large token count is embedded in a JSON object whose total serialized size exceeds 64KB.
func TestVeryLongLine(t *testing.T) {
	// Build a line that exceeds 64 KB by stuffing a large dummy field into the message.
	// We embed a "junk" field with a 100KB string value to force a line > 64KB.
	pad := strings.Repeat("x", 100*1024) // 100 KB of padding

	type usageJSON struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	}
	type msgJSON struct {
		ID    string    `json:"id"`
		Model string    `json:"model"`
		Role  string    `json:"role"`
		Usage usageJSON `json:"usage"`
		Junk  string    `json:"junk"` // large padding field
	}
	type lineJSON struct {
		Type      string  `json:"type"`
		Message   msgJSON `json:"message"`
		Timestamp string  `json:"timestamp"`
	}

	line := lineJSON{
		Type: "assistant",
		Message: msgJSON{
			ID:    "msg_large",
			Model: "claude-opus-4-8",
			Role:  "assistant",
			Usage: usageJSON{
				InputTokens:              42,
				OutputTokens:             7,
				CacheReadInputTokens:     3,
				CacheCreationInputTokens: 1,
			},
			Junk: pad,
		},
		Timestamp: "2026-06-07T00:00:00Z",
	}
	b, err := json.Marshal(line)
	require.NoError(t, err)
	require.Greater(t, len(b), 64*1024, "test fixture must exceed 64KB")

	// Write to a temp file
	f, err := os.CreateTemp(t.TempDir(), "large_line_*.jsonl")
	require.NoError(t, err)
	_, err = fmt.Fprintf(f, "%s\n", b)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	result, err := transcript.SumUsage(f.Name())
	require.NoError(t, err)

	u, ok := result["claude-opus-4-8"]
	require.True(t, ok, "expected key 'claude-opus-4-8' for large line")
	assert.Equal(t, int64(42), u.InputTokens)
	assert.Equal(t, int64(7), u.OutputTokens)
	assert.Equal(t, int64(3), u.CacheReadTokens)
	assert.Equal(t, int64(1), u.CacheCreationTokens)
}

// TestLineOverBufferLimitNotFatal verifies that a single line exceeding the scanner's
// buffer cap does NOT abort the whole scan. The two valid assistant lines before and
// after the giant junk line must both be counted, and the call must return nil error.
func TestLineOverBufferLimitNotFatal(t *testing.T) {
	line1 := assistantLine("claude-opus-4-8", 10, 2, 0, 0)
	giantJunk := strings.Repeat("x", 11*1024*1024) // 11 MiB of non-JSON junk, exceeds old 10 MiB cap
	line3 := assistantLine("claude-sonnet-4-6", 20, 4, 0, 0)

	f, err := os.CreateTemp(t.TempDir(), "overlimit_*.jsonl")
	require.NoError(t, err)
	_, err = fmt.Fprintf(f, "%s\n%s\n%s\n", line1, giantJunk, line3)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	result, err := transcript.SumUsage(f.Name())
	require.NoError(t, err, "giant junk line must not abort the scan")

	opus, ok := result["claude-opus-4-8"]
	require.True(t, ok, "expected 'claude-opus-4-8' key")
	assert.Equal(t, int64(10), opus.InputTokens)
	assert.Equal(t, int64(2), opus.OutputTokens)
	assert.Equal(t, int64(0), opus.CacheReadTokens)
	assert.Equal(t, int64(0), opus.CacheCreationTokens)

	sonnet, ok := result["claude-sonnet-4-6"]
	require.True(t, ok, "expected 'claude-sonnet-4-6' key")
	assert.Equal(t, int64(20), sonnet.InputTokens)
	assert.Equal(t, int64(4), sonnet.OutputTokens)
	assert.Equal(t, int64(0), sonnet.CacheReadTokens)
	assert.Equal(t, int64(0), sonnet.CacheCreationTokens)
}

// TestResultTypeIsPricingUsage is a compile-time assertion that SumUsage returns map[string]pricing.Usage.
// If the return type ever changes, this assignment will fail to compile.
func TestResultTypeIsPricingUsage(t *testing.T) {
	result, err := transcript.SumUsage(testdataPath("single.jsonl"))
	require.NoError(t, err)
	//nolint:staticcheck // QF1011: the explicit type is the compile-time assertion under test.
	var _ map[string]pricing.Usage = result
	_ = result
}
