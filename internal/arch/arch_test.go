package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const modulePath = "github.com/vishnujayvel/hookwise"

// loadPackages loads all packages matching the given patterns from the module root.
func loadPackages(t *testing.T, patterns ...string) []*packages.Package {
	t.Helper()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps,
		Dir:  findModuleRoot(t),
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		t.Fatalf("failed to load packages %v: %v", patterns, err)
	}
	return pkgs
}

// findModuleRoot walks up from the test file to find the go.mod directory.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	// Use the working directory and walk up to the module root (where go.mod lives).
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod in any parent directory")
		}
		dir = parent
	}
}

// hasImport checks if pkg directly imports the target import path.
func hasImport(pkg *packages.Package, target string) bool {
	for imp := range pkg.Imports {
		if imp == target || strings.HasPrefix(imp, target+"/") {
			return true
		}
	}
	return false
}

// collectImports returns all direct imports for a package that match the prefix.
func collectImports(pkg *packages.Package, prefix string) []string {
	var matches []string
	for imp := range pkg.Imports {
		if strings.HasPrefix(imp, prefix) {
			matches = append(matches, imp)
		}
	}
	return matches
}

// --------------------------------------------------------------------------
// Task 2.1: Helper + positive control test
// --------------------------------------------------------------------------

// TestArch_PositiveControl verifies the package loader works by confirming
// internal/core imports the gobwas/glob package (a known-good import).
func TestArch_PositiveControl(t *testing.T) {
	pkgs := loadPackages(t, modulePath+"/internal/core")
	if len(pkgs) == 0 {
		t.Fatal("failed to load internal/core package")
	}
	found := false
	for imp := range pkgs[0].Imports {
		if strings.Contains(imp, "gobwas/glob") {
			found = true
			break
		}
	}
	if !found {
		t.Error("positive control failed: internal/core should import gobwas/glob")
	}
}

// --------------------------------------------------------------------------
// Task 2.2: ARCH-3 -- feeds must not import analytics
// --------------------------------------------------------------------------

// TestArch_FeedsDoNotImportAnalytics enforces ARCH-3: the daemon/feeds packages
// must never import internal/analytics (daemon writes JSON cache only, not Dolt).
func TestArch_FeedsDoNotImportAnalytics(t *testing.T) {
	analyticsPath := modulePath + "/internal/analytics"
	pkgs := loadPackages(t, modulePath+"/internal/feeds/...")

	for _, pkg := range pkgs {
		forbidden := collectImports(pkg, analyticsPath)
		for _, imp := range forbidden {
			t.Errorf("ARCH-3 violation: %s imports %s (feeds must not import analytics -- daemon writes JSON cache only)", pkg.PkgPath, imp)
		}
	}
}

// --------------------------------------------------------------------------
// Task 2.3: CMD boundary + PKG boundary
// --------------------------------------------------------------------------

// TestArch_CmdImportsOnlyInternalAndPkg verifies that cmd/hookwise/ only imports
// from internal/ and pkg/ within the module (never test packages or testdata).
func TestArch_CmdImportsOnlyInternalAndPkg(t *testing.T) {
	pkgs := loadPackages(t, modulePath+"/cmd/...")

	for _, pkg := range pkgs {
		for imp := range pkg.Imports {
			if !strings.HasPrefix(imp, modulePath) {
				continue // external dependency, fine
			}
			// Module-internal import -- must be internal/ or pkg/
			rel := strings.TrimPrefix(imp, modulePath+"/")
			if strings.HasPrefix(rel, "internal/") || strings.HasPrefix(rel, "pkg/") {
				continue // allowed
			}
			t.Errorf("CMD-BOUNDARY violation: %s imports %s (cmd/ may only import internal/ and pkg/)", pkg.PkgPath, imp)
		}
	}
}

// TestArch_PkgDoesNotImportInternal verifies that pkg/hookwise/ does not
// import any internal/ package (public API boundary).
//
// Known exception: pkg/hookwise/testing is allowed to import internal/core
// because the testing harness intentionally wraps core types to provide
// in-process guard evaluation for external test suites.
func TestArch_PkgDoesNotImportInternal(t *testing.T) {
	internalPath := modulePath + "/internal"
	pkgs := loadPackages(t, modulePath+"/pkg/...")

	// Allowed exceptions: pkg/hookwise/testing may import internal/core
	// because GuardTester deliberately wraps core.GuardRuleConfig and
	// core.Evaluate to provide in-process guard evaluation.
	allowed := map[string]map[string]bool{
		modulePath + "/pkg/hookwise/testing": {
			modulePath + "/internal/core": true,
		},
	}

	for _, pkg := range pkgs {
		forbidden := collectImports(pkg, internalPath)
		for _, imp := range forbidden {
			if allowed[pkg.PkgPath][imp] {
				continue
			}
			t.Errorf("PKG-BOUNDARY violation: %s imports %s (pkg/ must not import internal/)", pkg.PkgPath, imp)
		}
	}
}

// --------------------------------------------------------------------------
// Task 2.4: Cycle detection
// --------------------------------------------------------------------------

// TestArch_NoCyclicImports checks for circular import dependencies
// among internal packages. Go's compiler prevents direct cycles, but
// this test documents and enforces the constraint explicitly.
func TestArch_NoCyclicImports(t *testing.T) {
	pkgs := loadPackages(t, modulePath+"/internal/...")

	// Build adjacency map
	adj := make(map[string][]string)
	for _, pkg := range pkgs {
		for imp := range pkg.Imports {
			if strings.HasPrefix(imp, modulePath+"/internal/") {
				adj[pkg.PkgPath] = append(adj[pkg.PkgPath], imp)
			}
		}
	}

	// DFS cycle detection
	const (
		white = 0 // unvisited
		gray  = 1 // in-progress
		black = 2 // complete
	)
	color := make(map[string]int)
	var path []string

	var visit func(node string) bool
	visit = func(node string) bool {
		color[node] = gray
		path = append(path, node)
		for _, next := range adj[node] {
			switch color[next] {
			case gray:
				// Found cycle -- find the start of the cycle in path
				cycleStart := -1
				for i, p := range path {
					if p == next {
						cycleStart = i
						break
					}
				}
				cycle := append(path[cycleStart:], next)
				t.Errorf("CYCLE violation: circular import detected: %s", strings.Join(cycle, " -> "))
				return true
			case white:
				if visit(next) {
					return true
				}
			}
		}
		path = path[:len(path)-1]
		color[node] = black
		return false
	}

	for node := range adj {
		if color[node] == white {
			visit(node)
		}
	}
}
