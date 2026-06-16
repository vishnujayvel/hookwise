package feeds

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initProjectTestRepo creates a temp git repo with one commit on the given
// branch and returns its directory.
func initProjectTestRepo(t *testing.T, branch string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hookwise-proj-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	runGitT(t, dir, "init", "-q")
	runGitT(t, dir, "config", "user.email", "test@example.com")
	runGitT(t, dir, "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o600))
	runGitT(t, dir, "add", ".")
	runGitT(t, dir, "commit", "-q", "-m", "init")
	runGitT(t, dir, "checkout", "-q", "-b", branch)
	return dir
}

func runGitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// TestProjectInfo_DerivesFromGivenDir is the core of #126: ProjectInfo is a pure
// function of its `dir` argument, so two different repos queried in the SAME
// process yield each repo's own name/branch with no shared/cached contamination.
func TestProjectInfo_DerivesFromGivenDir(t *testing.T) {
	ctx := context.Background()
	repoA := initProjectTestRepo(t, "feature-a")
	repoB := initProjectTestRepo(t, "release-b")

	a := ProjectInfo(ctx, repoA)
	assert.Equal(t, filepath.Base(repoA), a["name"])
	assert.Equal(t, "feature-a", a["branch"])

	b := ProjectInfo(ctx, repoB)
	assert.Equal(t, filepath.Base(repoB), b["name"])
	assert.Equal(t, "release-b", b["branch"])

	// Same process, different dirs → different results (no global state).
	assert.NotEqual(t, a["name"], b["name"])
	assert.NotEqual(t, a["branch"], b["branch"])
}

// TestProjectInfo_NonRepoFailsOpen verifies ARCH-1: a non-repo directory yields
// empty fields, never an error or panic.
func TestProjectInfo_NonRepoFailsOpen(t *testing.T) {
	dir, err := os.MkdirTemp("", "hookwise-nonrepo-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	info := ProjectInfo(context.Background(), dir)
	assert.Equal(t, "", info["name"])
	assert.Equal(t, "", info["branch"])
	assert.Equal(t, false, info["dirty"])
}
