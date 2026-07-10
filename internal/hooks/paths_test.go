package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettingsPaths_GlobalAndLocal(t *testing.T) {
	claude := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(claude, "settings.json"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(claude, "settings.local.json"), []byte("{}"), 0o600))

	paths := SettingsPaths(claude)

	// Both user-level settings files are scanned: settings.json (shared) and
	// settings.local.json (personal overrides) — the latter is a common place
	// for hooks and was previously never scanned.
	assert.Contains(t, paths, filepath.Join(claude, "settings.json"))
	assert.Contains(t, paths, filepath.Join(claude, "settings.local.json"))
	assert.Equal(t, filepath.Join(claude, "settings.json"), paths[0],
		"settings.json must stay first — runWire targets paths[0]")
	assert.Len(t, paths, 2)
}

func TestSettingsPaths_DoesNotChaseProjectsTranscriptDir(t *testing.T) {
	// Regression guard against the old mock-confidence glob: ~/.claude/projects/*/
	// holds conversation transcripts (.jsonl), never settings.local.json, so a
	// glob there matched nothing in production while its test invented a fictional
	// structure. Even if such a file exists, SettingsPaths must NOT return it.
	claude := t.TempDir()
	dir := filepath.Join(claude, "projects", "encoded-project")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.local.json"), []byte("{}"), 0o600))

	paths := SettingsPaths(claude)

	assert.NotContains(t, paths, filepath.Join(dir, "settings.local.json"),
		"must not scan the projects/ transcript directory")
	assert.Len(t, paths, 2)
}

func TestSettingsPaths_UserLevelAlwaysListedEvenIfAbsent(t *testing.T) {
	claude := t.TempDir() // empty: no files
	paths := SettingsPaths(claude)
	// Both user-level paths are always returned (Scan tolerates missing files).
	require.Len(t, paths, 2)
	assert.Equal(t, filepath.Join(claude, "settings.json"), paths[0])
	assert.Equal(t, filepath.Join(claude, "settings.local.json"), paths[1])
}

func TestDefaultSettingsPaths_HonorsClaudeDirEnv(t *testing.T) {
	claude := t.TempDir()
	t.Setenv("HOOKWISE_CLAUDE_DIR", claude)
	// The override must point the default scan at the temp dir, not real ~/.claude.
	paths := DefaultSettingsPaths()
	require.NotEmpty(t, paths)
	assert.Equal(t, filepath.Join(claude, "settings.json"), paths[0])
}

func TestProjectSettingsPaths(t *testing.T) {
	project := t.TempDir()

	paths := ProjectSettingsPaths(project)

	assert.Equal(t, []string{
		filepath.Join(project, ".claude", "settings.json"),
		filepath.Join(project, ".claude", "settings.local.json"),
	}, paths, "project scan targets the <dir>/.claude settings pair, settings.json first")
}
