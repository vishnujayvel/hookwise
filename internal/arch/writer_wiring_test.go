package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// writeStateAllowlist lists Write*State persistence methods that are KNOWN to
// have no production caller yet, each tied to a tracking issue. The guard below
// fails on any Write*State method NOT in this list that is referenced only from
// _test.go files — its job is to stop NEW unwired writers from being added.
//
// Entries are added deliberately, never to silence a freshly-introduced dead
// writer. Each value is the tracking issue for wiring (or removing) it.
var writeStateAllowlist = map[string]string{
	// WriteCoachingState has only test callers: the coaching-state producer was
	// not ported in the TS→Go rewrite. Tracked by the WRITER AUDIT.
	"WriteCoachingState": "#99 — coaching-state producer not yet ported (readers-without-writers)",
}

// TestArch_WriteStateMethodsHaveNonTestCaller enforces the WRITER AUDIT (#99)
// lesson: the TS→Go port shipped several "readers without writers" — Write*State
// persistence methods with no production caller — so cost/coaching/insights
// features were permanently $0/empty. A Write*State that is only ever called
// from tests is dead code dressed up as a feature.
//
// This guard walks the module source, finds every `func (...) Write\w+State(`
// declaration, and asserts each is referenced from at least one non-test .go
// file (or is explicitly allowlisted with a tracking issue).
func TestArch_WriteStateMethodsHaveNonTestCaller(t *testing.T) {
	root := findModuleRoot(t)

	declRe := regexp.MustCompile(`func \([^)]*\) (Write\w+State)\(`)
	callRe := regexp.MustCompile(`\.(Write\w+State)\(`)

	declared := map[string]string{} // method name -> declaring file (relative)
	calledInProd := map[string]bool{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil //nolint:nilerr // best-effort walk; unreadable entries are skipped
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip worktrees, vendored trees, and generated areas.
		if strings.Contains(path, "/.claude/") || strings.Contains(path, "/vendor/") {
			return nil
		}
		// Tests neither declare production writers nor count as production callers.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		text := string(src)
		rel := strings.TrimPrefix(path, root+"/")
		for _, m := range declRe.FindAllStringSubmatch(text, -1) {
			declared[m[1]] = rel
		}
		for _, m := range callRe.FindAllStringSubmatch(text, -1) {
			calledInProd[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking module source: %v", err)
	}

	if len(declared) == 0 {
		t.Fatal("found no Write*State declarations — the guard's discovery regex is likely broken")
	}

	for name, declFile := range declared {
		if calledInProd[name] {
			continue
		}
		if issue, ok := writeStateAllowlist[name]; ok {
			t.Logf("known dead writer %s (declared in %s) allowlisted: %s", name, declFile, issue)
			continue
		}
		t.Errorf("WRITER-AUDIT violation: %s (declared in %s) has no non-test caller — "+
			"a Write*State with no production caller is dead code (see #99). Wire it into a "+
			"producer/dispatch path, or add it to writeStateAllowlist with a tracking issue.",
			name, declFile)
	}
}
