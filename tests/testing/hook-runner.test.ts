/**
 * Tests for HookRunner subprocess test helper.
 *
 * Verifies:
 * - Basic execution captures stdout/stderr/exitCode
 * - Stdin is piped correctly
 * - Timeout kills process and returns non-zero exit code
 * - Static execute convenience method works
 */

import { describe, it, expect } from "vitest";
import { HookRunner } from "../../src/testing/hook-runner.js";

describe("HookRunner", () => {
  describe("run", () => {
    it("captures stdout from a simple command", () => {
      const runner = new HookRunner('node -e "process.stdout.write(\'hello\')"');
      const result = runner.run("PreToolUse");
      expect(result.stdout).toBe("hello");
      expect(result.exitCode).toBe(0);
    });

    it("captures stderr from a command", () => {
      const runner = new HookRunner(
        'node -e "process.stderr.write(\'error output\')"'
      );
      const result = runner.run("PreToolUse");
      expect(result.stderr).toBe("error output");
      expect(result.exitCode).toBe(0);
    });

    it("captures non-zero exit code", () => {
      const runner = new HookRunner('node -e "process.exit(2)"');
      const result = runner.run("PreToolUse");
      expect(result.exitCode).toBe(2);
    });

    it("pipes stdin correctly (event_type in payload)", () => {
      // Use cat-like behavior: read stdin and echo it
      const runner = new HookRunner(
        'node -e "let d=\'\';process.stdin.on(\'data\',c=>d+=c);process.stdin.on(\'end\',()=>process.stdout.write(d))"'
      );
      const result = runner.run("PreToolUse", { session_id: "test-123" });
      expect(result.exitCode).toBe(0);

      const parsed = JSON.parse(result.stdout);
      expect(parsed.event_type).toBe("PreToolUse");
      expect(parsed.session_id).toBe("test-123");
    });

    it("records duration in milliseconds", () => {
      const runner = new HookRunner('node -e "process.exit(0)"');
      const result = runner.run("PreToolUse");
      expect(result.durationMs).toBeGreaterThanOrEqual(0);
      expect(result.durationMs).toBeLessThan(10000);
    });

    it("timeout kills process and returns non-zero exit code", () => {
      const runner = new HookRunner(
        'node -e "setTimeout(()=>{},30000)"',
        { timeout: 500 }
      );
      const result = runner.run("PreToolUse");
      // Timed out processes typically have non-zero exit or null status
      expect(result.exitCode).not.toBe(0);
    });

    it("per-run timeout overrides constructor timeout", () => {
      const runner = new HookRunner(
        'node -e "setTimeout(()=>{},30000)"',
        { timeout: 30000 } // long default
      );
      const result = runner.run("PreToolUse", undefined, { timeout: 500 });
      expect(result.exitCode).not.toBe(0);
    });
  });

  describe("static execute", () => {
    it("works as convenience method", () => {
      const result = HookRunner.execute(
        'node -e "process.stdout.write(\'static\')"',
        "PreToolUse"
      );
      expect(result.stdout).toBe("static");
      expect(result.exitCode).toBe(0);
    });

    it("passes payload to the command", () => {
      const result = HookRunner.execute(
        'node -e "let d=\'\';process.stdin.on(\'data\',c=>d+=c);process.stdin.on(\'end\',()=>process.stdout.write(d))"',
        "PostToolUse",
        { tool_name: "Bash" }
      );
      const parsed = JSON.parse(result.stdout);
      expect(parsed.event_type).toBe("PostToolUse");
      expect(parsed.tool_name).toBe("Bash");
    });
  });
});
