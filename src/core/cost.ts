/**
 * Cost tracking and budget enforcement for hookwise v1.0
 *
 * Estimates token costs from tool invocation sizes, tracks daily/session
 * cost accumulation, and enforces configurable budget limits with
 * warn or enforce modes. All operations are synchronous and fail-open.
 */

import { join } from "node:path";
import { safeReadJSON, atomicWriteJSON } from "./state.js";
import type {
  CostEstimate,
  BudgetStatus,
  CostState,
  CostTrackingConfig,
} from "./types.js";

/** Default model for cost estimation */
const DEFAULT_MODEL = "claude-sonnet";

/** Approximate tokens per character (rough heuristic) */
const CHARS_PER_TOKEN = 4;

/** Default cost per 1K tokens in USD */
const DEFAULT_RATE = 0.003;

/** Cost state filename */
const COST_STATE_FILE = "cost-state.json";

/**
 * Create a fresh/empty cost state for today.
 */
function freshCostState(): CostState {
  return {
    dailyCosts: {},
    sessionCosts: {},
    today: new Date().toISOString().slice(0, 10),
    totalToday: 0,
  };
}

/**
 * Estimate the token cost for a tool invocation.
 *
 * Uses JSON.stringify length of input + output size to approximate
 * token count, then applies the model-specific rate.
 *
 * @param toolName - Name of the tool invoked
 * @param toolInput - Tool input parameters
 * @param toolOutputSize - Size of the tool output in characters
 * @param config - Optional cost tracking config for rates
 * @returns Cost estimate with tokens, USD cost, and model
 */
export function estimateCost(
  toolName: string,
  toolInput: Record<string, unknown>,
  toolOutputSize: number,
  config?: CostTrackingConfig
): CostEstimate {
  try {
    const inputStr = JSON.stringify(toolInput);
    const totalChars = inputStr.length + toolOutputSize;
    const estimatedTokens = Math.ceil(totalChars / CHARS_PER_TOKEN);

    // Look up rate: try the tool name as model key, then default
    const rates = config?.rates ?? {};
    const rate = rates[toolName] ?? rates[DEFAULT_MODEL] ?? DEFAULT_RATE;
    const estimatedCostUsd = (estimatedTokens / 1000) * rate;

    return {
      estimatedTokens,
      estimatedCostUsd: Math.round(estimatedCostUsd * 1_000_000) / 1_000_000,
      model: DEFAULT_MODEL,
    };
  } catch {
    return {
      estimatedTokens: 0,
      estimatedCostUsd: 0,
      model: DEFAULT_MODEL,
    };
  }
}

/**
 * Check whether the current daily spending is within budget.
 *
 * @param state - Current cost state
 * @param config - Cost tracking configuration with dailyBudget and enforcement
 * @returns Budget status: ok if under budget, not-ok with enforcement mode
 */
export function checkBudget(
  state: CostState,
  config: CostTrackingConfig
): BudgetStatus {
  try {
    if (!config.enabled) return { ok: true };

    if (state.totalToday >= config.dailyBudget) {
      return {
        ok: false,
        message: `Daily budget of $${config.dailyBudget.toFixed(2)} exceeded: $${state.totalToday.toFixed(2)} spent today`,
        enforcement: config.enforcement,
      };
    }

    return { ok: true };
  } catch {
    return { ok: true };
  }
}

/**
 * Accumulate a cost to session and daily totals.
 *
 * Handles date boundary resets: if the current date differs from
 * state.today, resets daily totals before accumulating.
 *
 * @param state - Mutable cost state (modified in place)
 * @param sessionId - Session to accumulate cost for
 * @param cost - Cost in USD to add
 * @returns Updated totalToday value
 */
export function accumulateCost(
  state: CostState,
  sessionId: string,
  cost: number
): number {
  try {
    const today = new Date().toISOString().slice(0, 10);

    // Handle date boundary reset
    if (state.today !== today) {
      state.today = today;
      state.totalToday = 0;
    }

    // Accumulate daily cost
    state.dailyCosts[today] = (state.dailyCosts[today] ?? 0) + cost;

    // Accumulate session cost
    state.sessionCosts[sessionId] =
      (state.sessionCosts[sessionId] ?? 0) + cost;

    // Update totalToday from dailyCosts for the current day
    state.totalToday = state.dailyCosts[today];

    return state.totalToday;
  } catch {
    return state.totalToday;
  }
}

/**
 * Load cost state from the state directory.
 *
 * @param stateDir - Base state directory (e.g., ~/.hookwise)
 * @returns Loaded cost state, or fresh state if file missing/corrupt
 */
export function loadCostState(stateDir: string): CostState {
  const filePath = join(stateDir, "state", COST_STATE_FILE);
  return safeReadJSON<CostState>(filePath, freshCostState());
}

/**
 * Save cost state to the state directory.
 *
 * @param stateDir - Base state directory (e.g., ~/.hookwise)
 * @param state - Cost state to persist
 */
export function saveCostState(stateDir: string, state: CostState): void {
  try {
    const filePath = join(stateDir, "state", COST_STATE_FILE);
    atomicWriteJSON(filePath, state);
  } catch {
    // Fail-open
  }
}
