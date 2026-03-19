package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vishnujayvel/hookwise/internal/core"
)

func newTestCmd() *cobra.Command {
	var projectDir string

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Evaluate guard test scenarios",
		Long:  "Loads config, creates synthetic test payloads for each guard rule, evaluates them, and reports pass/fail.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTest(cmd, projectDir)
		},
	}

	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory (defaults to cwd)")
	return cmd
}

func runTest(cmd *cobra.Command, projectDir string) error {
	if projectDir == "" {
		var err error
		projectDir, err = os.Getwd()
		if err != nil {
			projectDir = "."
		}
	}

	config, err := core.LoadConfig(projectDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "hookwise test -- guard rule evaluation")
	fmt.Fprintln(w, strings.Repeat("-", 40))

	if len(config.Guards) == 0 {
		fmt.Fprintln(w, "No guard rules defined. Nothing to test.")
		return nil
	}

	passed := 0
	failed := 0

	for i, rule := range config.Guards {
		// Create a synthetic payload that should trigger this rule.
		payload := buildTestPayload(rule)
		result := core.Evaluate(rule.Match, payload, config.Guards)

		// The rule should match and produce its expected action.
		expectAction := rule.Action
		actualAction := result.Action

		if actualAction == expectAction {
			fmt.Fprintf(w, "PASS  [%d] match=%q action=%s\n", i+1, rule.Match, actualAction)
			passed++
		} else {
			fmt.Fprintf(w, "FAIL  [%d] match=%q expected=%s got=%s\n", i+1, rule.Match, expectAction, actualAction)
			failed++
		}
	}

	fmt.Fprintln(w, strings.Repeat("-", 40))
	fmt.Fprintf(w, "Results: %d passed, %d failed, %d total\n", passed, failed, passed+failed)

	if failed > 0 {
		return fmt.Errorf("%d guard test(s) failed", failed)
	}
	return nil
}

// buildTestPayload creates a synthetic payload designed to trigger the given rule.
// If the rule has a "when" condition, we try to populate the payload so the condition is satisfied.
func buildTestPayload(rule core.GuardRuleConfig) map[string]interface{} {
	payload := map[string]interface{}{
		"session_id": "test-session",
		"tool_name":  rule.Match,
	}

	// Parse the "when" condition to build a payload that satisfies it.
	if rule.When != "" {
		parsed := core.ParseCondition(rule.When)
		if parsed != nil {
			setNestedField(payload, parsed.FieldPath, buildMatchValue(parsed))
		}
	}

	return payload
}

// setNestedField sets a value at a dot-separated path in a map.
func setNestedField(m map[string]interface{}, path string, value interface{}) {
	parts := strings.Split(path, ".")
	current := m
	for i := 0; i < len(parts)-1; i++ {
		child, ok := current[parts[i]].(map[string]interface{})
		if !ok {
			child = make(map[string]interface{})
			current[parts[i]] = child
		}
		current = child
	}
	current[parts[len(parts)-1]] = value
}

// buildMatchValue creates a string value that satisfies the given parsed condition.
func buildMatchValue(cond *core.ParsedCondition) string {
	switch cond.Operator {
	case "contains":
		return "prefix_" + cond.Value + "_suffix"
	case "starts_with":
		return cond.Value + "_rest"
	case "ends_with":
		return "start_" + cond.Value
	case "==", "equals":
		return cond.Value
	case "matches":
		return cond.Value // best effort: use the regex pattern as a literal
	default:
		return cond.Value
	}
}
