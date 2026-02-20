/**
 * Streak & Gamification recipe handler.
 *
 * Tracks daily activity streaks for coding sessions, test runs,
 * and AI ratio below threshold. Celebrates milestones.
 */

import type { HookPayload, HandlerResult } from "../../../src/core/types.js";

export interface StreakConfig {
  enabled: boolean;
  aiRatioThreshold: number;
  milestones: number[];
}

export interface StreakData {
  current: number;
  longest: number;
  lastActiveDate: string;
  milestoneHistory: MilestoneEntry[];
}

export interface MilestoneEntry {
  days: number;
  achievedAt: string;
}

export interface StreakState {
  codingStreak: StreakData;
  testStreak: StreakData;
  aiRatioStreak: StreakData;
  lastActivityDate: string;
}

export interface MilestoneResult {
  streakType: string;
  days: number;
  message: string;
}

export interface StreakSummary {
  coding: { current: number; longest: number };
  testing: { current: number; longest: number };
  aiRatio: { current: number; longest: number };
}

/**
 * Get today's date as YYYY-MM-DD string.
 */
function getToday(): string {
  return new Date().toISOString().slice(0, 10);
}

/**
 * Check if a date string is yesterday relative to today.
 */
function isYesterday(dateStr: string): boolean {
  const yesterday = new Date();
  yesterday.setDate(yesterday.getDate() - 1);
  return dateStr === yesterday.toISOString().slice(0, 10);
}

/**
 * Update a streak: increment if consecutive day, reset if gap.
 */
function updateStreak(streak: StreakData, today: string): void {
  if (streak.lastActiveDate === today) {
    // Already tracked today
    return;
  }

  if (isYesterday(streak.lastActiveDate)) {
    // Consecutive day: increment
    streak.current += 1;
  } else if (streak.lastActiveDate !== today) {
    // Gap: reset
    streak.current = 1;
  }

  if (streak.current > streak.longest) {
    streak.longest = streak.current;
  }

  streak.lastActiveDate = today;
}

/**
 * Create a fresh StreakData object.
 */
function freshStreak(): StreakData {
  return {
    current: 0,
    longest: 0,
    lastActiveDate: "",
    milestoneHistory: [],
  };
}

/**
 * Create a fresh StreakState object.
 */
export function freshStreakState(): StreakState {
  return {
    codingStreak: freshStreak(),
    testStreak: freshStreak(),
    aiRatioStreak: freshStreak(),
    lastActivityDate: "",
  };
}

/**
 * Track activity from a tool use event.
 *
 * @param event - Hook payload
 * @param state - Mutable streak state
 * @param config - Recipe configuration
 */
export function trackActivity(
  event: HookPayload,
  state: StreakState,
  config: StreakConfig
): void {
  if (!config.enabled) return;

  const today = getToday();
  const toolName = event.tool_name ?? "";

  // Track coding streak
  if (toolName === "Write" || toolName === "Edit" || toolName === "NotebookEdit") {
    if (!state.codingStreak) state.codingStreak = freshStreak();
    updateStreak(state.codingStreak, today);
  }

  // Track test streak
  const command = (event.tool_input?.command as string) ?? "";
  const testPatterns = ["vitest", "jest", "pytest", "npm test", "bun test"];
  if (toolName === "Bash" && testPatterns.some((p) => command.toLowerCase().includes(p))) {
    if (!state.testStreak) state.testStreak = freshStreak();
    updateStreak(state.testStreak, today);
  }

  state.lastActivityDate = today;
}

/**
 * Check if any streak has reached a milestone.
 *
 * @param state - Streak state
 * @param config - Recipe configuration with milestones
 * @returns MilestoneResult if a new milestone was reached, or null
 */
export function checkMilestone(
  state: StreakState,
  config: StreakConfig
): MilestoneResult | null {
  if (!config.enabled) return null;

  const milestones = config.milestones ?? [7, 14, 30, 60, 90, 365];
  const streaks: Array<{ type: string; data: StreakData }> = [
    { type: "coding", data: state.codingStreak ?? freshStreak() },
    { type: "testing", data: state.testStreak ?? freshStreak() },
    { type: "aiRatio", data: state.aiRatioStreak ?? freshStreak() },
  ];

  for (const { type, data } of streaks) {
    for (const milestone of milestones) {
      if (data.current === milestone) {
        // Check if this milestone was already achieved
        const alreadyAchieved = data.milestoneHistory.some(
          (m) => m.days === milestone
        );
        if (!alreadyAchieved) {
          const entry: MilestoneEntry = {
            days: milestone,
            achievedAt: new Date().toISOString(),
          };
          data.milestoneHistory.push(entry);

          return {
            streakType: type,
            days: milestone,
            message: `${milestone}-day ${type} streak! Keep it up!`,
          };
        }
      }
    }
  }

  return null;
}

/**
 * Get a summary of all streaks.
 *
 * @param state - Streak state
 * @returns Summary with current and longest for each streak type
 */
export function getStreakSummary(state: StreakState): StreakSummary {
  const coding = state.codingStreak ?? freshStreak();
  const testing = state.testStreak ?? freshStreak();
  const aiRatio = state.aiRatioStreak ?? freshStreak();

  return {
    coding: { current: coding.current, longest: coding.longest },
    testing: { current: testing.current, longest: testing.longest },
    aiRatio: { current: aiRatio.current, longest: aiRatio.longest },
  };
}
