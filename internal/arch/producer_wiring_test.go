package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// producerWiringAllowlist lists feed Producer types that are KNOWN to have no
// production wiring yet, each tied to a tracking issue. The guard below fails on
// any *Producer type NOT in this list that is referenced only from its own
// declaration file and/or _test.go files — its job is to stop NEW unwired
// producers (a "shareable feed" that never runs) from being added.
//
// Entries are added deliberately, never to silence a freshly-introduced dead
// producer. Each value is the tracking issue for wiring (or removing) it.
// Intentionally empty: CustomProducer was wired into the daemon/config path
// (#124 — registerCustomFeeds in cmd/hookwise/cmd_daemon.go constructs each
// config.Feeds.Custom entry via NewCustomProducer), so it no longer needs an
// exception. Add an entry here only to track a deliberately-deferred wiring.
var producerWiringAllowlist = map[string]string{}

// TestArch_ProducerTypesAreWired enforces the WRITER AUDIT (#99) lesson for the
// feed platform: a Producer type that is declared but never registered or
// constructed in production is a dead feature — a "shareable feed" that can
// never run. Built-in producers are wired via RegisterBuiltins
// (Register(&XProducer{})); custom producers via NewXProducer(...).
//
// The guard walks the module source, finds every `type \w+Producer struct`
// declaration, and asserts each is referenced from at least one non-test .go
// file OTHER than its declaration file — via the type itself (`&Name{}`,
// `*Name`) or its constructor (`NewName(`) — or is explicitly allowlisted with
// a tracking issue.
func TestArch_ProducerTypesAreWired(t *testing.T) {
	root := findModuleRoot(t)

	declRe := regexp.MustCompile(`type (\w+Producer) struct`)

	type decl struct{ file string }
	declared := map[string]decl{} // producer type -> declaring file (relative)

	// Collected non-test source so we can scan for references in a second pass
	// without re-reading the tree.
	type srcFile struct{ rel, text string }
	var sources []srcFile

	walk := func(collect bool) error {
		return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil || info.IsDir() {
				return nil //nolint:nilerr // best-effort walk; unreadable entries are skipped
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			// Skip worktrees and vendored trees.
			if strings.Contains(path, "/.claude/") || strings.Contains(path, "/vendor/") {
				return nil
			}
			// Tests neither declare production producers nor count as production
			// wiring.
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
				declared[m[1]] = decl{file: rel}
			}
			if collect {
				sources = append(sources, srcFile{rel: rel, text: text})
			}
			return nil
		})
	}

	if err := walk(true); err != nil {
		t.Fatalf("walking module source: %v", err)
	}

	if len(declared) == 0 {
		t.Fatal("found no Producer declarations — the guard's discovery regex is likely broken")
	}

	for name, d := range declared {
		// A producer is wired if some non-test file OTHER than its declaration
		// file references the type (`\bName\b` matches `&Name{`, `*Name`, etc.)
		// or its constructor (`\bNewName\b` matches `NewName(...)` call sites but
		// not the `func NewName(` definition, which lives in the decl file).
		refRe := regexp.MustCompile(`\b(New)?` + regexp.QuoteMeta(name) + `\b`)
		wired := false
		for _, s := range sources {
			if s.rel == d.file {
				continue // self-reference in the declaration file does not count
			}
			if refRe.MatchString(s.text) {
				wired = true
				break
			}
		}
		if wired {
			continue
		}
		if issue, ok := producerWiringAllowlist[name]; ok {
			t.Logf("known unwired producer %s (declared in %s) allowlisted: %s", name, d.file, issue)
			continue
		}
		t.Errorf("PRODUCER-WIRING violation: %s (declared in %s) has no production "+
			"registration/constructor caller — a Producer with no production wiring is a "+
			"dead feed (see #99). Register it in RegisterBuiltins, construct it from the "+
			"daemon/config path, or add it to producerWiringAllowlist with a tracking issue.",
			name, d.file)
	}
}
