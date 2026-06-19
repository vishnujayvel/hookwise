package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/notifications"
)

// TestRenderNotificationSegment_MultibyteTruncation guards against byte-index
// truncation corrupting multi-byte UTF-8 in the status-line notification
// segment. The content is built so a 4-byte emoji straddles byte offset 37 --
// the old `content[:37]` cut lands mid-rune, yielding invalid UTF-8 that the
// terminal renders as U+FFFD (replacement char). The rune-aware truncateRunes
// helper (already used by the news segment) cuts on a rune boundary instead.
func TestRenderNotificationSegment_MultibyteTruncation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOOKWISE_STATE_DIR", filepath.Join(tmpDir, ".hookwise"))
	dbPath := filepath.Join(tmpDir, "analytics.db")

	// 35 ASCII bytes + ten 4-byte emoji = 75 bytes / 45 runes. The first emoji
	// occupies bytes 35-38, so a byte-37 cut splits it; >40 runes also forces a
	// real truncation under the rune-aware path.
	content := strings.Repeat("a", 35) + strings.Repeat("\U0001F4B8", 10)
	require.Greater(t, len(content), 40, "content must exceed the 40-byte truncation threshold")
	require.False(t, utf8.RuneStart(content[37]), "byte 37 must land inside a multi-byte rune for this test to bite")

	db := openTestDB(t, dbPath)
	ns := notifications.NewNotificationService(db)
	require.NoError(t, ns.Create(context.Background(),
		notifications.ProducerBudget, notifications.TypeBudgetThreshold, content))
	db.Close()

	out := renderNotificationSegment(dbPath)
	require.NotEmpty(t, out, "an unsurfaced notification must render a non-empty segment")
	assert.True(t, utf8.ValidString(out),
		"rendered notification segment must be valid UTF-8 (no mid-rune byte slice)")
	assert.NotContains(t, out, "�",
		"segment must not contain the U+FFFD replacement char from a mid-rune cut")
}
