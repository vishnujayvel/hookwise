/**
 * Tests for guard rule matching engine and condition parser.
 *
 * Verifies:
 * - Exact tool name matching
 * - Glob pattern matching via picomatch
 * - First-match-wins semantics
 * - No match returns allow
 * - Reason preserved in result
 * - All 6 condition operators (positive + negative)
 * - Dot-notation field path resolution
 * - `when` condition gating
 * - `unless` exception logic
 * - Malformed expressions return null
 * - Regex injection safety
 * - Explicit `==` test for v0.1.0 compatibility
 */

import { describe, it, expect } from "vitest";
import {
  evaluate,
  parseCondition,
  evaluateCondition,
  resolveFieldPath,
} from "../../src/core/guards.js";
import type { GuardRule, ParsedCondition } from "../../src/core/types.js";

// --- parseCondition ---

describe("parseCondition", () => {
  it("parses contains operator", () => {
    const result = parseCondition('command contains "force push"');
    expect(result).toEqual({
      fieldPath: "command",
      operator: "contains",
      value: "force push",
    });
  });

  it("parses starts_with operator", () => {
    const result = parseCondition('command starts_with "rm -rf"');
    expect(result).toEqual({
      fieldPath: "command",
      operator: "starts_with",
      value: "rm -rf",
    });
  });

  it("parses ends_with operator", () => {
    const result = parseCondition('file_path ends_with ".env"');
    expect(result).toEqual({
      fieldPath: "file_path",
      operator: "ends_with",
      value: ".env",
    });
  });

  it("parses == operator (v0.1.0 compat)", () => {
    const result = parseCondition('command == "git push --force"');
    expect(result).toEqual({
      fieldPath: "command",
      operator: "==",
      value: "git push --force",
    });
  });

  it("parses equals operator (alias for ==)", () => {
    const result = parseCondition('tool_name equals "Bash"');
    expect(result).toEqual({
      fieldPath: "tool_name",
      operator: "equals",
      value: "Bash",
    });
  });

  it("parses matches operator (regex)", () => {
    const result = parseCondition('command matches "rm\\s+-rf"');
    expect(result).toEqual({
      fieldPath: "command",
      operator: "matches",
      value: "rm\\s+-rf",
    });
  });

  it("parses dot-notation field paths", () => {
    const result = parseCondition('tool_input.command contains "force"');
    expect(result).toEqual({
      fieldPath: "tool_input.command",
      operator: "contains",
      value: "force",
    });
  });

  it("parses deeply nested field paths", () => {
    const result = parseCondition('a.b.c.d contains "test"');
    expect(result).toEqual({
      fieldPath: "a.b.c.d",
      operator: "contains",
      value: "test",
    });
  });

  it("returns null for empty string", () => {
    expect(parseCondition("")).toBeNull();
  });

  it("returns null for null/undefined input", () => {
    expect(parseCondition(null as unknown as string)).toBeNull();
    expect(parseCondition(undefined as unknown as string)).toBeNull();
  });

  it("returns null for malformed expression (multi-word unquoted value)", () => {
    expect(parseCondition("command contains force push")).toBeNull();
  });

  it("returns null for malformed expression (unknown operator)", () => {
    expect(parseCondition('command like "force"')).toBeNull();
  });

  it("returns null for missing value", () => {
    expect(parseCondition("command contains")).toBeNull();
  });

  it("returns null for missing field path", () => {
    expect(parseCondition('contains "force"')).toBeNull();
  });

  it("handles escaped quotes in value", () => {
    const result = parseCondition('command contains "say \\"hello\\""');
    expect(result).not.toBeNull();
    expect(result!.value).toBe('say \\"hello\\"');
  });

  it("parses single-quoted condition values (v0.1.0 compat)", () => {
    const result = parseCondition("tool_input.command contains 'rm -rf'");
    expect(result).toEqual({
      fieldPath: "tool_input.command",
      operator: "contains",
      value: "rm -rf",
    });
  });

  it("parses single-quoted value with escaped single quotes", () => {
    const result = parseCondition("command contains 'say \\'hello\\''");
    expect(result).not.toBeNull();
    expect(result!.value).toBe("say \\'hello\\'");
  });

  it("parses unquoted single-word values (v0.1.0 compat)", () => {
    const result = parseCondition("command contains force");
    expect(result).toEqual({
      fieldPath: "command",
      operator: "contains",
      value: "force",
    });
  });

  it("parses unquoted value with equals operator", () => {
    const result = parseCondition("tool_name == Bash");
    expect(result).toEqual({
      fieldPath: "tool_name",
      operator: "==",
      value: "Bash",
    });
  });
});

// --- resolveFieldPath ---

describe("resolveFieldPath", () => {
  it("resolves top-level string field", () => {
    expect(resolveFieldPath({ command: "ls -la" }, "command")).toBe("ls -la");
  });

  it("resolves nested field via dot notation", () => {
    const data = { tool_input: { command: "git push" } };
    expect(resolveFieldPath(data, "tool_input.command")).toBe("git push");
  });

  it("resolves deeply nested field", () => {
    const data = { a: { b: { c: "deep" } } };
    expect(resolveFieldPath(data, "a.b.c")).toBe("deep");
  });

  it("returns null for missing field", () => {
    expect(resolveFieldPath({ command: "ls" }, "nonexistent")).toBeNull();
  });

  it("returns null for missing nested field", () => {
    const data = { tool_input: {} };
    expect(resolveFieldPath(data, "tool_input.command")).toBeNull();
  });

  it("returns null when intermediate is not an object", () => {
    const data = { tool_input: "not-an-object" };
    expect(resolveFieldPath(data, "tool_input.command")).toBeNull();
  });

  it("converts number values to string", () => {
    const data = { count: 42 };
    expect(resolveFieldPath(data, "count")).toBe("42");
  });

  it("converts boolean values to string", () => {
    const data = { enabled: true };
    expect(resolveFieldPath(data, "enabled")).toBe("true");
  });

  it("returns null for null field value", () => {
    const data = { command: null };
    expect(resolveFieldPath(data, "command")).toBeNull();
  });

  it("returns null for undefined field value", () => {
    const data = {};
    expect(resolveFieldPath(data, "command")).toBeNull();
  });
});

// --- evaluateCondition ---

describe("evaluateCondition", () => {
  describe("contains operator", () => {
    it("returns true when field contains value", () => {
      const result = evaluateCondition(
        'command contains "force push"',
        { command: "git push --force push origin main" }
      );
      expect(result).toBe(true);
    });

    it("returns false when field does not contain value", () => {
      const result = evaluateCondition(
        'command contains "force push"',
        { command: "git push origin main" }
      );
      expect(result).toBe(false);
    });
  });

  describe("starts_with operator", () => {
    it("returns true when field starts with value", () => {
      const result = evaluateCondition(
        'command starts_with "rm -rf"',
        { command: "rm -rf /tmp/dir" }
      );
      expect(result).toBe(true);
    });

    it("returns false when field does not start with value", () => {
      const result = evaluateCondition(
        'command starts_with "rm -rf"',
        { command: "ls -la" }
      );
      expect(result).toBe(false);
    });
  });

  describe("ends_with operator", () => {
    it("returns true when field ends with value", () => {
      const result = evaluateCondition(
        'file_path ends_with ".env"',
        { file_path: "/app/.env" }
      );
      expect(result).toBe(true);
    });

    it("returns false when field does not end with value", () => {
      const result = evaluateCondition(
        'file_path ends_with ".env"',
        { file_path: "/app/.env.example" }
      );
      expect(result).toBe(false);
    });
  });

  describe("== operator (v0.1.0 compat)", () => {
    it("returns true for exact match", () => {
      const result = evaluateCondition(
        'command == "git push --force"',
        { command: "git push --force" }
      );
      expect(result).toBe(true);
    });

    it("returns false for non-exact match", () => {
      const result = evaluateCondition(
        'command == "git push --force"',
        { command: "git push --force origin main" }
      );
      expect(result).toBe(false);
    });
  });

  describe("equals operator", () => {
    it("returns true for exact match", () => {
      const result = evaluateCondition(
        'command equals "ls"',
        { command: "ls" }
      );
      expect(result).toBe(true);
    });

    it("returns false for non-exact match", () => {
      const result = evaluateCondition(
        'command equals "ls"',
        { command: "ls -la" }
      );
      expect(result).toBe(false);
    });

    it("behaves identically to == operator", () => {
      const input = { command: "git push --force" };
      const eq = evaluateCondition('command == "git push --force"', input);
      const equals = evaluateCondition('command equals "git push --force"', input);
      expect(eq).toBe(equals);
    });
  });

  describe("matches operator (regex)", () => {
    it("returns true when regex matches", () => {
      const result = evaluateCondition(
        'command matches "rm\\s+-rf"',
        { command: "rm   -rf /tmp" }
      );
      expect(result).toBe(true);
    });

    it("returns false when regex does not match", () => {
      const result = evaluateCondition(
        'command matches "rm\\s+-rf"',
        { command: "ls -la" }
      );
      expect(result).toBe(false);
    });

    it("handles regex injection safely (invalid regex returns false)", () => {
      const result = evaluateCondition(
        'command matches "[invalid regex"',
        { command: "test" }
      );
      expect(result).toBe(false);
    });
  });

  describe("single-quoted values (v0.1.0 compat)", () => {
    it("evaluates single-quoted contains condition", () => {
      const result = evaluateCondition(
        "tool_input.command contains 'rm -rf'",
        { tool_input: { command: "rm -rf /tmp" } }
      );
      expect(result).toBe(true);
    });

    it("returns false for non-matching single-quoted condition", () => {
      const result = evaluateCondition(
        "tool_input.command contains 'rm -rf'",
        { tool_input: { command: "ls -la" } }
      );
      expect(result).toBe(false);
    });
  });

  describe("unquoted values (v0.1.0 compat)", () => {
    it("evaluates unquoted contains condition", () => {
      const result = evaluateCondition(
        "command contains force",
        { command: "git push --force" }
      );
      expect(result).toBe(true);
    });

    it("returns false for non-matching unquoted condition", () => {
      const result = evaluateCondition(
        "command contains force",
        { command: "git push origin main" }
      );
      expect(result).toBe(false);
    });
  });

  describe("edge cases", () => {
    it("returns null for malformed expression", () => {
      expect(evaluateCondition("bad expression", { command: "ls" })).toBeNull();
    });

    it("returns false when field does not exist", () => {
      const result = evaluateCondition(
        'nonexistent contains "test"',
        { command: "ls" }
      );
      expect(result).toBe(false);
    });

    it("resolves nested dot-notation paths", () => {
      const result = evaluateCondition(
        'tool_input.command contains "force"',
        { tool_input: { command: "git push --force" } }
      );
      expect(result).toBe(true);
    });
  });
});

// --- evaluate (GuardEngine) ---

describe("evaluate", () => {
  describe("exact tool name matching", () => {
    it("matches exact tool name", () => {
      const rules: GuardRule[] = [
        { match: "Bash", action: "block", reason: "No shell access" },
      ];
      const result = evaluate("Bash", {}, rules);
      expect(result.action).toBe("block");
      expect(result.reason).toBe("No shell access");
      expect(result.matchedRule).toBe(rules[0]);
    });

    it("does not match different tool name", () => {
      const rules: GuardRule[] = [
        { match: "Bash", action: "block", reason: "No shell" },
      ];
      const result = evaluate("Write", {}, rules);
      expect(result.action).toBe("allow");
      expect(result.reason).toBeUndefined();
      expect(result.matchedRule).toBeUndefined();
    });
  });

  describe("glob pattern matching", () => {
    it("matches glob pattern with wildcard", () => {
      const rules: GuardRule[] = [
        { match: "mcp__gmail__*", action: "warn", reason: "Email access" },
      ];
      const result = evaluate("mcp__gmail__send_email", {}, rules);
      expect(result.action).toBe("warn");
      expect(result.reason).toBe("Email access");
    });

    it("matches glob pattern with double wildcard", () => {
      const rules: GuardRule[] = [
        { match: "mcp__*", action: "confirm", reason: "MCP tool" },
      ];
      const result = evaluate("mcp__slack__post_message", {}, rules);
      expect(result.action).toBe("confirm");
    });

    it("does not match non-matching glob", () => {
      const rules: GuardRule[] = [
        { match: "mcp__gmail__*", action: "block", reason: "Email" },
      ];
      const result = evaluate("mcp__slack__post_message", {}, rules);
      expect(result.action).toBe("allow");
    });
  });

  describe("first-match-wins", () => {
    it("returns the first matching rule", () => {
      const rules: GuardRule[] = [
        { match: "Bash", action: "warn", reason: "First rule" },
        { match: "Bash", action: "block", reason: "Second rule" },
      ];
      const result = evaluate("Bash", {}, rules);
      expect(result.action).toBe("warn");
      expect(result.reason).toBe("First rule");
    });

    it("skips non-matching rules and matches later ones", () => {
      const rules: GuardRule[] = [
        { match: "Write", action: "block", reason: "No write" },
        { match: "Bash", action: "warn", reason: "Shell warning" },
      ];
      const result = evaluate("Bash", {}, rules);
      expect(result.action).toBe("warn");
      expect(result.reason).toBe("Shell warning");
    });

    it("first match prevails over more specific later rules", () => {
      const rules: GuardRule[] = [
        { match: "mcp__*", action: "warn", reason: "Any MCP" },
        { match: "mcp__gmail__send_email", action: "block", reason: "Exact email" },
      ];
      const result = evaluate("mcp__gmail__send_email", {}, rules);
      expect(result.action).toBe("warn");
      expect(result.reason).toBe("Any MCP");
    });
  });

  describe("no match allows", () => {
    it("returns allow with empty rules", () => {
      const result = evaluate("Bash", {}, []);
      expect(result.action).toBe("allow");
    });

    it("returns allow when no rules match", () => {
      const rules: GuardRule[] = [
        { match: "Write", action: "block", reason: "No write" },
        { match: "Edit", action: "block", reason: "No edit" },
      ];
      const result = evaluate("Bash", {}, rules);
      expect(result.action).toBe("allow");
    });
  });

  describe("reason preserved", () => {
    it("preserves the matched rule's reason in result", () => {
      const rules: GuardRule[] = [
        {
          match: "Bash",
          action: "block",
          reason: "Shell commands are not allowed in this project",
        },
      ];
      const result = evaluate("Bash", {}, rules);
      expect(result.reason).toBe(
        "Shell commands are not allowed in this project"
      );
    });

    it("preserves matchedRule reference", () => {
      const rules: GuardRule[] = [
        { match: "Bash", action: "confirm", reason: "Please confirm" },
      ];
      const result = evaluate("Bash", {}, rules);
      expect(result.matchedRule).toBe(rules[0]);
      expect(result.matchedRule?.action).toBe("confirm");
    });
  });

  describe("when condition gating", () => {
    it("applies rule when condition is true", () => {
      const rules: GuardRule[] = [
        {
          match: "Bash",
          action: "block",
          reason: "No force push",
          when: 'command contains "force push"',
        },
      ];
      const result = evaluate("Bash", { command: "git push --force push" }, rules);
      expect(result.action).toBe("block");
    });

    it("skips rule when condition is false", () => {
      const rules: GuardRule[] = [
        {
          match: "Bash",
          action: "block",
          reason: "No force push",
          when: 'command contains "force push"',
        },
      ];
      const result = evaluate("Bash", { command: "git push origin main" }, rules);
      expect(result.action).toBe("allow");
    });

    it("skips rule when condition field is missing", () => {
      const rules: GuardRule[] = [
        {
          match: "Bash",
          action: "block",
          reason: "No force push",
          when: 'command contains "force push"',
        },
      ];
      const result = evaluate("Bash", {}, rules);
      expect(result.action).toBe("allow");
    });

    it("falls through to next rule when when-condition fails", () => {
      const rules: GuardRule[] = [
        {
          match: "Bash",
          action: "block",
          reason: "No force push",
          when: 'command contains "force push"',
        },
        {
          match: "Bash",
          action: "warn",
          reason: "General shell warning",
        },
      ];
      const result = evaluate("Bash", { command: "ls -la" }, rules);
      expect(result.action).toBe("warn");
      expect(result.reason).toBe("General shell warning");
    });
  });

  describe("unless exception", () => {
    it("applies rule when unless condition is false", () => {
      const rules: GuardRule[] = [
        {
          match: "Bash",
          action: "block",
          reason: "No shell",
          unless: 'command starts_with "ls"',
        },
      ];
      const result = evaluate("Bash", { command: "rm -rf /" }, rules);
      expect(result.action).toBe("block");
    });

    it("skips rule when unless condition is true", () => {
      const rules: GuardRule[] = [
        {
          match: "Bash",
          action: "block",
          reason: "No shell",
          unless: 'command starts_with "ls"',
        },
      ];
      const result = evaluate("Bash", { command: "ls -la" }, rules);
      expect(result.action).toBe("allow");
    });

    it("applies rule when unless field is missing (unless condition is false)", () => {
      const rules: GuardRule[] = [
        {
          match: "Bash",
          action: "block",
          reason: "No shell",
          unless: 'command starts_with "ls"',
        },
      ];
      const result = evaluate("Bash", {}, rules);
      expect(result.action).toBe("block");
    });
  });

  describe("combined when + unless", () => {
    it("matches when both conditions allow", () => {
      const rules: GuardRule[] = [
        {
          match: "Bash",
          action: "block",
          reason: "Dangerous git",
          when: 'command contains "git"',
          unless: 'command contains "status"',
        },
      ];
      // when=true (has git), unless=false (no status) -> matches
      const result = evaluate(
        "Bash",
        { command: "git push --force" },
        rules
      );
      expect(result.action).toBe("block");
    });

    it("skips when when-condition fails", () => {
      const rules: GuardRule[] = [
        {
          match: "Bash",
          action: "block",
          reason: "Dangerous git",
          when: 'command contains "git"',
          unless: 'command contains "status"',
        },
      ];
      // when=false (no git) -> skip
      const result = evaluate("Bash", { command: "ls -la" }, rules);
      expect(result.action).toBe("allow");
    });

    it("skips when unless-condition is true", () => {
      const rules: GuardRule[] = [
        {
          match: "Bash",
          action: "block",
          reason: "Dangerous git",
          when: 'command contains "git"',
          unless: 'command contains "status"',
        },
      ];
      // when=true (has git), unless=true (has status) -> skip
      const result = evaluate("Bash", { command: "git status" }, rules);
      expect(result.action).toBe("allow");
    });
  });

  describe("all three actions", () => {
    it("returns block action", () => {
      const rules: GuardRule[] = [
        { match: "Bash", action: "block", reason: "Blocked" },
      ];
      expect(evaluate("Bash", {}, rules).action).toBe("block");
    });

    it("returns warn action", () => {
      const rules: GuardRule[] = [
        { match: "Bash", action: "warn", reason: "Warning" },
      ];
      expect(evaluate("Bash", {}, rules).action).toBe("warn");
    });

    it("returns confirm action", () => {
      const rules: GuardRule[] = [
        { match: "Bash", action: "confirm", reason: "Confirm" },
      ];
      expect(evaluate("Bash", {}, rules).action).toBe("confirm");
    });
  });
});
