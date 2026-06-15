// Package transcript parses Claude Code .jsonl transcript files and aggregates
// token usage per model.
package transcript

import (
	"bufio"
	"encoding/json"
	"os"

	"github.com/vishnujayvel/hookwise/internal/pricing"
)

// transcriptLine is the top-level shape of each JSONL line.
// Only the fields we care about are decoded; everything else is silently ignored.
type transcriptLine struct {
	Type    string          `json:"type"`
	Message transcriptMsg   `json:"message"`
}

// transcriptMsg holds the assistant message fields we need.
type transcriptMsg struct {
	Model string         `json:"model"`
	Usage *transcriptUsage `json:"usage"` // pointer so we can detect absent usage (nil)
}

// transcriptUsage maps directly to Anthropic's reported usage block.
type transcriptUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// scannerBufSize is the initial Scanner buffer size (10 MiB).
// Transcript lines can easily exceed the default 64 KiB limit when assistant
// messages embed large tool-result payloads.
const scannerBufSize = 10 * 1024 * 1024 // 10 MiB

// SumUsage opens the .jsonl transcript at path, scans it line by line, and
// returns a map from model ID to the total token usage accumulated across all
// assistant messages that model produced.
//
// Parsing is lenient:
//   - Non-JSON and malformed lines are skipped (not fatal).
//   - Lines where type != "assistant" are skipped.
//   - Assistant lines with no usage block are skipped.
//   - All other lines contribute their token counts to the per-model accumulator.
//
// A missing file returns a non-nil error.
// An empty file (or a file with no parseable assistant+usage lines) returns an
// empty (non-nil) map and a nil error.
func SumUsage(path string) (map[string]pricing.Usage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]pricing.Usage)

	sc := bufio.NewScanner(f)
	// Allocate a 10 MiB initial buffer; allow it to grow up to 10 MiB max.
	buf := make([]byte, scannerBufSize)
	sc.Buffer(buf, scannerBufSize)

	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}

		var line transcriptLine
		if err := json.Unmarshal(raw, &line); err != nil {
			// Malformed line — skip, not fatal.
			continue
		}

		if line.Type != "assistant" {
			continue
		}
		if line.Message.Usage == nil {
			continue
		}
		if line.Message.Model == "" {
			continue
		}

		u := line.Message.Usage
		existing := result[line.Message.Model]
		result[line.Message.Model] = pricing.Usage{
			InputTokens:         existing.InputTokens + u.InputTokens,
			OutputTokens:        existing.OutputTokens + u.OutputTokens,
			CacheReadTokens:     existing.CacheReadTokens + u.CacheReadInputTokens,
			CacheCreationTokens: existing.CacheCreationTokens + u.CacheCreationInputTokens,
		}
	}

	// sc.Err() returns nil on normal EOF; any scanner error (e.g., token too
	// long even after our large buffer) would surface here — treat as fatal.
	if err := sc.Err(); err != nil {
		return nil, err
	}

	return result, nil
}
