package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
	"github.com/vishnujayvel/hookwise/internal/notifications"
	"github.com/vishnujayvel/hookwise/internal/pricing"
	"github.com/vishnujayvel/hookwise/internal/transcript"
)

// newDispatchCmd handles "hookwise dispatch <EventType>".
// Reads JSON from stdin, runs the three-phase dispatch pipeline, writes result to stdout.
func newDispatchCmd() *cobra.Command {
	var projectDir string

	cmd := &cobra.Command{
		Use:   "dispatch <EventType>",
		Short: "Dispatch a hook event (called by Claude Code)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eventType := args[0]

			var tuiLaunchMethod string // set inside SafeDispatch, used after
			wc := core.NewWarningCollector()

			result := core.SafeDispatchWithWarnings(wc, func() core.DispatchResult {
				// Read payload from stdin
				payload := core.ReadStdinPayload()

				// Resolve project directory
				dir := projectDir
				if dir == "" {
					var err error
					dir, err = os.Getwd()
					if err != nil {
						dir = "."
					}
				}

				// Load config
				config, err := core.LoadConfig(dir)
				if err != nil {
					core.Logger().Error("failed to load config", "error", err)
					wc.Add("config", err.Error())
					// Malformed config -> exit 0 silently (fail-open)
					return core.DispatchResult{ExitCode: 0}
				}

				// Resolve dispatch timeout (0 = default 500ms, negative = no timeout)
				timeoutMs := config.Dispatch.TimeoutMs
				if timeoutMs == 0 {
					timeoutMs = core.DefaultDispatchTimeoutMs
				}

				var ctx context.Context
				var cancel context.CancelFunc
				if timeoutMs > 0 {
					ctx, cancel = context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
				} else {
					ctx, cancel = context.WithCancel(context.Background())
				}
				// cancel is NOT deferred: side-effect goroutines and the analytics
				// goroutine share this context and should keep running until os.Exit
				// terminates the process. On timeout, WithTimeout cancels automatically.
				_ = cancel
				dispatchStart := time.Now()

				// Run the three-phase dispatch engine
				dispatchResult := core.Dispatch(ctx, eventType, payload, config)

				// Budget enforcement (PreToolUse only, opt-in via
				// cost_tracking.enforcement: enforce). A deny here is the hardest
				// gate, so it overrides whatever dispatch decided. Off by default,
				// so normal users incur no extra read. The DB read lives here in
				// the command layer (not the pure engine) because core.Dispatch
				// has no analytics dependency.
				if eventType == core.EventPreToolUse {
					if override := enforceBudget(ctx, config); override != nil {
						dispatchResult = *override
					}
				}

				if ctx.Err() == context.DeadlineExceeded {
					elapsed := time.Since(dispatchStart)
					core.Logger().Warn("dispatch timeout",
						"event_type", eventType,
						"timeout_ms", timeoutMs,
						"elapsed_ms", elapsed.Milliseconds(),
					)
					wc.Add("dispatch", fmt.Sprintf("timeout after %dms (limit %dms)", elapsed.Milliseconds(), timeoutMs))
				}

				// Analytics recording. This MUST complete before the process
				// exits: dispatch calls os.Exit() shortly after, which kills any
				// still-running goroutine — so a fire-and-forget
				// `go recordAnalytics(...)` almost never persists its SQLite write
				// (the open+insert loses the race with os.Exit). Run it in a
				// goroutine so a hung DB can't block the hook forever, but WAIT up
				// to analyticsRecordTimeout for it. Fail-open (ARCH-1) on timeout.
				if config.Analytics.Enabled && payload.SessionID != "" {
					done := make(chan struct{})
					go func() {
						defer close(done)
						recordAnalytics(ctx, eventType, payload, config.Analytics.DBPath, config.CostTracking)
					}()
					select {
					case <-done:
					case <-time.After(analyticsRecordTimeout):
						core.Logger().Warn("analytics: recording did not finish in time",
							"timeout", analyticsRecordTimeout)
					}
				}

				// Signal TUI launch intent (executed synchronously after SafeDispatch)
				if eventType == core.EventSessionStart && config.TUI.AutoLaunch {
					tuiLaunchMethod = config.TUI.LaunchMethod
				}

				return dispatchResult
			})

			// Flush warnings after dispatch completes (observability only, ARCH-1).
			if wc.Count() > 0 {
				_ = wc.Flush() // best-effort, ignore errors
			}

			if result.Stdout != nil {
				fmt.Print(*result.Stdout)
			}

			// Launch TUI synchronously — cmd.Start() returns immediately after
			// fork, so this doesn't block. Running it outside SafeDispatch
			// ensures the launch completes before os.Exit().
			if tuiLaunchMethod != "" {
				launchTUIIfNeeded(tuiLaunchMethod)
			}

			os.Exit(result.ExitCode)
			return nil
		},
	}

	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory (defaults to cwd)")

	return cmd
}

// budgetDenyReason returns a non-empty deny reason when daily-budget enforcement
// is active and today's spend has reached the budget, or "" otherwise. Pure: the
// caller supplies today's total so this is trivially testable without a DB.
//
// Enforcement is opt-in — it only triggers when cost_tracking.enforcement is set
// to "enforce" (the default is "warn", which never blocks). This is what makes
// the cost-tracking recipe's advertised "budget enforcement" real (previously
// the Enforcement field was defined, defaulted, and read nowhere).
func budgetDenyReason(cc core.CostTrackingConfig, totalToday float64) string {
	if !cc.Enabled || cc.Enforcement != "enforce" || cc.DailyBudget <= 0 {
		return ""
	}
	if totalToday < cc.DailyBudget {
		return ""
	}
	return fmt.Sprintf(
		"Daily budget reached: $%.2f spent today (budget $%.2f). Set cost_tracking.enforcement to \"warn\" to allow tool calls.",
		totalToday, cc.DailyBudget,
	)
}

// enforceBudget reads today's cost state and, when daily-budget enforcement is
// active and exceeded, returns a PreToolUse "deny" override. Returns nil
// (allow) on any error or when not over budget — fail-open per ARCH-1.
//
// The cheap gate is checked BEFORE opening the DB, so default (warn) users pay
// no extra hot-path read. Only opt-in enforce users incur one singleton cost-
// state read per PreToolUse.
func enforceBudget(ctx context.Context, config core.HooksConfig) *core.DispatchResult {
	cc := config.CostTracking
	if !cc.Enabled || cc.Enforcement != "enforce" || cc.DailyBudget <= 0 {
		return nil
	}

	db, err := analytics.Open(config.Analytics.DBPath)
	if err != nil {
		core.Logger().Warn("budget enforcement: failed to open DB (fail-open)", "error", err)
		return nil
	}
	defer db.Close()

	cs, err := db.ReadCostState(ctx)
	if err != nil || cs == nil {
		core.Logger().Warn("budget enforcement: failed to read cost state (fail-open)", "error", err)
		return nil
	}

	reason := budgetDenyReason(cc, cs.TotalToday)
	if reason == "" {
		return nil
	}
	stdout := core.BuildPermissionDenyJSON(reason)
	return &core.DispatchResult{Stdout: &stdout, ExitCode: 0}
}

// analyticsRecordTimeout bounds how long dispatch waits for the analytics write
// before exiting fail-open. A SQLite WAL insert is single-digit ms; this ceiling
// only trips if the DB is locked/slow, and prevents a stuck write from hanging
// the hook (and thus the tool call) indefinitely.
const analyticsRecordTimeout = 2 * time.Second

// recordAnalytics writes session/event data to the SQLite analytics DB. The
// caller waits for it (bounded) before os.Exit so the write actually persists.
// Fail-open: any error is logged but never surfaces to the user (ARCH-1).
func recordAnalytics(ctx context.Context, eventType string, payload core.HookPayload, dataDir string, costCfg core.CostTrackingConfig) {
	defer func() {
		if r := recover(); r != nil {
			core.Logger().Error("panic in analytics recording", "recovered", fmt.Sprintf("%v", r))
		}
	}()

	if ctx.Err() != nil {
		return
	}

	db, err := analytics.Open(dataDir)
	if err != nil {
		core.Logger().Error("analytics: failed to open DB", "error", err)
		return
	}
	defer db.Close()

	a := analytics.NewAnalytics(db)
	now := time.Now()

	switch eventType {
	case core.EventSessionStart:
		if err := a.StartSession(ctx, payload.SessionID, now); err != nil {
			core.Logger().Error("analytics: start session", "error", err)
		}

	case core.EventPostToolUse:
		event := analytics.EventRecord{
			EventType: eventType,
			ToolName:  payload.ToolName,
			Timestamp: now,
		}
		if err := a.RecordEvent(ctx, payload.SessionID, event); err != nil {
			core.Logger().Error("analytics: record event", "error", err)
		}

		// Best-effort notification generation (fail-open, ARCH-1). Producers dedup
		// per day, so repeated calls don't spam.
		costState, _ := db.ReadCostState(ctx)
		ns := notifications.NewNotificationService(db)
		if err := notifications.RunAll(ctx, ns, db, costState, costCfg.DailyBudget); err != nil {
			core.Logger().Warn("notifications: generation had errors", "error", err)
		}

	case core.EventSessionEnd, core.EventStop:
		var sessionCost float64
		if costCfg.Enabled && payload.TranscriptPath != "" {
			usageByModel, terr := transcript.SumUsage(payload.TranscriptPath)
			if terr != nil {
				core.Logger().Warn("cost: read transcript usage", "error", terr)
			} else {
				for model, u := range usageByModel {
					// Degrade gracefully AND warn: an unknown model still costs
					// at Sonnet fallback rates, but silently mispricing an
					// as-yet-unknown family undercounts cost. Surface it (audit #26).
					if !pricing.Recognized(model) {
						core.Logger().Warn("cost: unknown model, using fallback rates",
							"model", model)
					}
					sessionCost += pricing.ComputeWithRates(model, u, costCfg.Rates)
				}
				// Atomic read-modify-write: each dispatch is its own process
				// sharing one WAL DB, so a plain Read→Write would lose updates
				// when two Stop events race. UpdateCostState serializes via
				// BEGIN IMMEDIATE (audit #6).
				if uerr := db.UpdateCostState(ctx, func(state *analytics.CostState) {
					today := now.Format("2006-01-02")
					delta := sessionCost - state.SessionCosts[payload.SessionID]
					state.SessionCosts[payload.SessionID] = sessionCost
					state.TotalToday += delta
					if state.TotalToday < 0 {
						state.TotalToday = 0
					}
					state.DailyCosts[today] += delta
					// Mirror the non-negative floor: TotalToday and DailyCosts[today]
					// are redundant views of the same daily total, so a negative delta
					// (lowered Rates override or truncated transcript) must clamp both
					// identically — clamping only one lets them diverge permanently.
					if state.DailyCosts[today] < 0 {
						state.DailyCosts[today] = 0
					}
					state.Today = today
				}); uerr != nil {
					core.Logger().Error("cost: update cost_state", "error", uerr)
				}
			}
		}
		if err := a.EndSession(ctx, payload.SessionID, now, analytics.SessionStats{EstimatedCostUSD: sessionCost}); err != nil {
			core.Logger().Error("analytics: end session", "error", err)
		}
	}

	// CommitDispatch is a no-op under SQLite: WAL makes writes immediately
	// visible across connections, so no explicit commit is needed (ARCH-2).
	if _, err := db.CommitDispatch(ctx, eventType, payload.SessionID); err != nil {
		core.Logger().Error("analytics: commit", "error", err)
	}
}
