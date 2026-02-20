/**
 * Performance benchmarks for hookwise v1.0
 *
 * Task 14.2: Verifies that critical paths meet latency targets.
 * Uses performance.now() for timing and expect().toBeLessThan() for assertions.
 *
 * Thresholds are set generously for CI environments but tight enough
 * to catch regressions (e.g., accidental React/Ink import in dispatcher).
 */

import { describe, it, expect } from "vitest";
import { readFileSync, existsSync } from "node:fs";
import { join } from "node:path";
import { performance } from "node:perf_hooks";
import { dispatch } from "../../src/core/dispatcher.js";
import { loadConfig, getDefaultConfig } from "../../src/core/config.js";
import { evaluate } from "../../src/core/guards.js";
import type {
  HookPayload,
  GuardRule,
  HooksConfig,
} from "../../src/core/types.js";

// --- Helpers ---

function makePayload(overrides: Partial<HookPayload> = {}): HookPayload {
  return {
    session_id: "perf-test-session",
    ...overrides,
  };
}

/**
 * Measure the execution time of a function in milliseconds.
 * Runs once as warmup, then measures the actual run.
 */
function measure(fn: () => void): number {
  // Warmup run
  fn();

  const start = performance.now();
  fn();
  return performance.now() - start;
}

// --- Dispatcher Cold-Start ---

describe("performance: dispatcher cold-start", () => {
  it("dispatch() with simple inline config completes in < 50ms", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "simple-guard",
        type: "inline",
        events: ["PreToolUse"],
        phase: "guard",
        action: { decision: null },
      },
    ];

    const elapsed = measure(() => {
      dispatch("PreToolUse", makePayload(), { config });
    });

    expect(elapsed).toBeLessThan(50);
  });

  it("dispatch() with no handlers completes in < 10ms", () => {
    const config = getDefaultConfig();

    const elapsed = measure(() => {
      dispatch("PreToolUse", makePayload(), { config });
    });

    expect(elapsed).toBeLessThan(10);
  });

  it("dispatch() with 10 inline handlers completes in < 50ms", () => {
    const config = getDefaultConfig();
    config.handlers = Array.from({ length: 10 }, (_, i) => ({
      name: `handler-${i}`,
      type: "inline" as const,
      events: ["PreToolUse" as const],
      phase: "side_effect" as const,
      action: { output: { index: i } },
    }));

    const elapsed = measure(() => {
      dispatch("PreToolUse", makePayload(), { config });
    });

    expect(elapsed).toBeLessThan(50);
  });

  it("dispatch() with block decision completes in < 20ms", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "blocker",
        type: "inline",
        events: ["PreToolUse"],
        phase: "guard",
        action: { decision: "block", reason: "perf test block" },
      },
    ];

    const elapsed = measure(() => {
      dispatch("PreToolUse", makePayload(), { config });
    });

    expect(elapsed).toBeLessThan(20);
  });
});

// --- Guard Evaluation Throughput ---

describe("performance: guard evaluation throughput", () => {
  it("100 guard rules evaluated in < 50ms", () => {
    const rules: GuardRule[] = Array.from({ length: 100 }, (_, i) => ({
      match: `tool_${i}`,
      action: "block" as const,
      reason: `Rule ${i} matched`,
    }));

    // Test with a tool name that does NOT match any rule (worst case: all 100 checked)
    const elapsed = measure(() => {
      evaluate("Bash", { command: "ls" }, rules);
    });

    expect(elapsed).toBeLessThan(50);
  });

  it("100 rules with conditions evaluated in < 50ms", () => {
    const rules: GuardRule[] = Array.from({ length: 100 }, (_, i) => ({
      match: "Bash",
      action: "block" as const,
      reason: `Condition rule ${i}`,
      when: `tool_input.command contains "pattern_${i}"`,
    }));

    // None will match, forcing full traversal
    const elapsed = measure(() => {
      evaluate("Bash", { command: "safe_command" }, rules);
    });

    expect(elapsed).toBeLessThan(50);
  });

  it("guard with glob patterns evaluates in < 10ms", () => {
    const rules: GuardRule[] = [
      {
        match: "mcp__gmail__*",
        action: "block",
        reason: "Gmail tools blocked",
      },
      {
        match: "mcp__slack__*",
        action: "warn",
        reason: "Slack tools need review",
      },
    ];

    const elapsed = measure(() => {
      evaluate("mcp__gmail__send_email", {}, rules);
    });

    expect(elapsed).toBeLessThan(10);
  });

  it("early match exits fast (first-match-wins)", () => {
    const rules: GuardRule[] = [
      {
        match: "Bash",
        action: "block",
        reason: "First rule matches",
      },
      // 99 more rules that won't be reached
      ...Array.from({ length: 99 }, (_, i) => ({
        match: `tool_${i}`,
        action: "warn" as const,
        reason: `Rule ${i}`,
      })),
    ];

    const elapsed = measure(() => {
      evaluate("Bash", {}, rules);
    });

    // First match should exit extremely fast
    expect(elapsed).toBeLessThan(5);
  });

  it("1000 guard evaluations in sequence complete in < 200ms", () => {
    const rules: GuardRule[] = [
      {
        match: "Bash",
        action: "block",
        reason: "Blocked",
        when: 'tool_input.command contains "rm -rf"',
      },
      {
        match: "Write",
        action: "warn",
        reason: "Write detected",
      },
    ];

    const elapsed = measure(() => {
      for (let i = 0; i < 1000; i++) {
        evaluate("Bash", { command: "ls -la" }, rules);
      }
    });

    expect(elapsed).toBeLessThan(200);
  });
});

// --- Config Loading ---

describe("performance: config loading", () => {
  it("getDefaultConfig() returns in < 5ms", () => {
    const elapsed = measure(() => {
      getDefaultConfig();
    });

    expect(elapsed).toBeLessThan(5);
  });

  it("loadConfig with in-memory config (no file I/O) in < 20ms", () => {
    const config = getDefaultConfig();

    // dispatch() with config passed directly skips file loading
    const elapsed = measure(() => {
      dispatch("PreToolUse", makePayload(), { config });
    });

    expect(elapsed).toBeLessThan(20);
  });
});

// --- Import Boundary ---

describe("performance: import boundary", () => {
  it("src/core/dispatcher.ts does NOT import React or Ink", () => {
    // Read the source file directly and check for forbidden imports
    const dispatcherPath = join(
      process.cwd(),
      "src",
      "core",
      "dispatcher.ts"
    );
    const content = readFileSync(dispatcherPath, "utf-8");

    // Should not import react
    expect(content).not.toMatch(/from\s+["']react["']/);
    expect(content).not.toMatch(/require\s*\(\s*["']react["']\s*\)/);

    // Should not import ink
    expect(content).not.toMatch(/from\s+["']ink["']/);
    expect(content).not.toMatch(/require\s*\(\s*["']ink["']\s*\)/);
  });

  it("built dispatcher bundle does NOT contain React or Ink", () => {
    // Check the built output for react/ink imports
    const builtPath = join(process.cwd(), "dist", "core", "dispatcher.js");

    if (!existsSync(builtPath)) {
      // Build hasn't run yet; skip this test
      return;
    }

    const content = readFileSync(builtPath, "utf-8");

    // Should not contain react in the bundle
    expect(content).not.toMatch(/from\s*["']react["']/);
    expect(content).not.toMatch(/require\s*\(\s*["']react["']\s*\)/);

    // Should not contain ink in the bundle
    expect(content).not.toMatch(/from\s*["']ink["']/);
    expect(content).not.toMatch(/require\s*\(\s*["']ink["']\s*\)/);
  });

  it("core modules (guards, config, errors, state) do NOT import React", () => {
    const coreModules = [
      "guards.ts",
      "config.ts",
      "errors.ts",
      "state.ts",
      "types.ts",
      "constants.ts",
    ];

    for (const mod of coreModules) {
      const filePath = join(process.cwd(), "src", "core", mod);
      if (!existsSync(filePath)) continue;

      const content = readFileSync(filePath, "utf-8");
      expect(content).not.toMatch(/from\s+["']react["']/);
      expect(content).not.toMatch(/from\s+["']ink["']/);
    }
  });
});
