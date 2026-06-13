package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// backupTimeLayout is the UTC timestamp format used in backup file names.
// Example: settings.json.bak-20260613T183115Z
const backupTimeLayout = "20060102T150405Z"

// defaultEvents are the hook events wired when --events is not specified.
var defaultEvents = []string{"PreToolUse", "PostToolUse"}

// WireOptions configures a Wire or Unwire operation.
type WireOptions struct {
	// SettingsPath is the absolute path to the Claude Code settings.json to
	// modify. Required — callers resolve the default via DefaultSettingsPaths().
	SettingsPath string

	// Events lists the hook events to wire (default: PreToolUse, PostToolUse).
	// Empty slice is normalised to the defaults.
	Events []string

	// StatusLine controls whether a statusLine entry is added/removed.
	StatusLine bool

	// DryRun computes changes but writes nothing. Diff is populated.
	DryRun bool

	// Unwire removes hookwise's own entries instead of adding them.
	Unwire bool

	// Force wires even when pre-flight finds FAIL-level findings.
	Force bool

	// DispatchCommand is the command prefix for dispatch hooks.
	// Default: "hookwise dispatch"
	DispatchCommand string

	// StatusLineCommand is the full command for the statusLine entry.
	// Default: "hookwise status-line"
	StatusLineCommand string

	// RollbackNotePath overrides the path for the rollback note. When empty,
	// defaults to <GetStateDir()>/dogfood-rollback.md. Set to "-" to skip.
	RollbackNotePath string

	// nowFn is injectable for deterministic backup timestamps in tests.
	nowFn func() time.Time
}

// normalise fills in defaults so callers only set what they care about.
func (o *WireOptions) normalise() {
	if len(o.Events) == 0 {
		o.Events = defaultEvents
	}
	if o.DispatchCommand == "" {
		o.DispatchCommand = "hookwise dispatch"
	}
	if o.StatusLineCommand == "" {
		o.StatusLineCommand = "hookwise status-line"
	}
	if o.nowFn == nil {
		o.nowFn = func() time.Time { return time.Now().UTC() }
	}
}

// WireResult reports the outcome of a Wire or Unwire operation.
type WireResult struct {
	// BackupPath is where the original settings.json was backed up (empty on DryRun).
	BackupPath string

	// Diff is a human-readable summary of what would change (populated on DryRun).
	Diff string

	// WiredEvents lists events for which a new dispatch group was added.
	WiredEvents []string

	// SkippedEvents lists events that already had a matching dispatch entry.
	SkippedEvents []string

	// StatusLineWired is true when the statusLine entry was added.
	StatusLineWired bool

	// StatusLineSkipped is true when the statusLine was already set correctly.
	StatusLineSkipped bool

	// Changed is true when the settings file was actually modified.
	Changed bool

	// Findings holds the pre-flight analysis results.
	Findings []Finding
}

// preflight runs the hook-safety scan and returns a non-nil error when the
// settings file has FAIL-level findings and Force is false.
func preflight(opts *WireOptions) ([]Finding, error) {
	inv, err := Scan([]string{opts.SettingsPath})
	if err != nil {
		// Unreadable existing file is still reported, not fatal for wire.
		return nil, fmt.Errorf("scan settings: %w", err)
	}

	findings := AllFindings(inv, nil)

	if opts.Force {
		return findings, nil
	}

	// Refuse if there are FAIL-level findings from duplicate or sprawl codes.
	for _, f := range findings {
		if f.Level == LevelFail && (f.Code == "hook-sprawl" || f.Code == "hook-duplicate") {
			return findings, fmt.Errorf("pre-flight refused: %s %s: %s (use --force to override)", f.Level, f.Code, f.Message)
		}
	}

	return findings, nil
}

// readSettingsMap reads the settings.json into a raw map so all other keys are
// preserved. Returns an empty map when the file does not exist.
func readSettingsMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return m, nil
}

// marshalSettings produces indented JSON bytes for a settings map.
func marshalSettings(m map[string]any) ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// backup copies the current settings file to a timestamped backup path and
// returns the backup path.
func backup(settingsPath string, nowFn func() time.Time) (string, error) {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Nothing to back up.
			return "", nil
		}
		return "", fmt.Errorf("read for backup: %w", err)
	}
	ts := nowFn().UTC().Format(backupTimeLayout)
	backupPath := settingsPath + ".bak-" + ts
	if err := os.WriteFile(backupPath, data, 0o600); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}
	return backupPath, nil
}

// validateWritten re-reads the written settings file and confirms it parses.
// On failure it restores the backup bytes and returns the error.
func validateWritten(settingsPath, backupPath string) error {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("re-read after write: %w", err)
	}
	var tmp any
	if err := json.Unmarshal(data, &tmp); err != nil {
		// Restore the backup.
		if backupPath != "" {
			bdata, berr := os.ReadFile(backupPath)
			if berr == nil {
				_ = os.WriteFile(settingsPath, bdata, 0o600)
			}
		}
		return fmt.Errorf("validate written JSON: %w (backup restored)", err)
	}
	return nil
}

// writeRollbackNote writes a rollback command to the dogfood-rollback.md note.
// Best-effort: errors are silently ignored per the spec.
func writeRollbackNote(opts *WireOptions, backupPath string) {
	if opts.RollbackNotePath == "-" || backupPath == "" {
		return
	}
	notePath := opts.RollbackNotePath
	if notePath == "" {
		notePath = filepath.Join(core.GetStateDir(), "dogfood-rollback.md")
	}
	content := fmt.Sprintf("# hookwise rollback\n\nTo restore your previous settings:\n\n```sh\ncp %s %s\n```\n",
		backupPath, opts.SettingsPath)
	_ = os.MkdirAll(filepath.Dir(notePath), 0o700)
	_ = os.WriteFile(notePath, []byte(content), 0o600)
}

// hookGroupForEvent returns the dispatch command for a given event.
func hookGroupForEvent(dispatchCmd, event string) map[string]any {
	return map[string]any{
		"matcher": "",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": dispatchCmd + " " + event,
			},
		},
	}
}

// hooksMapFromRaw extracts and type-asserts the "hooks" key as map[string]any.
func hooksMapFromRaw(m map[string]any) map[string]any {
	if raw, ok := m["hooks"]; ok {
		if hm, ok := raw.(map[string]any); ok {
			return hm
		}
	}
	return map[string]any{}
}

// groupsForEvent returns the slice of matcher-group maps for a given event.
func groupsForEvent(hooksMap map[string]any, event string) []any {
	if raw, ok := hooksMap[event]; ok {
		if sl, ok := raw.([]any); ok {
			return sl
		}
	}
	return nil
}

// dispatchCommandForEvent builds the exact command string expected for an event.
func dispatchCommandForEvent(dispatchCmd, event string) string {
	return dispatchCmd + " " + event
}

// hasDispatchEntry returns true when the groups slice already contains a group
// with matcher="" and the exact dispatch command for this event.
func hasDispatchEntry(groups []any, wantCmd string) bool {
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		matcher, _ := gm["matcher"].(string)
		if matcher != "" {
			continue
		}
		hooks, _ := gm["hooks"].([]any)
		for _, h := range hooks {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			cmd, _ := hm["command"].(string)
			if cmd == wantCmd {
				return true
			}
		}
	}
	return false
}

// applyWire mutates the raw settings map to add hookwise entries.
// It returns which events were wired and which were skipped.
func applyWire(m map[string]any, opts *WireOptions) (wired, skipped []string, statusWired, statusSkipped bool) {
	hooksMap := hooksMapFromRaw(m)

	for _, event := range opts.Events {
		wantCmd := dispatchCommandForEvent(opts.DispatchCommand, event)
		groups := groupsForEvent(hooksMap, event)
		if hasDispatchEntry(groups, wantCmd) {
			skipped = append(skipped, event)
			continue
		}
		groups = append(groups, hookGroupForEvent(opts.DispatchCommand, event))
		hooksMap[event] = groups
		wired = append(wired, event)
	}

	m["hooks"] = hooksMap

	if opts.StatusLine {
		// Check current statusLine value.
		if sl, ok := m["statusLine"]; ok {
			slm, ok := sl.(map[string]any)
			if ok {
				cmd, _ := slm["command"].(string)
				if cmd == opts.StatusLineCommand {
					statusSkipped = true
				} else {
					m["statusLine"] = map[string]any{
						"type":    "command",
						"command": opts.StatusLineCommand,
					}
					statusWired = true
				}
			} else {
				m["statusLine"] = map[string]any{
					"type":    "command",
					"command": opts.StatusLineCommand,
				}
				statusWired = true
			}
		} else {
			m["statusLine"] = map[string]any{
				"type":    "command",
				"command": opts.StatusLineCommand,
			}
			statusWired = true
		}
	}

	return wired, skipped, statusWired, statusSkipped
}

// applyUnwire removes hookwise's own entries from the raw settings map.
// It removes any matcher-group whose command has prefix DispatchCommand and
// removes the statusLine if it matches StatusLineCommand.
func applyUnwire(m map[string]any, opts *WireOptions) {
	hooksMap := hooksMapFromRaw(m)

	for event, raw := range hooksMap {
		groups, ok := raw.([]any)
		if !ok {
			continue
		}
		var kept []any
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				kept = append(kept, g)
				continue
			}
			hooks, _ := gm["hooks"].([]any)
			isHookwise := false
			for _, h := range hooks {
				hm, ok := h.(map[string]any)
				if !ok {
					continue
				}
				cmd, _ := hm["command"].(string)
				if strings.HasPrefix(cmd, opts.DispatchCommand) {
					isHookwise = true
					break
				}
			}
			if !isHookwise {
				kept = append(kept, g)
			}
		}
		if len(kept) == 0 {
			delete(hooksMap, event)
		} else {
			hooksMap[event] = kept
		}
	}

	if len(hooksMap) > 0 {
		m["hooks"] = hooksMap
	} else {
		// Keep empty map rather than dropping the key to preserve structure.
		m["hooks"] = hooksMap
	}

	// Remove statusLine if it matches.
	if sl, ok := m["statusLine"]; ok {
		if slm, ok := sl.(map[string]any); ok {
			cmd, _ := slm["command"].(string)
			if cmd == opts.StatusLineCommand {
				delete(m, "statusLine")
			}
		}
	}
}

// simpleDiff produces a human-readable diff summary between old and new JSON bytes.
func simpleDiff(oldBytes, newBytes []byte) string {
	if bytes.Equal(oldBytes, newBytes) {
		return "(no changes)"
	}
	var buf strings.Builder
	buf.WriteString("--- current settings.json\n")
	buf.WriteString("+++ proposed settings.json\n")

	oldLines := strings.Split(strings.TrimRight(string(oldBytes), "\n"), "\n")
	newLines := strings.Split(strings.TrimRight(string(newBytes), "\n"), "\n")

	// Build sets for quick lookup.
	oldSet := make(map[string]bool, len(oldLines))
	for _, l := range oldLines {
		oldSet[l] = true
	}
	newSet := make(map[string]bool, len(newLines))
	for _, l := range newLines {
		newSet[l] = true
	}

	// Lines removed.
	for _, l := range oldLines {
		if !newSet[l] {
			buf.WriteString("- " + l + "\n")
		}
	}
	// Lines added.
	for _, l := range newLines {
		if !oldSet[l] {
			buf.WriteString("+ " + l + "\n")
		}
	}
	return buf.String()
}

// Wire applies (or dry-runs) the wiring described by opts.
func Wire(opts WireOptions) (*WireResult, error) {
	opts.normalise()
	result := &WireResult{}

	// Pre-flight (skip if unwiring — we're removing, not adding).
	if !opts.Unwire {
		findings, err := preflight(&opts)
		result.Findings = findings
		if err != nil {
			return result, err
		}
	}

	// Read current settings.
	currentMap, err := readSettingsMap(opts.SettingsPath)
	if err != nil {
		return result, err
	}

	oldBytes, _ := marshalSettings(currentMap)

	// Apply changes to a working copy.
	workMap, err := readSettingsMap(opts.SettingsPath)
	if err != nil {
		return result, err
	}

	if opts.Unwire {
		applyUnwire(workMap, &opts)
	} else {
		wired, skipped, slWired, slSkipped := applyWire(workMap, &opts)
		result.WiredEvents = wired
		result.SkippedEvents = skipped
		result.StatusLineWired = slWired
		result.StatusLineSkipped = slSkipped
	}

	newBytes, err := marshalSettings(workMap)
	if err != nil {
		return result, fmt.Errorf("marshal new settings: %w", err)
	}

	changed := !bytes.Equal(oldBytes, newBytes)

	if opts.DryRun {
		// Dry-run writes nothing, so the file is never modified: Changed stays
		// false. The Diff still reflects what *would* change.
		result.Changed = false
		result.Diff = simpleDiff(oldBytes, newBytes)
		return result, nil
	}

	result.Changed = changed
	if !changed {
		// Nothing to write.
		return result, nil
	}

	// Backup before write.
	backupPath, err := backup(opts.SettingsPath, opts.nowFn)
	if err != nil {
		return result, fmt.Errorf("backup: %w", err)
	}
	result.BackupPath = backupPath

	// Write the new settings.
	if err := os.MkdirAll(filepath.Dir(opts.SettingsPath), 0o700); err != nil {
		return result, fmt.Errorf("ensure dir: %w", err)
	}
	if err := os.WriteFile(opts.SettingsPath, newBytes, 0o600); err != nil {
		return result, fmt.Errorf("write settings: %w", err)
	}

	// Validate the written file.
	if err := validateWritten(opts.SettingsPath, backupPath); err != nil {
		return result, err
	}

	// Write rollback note (best-effort).
	writeRollbackNote(&opts, backupPath)

	return result, nil
}
