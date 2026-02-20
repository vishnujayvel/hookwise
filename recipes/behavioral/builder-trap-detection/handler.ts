/**
 * Builder's Trap Detection recipe handler.
 *
 * Classifies tool usage into modes (coding, tooling, practice, etc.)
 * and alerts when too much time is spent on tooling/infrastructure.
 */

import type { HookPayload, HandlerResult } from "../../../src/core/types.js";

export interface BuilderTrapConfig {
  enabled: boolean;
  thresholds: { yellow: number; orange: number; red: number };
  toolingPatterns: string[];
  practiceTools: string[];
}

export type Mode = "coding" | "tooling" | "practice" | "neutral";
export type AlertLevel = "none" | "yellow" | "orange" | "red";

export interface BuilderTrapState {
  currentMode: Mode;
  modeStartedAt: string;
  toolingMinutes: number;
  alertLevel: AlertLevel;
  todayDate: string;
}

/**
 * Classify a tool use event into a mode.
 *
 * @param event - Hook payload with tool_name and tool_input
 * @param config - Recipe configuration with tooling/practice patterns
 * @returns Classified mode
 */
export function classifyMode(
  event: HookPayload,
  config: BuilderTrapConfig
): Mode {
  const toolName = event.tool_name ?? "";
  const command = (event.tool_input?.command as string) ?? "";
  const combined = `${toolName} ${command}`.toLowerCase();

  const practiceTools = config.practiceTools ?? [];
  for (const pattern of practiceTools) {
    if (combined.includes(pattern.toLowerCase())) return "practice";
  }

  const toolingPatterns = config.toolingPatterns ?? [];
  for (const pattern of toolingPatterns) {
    if (combined.includes(pattern.toLowerCase())) return "tooling";
  }

  if (toolName === "Write" || toolName === "Edit" || toolName === "NotebookEdit") {
    return "coding";
  }

  return "neutral";
}

/**
 * Compute the alert level based on accumulated tooling minutes.
 *
 * @param toolingMinutes - Total tooling minutes today
 * @param thresholds - Alert thresholds in minutes
 * @returns Current alert level
 */
export function computeAlertLevel(
  toolingMinutes: number,
  thresholds: { yellow: number; orange: number; red: number }
): AlertLevel {
  if (toolingMinutes >= thresholds.red) return "red";
  if (toolingMinutes >= thresholds.orange) return "orange";
  if (toolingMinutes >= thresholds.yellow) return "yellow";
  return "none";
}

/**
 * Check for builder's trap and generate appropriate alert.
 *
 * @param event - Hook payload
 * @param state - Mutable builder trap state
 * @param config - Recipe configuration
 * @returns HandlerResult with alert, or null if no alert needed
 */
export function checkBuilderTrap(
  event: HookPayload,
  state: BuilderTrapState,
  config: BuilderTrapConfig
): HandlerResult | null {
  if (!config.enabled) return null;

  const mode = classifyMode(event, config);

  // Accumulate tooling time
  if (mode === "tooling") {
    const elapsed = state.modeStartedAt
      ? (Date.now() - new Date(state.modeStartedAt).getTime()) / 60000
      : 1;
    state.toolingMinutes += Math.min(elapsed, 5); // Cap at 5 min per event
  }

  state.currentMode = mode;
  state.modeStartedAt = new Date().toISOString();

  const newLevel = computeAlertLevel(state.toolingMinutes, config.thresholds);
  const previousLevel = state.alertLevel;
  state.alertLevel = newLevel;

  // Only alert on escalation
  if (newLevel !== "none" && newLevel !== previousLevel) {
    const messages: Record<string, string> = {
      yellow: "You've been in tooling mode for a while. Is this supporting your core goal?",
      orange: "Significant time spent on tooling. Consider shifting to coding or testing.",
      red: "Builder's trap alert! Most of today has been tooling. What is your core deliverable?",
    };

    return {
      decision: "warn",
      reason: `[Builder's Trap - ${newLevel.toUpperCase()}] ${messages[newLevel]}`,
      additionalContext: null,
      output: { mode, toolingMinutes: state.toolingMinutes, alertLevel: newLevel },
    };
  }

  return null;
}
