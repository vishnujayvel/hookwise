package hooks

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inv is a tiny builder for an Inventory from (event, command) pairs, all from
// the same fake source file.
func inv(pairs ...[2]string) *Inventory {
	i := &Inventory{}
	for _, p := range pairs {
		i.Entries = append(i.Entries, HookEntry{
			Event: p[0], Command: p[1], Type: "command", SourceFile: "settings.json",
		})
	}
	return i
}

// invM builds an Inventory from {event, matcher, command} triples so tests can
// distinguish always-fire ("" / "*") from matcher-scoped hooks.
func invM(triples ...[3]string) *Inventory {
	i := &Inventory{}
	for _, t := range triples {
		i.Entries = append(i.Entries, HookEntry{
			Event: t[0], Matcher: t[1], Command: t[2], Type: "command", SourceFile: "settings.json",
		})
	}
	return i
}

// --- matcher-awareness (real-config false positives) ---

// TestSprawlFindings_MatcherScopedDoNotAlarm: many hooks on an event, but each
// is scoped to a distinct tool matcher → none fire on a generic call → no alarm.
func TestSprawlFindings_MatcherScopedDoNotAlarm(t *testing.T) {
	var triples [][3]string
	for i := 0; i < 12; i++ { // 12 PreToolUse hooks, each a different specific matcher
		triples = append(triples, [3]string{"PreToolUse", "mcp__tool__op" + string(rune('a'+i)), "guard.py"})
	}
	assert.Empty(t, SprawlFindings(invM(triples...)),
		"matcher-scoped hooks must not trigger sprawl — only one fires per matching call")
}

// TestSprawlFindings_AlwaysFireAlarms: hooks with empty/'*' matcher fire on every
// call, so they count toward the threshold.
func TestSprawlFindings_AlwaysFireAlarms(t *testing.T) {
	var triples [][3]string
	for i := 0; i < 6; i++ { // 6 always-fire PreToolUse hooks → WARN (>5)
		triples = append(triples, [3]string{"PreToolUse", "", "guard.py"})
	}
	fs := SprawlFindings(invM(triples...))
	require.Len(t, fs, 1)
	assert.Equal(t, LevelWarn, fs[0].Level)
}

// TestDuplicateFindings_DifferentMatchersNotDuplicate: the same command under
// different matchers is intentional per-tool protection, NOT a duplicate.
func TestDuplicateFindings_DifferentMatchersNotDuplicate(t *testing.T) {
	i := invM(
		[3]string{"PreToolUse", "mcp__cal__create", "calendar_guard.py"},
		[3]string{"PreToolUse", "mcp__cal__delete", "calendar_guard.py"},
		[3]string{"PreToolUse", "mcp__cal__update", "calendar_guard.py"},
	)
	for _, f := range DuplicateFindings(i) {
		assert.NotEqual(t, "hook-duplicate", f.Code,
			"same command under different matchers is not a duplicate")
	}
}

// TestDuplicateFindings_SameMatcherIsDuplicate: same command AND same matcher
// repeated is a true copy-paste duplicate.
func TestDuplicateFindings_SameMatcherIsDuplicate(t *testing.T) {
	i := invM(
		[3]string{"PreToolUse", "Bash", "guard.py"},
		[3]string{"PreToolUse", "Bash", "guard.py"},
	)
	var dup *Finding
	for idx := range DuplicateFindings(i) {
		if DuplicateFindings(i)[idx].Code == "hook-duplicate" {
			dup = &DuplicateFindings(i)[idx]
		}
	}
	require.NotNil(t, dup)
	assert.Contains(t, dup.Message, "2 times")
}

// --- #34 inventory + sprawl ---

func TestInventoryFinding_TotalsAndPerEvent(t *testing.T) {
	f := InventoryFinding(inv(
		[2]string{"PreToolUse", "a"}, [2]string{"PreToolUse", "b"},
		[2]string{"SessionStart", "c"},
	))
	assert.Equal(t, LevelScan, f.Level)
	assert.Equal(t, "hooks", f.Code)
	assert.Equal(t, "3 hooks across 2 events", f.Message)
	// Per-event details sorted by count desc.
	require.Len(t, f.Details, 2)
	assert.Contains(t, f.Details[0], "PreToolUse")
	assert.Contains(t, f.Details[0], "2 hooks")
	assert.Contains(t, f.Details[1], "SessionStart")
	assert.Contains(t, f.Details[1], "1 hook")     // singular
	assert.NotContains(t, f.Details[1], "1 hooks") // not "1 hooks"
}

func TestSprawlFindings_HotPathThresholds(t *testing.T) {
	var pairs [][2]string
	for i := 0; i < 6; i++ { // 6 > 5 WARN, <= 10
		pairs = append(pairs, [2]string{"PreToolUse", "cmd"})
	}
	fs := SprawlFindings(inv(pairs...))
	require.Len(t, fs, 1)
	assert.Equal(t, LevelWarn, fs[0].Level)
	assert.Equal(t, "hook-sprawl", fs[0].Code)
	assert.Contains(t, fs[0].Message, "PreToolUse has 6 always-on hooks")
}

func TestSprawlFindings_HotPathFail(t *testing.T) {
	var pairs [][2]string
	for i := 0; i < 11; i++ { // 11 > 10 FAIL
		pairs = append(pairs, [2]string{"PostToolUse", "cmd"})
	}
	fs := SprawlFindings(inv(pairs...))
	require.Len(t, fs, 1)
	assert.Equal(t, LevelFail, fs[0].Level)
}

func TestSprawlFindings_OtherEventThresholds(t *testing.T) {
	var pairs [][2]string
	for i := 0; i < 4; i++ { // 4 > 3 WARN for non-hot-path
		pairs = append(pairs, [2]string{"SessionStart", "cmd"})
	}
	fs := SprawlFindings(inv(pairs...))
	require.Len(t, fs, 1)
	assert.Equal(t, LevelWarn, fs[0].Level)
}

func TestSprawlFindings_UnderThresholdQuiet(t *testing.T) {
	fs := SprawlFindings(inv(
		[2]string{"PreToolUse", "a"}, [2]string{"PreToolUse", "b"},
		[2]string{"SessionStart", "c"},
	))
	assert.Empty(t, fs)
}

// --- #33 missing binary ---

func TestMissingBinaryFindings_FlagsAbsentBinary(t *testing.T) {
	i := inv(
		[2]string{"PreToolUse", "hookwise dispatch PreToolUse"},
		[2]string{"PreToolUse", "hookwise dispatch PreToolUse"},
		[2]string{"SessionStart", "python3 /x/quote.py"},
	)
	// Fake PATH: python3 present, hookwise absent.
	look := func(name string) (string, error) {
		if name == "python3" {
			return "/usr/bin/python3", nil
		}
		return "", errors.New("not found")
	}
	fs := MissingBinaryFindings(i, look)
	require.Len(t, fs, 1)
	assert.Equal(t, LevelFail, fs[0].Level)
	assert.Equal(t, "hook-binary", fs[0].Code)
	assert.Contains(t, fs[0].Message, "hookwise")
	assert.Contains(t, fs[0].Message, "2 hooks") // dependent-hook count
}

func TestMissingBinaryFindings_AllPresentQuiet(t *testing.T) {
	i := inv([2]string{"PreToolUse", "python3 /x/quote.py"})
	look := func(string) (string, error) { return "/usr/bin/python3", nil }
	assert.Empty(t, MissingBinaryFindings(i, look))
}

// --- #35 network on hot path ---

func TestNetworkHookFindings_HotPathOnly(t *testing.T) {
	i := inv(
		[2]string{"PreToolUse", "uvx claude-code-guardian"},
		[2]string{"SessionStart", "uvx some-tool"}, // not hot-path → ignored
		[2]string{"PostToolUse", "npx foo"},
		[2]string{"PreToolUse", "python3 local.py"}, // safe
	)
	fs := NetworkHookFindings(i)
	require.Len(t, fs, 2)
	for _, f := range fs {
		assert.Equal(t, LevelWarn, f.Level)
		assert.Equal(t, "hook-network", f.Code)
	}
}

func TestNetworkHookFindings_DetectsPatterns(t *testing.T) {
	cases := []string{"uvx t", "uv run t", "npx t", "pip install t", "curl http://x", "wget x", "docker run img"}
	for _, c := range cases {
		fs := NetworkHookFindings(inv([2]string{"PreToolUse", c}))
		assert.Len(t, fs, 1, "should flag %q", c)
	}
	// docker run with --pull=never is acceptable.
	assert.Empty(t, NetworkHookFindings(inv([2]string{"PreToolUse", "docker run --pull=never img"})))
}

// --- #36 duplicates + overlap ---

func TestDuplicateFindings_ExactDuplicates(t *testing.T) {
	i := inv(
		[2]string{"PreToolUse", "skill-routing-guard"},
		[2]string{"PreToolUse", "skill-routing-guard"},
		[2]string{"PreToolUse", "skill-routing-guard"},
	)
	fs := DuplicateFindings(i)
	// Expect a duplicate WARN (3 times) — and overlap is only for >1 distinct guard.
	var dup *Finding
	for idx := range fs {
		if fs[idx].Code == "hook-duplicate" {
			dup = &fs[idx]
		}
	}
	require.NotNil(t, dup)
	assert.Equal(t, LevelWarn, dup.Level)
	assert.Contains(t, dup.Message, "3 times")
	assert.Contains(t, dup.Message, "PreToolUse")
}

func TestDuplicateFindings_GuardOverlap(t *testing.T) {
	i := inv(
		[2]string{"PreToolUse", "hookwise dispatch PreToolUse"},
		[2]string{"PreToolUse", "uvx claude-code-guardian"},
		[2]string{"PreToolUse", "skill-routing-guard"},
	)
	fs := DuplicateFindings(i)
	var overlap *Finding
	for idx := range fs {
		if fs[idx].Code == "hook-overlap" {
			overlap = &fs[idx]
		}
	}
	require.NotNil(t, overlap)
	assert.Equal(t, LevelInfo, overlap.Level)
	assert.Contains(t, overlap.Message, "3 guard systems")
}

func TestDuplicateFindings_NoFalsePositives(t *testing.T) {
	i := inv(
		[2]string{"PreToolUse", "hookwise dispatch PreToolUse"},
		[2]string{"PostToolUse", "python3 fmt.py"},
	)
	assert.Empty(t, DuplicateFindings(i))
}
