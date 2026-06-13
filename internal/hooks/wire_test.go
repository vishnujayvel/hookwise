package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedNow returns a deterministic time for backup-name assertions.
func fixedNow() time.Time {
	return time.Date(2026, 6, 13, 18, 31, 15, 0, time.UTC)
}

// fixedNowFn is the injectable nowFn that returns fixedNow().
func fixedNowFn() time.Time { return fixedNow() }

// emptySettings writes an empty JSON object to a temp file and returns its path.
func emptySettings(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(p, []byte("{}\n"), 0o600))
	return p
}

// settingsWithContent writes arbitrary JSON to a temp file and returns its path.
func settingsWithContent(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

// nonexistentSettings returns a path inside a temp dir that does not yet exist.
func nonexistentSettings(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "settings.json")
}

// readJSON reads a file and unmarshals it into a map.
func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

// baseOpts returns a WireOptions with a fixed nowFn and rollback note disabled.
func baseOpts(settingsPath string) WireOptions {
	return WireOptions{
		SettingsPath:     settingsPath,
		StatusLine:       true,
		nowFn:            fixedNowFn,
		RollbackNotePath: "-", // skip rollback note in tests
	}
}

// ---------------------------------------------------------------------------
// Happy path: wire from empty
// ---------------------------------------------------------------------------

func TestWire_AddsHooksAndStatusLine(t *testing.T) {
	p := emptySettings(t)
	opts := baseOpts(p)

	result, err := Wire(opts)
	require.NoError(t, err)
	assert.True(t, result.Changed)
	assert.ElementsMatch(t, []string{"PreToolUse", "PostToolUse"}, result.WiredEvents)
	assert.Empty(t, result.SkippedEvents)
	assert.True(t, result.StatusLineWired)
	assert.False(t, result.StatusLineSkipped)

	m := readJSON(t, p)
	hooksRaw, ok := m["hooks"].(map[string]any)
	require.True(t, ok, "hooks should be an object")

	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		groups, ok := hooksRaw[event].([]any)
		require.True(t, ok, "event %s should have groups", event)
		found := false
		for _, g := range groups {
			gm := g.(map[string]any)
			matcher, _ := gm["matcher"].(string)
			if matcher != "" {
				continue
			}
			hooks, _ := gm["hooks"].([]any)
			for _, h := range hooks {
				hm := h.(map[string]any)
				cmd, _ := hm["command"].(string)
				if cmd == "hookwise dispatch "+event {
					found = true
				}
			}
		}
		assert.True(t, found, "should have dispatch entry for %s", event)
	}

	slRaw, ok := m["statusLine"].(map[string]any)
	require.True(t, ok, "statusLine should be a map")
	assert.Equal(t, "hookwise status-line", slRaw["command"])
	assert.Equal(t, "command", slRaw["type"])
}

// ---------------------------------------------------------------------------
// Wire from a nonexistent file (new settings)
// ---------------------------------------------------------------------------

func TestWire_CreatesFileIfMissing(t *testing.T) {
	p := nonexistentSettings(t)
	opts := baseOpts(p)

	result, err := Wire(opts)
	require.NoError(t, err)
	assert.True(t, result.Changed)

	_, err = os.Stat(p)
	assert.NoError(t, err, "settings.json should have been created")
}

// ---------------------------------------------------------------------------
// Backup
// ---------------------------------------------------------------------------

func TestWire_BackupCreatedBeforeWrite(t *testing.T) {
	p := emptySettings(t)
	opts := baseOpts(p)

	result, err := Wire(opts)
	require.NoError(t, err)
	require.NotEmpty(t, result.BackupPath)

	// Backup path uses the fixed timestamp.
	expectedBackup := p + ".bak-20260613T183115Z"
	assert.Equal(t, expectedBackup, result.BackupPath)

	// Backup exists and contains the original content.
	data, err := os.ReadFile(result.BackupPath)
	require.NoError(t, err)
	assert.Equal(t, "{}\n", string(data))
}

// ---------------------------------------------------------------------------
// Idempotency: second Wire is a no-op
// ---------------------------------------------------------------------------

func TestWire_IdempotentRerun(t *testing.T) {
	p := emptySettings(t)
	opts := baseOpts(p)

	// First wire.
	r1, err := Wire(opts)
	require.NoError(t, err)
	assert.True(t, r1.Changed)

	// Second wire with a different timestamp so backup names won't clash.
	opts2 := opts
	ts2 := fixedNow().Add(time.Minute)
	opts2.nowFn = func() time.Time { return ts2 }

	r2, err := Wire(opts2)
	require.NoError(t, err)
	assert.False(t, r2.Changed, "second wire should be a no-op")
	assert.ElementsMatch(t, []string{"PreToolUse", "PostToolUse"}, r2.SkippedEvents)
	assert.True(t, r2.StatusLineSkipped)
	assert.False(t, r2.StatusLineWired)
	assert.Empty(t, r2.BackupPath, "no backup when nothing changed")
}

// ---------------------------------------------------------------------------
// DryRun: file unchanged, Diff populated
// ---------------------------------------------------------------------------

func TestWire_DryRun(t *testing.T) {
	p := emptySettings(t)
	mtime1, err := os.Stat(p)
	require.NoError(t, err)

	opts := baseOpts(p)
	opts.DryRun = true

	result, err := Wire(opts)
	require.NoError(t, err)
	assert.False(t, result.Changed)
	assert.NotEmpty(t, result.Diff)
	assert.Empty(t, result.BackupPath)

	mtime2, err := os.Stat(p)
	require.NoError(t, err)
	assert.Equal(t, mtime1.ModTime(), mtime2.ModTime(), "file mtime must not change on dry-run")

	// File content must still be the original.
	data, _ := os.ReadFile(p)
	assert.Equal(t, "{}\n", string(data))

	// Diff should mention the added content.
	assert.True(t, strings.Contains(result.Diff, "hookwise dispatch"), "diff should mention dispatch command")
}

// ---------------------------------------------------------------------------
// --events: only wire specified event
// ---------------------------------------------------------------------------

func TestWire_CustomEvents(t *testing.T) {
	p := emptySettings(t)
	opts := baseOpts(p)
	opts.Events = []string{"PreToolUse"}
	opts.StatusLine = false

	result, err := Wire(opts)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"PreToolUse"}, result.WiredEvents)
	assert.Empty(t, result.SkippedEvents)

	m := readJSON(t, p)
	hooksRaw := m["hooks"].(map[string]any)
	assert.Contains(t, hooksRaw, "PreToolUse")
	assert.NotContains(t, hooksRaw, "PostToolUse")
}

// ---------------------------------------------------------------------------
// --no-status-line: statusLine not added
// ---------------------------------------------------------------------------

func TestWire_NoStatusLine(t *testing.T) {
	p := emptySettings(t)
	opts := baseOpts(p)
	opts.StatusLine = false

	result, err := Wire(opts)
	require.NoError(t, err)
	assert.False(t, result.StatusLineWired)
	assert.False(t, result.StatusLineSkipped)

	m := readJSON(t, p)
	_, hasStatusLine := m["statusLine"]
	assert.False(t, hasStatusLine, "statusLine should not be present")
}

// ---------------------------------------------------------------------------
// Preserves unrelated keys
// ---------------------------------------------------------------------------

func TestWire_PreservesOtherKeys(t *testing.T) {
	content := `{"theme": "dark", "someOtherSetting": true}`
	p := settingsWithContent(t, content)
	opts := baseOpts(p)

	_, err := Wire(opts)
	require.NoError(t, err)

	m := readJSON(t, p)
	assert.Equal(t, "dark", m["theme"])
	assert.Equal(t, true, m["someOtherSetting"])
}

// ---------------------------------------------------------------------------
// Unwire: removes only hookwise entries, preserves others
// ---------------------------------------------------------------------------

func TestWire_Unwire_RemovesOnlyHookwiseEntries(t *testing.T) {
	// Seed a settings.json with both hookwise entries AND an unrelated guard hook.
	content := `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "",
        "hooks": [{"type": "command", "command": "hookwise dispatch PreToolUse"}]
      },
      {
        "matcher": "Bash",
        "hooks": [{"type": "command", "command": "python3 /usr/local/lib/guard.py"}]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "",
        "hooks": [{"type": "command", "command": "hookwise dispatch PostToolUse"}]
      }
    ]
  },
  "statusLine": {"type": "command", "command": "hookwise status-line"},
  "theme": "dark"
}`
	p := settingsWithContent(t, content)
	opts := baseOpts(p)
	opts.Unwire = true

	result, err := Wire(opts)
	require.NoError(t, err)
	assert.True(t, result.Changed)

	m := readJSON(t, p)

	// The unrelated guard hook must still be present.
	hooksRaw, ok := m["hooks"].(map[string]any)
	require.True(t, ok)
	preGroups, ok := hooksRaw["PreToolUse"].([]any)
	require.True(t, ok, "PreToolUse should still exist (has non-hookwise group)")
	assert.Len(t, preGroups, 1, "only the guard.py group should remain")
	gm := preGroups[0].(map[string]any)
	hooks, _ := gm["hooks"].([]any)
	hm := hooks[0].(map[string]any)
	assert.Equal(t, "python3 /usr/local/lib/guard.py", hm["command"])

	// PostToolUse had only hookwise entries; the event key should be removed.
	_, postExists := hooksRaw["PostToolUse"]
	assert.False(t, postExists, "PostToolUse should be removed entirely")

	// statusLine should be removed.
	_, slExists := m["statusLine"]
	assert.False(t, slExists, "statusLine should be removed on unwire")

	// Unrelated key preserved.
	assert.Equal(t, "dark", m["theme"])
}

// ---------------------------------------------------------------------------
// Unwire is idempotent (second unwire is a no-op)
// ---------------------------------------------------------------------------

func TestWire_Unwire_Idempotent(t *testing.T) {
	content := `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "", "hooks": [{"type": "command", "command": "hookwise dispatch PreToolUse"}]}
    ]
  },
  "statusLine": {"type": "command", "command": "hookwise status-line"}
}`
	p := settingsWithContent(t, content)
	opts := baseOpts(p)
	opts.Unwire = true

	// First unwire.
	r1, err := Wire(opts)
	require.NoError(t, err)
	assert.True(t, r1.Changed)

	// Second unwire — already gone, nothing to change.
	opts2 := opts
	ts2 := fixedNow().Add(time.Minute)
	opts2.nowFn = func() time.Time { return ts2 }
	r2, err := Wire(opts2)
	require.NoError(t, err)
	assert.False(t, r2.Changed)
}

// ---------------------------------------------------------------------------
// Validate-after-write: malformed backup restore
// ---------------------------------------------------------------------------

func TestWire_ValidateWritten_PassesOnHappyPath(t *testing.T) {
	// Confirm that the written file validates cleanly (no restore needed).
	p := emptySettings(t)
	opts := baseOpts(p)

	result, err := Wire(opts)
	require.NoError(t, err)
	// Backup exists, written file parses.
	require.NotEmpty(t, result.BackupPath)

	var tmp any
	data, _ := os.ReadFile(p)
	assert.NoError(t, json.Unmarshal(data, &tmp), "written file must be valid JSON")
}

// ---------------------------------------------------------------------------
// Pre-flight: malformed existing settings file is recorded (ParseError)
// ---------------------------------------------------------------------------

func TestWire_PreExistingMalformedFile_HandledGracefully(t *testing.T) {
	p := settingsWithContent(t, `{ not valid json }`)
	opts := baseOpts(p)

	// Wire should fail because readSettingsMap will return an error for malformed JSON.
	_, err := Wire(opts)
	assert.Error(t, err, "malformed settings.json should yield an error")
}

// ---------------------------------------------------------------------------
// Custom DispatchCommand and StatusLineCommand
// ---------------------------------------------------------------------------

func TestWire_CustomCommands(t *testing.T) {
	p := emptySettings(t)
	opts := baseOpts(p)
	opts.DispatchCommand = "/usr/local/bin/hookwise dispatch"
	opts.StatusLineCommand = "/usr/local/bin/hookwise status-line"

	result, err := Wire(opts)
	require.NoError(t, err)
	assert.True(t, result.Changed)

	m := readJSON(t, p)
	hooksRaw := m["hooks"].(map[string]any)
	groups := hooksRaw["PreToolUse"].([]any)
	gm := groups[0].(map[string]any)
	hooks := gm["hooks"].([]any)
	hm := hooks[0].(map[string]any)
	assert.Equal(t, "/usr/local/bin/hookwise dispatch PreToolUse", hm["command"])
}

// ---------------------------------------------------------------------------
// DryRun on already-wired file: reports no changes
// ---------------------------------------------------------------------------

func TestWire_DryRun_AlreadyWired_ReportsNoChanges(t *testing.T) {
	p := emptySettings(t)
	opts := baseOpts(p)

	// Wire first.
	_, err := Wire(opts)
	require.NoError(t, err)

	// DryRun second time.
	opts2 := opts
	opts2.DryRun = true
	opts2.nowFn = fixedNowFn
	result, err := Wire(opts2)
	require.NoError(t, err)
	assert.False(t, result.Changed)
	assert.Contains(t, result.Diff, "(no changes)")
}
