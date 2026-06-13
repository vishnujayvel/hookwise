package hooks

import (
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

// DefaultSettingsPaths returns SettingsPaths for the real ~/.claude directory.
func DefaultSettingsPaths() []string {
	return SettingsPaths(filepath.Join(core.HomeDir(), ".claude"))
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
