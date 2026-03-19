package coaching_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/coaching"
)

// ---------------------------------------------------------------------------
// Mode constant values
// ---------------------------------------------------------------------------

func TestModeConstants(t *testing.T) {
	tests := []struct {
		mode coaching.Mode
		want string
	}{
		{coaching.ModeCoding, "coding"},
		{coaching.ModeTooling, "tooling"},
		{coaching.ModePractice, "practice"},
		{coaching.ModePrep, "prep"},
		{coaching.ModeNeutral, "neutral"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.mode))
			assert.Equal(t, tt.want, tt.mode.String())
			assert.Equal(t, tt.want, fmt.Sprintf("%s", tt.mode))
		})
	}
}

// ---------------------------------------------------------------------------
// AlertLevel constant values
// ---------------------------------------------------------------------------

func TestAlertLevelConstants(t *testing.T) {
	tests := []struct {
		level coaching.AlertLevel
		want  string
	}{
		{coaching.AlertNone, "none"},
		{coaching.AlertYellow, "yellow"},
		{coaching.AlertOrange, "orange"},
		{coaching.AlertRed, "red"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.level))
			assert.Equal(t, tt.want, tt.level.String())
			assert.Equal(t, tt.want, fmt.Sprintf("%s", tt.level))
		})
	}
}

// ---------------------------------------------------------------------------
// Struct zero-value sanity
// ---------------------------------------------------------------------------

func TestCoachingCacheZeroValue(t *testing.T) {
	var c coaching.CoachingCache
	assert.Equal(t, coaching.Mode(""), c.CurrentMode)
	assert.Equal(t, coaching.AlertLevel(""), c.AlertLevel)
	assert.Zero(t, c.ToolingMinutes)
	assert.Nil(t, c.LastLargeChange)
}

func TestMetacognitionResultZeroValue(t *testing.T) {
	var m coaching.MetacognitionResult
	assert.False(t, m.ShouldEmit)
	assert.Empty(t, m.PromptText)
}

func TestGrammarResultZeroValue(t *testing.T) {
	var g coaching.GrammarResult
	assert.False(t, g.ShouldCorrect)
	assert.Nil(t, g.Issues)
}

// ---------------------------------------------------------------------------
// Dependency direction: coaching must NOT import cmd/hookwise
// ---------------------------------------------------------------------------

func TestCoachingDoesNotImportCmd(t *testing.T) {
	// Find the coaching package directory relative to this test file.
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	coachingDir := filepath.Dir(thisFile)

	entries, err := os.ReadDir(coachingDir)
	require.NoError(t, err)

	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		// Skip test files — we only care about production imports.
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		path := filepath.Join(coachingDir, entry.Name())
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		require.NoError(t, err, "parsing %s", entry.Name())

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			assert.False(t,
				strings.HasPrefix(importPath, "github.com/vishnujayvel/hookwise/cmd"),
				"coaching must not import cmd packages, found %q in %s", importPath, entry.Name(),
			)
		}
	}
}
