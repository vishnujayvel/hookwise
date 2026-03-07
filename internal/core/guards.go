package core

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/gobwas/glob"
)

// Caches for compiled patterns (hot-path optimization).
var (
	globCache  sync.Map // pattern string -> glob.Glob
	regexCache sync.Map // pattern string -> *regexp.Regexp
)

// --- Condition Parser ---

// conditionRegex parses condition expressions in the format:
//
//	field_path operator value
//
// Operators: contains, starts_with, ends_with, ==, equals, matches
// Value can be double-quoted ("val"), single-quoted ('val'), or unquoted (single_word).
var conditionRegex = regexp.MustCompile(
	`^([\w.]+)\s+(contains|starts_with|ends_with|==|equals|matches)\s+(?:"((?:[^"\\]|\\.)*)"|'((?:[^'\\]|\\.)*)'|(\S+))$`,
)

// hasGlobChars returns true if the pattern contains glob metacharacters.
func hasGlobChars(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[{")
}

// ParseCondition parses a condition expression string into a structured ParsedCondition.
// Returns nil if the expression is malformed or empty.
func ParseCondition(expression string) *ParsedCondition {
	if expression == "" {
		return nil
	}

	trimmed := strings.TrimSpace(expression)
	match := conditionRegex.FindStringSubmatch(trimmed)
	if match == nil {
		return nil
	}

	fieldPath := match[1]
	operator := match[2]

	// Value from first non-empty capture group: double-quoted, single-quoted, or unquoted
	var value string
	switch {
	case match[3] != "":
		value = match[3]
	case match[4] != "":
		value = match[4]
	default:
		value = match[5]
	}

	return &ParsedCondition{
		FieldPath: fieldPath,
		Operator:  operator,
		Value:     value,
	}
}

// ResolveFieldPath traverses a dot-notation path through nested map[string]interface{}.
// Returns nil (not panic) if any intermediate key is missing or the value is not a string-like type.
func ResolveFieldPath(data map[string]interface{}, fieldPath string) *string {
	if data == nil || fieldPath == "" {
		return nil
	}

	parts := strings.Split(fieldPath, ".")
	var current interface{} = data

	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current, ok = m[part]
		if !ok {
			return nil
		}
	}

	switch v := current.(type) {
	case string:
		return &v
	case nil:
		return nil
	default:
		// Convert numbers, booleans, etc. to string for comparison
		s := fmt.Sprintf("%v", v)
		return &s
	}
}

// EvaluateCondition evaluates a condition expression against a data map.
// Returns a pointer to bool:
//   - *true if the condition is satisfied
//   - *false if the condition is not satisfied (including missing field)
//   - nil if the expression is malformed
func EvaluateCondition(expression string, data map[string]interface{}) *bool {
	parsed := ParseCondition(expression)
	if parsed == nil {
		return nil
	}

	fieldValue := ResolveFieldPath(data, parsed.FieldPath)
	if fieldValue == nil {
		f := false
		return &f
	}

	var result bool

	switch parsed.Operator {
	case "contains":
		result = strings.Contains(
			strings.ToLower(*fieldValue),
			strings.ToLower(parsed.Value),
		)

	case "starts_with":
		result = strings.HasPrefix(
			strings.ToLower(*fieldValue),
			strings.ToLower(parsed.Value),
		)

	case "ends_with":
		result = strings.HasSuffix(
			strings.ToLower(*fieldValue),
			strings.ToLower(parsed.Value),
		)

	case "==", "equals":
		result = strings.EqualFold(*fieldValue, parsed.Value)

	case "matches":
		var re *regexp.Regexp
		if cached, ok := regexCache.Load(parsed.Value); ok {
			re = cached.(*regexp.Regexp)
		} else {
			compiled, err := regexp.Compile(parsed.Value)
			if err != nil {
				Logger().Debug("invalid regex in guard condition", "pattern", parsed.Value, "error", err)
				result = false
				break
			}
			regexCache.Store(parsed.Value, compiled)
			re = compiled
		}
		// matches is case-SENSITIVE (unlike all other operators)
		result = re.MatchString(*fieldValue)

	default:
		// Unknown operator
		return nil
	}

	return &result
}

// --- Guard Engine ---

// Evaluate evaluates a tool call against an ordered list of guard rules.
// Uses first-match-wins semantics (firewall rules):
//  1. Iterate rules in order
//  2. Check if tool name matches (exact or glob)
//  3. Check when condition: rule only matches if condition is true
//  4. Check unless condition: rule is skipped if condition is true
//  5. First matching rule wins — return its action and reason
//  6. No match = GuardResult{Action: "allow"}
//
// On errors, fails open (returns allow) per ARCH-1.
func Evaluate(toolName string, payload map[string]interface{}, rules []GuardRuleConfig) GuardResult {
	// Guard against panic — fail open
	defer func() {
		if r := recover(); r != nil {
			Logger().Error("panic in guard evaluation", "recovered", fmt.Sprintf("%v", r))
		}
	}()

	for i := range rules {
		rule := &rules[i]

		// Step 1: Check tool name match (exact or glob)
		if !matchToolName(toolName, rule.Match) {
			continue
		}

		// Step 2: Check when condition — rule only applies if true
		if rule.When != "" {
			whenResult := EvaluateCondition(rule.When, payload)
			if whenResult == nil || !*whenResult {
				continue
			}
		}

		// Step 3: Check unless condition — rule skipped if true
		if rule.Unless != "" {
			unlessResult := EvaluateCondition(rule.Unless, payload)
			if unlessResult != nil && *unlessResult {
				continue
			}
		}

		// First match wins
		Logger().Debug("guard rule matched", "rule", rule.Match, "action", rule.Action)
		matchedRule := *rule
		return GuardResult{
			Action:      rule.Action,
			Reason:      rule.Reason,
			MatchedRule: &matchedRule,
		}
	}

	// No match = allow
	return GuardResult{Action: ActionAllow}
}

// matchToolName checks if a tool name matches a rule's match pattern.
// Uses exact comparison first, then falls back to glob matching if the pattern
// contains glob metacharacters (* ? [ {).
func matchToolName(toolName string, pattern string) bool {
	// Exact match (fast path)
	if pattern == toolName {
		return true
	}

	// Only attempt glob if pattern contains glob characters
	if !hasGlobChars(pattern) {
		return false
	}

	// Check cache first, compile and cache on miss
	if cached, ok := globCache.Load(pattern); ok {
		return cached.(glob.Glob).Match(toolName)
	}
	g, err := glob.Compile(pattern)
	if err != nil {
		Logger().Debug("invalid glob pattern in guard rule", "pattern", pattern, "error", err)
		return false
	}
	globCache.Store(pattern, g)
	return g.Match(toolName)
}
