/**
 * Tests for the Streak & Gamification recipe handler.
 */

import { describe, it, expect } from "vitest";
import type { HookPayload } from "../../src/core/types.js";
import {
  trackActivity,
  checkMilestone,
  getStreakSummary,
  freshStreakState,
} from "../../recipes/gamification/streak-tracker/handler.js";
import type {
  StreakConfig,
  StreakState,
} from "../../recipes/gamification/streak-tracker/handler.js";

function makePayload(overrides: Partial<HookPayload> = {}): HookPayload {
  return {
    session_id: "test-session",
    ...overrides,
  };
}

const config: StreakConfig = {
  enabled: true,
  aiRatioThreshold: 0.8,
  milestones: [7, 14, 30, 60, 90, 365],
};

describe("trackActivity", () => {
  it("tracks coding streak for Write tool", () => {
    const state = freshStreakState();
    const event = makePayload({ tool_name: "Write", tool_input: {} });

    trackActivity(event, state, config);
    expect(state.codingStreak.current).toBe(1);
    expect(state.codingStreak.lastActiveDate).toBe(
      new Date().toISOString().slice(0, 10)
    );
  });

  it("tracks coding streak for Edit tool", () => {
    const state = freshStreakState();
    const event = makePayload({ tool_name: "Edit", tool_input: {} });

    trackActivity(event, state, config);
    expect(state.codingStreak.current).toBe(1);
  });

  it("tracks test streak for vitest command", () => {
    const state = freshStreakState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "npx vitest run" },
    });

    trackActivity(event, state, config);
    expect(state.testStreak.current).toBe(1);
  });

  it("tracks test streak for jest command", () => {
    const state = freshStreakState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "npx jest --watch" },
    });

    trackActivity(event, state, config);
    expect(state.testStreak.current).toBe(1);
  });

  it("does not increment same-day activity twice", () => {
    const state = freshStreakState();
    const today = new Date().toISOString().slice(0, 10);
    state.codingStreak.current = 1;
    state.codingStreak.lastActiveDate = today;

    const event = makePayload({ tool_name: "Write", tool_input: {} });
    trackActivity(event, state, config);
    expect(state.codingStreak.current).toBe(1);
  });

  it("increments streak for consecutive days", () => {
    const state = freshStreakState();
    const yesterday = new Date();
    yesterday.setDate(yesterday.getDate() - 1);

    state.codingStreak.current = 5;
    state.codingStreak.lastActiveDate = yesterday.toISOString().slice(0, 10);

    const event = makePayload({ tool_name: "Write", tool_input: {} });
    trackActivity(event, state, config);
    expect(state.codingStreak.current).toBe(6);
  });

  it("resets streak after a gap", () => {
    const state = freshStreakState();
    const twoDaysAgo = new Date();
    twoDaysAgo.setDate(twoDaysAgo.getDate() - 3);

    state.codingStreak.current = 10;
    state.codingStreak.lastActiveDate = twoDaysAgo.toISOString().slice(0, 10);

    const event = makePayload({ tool_name: "Write", tool_input: {} });
    trackActivity(event, state, config);
    expect(state.codingStreak.current).toBe(1);
  });

  it("updates longest streak", () => {
    const state = freshStreakState();
    const yesterday = new Date();
    yesterday.setDate(yesterday.getDate() - 1);

    state.codingStreak.current = 5;
    state.codingStreak.longest = 5;
    state.codingStreak.lastActiveDate = yesterday.toISOString().slice(0, 10);

    const event = makePayload({ tool_name: "Write", tool_input: {} });
    trackActivity(event, state, config);
    expect(state.codingStreak.longest).toBe(6);
  });

  it("does nothing when disabled", () => {
    const state = freshStreakState();
    const event = makePayload({ tool_name: "Write", tool_input: {} });
    trackActivity(event, state, { ...config, enabled: false });
    expect(state.codingStreak.current).toBe(0);
  });

  it("updates lastActivityDate", () => {
    const state = freshStreakState();
    const event = makePayload({ tool_name: "Write", tool_input: {} });

    trackActivity(event, state, config);
    expect(state.lastActivityDate).toBe(new Date().toISOString().slice(0, 10));
  });
});

describe("checkMilestone", () => {
  it("returns milestone when streak matches", () => {
    const state = freshStreakState();
    state.codingStreak.current = 7;
    state.codingStreak.lastActiveDate = new Date().toISOString().slice(0, 10);

    const result = checkMilestone(state, config);
    expect(result).not.toBeNull();
    expect(result!.days).toBe(7);
    expect(result!.streakType).toBe("coding");
    expect(result!.message).toContain("7-day");
  });

  it("does not repeat already-achieved milestone", () => {
    const state = freshStreakState();
    state.codingStreak.current = 7;
    state.codingStreak.milestoneHistory = [
      { days: 7, achievedAt: new Date().toISOString() },
    ];

    const result = checkMilestone(state, config);
    expect(result).toBeNull();
  });

  it("returns null when no milestone matches", () => {
    const state = freshStreakState();
    state.codingStreak.current = 5;

    const result = checkMilestone(state, config);
    expect(result).toBeNull();
  });

  it("returns null when disabled", () => {
    const state = freshStreakState();
    state.codingStreak.current = 7;

    const result = checkMilestone(state, { ...config, enabled: false });
    expect(result).toBeNull();
  });

  it("detects test streak milestone", () => {
    const state = freshStreakState();
    state.testStreak.current = 14;

    const result = checkMilestone(state, config);
    expect(result).not.toBeNull();
    expect(result!.streakType).toBe("testing");
    expect(result!.days).toBe(14);
  });

  it("adds milestone to history", () => {
    const state = freshStreakState();
    state.codingStreak.current = 30;

    checkMilestone(state, config);
    expect(state.codingStreak.milestoneHistory).toHaveLength(1);
    expect(state.codingStreak.milestoneHistory[0].days).toBe(30);
  });
});

describe("getStreakSummary", () => {
  it("returns summary of all streaks", () => {
    const state = freshStreakState();
    state.codingStreak.current = 5;
    state.codingStreak.longest = 10;
    state.testStreak.current = 3;
    state.testStreak.longest = 7;
    state.aiRatioStreak.current = 0;
    state.aiRatioStreak.longest = 0;

    const summary = getStreakSummary(state);
    expect(summary.coding).toEqual({ current: 5, longest: 10 });
    expect(summary.testing).toEqual({ current: 3, longest: 7 });
    expect(summary.aiRatio).toEqual({ current: 0, longest: 0 });
  });

  it("handles fresh state", () => {
    const summary = getStreakSummary(freshStreakState());
    expect(summary.coding.current).toBe(0);
    expect(summary.testing.current).toBe(0);
    expect(summary.aiRatio.current).toBe(0);
  });
});
