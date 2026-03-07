//go:build mutation

package mutation

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

// Mutant describes a single source-level mutation.
type Mutant struct {
	File     string // source file path relative to module root
	Line     int    // line number of the mutation site
	Original string // original code fragment
	Mutated  string // mutated code fragment
	Operator string // mutation operator name
}

// MutationResult captures the outcome of running the test suite against one mutant.
type MutationResult struct {
	Mutant  Mutant
	Killed  bool   // true if the test suite caught the mutation
	Timeout bool   // true if the test timed out
	Error   string // error message if test execution failed unexpectedly
}

// MutationReport aggregates results across all mutants.
type MutationReport struct {
	Target    string  // package under test
	Total     int
	Killed    int
	Survived  int
	Timeouts  int
	Score     float64 // killed / total * 100
	Survivors []MutationResult
}

// ---------------------------------------------------------------------------
// Module copy helper
// ---------------------------------------------------------------------------

// copyModule copies the entire Go module rooted at src into dst.
// It skips .git, vendor, node_modules, and hidden dirs to keep the copy fast.
func copyModule(src, dst string) error {
	return filepath.Walk(src, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Skip directories that are not needed for `go test`.
		if info.IsDir() {
			base := filepath.Base(rel)
			switch base {
			case ".git", "vendor", "node_modules", ".claude", ".playwright-mcp",
				"tui", "screenshots", "hooks", "recipes", "examples", "docs":
				return filepath.SkipDir
			}
			if strings.HasPrefix(base, ".") && rel != "." {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), info.Mode()|0o700)
		}

		return copyFile(filepath.Join(dst, rel), path, info.Mode())
	})
}

func copyFile(dst, src string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// ---------------------------------------------------------------------------
// AST mutation operators
// ---------------------------------------------------------------------------

// mutationFunc applies a single mutation to the AST.
// The applyIdx parameter controls which site to actually mutate (-1 = enumerate only).
// Returns the list of Mutant descriptors found.
type mutationFunc func(fset *token.FileSet, file *ast.File, relPath string, applyIdx int) []Mutant

// swapOperatorMutations swaps the *function call* used for each string-comparison
// operator in the EvaluateCondition switch body inside guards.go.
//
// Instead of swapping the case label strings (which causes duplicate-case compile
// errors), we swap the function being called in the case body:
//
//	strings.Contains  -> strings.HasPrefix
//	strings.HasPrefix -> strings.HasSuffix
//	strings.HasSuffix -> strings.Contains
//
// This produces compilable mutants that should be killed by operator-specific tests.
func swapOperatorMutations(fset *token.FileSet, file *ast.File, relPath string, applyIdx int) []Mutant {
	swapMap := map[string]string{
		"Contains":  "HasPrefix",
		"HasPrefix": "HasSuffix",
		"HasSuffix": "Contains",
	}

	var mutants []Mutant
	siteIdx := 0

	ast.Inspect(file, func(n ast.Node) bool {
		// Find calls like strings.Contains(...), strings.HasPrefix(...), etc.
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != "strings" {
			return true
		}

		replacement, found := swapMap[sel.Sel.Name]
		if !found {
			return true
		}

		pos := fset.Position(sel.Pos())
		m := Mutant{
			File:     relPath,
			Line:     pos.Line,
			Original: "strings." + sel.Sel.Name,
			Mutated:  "strings." + replacement,
			Operator: "swap_operator",
		}

		if applyIdx == siteIdx {
			sel.Sel.Name = replacement
		}

		mutants = append(mutants, m)
		siteIdx++
		return true
	})
	return mutants
}

// swapStringLiteralMutations swaps "allow" <-> "block" in string literals.
// Primary target: ActionAllow/ActionBlock constants in handlers.go.
func swapStringLiteralMutations(fset *token.FileSet, file *ast.File, relPath string, applyIdx int) []Mutant {
	swapMap := map[string]string{
		`"allow"`: `"block"`,
		`"block"`: `"allow"`,
	}

	var mutants []Mutant
	siteIdx := 0

	ast.Inspect(file, func(n ast.Node) bool {
		bl, ok := n.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return true
		}
		replacement, found := swapMap[bl.Value]
		if !found {
			return true
		}
		pos := fset.Position(bl.Pos())
		m := Mutant{
			File:     relPath,
			Line:     pos.Line,
			Original: bl.Value,
			Mutated:  replacement,
			Operator: "swap_string_literal",
		}
		if applyIdx == siteIdx {
			bl.Value = replacement
		}
		mutants = append(mutants, m)
		siteIdx++
		return true
	})
	return mutants
}

// negateConditionMutations negates boolean expressions in if-statement conditions
// within the Evaluate and EvaluateCondition functions.
func negateConditionMutations(fset *token.FileSet, file *ast.File, relPath string, applyIdx int) []Mutant {
	var mutants []Mutant
	siteIdx := 0

	ast.Inspect(file, func(n ast.Node) bool {
		funcDecl, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		fname := funcDecl.Name.Name
		if fname != "Evaluate" && fname != "EvaluateCondition" {
			return false
		}

		ast.Inspect(funcDecl.Body, func(inner ast.Node) bool {
			ifStmt, ok := inner.(*ast.IfStmt)
			if !ok {
				return true
			}
			cond := ifStmt.Cond
			if cond == nil {
				return true
			}

			// Only target unary-not and binary expressions (the interesting logic).
			switch cond.(type) {
			case *ast.UnaryExpr, *ast.BinaryExpr:
				// good targets
			default:
				return true
			}

			pos := fset.Position(cond.Pos())
			var origBuf bytes.Buffer
			printer.Fprint(&origBuf, fset, cond)

			m := Mutant{
				File:     relPath,
				Line:     pos.Line,
				Original: origBuf.String(),
				Mutated:  "!(" + origBuf.String() + ")",
				Operator: "negate_condition",
			}

			if applyIdx == siteIdx {
				ifStmt.Cond = &ast.UnaryExpr{
					Op: token.NOT,
					X:  &ast.ParenExpr{X: cond},
				}
			}

			mutants = append(mutants, m)
			siteIdx++
			return true
		})
		return false
	})
	return mutants
}

// ---------------------------------------------------------------------------
// Enumerate mutation sites
// ---------------------------------------------------------------------------

type operatorEntry struct {
	name string
	fn   mutationFunc
}

func enumerateMutants(moduleRoot, relPath string, operators []operatorEntry) ([]Mutant, error) {
	absPath := filepath.Join(moduleRoot, relPath)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", relPath, err)
	}

	var all []Mutant
	for _, op := range operators {
		mutants := op.fn(fset, file, relPath, -1)
		all = append(all, mutants...)
	}
	return all, nil
}

// ---------------------------------------------------------------------------
// Apply a single mutation
// ---------------------------------------------------------------------------

func applyMutation(tmpDir, relPath string, op operatorEntry, siteIdx int) error {
	absPath := filepath.Join(tmpDir, relPath)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse for mutation: %w", err)
	}

	op.fn(fset, file, relPath, siteIdx)

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, file); err != nil {
		return fmt.Errorf("print mutated AST: %w", err)
	}

	return os.WriteFile(absPath, buf.Bytes(), 0o644)
}

// ---------------------------------------------------------------------------
// Run test suite against a mutant
// ---------------------------------------------------------------------------

func runTestsAgainstMutant(tmpDir string, timeout time.Duration) MutationResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "./internal/core/...")
	cmd.Dir = tmpDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return MutationResult{Timeout: true}
	}

	if err != nil {
		// Check if go test reported its own internal timeout
		stderrStr := stderr.String()
		if strings.Contains(stderrStr, "test timed out") ||
			strings.Contains(stderrStr, "panic: test timed out") {
			return MutationResult{Timeout: true}
		}
		// Test failed (or compile failed) = mutation was killed.
		return MutationResult{Killed: true}
	}

	// Tests passed = mutation survived.
	return MutationResult{Killed: false}
}

// ---------------------------------------------------------------------------
// Test 5.1: AST Mutation Engine
// ---------------------------------------------------------------------------

func TestASTMutationEngine(t *testing.T) {
	moduleRoot := findModuleRoot(t)

	t.Run("swap_operator enumeration", func(t *testing.T) {
		ops := []operatorEntry{{name: "swap_operator", fn: swapOperatorMutations}}
		mutants, err := enumerateMutants(moduleRoot, "internal/core/guards.go", ops)
		if err != nil {
			t.Fatalf("enumerate: %v", err)
		}
		if len(mutants) == 0 {
			t.Fatal("expected at least one swap_operator mutation site")
		}
		for _, m := range mutants {
			t.Logf("  site: %s:%d  %s -> %s", m.File, m.Line, m.Original, m.Mutated)
		}
		t.Logf("found %d swap_operator sites", len(mutants))
	})

	t.Run("swap_string_literal enumeration on handlers.go", func(t *testing.T) {
		ops := []operatorEntry{{name: "swap_string_literal", fn: swapStringLiteralMutations}}
		mutants, err := enumerateMutants(moduleRoot, "internal/core/handlers.go", ops)
		if err != nil {
			t.Fatalf("enumerate: %v", err)
		}
		if len(mutants) == 0 {
			t.Fatal("expected at least one swap_string_literal site in handlers.go")
		}
		for _, m := range mutants {
			t.Logf("  site: %s:%d  %s -> %s", m.File, m.Line, m.Original, m.Mutated)
		}
		t.Logf("found %d swap_string_literal sites", len(mutants))
	})

	t.Run("negate_condition enumeration", func(t *testing.T) {
		ops := []operatorEntry{{name: "negate_condition", fn: negateConditionMutations}}
		mutants, err := enumerateMutants(moduleRoot, "internal/core/guards.go", ops)
		if err != nil {
			t.Fatalf("enumerate: %v", err)
		}
		if len(mutants) == 0 {
			t.Fatal("expected at least one negate_condition mutation site")
		}
		for _, m := range mutants {
			t.Logf("  site: %s:%d  %s -> !(%s)", m.File, m.Line, m.Original, m.Original)
		}
		t.Logf("found %d negate_condition sites", len(mutants))
	})

	t.Run("AST round-trip preserves compilability", func(t *testing.T) {
		// Apply swap_string_literal mutation 0 on handlers.go in a temp dir
		// and verify it still compiles.
		tmpDir := t.TempDir()
		if err := copyModule(moduleRoot, tmpDir); err != nil {
			t.Fatalf("copy module: %v", err)
		}

		op := operatorEntry{name: "swap_string_literal", fn: swapStringLiteralMutations}
		if err := applyMutation(tmpDir, "internal/core/handlers.go", op, 0); err != nil {
			t.Fatalf("apply mutation: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "./internal/core/...")
		cmd.Dir = tmpDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mutated code does not compile:\n%s", out)
		}
		t.Log("mutated AST round-trip compiles successfully")
	})

	t.Run("swap_operator round-trip preserves compilability", func(t *testing.T) {
		// Apply swap_operator mutation 0 on guards.go and verify compilation.
		tmpDir := t.TempDir()
		if err := copyModule(moduleRoot, tmpDir); err != nil {
			t.Fatalf("copy module: %v", err)
		}

		op := operatorEntry{name: "swap_operator", fn: swapOperatorMutations}
		if err := applyMutation(tmpDir, "internal/core/guards.go", op, 0); err != nil {
			t.Fatalf("apply mutation: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "./internal/core/...")
		cmd.Dir = tmpDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mutated code does not compile:\n%s", out)
		}
		t.Log("swap_operator round-trip compiles successfully")
	})
}

// ---------------------------------------------------------------------------
// Test 5.2: Mutation Test Runner and Result Reporter
// ---------------------------------------------------------------------------

func TestMutationRunner(t *testing.T) {
	moduleRoot := findModuleRoot(t)

	type target struct {
		relPath   string
		operators []operatorEntry
	}
	targets := []target{
		{
			relPath: "internal/core/guards.go",
			operators: []operatorEntry{
				{name: "swap_operator", fn: swapOperatorMutations},
				{name: "negate_condition", fn: negateConditionMutations},
			},
		},
		{
			relPath: "internal/core/handlers.go",
			operators: []operatorEntry{
				{name: "swap_string_literal", fn: swapStringLiteralMutations},
			},
		},
	}

	type siteInfo struct {
		relPath string
		op      operatorEntry
		siteIdx int
		mutant  Mutant
	}
	var sites []siteInfo

	for _, tgt := range targets {
		for _, op := range tgt.operators {
			mutants, err := enumerateMutants(moduleRoot, tgt.relPath, []operatorEntry{op})
			if err != nil {
				t.Fatalf("enumerate %s/%s: %v", tgt.relPath, op.name, err)
			}
			for i, m := range mutants {
				sites = append(sites, siteInfo{
					relPath: tgt.relPath,
					op:      op,
					siteIdx: i,
					mutant:  m,
				})
			}
		}
	}

	if len(sites) == 0 {
		t.Fatal("no mutation sites found")
	}
	t.Logf("discovered %d mutation sites", len(sites))

	var results []MutationResult
	perMutantTimeout := 60 * time.Second

	for idx, site := range sites {
		t.Logf("[%d/%d] %s:%d %s  %s -> %s",
			idx+1, len(sites),
			site.mutant.File, site.mutant.Line,
			site.mutant.Operator,
			site.mutant.Original, site.mutant.Mutated,
		)

		tmpDir := t.TempDir()
		if err := copyModule(moduleRoot, tmpDir); err != nil {
			t.Fatalf("copy module for mutant %d: %v", idx, err)
		}

		if err := applyMutation(tmpDir, site.relPath, site.op, site.siteIdx); err != nil {
			t.Logf("  SKIP (apply error): %v", err)
			results = append(results, MutationResult{
				Mutant: site.mutant,
				Error:  err.Error(),
			})
			continue
		}

		result := runTestsAgainstMutant(tmpDir, perMutantTimeout)
		result.Mutant = site.mutant

		status := "SURVIVED"
		if result.Killed {
			status = "KILLED"
		} else if result.Timeout {
			status = "TIMEOUT"
		}
		t.Logf("  -> %s", status)

		results = append(results, result)
	}

	report := buildReport("./internal/core/...", results)
	logReport(t, report)

	if report.Total == 0 {
		t.Fatal("no mutants were tested")
	}
	t.Logf("MUTATION SCORE: %.1f%% (%d killed, %d survived, %d timeouts out of %d total)",
		report.Score, report.Killed, report.Survived, report.Timeouts, report.Total)
}

// ---------------------------------------------------------------------------
// Test 5.3: Target Guard Logic Mutation Verification
// ---------------------------------------------------------------------------

func TestGuardMutationVerification(t *testing.T) {
	moduleRoot := findModuleRoot(t)
	perMutantTimeout := 60 * time.Second

	// 5.3a: Operator swap — strings.Contains <-> strings.HasPrefix <-> strings.HasSuffix
	t.Run("operator_swap_kills", func(t *testing.T) {
		op := operatorEntry{name: "swap_operator", fn: swapOperatorMutations}
		mutants, err := enumerateMutants(moduleRoot, "internal/core/guards.go", []operatorEntry{op})
		if err != nil {
			t.Fatalf("enumerate: %v", err)
		}
		if len(mutants) == 0 {
			t.Fatal("no swap_operator mutation sites found")
		}

		var killed, survived int
		for i, m := range mutants {
			tmpDir := t.TempDir()
			if err := copyModule(moduleRoot, tmpDir); err != nil {
				t.Fatalf("copy module: %v", err)
			}
			if err := applyMutation(tmpDir, "internal/core/guards.go", op, i); err != nil {
				t.Fatalf("apply mutation %d: %v", i, err)
			}

			result := runTestsAgainstMutant(tmpDir, perMutantTimeout)
			if result.Killed {
				killed++
				t.Logf("  KILLED: %s:%d %s -> %s", m.File, m.Line, m.Original, m.Mutated)
			} else {
				survived++
				t.Logf("  SURVIVED: %s:%d %s -> %s", m.File, m.Line, m.Original, m.Mutated)
			}
		}
		t.Logf("operator_swap: %d/%d killed", killed, len(mutants))
		if killed == 0 {
			t.Error("expected at least one operator swap mutation to be killed")
		}
	})

	// 5.3b: Action string literal swap — "allow" <-> "block" in handlers.go constants
	t.Run("action_swap_kills", func(t *testing.T) {
		op := operatorEntry{name: "swap_string_literal", fn: swapStringLiteralMutations}
		mutants, err := enumerateMutants(moduleRoot, "internal/core/handlers.go", []operatorEntry{op})
		if err != nil {
			t.Fatalf("enumerate: %v", err)
		}
		if len(mutants) == 0 {
			t.Fatal("no swap_string_literal mutation sites found in handlers.go")
		}

		var killed, survived int
		for i, m := range mutants {
			tmpDir := t.TempDir()
			if err := copyModule(moduleRoot, tmpDir); err != nil {
				t.Fatalf("copy module: %v", err)
			}
			if err := applyMutation(tmpDir, "internal/core/handlers.go", op, i); err != nil {
				t.Fatalf("apply mutation %d: %v", i, err)
			}

			result := runTestsAgainstMutant(tmpDir, perMutantTimeout)
			if result.Killed {
				killed++
				t.Logf("  KILLED: %s:%d %s -> %s", m.File, m.Line, m.Original, m.Mutated)
			} else {
				survived++
				t.Logf("  SURVIVED: %s:%d %s -> %s", m.File, m.Line, m.Original, m.Mutated)
			}
		}
		t.Logf("action_swap (handlers.go): %d/%d killed", killed, len(mutants))
		if killed == 0 {
			t.Error("expected at least one action swap mutation to be killed")
		}
	})

	// 5.3c: Negate conditions in Evaluate/EvaluateCondition
	t.Run("negate_condition_kills", func(t *testing.T) {
		op := operatorEntry{name: "negate_condition", fn: negateConditionMutations}
		mutants, err := enumerateMutants(moduleRoot, "internal/core/guards.go", []operatorEntry{op})
		if err != nil {
			t.Fatalf("enumerate: %v", err)
		}
		if len(mutants) == 0 {
			t.Fatal("no negate_condition mutation sites found")
		}

		var killed, survived int
		for i, m := range mutants {
			tmpDir := t.TempDir()
			if err := copyModule(moduleRoot, tmpDir); err != nil {
				t.Fatalf("copy module: %v", err)
			}
			if err := applyMutation(tmpDir, "internal/core/guards.go", op, i); err != nil {
				t.Fatalf("apply mutation %d: %v", i, err)
			}

			result := runTestsAgainstMutant(tmpDir, perMutantTimeout)
			if result.Killed {
				killed++
				t.Logf("  KILLED: %s:%d %s", m.File, m.Line, m.Original)
			} else {
				survived++
				t.Logf("  SURVIVED: %s:%d %s", m.File, m.Line, m.Original)
			}
		}
		t.Logf("negate_condition: %d/%d killed", killed, len(mutants))
		if killed == 0 {
			t.Error("expected at least one negate_condition mutation to be killed")
		}
	})

	// 5.3d: Canary mutation — trivially killable
	t.Run("canary_mutation", func(t *testing.T) {
		// Canary: swap ActionAllow = "allow" -> "block" in handlers.go.
		// This changes the default allow action to block, which is caught by
		// TestEvaluate_NoMatchReturnsAllow and many other tests.
		op := operatorEntry{name: "swap_string_literal", fn: swapStringLiteralMutations}
		mutants, err := enumerateMutants(moduleRoot, "internal/core/handlers.go", []operatorEntry{op})
		if err != nil {
			t.Fatalf("enumerate canary: %v", err)
		}

		// Find the ActionAllow = "allow" -> "block" mutation.
		canaryIdx := -1
		for i, m := range mutants {
			if m.Original == `"allow"` && m.Mutated == `"block"` {
				canaryIdx = i
				break
			}
		}
		if canaryIdx == -1 {
			t.Fatal("canary mutation site not found: expected \"allow\" -> \"block\" in handlers.go")
		}

		tmpDir := t.TempDir()
		if err := copyModule(moduleRoot, tmpDir); err != nil {
			t.Fatalf("copy module: %v", err)
		}
		if err := applyMutation(tmpDir, "internal/core/handlers.go", op, canaryIdx); err != nil {
			t.Fatalf("apply canary mutation: %v", err)
		}

		result := runTestsAgainstMutant(tmpDir, perMutantTimeout)
		if !result.Killed {
			t.Fatal("CANARY FAILED: swapping ActionAllow from 'allow' to 'block' was NOT killed — mutation framework may be broken")
		}
		t.Log("canary mutation killed successfully — framework is working")
	})

	// 5.3e: Overall mutation score across all operators and files
	t.Run("overall_score", func(t *testing.T) {
		type targetOp struct {
			relPath string
			op      operatorEntry
		}
		allOps := []targetOp{
			{"internal/core/guards.go", operatorEntry{"swap_operator", swapOperatorMutations}},
			{"internal/core/guards.go", operatorEntry{"negate_condition", negateConditionMutations}},
			{"internal/core/handlers.go", operatorEntry{"swap_string_literal", swapStringLiteralMutations}},
		}

		var allResults []MutationResult
		totalSites := 0

		for _, top := range allOps {
			mutants, err := enumerateMutants(moduleRoot, top.relPath, []operatorEntry{top.op})
			if err != nil {
				t.Fatalf("enumerate %s/%s: %v", top.relPath, top.op.name, err)
			}

			for i, m := range mutants {
				totalSites++
				t.Logf("[%d] %s:%d %s  %s -> %s",
					totalSites, m.File, m.Line, m.Operator, m.Original, m.Mutated)

				tmpDir := t.TempDir()
				if err := copyModule(moduleRoot, tmpDir); err != nil {
					t.Fatalf("copy module: %v", err)
				}
				if err := applyMutation(tmpDir, top.relPath, top.op, i); err != nil {
					t.Logf("  SKIP (apply error): %v", err)
					allResults = append(allResults, MutationResult{
						Mutant: m,
						Error:  err.Error(),
					})
					continue
				}

				result := runTestsAgainstMutant(tmpDir, perMutantTimeout)
				result.Mutant = m

				status := "SURVIVED"
				if result.Killed {
					status = "KILLED"
				} else if result.Timeout {
					status = "TIMEOUT"
				}
				t.Logf("  -> %s", status)
				allResults = append(allResults, result)
			}
		}

		report := buildReport("./internal/core/...", allResults)
		logReport(t, report)
	})
}

// ---------------------------------------------------------------------------
// Report builder & logger
// ---------------------------------------------------------------------------

func buildReport(target string, results []MutationResult) MutationReport {
	r := MutationReport{Target: target}
	for _, res := range results {
		switch {
		case res.Error != "":
			continue // framework errors are not counted
		case res.Timeout:
			r.Timeouts++
		case res.Killed:
			r.Killed++
		default:
			r.Survived++
			r.Survivors = append(r.Survivors, res)
		}
	}
	r.Total = r.Killed + r.Survived + r.Timeouts
	if r.Total > 0 {
		r.Score = float64(r.Killed) / float64(r.Total) * 100
	}
	return r
}

func logReport(t *testing.T, r MutationReport) {
	t.Helper()
	t.Logf("=== MUTATION REPORT ===")
	t.Logf("Target:   %s", r.Target)
	t.Logf("Total:    %d", r.Total)
	t.Logf("Killed:   %d", r.Killed)
	t.Logf("Survived: %d", r.Survived)
	t.Logf("Timeouts: %d", r.Timeouts)
	t.Logf("Score:    %.1f%%", r.Score)

	if len(r.Survivors) > 0 {
		t.Logf("")
		t.Logf("--- Surviving Mutants ---")
		for _, s := range r.Survivors {
			t.Logf("  %s:%d [%s] %s -> %s",
				s.Mutant.File, s.Mutant.Line,
				s.Mutant.Operator,
				s.Mutant.Original, s.Mutant.Mutated,
			)
			if s.Error != "" {
				t.Logf("    error: %s", s.Error)
			}
		}
	}
	t.Logf("=======================")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// findModuleRoot locates the Go module root by querying `go env GOMOD` or
// walking up the directory tree to find go.mod.
func findModuleRoot(t *testing.T) string {
	t.Helper()

	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	if err == nil {
		mod := strings.TrimSpace(string(gomod))
		if mod != "" && mod != os.DevNull {
			return filepath.Dir(mod)
		}
	}

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
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
