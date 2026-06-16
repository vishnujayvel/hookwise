package notifications

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
)

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
	today := time.Now().UTC().Format("2006-01-02")

	tool := "mcp__playwright__click"
	content := `Guard rule for "` + tool + `" triggered 7 times today`
	require.NoError(t, ns.Create(ctx, ProducerGuard, TypeGuardEffectiveness, content))

	// The dedup lookup must find the row it just wrote, despite the underscores.
	found, err := hasNotificationTodayWithContent(ctx, ns, ProducerGuard, TypeGuardEffectiveness, today, tool)
	require.NoError(t, err)
	assert.True(t, found, "dedup must match an MCP tool name containing underscores")

	// A different tool name must NOT match (no false-positive dedup).
	other, err := hasNotificationTodayWithContent(ctx, ns, ProducerGuard, TypeGuardEffectiveness, today, "mcp__other__tool")
	require.NoError(t, err)
	assert.False(t, other, "dedup must not match a different tool name")
}

// TestCheckGuardEffectiveness_DeduplicatesUnderscoreTool is the user-facing
// proof: running the producer twice for an MCP-style tool with >=5 blocks must
// create exactly ONE notification, not a duplicate on every run.
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
