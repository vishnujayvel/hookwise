package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// vaporwareTerms are features that were deleted from the product but kept
// resurfacing in user-visible text. The 2026-06-24 relaunch audit found
// references to them surviving in --help output and openspec specs after the
// code was long gone:
//
//   - guard effectiveness: notification producer deleted as vaporware (#207),
//     doc references purged (#214), audit still found leftovers.
//   - transcript backup: TS-era feature never ported to Go; config block and
//     doc surface removed (#200, #215).
//
// Matching is case-insensitive and tolerant of space/underscore/hyphen
// separators so `guard_effectiveness`, `guard-effectiveness`, and
// "Guard Effectiveness" are all caught.
var vaporwareTerms = []*regexp.Regexp{
	regexp.MustCompile(`(?i)guard[\s_-]?effectiveness`),
	regexp.MustCompile(`(?i)transcript[\s_-]?backup`),
}

// vaporwareSweptDirs are the user-visible surfaces the guard sweeps, relative
// to the module root: command sources (help strings, error messages), Go
// internals (comments leak into godoc), published docs, the current openspec
// specs, the Python TUI sources, and recipe YAMLs.
//
// Deliberately NOT swept: _test.go files and tui/tests (the negative-guard
// tests themselves must name the dead terms), openspec/changes (immutable
// historical change proposals), CHANGELOG.md and docs/retro-*.md (forensic
// history is never rewritten to match the present).
var vaporwareSweptDirs = []string{
	"cmd",
	"internal",
	"docs",
	"openspec/specs",
	"tui/hookwise_tui",
	"recipes",
}

// vaporwareSweptFiles are individual files swept in addition to the dirs.
var vaporwareSweptFiles = []string{
	"README.md",
	"openspec/config.yaml",
}

// vaporwareAllowlist maps relative paths to the reason a match there is
// acceptable. Entries are added deliberately — only for text that documents
// the REMOVAL itself, never for text that advertises the feature as present.
var vaporwareAllowlist = map[string]string{
	"openspec/specs/notifications/spec.md": "prune note documents the #207 removal by name",
}

var vaporwareTextExts = map[string]bool{
	".go": true, ".md": true, ".py": true, ".yaml": true, ".yml": true,
	".txt": true, ".json": true, ".ts": true, ".sh": true,
}

// TestArch_NoVaporwareReferencesInUserSurfaces greps the swept surfaces for
// references to deleted features. A hit means user-visible text is promising
// functionality that no longer exists (bead hw-l05u; relaunch audit
// 2026-06-24). Fix the text — do not extend the allowlist unless the text
// documents the removal itself.
func TestArch_NoVaporwareReferencesInUserSurfaces(t *testing.T) {
	root := findModuleRoot(t)

	var targets []string
	for _, d := range vaporwareSweptDirs {
		err := filepath.Walk(filepath.Join(root, d), func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return nil //nolint:nilerr // best-effort walk; unreadable entries are skipped
			}
			if info.IsDir() {
				base := info.Name()
				if base == ".venv" || base == "__pycache__" || base == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			targets = append(targets, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", d, err)
		}
	}
	for _, f := range vaporwareSweptFiles {
		targets = append(targets, filepath.Join(root, f))
	}

	for _, path := range targets {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("rel path for %s: %v", path, err)
		}
		rel = filepath.ToSlash(rel)

		if !vaporwareTextExts[filepath.Ext(path)] {
			continue
		}
		if strings.HasSuffix(path, "_test.go") || strings.HasPrefix(rel, "tui/tests/") {
			continue
		}
		if _, ok := vaporwareAllowlist[rel]; ok {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			continue // best-effort: unreadable files are skipped, same as the walk
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, re := range vaporwareTerms {
				if re.MatchString(line) {
					t.Errorf("%s:%d references deleted feature (%s): %s",
						rel, i+1, re.String(), strings.TrimSpace(line))
				}
			}
		}
	}
}
