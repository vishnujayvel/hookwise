package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/hooks"
)

// auditSchemaVersion identifies the shape of `hookwise audit --json` output.
// Bump when the structure changes incompatibly.
const auditSchemaVersion = 1

// errAuditFailed signals FAIL-level findings; main translates it into exit
// code 1. The message is silenced (findings were already rendered).
var errAuditFailed = errors.New("audit found FAIL-level issues")

// auditReport is the JSON shape for `hookwise audit --json`. Performance data
// is deliberately absent: per-dispatch latency tracking does not exist yet,
// and the JSON contract must never carry fabricated numbers.
type auditReport struct {
	SchemaVersion int                `json:"schema_version"`
	SettingsPaths []string           `json:"settings_paths"`
	Inventory     auditInventory     `json:"inventory"`
	Findings      []auditFindingJSON `json:"findings"`
	Summary       auditSummary       `json:"summary"`
}

type auditInventory struct {
	TotalHooks int              `json:"total_hooks"`
	ByEvent    map[string]int   `json:"by_event"`
	Hooks      []auditHookEntry `json:"hooks"`
}

type auditHookEntry struct {
	Event      string `json:"event"`
	Matcher    string `json:"matcher"`
	Type       string `json:"type"`
	Command    string `json:"command"`
	SourceFile string `json:"source_file"`
}

type auditFindingJSON struct {
	Level   string   `json:"level"`
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
}

type auditSummary struct {
	Result   string `json:"result"` // PASS | WARN | FAIL
	Warnings int    `json:"warnings"`
	Failures int    `json:"failures"`
}

func newAuditCmd() *cobra.Command {
	var jsonOut bool
	var projectDir string

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Scan Claude Code hook configuration for health issues",
		Long: "Scans Claude Code settings files for hook-safety issues: inventory/sprawl,\n" +
			"missing binaries, network-dependent hooks on hot paths, and duplicate or\n" +
			"overlapping hooks. Exits 0 on PASS/WARN, 1 on FAIL.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudit(cmd, jsonOut, projectDir)
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit a schema-versioned JSON report instead of text")
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Scan <dir>/.claude/settings.json + settings.local.json instead of the user-level settings")
	return cmd
}

// auditSettingsPaths resolves which settings files to scan: the project pair
// when --project-dir is given, otherwise the user-level defaults.
func auditSettingsPaths(projectDir string) []string {
	if projectDir != "" {
		return hooks.ProjectSettingsPaths(projectDir)
	}
	return hooks.DefaultSettingsPaths()
}

// auditFindings runs the scan once and returns the inventory plus the full
// finding list. Malformed settings files become FAIL findings (never a crash):
// a broken settings.json means the hook system's real state is unknowable,
// which for a health audit is a failure — unlike doctor, which folds parse
// errors into its warning count among many other checks.
func auditFindings(paths []string) (*hooks.Inventory, []hooks.Finding) {
	inv, err := hooks.Scan(paths)
	if err != nil {
		// Scan currently never returns a non-nil error, but stay fail-safe.
		return &hooks.Inventory{}, []hooks.Finding{{
			Level:   hooks.LevelFail,
			Code:    "hook-settings",
			Message: fmt.Sprintf("settings scan failed: %v", err),
		}}
	}

	var out []hooks.Finding
	for _, pe := range inv.ParseErrors {
		out = append(out, hooks.Finding{
			Level:   hooks.LevelFail,
			Code:    "hook-settings",
			Message: fmt.Sprintf("%s could not be parsed", pe.File),
			Details: []string{
				pe.Err.Error(),
				"Hooks in this file are invisible to the scan — fix the JSON and re-run.",
			},
		})
	}
	return inv, append(out, hooks.AllFindings(inv, nil)...)
}

// summarize folds findings into the audit verdict. INFO counts as a warning
// (matching doctor's checkHookSafety accounting); SCAN/PASS are neutral.
func summarize(findings []hooks.Finding) auditSummary {
	s := auditSummary{Result: "PASS"}
	for _, f := range findings {
		switch f.Level {
		case hooks.LevelWarn, hooks.LevelInfo:
			s.Warnings++
		case hooks.LevelFail:
			s.Failures++
		}
	}
	switch {
	case s.Failures > 0:
		s.Result = "FAIL"
	case s.Warnings > 0:
		s.Result = "WARN"
	}
	return s
}

func runAudit(cmd *cobra.Command, jsonOut bool, projectDir string) error {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	paths := auditSettingsPaths(projectDir)
	inv, findings := auditFindings(paths)
	summary := summarize(findings)

	w := cmd.OutOrStdout()
	if jsonOut {
		if err := renderAuditJSON(w, paths, inv, findings, summary); err != nil {
			return err
		}
	} else {
		renderAuditText(w, paths, findings, summary)
	}

	if summary.Failures > 0 {
		return errAuditFailed
	}
	return nil
}

// renderAuditText prints the human-readable report in doctor's house style.
func renderAuditText(w io.Writer, paths []string, findings []hooks.Finding, summary auditSummary) {
	fmt.Fprintln(w, "hookwise audit — hook health scan")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Settings scanned:")
	for _, p := range paths {
		fmt.Fprintf(w, "  %s\n", p)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "HOOKS")
	for _, f := range findings {
		renderHookFinding(w, f)
	}
	fmt.Fprintln(w)

	// Per-dispatch latency tracking does not exist yet; say so rather than
	// fabricate numbers. This section is text-only and omitted from JSON.
	fmt.Fprintln(w, "PERFORMANCE")
	fmt.Fprintln(w, "  latency tracking not enabled")
	fmt.Fprintln(w)

	fmt.Fprintln(w, strings.Repeat("-", 40))
	switch summary.Result {
	case "FAIL":
		fmt.Fprintf(w, "Result: FAIL (%d warning(s), %d failure(s))\n", summary.Warnings, summary.Failures)
	case "WARN":
		fmt.Fprintf(w, "Result: WARN (%d warning(s))\n", summary.Warnings)
	default:
		fmt.Fprintln(w, "Result: PASS")
	}
}

// renderAuditJSON emits the schema-versioned report, carrying the raw hook
// entries from the same scan that produced the findings.
func renderAuditJSON(w io.Writer, paths []string, inv *hooks.Inventory, findings []hooks.Finding, summary auditSummary) error {
	byEvent := inv.CountByEvent()
	entries := make([]auditHookEntry, 0, len(inv.Entries))
	for _, e := range inv.Entries {
		entries = append(entries, auditHookEntry{
			Event:      e.Event,
			Matcher:    e.Matcher,
			Type:       e.Type,
			Command:    e.Command,
			SourceFile: e.SourceFile,
		})
	}

	fs := make([]auditFindingJSON, 0, len(findings))
	for _, f := range findings {
		fs = append(fs, auditFindingJSON{
			Level:   f.Level,
			Code:    f.Code,
			Message: f.Message,
			Details: f.Details,
		})
	}

	report := auditReport{
		SchemaVersion: auditSchemaVersion,
		SettingsPaths: paths,
		Inventory: auditInventory{
			TotalHooks: len(inv.Entries),
			ByEvent:    byEvent,
			Hooks:      entries,
		},
		Findings: fs,
		Summary:  summary,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
