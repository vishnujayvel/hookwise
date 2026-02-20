/**
 * Tests for the Metacognition Reminder Engine.
 *
 * Verifies:
 * - Interval gating (emits after interval, not before)
 * - Prompt non-repetition (avoids last 3)
 * - Rapid acceptance detection
 * - Custom prompts file loading
 * - Trigger type classification
 * - Builder trap escalation triggers emission
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { checkAndEmit } from "../../../src/core/coaching/metacognition.js";
import type {
  HookPayload,
  CoachingConfig,
  CoachingCache,
} from "../../../src/core/types.js";

function makeConfig(
  overrides: Partial<CoachingConfig> = {}
): CoachingConfig {
  return {
    metacognition: {
      enabled: true,
      intervalSeconds: 300,
      ...overrides.metacognition,
    },
    builderTrap: {
      enabled: true,
      thresholds: { yellow: 30, orange: 60, red: 90 },
      toolingPatterns: ["npm"],
      practiceTools: ["vitest"],
      ...overrides.builderTrap,
    },
    communication: {
      enabled: false,
      frequency: 3,
      minLength: 10,
      rules: [],
      tone: "gentle",
      ...overrides.communication,
    },
  };
}

function makeCache(overrides: Partial<CoachingCache> = {}): CoachingCache {
  return {
    lastPromptAt: "2026-02-20T10:00:00Z",
    promptHistory: [],
    currentMode: "neutral",
    modeStartedAt: "2026-02-20T10:00:00Z",
    toolingMinutes: 0,
    alertLevel: "none",
    todayDate: "2026-02-20",
    practiceCount: 0,
    lastLargeChange: null,
    ...overrides,
  };
}

function makePayload(overrides: Partial<HookPayload> = {}): HookPayload {
  return {
    session_id: "test-session-123",
    ...overrides,
  };
}

// --- Interval Gating ---

describe("checkAndEmit - interval gating", () => {
  it("does not emit when interval has not elapsed", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 300 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T10:00:00Z",
    });
    const event = makePayload();
    // "now" is 2 minutes after last prompt (< 300s interval)
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:02:00Z");
    expect(result.shouldEmit).toBe(false);
  });

  it("emits when interval has elapsed", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 300 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T10:00:00Z",
    });
    const event = makePayload();
    // "now" is 6 minutes after last prompt (360s > 300s interval)
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
    expect(result.shouldEmit).toBe(true);
    expect(result.triggerType).toBe("interval");
  });

  it("emits at exactly the interval boundary", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 300 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T10:00:00Z",
    });
    const event = makePayload();
    // Exactly 300 seconds
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:05:00Z");
    expect(result.shouldEmit).toBe(true);
  });

  it("does not emit when metacognition is disabled", () => {
    const config = makeConfig({
      metacognition: { enabled: false, intervalSeconds: 300 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T10:00:00Z",
    });
    const event = makePayload();
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
    expect(result.shouldEmit).toBe(false);
  });
});

// --- Prompt Non-Repetition ---

describe("checkAndEmit - prompt non-repetition", () => {
  it("avoids prompts in the last 3 shown", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 0 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T09:00:00Z",
      promptHistory: ["prompt-1", "prompt-2", "prompt-3"],
    });
    const event = makePayload();
    // Run multiple times to statistically verify no recent prompts are repeated
    const results = new Set<string>();
    for (let i = 0; i < 50; i++) {
      const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
      if (result.promptId) results.add(result.promptId);
    }
    expect(results.has("prompt-1")).toBe(false);
    expect(results.has("prompt-2")).toBe(false);
    expect(results.has("prompt-3")).toBe(false);
  });

  it("returns a promptText when emitting", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 0 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T09:00:00Z",
    });
    const event = makePayload();
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
    expect(result.shouldEmit).toBe(true);
    expect(result.promptText).toBeDefined();
    expect(typeof result.promptText).toBe("string");
    expect(result.promptText!.length).toBeGreaterThan(0);
  });

  it("returns a promptId when emitting", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 0 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T09:00:00Z",
    });
    const event = makePayload();
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
    expect(result.promptId).toBeDefined();
    expect(typeof result.promptId).toBe("string");
  });

  it("returns a category when emitting", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 0 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T09:00:00Z",
    });
    const event = makePayload();
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
    expect(result.category).toBeDefined();
    expect(typeof result.category).toBe("string");
  });
});

// --- Rapid Acceptance Detection ---

describe("checkAndEmit - rapid acceptance", () => {
  it("triggers on large change accepted within 5 seconds", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 0 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T09:00:00Z",
      lastLargeChange: {
        timestamp: "2026-02-20T10:05:55Z",
        toolName: "Write",
        linesChanged: 60,
        acceptedWithinSeconds: 3,
      },
    });
    const event = makePayload();
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
    expect(result.shouldEmit).toBe(true);
    expect(result.triggerType).toBe("rapid_acceptance");
  });

  it("does not trigger rapid acceptance on small change", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 0 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T09:00:00Z",
      lastLargeChange: {
        timestamp: "2026-02-20T10:05:55Z",
        toolName: "Write",
        linesChanged: 10,
        acceptedWithinSeconds: 3,
      },
    });
    const event = makePayload();
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
    // Should still emit (interval), but NOT as rapid_acceptance
    expect(result.shouldEmit).toBe(true);
    expect(result.triggerType).not.toBe("rapid_acceptance");
  });

  it("does not trigger rapid acceptance when accepted slowly", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 0 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T09:00:00Z",
      lastLargeChange: {
        timestamp: "2026-02-20T10:05:55Z",
        toolName: "Write",
        linesChanged: 100,
        acceptedWithinSeconds: 30,
      },
    });
    const event = makePayload();
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
    expect(result.shouldEmit).toBe(true);
    expect(result.triggerType).not.toBe("rapid_acceptance");
  });
});

// --- Custom Prompts File Loading ---

describe("checkAndEmit - custom prompts", () => {
  it("loads custom prompts from config path", () => {
    const config = makeConfig({
      metacognition: {
        enabled: true,
        intervalSeconds: 0,
        promptsFile: "/tmp/hookwise-test-custom-prompts.json",
      },
    });
    // Write a custom prompts file
    const fs = require("node:fs");
    const customPrompts = [
      { id: "custom-1", text: "Custom prompt one", category: "custom" },
      { id: "custom-2", text: "Custom prompt two", category: "custom" },
    ];
    fs.writeFileSync(
      "/tmp/hookwise-test-custom-prompts.json",
      JSON.stringify(customPrompts)
    );

    const cache = makeCache({
      lastPromptAt: "2026-02-20T09:00:00Z",
    });
    const event = makePayload();

    // Run multiple times to see if custom prompts appear
    const foundIds = new Set<string>();
    for (let i = 0; i < 50; i++) {
      const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
      if (result.promptId) foundIds.add(result.promptId);
    }
    expect(foundIds.has("custom-1") || foundIds.has("custom-2")).toBe(true);

    // Cleanup
    fs.unlinkSync("/tmp/hookwise-test-custom-prompts.json");
  });

  it("falls back to defaults when custom prompts file is missing", () => {
    const config = makeConfig({
      metacognition: {
        enabled: true,
        intervalSeconds: 0,
        promptsFile: "/tmp/nonexistent-file.json",
      },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T09:00:00Z",
    });
    const event = makePayload();
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
    expect(result.shouldEmit).toBe(true);
    expect(result.promptText).toBeDefined();
  });
});

// --- Trigger Type Classification ---

describe("checkAndEmit - trigger types", () => {
  it("reports interval trigger when time-based", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 300 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T10:00:00Z",
    });
    const event = makePayload();
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
    expect(result.triggerType).toBe("interval");
  });

  it("reports mode_change when mode transitioned", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 0 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T09:00:00Z",
      currentMode: "coding",
    });
    const event = makePayload({ tool_name: "Bash", tool_input: { command: "npm install" } });
    // Event classifies as tooling, cache says coding -> mode change
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
    expect(result.shouldEmit).toBe(true);
    // mode_change has higher priority than interval
    expect(result.triggerType).toBe("mode_change");
  });

  it("returns mode_change when event mode differs from cached mode", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 600 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T10:00:00Z",
      currentMode: "tooling",
    });
    // Write tool classifies as "coding", cache says "tooling" -> mode_change
    const event = makePayload({ tool_name: "Write", tool_input: {} });
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:02:00Z");
    expect(result.shouldEmit).toBe(true);
    expect(result.triggerType).toBe("mode_change");
  });

  it("reports builder_trap when alert level escalated", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 0 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T09:00:00Z",
      alertLevel: "yellow",
      toolingMinutes: 65, // Above orange=60 threshold; escalation from yellow
    });
    const event = makePayload();
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
    expect(result.shouldEmit).toBe(true);
    expect(result.triggerType).toBe("builder_trap");
  });
});

// --- Builder Trap Escalation ---

describe("checkAndEmit - builder trap escalation", () => {
  it("triggers emission on alert level escalation from none to yellow", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 0 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T09:00:00Z",
      alertLevel: "none",
      toolingMinutes: 35, // Above yellow=30
    });
    const event = makePayload();
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:06:00Z");
    expect(result.shouldEmit).toBe(true);
    expect(result.triggerType).toBe("builder_trap");
  });

  it("does not trigger builder_trap when alert level stable", () => {
    const config = makeConfig({
      metacognition: { enabled: true, intervalSeconds: 600 },
    });
    const cache = makeCache({
      lastPromptAt: "2026-02-20T10:00:00Z",
      alertLevel: "yellow",
      toolingMinutes: 35, // Still yellow, no escalation
    });
    const event = makePayload();
    const result = checkAndEmit(event, config, cache, "2026-02-20T10:02:00Z");
    // No interval elapsed (2min < 600s), no escalation (yellow -> yellow)
    expect(result.shouldEmit).toBe(false);
  });
});
