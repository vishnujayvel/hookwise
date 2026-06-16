package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishnujayvel/hookwise/internal/feeds"
)

// initStatusTestRepo creates a temp git repo with one commit on the given branch.
func initStatusTestRepo(t *testing.T, branch string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hookwise-sl-proj-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	gitInitRepoAt(t, dir, branch)
	return dir
}

// gitInitRepoAt initialises a git repo with one commit on `branch` in `dir`.
// Shared with cli_test.go so status-line tests can make their cwd a real repo.
func gitInitRepoAt(t *testing.T, dir, branch string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, gerr := cmd.CombinedOutput()
		require.NoError(t, gerr, "git %v: %s", args, out)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o600))
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-q", "-m", "init"},
		{"checkout", "-q", "-b", branch},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, gerr := cmd.CombinedOutput()
		require.NoError(t, gerr, "git %v: %s", args, out)
	}
}

// TestOverlayLiveProject_OverwritesStaleCache reproduces #126: the shared daemon
// cache holds another session's repo, but the rendered project segment must show
// THIS directory's repo/branch, not the stale cached one.
func TestOverlayLiveProject_OverwritesStaleCache(t *testing.T) {
	repo := initStatusTestRepo(t, "live-branch")

	// Simulate a fresh-but-wrong daemon cache entry from a different repo.
	staleCache := map[string]interface{}{
		"project": feeds.NewEnvelope("project", map[string]interface{}{
			"name":   "some-other-repo",
			"branch": "stale-branch",
		}),
	}

	out := overlayLiveProject(staleCache, repo)
	seg := renderProjectSegment(out)

	assert.Contains(t, seg, filepath.Base(repo), "should show this repo's name")
	assert.Contains(t, seg, "live-branch", "should show this repo's branch")
	assert.NotContains(t, seg, "some-other-repo", "must not show the stale cached repo")
	assert.NotContains(t, seg, "stale-branch", "must not show the stale cached branch")
}

// TestOverlayLiveProject_NilCache verifies the helper tolerates a nil cache
// (CollectFeedCache can return nil) and still renders live project data.
func TestOverlayLiveProject_NilCache(t *testing.T) {
	repo := initStatusTestRepo(t, "dev-trunk")

	out := overlayLiveProject(nil, repo)
	require.NotNil(t, out)

	seg := renderProjectSegment(out)
	assert.Contains(t, seg, filepath.Base(repo))
}
