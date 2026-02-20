/**
 * AI Dependency Tracker recipe handler.
 *
 * Tracks the ratio of AI-authored vs human-authored code changes
 * to help developers maintain awareness of their AI dependency level.
 */

import type { HookPayload, HandlerResult } from "../../../src/core/types.js";

export interface AIDependencyConfig {
  enabled: boolean;
  aiRatioThreshold: number;
  warnOnHighRatio: boolean;
  trackTools: string[];
}

export interface AuthorshipState {
  sessionId: string;
  totalChanges: number;
  aiChanges: number;
  humanChanges: number;
  fileTracker: Record<string, { ai: number; human: number }>;
}

/**
 * Track a tool use event for authorship analysis.
 *
 * @param event - Hook payload from PostToolUse
 * @param state - Mutable authorship tracking state
 * @param config - Recipe configuration
 */
export function trackAuthorship(
  event: HookPayload,
  state: AuthorshipState,
  config: AIDependencyConfig
): void {
  if (!config.enabled) return;

  const toolName = event.tool_name ?? "";
  const trackTools = config.trackTools ?? ["Write", "Edit", "NotebookEdit"];

  if (!trackTools.includes(toolName)) return;

  // Estimate change size from tool input
  const content = (event.tool_input?.content as string) ??
    (event.tool_input?.new_string as string) ?? "";
  const lines = content.split("\n").length;

  state.totalChanges += lines;
  state.aiChanges += lines; // All tool-invoked changes are AI-authored

  const filePath = (event.tool_input?.file_path as string) ?? "unknown";
  if (!state.fileTracker[filePath]) {
    state.fileTracker[filePath] = { ai: 0, human: 0 };
  }
  state.fileTracker[filePath].ai += lines;
}

/**
 * Get the current AI authorship ratio.
 *
 * @param state - Authorship tracking state
 * @returns Ratio between 0 and 1 (AI changes / total changes)
 */
export function getAIRatio(state: AuthorshipState): number {
  if (state.totalChanges === 0) return 0;
  return state.aiChanges / state.totalChanges;
}

/**
 * Check if the AI ratio exceeds the threshold and generate a warning.
 *
 * @param state - Authorship tracking state
 * @param config - Recipe configuration with threshold
 * @returns HandlerResult with warning if threshold exceeded, or null
 */
export function checkRatio(
  state: AuthorshipState,
  config: AIDependencyConfig
): HandlerResult | null {
  if (!config.enabled || !config.warnOnHighRatio) return null;

  const ratio = getAIRatio(state);
  const threshold = config.aiRatioThreshold ?? 0.8;

  if (ratio > threshold && state.totalChanges > 10) {
    return {
      decision: "warn",
      reason: `AI authorship ratio is ${(ratio * 100).toFixed(0)}% (threshold: ${(threshold * 100).toFixed(0)}%). Consider reviewing and modifying AI-generated code.`,
      additionalContext: null,
      output: { ratio, threshold, totalChanges: state.totalChanges },
    };
  }

  return null;
}
