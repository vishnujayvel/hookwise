package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSettings writes a settings.json with the given hooks block to a temp
// file and returns its path.
func writeSettings(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestScan_ParsesHooksAcrossEvents(t *testing.T) {
	path := writeSettings(t, `{
      "hooks": {
        "PreToolUse": [
          {"matcher": "Bash", "hooks": [
            {"type": "command", "command": "hookwise dispatch PreToolUse"},
            {"type": "command", "command": "uvx claude-code-guardian"}
          ]},
          {"matcher": "", "hooks": [
            {"type": "command", "command": "hookwise dispatch PreToolUse"}
          ]}
        ],
        "SessionStart": [
          {"matcher": "", "hooks": [
            {"type": "command", "command": "python3 /x/quote.py"}
          ]}
        ]
      }
    }`)

	inv, err := Scan([]string{path})
	require.NoError(t, err)

	// 4 total hook entries across 2 events.
	assert.Len(t, inv.Entries, 4)
	assert.Equal(t, 3, inv.CountByEvent()["PreToolUse"])
	assert.Equal(t, 1, inv.CountByEvent()["SessionStart"])

	// Source file is recorded on each entry.
	for _, e := range inv.Entries {
		assert.Equal(t, path, e.SourceFile)
	}
}

func TestScan_MissingFileIsSkipped(t *testing.T) {
	inv, err := Scan([]string{"/nonexistent/settings.json"})
	require.NoError(t, err, "a missing settings file is not an error")
	assert.Empty(t, inv.Entries)
}

func TestScan_MalformedJSONRecordedNotFatal(t *testing.T) {
	bad := writeSettings(t, `{ this is not json `)
	good := writeSettings(t, `{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"echo hi"}]}]}}`)

	inv, err := Scan([]string{bad, good})
	require.NoError(t, err, "one malformed file must not fail the whole scan")
	assert.Len(t, inv.Entries, 1, "the good file's hook is still parsed")
	assert.Len(t, inv.ParseErrors, 1, "the malformed file is recorded as a parse error")
}

func TestScan_LaterFilesAppend(t *testing.T) {
	a := writeSettings(t, `{"hooks":{"PreToolUse":[{"matcher":"","hooks":[{"type":"command","command":"a"}]}]}}`)
	b := writeSettings(t, `{"hooks":{"PreToolUse":[{"matcher":"","hooks":[{"type":"command","command":"b"}]}]}}`)
	inv, err := Scan([]string{a, b})
	require.NoError(t, err)
	assert.Equal(t, 2, inv.CountByEvent()["PreToolUse"])
}
