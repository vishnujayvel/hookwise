package notifications

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
)

// ---------------------------------------------------------------------------
// Test helper: open a fresh Dolt DB and return a NotificationService
// ---------------------------------------------------------------------------

func testService(t *testing.T) (*NotificationService, *analytics.DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "hookwise-notif-test-*")
	require.NoError(t, err)

	db, err := analytics.Open(tmpDir)
	require.NoError(t, err)

	ns := NewNotificationService(db)

	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return ns, db, cleanup
}

// ---------------------------------------------------------------------------
// Test 1: Create + List roundtrip
// ---------------------------------------------------------------------------

func TestCreate_List_Roundtrip(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	// Create two notifications.
	require.NoError(t, ns.Create(ctx, "budget", "budget_threshold", "Cost exceeded $5.00"))
	require.NoError(t, ns.Create(ctx, "guard", "guard_effectiveness", "Bash blocked 10 times"))

	// List with limit=10.
	notifs, err := ns.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, notifs, 2)

	// Newest first.
	assert.Equal(t, "guard", notifs[0].Producer)
	assert.Equal(t, "guard_effectiveness", notifs[0].Type)
	assert.Contains(t, notifs[0].Content, "Bash blocked")

	assert.Equal(t, "budget", notifs[1].Producer)
	assert.Equal(t, "budget_threshold", notifs[1].Type)
	assert.Contains(t, notifs[1].Content, "Cost exceeded")

	// IDs should be auto-incremented.
	assert.True(t, notifs[0].ID > 0)
	assert.True(t, notifs[1].ID > 0)

	// CreatedAt should be recent.
	assert.False(t, notifs[0].CreatedAt.IsZero())

	// SurfacedAt should be nil (not yet surfaced).
	assert.Nil(t, notifs[0].SurfacedAt)
	assert.Nil(t, notifs[1].SurfacedAt)

	// ActedOn should be false.
	assert.False(t, notifs[0].ActedOn)
}

// ---------------------------------------------------------------------------
// Test 2: Unsurfaced filtering
// ---------------------------------------------------------------------------

func TestUnsurfaced_FiltersCorrectly(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	// Create 3 notifications.
	require.NoError(t, ns.Create(ctx, "budget", "budget_threshold", "First"))
	require.NoError(t, ns.Create(ctx, "guard", "guard_effectiveness", "Second"))
	require.NoError(t, ns.Create(ctx, "coaching", "coaching_prompt", "Third"))

	// All 3 should be unsurfaced.
	unsurfaced, err := ns.Unsurfaced(ctx)
	require.NoError(t, err)
	require.Len(t, unsurfaced, 3)

	// Unsurfaced should be in chronological order (oldest first).
	assert.Equal(t, "First", unsurfaced[0].Content)
	assert.Equal(t, "Third", unsurfaced[2].Content)

	// Mark the first as surfaced.
	require.NoError(t, ns.MarkSurfaced(ctx, unsurfaced[0].ID))

	// Now only 2 should be unsurfaced.
	unsurfaced, err = ns.Unsurfaced(ctx)
	require.NoError(t, err)
	require.Len(t, unsurfaced, 2)
	assert.Equal(t, "Second", unsurfaced[0].Content)
	assert.Equal(t, "Third", unsurfaced[1].Content)
}

// ---------------------------------------------------------------------------
// Test 3: MarkSurfaced sets surfaced_at
// ---------------------------------------------------------------------------

func TestMarkSurfaced(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	require.NoError(t, ns.Create(ctx, "budget", "budget_threshold", "Test surfacing"))

	notifs, err := ns.List(ctx, 1)
	require.NoError(t, err)
	require.Len(t, notifs, 1)

	id := notifs[0].ID
	assert.Nil(t, notifs[0].SurfacedAt)

	// Mark as surfaced.
	require.NoError(t, ns.MarkSurfaced(ctx, id))

	// Re-read and verify surfaced_at is set.
	notifs, err = ns.List(ctx, 1)
	require.NoError(t, err)
	require.Len(t, notifs, 1)
	require.NotNil(t, notifs[0].SurfacedAt)
	assert.False(t, notifs[0].SurfacedAt.IsZero())
}

// ---------------------------------------------------------------------------
// Test 4: CheckBudget triggers notification when threshold exceeded
// ---------------------------------------------------------------------------

func TestCheckBudget_TriggersWhenExceeded(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	costState := &analytics.CostState{
		TotalToday: 12.50,
		Today:      time.Now().Format("2006-01-02"),
	}
	threshold := 10.00

	err := CheckBudget(ctx, ns, costState, threshold)
	require.NoError(t, err)

	notifs, err := ns.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, notifs, 1)

	assert.Equal(t, ProducerBudget, notifs[0].Producer)
	assert.Equal(t, TypeBudgetThreshold, notifs[0].Type)
	assert.Contains(t, notifs[0].Content, "$12.50")
	assert.Contains(t, notifs[0].Content, "$10.00")
}

// ---------------------------------------------------------------------------
// Test 5: CheckBudget does NOT trigger when under threshold
// ---------------------------------------------------------------------------

func TestCheckBudget_NoNotificationUnderThreshold(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	costState := &analytics.CostState{
		TotalToday: 3.00,
		Today:      time.Now().Format("2006-01-02"),
	}
	threshold := 10.00

	err := CheckBudget(ctx, ns, costState, threshold)
	require.NoError(t, err)

	notifs, err := ns.List(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, notifs)
}

// ---------------------------------------------------------------------------
// Test 6: CheckBudget deduplicates (does not create twice in same day)
// ---------------------------------------------------------------------------

func TestCheckBudget_Deduplicates(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	costState := &analytics.CostState{
		TotalToday: 15.00,
		Today:      time.Now().Format("2006-01-02"),
	}
	threshold := 10.00

	// Call twice.
	require.NoError(t, CheckBudget(ctx, ns, costState, threshold))
	require.NoError(t, CheckBudget(ctx, ns, costState, threshold))

	notifs, err := ns.List(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, notifs, 1, "should only create one notification per day")
}

// ---------------------------------------------------------------------------
// Test 7: CheckBudget with nil cost state is a no-op
// ---------------------------------------------------------------------------

func TestCheckBudget_NilCostState(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	err := CheckBudget(ctx, ns, nil, 10.00)
	require.NoError(t, err)

	notifs, err := ns.List(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, notifs)
}

// ---------------------------------------------------------------------------
// Test 8: CheckBudget with zero threshold is a no-op
// ---------------------------------------------------------------------------

func TestCheckBudget_ZeroThreshold(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	costState := &analytics.CostState{
		TotalToday: 5.00,
		Today:      time.Now().Format("2006-01-02"),
	}

	err := CheckBudget(ctx, ns, costState, 0)
	require.NoError(t, err)

	notifs, err := ns.List(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, notifs)
}

// ---------------------------------------------------------------------------
// Test 9: CheckCoaching triggers on elevated alert level
// ---------------------------------------------------------------------------

func TestCheckCoaching_TriggersOnElevatedAlert(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	coachState := &analytics.CoachingState{
		AlertLevel:     "yellow",
		CurrentMode:    "tooling",
		ToolingMinutes: 30,
		TodayDate:      time.Now().Format("2006-01-02"),
	}

	err := CheckCoaching(ctx, ns, coachState)
	require.NoError(t, err)

	notifs, err := ns.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, notifs, 1)

	assert.Equal(t, ProducerCoaching, notifs[0].Producer)
	assert.Equal(t, TypeCoachingPrompt, notifs[0].Type)
	assert.Contains(t, notifs[0].Content, "yellow")
	assert.Contains(t, notifs[0].Content, "30 minutes")
}

// ---------------------------------------------------------------------------
// Test 10: CheckCoaching does NOT trigger on "none" alert level
// ---------------------------------------------------------------------------

func TestCheckCoaching_NoNotificationOnNone(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	coachState := &analytics.CoachingState{
		AlertLevel:  "none",
		CurrentMode: "coding",
	}

	err := CheckCoaching(ctx, ns, coachState)
	require.NoError(t, err)

	notifs, err := ns.List(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, notifs)
}

// ---------------------------------------------------------------------------
// Test 11: CheckCoaching deduplicates same alert level on same day
// ---------------------------------------------------------------------------

func TestCheckCoaching_Deduplicates(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	coachState := &analytics.CoachingState{
		AlertLevel:     "orange",
		CurrentMode:    "tooling",
		ToolingMinutes: 60,
		TodayDate:      time.Now().Format("2006-01-02"),
	}

	// Call twice.
	require.NoError(t, CheckCoaching(ctx, ns, coachState))
	require.NoError(t, CheckCoaching(ctx, ns, coachState))

	notifs, err := ns.List(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, notifs, 1, "should not create duplicate coaching notification")
}

// ---------------------------------------------------------------------------
// Test 12: CheckCoaching with nil state is a no-op
// ---------------------------------------------------------------------------

func TestCheckCoaching_NilState(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	err := CheckCoaching(ctx, ns, nil)
	require.NoError(t, err)

	notifs, err := ns.List(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, notifs)
}

// ---------------------------------------------------------------------------
// Test 13: Notification content formatting for coaching levels
// ---------------------------------------------------------------------------

func TestFormatCoachingContent_AllLevels(t *testing.T) {
	tests := []struct {
		alertLevel string
		expectSub  string
	}{
		{"yellow", "Coaching alert (yellow)"},
		{"orange", "Coaching alert (orange)"},
		{"red", "Coaching alert (red)"},
		{"custom", "Coaching alert (custom):"},
	}

	for _, tc := range tests {
		t.Run(tc.alertLevel, func(t *testing.T) {
			state := &analytics.CoachingState{
				AlertLevel:     tc.alertLevel,
				CurrentMode:    "coding",
				ToolingMinutes: 45,
			}
			content := formatCoachingContent(state)
			assert.Contains(t, content, tc.expectSub)
			assert.Contains(t, content, "45")
		})
	}
}

// ---------------------------------------------------------------------------
// Test 14: CheckGuardEffectiveness with frequent blocks
// ---------------------------------------------------------------------------

func TestCheckGuardEffectiveness_FrequentBlocks(t *testing.T) {
	ns, db, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()
	a := analytics.NewAnalytics(db)

	// Create a session and insert 6 PreToolUse events for "Bash" today.
	now := time.Now().UTC()
	sessionID := "guard-eff-sess"
	require.NoError(t, a.StartSession(ctx, sessionID, now))

	for i := 0; i < 6; i++ {
		require.NoError(t, a.RecordEvent(ctx, sessionID, analytics.EventRecord{
			EventType: "PreToolUse",
			ToolName:  "Bash",
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		}))
	}

	err := CheckGuardEffectiveness(ctx, ns, db)
	require.NoError(t, err)

	notifs, err := ns.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, notifs, 1)

	assert.Equal(t, ProducerGuard, notifs[0].Producer)
	assert.Equal(t, TypeGuardEffectiveness, notifs[0].Type)
	assert.Contains(t, notifs[0].Content, "Bash")
	assert.Contains(t, notifs[0].Content, "6 times")
}

// ---------------------------------------------------------------------------
// Test 15: CheckGuardEffectiveness with low count does not trigger
// ---------------------------------------------------------------------------

func TestCheckGuardEffectiveness_LowCount_NoNotification(t *testing.T) {
	ns, db, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()
	a := analytics.NewAnalytics(db)

	// Create a session and insert only 3 PreToolUse events (below threshold of 5).
	now := time.Now().UTC()
	sessionID := "guard-low-sess"
	require.NoError(t, a.StartSession(ctx, sessionID, now))

	for i := 0; i < 3; i++ {
		require.NoError(t, a.RecordEvent(ctx, sessionID, analytics.EventRecord{
			EventType: "PreToolUse",
			ToolName:  "Write",
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		}))
	}

	err := CheckGuardEffectiveness(ctx, ns, db)
	require.NoError(t, err)

	notifs, err := ns.List(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, notifs)
}

// ---------------------------------------------------------------------------
// Test 16: List with default limit
// ---------------------------------------------------------------------------

func TestList_DefaultLimit(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	// Create 25 notifications.
	for i := 0; i < 25; i++ {
		require.NoError(t, ns.Create(ctx, "test", "test_type", "notification"))
	}

	// List with limit=0 should use default (20).
	notifs, err := ns.List(ctx, 0)
	require.NoError(t, err)
	assert.Len(t, notifs, 20)
}

// ---------------------------------------------------------------------------
// Test 17: NewNotificationService returns non-nil
// ---------------------------------------------------------------------------

func TestNewNotificationService_NotNil(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	assert.NotNil(t, ns)
	assert.NotNil(t, ns.db)
}

// ---------------------------------------------------------------------------
// Test 18: Unsurfaced returns empty when all are surfaced
// ---------------------------------------------------------------------------

func TestUnsurfaced_EmptyWhenAllSurfaced(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	require.NoError(t, ns.Create(ctx, "test", "test_type", "will be surfaced"))

	notifs, err := ns.List(ctx, 1)
	require.NoError(t, err)
	require.Len(t, notifs, 1)

	require.NoError(t, ns.MarkSurfaced(ctx, notifs[0].ID))

	unsurfaced, err := ns.Unsurfaced(ctx)
	require.NoError(t, err)
	assert.Empty(t, unsurfaced)
}
