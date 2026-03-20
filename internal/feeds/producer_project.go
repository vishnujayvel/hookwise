package feeds

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

// ProjectProducer returns project directory info by running git commands.
// It implements ConfigAware to receive feed configuration.
type ProjectProducer struct {
	mu       sync.Mutex
	feedsCfg core.FeedsConfig
}

func (p *ProjectProducer) Name() string { return "project" }

// SetFeedsConfig receives the feed configuration (ConfigAware interface).
func (p *ProjectProducer) SetFeedsConfig(cfg core.FeedsConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.feedsCfg = cfg
}

func (p *ProjectProducer) Produce(ctx context.Context) (interface{}, error) {
	// Add timeout to prevent blocking the poll cycle on slow git operations.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cwd, err := os.Getwd()
	if err != nil {
		return p.fallbackResult()
	}

	// Detect git repo root.
	repoRoot, err := p.runGit(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		// Not a git repo or git not installed — fail-open (ARCH-1).
		return p.fallbackResult()
	}

	repoName := filepath.Base(repoRoot)

	// Get current branch.
	branchName, err := p.runGit(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		branchName = ""
	}

	// Get short commit hash.
	commitHash, err := p.runGit(ctx, cwd, "rev-parse", "--short", "HEAD")
	if err != nil {
		commitHash = ""
	}

	// Detect dirty state.
	porcelain, err := p.runGit(ctx, cwd, "status", "--porcelain")
	isDirty := err == nil && porcelain != ""

	return NewEnvelope("project", map[string]interface{}{
		"name":        repoName,
		"branch":      branchName,
		"last_commit": commitHash,
		"dirty":       isDirty,
	}), nil
}

// runGit executes a git command in the given directory and returns trimmed stdout.
func (p *ProjectProducer) runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// fallbackResult returns a valid envelope with empty fields when git is unavailable (ARCH-1: fail-open).
func (p *ProjectProducer) fallbackResult() (map[string]interface{}, error) {
	return NewEnvelope("project", map[string]interface{}{
		"name":        "",
		"branch":      "",
		"last_commit": "",
		"dirty":       false,
	}), nil
}

// ProjectTestFixture returns a deterministic project envelope for use in
// cross-package tests. Shared fixture so tests bind to actual field names.
func ProjectTestFixture() map[string]interface{} {
	return NewEnvelopeAt("project", map[string]interface{}{
		"name":        "hookwise",
		"branch":      "main",
		"last_commit": "abc1234",
		"dirty":       false,
	}, time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC))
}
