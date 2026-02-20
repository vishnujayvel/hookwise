/**
 * Builder's Trap Detector for hookwise coaching system.
 *
 * Classifies tool calls into behavioral modes, tracks tooling time,
 * and alerts when the user is spending too much time on tooling
 * without productive coding or practice.
 *
 * All functions are synchronous and fail-open.
 */

import type {
  CoachingConfig,
  CoachingCache,
  Mode,
  AlertLevel,
} from "../types.js";

/** Default thresholds in minutes for alert levels. */
const DEFAULT_THRESHOLDS = { yellow: 30, orange: 60, red: 90 };

/** Maximum gap in minutes before treating as a session break. */
const SESSION_BREAK_MINUTES = 10;

/** Tool names that indicate coding activity. */
const CODING_TOOLS = new Set(["Write", "Edit", "NotebookEdit"]);

/** Tool names that indicate prep/reading activity. */
const PREP_TOOLS = new Set(["Read", "Grep", "Glob"]);

/**
 * Classify a tool call into a behavioral mode.
 *
 * Priority order (first match wins):
 * 1. Coding tools (Write, Edit, NotebookEdit)
 * 2. Prep tools (Read, Grep, Glob)
 * 3. For Bash commands: practice tools > tooling patterns > neutral
 * 4. Neutral (default)
 */
export function classifyMode(
  toolName: string,
  toolInput: Record<string, unknown>,
  config: CoachingConfig["builderTrap"]
): Mode {
  // Direct tool name classification
  if (CODING_TOOLS.has(toolName)) return "coding";
  if (PREP_TOOLS.has(toolName)) return "prep";

  // For Bash/command tools, inspect the command string
  const command = typeof toolInput?.command === "string"
    ? toolInput.command.toLowerCase()
    : "";

  if (command) {
    // Check practice tools first (higher priority)
    if (config.practiceTools.some((p) => command.includes(p.toLowerCase()))) {
      return "practice";
    }

    // Check tooling patterns
    if (config.toolingPatterns.some((p) => command.includes(p.toLowerCase()))) {
      return "tooling";
    }
  }

  return "neutral";
}

/**
 * Accumulate time in the current mode and update the cache.
 *
 * Rules:
 * - If the gap since modeStartedAt > 10 minutes, treat as session break
 * - If previous mode was "tooling", accumulate the delta into toolingMinutes
 * - Reset tooling timer when practice or coding mode detected
 * - Reset daily counters on date boundary
 * - Always update currentMode and modeStartedAt
 */
export function accumulateTime(
  cache: CoachingCache,
  currentMode: Mode,
  now: string
): void {
  const nowDate = now.slice(0, 10); // YYYY-MM-DD
  const nowMs = new Date(now).getTime();
  const startMs = new Date(cache.modeStartedAt).getTime();
  const deltaMinutes = (nowMs - startMs) / 60000;
  const crossedDateBoundary = nowDate !== cache.todayDate;

  // Reset daily counters on date boundary
  if (crossedDateBoundary) {
    cache.toolingMinutes = 0;
    cache.practiceCount = 0;
    cache.todayDate = nowDate;
  }

  // Only accumulate if within session break threshold AND same day
  if (
    !crossedDateBoundary &&
    deltaMinutes >= 0 &&
    deltaMinutes <= SESSION_BREAK_MINUTES
  ) {
    // Accumulate tooling time if previous mode was tooling
    if (cache.currentMode === "tooling") {
      cache.toolingMinutes += deltaMinutes;
    }
  }

  // Reset tooling timer on practice or coding
  if (currentMode === "practice") {
    cache.toolingMinutes = 0;
    cache.practiceCount += 1;
  } else if (currentMode === "coding") {
    cache.toolingMinutes = 0;
  }

  // Update mode tracking
  cache.currentMode = currentMode;
  cache.modeStartedAt = now;
}

/**
 * Compute the current alert level based on tooling minutes.
 *
 * Uses the provided thresholds or defaults.
 */
export function computeAlertLevel(
  cache: CoachingCache,
  thresholds?: { yellow: number; orange: number; red: number }
): AlertLevel {
  const t = thresholds ?? DEFAULT_THRESHOLDS;
  const minutes = cache.toolingMinutes;

  if (minutes >= t.red) return "red";
  if (minutes >= t.orange) return "orange";
  if (minutes >= t.yellow) return "yellow";
  return "none";
}
