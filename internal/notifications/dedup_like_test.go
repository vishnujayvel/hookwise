package notifications

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
)

// storedDay returns the UTC date string of the most recently created
// notification. Deriving "today" from the row's own created_at stamp (rather
// than a separate time.Now() call) makes the dedup-lookup tests immune to a
// UTC-midnight race between the test's clock read and the stamp inside Create.
func storedDay(t *testing.T, ns *NotificationService) string {
	t.Helper()
	rows, err := ns.List(context.Background(), 1)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	return rows[0].CreatedAt.UTC().Format("2006-01-02")
}

// TestHasNotificationTodayWithContent_UnderscoreToolName guards the LIKE-escape
// dedup path. A content substring containing SQL LIKE wildcards (`_` or `%`) --
// as in MCP tool names like "mcp__server__tool" -- must still match its own
// stored row. The helper backslash-escapes those wildcards, which only takes
// effect if the SQL LIKE clause declares `ESCAPE '\'`. Without that clause,
// SQLite has no default escape character, treats the backslashes as literals,
// the dedup lookup never matches, and duplicate guard-effectiveness
// notifications pile up for every MCP tool (double underscores).
func TestHasNotificationTodayWithContent_UnderscoreToolName(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	tool := "mcp__playwright__click"
	content := `Guard rule for "` + tool + `" triggered 7 times today`
	require.NoError(t, ns.Create(ctx, ProducerGuard, TypeGuardEffectiveness, content))
	today := storedDay(t, ns)

	// The dedup lookup must find the row it just wrote, despite the underscores.
	found, err := hasNotificationTodayWithContent(ctx, ns, ProducerGuard, TypeGuardEffectiveness, today, tool)
	require.NoError(t, err)
	assert.True(t, found, "dedup must match an MCP tool name containing underscores")

	// A different tool name must NOT match (no false-positive dedup).
	other, err := hasNotificationTodayWithContent(ctx, ns, ProducerGuard, TypeGuardEffectiveness, today, "mcp__other__tool")
	require.NoError(t, err)
	assert.False(t, other, "dedup must not match a different tool name")
}

// TestHasNotificationTodayWithContent_BackslashToolName guards the escape-char
// completeness: with the ESCAPE '\' clause in place, a literal backslash in the
// substring must itself be escaped, otherwise SQLite treats it as the escape
// character and corrupts the match. (escapeLIKE escapes `\` before % and _.)
func TestHasNotificationTodayWithContent_BackslashToolName(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	tool := `weird\tool`
	content := `Guard rule for "` + tool + `" triggered 9 times today`
	require.NoError(t, ns.Create(ctx, ProducerGuard, TypeGuardEffectiveness, content))
	today := storedDay(t, ns)

	found, err := hasNotificationTodayWithContent(ctx, ns, ProducerGuard, TypeGuardEffectiveness, today, tool)
	require.NoError(t, err)
	assert.True(t, found, "dedup must match a tool name containing a literal backslash")
}

// TestHasNotificationTodayWithContent_PercentToolName completes the LIKE-escape
// trio: sibling tests cover `_` and `\`, but the `%` branch of escapeLIKE was
// untested even though its doc comment claims to escape `%`. A literal `%` in
// the dedup substring must be escaped so SQLite treats it as a literal, not a
// wildcard. If the `%` escape were dropped, dedup substrings would turn into
// wildcards, producing false-positive matches that SILENTLY SUPPRESS legitimate
// notifications. This test matches the row with a literal-`%` tool name AND
// proves the discriminating negative that only fails under wildcard behaviour.
func TestHasNotificationTodayWithContent_PercentToolName(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	// A tool name containing a literal percent sign must match its own row.
	tool := "build%cache"
	content := `Guard rule for "` + tool + `" triggered 8 times today`
	require.NoError(t, ns.Create(ctx, ProducerGuard, TypeGuardEffectiveness, content))
	today := storedDay(t, ns)

	found, err := hasNotificationTodayWithContent(ctx, ns, ProducerGuard, TypeGuardEffectiveness, today, tool)
	require.NoError(t, err)
	assert.True(t, found, "dedup must match a tool name containing a literal percent sign")

	// Discriminating negative: "build%che" only matches the stored "build%cache"
	// if `%` is treated as a SQL wildcard (build…che, where "che" falls inside
	// "cache"). With `%` correctly escaped it requires the literal "build%che",
	// which is absent -> no false-positive dedup. This assertion is what fails if
	// the `%` escape line is removed from escapeLIKE.
	wildcard, err := hasNotificationTodayWithContent(ctx, ns, ProducerGuard, TypeGuardEffectiveness, today, "build%che")
	require.NoError(t, err)
	assert.False(t, wildcard, "an unescaped %-wildcard must not produce a false-positive dedup match")
}

// TestCheckGuardEffectiveness_DeduplicatesUnderscoreTool is the user-facing
// proof: running the producer twice for an MCP-style tool with >=5 blocks must
// create exactly ONE notification, not a duplicate on every run. The assertion
// counts by producer/type (no test-side clock), so it is not midnight-sensitive.
func TestCheckGuardEffectiveness_DeduplicatesUnderscoreTool(t *testing.T) {
	ns, db, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()
	a := analytics.NewAnalytics(db)
	now := time.Now().UTC()
	sessionID := "guard-eff-mcp-sess"
	require.NoError(t, a.StartSession(ctx, sessionID, now))

	tool := "mcp__playwright__click"
	for i := 0; i < 6; i++ {
		require.NoError(t, a.RecordEvent(ctx, sessionID, analytics.EventRecord{
			EventType: "PreToolUse",
			ToolName:  tool,
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		}))
	}

	require.NoError(t, CheckGuardEffectiveness(ctx, ns, db))
	require.NoError(t, CheckGuardEffectiveness(ctx, ns, db)) // second run must dedup

	notifs, err := ns.List(ctx, 50)
	require.NoError(t, err)
	count := 0
	for _, n := range notifs {
		if n.Producer == ProducerGuard && n.Type == TypeGuardEffectiveness {
			count++
		}
	}
	assert.Equal(t, 1, count, "an MCP tool must yield exactly one guard-effectiveness notification across runs")
}
