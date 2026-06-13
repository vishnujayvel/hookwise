package feeds

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vishnujayvel/hookwise/internal/core"
)

const memoriesCap = 5

// MemoriesProducer surfaces recently-modified memory markdown files from the
// Claude projects directory (~/.claude/projects/*/memory/*.md).
type MemoriesProducer struct {
	// dir is the base directory to scan (<dir>/*/memory/*.md).
	// When empty, defaults to filepath.Join(core.HomeDir(), ".claude", "projects").
	// Tests set this field to a temp directory.
	dir string
}

func (p *MemoriesProducer) Name() string { return "memories" }

// memoryEntry holds metadata for one memory markdown file.
type memoryEntry struct {
	title    string
	modified time.Time
	basename string
}

func (p *MemoriesProducer) Produce(_ context.Context) (interface{}, error) {
	baseDir := p.dir
	if baseDir == "" {
		baseDir = filepath.Join(core.HomeDir(), ".claude", "projects")
	}

	pattern := filepath.Join(baseDir, "*", "memory", "*.md")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return p.emptyEnvelope(), nil
	}

	totalCount := len(matches)

	entries := make([]memoryEntry, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		title := extractTitle(path)
		entries = append(entries, memoryEntry{
			title:    title,
			modified: info.ModTime(),
			basename: filepath.Base(path),
		})
	}

	// Sort newest first.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].modified.After(entries[j].modified)
	})

	// Cap at memoriesCap.
	if len(entries) > memoriesCap {
		entries = entries[:memoriesCap]
	}

	recent := make([]interface{}, 0, len(entries))
	for _, e := range entries {
		recent = append(recent, map[string]interface{}{
			"title":    e.title,
			"modified": e.modified.UTC().Format(time.RFC3339),
		})
	}

	return NewEnvelope("memories", map[string]interface{}{
		"recent_memories": recent,
		"total_count":     totalCount,
		"source":          "claude-memory",
	}), nil
}

// emptyEnvelope returns a zero-state envelope. Source is "claude-memory", never "placeholder".
func (p *MemoriesProducer) emptyEnvelope() map[string]interface{} {
	return NewEnvelope("memories", map[string]interface{}{
		"recent_memories": []interface{}{},
		"total_count":     0,
		"source":          "claude-memory",
	})
}

// extractTitle reads the title from a markdown file using the following precedence:
//  1. YAML frontmatter `name:` field
//  2. First `# ` heading line
//  3. Filename without extension
func extractTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return filenameTitle(path)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	// Check for frontmatter (file starts with "---").
	if !scanner.Scan() {
		return filenameTitle(path)
	}
	firstLine := scanner.Text()

	if strings.TrimSpace(firstLine) == "---" {
		// Parse frontmatter lines until closing "---".
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "---" {
				break
			}
			if strings.HasPrefix(line, "name:") {
				value := strings.TrimSpace(strings.TrimPrefix(line, "name:"))
				if value != "" {
					return value
				}
			}
		}
		// Frontmatter ended; continue scanning for H1.
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "# ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "# "))
			}
		}
		return filenameTitle(path)
	}

	// No frontmatter — check first line for H1.
	if strings.HasPrefix(firstLine, "# ") {
		return strings.TrimSpace(strings.TrimPrefix(firstLine, "# "))
	}

	// Scan remaining lines for first H1.
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}

	return filenameTitle(path)
}

// filenameTitle returns the filename without its extension.
func filenameTitle(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}
