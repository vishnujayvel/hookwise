// Package contract implements the dual-execution contract test harness.
//
// Contract tests ensure byte-identical stdout JSON between Go and TypeScript
// implementations (ARCH-6). Each test fixture is a JSON file in
// testdata/contracts/ that specifies an event type, config, stdin payload,
// and expected stdout output.
//
// The Go side runs core.Dispatch() in-process against each fixture and
// validates the output matches exactly. The TypeScript side (deferred to a
// future batch) will run the same fixtures through the TS dispatcher.
package contract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// =============================================================================
// Fixture Schema
// =============================================================================

// ContractFixture is the JSON schema for a single contract test fixture file.
type ContractFixture struct {
	Name             string          `json:"name"`
	Description      string          `json:"description,omitempty"`
	EventType        string          `json:"event_type"`
	Config           FixtureConfig   `json:"config"`
	Stdin            json.RawMessage `json:"stdin"`
	ExpectedStdout   *string         `json:"expected_stdout"`
	ExpectedExitCode int             `json:"expected_exit_code"`
}

// FixtureConfig is the subset of HooksConfig used in fixtures.
// Guards are the primary focus; handlers/other fields use defaults.
type FixtureConfig struct {
	Guards []core.GuardRuleConfig `json:"guards"`
}

// =============================================================================
// Fixture Loader
// =============================================================================

// fixturesDir returns the absolute path to testdata/contracts/ relative to
// the repository root. It walks up from the current test file to find it.
func fixturesDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")

	// Walk up from internal/contract/ to repo root
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	dir := filepath.Join(repoRoot, "testdata", "contracts")

	info, err := os.Stat(dir)
	require.NoError(t, err, "testdata/contracts/ directory must exist: %s", dir)
	require.True(t, info.IsDir(), "%s must be a directory", dir)

	return dir
}

// loadFixtures reads all .json files from testdata/contracts/ and returns
// parsed ContractFixture values sorted by filename for deterministic ordering.
func loadFixtures(t *testing.T) []ContractFixture {
	t.Helper()
	dir := fixturesDir(t)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "reading testdata/contracts/")

	var filenames []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			filenames = append(filenames, entry.Name())
		}
	}
	sort.Strings(filenames)

	require.NotEmpty(t, filenames, "no .json fixtures found in %s", dir)

	fixtures := make([]ContractFixture, 0, len(filenames))
	for _, name := range filenames {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		require.NoError(t, err, "reading fixture %s", name)

		var fixture ContractFixture
		err = json.Unmarshal(data, &fixture)
		require.NoError(t, err, "parsing fixture %s: %s", name, string(data))
		require.NotEmpty(t, fixture.Name, "fixture %s must have a name", name)
		require.NotEmpty(t, fixture.EventType, "fixture %s must have an event_type", name)

		fixtures = append(fixtures, fixture)
	}

	return fixtures
}

// =============================================================================
// Config Builder
// =============================================================================

// buildConfig constructs a HooksConfig from fixture config, using defaults
// for all fields except guards.
func buildConfig(fc FixtureConfig) core.HooksConfig {
	cfg := core.GetDefaultConfig()
	cfg.Guards = fc.Guards
	// Disable analytics and other side-effect-producing features in contract tests.
	cfg.Analytics.Enabled = false
	cfg.Coaching.Metacognition.Enabled = false
	cfg.Coaching.BuilderTrap.Enabled = false
	cfg.Coaching.Communication.Enabled = false
	cfg.Sounds.Enabled = false
	cfg.StatusLine.Enabled = false
	cfg.CostTracking.Enabled = false
	cfg.TranscriptBackup.Enabled = false
	cfg.Greeting.Enabled = false
	// No custom handlers for contract tests (guards-only)
	cfg.Handlers = nil
	return cfg
}

// =============================================================================
// Payload Builder
// =============================================================================

// buildPayload parses the fixture's stdin JSON into a HookPayload.
func buildPayload(t *testing.T, stdinRaw json.RawMessage) core.HookPayload {
	t.Helper()
	var payload core.HookPayload
	err := json.Unmarshal(stdinRaw, &payload)
	require.NoError(t, err, "parsing stdin payload: %s", string(stdinRaw))
	return payload
}

// =============================================================================
// Contract Test: Go Dispatch
// =============================================================================

// TestContractFixtures_GoDispatch is the main contract test that runs all
// fixtures through core.Dispatch() and validates the output matches exactly.
//
// This is the Go side of the dual-execution harness (R11.2).
// The TypeScript side will be added in a future batch.
func TestContractFixtures_GoDispatch(t *testing.T) {
	fixtures := loadFixtures(t)
	t.Logf("Loaded %d contract fixtures", len(fixtures))

	for _, fixture := range fixtures {
		fixture := fixture // capture loop variable
		t.Run(fixture.Name, func(t *testing.T) {
			// Build config and payload from fixture
			config := buildConfig(fixture.Config)
			payload := buildPayload(t, fixture.Stdin)

			// Run dispatch
			result := core.Dispatch(context.Background(), fixture.EventType, payload, config)

			// Validate exit code
			assert.Equal(t, fixture.ExpectedExitCode, result.ExitCode,
				"exit code mismatch for fixture %q", fixture.Name)

			// Validate stdout
			if fixture.ExpectedStdout == nil {
				// Expected: no stdout
				assert.Nil(t, result.Stdout,
					"expected nil stdout for fixture %q, got: %v",
					fixture.Name, stringPtrToDebug(result.Stdout))
			} else {
				// Expected: specific stdout JSON
				require.NotNil(t, result.Stdout,
					"expected non-nil stdout for fixture %q", fixture.Name)

				// Byte-exact comparison (ARCH-6)
				assert.Equal(t, *fixture.ExpectedStdout, *result.Stdout,
					"stdout mismatch for fixture %q\n  expected: %s\n  actual:   %s",
					fixture.Name, *fixture.ExpectedStdout, *result.Stdout)
			}
		})
	}
}

// =============================================================================
// Coverage Verification Tests
// =============================================================================

// TestContractFixtures_AllEventTypesCovered verifies that our fixture suite
// includes at least one fixture for each of the 13 canonical event types.
func TestContractFixtures_AllEventTypesCovered(t *testing.T) {
	fixtures := loadFixtures(t)

	coveredEvents := make(map[string]bool)
	for _, f := range fixtures {
		coveredEvents[f.EventType] = true
	}

	for _, eventType := range core.EventTypes {
		assert.True(t, coveredEvents[eventType],
			"missing fixture for event type %q — contract tests must cover all 13 event types", eventType)
	}

	t.Logf("All %d event types covered by fixtures", len(core.EventTypes))
}

// TestContractFixtures_AllGuardOperatorsCovered verifies fixtures exercise
// all 6 guard condition operators.
func TestContractFixtures_AllGuardOperatorsCovered(t *testing.T) {
	fixtures := loadFixtures(t)

	operators := map[string]bool{
		"contains":    false,
		"starts_with": false,
		"ends_with":   false,
		"==":          false,
		"equals":      false,
		"matches":     false,
	}

	for _, f := range fixtures {
		for _, guard := range f.Config.Guards {
			extractOperator(guard.When, operators)
			extractOperator(guard.Unless, operators)
		}
	}

	for op, found := range operators {
		assert.True(t, found,
			"missing fixture exercising operator %q — contract tests must cover all 6 operators", op)
	}

	t.Logf("All 6 guard operators covered by fixtures")
}

// TestContractFixtures_GlobMatchingCovered verifies fixtures include glob patterns.
func TestContractFixtures_GlobMatchingCovered(t *testing.T) {
	fixtures := loadFixtures(t)

	hasGlob := false
	hasBrace := false
	for _, f := range fixtures {
		for _, guard := range f.Config.Guards {
			if strings.ContainsAny(guard.Match, "*?[") {
				hasGlob = true
			}
			if strings.Contains(guard.Match, "{") {
				hasBrace = true
			}
		}
	}

	assert.True(t, hasGlob, "fixtures must include at least one glob wildcard match pattern")
	assert.True(t, hasBrace, "fixtures must include at least one brace expansion glob pattern")
}

// TestContractFixtures_WhenUnlessCovered verifies fixtures cover when and unless conditions.
func TestContractFixtures_WhenUnlessCovered(t *testing.T) {
	fixtures := loadFixtures(t)

	hasWhen := false
	hasUnless := false
	hasBoth := false

	for _, f := range fixtures {
		for _, guard := range f.Config.Guards {
			if guard.When != "" {
				hasWhen = true
			}
			if guard.Unless != "" {
				hasUnless = true
			}
			if guard.When != "" && guard.Unless != "" {
				hasBoth = true
			}
		}
	}

	assert.True(t, hasWhen, "fixtures must include at least one when condition")
	assert.True(t, hasUnless, "fixtures must include at least one unless condition")
	assert.True(t, hasBoth, "fixtures must include at least one fixture with both when and unless")
}

// TestContractFixtures_FirstMatchWinsCovered verifies fixtures cover first-match-wins semantics.
func TestContractFixtures_FirstMatchWinsCovered(t *testing.T) {
	fixtures := loadFixtures(t)

	hasMultipleRules := false
	for _, f := range fixtures {
		if len(f.Config.Guards) >= 2 {
			hasMultipleRules = true
			break
		}
	}

	assert.True(t, hasMultipleRules, "fixtures must include at least one fixture with multiple guard rules for first-match-wins")
}

// TestContractFixtures_UnrecognizedEventCovered verifies at least one fixture
// tests the unrecognized event type path (ARCH-1 fail-open holdout).
func TestContractFixtures_UnrecognizedEventCovered(t *testing.T) {
	fixtures := loadFixtures(t)

	hasUnrecognized := false
	for _, f := range fixtures {
		if !core.IsEventType(f.EventType) {
			hasUnrecognized = true
			break
		}
	}

	assert.True(t, hasUnrecognized, "fixtures must include at least one unrecognized event type")
}

// TestContractFixtures_RegressionDetection verifies fixture integrity.
// If a fixture has expected_stdout, the JSON must be valid.
// This catches accidental corruption of fixture files.
func TestContractFixtures_RegressionDetection(t *testing.T) {
	fixtures := loadFixtures(t)

	for _, f := range fixtures {
		if f.ExpectedStdout != nil {
			var parsed interface{}
			err := json.Unmarshal([]byte(*f.ExpectedStdout), &parsed)
			assert.NoError(t, err,
				"fixture %q has invalid JSON in expected_stdout: %s",
				f.Name, *f.ExpectedStdout)
		}
	}
}

// TestContractFixtures_FixtureCount ensures we have the minimum number of
// fixtures required by the spec.
func TestContractFixtures_FixtureCount(t *testing.T) {
	fixtures := loadFixtures(t)
	// Minimum: 13 event types + guards/operators + edge cases
	assert.GreaterOrEqual(t, len(fixtures), 20,
		"expected at least 20 contract fixtures, got %d", len(fixtures))
	t.Logf("Total fixtures: %d", len(fixtures))
}

// =============================================================================
// Helpers
// =============================================================================

// extractOperator checks if an expression string contains one of the known
// operators and marks it as found in the map.
func extractOperator(expr string, operators map[string]bool) {
	if expr == "" {
		return
	}
	for op := range operators {
		// Check with spaces (normal format: "field contains value")
		// and without spaces (compact format: "field==value").
		if strings.Contains(expr, " "+op+" ") || strings.Contains(expr, op) {
			operators[op] = true
		}
	}
}

// stringPtrToDebug returns a debug-friendly representation of a *string.
func stringPtrToDebug(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
