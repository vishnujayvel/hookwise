package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/analytics"
	"github.com/vishnujayvel/hookwise/internal/core"
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

				if ctx.Err() == context.DeadlineExceeded {
					elapsed := time.Since(dispatchStart)
					core.Logger().Warn("dispatch timeout",
						"event_type", eventType,
						"timeout_ms", timeoutMs,
						"elapsed_ms", elapsed.Milliseconds(),
					)
					wc.Add("dispatch", fmt.Sprintf("timeout after %dms (limit %dms)", elapsed.Milliseconds(), timeoutMs))
				}

				// Analytics recording (non-blocking, ARCH-7).
				if config.Analytics.Enabled && payload.SessionID != "" {
					go recordAnalytics(ctx, eventType, payload, config.Analytics.DBPath)
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

// recordAnalytics writes session/event data to Dolt in a background goroutine.
// Fail-open: any error is logged but never surfaces to the user (ARCH-1).
func recordAnalytics(ctx context.Context, eventType string, payload core.HookPayload, dataDir string) {
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

	case core.EventSessionEnd, core.EventStop:
		if err := a.EndSession(ctx, payload.SessionID, now, analytics.SessionStats{}); err != nil {
			core.Logger().Error("analytics: end session", "error", err)
		}
	}

	// Commit to Dolt so data is visible across connections (ARCH-2).
	if _, err := db.CommitDispatch(ctx, eventType, payload.SessionID); err != nil {
		core.Logger().Error("analytics: commit", "error", err)
	}
}
