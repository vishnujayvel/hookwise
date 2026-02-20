/**
 * Tests for the Context Window Monitor recipe handler.
 */

import { describe, it, expect } from "vitest";
import type { HookPayload } from "../../src/core/types.js";
import {
  checkContextUsage,
  estimateEventTokens,
} from "../../recipes/productivity/context-window-monitor/handler.js";
import type {
  ContextWindowMonitorConfig,
  ContextState,
} from "../../recipes/productivity/context-window-monitor/handler.js";

function makePayload(overrides: Partial<HookPayload> = {}): HookPayload {
  return {
    session_id: "test-session",
    ...overrides,
  };
}

function freshState(): ContextState {
  return {
    estimatedTokensUsed: 0,
    lastEventType: "",
    preCompactTokens: null,
  };
}

const config: ContextWindowMonitorConfig = {
  enabled: true,
  warningThreshold: 0.6,
  criticalThreshold: 0.8,
  maxTokens: 200000,
};

describe("estimateEventTokens", () => {
  it("estimates tokens from tool input size", () => {
    const event = makePayload({
      tool_input: { content: "a".repeat(400) },
    });
    const tokens = estimateEventTokens(event);
    // 400 chars / 4 chars_per_token ~= 100+ tokens (JSON overhead)
    expect(tokens).toBeGreaterThan(0);
  });

  it("returns small value for empty input", () => {
    const event = makePayload({ tool_input: {} });
    const tokens = estimateEventTokens(event);
    expect(tokens).toBeGreaterThanOrEqual(1);
  });

  it("handles missing tool_input", () => {
    const event = makePayload({});
    const tokens = estimateEventTokens(event);
    expect(tokens).toBeGreaterThanOrEqual(1);
  });
});

describe("checkContextUsage", () => {
  it("returns ok when usage is below warning threshold", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "small" },
    });

    const result = checkContextUsage(event, config, state);
    expect(result.level).toBe("ok");
    expect(result.usagePercent).toBeLessThan(0.6);
  });

  it("returns warning when usage exceeds warning threshold", () => {
    const state = freshState();
    state.estimatedTokensUsed = 120001; // Just over 60%

    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "data" },
    });

    const result = checkContextUsage(event, config, state);
    expect(result.level).toBe("warning");
    expect(result.message).toContain("warning");
    expect(result.usagePercent).toBeGreaterThanOrEqual(0.6);
  });

  it("returns critical when usage exceeds critical threshold", () => {
    const state = freshState();
    state.estimatedTokensUsed = 160001; // Over 80%

    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "data" },
    });

    const result = checkContextUsage(event, config, state);
    expect(result.level).toBe("critical");
    expect(result.message).toContain("critical");
    expect(result.usagePercent).toBeGreaterThanOrEqual(0.8);
  });

  it("accumulates tokens across events", () => {
    const state = freshState();

    for (let i = 0; i < 5; i++) {
      const event = makePayload({
        tool_name: "Write",
        tool_input: { content: "a".repeat(100) },
      });
      checkContextUsage(event, config, state);
    }

    expect(state.estimatedTokensUsed).toBeGreaterThan(0);
  });

  it("handles PreCompact event by reducing estimate", () => {
    const state = freshState();
    state.estimatedTokensUsed = 100000;
    state.lastEventType = "PreCompact";

    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "data" },
    });

    const result = checkContextUsage(event, config, state);
    // After compaction, tokens reduced to ~30%
    expect(state.preCompactTokens).toBe(100000);
    expect(result.usagePercent).toBeLessThan(0.5);
  });

  it("returns ok when disabled", () => {
    const state = freshState();
    state.estimatedTokensUsed = 200000;

    const event = makePayload({ tool_name: "Write" });
    const result = checkContextUsage(event, { ...config, enabled: false }, state);
    expect(result.level).toBe("ok");
    expect(result.usagePercent).toBe(0);
  });

  it("includes status line update", () => {
    const state = freshState();
    state.estimatedTokensUsed = 50000;

    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "data" },
    });

    const result = checkContextUsage(event, config, state);
    expect(result.statusLineUpdate).toBeDefined();
    expect(result.statusLineUpdate).toContain("CTX:");
  });

  it("uses configurable thresholds", () => {
    const customConfig: ContextWindowMonitorConfig = {
      enabled: true,
      warningThreshold: 0.3,
      criticalThreshold: 0.5,
      maxTokens: 100000,
    };

    const state = freshState();
    state.estimatedTokensUsed = 35000;

    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "data" },
    });

    const result = checkContextUsage(event, customConfig, state);
    expect(result.level).toBe("warning");
  });
});
