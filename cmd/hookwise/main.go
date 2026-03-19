package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Injected at build time via -ldflags.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

// ANSI color helpers — shared across multiple command files.
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiGray   = "\033[90m"
)

func main() {
	rootCmd := newRootCmd()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// newRootCmd creates the root command with all subcommands attached.
// Extracted from main() so tests can invoke commands without exec-ing a binary.
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "hookwise",
		Short:   "Config-driven hook framework for Claude Code",
		Long:    "Hookwise provides guards, analytics, coaching, feeds, and an interactive TUI for Claude Code sessions.",
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, buildDate),
	}

	rootCmd.AddCommand(
		newDispatchCmd(),
		newInitCmd(),
		newDoctorCmd(),
		newStatsCmd(),
		newDiffCmd(),
		newLogCmd(),
		newStatusLineCmd(),
		newTestCmd(),
		newUpgradeCmd(),
		newNotificationsCmd(),
		newDaemonCmd(),
	)

	return rootCmd
}
