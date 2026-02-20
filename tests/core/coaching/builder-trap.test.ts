/**
 * Tests for the Builder's Trap Detector.
 *
 * Verifies:
 * - Mode classification for each category (coding, tooling, practice, prep, neutral)
 * - Time accumulation with gaps
 * - Alert level thresholds (none -> yellow -> orange -> red)
 * - Reset on practice/coding activity
 * - Daily counter reset on date boundary
 * - Session break detection (> 10 min gap)
 */

import { describe, it, expect } from "vitest";
import {
  classifyMode,
  accumulateTime,
  computeAlertLevel,
} from "../../../src/core/coaching/builder-trap.js";
import type {
  CoachingConfig,
  CoachingCache,
  Mode,
} from "../../../src/core/types.js";

function makeConfig(
  overrides: Partial<CoachingConfig["builderTrap"]> = {}
): CoachingConfig["builderTrap"] {
  return {
    enabled: true,
    thresholds: { yellow: 30, orange: 60, red: 90 },
    toolingPatterns: ["npm", "pip", "brew", "apt-get"],
    practiceTools: ["vitest", "pytest", "jest"],
    ...overrides,
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

// --- classifyMode ---

describe("classifyMode", () => {
  const config = makeConfig();

  it("classifies Write tool as coding", () => {
    expect(classifyMode("Write", {}, config)).toBe("coding");
  });

  it("classifies Edit tool as coding", () => {
    expect(classifyMode("Edit", {}, config)).toBe("coding");
  });

  it("classifies NotebookEdit as coding", () => {
    expect(classifyMode("NotebookEdit", {}, config)).toBe("coding");
  });

  it("classifies tool matching toolingPatterns as tooling", () => {
    expect(classifyMode("Bash", { command: "npm install express" }, config)).toBe("tooling");
  });

  it("classifies pip in command as tooling", () => {
    expect(classifyMode("Bash", { command: "pip install requests" }, config)).toBe("tooling");
  });

  it("classifies brew in command as tooling", () => {
    expect(classifyMode("Bash", { command: "brew install node" }, config)).toBe("tooling");
  });

  it("classifies tool matching practiceTools as practice", () => {
    expect(classifyMode("Bash", { command: "vitest run" }, config)).toBe("practice");
  });

  it("classifies pytest as practice", () => {
    expect(classifyMode("Bash", { command: "pytest tests/" }, config)).toBe("practice");
  });

  it("classifies jest as practice", () => {
    expect(classifyMode("Bash", { command: "jest --watch" }, config)).toBe("practice");
  });

  it("classifies Read tool as prep", () => {
    expect(classifyMode("Read", {}, config)).toBe("prep");
  });

  it("classifies Grep tool as prep", () => {
    expect(classifyMode("Grep", {}, config)).toBe("prep");
  });

  it("classifies Glob tool as prep", () => {
    expect(classifyMode("Glob", {}, config)).toBe("prep");
  });

  it("classifies unknown tool as neutral", () => {
    expect(classifyMode("SendMessage", {}, config)).toBe("neutral");
  });

  it("classifies Bash with no matching patterns as neutral", () => {
    expect(classifyMode("Bash", { command: "echo hello" }, config)).toBe("neutral");
  });

  it("handles missing tool_input gracefully", () => {
    expect(classifyMode("Bash", {}, config)).toBe("neutral");
  });

  it("matches tooling pattern case-insensitively in command string", () => {
    expect(classifyMode("Bash", { command: "NPM install foo" }, config)).toBe("tooling");
  });

  it("prioritizes practice over tooling when both match", () => {
    // e.g., "npx vitest run" has both npx (not in tooling) and vitest (in practice)
    expect(classifyMode("Bash", { command: "npx vitest run" }, config)).toBe("practice");
  });

  it("handles custom tooling patterns", () => {
    const customConfig = makeConfig({ toolingPatterns: ["cargo", "make"] });
    expect(classifyMode("Bash", { command: "cargo build" }, customConfig)).toBe("tooling");
  });

  it("handles custom practice tools", () => {
    const customConfig = makeConfig({ practiceTools: ["mocha"] });
    expect(classifyMode("Bash", { command: "mocha tests" }, customConfig)).toBe("practice");
  });
});

// --- accumulateTime ---

describe("accumulateTime", () => {
  it("accumulates tooling minutes when in tooling mode", () => {
    const cache = makeCache({
      currentMode: "tooling",
      modeStartedAt: "2026-02-20T10:00:00Z",
      toolingMinutes: 0,
    });
    accumulateTime(cache, "tooling", "2026-02-20T10:05:00Z");
    expect(cache.toolingMinutes).toBe(5);
  });

  it("does not accumulate non-tooling modes", () => {
    const cache = makeCache({
      currentMode: "coding",
      modeStartedAt: "2026-02-20T10:00:00Z",
      toolingMinutes: 0,
    });
    accumulateTime(cache, "coding", "2026-02-20T10:05:00Z");
    expect(cache.toolingMinutes).toBe(0);
  });

  it("treats gap > 10 minutes as session break (no accumulation)", () => {
    const cache = makeCache({
      currentMode: "tooling",
      modeStartedAt: "2026-02-20T10:00:00Z",
      toolingMinutes: 5,
    });
    accumulateTime(cache, "tooling", "2026-02-20T10:15:00Z");
    // 15 min gap > 10 min threshold, should NOT accumulate
    expect(cache.toolingMinutes).toBe(5);
  });

  it("accumulates exactly at 10-minute boundary", () => {
    const cache = makeCache({
      currentMode: "tooling",
      modeStartedAt: "2026-02-20T10:00:00Z",
      toolingMinutes: 0,
    });
    accumulateTime(cache, "tooling", "2026-02-20T10:10:00Z");
    expect(cache.toolingMinutes).toBe(10);
  });

  it("resets tooling timer when practice mode detected", () => {
    const cache = makeCache({
      currentMode: "tooling",
      modeStartedAt: "2026-02-20T10:00:00Z",
      toolingMinutes: 25,
    });
    accumulateTime(cache, "practice", "2026-02-20T10:05:00Z");
    expect(cache.toolingMinutes).toBe(0);
    expect(cache.practiceCount).toBe(1);
  });

  it("resets tooling timer when coding mode detected", () => {
    const cache = makeCache({
      currentMode: "tooling",
      modeStartedAt: "2026-02-20T10:00:00Z",
      toolingMinutes: 45,
    });
    accumulateTime(cache, "coding", "2026-02-20T10:05:00Z");
    expect(cache.toolingMinutes).toBe(0);
  });

  it("resets daily counters on date boundary", () => {
    const cache = makeCache({
      currentMode: "tooling",
      modeStartedAt: "2026-02-20T23:55:00Z",
      toolingMinutes: 80,
      practiceCount: 5,
      todayDate: "2026-02-20",
    });
    accumulateTime(cache, "neutral", "2026-02-21T00:01:00Z");
    expect(cache.toolingMinutes).toBe(0);
    expect(cache.practiceCount).toBe(0);
    expect(cache.todayDate).toBe("2026-02-21");
  });

  it("updates currentMode after accumulation", () => {
    const cache = makeCache({
      currentMode: "coding",
      modeStartedAt: "2026-02-20T10:00:00Z",
    });
    accumulateTime(cache, "tooling", "2026-02-20T10:03:00Z");
    expect(cache.currentMode).toBe("tooling");
  });

  it("updates modeStartedAt after accumulation", () => {
    const cache = makeCache({
      currentMode: "coding",
      modeStartedAt: "2026-02-20T10:00:00Z",
    });
    accumulateTime(cache, "tooling", "2026-02-20T10:03:00Z");
    expect(cache.modeStartedAt).toBe("2026-02-20T10:03:00Z");
  });

  it("accumulates fractional minutes", () => {
    const cache = makeCache({
      currentMode: "tooling",
      modeStartedAt: "2026-02-20T10:00:00Z",
      toolingMinutes: 0,
    });
    // 2.5 minutes
    accumulateTime(cache, "tooling", "2026-02-20T10:02:30Z");
    expect(cache.toolingMinutes).toBe(2.5);
  });
});

// --- computeAlertLevel ---

describe("computeAlertLevel", () => {
  it("returns none when toolingMinutes below yellow threshold", () => {
    const cache = makeCache({ toolingMinutes: 0 });
    expect(computeAlertLevel(cache)).toBe("none");
  });

  it("returns none at 29 minutes (just below yellow=30)", () => {
    const cache = makeCache({ toolingMinutes: 29 });
    expect(computeAlertLevel(cache)).toBe("none");
  });

  it("returns yellow at exactly 30 minutes", () => {
    const cache = makeCache({ toolingMinutes: 30 });
    expect(computeAlertLevel(cache)).toBe("yellow");
  });

  it("returns yellow at 59 minutes (below orange=60)", () => {
    const cache = makeCache({ toolingMinutes: 59 });
    expect(computeAlertLevel(cache)).toBe("yellow");
  });

  it("returns orange at exactly 60 minutes", () => {
    const cache = makeCache({ toolingMinutes: 60 });
    expect(computeAlertLevel(cache)).toBe("orange");
  });

  it("returns orange at 89 minutes (below red=90)", () => {
    const cache = makeCache({ toolingMinutes: 89 });
    expect(computeAlertLevel(cache)).toBe("orange");
  });

  it("returns red at exactly 90 minutes", () => {
    const cache = makeCache({ toolingMinutes: 90 });
    expect(computeAlertLevel(cache)).toBe("red");
  });

  it("returns red at 120 minutes (above red=90)", () => {
    const cache = makeCache({ toolingMinutes: 120 });
    expect(computeAlertLevel(cache)).toBe("red");
  });

  it("uses custom thresholds from cache context", () => {
    // computeAlertLevel uses default thresholds; config passed separately
    const cache = makeCache({ toolingMinutes: 15 });
    expect(
      computeAlertLevel(cache, { yellow: 10, orange: 20, red: 30 })
    ).toBe("yellow");
  });

  it("handles zero thresholds gracefully", () => {
    const cache = makeCache({ toolingMinutes: 0 });
    expect(
      computeAlertLevel(cache, { yellow: 0, orange: 0, red: 0 })
    ).toBe("red");
  });
});
