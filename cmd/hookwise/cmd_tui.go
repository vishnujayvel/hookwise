package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/core"
)

func newTUICmd() *cobra.Command {
	var launchMethod string

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive TUI",
		Long: "Launches the hookwise-tui dashboard if it is not already running. " +
			"Uses the same singleton launch guard as session auto-launch, so " +
			"concurrent invocations spawn at most one TUI.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd.OutOrStdout(), launchMethod)
		},
	}

	cmd.Flags().StringVar(&launchMethod, "launch-method", "",
		"How to launch the TUI: newWindow or background (defaults to config tui.launch_method, else newWindow)")
	return cmd
}

func runTUI(w io.Writer, flagMethod string) error {
	// Load config the same way other commands do, falling back to defaults
	// (ARCH-1: never hard-fail on a missing/broken config).
	cwd, _ := os.Getwd()
	config, err := core.LoadConfig(cwd)
	if err != nil {
		config = core.GetDefaultConfig()
	}

	method, err := resolveTUILaunchMethod(flagMethod, config.TUI.LaunchMethod)
	if err != nil {
		return err
	}

	if pid, ok := runningTUIPID(); ok {
		fmt.Fprintf(w, "TUI is already running (PID %d)\n", pid)
		return nil
	}

	if _, err := exec.LookPath("hookwise-tui"); err != nil {
		return fmt.Errorf("hookwise-tui not found in PATH — the TUI ships separately from the core binary; install it from the hookwise repo with `uv tool install ./tui` (see README)")
	}

	// Delegate to the existing singleton launch path (mtime-marker guard,
	// double-check, fail-open) — identical to session auto-launch.
	launchTUIIfNeeded(method)
	fmt.Fprintf(w, "TUI launching (method: %s)\n", method)
	return nil
}

// resolveTUILaunchMethod picks the launch method: explicit flag wins (and must
// be valid), then the config value, then the newWindow default.
func resolveTUILaunchMethod(flagMethod, configMethod string) (string, error) {
	if flagMethod != "" {
		if flagMethod != "newWindow" && flagMethod != "background" {
			return "", fmt.Errorf("invalid --launch-method %q (use one of: newWindow, background)", flagMethod)
		}
		return flagMethod, nil
	}
	if configMethod != "" {
		return configMethod, nil
	}
	return "newWindow", nil
}

// runningTUIPID reports the PID of a live TUI process, if any. It mirrors
// isTUIRunning's detection order (comm-validated PID file first, then the
// comm-filtered process scan) but surfaces the PID for user-facing output.
func runningTUIPID() (int, bool) {
	if tuiPIDFileAlive() {
		data, err := os.ReadFile(tuiPIDPath())
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid, true
			}
		}
	}
	if procs := listTUIProcs(); len(procs) > 0 {
		return procs[0], true
	}
	return 0, false
}
