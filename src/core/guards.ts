/**
 * Guard rule matching engine for hookwise v1.0
 *
 * Implements first-match-wins firewall-style rule evaluation:
 * - Exact tool name matching: "Bash" matches toolName === "Bash"
 * - Glob pattern matching via picomatch: "mcp__gmail__*" matches mcp__gmail__send_email
 * - Condition expressions: when/unless with 6 operators
 * - No eval() — strict regex-based parser only
 */

import picomatch from "picomatch";
import type { GuardRule, GuardResult, ParsedCondition } from "./types.js";
import { logDebug } from "./errors.js";

// --- Condition Parser ---

/**
 * Regex for parsing condition expressions.
 *
 * Format: <field_path> <operator> <value>
 * Operators: contains, starts_with, ends_with, ==, equals, matches
 *
 * The value can be:
 * - Double-quoted: "some value"
 * - Single-quoted: 'some value' (v0.1.0 compat)
 * - Unquoted: single_word (v0.1.0 compat)
 */
const CONDITION_REGEX =
  /^([\w.]+)\s+(contains|starts_with|ends_with|==|equals|matches)\s+(?:"((?:[^"\\]|\\.)*)"|'((?:[^'\\]|\\.)*)'|(\S+))$/;

/**
 * Parse a condition expression string into a structured ParsedCondition.
 *
 * Returns null if the expression is malformed or uses an unknown operator.
 * No eval() is used — strict regex-based parsing only.
 *
 * @param expression - Condition string like `tool_input.command contains "force push"`
 */
export function parseCondition(expression: string): ParsedCondition | null {
  if (!expression || typeof expression !== "string") return null;

  const trimmed = expression.trim();
  const match = CONDITION_REGEX.exec(trimmed);
  if (!match) return null;

  const [, fieldPath, operator, doubleQuoted, singleQuoted, unquoted] = match;
  const value = doubleQuoted ?? singleQuoted ?? unquoted;

  return {
    fieldPath,
    operator: operator as ParsedCondition["operator"],
    value,
  };
}

// --- Field Path Resolution ---

/**
 * Resolve a dot-notation field path against a data object.
 *
 * Returns the string value at the path, or null if not found or not a string.
 * Safely handles missing keys and non-object intermediates.
 *
 * @param data - Root object to traverse
 * @param fieldPath - Dot-separated path like "tool_input.command"
 */
export function resolveFieldPath(
  data: Record<string, unknown>,
  fieldPath: string
): string | null {
  const parts = fieldPath.split(".");
  let current: unknown = data;

  for (const part of parts) {
    if (current === null || current === undefined || typeof current !== "object") {
      return null;
    }
    current = (current as Record<string, unknown>)[part];
  }

  if (typeof current === "string") return current;
  if (current === null || current === undefined) return null;
  // Convert numbers and booleans to string for comparison
  if (typeof current === "number" || typeof current === "boolean") {
    return String(current);
  }
  return null;
}

// --- Condition Evaluation ---

/**
 * Evaluate a condition expression against a tool input object.
 *
 * Returns true/false if the condition can be evaluated, null if malformed.
 *
 * @param expression - Condition string
 * @param toolInput - Tool input data to evaluate against
 */
export function evaluateCondition(
  expression: string,
  toolInput: Record<string, unknown>
): boolean | null {
  const parsed = parseCondition(expression);
  if (!parsed) return null;

  const fieldValue = resolveFieldPath(toolInput, parsed.fieldPath);
  if (fieldValue === null) return false;

  switch (parsed.operator) {
    case "contains":
      return fieldValue.includes(parsed.value);

    case "starts_with":
      return fieldValue.startsWith(parsed.value);

    case "ends_with":
      return fieldValue.endsWith(parsed.value);

    case "==":
    case "equals":
      return fieldValue === parsed.value;

    case "matches": {
      try {
        const regex = new RegExp(parsed.value);
        return regex.test(fieldValue);
      } catch {
        // Invalid regex pattern — treat as no match
        logDebug("Invalid regex in condition", { pattern: parsed.value });
        return false;
      }
    }

    default:
      return null;
  }
}

// --- Guard Engine ---

/**
 * Evaluate a tool call against an ordered list of guard rules.
 *
 * Uses first-match-wins semantics (firewall rules):
 * 1. Iterate rules in order
 * 2. Check if tool name matches (exact or glob)
 * 3. Check `when` condition: rule only matches if condition is true
 * 4. Check `unless` condition: rule is skipped if condition is true
 * 5. First matching rule wins — return its action and reason
 * 6. No match = { action: "allow" }
 *
 * @param toolName - The tool being invoked (e.g., "Bash", "mcp__gmail__send_email")
 * @param toolInput - The tool's input parameters
 * @param rules - Ordered guard rules from config
 */
export function evaluate(
  toolName: string,
  toolInput: Record<string, unknown>,
  rules: GuardRule[]
): GuardResult {
  for (const rule of rules) {
    // Step 1: Check tool name match (exact or glob)
    const isMatch = matchToolName(toolName, rule.match);
    if (!isMatch) continue;

    // Step 2: Check `when` condition — rule only applies if true
    if (rule.when) {
      const whenResult = evaluateCondition(rule.when, toolInput);
      if (whenResult !== true) continue;
    }

    // Step 3: Check `unless` condition — rule skipped if true
    if (rule.unless) {
      const unlessResult = evaluateCondition(rule.unless, toolInput);
      if (unlessResult === true) continue;
    }

    // First match wins
    logDebug("Guard rule matched", { rule: rule.match, action: rule.action });
    return {
      action: rule.action,
      reason: rule.reason,
      matchedRule: rule,
    };
  }

  // No match = allow
  return { action: "allow" };
}

/**
 * Check if a tool name matches a rule's match pattern.
 * Supports exact matching and glob patterns via picomatch.
 */
function matchToolName(toolName: string, pattern: string): boolean {
  // Exact match (fast path)
  if (pattern === toolName) return true;

  // Glob match via picomatch
  const isGlob = picomatch.isMatch(toolName, pattern);
  return isGlob;
}
