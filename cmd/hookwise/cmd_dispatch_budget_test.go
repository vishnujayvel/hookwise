package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
)

// --- Pure policy tests: budgetDenyReason ---

func TestBudgetDenyReason(t *testing.T) {
	tests := []struct {
		name       string
		cfg        core.CostTrackingConfig
		totalToday float64
		wantDeny   bool
	}{
		{
			name:       "enforce + over budget -> deny",
			cfg:        core.CostTrackingConfig{Enabled: true, Enforcement: "enforce", DailyBudget: 10},
			totalToday: 12.50,
			wantDeny:   true,
		},
		{
			name:       "enforce + exactly at budget -> deny",
			cfg:        core.CostTrackingConfig{Enabled: true, Enforcement: "enforce", DailyBudget: 10},
			totalToday: 10,
			wantDeny:   true,
		},
		{
			name:       "enforce + under budget -> allow",
			cfg:        core.CostTrackingConfig{Enabled: true, Enforcement: "enforce", DailyBudget: 10},
			totalToday: 9.99,
			wantDeny:   false,
		},
		{
			name:       "warn + over budget -> allow (warn never blocks)",
			cfg:        core.CostTrackingConfig{Enabled: true, Enforcement: "warn", DailyBudget: 10},
			totalToday: 50,
			wantDeny:   false,
		},
		{
			name:       "disabled + over budget -> allow",
			cfg:        core.CostTrackingConfig{Enabled: false, Enforcement: "enforce", DailyBudget: 10},
			totalToday: 50,
			wantDeny:   false,
		},
		{
			name:       "zero budget -> allow (no budget configured)",
			cfg:        core.CostTrackingConfig{Enabled: true, Enforcement: "enforce", DailyBudget: 0},
			totalToday: 50,
			wantDeny:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := budgetDenyReason(tt.cfg, tt.totalToday)
			if tt.wantDeny {
				assert.NotEmpty(t, reason, "expected a deny reason")
				assert.Contains(t, reason, "budget")
			} else {
				assert.Empty(t, reason, "expected no deny")
			}
		})
	}
}

// --- DB-backed integration test: enforceBudget reads cost state and denies ---

func TestEnforceBudget_OverBudget_DeniesWithPermissionJSON(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	db, err := analytics.Open(dbPath)
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	// Seed cost state above the budget.
	cs := &analytics.CostState{
		DailyCosts:   map[string]float64{},
		SessionCosts: map[string]float64{"s1": 18.0},
		TotalToday:   18.0,
	}
	require.NoError(t, db.WriteCostState(ctx, cs))

	config := core.GetDefaultConfig()
	config.Analytics.DBPath = dbPath
	config.CostTracking.Enabled = true
	config.CostTracking.Enforcement = "enforce"
	config.CostTracking.DailyBudget = 10

	override := enforceBudget(ctx, config)
	require.NotNil(t, override, "over-budget enforce must produce a deny override")
	require.NotNil(t, override.Stdout)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(*override.Stdout), &parsed))
	hso, ok := parsed["hookSpecificOutput"].(map[string]interface{})
	require.True(t, ok, "deny must use hookSpecificOutput permission format")
	assert.Equal(t, "deny", hso["permissionDecision"])
	assert.Equal(t, "PreToolUse", hso["hookEventName"])
}

func TestEnforceBudget_UnderBudget_NoOverride(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	db, err := analytics.Open(dbPath)
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	require.NoError(t, db.WriteCostState(ctx, &analytics.CostState{
		DailyCosts:   map[string]float64{},
		SessionCosts: map[string]float64{"s1": 2.0},
		TotalToday:   2.0,
	}))

	config := core.GetDefaultConfig()
	config.Analytics.DBPath = dbPath
	config.CostTracking.Enabled = true
	config.CostTracking.Enforcement = "enforce"
	config.CostTracking.DailyBudget = 10

	assert.Nil(t, enforceBudget(ctx, config), "under budget must not override")
}

func TestEnforceBudget_WarnMode_NoOverride(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "analytics.db")
	db, err := analytics.Open(dbPath)
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	require.NoError(t, db.WriteCostState(ctx, &analytics.CostState{
		DailyCosts:   map[string]float64{},
		SessionCosts: map[string]float64{"s1": 99.0},
		TotalToday:   99.0,
	}))

	config := core.GetDefaultConfig()
	config.Analytics.DBPath = dbPath
	config.CostTracking.Enabled = true
	config.CostTracking.Enforcement = "warn" // default: never blocks
	config.CostTracking.DailyBudget = 10

	assert.Nil(t, enforceBudget(ctx, config), "warn mode must never override even over budget")
}
