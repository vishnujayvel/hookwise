/**
 * Metacognition Prompts recipe handler.
 *
 * Emits time-gated cognitive nudges to encourage reflective thinking
 * during coding sessions. Integrates with the coaching.metacognition subsystem.
 */

import type { HookPayload, HandlerResult } from "../../../src/core/types.js";

export interface MetacognitionPromptsConfig {
  enabled: boolean;
  intervalSeconds: number;
  prompts: string[];
}

export interface MetacognitionState {
  lastPromptAt: string;
  promptIndex: number;
  promptHistory: string[];
}

/**
 * Check if enough time has elapsed to emit a new prompt.
 *
 * @param event - Hook payload
 * @param state - Mutable metacognition state
 * @param config - Recipe configuration with interval and prompts
 * @returns HandlerResult with the prompt, or null if not time yet
 */
export function checkAndEmitPrompt(
  event: HookPayload,
  state: MetacognitionState,
  config: MetacognitionPromptsConfig
): HandlerResult | null {
  if (!config.enabled) return null;

  const now = Date.now();
  const lastAt = state.lastPromptAt ? new Date(state.lastPromptAt).getTime() : 0;
  const elapsed = (now - lastAt) / 1000;

  if (elapsed < (config.intervalSeconds ?? 300)) {
    return null;
  }

  const prompts = config.prompts ?? [];
  if (prompts.length === 0) return null;

  // Cycle through prompts
  const index = state.promptIndex % prompts.length;
  const prompt = prompts[index];

  // Update state
  state.lastPromptAt = new Date(now).toISOString();
  state.promptIndex = index + 1;
  state.promptHistory.push(prompt);

  return {
    decision: null,
    reason: null,
    additionalContext: `[Metacognition] ${prompt}`,
    output: { prompt, index, elapsed },
  };
}
