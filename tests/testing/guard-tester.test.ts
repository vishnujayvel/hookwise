/**
 * Tests for GuardTester in-process guard testing.
 *
 * Verifies:
 * - Direct guards array
 * - Config dict loading
 * - Assertion methods (pass and fail cases)
 * - Batch scenario testing
 * - Config file loading (writes a temp YAML file)
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdtempSync, writeFileSync, mkdirSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { GuardTester } from "../../src/testing/guard-tester.js";
import type { GuardRuleConfig, TestScenario } from "../../src/core/types.js";

describe("GuardTester", () => {
  const sampleGuards: GuardRuleConfig[] = [
    { match: "Bash", action: "block", reason: "No shell access" },
    {
      match: "mcp__gmail__*",
      action: "warn",
      reason: "Email access requires review",
    },
    {
      match: "Write",
      action: "confirm",
      reason: "File write requires confirmation",
    },
  ];

  describe("constructor with guards array", () => {
    it("loads rules from direct array", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      expect(tester.rules).toHaveLength(3);
      expect(tester.rules[0].match).toBe("Bash");
    });

    it("handles empty guards array", () => {
      const tester = new GuardTester({ guards: [] });
      expect(tester.rules).toHaveLength(0);
    });
  });

  describe("constructor with configDict", () => {
    it("extracts guards from config dict", () => {
      const tester = new GuardTester({
        configDict: {
          guards: sampleGuards,
          version: 1,
        },
      });
      expect(tester.rules).toHaveLength(3);
    });

    it("handles config dict without guards key", () => {
      const tester = new GuardTester({
        configDict: { version: 1 },
      });
      expect(tester.rules).toHaveLength(0);
    });
  });

  describe("constructor with configPath", () => {
    let tempDir: string;

    beforeEach(() => {
      tempDir = mkdtempSync(join(tmpdir(), "hookwise-guard-tester-"));
      // Set HOOKWISE_STATE_DIR to avoid touching the real config
      process.env.HOOKWISE_STATE_DIR = join(tempDir, ".hookwise");
    });

    afterEach(() => {
      delete process.env.HOOKWISE_STATE_DIR;
      rmSync(tempDir, { recursive: true, force: true });
    });

    it("loads guards from a YAML config file", () => {
      const yamlContent = `
version: 1
guards:
  - match: "Bash"
    action: block
    reason: "No shell"
  - match: "Edit"
    action: warn
    reason: "Be careful"
`;
      writeFileSync(join(tempDir, "hookwise.yaml"), yamlContent);

      const tester = new GuardTester({ configPath: tempDir });
      expect(tester.rules).toHaveLength(2);
      expect(tester.rules[0].match).toBe("Bash");
      expect(tester.rules[0].action).toBe("block");
    });
  });

  describe("testToolCall", () => {
    it("returns block for matching blocked tool", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      const result = tester.testToolCall("Bash");
      expect(result.action).toBe("block");
      expect(result.reason).toBe("No shell access");
    });

    it("returns allow for non-matching tool", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      const result = tester.testToolCall("Read");
      expect(result.action).toBe("allow");
    });

    it("returns warn for glob-matched tool", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      const result = tester.testToolCall("mcp__gmail__send_email");
      expect(result.action).toBe("warn");
    });

    it("passes tool input to the evaluator", () => {
      const guards: GuardRuleConfig[] = [
        {
          match: "Bash",
          action: "block",
          reason: "No force push",
          when: 'command contains "force"',
        },
      ];
      const tester = new GuardTester({ guards });

      const blocked = tester.testToolCall("Bash", { command: "git push --force" });
      expect(blocked.action).toBe("block");

      const allowed = tester.testToolCall("Bash", { command: "git push" });
      expect(allowed.action).toBe("allow");
    });
  });

  describe("assertBlocked", () => {
    it("passes for blocked tool", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      expect(() => tester.assertBlocked("Bash")).not.toThrow();
    });

    it("passes with reason check", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      expect(() => tester.assertBlocked("Bash", undefined, "shell")).not.toThrow();
    });

    it("throws for allowed tool", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      expect(() => tester.assertBlocked("Read")).toThrow(/allow/i);
    });

    it("throws when reason doesn't match", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      expect(() =>
        tester.assertBlocked("Bash", undefined, "nonexistent reason")
      ).toThrow(/nonexistent reason/);
    });
  });

  describe("assertAllowed", () => {
    it("passes for allowed tool", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      expect(() => tester.assertAllowed("Read")).not.toThrow();
    });

    it("throws for blocked tool", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      expect(() => tester.assertAllowed("Bash")).toThrow(/block/i);
    });

    it("throws for warned tool", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      expect(() =>
        tester.assertAllowed("mcp__gmail__send_email")
      ).toThrow(/warn/i);
    });
  });

  describe("assertWarns", () => {
    it("passes for warned tool", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      expect(() =>
        tester.assertWarns("mcp__gmail__send_email")
      ).not.toThrow();
    });

    it("throws for blocked tool", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      expect(() => tester.assertWarns("Bash")).toThrow(/block/i);
    });

    it("throws for allowed tool", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      expect(() => tester.assertWarns("Read")).toThrow(/allow/i);
    });
  });

  describe("runScenarios", () => {
    it("batch-tests multiple scenarios", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      const scenarios: TestScenario[] = [
        { toolName: "Bash", expected: "block" },
        { toolName: "Read", expected: "allow" },
        { toolName: "mcp__gmail__send_email", expected: "warn" },
        { toolName: "Write", expected: "confirm" },
      ];

      const results = tester.runScenarios(scenarios);
      expect(results).toHaveLength(4);
      expect(results.every((r) => r.passed)).toBe(true);
    });

    it("reports failed scenarios correctly", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      const scenarios: TestScenario[] = [
        { toolName: "Bash", expected: "allow" }, // Wrong: Bash is blocked
        { toolName: "Read", expected: "allow" }, // Correct
      ];

      const results = tester.runScenarios(scenarios);
      expect(results[0].passed).toBe(false);
      expect(results[0].guardResult.action).toBe("block");
      expect(results[1].passed).toBe(true);
    });

    it("passes tool input to scenarios", () => {
      const guards: GuardRuleConfig[] = [
        {
          match: "Bash",
          action: "block",
          reason: "Dangerous",
          when: 'command contains "rm"',
        },
      ];
      const tester = new GuardTester({ guards });
      const scenarios: TestScenario[] = [
        {
          toolName: "Bash",
          toolInput: { command: "rm -rf /" },
          expected: "block",
        },
        {
          toolName: "Bash",
          toolInput: { command: "ls" },
          expected: "allow",
        },
      ];

      const results = tester.runScenarios(scenarios);
      expect(results[0].passed).toBe(true);
      expect(results[1].passed).toBe(true);
    });

    it("returns empty array for empty scenarios", () => {
      const tester = new GuardTester({ guards: sampleGuards });
      const results = tester.runScenarios([]);
      expect(results).toEqual([]);
    });
  });
});
