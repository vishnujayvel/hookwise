package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixFixture is a settings file exercising every planner branch at once:
// unknown JSON keys at every level, an exact duplicate (g0h1), a key-order
// duplicate that is canonically equal (g0h2), a near-duplicate differing
// only in timeout (g0h3, must NOT be removed), and a cross-group exact
// duplicate within the same file (g1h0). Arrays are single-line so expected
// post-splice content can be written as an exact literal.
const fixFixture = `{
  "unknownTop": {"keep": true, "n": 1},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "customGroupKey": "g1", "hooks": [{"type": "command", "command": "echo a", "timeout": 30, "custom": "x"}, {"type": "command", "command": "echo a", "timeout": 30, "custom": "x"}, {"timeout": 30, "command": "echo a", "type": "command", "custom": "x"}, {"type": "command", "command": "echo a", "timeout": 60}]},
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "echo a", "timeout": 30, "custom": "x"}]}
    ],
    "PostToolUse": [
      {"matcher": "", "hooks": [{"type": "command", "command": "echo b"}]}
    ]
  },
  "otherSetting": [1, 2, 3]
}`

// fixFixtureAfterAll is fixFixture with all three removable duplicates
// spliced out: g0 keeps its first occurrence and the timeout-60 variant,
// g1's array empties (the group itself is preserved).
const fixFixtureAfterAll = `{
  "unknownTop": {"keep": true, "n": 1},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "customGroupKey": "g1", "hooks": [{"type": "command", "command": "echo a", "timeout": 30, "custom": "x"}, {"type": "command", "command": "echo a", "timeout": 60}]},
      {"matcher": "Bash", "hooks": []}
    ],
    "PostToolUse": [
      {"matcher": "", "hooks": [{"type": "command", "command": "echo b"}]}
    ]
  },
  "otherSetting": [1, 2, 3]
}`

// fixFixtureAfterG1Only is fixFixture with only the cross-group duplicate
// (g1h0) removed; g0's intra-group duplicates remain untouched.
const fixFixtureAfterG1Only = `{
  "unknownTop": {"keep": true, "n": 1},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "customGroupKey": "g1", "hooks": [{"type": "command", "command": "echo a", "timeout": 30, "custom": "x"}, {"type": "command", "command": "echo a", "timeout": 30, "custom": "x"}, {"timeout": 30, "command": "echo a", "type": "command", "custom": "x"}, {"type": "command", "command": "echo a", "timeout": 60}]},
      {"matcher": "Bash", "hooks": []}
    ],
    "PostToolUse": [
      {"matcher": "", "hooks": [{"type": "command", "command": "echo b"}]}
    ]
  },
  "otherSetting": [1, 2, 3]
}`

// writeFixSettings writes content as settings.json in a temp dir.
func writeFixSettings(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// mustPlan plans over a single file and requires success.
func mustPlan(t *testing.T, path string) *FixPlan {
	t.Helper()
	plan, err := PlanDuplicateRemovals([]string{path})
	require.NoError(t, err)
	return plan
}

// backupFiles globs the "<settings>.bak-*" siblings of path.
func backupFiles(t *testing.T, path string) []string {
	t.Helper()
	matches, err := filepath.Glob(path + ".bak-*")
	require.NoError(t, err)
	return matches
}

func TestPlanDetectsExactIntraFileDuplicates(t *testing.T) {
	path := writeFixSettings(t, fixFixture)
	plan := mustPlan(t, path)

	require.Len(t, plan.Removals, 3, "g0h1, g0h2 (key-order variant), g1h0 are the exact duplicates")
	for _, r := range plan.Removals {
		assert.Equal(t, path, r.File)
		assert.Equal(t, "PreToolUse", r.Event)
		assert.Equal(t, "Bash", r.Matcher)
		assert.Equal(t, "echo a", r.Command)
		assert.False(t, strings.HasPrefix(r.ID, "PreToolUse#g0#h0#"),
			"the first occurrence must be kept, never offered for removal")
	}

	// The guard must fingerprint the file for the apply-time TOCTOU check.
	guard, ok := plan.Guards[path]
	require.True(t, ok)
	assert.NotEmpty(t, guard.SHA256)
	assert.EqualValues(t, len(fixFixture), guard.Size)
}

func TestPlanFullObjectInequalityIsRecommendationNotRemoval(t *testing.T) {
	path := writeFixSettings(t, fixFixture)
	plan := mustPlan(t, path)

	for _, r := range plan.Removals {
		assert.NotContains(t, r.ID, "#g0#h3#",
			"the timeout-60 variant differs in a field — must not be removable")
	}
	var nearDup bool
	for _, rec := range plan.Recommendations {
		if rec.Kind == "near-duplicate" {
			nearDup = true
			assert.Contains(t, rec.Message, "echo a")
		}
	}
	assert.True(t, nearDup, "full-object inequality must surface as a near-duplicate recommendation")
}

func TestPlanCrossFileRepeatIsRecommendationOnly(t *testing.T) {
	dir := t.TempDir()
	content := `{"hooks": {"PreToolUse": [{"matcher": "", "hooks": [{"type": "command", "command": "echo shared"}]}]}}`
	shared := filepath.Join(dir, "settings.json")
	local := filepath.Join(dir, "settings.local.json")
	require.NoError(t, os.WriteFile(shared, []byte(content), 0o600))
	require.NoError(t, os.WriteFile(local, []byte(content), 0o600))

	plan, err := PlanDuplicateRemovals([]string{shared, local})
	require.NoError(t, err)

	assert.Empty(t, plan.Removals,
		"a hook in both settings.json and settings.local.json is layering, never auto-removed")
	require.Len(t, plan.Recommendations, 1)
	assert.Equal(t, "cross-file", plan.Recommendations[0].Kind)
	assert.Contains(t, plan.Recommendations[0].Message, "echo shared")
}

func TestApplyAllAcceptedPreservesEverythingElseByteExact(t *testing.T) {
	path := writeFixSettings(t, fixFixture)
	plan := mustPlan(t, path)
	require.Len(t, plan.Removals, 3)

	ids := make([]string, 0, 3)
	for _, r := range plan.Removals {
		ids = append(ids, r.ID)
	}
	removed, backup, err := ApplyRemovals(path, ids, plan.Guards[path])
	require.NoError(t, err)
	assert.Equal(t, 3, removed)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, fixFixtureAfterAll, string(after),
		"everything not removed — unknown keys included — must survive byte-identical")

	// Backup must hold the exact original.
	require.NotEmpty(t, backup)
	bak, err := os.ReadFile(backup)
	require.NoError(t, err)
	assert.Equal(t, fixFixture, string(bak), "backup must be the pristine original")
}

func TestApplySubsetRemovesOnlyAccepted(t *testing.T) {
	path := writeFixSettings(t, fixFixture)
	plan := mustPlan(t, path)

	var g1ID string
	for _, r := range plan.Removals {
		if strings.HasPrefix(r.ID, "PreToolUse#g1#h0#") {
			g1ID = r.ID
		}
	}
	require.NotEmpty(t, g1ID, "cross-group duplicate must be in the plan")

	removed, _, err := ApplyRemovals(path, []string{g1ID}, plan.Guards[path])
	require.NoError(t, err)
	assert.Equal(t, 1, removed)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, fixFixtureAfterG1Only, string(after),
		"declined/unaccepted duplicates must remain byte-identical in place")

	// The remaining duplicates are still discoverable on a fresh plan.
	replan := mustPlan(t, path)
	assert.Len(t, replan.Removals, 2)
}

func TestApplyTOCTOURefusesWhenFileChanged(t *testing.T) {
	path := writeFixSettings(t, fixFixture)
	plan := mustPlan(t, path)
	require.NotEmpty(t, plan.Removals)

	// Same content, different mtime — a live session may have rewritten the
	// file to identical-looking-but-not-fingerprinted state; refuse anyway.
	require.NoError(t, os.Chtimes(path, time.Now().Add(time.Hour), time.Now().Add(time.Hour)))

	removed, backup, err := ApplyRemovals(path, []string{plan.Removals[0].ID}, plan.Guards[path])
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed since the plan")
	assert.Zero(t, removed)
	assert.Empty(t, backup, "refusal must happen before any backup/write")

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, fixFixture, string(after), "refused apply must not touch the file")
	assert.Empty(t, backupFiles(t, path))
}

func TestApplyContentChangeRefusedEvenWithSameSize(t *testing.T) {
	path := writeFixSettings(t, fixFixture)
	plan := mustPlan(t, path)
	require.NotEmpty(t, plan.Removals)
	info, err := os.Stat(path)
	require.NoError(t, err)

	// Same size, same mtime, different bytes — only sha256 catches this.
	mutated := []byte(strings.Replace(fixFixture, `"keep": true`, `"keep": zrue`, 1))
	require.Len(t, mutated, len(fixFixture))
	require.NoError(t, os.WriteFile(path, mutated, 0o600))
	require.NoError(t, os.Chtimes(path, info.ModTime(), info.ModTime()))

	_, _, err = ApplyRemovals(path, []string{plan.Removals[0].ID}, plan.Guards[path])
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed since the plan")
	assert.Empty(t, backupFiles(t, path))
}

func TestApplyRefusesKeeperOrUnknownID(t *testing.T) {
	path := writeFixSettings(t, fixFixture)
	plan := mustPlan(t, path)

	for _, badID := range []string{"PreToolUse#g0#h0#deadbeef", "nonsense"} {
		removed, backup, err := ApplyRemovals(path, []string{badID}, plan.Guards[path])
		require.Error(t, err, "ID %q must be refused", badID)
		assert.Contains(t, err.Error(), "not a removable duplicate")
		assert.Zero(t, removed)
		assert.Empty(t, backup)
	}

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, fixFixture, string(after))
}

func TestApplyCorruptedWriteRestoresBackup(t *testing.T) {
	path := writeFixSettings(t, fixFixture)
	plan := mustPlan(t, path)
	require.NotEmpty(t, plan.Removals)

	orig := applyWriteFile
	applyWriteFile = func(p string, _ []byte, perm os.FileMode) error {
		return atomicWriteFile(p, []byte(`{"hooks": `), perm) // truncated JSON
	}
	defer func() { applyWriteFile = orig }()

	removed, backup, err := ApplyRemovals(path, []string{plan.Removals[0].ID}, plan.Guards[path])
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restored")
	assert.Zero(t, removed)
	require.NotEmpty(t, backup)

	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, fixFixture, string(after),
		"a write that fails post-write validation must be rolled back from the backup")
}

func TestPlanIdempotentAfterFullApply(t *testing.T) {
	path := writeFixSettings(t, fixFixture)
	plan := mustPlan(t, path)
	ids := make([]string, 0, len(plan.Removals))
	for _, r := range plan.Removals {
		ids = append(ids, r.ID)
	}
	_, _, err := ApplyRemovals(path, ids, plan.Guards[path])
	require.NoError(t, err)

	replan := mustPlan(t, path)
	assert.Empty(t, replan.Removals, "second run must find nothing left to fix")
}

func TestPlanSkipsMalformedFileAndPlansNothingForIt(t *testing.T) {
	path := writeFixSettings(t, `{"hooks": {`)
	plan := mustPlan(t, path)
	assert.Empty(t, plan.Removals)
	require.Len(t, plan.SkippedFiles, 1)
	assert.Equal(t, path, plan.SkippedFiles[0].File)
	_, hasGuard := plan.Guards[path]
	assert.False(t, hasGuard, "no guard for a file we refuse to plan against")
}

func TestPlanMissingFilesAreSkippedSilently(t *testing.T) {
	plan, err := PlanDuplicateRemovals([]string{filepath.Join(t.TempDir(), "settings.json")})
	require.NoError(t, err)
	assert.Empty(t, plan.Removals)
	assert.Empty(t, plan.SkippedFiles)
}
