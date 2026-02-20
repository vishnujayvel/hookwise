/**
 * Core dispatcher for hookwise v1.0
 *
 * Three-phase execution engine: Guards -> Context Injection -> Side Effects.
 * This module is the dispatch hot path. It must NOT import React/Ink.
 *
 * Fail-open philosophy: any unhandled exception -> exit 0.
 * hookwise must never break the user's Claude Code session.
 */

import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import type {
  EventType,
  HookPayload,
  DispatchResult,
  HandlerResult,
  HooksConfig,
  ResolvedHandler,
} from "./types.js";
import { isEventType, isHookPayload } from "./types.js";
import { safeDispatch, logError, logDebug } from "./errors.js";
import { loadConfig, getHandlersForEvent, getDefaultConfig } from "./config.js";

// --- Stdin Reading ---

/**
 * Read and parse the hook payload from stdin (synchronous).
 *
 * Claude Code pipes a JSON payload to stdin for each hook invocation.
 * On malformed JSON or read failure, logs the error and returns
 * a minimal empty payload to fail open.
 */
export function readStdinPayload(): HookPayload {
  try {
    const input = readFileSync(0, "utf-8");
    if (!input || input.trim() === "") {
      return { session_id: "" };
    }
    const parsed = JSON.parse(input);
    if (isHookPayload(parsed)) {
      return parsed;
    }
    // Has data but not a valid payload shape
    logDebug("stdin parsed but not a valid HookPayload, using as-is", parsed);
    return { session_id: "", ...parsed };
  } catch (error) {
    logError(
      error instanceof Error ? error : new Error(String(error)),
      { context: "readStdinPayload" }
    );
    return { session_id: "" };
  }
}

// --- Handler Execution ---

/**
 * Execute a builtin handler by calling its module function directly.
 *
 * Builtin handlers are imported at dispatch time and called synchronously.
 * The module field points to a function export path.
 */
function executeBuiltinHandler(
  handler: ResolvedHandler,
  payload: HookPayload
): HandlerResult {
  // For builtin handlers, the module field contains the module path.
  // In v1.0, builtin modules are resolved from the handlers/ directory.
  // For now, return a null result since no builtin modules exist yet.
  logDebug(`Executing builtin handler: ${handler.name}`, { module: handler.module });

  // If the handler has an action object (inline-style config on a builtin),
  // return it as the result
  if (handler.action) {
    return {
      decision: (handler.action.decision as HandlerResult["decision"]) ?? null,
      reason: (handler.action.reason as string) ?? null,
      additionalContext: (handler.action.additionalContext as string) ?? null,
      output: (handler.action.output as Record<string, unknown>) ?? null,
    };
  }

  return {
    decision: null,
    reason: null,
    additionalContext: null,
    output: null,
  };
}

/**
 * Execute a script handler via child_process.spawnSync.
 *
 * The payload is piped to stdin as JSON. stdout and stderr are captured.
 * Timeout kills the process and logs a warning.
 *
 * Exit codes:
 * - 0: success, parse stdout as HandlerResult
 * - 2: block (only if stdout is valid block JSON)
 * - Other: error, log and skip
 */
function executeScriptHandler(
  handler: ResolvedHandler,
  payload: HookPayload
): HandlerResult {
  const command = handler.command;
  if (!command) {
    logError(new Error(`Script handler "${handler.name}" has no command`));
    return { decision: null, reason: null, additionalContext: null, output: null };
  }

  logDebug(`Executing script handler: ${handler.name}`, { command, timeout: handler.timeout });

  const parts = command.split(/\s+/);
  const result = spawnSync(parts[0], parts.slice(1), {
    input: JSON.stringify(payload),
    encoding: "utf-8",
    timeout: handler.timeout,
    stdio: ["pipe", "pipe", "pipe"],
  });

  // Check for timeout (killed by signal)
  if (result.error) {
    const isTimeout =
      (result.error as NodeJS.ErrnoException).code === "ETIMEDOUT" ||
      result.signal === "SIGTERM";
    if (isTimeout) {
      logError(new Error(`Handler "${handler.name}" timed out after ${handler.timeout}ms`), {
        context: "executeScriptHandler",
        signal: result.signal,
      });
    } else {
      logError(result.error, { context: "executeScriptHandler", handler: handler.name });
    }
    return { decision: null, reason: null, additionalContext: null, output: null };
  }

  // Non-zero exit (not exit 2 with valid block JSON) -> error
  if (result.status !== 0 && result.status !== 2) {
    logError(
      new Error(`Handler "${handler.name}" exited with code ${result.status}`),
      { stderr: result.stderr }
    );
    return { decision: null, reason: null, additionalContext: null, output: null };
  }

  // Parse stdout as HandlerResult
  const stdout = (result.stdout ?? "").trim();
  if (!stdout) {
    return { decision: null, reason: null, additionalContext: null, output: null };
  }

  try {
    const parsed = JSON.parse(stdout) as Record<string, unknown>;

    // Exit code 2 is only a block if stdout has decision: "block"
    if (result.status === 2 && parsed.decision !== "block") {
      logError(
        new Error(`Handler "${handler.name}" exited 2 but stdout is not a block`),
        { stdout }
      );
      return { decision: null, reason: null, additionalContext: null, output: null };
    }

    return {
      decision: (parsed.decision as HandlerResult["decision"]) ?? null,
      reason: (parsed.reason as string) ?? null,
      additionalContext: (parsed.additionalContext as string) ?? null,
      output: (parsed.output as Record<string, unknown>) ?? null,
    };
  } catch {
    // Non-JSON stdout: if exit 2, this is an error (not a valid block)
    if (result.status === 2) {
      logError(
        new Error(`Handler "${handler.name}" exited 2 but stdout is not valid JSON`),
        { stdout }
      );
    }
    return { decision: null, reason: null, additionalContext: null, output: null };
  }
}

/**
 * Execute an inline handler by evaluating its action object.
 *
 * Inline handlers have a static action object that is returned directly.
 */
function executeInlineHandler(handler: ResolvedHandler): HandlerResult {
  logDebug(`Executing inline handler: ${handler.name}`);

  if (!handler.action) {
    return { decision: null, reason: null, additionalContext: null, output: null };
  }

  return {
    decision: (handler.action.decision as HandlerResult["decision"]) ?? null,
    reason: (handler.action.reason as string) ?? null,
    additionalContext: (handler.action.additionalContext as string) ?? null,
    output: (handler.action.output as Record<string, unknown>) ?? null,
  };
}

/**
 * Execute a single handler with error boundary.
 * Catches all errors and returns a null result on failure.
 */
export function executeHandler(
  handler: ResolvedHandler,
  payload: HookPayload
): HandlerResult {
  try {
    switch (handler.handlerType) {
      case "builtin":
        return executeBuiltinHandler(handler, payload);
      case "script":
        return executeScriptHandler(handler, payload);
      case "inline":
        return executeInlineHandler(handler);
      default:
        logError(new Error(`Unknown handler type: ${handler.handlerType}`));
        return { decision: null, reason: null, additionalContext: null, output: null };
    }
  } catch (error) {
    logError(
      error instanceof Error ? error : new Error(String(error)),
      { context: "executeHandler", handler: handler.name }
    );
    return { decision: null, reason: null, additionalContext: null, output: null };
  }
}

// --- Three-Phase Execution ---

/**
 * Phase 1: Blocking Guards
 *
 * Execute guard handlers synchronously. On first block decision,
 * short-circuit and return the block result immediately.
 *
 * Error boundary: any exception in Phase 1 -> exit 0 (fail-open).
 */
function executeGuardPhase(
  handlers: ResolvedHandler[],
  payload: HookPayload
): DispatchResult | null {
  const guards = handlers.filter((h) => h.phase === "guard");

  for (const guard of guards) {
    try {
      const result = executeHandler(guard, payload);

      if (result.decision === "block") {
        const stdout = JSON.stringify({
          decision: "block",
          reason: result.reason ?? "Blocked by guard rule",
        });
        return { stdout, stderr: null, exitCode: 0 };
      }
    } catch (error) {
      logError(
        error instanceof Error ? error : new Error(String(error)),
        { context: "executeGuardPhase", handler: guard.name }
      );
      // Phase 1 error -> exit 0 (fail-open)
      return { stdout: null, stderr: null, exitCode: 0 };
    }
  }

  return null; // No block, continue to next phase
}

/**
 * Phase 2: Context Injection
 *
 * Execute context handlers (greeting, metacognition, communication coach).
 * Collect additionalContext strings and merge into output JSON.
 *
 * Error boundary: any exception in Phase 2 -> skip (don't add context).
 */
function executeContextPhase(
  handlers: ResolvedHandler[],
  payload: HookPayload
): string | null {
  const contextHandlers = handlers.filter((h) => h.phase === "context");
  const contextParts: string[] = [];

  for (const handler of contextHandlers) {
    try {
      const result = executeHandler(handler, payload);
      if (result.additionalContext) {
        contextParts.push(result.additionalContext);
      }
    } catch (error) {
      logError(
        error instanceof Error ? error : new Error(String(error)),
        { context: "executeContextPhase", handler: handler.name }
      );
      // Phase 2 error: skip this handler, continue
    }
  }

  if (contextParts.length === 0) return null;

  const combined = contextParts.join("\n\n");
  return JSON.stringify({
    hookSpecificOutput: { additionalContext: combined },
  });
}

/**
 * Phase 3: Non-Blocking Side Effects
 *
 * Execute analytics, coaching state, sounds, transcript handlers.
 * All wrapped in try/catch: errors are logged and swallowed.
 */
function executeSideEffectPhase(
  handlers: ResolvedHandler[],
  payload: HookPayload
): void {
  const sideEffects = handlers.filter((h) => h.phase === "side_effect");

  for (const handler of sideEffects) {
    try {
      executeHandler(handler, payload);
    } catch (error) {
      logError(
        error instanceof Error ? error : new Error(String(error)),
        { context: "executeSideEffectPhase", handler: handler.name }
      );
      // Phase 3 error: log and continue
    }
  }
}

// --- Main Dispatch ---

/**
 * Main dispatch function -- entry point for all hook events.
 *
 * Reads config, resolves handlers for the event type, then executes
 * the three-phase pipeline: Guards -> Context -> Side Effects.
 *
 * Wrapped in safeDispatch for fail-open guarantee.
 */
export function dispatch(
  eventType: EventType,
  payload: HookPayload,
  options?: { config?: HooksConfig; projectDir?: string }
): DispatchResult {
  return safeDispatch(() => {
    // Validate event type
    if (!isEventType(eventType)) {
      return { stdout: null, stderr: null, exitCode: 0 };
    }

    // Load config
    let config: HooksConfig;
    if (options?.config) {
      config = options.config;
    } else {
      try {
        config = loadConfig(options?.projectDir);
      } catch (error) {
        logError(
          error instanceof Error ? error : new Error(String(error)),
          { context: "dispatch.loadConfig" }
        );
        // Malformed config -> exit 0 silently
        return { stdout: null, stderr: null, exitCode: 0 };
      }
    }

    // Get handlers for this event
    const handlers = getHandlersForEvent(config, eventType);

    // No handlers -> exit 0
    if (handlers.length === 0) {
      return { stdout: null, stderr: null, exitCode: 0 };
    }

    // Phase 1: Blocking Guards
    const guardResult = executeGuardPhase(handlers, payload);
    if (guardResult) {
      return guardResult;
    }

    // Phase 2: Context Injection
    const contextStdout = executeContextPhase(handlers, payload);

    // Phase 3: Non-Blocking Side Effects (after stdout decided)
    executeSideEffectPhase(handlers, payload);

    return {
      stdout: contextStdout,
      stderr: null,
      exitCode: 0,
    };
  });
}

export { safeDispatch };
