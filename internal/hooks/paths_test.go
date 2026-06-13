package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettingsPaths_GlobalAndProjects(t *testing.T) {
	claude := t.TempDir()
	// Global settings.
	require.NoError(t, os.WriteFile(filepath.Join(claude, "settings.json"), []byte("{}"), 0o600))
	// Two project settings.local.json files.
	for _, proj := range []string{"projA", "projB"} {
		dir := filepath.Join(claude, "projects", proj)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.local.json"), []byte("{}"), 0o600))
	}

	paths := SettingsPaths(claude)

	assert.Contains(t, paths, filepath.Join(claude, "settings.json"))
	assert.Contains(t, paths, filepath.Join(claude, "projects", "projA", "settings.local.json"))
	assert.Contains(t, paths, filepath.Join(claude, "projects", "projB", "settings.local.json"))
	assert.Len(t, paths, 3)
}

func TestSettingsPaths_GlobalAlwaysListedEvenIfAbsent(t *testing.T) {
	claude := t.TempDir() // empty: no files
	paths := SettingsPaths(claude)
	// Global path is always returned (Scan tolerates it being missing).
	require.Len(t, paths, 1)
	assert.Equal(t, filepath.Join(claude, "settings.json"), paths[0])
}
