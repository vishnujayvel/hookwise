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
  AnalyticsConfig,
} from "./types.js";
import { isEventType, isHookPayload } from "./types.js";
import { safeDispatch, logError, logDebug } from "./errors.js";
import { loadConfig, getHandlersForEvent, getDefaultConfig } from "./config.js";
import { evaluate } from "./guards.js";
import { mergeKey } from "./feeds/cache-bus.js";
import { isRunning, startDaemon } from "./feeds/daemon-manager.js";
import { launchTui } from "./tui-launcher.js";
import { AnalyticsDB } from "./analytics/db.js";
import { AnalyticsEngine } from "./analytics/session.js";

// --- Analytics Engine (lazy singleton) ---

/**
 * Cached analytics engine instance, keyed by dbPath.
 * Re-created if the dbPath changes (e.g., between test runs).
 */
let cachedEngine: AnalyticsEngine | null = null;
let cachedDbPath: string | null = null;

/**
 * Get or create the analytics engine for the given config.
 * Returns null if analytics is disabled.
 */
function getAnalyticsEngine(analyticsConfig: AnalyticsConfig): AnalyticsEngine | null {
  if (!analyticsConfig.enabled) {
    if (cachedEngine) {
      try { cachedEngine.getDB().close(); } catch { /* best-effort */ }
      cachedEngine = null;
      cachedDbPath = null;
    }
    return null;
  }

  // Normalize undefined to null so the cache comparison works correctly
  const dbPath = analyticsConfig.dbPath ?? null;

  // Re-use cached engine if dbPath matches
  if (cachedEngine && cachedDbPath === dbPath) {
    return cachedEngine;
  }

  // Close old DB handle before rotating (avoid leaking SQLite connections)
  if (cachedEngine) {
    try { cachedEngine.getDB().close(); } catch { /* best-effort cleanup */ }
  }

  // Create new engine
  const db = new AnalyticsDB(dbPath ?? undefined);
  cachedEngine = new AnalyticsEngine(db);
  cachedDbPath = dbPath;
  return cachedEngine;
}

/**
 * Record analytics events based on the dispatch event type.
 *
 * Runs in Phase 3 (side effects). Wrapped in try/catch for fail-open
 * behavior: analytics errors must NEVER affect the dispatch exit code (ARCH-3).
 *
 * - SessionStart: creates a session row
 * - SessionEnd: updates the session row with end timestamp and summary
 * - PostToolUse: records a tool use event
 */
function executeAnalytics(
  engine: AnalyticsEngine,
  eventType: EventType,
  payload: HookPayload
): void {
  try {
    const sessionId = payload.session_id;
    if (!sessionId) return;

    const timestamp = new Date().toISOString();

    switch (eventType) {
      case "SessionStart":
        engine.startSession(sessionId);
        break;

      case "SessionEnd":
        engine.endSession(sessionId, {
          totalToolCalls: 0,
          fileEditsCount: 0,
          aiAuthoredLines: 0,
          humanVerifiedLines: 0,
        });
        break;

      case "PostToolUse":
        engine.recordEvent({
          sessionId,
          eventType,
          toolName: payload.tool_name ?? undefined,
          timestamp,
        });
        break;

      default:
        // Other event types: record a generic event
        engine.recordEvent({
          sessionId,
          eventType,
          timestamp,
        });
        break;
    }
  } catch (error) {
    // ARCH-3: Fail-open — analytics errors must never affect dispatch exit code
    logError(
      error instanceof Error ? error : new Error(String(error)),
      { context: "executeAnalytics", eventType }
    );
  }
}

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

    // Feed platform: write heartbeat + CWD for daemon consumption (on every dispatch)
    try {
      const cachePath = config.statusLine.cachePath;
      mergeKey(cachePath, "_dispatch_heartbeat", { value: Date.now() }, 999999);
      mergeKey(cachePath, "_cwd", { value: process.cwd() }, 999999);
    } catch {
      // Fail-open: feed cache write errors must never affect dispatch result
    }

    // Feed platform: auto-start daemon on SessionStart
    if (eventType === "SessionStart" && config.daemon?.autoStart) {
      try {
        if (!isRunning()) {
          startDaemon(options?.projectDir ?? process.cwd());
        }
      } catch {
        // Fail-open: daemon start failure must never affect dispatch
      }
    }

    // Get handlers for this event
    const handlers = getHandlersForEvent(config, eventType);

    // No handlers AND no declarative guards AND no analytics AND no TUI -> exit 0
    const hasAnalytics = config.analytics.enabled;
    const hasTuiLaunch = eventType === "SessionStart" && config.tui?.autoLaunch;
    if (handlers.length === 0 && config.guards.length === 0 && !hasAnalytics && !hasTuiLaunch) {
      return { stdout: null, stderr: null, exitCode: 0 };
    }

    // Track warn context from declarative guards (surfaced to user in output)
    let warnContext: string | null = null;

    // Phase 1a: Declarative guard rules (config.guards[])
    // Evaluated on PreToolUse events only — first match wins
    if (eventType === "PreToolUse" && config.guards.length > 0) {
      try {
        const toolName = payload.tool_name ?? "";
        // Pass full payload so field paths like "tool_input.command" resolve correctly
        const guardEval = evaluate(toolName, payload as Record<string, unknown>, config.guards);

        if (guardEval.action === "block") {
          const stdout = JSON.stringify({
            hookSpecificOutput: {
              hookEventName: "PreToolUse",
              permissionDecision: "deny",
              permissionDecisionReason: guardEval.reason ?? "Blocked by guard rule",
            },
          });
          return { stdout, stderr: null, exitCode: 0 };
        }

        if (guardEval.action === "confirm") {
          const stdout = JSON.stringify({
            hookSpecificOutput: {
              hookEventName: "PreToolUse",
              permissionDecision: "ask",
              permissionDecisionReason: guardEval.reason ?? "Requires confirmation",
            },
          });
          return { stdout, stderr: null, exitCode: 0 };
        }

        if (guardEval.action === "warn" && guardEval.reason) {
          logDebug(`Guard warning: ${guardEval.reason}`);
          warnContext = `\u26a0\ufe0f Guard warning: ${guardEval.reason}`;
        }
      } catch (error) {
        logError(
          error instanceof Error ? error : new Error(String(error)),
          { context: "declarativeGuardEvaluation" }
        );
        // Fail-open: guard error -> continue
      }
    }

    // Phase 1b: Handler-based guards (handlers with phase: "guard")
    const guardResult = executeGuardPhase(handlers, payload);
    if (guardResult) {
      return guardResult;
    }

    // Phase 2: Context Injection
    const contextStdout = executeContextPhase(handlers, payload);

    // Phase 3: Non-Blocking Side Effects (after stdout decided)
    executeSideEffectPhase(handlers, payload);

    // Phase 3b: TUI auto-launch on SessionStart (side effect)
    if (eventType === "SessionStart" && config.tui?.autoLaunch) {
      try {
        launchTui(config.tui);
      } catch {
        // Fail-open: TUI launch failure must never affect dispatch (ARCH-3)
      }
    }

    // Phase 3c: Built-in analytics (always runs if enabled, independent of custom handlers)
    const analyticsEngine = getAnalyticsEngine(config.analytics);
    if (analyticsEngine) {
      executeAnalytics(analyticsEngine, eventType, payload);
    }

    // Merge warn context with Phase 2 context output
    let finalStdout: string | null = contextStdout;

    if (warnContext) {
      if (contextStdout) {
        // Merge: parse existing context, combine additionalContext strings
        try {
          const existing = JSON.parse(contextStdout) as {
            hookSpecificOutput?: { additionalContext?: string };
          };
          const existingContext =
            existing.hookSpecificOutput?.additionalContext ?? "";
          const merged = existingContext
            ? `${warnContext}\n\n${existingContext}`
            : warnContext;
          finalStdout = JSON.stringify({
            hookSpecificOutput: { additionalContext: merged },
          });
        } catch {
          // If parsing fails, just use warn context alone
          finalStdout = JSON.stringify({
            hookSpecificOutput: { additionalContext: warnContext },
          });
        }
      } else {
        // No Phase 2 context — output warn context as stdout
        finalStdout = JSON.stringify({
          hookSpecificOutput: { additionalContext: warnContext },
        });
      }
    }

    return {
      stdout: finalStdout,
      stderr: null,
      exitCode: 0,
    };
  });
}

export { safeDispatch };
