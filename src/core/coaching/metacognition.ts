/**
 * Metacognition Reminder Engine for hookwise coaching system.
 *
 * Checks elapsed time, detects behavioral triggers, and selects
 * contextually relevant prompts while avoiding repetition.
 *
 * All functions are synchronous and fail-open.
 */

import { readFileSync, existsSync } from "node:fs";
import type {
  HookPayload,
  CoachingConfig,
  CoachingCache,
  MetacognitionResult,
  AlertLevel,
} from "../types.js";
import { classifyMode, computeAlertLevel } from "./builder-trap.js";

/** Minimum lines for rapid acceptance detection. */
const RAPID_ACCEPTANCE_MIN_LINES = 50;

/** Maximum seconds for rapid acceptance detection. */
const RAPID_ACCEPTANCE_MAX_SECONDS = 5;

/** Number of recent prompts to avoid repeating. */
const HISTORY_WINDOW = 3;

/**
 * Built-in metacognition prompts organized by category.
 */
interface PromptEntry {
  id: string;
  text: string;
  category: string;
}

const DEFAULT_PROMPTS: PromptEntry[] = [
  { id: "meta-1", text: "What assumption am I making right now that I haven't verified?", category: "reflection" },
  { id: "meta-2", text: "Am I solving the right problem, or the problem I want to solve?", category: "reflection" },
  { id: "meta-3", text: "If I had to explain this approach to a colleague, would it make sense?", category: "clarity" },
  { id: "meta-4", text: "What's the simplest thing that could possibly work here?", category: "simplicity" },
  { id: "meta-5", text: "Am I building what was asked for, or what I think should be built?", category: "alignment" },
  { id: "meta-6", text: "Have I tested my understanding before writing more code?", category: "verification" },
  { id: "meta-7", text: "Is this complexity essential, or am I over-engineering?", category: "simplicity" },
  { id: "meta-8", text: "When was the last time I stepped back and reviewed the big picture?", category: "reflection" },
  { id: "meta-9", text: "Am I learning something new, or just going through the motions?", category: "growth" },
  { id: "meta-10", text: "What would I do differently if I had to start over right now?", category: "reflection" },
  { id: "meta-11", text: "One idea. One pause. Let them ask.", category: "communication" },
  { id: "meta-12", text: "Trust the pull. Don't push.", category: "communication" },
];

/**
 * Load custom prompts from a JSON file, falling back to empty array.
 */
function loadCustomPrompts(filePath: string): PromptEntry[] {
  try {
    if (!existsSync(filePath)) return [];
    const content = readFileSync(filePath, "utf-8");
    const parsed = JSON.parse(content);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (p: unknown) =>
        typeof p === "object" &&
        p !== null &&
        "id" in p &&
        "text" in p &&
        "category" in p
    ) as PromptEntry[];
  } catch {
    return [];
  }
}

/**
 * Select a prompt that avoids the last N shown.
 * Prefers prompts matching the current mode category.
 */
function selectPrompt(
  prompts: PromptEntry[],
  recentIds: string[],
  _currentMode: string
): PromptEntry | null {
  const recentSet = new Set(recentIds.slice(-HISTORY_WINDOW));
  const eligible = prompts.filter((p) => !recentSet.has(p.id));

  if (eligible.length === 0) {
    // All prompts exhausted; pick any
    if (prompts.length === 0) return null;
    return prompts[Math.floor(Math.random() * prompts.length)];
  }

  // Randomize within eligible
  return eligible[Math.floor(Math.random() * eligible.length)];
}

/**
 * Determine the most important trigger type for this check.
 *
 * Priority: rapid_acceptance > builder_trap > mode_change > interval
 */
function determineTriggerType(
  cache: CoachingCache,
  config: CoachingConfig,
  event: HookPayload,
  intervalElapsed: boolean
): MetacognitionResult["triggerType"] | null {
  // 1. Rapid acceptance: > 50 lines in < 5s
  if (cache.lastLargeChange) {
    if (
      cache.lastLargeChange.linesChanged > RAPID_ACCEPTANCE_MIN_LINES &&
      cache.lastLargeChange.acceptedWithinSeconds < RAPID_ACCEPTANCE_MAX_SECONDS
    ) {
      return "rapid_acceptance";
    }
  }

  // 2. Builder trap escalation: alert level went up
  const currentAlertLevel = computeAlertLevel(cache, config.builderTrap.thresholds);
  if (isEscalation(cache.alertLevel, currentAlertLevel)) {
    return "builder_trap";
  }

  // 3. Mode change: tool classifies to a different mode than cached
  if (event.tool_name) {
    const eventMode = classifyMode(
      event.tool_name,
      (event.tool_input ?? {}) as Record<string, unknown>,
      config.builderTrap
    );
    if (eventMode !== cache.currentMode) {
      return "mode_change";
    }
  }

  // 4. Interval
  if (intervalElapsed) {
    return "interval";
  }

  return null;
}

/** Alert level ordering for escalation detection. */
const ALERT_ORDER: Record<AlertLevel, number> = {
  none: 0,
  yellow: 1,
  orange: 2,
  red: 3,
};

function isEscalation(previous: AlertLevel, current: AlertLevel): boolean {
  return ALERT_ORDER[current] > ALERT_ORDER[previous];
}

/**
 * Check whether a metacognition prompt should be emitted.
 *
 * @param event - The current hook payload
 * @param config - Full coaching configuration
 * @param cache - Mutable coaching cache (updates cache.alertLevel, lastPromptAt, and promptHistory)
 * @param now - Current timestamp ISO string (for testability)
 * @returns MetacognitionResult with shouldEmit and optional prompt details
 */
export function checkAndEmit(
  event: HookPayload,
  config: CoachingConfig,
  cache: CoachingCache,
  now: string
): MetacognitionResult {
  // Disabled check
  if (!config.metacognition.enabled) {
    return { shouldEmit: false };
  }

  const nowMs = new Date(now).getTime();
  const lastPromptMs = new Date(cache.lastPromptAt).getTime();
  const elapsedSeconds = (nowMs - lastPromptMs) / 1000;
  const intervalElapsed = elapsedSeconds >= config.metacognition.intervalSeconds;

  // Determine trigger type
  const triggerType = determineTriggerType(cache, config, event, intervalElapsed);

  // Update alert level to prevent repeated escalation triggers
  const currentAlertLevel = computeAlertLevel(cache, config.builderTrap.thresholds);
  cache.alertLevel = currentAlertLevel;

  if (!triggerType) {
    return { shouldEmit: false };
  }

  // Load prompts
  let prompts = [...DEFAULT_PROMPTS];
  if (config.metacognition.promptsFile) {
    const custom = loadCustomPrompts(config.metacognition.promptsFile);
    prompts = [...prompts, ...custom];
  }

  // Select a non-repeating prompt
  const selected = selectPrompt(prompts, cache.promptHistory, cache.currentMode);
  if (!selected) {
    return { shouldEmit: false };
  }

  // Update cache tracking for prompt emission
  cache.lastPromptAt = now;
  cache.promptHistory = [...cache.promptHistory.slice(-(HISTORY_WINDOW - 1)), selected.id];

  return {
    shouldEmit: true,
    promptText: selected.text,
    promptId: selected.id,
    category: selected.category,
    triggerType,
  };
}
