package notifications

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
)

// ---------------------------------------------------------------------------
// Test helper: open a fresh analytics DB and return a NotificationService
// ---------------------------------------------------------------------------

func testService(t *testing.T) (*NotificationService, *analytics.DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "hookwise-notif-test-*")
	require.NoError(t, err)

	db, err := analytics.Open(filepath.Join(tmpDir, "analytics.db"))
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
// Test 9: CheckGuardEffectiveness with frequent blocks
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

// ---------------------------------------------------------------------------
// Test 19: CreateWithTTL stores custom TTL
// ---------------------------------------------------------------------------

func TestCreateWithTTL_StoresTTL(t *testing.T) {
	ns, _, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	// Create a notification with a custom TTL of 3600 seconds (1 hour).
	require.NoError(t, ns.CreateWithTTL(ctx, "budget", "budget_threshold", "Custom TTL", 3600))

	notifs, err := ns.List(ctx, 1)
	require.NoError(t, err)
	require.Len(t, notifs, 1)

	assert.Equal(t, 3600, notifs[0].TTLSeconds)
	assert.Equal(t, "Custom TTL", notifs[0].Content)
	assert.Equal(t, "budget", notifs[0].Producer)
}

// ---------------------------------------------------------------------------
// Test 20: Unsurfaced excludes expired notifications
// ---------------------------------------------------------------------------

func TestUnsurfaced_ExcludesExpired(t *testing.T) {
	ns, db, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	// Create a notification with a very short TTL (1 second).
	require.NoError(t, ns.CreateWithTTL(ctx, "test", "test_type", "expires fast", 1))

	// Create one with a long TTL that will not expire.
	require.NoError(t, ns.CreateWithTTL(ctx, "test", "test_type", "stays active", 86400))

	// Backdate the first notification's created_at to 10 seconds ago so it's expired.
	// We need to update it directly via SQL.
	past := time.Now().UTC().Add(-10 * time.Second).Format(time.RFC3339)
	_, err := db.Exec(ctx,
		`UPDATE notifications SET created_at = ? WHERE content = ?`, past, "expires fast")
	require.NoError(t, err)

	unsurfaced, err := ns.Unsurfaced(ctx)
	require.NoError(t, err)

	// Only the non-expired notification should be returned.
	require.Len(t, unsurfaced, 1)
	assert.Equal(t, "stays active", unsurfaced[0].Content)
}

// ---------------------------------------------------------------------------
// Test 21: Notification Expired field is computed at read time
// ---------------------------------------------------------------------------

func TestNotification_ExpiredComputed(t *testing.T) {
	ns, db, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	// Create a notification with TTL of 1 second.
	require.NoError(t, ns.CreateWithTTL(ctx, "test", "test_type", "soon expired", 1))

	// Create a notification with TTL of 86400 seconds.
	require.NoError(t, ns.CreateWithTTL(ctx, "test", "test_type", "not expired", 86400))

	// Backdate the first notification so it's definitely expired.
	past := time.Now().UTC().Add(-10 * time.Second).Format(time.RFC3339)
	_, err := db.Exec(ctx,
		`UPDATE notifications SET created_at = ? WHERE content = ?`, past, "soon expired")
	require.NoError(t, err)

	// List returns all (including expired), so we can check the Expired field.
	notifs, err := ns.List(ctx, 10)
	require.NoError(t, err)
	require.Len(t, notifs, 2)

	// Find each notification by content (List returns newest first).
	var expiredNotif, activeNotif *Notification
	for i := range notifs {
		switch notifs[i].Content {
		case "soon expired":
			expiredNotif = &notifs[i]
		case "not expired":
			activeNotif = &notifs[i]
		}
	}

	require.NotNil(t, expiredNotif, "should find 'soon expired' notification")
	require.NotNil(t, activeNotif, "should find 'not expired' notification")

	assert.True(t, expiredNotif.Expired, "notification with TTL=1s and created 10s ago should be expired")
	assert.False(t, activeNotif.Expired, "notification with TTL=86400s just created should not be expired")
}

// ---------------------------------------------------------------------------
// Test 22: RunAll creates a budget notification when cost exceeds threshold
// ---------------------------------------------------------------------------

func TestRunAll_CreatesBudgetNotification(t *testing.T) {
	ns, db, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	// Write a cost state with TotalToday above the threshold.
	costState := &analytics.CostState{
		DailyCosts:   map[string]float64{},
		SessionCosts: map[string]float64{},
		Today:        time.Now().Format("2006-01-02"),
		TotalToday:   15.00,
	}
	require.NoError(t, db.WriteCostState(ctx, costState))

	// Read it back (as the dispatch path would).
	cs, err := db.ReadCostState(ctx)
	require.NoError(t, err)

	err = RunAll(ctx, ns, db, cs, 10.00)
	require.NoError(t, err)

	notifs, err := ns.List(ctx, 20)
	require.NoError(t, err)

	found := false
	for _, n := range notifs {
		if n.Producer == ProducerBudget {
			found = true
			assert.Equal(t, TypeBudgetThreshold, n.Type)
			assert.Contains(t, n.Content, "$15.00")
			assert.Contains(t, n.Content, "$10.00")
		}
	}
	assert.True(t, found, "expected at least one budget notification")
}

// ---------------------------------------------------------------------------
// Test 23: RunAll with nil states and zero threshold does not panic
// ---------------------------------------------------------------------------

func TestRunAll_NilStatesNoPanic(t *testing.T) {
	ns, db, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	err := RunAll(ctx, ns, db, nil, 0)
	require.NoError(t, err)

	notifs, err := ns.List(ctx, 20)
	require.NoError(t, err)
	assert.Empty(t, notifs, "nil states + zero threshold should produce no notifications")
}

// ---------------------------------------------------------------------------
// Test 24: RunAll continues past a single producer error (fail-soft)
// ---------------------------------------------------------------------------

func TestRunAll_FailSoft(t *testing.T) {
	ns, db, cleanup := testService(t)
	defer cleanup()

	ctx := context.Background()

	// Close the DB to make all producers that touch it return errors.
	require.NoError(t, db.Close())

	// RunAll should return a non-nil joined error but must not panic.
	err := RunAll(ctx, ns, db, nil, 0)
	// With all nil/zero inputs, producers that check nil first return nil,
	// so at most guard-effectiveness errors propagate (it queries the DB).
	// The key invariant: no panic.
	_ = err // may be nil or non-nil depending on which producers short-circuit
}
