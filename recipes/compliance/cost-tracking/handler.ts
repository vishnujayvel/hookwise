/**
 * Cost Tracking recipe handler.
 *
 * Estimates token costs from tool invocations and enforces
 * configurable daily budget limits.
 */

import type { HookPayload, HandlerResult } from "../../../src/core/types.js";

export interface CostTrackingRecipeConfig {
  enabled: boolean;
  rates: Record<string, number>;
  dailyBudget: number;
  enforcement: "warn" | "enforce";
}

export interface CostTrackingState {
  dailyCosts: Record<string, number>;
  sessionCosts: Record<string, number>;
  today: string;
  totalToday: number;
}

/** Approximate tokens per character */
const CHARS_PER_TOKEN = 4;

/**
 * Estimate token cost for a tool invocation.
 *
 * @param event - Hook payload from PostToolUse
 * @param config - Cost tracking configuration
 * @returns Estimated cost in USD
 */
export function estimateToolCost(
  event: HookPayload,
  config: CostTrackingRecipeConfig
): number {
  if (!config.enabled) return 0;

  const input = event.tool_input ?? {};
  const inputStr = JSON.stringify(input);
  const tokens = Math.ceil(inputStr.length / CHARS_PER_TOKEN);
  const rate = config.rates?.["claude-sonnet"] ?? 0.003;

  return (tokens / 1000) * rate;
}

/**
 * Accumulate cost and check budget.
 *
 * @param event - Hook payload
 * @param state - Mutable cost state
 * @param config - Recipe configuration
 * @returns HandlerResult with warning/block if over budget, or null
 */
export function trackCost(
  event: HookPayload,
  state: CostTrackingState,
  config: CostTrackingRecipeConfig
): HandlerResult | null {
  if (!config.enabled) return null;

  const cost = estimateToolCost(event, config);
  const today = new Date().toISOString().slice(0, 10);

  // Reset on new day
  if (state.today !== today) {
    state.today = today;
    state.totalToday = 0;
  }

  state.totalToday += cost;
  state.dailyCosts[today] = (state.dailyCosts[today] ?? 0) + cost;

  const sessionId = event.session_id;
  state.sessionCosts[sessionId] = (state.sessionCosts[sessionId] ?? 0) + cost;

  // Check budget
  if (state.totalToday >= config.dailyBudget) {
    return {
      decision: config.enforcement === "enforce" ? "block" : "warn",
      reason: `Daily budget of $${config.dailyBudget.toFixed(2)} exceeded: $${state.totalToday.toFixed(4)} spent today`,
      additionalContext: null,
      output: { totalToday: state.totalToday, dailyBudget: config.dailyBudget },
    };
  }

  return null;
}
