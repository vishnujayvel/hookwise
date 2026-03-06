package core

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

// ReadStdinPayload reads and parses the hook payload from stdin.
// On malformed JSON or read failure, returns a minimal empty payload (fail-open).
func ReadStdinPayload() HookPayload {
	data, err := io.ReadAll(os.Stdin)
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
		Logger().Warn("stdin parsed but not valid JSON, using empty payload", "error", err)
		return HookPayload{}
	}

	return payload
}
