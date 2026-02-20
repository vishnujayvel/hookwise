/**
 * Context Window Monitor recipe handler.
 *
 * Tracks estimated context window usage and emits warnings
 * at configurable thresholds (default 60% warning, 80% critical).
 */

import type { HookPayload } from "../../../src/core/types.js";

export interface ContextWindowMonitorConfig {
  enabled: boolean;
  warningThreshold: number;
  criticalThreshold: number;
  maxTokens: number;
}

export interface ContextState {
  estimatedTokensUsed: number;
  lastEventType: string;
  preCompactTokens: number | null;
}

export interface ThresholdResult {
  level: "ok" | "warning" | "critical";
  usagePercent: number;
  message?: string;
  statusLineUpdate?: string;
}

/** Approximate tokens per character */
const CHARS_PER_TOKEN = 4;

/**
 * Estimate token count from a tool event payload.
 *
 * @param event - Hook payload
 * @returns Estimated token count for this event
 */
export function estimateEventTokens(event: HookPayload): number {
  const inputStr = JSON.stringify(event.tool_input ?? {});
  return Math.ceil(inputStr.length / CHARS_PER_TOKEN);
}

/**
 * Check context window usage against thresholds.
 *
 * @param event - Hook payload from PostToolUse or PreCompact
 * @param config - Recipe configuration with thresholds
 * @param state - Mutable context tracking state
 * @returns ThresholdResult with level, usage percent, and optional message
 */
export function checkContextUsage(
  event: HookPayload,
  config: ContextWindowMonitorConfig,
  state: ContextState
): ThresholdResult {
  if (!config.enabled) {
    return { level: "ok", usagePercent: 0 };
  }

  const maxTokens = config.maxTokens ?? 200000;
  const warningThreshold = config.warningThreshold ?? 0.6;
  const criticalThreshold = config.criticalThreshold ?? 0.8;

  // Handle PreCompact: log the reset
  if (state.lastEventType === "PreCompact") {
    state.preCompactTokens = state.estimatedTokensUsed;
    // After compaction, estimate context is reduced to ~30%
    state.estimatedTokensUsed = Math.floor(state.estimatedTokensUsed * 0.3);
  }

  // Accumulate tokens from tool use
  const eventTokens = estimateEventTokens(event);
  state.estimatedTokensUsed += eventTokens;
  state.lastEventType = event.tool_name ?? "unknown";

  const usagePercent = state.estimatedTokensUsed / maxTokens;

  if (usagePercent >= criticalThreshold) {
    return {
      level: "critical",
      usagePercent,
      message: `Context window at ${(usagePercent * 100).toFixed(0)}% (critical threshold: ${(criticalThreshold * 100).toFixed(0)}%). Consider compacting or starting a new session.`,
      statusLineUpdate: `CTX:${(usagePercent * 100).toFixed(0)}%!`,
    };
  }

  if (usagePercent >= warningThreshold) {
    return {
      level: "warning",
      usagePercent,
      message: `Context window at ${(usagePercent * 100).toFixed(0)}% (warning threshold: ${(warningThreshold * 100).toFixed(0)}%).`,
      statusLineUpdate: `CTX:${(usagePercent * 100).toFixed(0)}%`,
    };
  }

  return {
    level: "ok",
    usagePercent,
    statusLineUpdate: `CTX:${(usagePercent * 100).toFixed(0)}%`,
  };
}
