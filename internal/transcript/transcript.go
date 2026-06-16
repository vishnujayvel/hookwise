// Package transcript parses Claude Code .jsonl transcript files and aggregates
// token usage per model.
package transcript

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"

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

// maxLineBytes is the upper bound on lines we will attempt to JSON-parse.
// Lines longer than this are skipped (not fatal) — they exceed any realistic
// transcript line shape and would only arise from embedded base64 blobs or
// corrupted data. ReadString buffers a full line in memory; this cap bounds
// the json-parse step while keeping accounting for all other lines intact.
const maxLineBytes = 50 * 1024 * 1024 // 50 MiB

// SumUsage opens the .jsonl transcript at path, scans it line by line, and
// returns a map from model ID to the total token usage accumulated across all
// assistant messages that model produced.
//
// Parsing is lenient:
//   - Non-JSON and malformed lines are skipped (not fatal).
//   - Lines where type != "assistant" are skipped.
//   - Assistant lines with no usage block are skipped.
//   - Lines longer than maxLineBytes are skipped (not fatal).
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

	r := bufio.NewReader(f)
	for {
		raw, err := r.ReadString('\n')
		// Process whatever was read before checking the error.
		line := strings.TrimSpace(raw)
		if line != "" {
			// Skip pathologically large lines — don't attempt to json.Unmarshal them.
			if len(line) > maxLineBytes {
				// oversized line: skip, not fatal (ARCH-1 spirit: degrade gracefully)
			} else {
				var tl transcriptLine
				if jsonErr := json.Unmarshal([]byte(line), &tl); jsonErr == nil {
					if tl.Type == "assistant" && tl.Message.Usage != nil && tl.Message.Model != "" {
						u := tl.Message.Usage
						existing := result[tl.Message.Model]
						result[tl.Message.Model] = pricing.Usage{
							InputTokens:         existing.InputTokens + u.InputTokens,
							OutputTokens:        existing.OutputTokens + u.OutputTokens,
							CacheReadTokens:     existing.CacheReadTokens + u.CacheReadInputTokens,
							CacheCreationTokens: existing.CacheCreationTokens + u.CacheCreationInputTokens,
						}
					}
				}
				// Malformed JSON or non-assistant line — skip, not fatal.
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}

	return result, nil
}
