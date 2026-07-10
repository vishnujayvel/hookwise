package hooks

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// SettingsPaths returns the Claude Code user-level settings files to scan for
// hooks, given the .claude directory: settings.json (shared) and
// settings.local.json (personal overrides). Both are always listed even if
// absent — Scan tolerates missing files. settings.json is kept first because
// runWire targets paths[0] as the wire destination.
//
// Note: the previous implementation globbed
// ~/.claude/projects/*/settings.local.json, but that directory holds
// conversation transcripts (.jsonl), not settings, so the glob never matched in
// production (its test invented an on-disk structure that does not exist — a
// mock-confidence trap). Restoring the user-level settings.local.json scan
// closes a real gap (personal hooks were unscanned). Project-local settings
// discovery (<project>/.claude/settings{,.local}.json) needs real project
// enumeration and is a separate follow-up.
func SettingsPaths(claudeDir string) []string {
	return []string{
		filepath.Join(claudeDir, "settings.json"),
		filepath.Join(claudeDir, "settings.local.json"),
	}
}

// DefaultSettingsPaths returns SettingsPaths for the Claude Code settings
// directory. It honors the HOOKWISE_CLAUDE_DIR env override (used by tests to
// isolate the hook scan from the developer's real ~/.claude), falling back to
// the real ~/.claude directory.
func DefaultSettingsPaths() []string {
	claudeDir := os.Getenv("HOOKWISE_CLAUDE_DIR")
	if claudeDir == "" {
		claudeDir = filepath.Join(core.HomeDir(), ".claude")
	}
	return SettingsPaths(claudeDir)
}

// ProjectSettingsPaths returns the project-level settings files to scan for a
// given project directory: <dir>/.claude/settings.json and
// <dir>/.claude/settings.local.json. Like SettingsPaths, both are always
// listed even if absent — Scan tolerates missing files.
func ProjectSettingsPaths(projectDir string) []string {
	return SettingsPaths(filepath.Join(projectDir, ".claude"))
}

// AllFindings runs every hook-safety analysis over the inventory and returns the
// findings in a stable order: inventory (SCAN) → sprawl → missing binary →
// network → duplicates/overlap. lookPath is injected for the binary check
// (production passes exec.LookPath).
func AllFindings(inv *Inventory, lookPath func(string) (string, error)) []Finding {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	out := []Finding{InventoryFinding(inv)}
	out = append(out, SprawlFindings(inv)...)
	out = append(out, MissingBinaryFindings(inv, lookPath)...)
	out = append(out, NetworkHookFindings(inv)...)
	out = append(out, DuplicateFindings(inv)...)
	return out
}
