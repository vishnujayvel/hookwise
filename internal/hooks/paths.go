package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// SettingsPaths returns the Claude Code settings files to scan for hooks, given
// the .claude directory: the global settings.json (always listed, even if
// absent — Scan tolerates missing files) plus every
// projects/*/settings.local.json. Project paths are sorted for deterministic
// output.
func SettingsPaths(claudeDir string) []string {
	paths := []string{filepath.Join(claudeDir, "settings.json")}

	matches, _ := filepath.Glob(filepath.Join(claudeDir, "projects", "*", "settings.local.json"))
	sort.Strings(matches)
	paths = append(paths, matches...)
	return paths
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
