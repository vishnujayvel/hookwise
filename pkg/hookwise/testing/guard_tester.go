// Package hwtesting provides testing utilities for hookwise configurations.
//
// GuardTester evaluates guard rules in-process without running a full dispatch
// cycle. HookRunner invokes the hookwise binary as a subprocess for integration
// testing. Both are designed for use in external test suites that validate
// hookwise.yaml configurations.
package hwtesting

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vishnujayvel/hookwise/internal/core"
	"gopkg.in/yaml.v3"
)

// GuardTester evaluates guard rules in-process without a full dispatch cycle.
// It loads rules from a hookwise config file and exposes methods to test
// individual tool names or batch scenarios against those rules.
type GuardTester struct {
	guards []core.GuardRuleConfig
}

// TestScenario describes a single guard evaluation scenario.
type TestScenario struct {
	// Name is a human-readable label for the scenario (used in reports).
	Name string
	// ToolName is the tool name to evaluate (e.g. "Bash", "mcp__gmail__*").
	ToolName string
	// Payload is the data map passed to guard condition evaluation.
	// Field paths like "tool_input.command" are resolved against this map.
	Payload map[string]interface{}
	// Expected is the action the guard should produce: "allow", "block", "warn", or "confirm".
	Expected string
}

// GuardResult is the outcome of evaluating a single tool name against the guard rules.
type GuardResult struct {
	// Action is the resolved guard action: "allow", "block", "warn", or "confirm".
	Action string
	// Reason is the human-readable reason from the matched rule (empty if no match).
	Reason string
	// RuleName is the match pattern of the rule that fired (empty if no match).
	RuleName string
	// Passed is true if Action matches the expected value from a TestScenario.
	// For standalone Evaluate calls (no Expected), Passed is always true.
	Passed bool
}

// NewGuardTester creates a GuardTester by loading guard rules from the config
// file at configPath. The path can point to a hookwise.yaml file or a directory
// containing one (the file name "hookwise.yaml" is appended automatically).
//
// Returns an error if the file cannot be read or parsed.
func NewGuardTester(configPath string) (*GuardTester, error) {
	// If configPath is a directory, append the default config file name.
	info, err := os.Stat(configPath)
	if err != nil {
		return nil, fmt.Errorf("hwtesting: cannot stat config path %q: %w", configPath, err)
	}
	if info.IsDir() {
		configPath = filepath.Join(configPath, "hookwise.yaml")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("hwtesting: cannot read config %q: %w", configPath, err)
	}

	var raw struct {
		Guards []core.GuardRuleConfig `yaml:"guards"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("hwtesting: cannot parse config %q: %w", configPath, err)
	}

	return &GuardTester{guards: raw.Guards}, nil
}

// NewGuardTesterFromRules creates a GuardTester directly from a slice of guard
// rule definitions. This is useful for programmatic tests that build rules
// in-memory rather than loading from a file.
func NewGuardTesterFromRules(rules []GuardRule) *GuardTester {
	coreRules := make([]core.GuardRuleConfig, len(rules))
	for i, r := range rules {
		coreRules[i] = core.GuardRuleConfig{
			Match:  r.Match,
			Action: r.Action,
			Reason: r.Reason,
			When:   r.When,
			Unless: r.Unless,
		}
	}
	return &GuardTester{guards: coreRules}
}

// GuardRule is a user-facing guard rule definition that mirrors the YAML config
// structure without requiring callers to import internal/core.
type GuardRule struct {
	Match  string // Tool name or glob pattern (e.g. "Bash", "mcp__gmail__*")
	Action string // "block", "warn", or "confirm"
	Reason string // Human-readable reason
	When   string // Optional condition: rule applies only when true
	Unless string // Optional condition: rule is skipped when true
}

// Evaluate tests a single tool name against the loaded guard rules.
// The payload map is used for when/unless condition evaluation.
// Returns a GuardResult with Passed always set to true (no expected value).
func (gt *GuardTester) Evaluate(toolName string, payload map[string]interface{}) *GuardResult {
	coreResult := core.Evaluate(toolName, payload, gt.guards)

	ruleName := ""
	if coreResult.MatchedRule != nil {
		ruleName = coreResult.MatchedRule.Match
	}

	return &GuardResult{
		Action:   coreResult.Action,
		Reason:   coreResult.Reason,
		RuleName: ruleName,
		Passed:   true, // no expectation to compare against
	}
}

// EvaluateAll runs multiple scenarios and returns all results.
// Each result's Passed field reflects whether the actual action matched the
// scenario's Expected value.
func (gt *GuardTester) EvaluateAll(scenarios []TestScenario) []GuardResult {
	results := make([]GuardResult, len(scenarios))

	for i, scenario := range scenarios {
		coreResult := core.Evaluate(scenario.ToolName, scenario.Payload, gt.guards)

		ruleName := ""
		if coreResult.MatchedRule != nil {
			ruleName = coreResult.MatchedRule.Match
		}

		results[i] = GuardResult{
			Action:   coreResult.Action,
			Reason:   coreResult.Reason,
			RuleName: ruleName,
			Passed:   coreResult.Action == scenario.Expected,
		}
	}

	return results
}

// Rules returns the number of guard rules loaded in this tester.
func (gt *GuardTester) Rules() int {
	return len(gt.guards)
}
