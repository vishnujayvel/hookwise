/**
 * Guard Output Contract Tests
 *
 * Verifies that hookwise declarative guard actions produce output
 * matching Claude Code's expected hook formats. This is the test
 * suite that would have caught the confirm-action bug (Feb 2026).
 *
 * Each guard action (block, confirm, warn) has its output shape
 * tested against Claude Code's PreToolUse hook contract:
 *
 * - block  → hookSpecificOutput.permissionDecision = "deny"
 * - confirm → hookSpecificOutput.permissionDecision = "ask"
 * - warn   → hookSpecificOutput.additionalContext (no permissionDecision)
 * - allow  → null stdout (no output)
 *
 * These are SEMANTIC CONTRACT tests, not unit tests. They verify
 * the meaning of the output, not just its syntactic validity.
 */

import { describe, it, expect } from "vitest";
import { dispatch } from "../../src/core/dispatcher.js";
import { getDefaultConfig } from "../../src/core/config.js";
import type { HookPayload, HooksConfig } from "../../src/core/types.js";

// --- Helpers ---

function makePayload(overrides: Partial<HookPayload> = {}): HookPayload {
  return {
    session_id: "contract-test-session",
    ...overrides,
  };
}

function makeConfig(overrides?: Partial<HooksConfig>): HooksConfig {
  return {
    ...getDefaultConfig(),
    ...overrides,
  };
}

/**
 * Claude Code PreToolUse hook output contract.
 * See: https://docs.anthropic.com/en/docs/claude-code/hooks
 *
 * Valid permissionDecision values: "allow", "deny", "ask"
 * Output shape: { hookSpecificOutput: { hookEventName, permissionDecision, permissionDecisionReason } }
 */
interface ClaudeCodeHookOutput {
  hookSpecificOutput: {
    hookEventName: string;
    permissionDecision: "allow" | "deny" | "ask";
    permissionDecisionReason: string;
  };
}

// --- Contract Tests ---

describe("guard output contracts — Claude Code PreToolUse format", () => {
  describe("block action → permissionDecision: deny", () => {
    it("exact tool name match outputs deny with reason", () => {
      const config = makeConfig();
      config.guards = [
        { match: "Bash", action: "block", reason: "Shell commands blocked" },
      ];

      const result = dispatch(
        "PreToolUse",
        makePayload({ tool_name: "Bash" }),
        { config },
      );

      expect(result.exitCode).toBe(0);
      expect(result.stdout).toBeTruthy();
      const output: ClaudeCodeHookOutput = JSON.parse(result.stdout!);

      // Contract: must use hookSpecificOutput, NOT top-level decision
      expect(output.hookSpecificOutput).toBeDefined();
      expect(output.hookSpecificOutput.hookEventName).toBe("PreToolUse");
      expect(output.hookSpecificOutput.permissionDecision).toBe("deny");
      expect(output.hookSpecificOutput.permissionDecisionReason).toBe("Shell commands blocked");

      // Contract: must NOT have deprecated top-level fields
      expect((output as Record<string, unknown>).decision).toBeUndefined();
      expect((output as Record<string, unknown>).reason).toBeUndefined();
    });

    it("glob pattern match outputs deny", () => {
      const config = makeConfig();
      config.guards = [
        { match: "mcp__gmail__*", action: "block", reason: "Gmail blocked" },
      ];

      const result = dispatch(
        "PreToolUse",
        makePayload({ tool_name: "mcp__gmail__send_email" }),
        { config },
      );

      const output: ClaudeCodeHookOutput = JSON.parse(result.stdout!);
      expect(output.hookSpecificOutput.permissionDecision).toBe("deny");
      expect(output.hookSpecificOutput.permissionDecisionReason).toBe("Gmail blocked");
    });

    it("block with default reason provides fallback", () => {
      const config = makeConfig();
      config.guards = [
        { match: "Bash", action: "block" },
      ];

      const result = dispatch(
        "PreToolUse",
        makePayload({ tool_name: "Bash" }),
        { config },
      );

      const output: ClaudeCodeHookOutput = JSON.parse(result.stdout!);
      expect(output.hookSpecificOutput.permissionDecision).toBe("deny");
      expect(output.hookSpecificOutput.permissionDecisionReason).toBeTruthy();
    });
  });

  describe("confirm action → permissionDecision: ask", () => {
    it("exact tool name match outputs ask with reason", () => {
      const config = makeConfig();
      config.guards = [
        { match: "Write", action: "confirm", reason: "Confirm file write" },
      ];

      const result = dispatch(
        "PreToolUse",
        makePayload({ tool_name: "Write" }),
        { config },
      );

      expect(result.exitCode).toBe(0);
      expect(result.stdout).toBeTruthy();
      const output: ClaudeCodeHookOutput = JSON.parse(result.stdout!);

      // Contract: confirm → ask, NOT block/deny
      expect(output.hookSpecificOutput).toBeDefined();
      expect(output.hookSpecificOutput.hookEventName).toBe("PreToolUse");
      expect(output.hookSpecificOutput.permissionDecision).toBe("ask");
      expect(output.hookSpecificOutput.permissionDecisionReason).toBe("Confirm file write");

      // Contract: must NOT have deprecated top-level fields
      expect((output as Record<string, unknown>).decision).toBeUndefined();
      expect((output as Record<string, unknown>).reason).toBeUndefined();
    });

    it("glob pattern match outputs ask", () => {
      const config = makeConfig();
      config.guards = [
        { match: "mcp__gmail__*", action: "confirm", reason: "Gmail requires confirmation" },
      ];

      const result = dispatch(
        "PreToolUse",
        makePayload({ tool_name: "mcp__gmail__send_email" }),
        { config },
      );

      const output: ClaudeCodeHookOutput = JSON.parse(result.stdout!);
      expect(output.hookSpecificOutput.permissionDecision).toBe("ask");
      expect(output.hookSpecificOutput.permissionDecisionReason).toBe("Gmail requires confirmation");
    });

    it("confirm with default reason provides fallback", () => {
      const config = makeConfig();
      config.guards = [
        { match: "Write", action: "confirm" },
      ];

      const result = dispatch(
        "PreToolUse",
        makePayload({ tool_name: "Write" }),
        { config },
      );

      const output: ClaudeCodeHookOutput = JSON.parse(result.stdout!);
      expect(output.hookSpecificOutput.permissionDecision).toBe("ask");
      expect(output.hookSpecificOutput.permissionDecisionReason).toBeTruthy();
    });

    it("confirm is semantically different from block", () => {
      const confirmConfig = makeConfig();
      confirmConfig.guards = [
        { match: "Write", action: "confirm", reason: "Confirm write" },
      ];
      const blockConfig = makeConfig();
      blockConfig.guards = [
        { match: "Write", action: "block", reason: "Block write" },
      ];

      const confirmResult = dispatch(
        "PreToolUse",
        makePayload({ tool_name: "Write" }),
        { config: confirmConfig },
      );
      const blockResult = dispatch(
        "PreToolUse",
        makePayload({ tool_name: "Write" }),
        { config: blockConfig },
      );

      const confirmOutput: ClaudeCodeHookOutput = JSON.parse(confirmResult.stdout!);
      const blockOutput: ClaudeCodeHookOutput = JSON.parse(blockResult.stdout!);

      // The critical contract: these MUST be different
      expect(confirmOutput.hookSpecificOutput.permissionDecision).toBe("ask");
      expect(blockOutput.hookSpecificOutput.permissionDecision).toBe("deny");
      expect(confirmOutput.hookSpecificOutput.permissionDecision).not.toBe(
        blockOutput.hookSpecificOutput.permissionDecision,
      );
    });
  });

  describe("warn action → additionalContext (no permissionDecision)", () => {
    it("warn outputs additionalContext, not a permission decision", () => {
      const config = makeConfig();
      config.guards = [
        { match: "Bash", action: "warn", reason: "Shell usage detected" },
      ];

      const result = dispatch(
        "PreToolUse",
        makePayload({ tool_name: "Bash" }),
        { config },
      );

      expect(result.exitCode).toBe(0);
      expect(result.stdout).toBeTruthy();
      const output = JSON.parse(result.stdout!);

      // Contract: warn uses additionalContext, NOT permissionDecision
      expect(output.hookSpecificOutput.additionalContext).toBeTruthy();
      expect(output.hookSpecificOutput.additionalContext).toContain("Shell usage detected");
      expect(output.hookSpecificOutput.permissionDecision).toBeUndefined();
    });

    it("warn does not block the operation", () => {
      const config = makeConfig();
      config.guards = [
        { match: "Bash", action: "warn", reason: "Caution" },
      ];

      const result = dispatch(
        "PreToolUse",
        makePayload({ tool_name: "Bash" }),
        { config },
      );

      const output = JSON.parse(result.stdout!);
      // Must NOT have deny or ask
      expect(output.hookSpecificOutput.permissionDecision).toBeUndefined();
      expect((output as Record<string, unknown>).decision).toBeUndefined();
    });
  });

  describe("allow (no match) → null stdout", () => {
    it("unmatched tool produces no output", () => {
      const config = makeConfig();
      config.guards = [
        { match: "Bash", action: "block", reason: "Block bash" },
      ];

      const result = dispatch(
        "PreToolUse",
        makePayload({ tool_name: "Read" }),
        { config },
      );

      expect(result.exitCode).toBe(0);
      expect(result.stdout).toBeNull();
    });

    it("non-PreToolUse events don't trigger guards", () => {
      const config = makeConfig();
      config.guards = [
        { match: "Bash", action: "block", reason: "Block bash" },
      ];

      const result = dispatch(
        "PostToolUse",
        makePayload({ tool_name: "Bash" }),
        { config },
      );

      expect(result.exitCode).toBe(0);
      // Guards only run on PreToolUse
      expect(result.stdout).toBeNull();
    });
  });

  describe("output shape compliance", () => {
    it("block output has exactly the required fields", () => {
      const config = makeConfig();
      config.guards = [
        { match: "Bash", action: "block", reason: "Blocked" },
      ];

      const result = dispatch(
        "PreToolUse",
        makePayload({ tool_name: "Bash" }),
        { config },
      );

      const output = JSON.parse(result.stdout!);
      const hso = output.hookSpecificOutput;

      // Required fields present
      expect(hso).toHaveProperty("hookEventName");
      expect(hso).toHaveProperty("permissionDecision");
      expect(hso).toHaveProperty("permissionDecisionReason");

      // No extra top-level keys
      const topKeys = Object.keys(output);
      expect(topKeys).toEqual(["hookSpecificOutput"]);
    });

    it("confirm output has exactly the required fields", () => {
      const config = makeConfig();
      config.guards = [
        { match: "Write", action: "confirm", reason: "Confirm" },
      ];

      const result = dispatch(
        "PreToolUse",
        makePayload({ tool_name: "Write" }),
        { config },
      );

      const output = JSON.parse(result.stdout!);
      const hso = output.hookSpecificOutput;

      // Required fields present
      expect(hso).toHaveProperty("hookEventName");
      expect(hso).toHaveProperty("permissionDecision");
      expect(hso).toHaveProperty("permissionDecisionReason");

      // No extra top-level keys
      const topKeys = Object.keys(output);
      expect(topKeys).toEqual(["hookSpecificOutput"]);
    });
  });
});
