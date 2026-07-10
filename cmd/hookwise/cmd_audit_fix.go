package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/hooks"
)

// auditStdinIsInteractive reports whether stdin is a terminal. It is a
// package var so tests can force either branch: applying removals is only
// permitted with a human answering the per-change prompts (design F —
// non-interactive stdin must refuse to apply).
var auditStdinIsInteractive = func() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// runAuditFix implements `audit --fix` / `audit --dry-run`: plan exact
// intra-file duplicate removals, print recommendations for everything else,
// then (fix only, interactive only) prompt y/N per removal and apply the
// accepted ones per file. A declined removal is never added to acceptedIDs,
// so it is unreachable by ApplyRemovals by construction.
func runAuditFix(cmd *cobra.Command, paths []string, dryRun bool) error {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	w := cmd.OutOrStdout()

	plan, err := hooks.PlanDuplicateRemovals(paths)
	if err != nil {
		return err
	}

	fmt.Fprintln(w, "hookwise audit --fix — exact duplicate hook removal")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Settings scanned:")
	for _, p := range paths {
		fmt.Fprintf(w, "  %s\n", p)
	}
	fmt.Fprintln(w)

	for _, pe := range plan.SkippedFiles {
		fmt.Fprintf(w, "SKIP  %s: %v (nothing will be removed from this file)\n", pe.File, pe.Err)
	}
	for _, rec := range plan.Recommendations {
		fmt.Fprintf(w, "NOTE  [%s] %s\n", rec.Kind, rec.Message)
	}
	if len(plan.SkippedFiles) > 0 || len(plan.Recommendations) > 0 {
		fmt.Fprintln(w)
	}

	if len(plan.Removals) == 0 {
		fmt.Fprintln(w, "Nothing to fix: no exact duplicate hook entries found.")
		return nil
	}

	fmt.Fprintf(w, "%d exact duplicate hook entr%s eligible for removal:\n",
		len(plan.Removals), pluralYIes(len(plan.Removals)))
	for i, r := range plan.Removals {
		fmt.Fprintf(w, "  [%d] %s\n      event=%s matcher=%q command=%q\n",
			i+1, r.File, r.Event, r.Matcher, r.Command)
	}
	fmt.Fprintln(w)

	if dryRun {
		fmt.Fprintln(w, "Dry run: no changes made.")
		return nil
	}

	if !auditStdinIsInteractive() {
		fmt.Fprintln(w, "Refusing to apply: stdin is not an interactive terminal.")
		fmt.Fprintln(w, "Removals mutate your Claude Code settings and require per-change confirmation.")
		fmt.Fprintln(w, "Run `hookwise audit --fix` in an interactive terminal to proceed.")
		return nil
	}

	// Per-change confirmation, default No. Only explicitly accepted IDs are
	// ever handed to ApplyRemovals.
	reader := bufio.NewReader(cmd.InOrStdin())
	acceptedByFile := map[string][]string{}
	accepted := 0
	for i, r := range plan.Removals {
		fmt.Fprintf(w, "Remove [%d/%d] event=%s matcher=%q command=%q from %s? [y/N] ",
			i+1, len(plan.Removals), r.Event, r.Matcher, r.Command, r.File)
		line, readErr := reader.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer == "y" || answer == "yes" {
			acceptedByFile[r.File] = append(acceptedByFile[r.File], r.ID)
			accepted++
		}
		fmt.Fprintln(w)
		if readErr != nil {
			if readErr == io.EOF {
				break // remaining prompts default to No
			}
			return readErr
		}
	}

	if accepted == 0 {
		fmt.Fprintln(w, "No removals accepted: no changes made.")
		return nil
	}

	// Apply per file, deterministically in scan-path order.
	var failed bool
	for _, p := range paths {
		ids := acceptedByFile[p]
		if len(ids) == 0 {
			continue
		}
		removed, backup, applyErr := hooks.ApplyRemovals(p, ids, plan.Guards[p])
		if applyErr != nil {
			failed = true
			fmt.Fprintf(w, "FAIL  %s: %v\n", p, applyErr)
			if backup != "" {
				fmt.Fprintf(w, "      backup: %s\n", backup)
			}
			continue
		}
		fmt.Fprintf(w, "OK    %s: removed %d duplicate hook entr%s (backup: %s)\n",
			p, removed, pluralYIes(removed), backup)
	}
	if failed {
		return fmt.Errorf("audit --fix: one or more files could not be fixed")
	}
	return nil
}

// pluralYIes returns "y" for 1 and "ies" otherwise, for "entry/entries".
func pluralYIes(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
