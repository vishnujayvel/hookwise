package feeds

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeMemoryFile creates a memory markdown file under <base>/projName/memory/filename.
func makeMemoryFile(t *testing.T, baseDir, projName, filename, content string) string {
	t.Helper()
	dir := filepath.Join(baseDir, projName, "memory")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, filename)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// ---------------------------------------------------------------------------
// Test A: Returns both memories, newest first, correct titles, total_count
// ---------------------------------------------------------------------------

func TestMemoriesProducer_ReturnsBothMemoriesNewestFirst(t *testing.T) {
	tmpDir := t.TempDir()

	// Write projA/memory/a.md with frontmatter name.
	pathA := makeMemoryFile(t, tmpDir, "projA", "a.md", "---\nname: alpha\n---\nSome content.\n")
	// Write projB/memory/b.md with H1 title.
	pathB := makeMemoryFile(t, tmpDir, "projB", "b.md", "# Beta Title\n\nMore content.\n")

	// Set distinct mod times so sort order is deterministic.
	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(pathA, older, older))
	require.NoError(t, os.Chtimes(pathB, newer, newer))

	p := &MemoriesProducer{dir: tmpDir}

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "ARCH-1: must not return error")

	envelope, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "memories", envelope["type"])
	assert.NotEmpty(t, envelope["timestamp"])

	data, ok := envelope["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "claude-memory", data["source"])
	assert.Equal(t, 2, toInt(data["total_count"]))

	recent, ok := data["recent_memories"].([]interface{})
	require.True(t, ok)
	require.Len(t, recent, 2, "both memories should appear")

	// Newest first: b.md (Beta Title) should come before a.md (alpha).
	first := recent[0].(map[string]interface{})
	assert.Equal(t, "Beta Title", first["title"])
	assert.NotEmpty(t, first["modified"])

	second := recent[1].(map[string]interface{})
	assert.Equal(t, "alpha", second["title"])
	assert.NotEmpty(t, second["modified"])
}

// ---------------------------------------------------------------------------
// Test B: Missing/nonexistent dir → empty result, no error
// ---------------------------------------------------------------------------

func TestMemoriesProducer_MissingDir_ReturnsEmpty(t *testing.T) {
	nonexistent := filepath.Join(t.TempDir(), "does-not-exist")

	p := &MemoriesProducer{dir: nonexistent}

	result, err := p.Produce(context.Background())
	require.NoError(t, err, "missing dir must not return an error (ARCH-1)")

	envelope, ok := result.(map[string]interface{})
	require.True(t, ok)

	data, ok := envelope["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "claude-memory", data["source"])
	assert.Equal(t, 0, toInt(data["total_count"]))

	recent, ok := data["recent_memories"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, recent)
}

// ---------------------------------------------------------------------------
// Test C: .md with neither frontmatter nor H1 → title falls back to filename
// ---------------------------------------------------------------------------

func TestMemoriesProducer_NoTitleFallsBackToFilename(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a file with neither frontmatter nor H1.
	makeMemoryFile(t, tmpDir, "projX", "my-notes.md", "Just some plain text.\nNo heading here.\n")

	p := &MemoriesProducer{dir: tmpDir}

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	recent := data["recent_memories"].([]interface{})
	require.Len(t, recent, 1)

	mem := recent[0].(map[string]interface{})
	assert.Equal(t, "my-notes", mem["title"], "filename without extension should be the fallback title")
}

// ---------------------------------------------------------------------------
// Test D: source is never "placeholder"
// ---------------------------------------------------------------------------

func TestMemoriesProducer_NeverPlaceholder(t *testing.T) {
	p := &MemoriesProducer{dir: filepath.Join(t.TempDir(), "nonexistent")}

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	src, _ := data["source"].(string)
	assert.NotEqual(t, "placeholder", src, "source must never be 'placeholder'")
}

// ---------------------------------------------------------------------------
// Test E: Cap at memoriesCap (5) entries
// ---------------------------------------------------------------------------

func TestMemoriesProducer_CapsAtMemoriesCap(t *testing.T) {
	tmpDir := t.TempDir()

	// Write 8 files across 8 projects.
	for i := 0; i < 8; i++ {
		makeMemoryFile(t, tmpDir, fmt.Sprintf("proj%d", i), "mem.md",
			fmt.Sprintf("# Memory %d\n", i))
	}

	p := &MemoriesProducer{dir: tmpDir}

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	assert.Equal(t, 8, toInt(data["total_count"]), "total_count reflects all found files")

	recent := data["recent_memories"].([]interface{})
	assert.LessOrEqual(t, len(recent), memoriesCap, "recent_memories should be capped at memoriesCap")
}

// ---------------------------------------------------------------------------
// Test F: Frontmatter name takes precedence over H1
// ---------------------------------------------------------------------------

func TestMemoriesProducer_FrontmatterNameTakesPrecedence(t *testing.T) {
	tmpDir := t.TempDir()

	// File has both frontmatter name AND an H1 — name wins.
	makeMemoryFile(t, tmpDir, "projP", "priority.md",
		"---\nname: Frontmatter Name\n---\n# H1 Heading\n\nContent.\n")

	p := &MemoriesProducer{dir: tmpDir}

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	recent := data["recent_memories"].([]interface{})
	require.Len(t, recent, 1)

	mem := recent[0].(map[string]interface{})
	assert.Equal(t, "Frontmatter Name", mem["title"])
}

// ---------------------------------------------------------------------------
// Test G: modified timestamp is valid RFC3339
// ---------------------------------------------------------------------------

func TestMemoriesProducer_ModifiedIsRFC3339(t *testing.T) {
	tmpDir := t.TempDir()

	makeMemoryFile(t, tmpDir, "projT", "ts.md", "# Timestamp Test\n")

	p := &MemoriesProducer{dir: tmpDir}

	result, err := p.Produce(context.Background())
	require.NoError(t, err)

	data := result.(map[string]interface{})["data"].(map[string]interface{})
	recent := data["recent_memories"].([]interface{})
	require.Len(t, recent, 1)

	mem := recent[0].(map[string]interface{})
	modStr, ok := mem["modified"].(string)
	require.True(t, ok, "modified should be a string")
	_, err = time.Parse(time.RFC3339, modStr)
	assert.NoError(t, err, "modified should be a valid RFC3339 timestamp")
}
