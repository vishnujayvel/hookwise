package feeds

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	cwd, err := os.Getwd()
	if err != nil {
		return NewEnvelope("project", emptyProjectInfo()), nil
	}
	return NewEnvelope("project", ProjectInfo(ctx, cwd)), nil
}

// ProjectInfo derives git project metadata (name/branch/last_commit/dirty) for
// the given directory. It is a PURE function of `dir` — it does NOT call
// os.Getwd — so a caller that knows its own working directory gets data for
// THAT directory.
//
// This matters for the status line: project/branch is session-specific, but the
// shared daemon feed cache holds whichever repo the daemon last polled. Rendering
// from the cache showed a different session's repo/branch (#126). The status line
// calls ProjectInfo with its own cwd instead.
//
// Fail-open (ARCH-1): a non-repo or missing git yields empty fields, never an error.
func ProjectInfo(ctx context.Context, dir string) map[string]interface{} {
	repoRoot, err := gitOutput(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return emptyProjectInfo()
	}

	branch, _ := gitOutput(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	commit, _ := gitOutput(ctx, dir, "rev-parse", "--short", "HEAD")
	porcelain, perr := gitOutput(ctx, dir, "status", "--porcelain")

	info := map[string]interface{}{
		"name":        filepath.Base(repoRoot),
		"branch":      branch,
		"last_commit": commit,
		"dirty":       perr == nil && porcelain != "",
		// last_commit_ts is the committer epoch (seconds) of HEAD. The status
		// line renders it as a "Xm ago" suffix; nil when unavailable so the
		// suffix is simply omitted. Keeping the key always-present keeps the
		// shape uniform with the fixture (cross-boundary parity, issue #155).
		"last_commit_ts": nil,
	}
	if ctStr, cerr := gitOutput(ctx, dir, "log", "-1", "--format=%ct"); cerr == nil {
		if ts, perr := strconv.ParseInt(ctStr, 10, 64); perr == nil {
			info["last_commit_ts"] = ts
		}
	}
	return info
}

// emptyProjectInfo returns the zero-value project data (ARCH-1 fail-open shape).
func emptyProjectInfo() map[string]interface{} {
	return map[string]interface{}{
		"name":           "",
		"branch":         "",
		"last_commit":    "",
		"dirty":          false,
		"last_commit_ts": nil,
	}
}

// gitOutput executes a git command in the given directory and returns trimmed stdout.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ProjectTestFixture returns a deterministic project envelope for use in
// cross-package tests. Shared fixture so tests bind to actual field names.
func ProjectTestFixture() map[string]interface{} {
	return NewEnvelopeAt("project", map[string]interface{}{
		"name":           "hookwise",
		"branch":         "main",
		"last_commit":    "abc1234",
		"dirty":          false,
		"last_commit_ts": int64(1772877600), // 2026-03-07T10:00:00Z
	}, time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC))
}
