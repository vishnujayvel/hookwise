package core

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

// maxStdinSize is the maximum bytes we'll read from stdin (10 MB).
const maxStdinSize = 10 * 1024 * 1024

// ReadStdinPayload reads and parses the hook payload from stdin.
// On malformed JSON or read failure, returns a minimal empty payload (fail-open).
func ReadStdinPayload() HookPayload {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, maxStdinSize))
	if err != nil {
		Logger().Error("failed to read stdin", "error", err)
		return HookPayload{}
	}

	input := strings.TrimSpace(string(data))
	if input == "" {
		return HookPayload{}
	}

	var payload HookPayload
	if err := json.Unmarshal([]byte(input), &payload); err != nil {
		Logger().Warn("malformed stdin JSON, guard evaluation will use empty payload", "error", err)
		return HookPayload{}
	}

	return payload
}
