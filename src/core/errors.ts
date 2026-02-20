/**
 * Error handling and logging infrastructure for hookwise v1.0
 *
 * Follows the fail-open philosophy: hookwise must never break the
 * user's Claude Code session. All dispatch errors exit 0.
 */

import { existsSync, mkdirSync, statSync, renameSync, appendFileSync, unlinkSync } from "node:fs";
import { join } from "node:path";
import { getStateDir } from "./state.js";
import {
  MAX_LOG_SIZE_BYTES,
  MAX_LOG_ROTATIONS,
  DEFAULT_LOG_PATH,
} from "./constants.js";
import type { DispatchResult } from "./types.js";

// --- Custom Error Classes ---

/**
 * Base error class for all hookwise errors.
 */
export class HookwiseError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "HookwiseError";
  }
}

/**
 * Configuration-related errors: malformed YAML, missing files, invalid schema.
 */
export class ConfigError extends HookwiseError {
  constructor(message: string) {
    super(message);
    this.name = "ConfigError";
  }
}

/**
 * Handler timeout errors: script handler exceeds timeout.
 */
export class HandlerTimeoutError extends HookwiseError {
  public readonly timeoutMs: number;
  public readonly handlerName: string;

  constructor(handlerName: string, timeoutMs: number) {
    super(
      `Handler "${handlerName}" timed out after ${timeoutMs}ms`
    );
    this.name = "HandlerTimeoutError";
    this.timeoutMs = timeoutMs;
    this.handlerName = handlerName;
  }
}

/**
 * State management errors: corrupt cache, missing directories, atomic write failures.
 */
export class StateError extends HookwiseError {
  constructor(message: string) {
    super(message);
    this.name = "StateError";
  }
}

/**
 * Analytics errors: SQLite write failures, schema problems.
 */
export class AnalyticsError extends HookwiseError {
  constructor(message: string) {
    super(message);
    this.name = "AnalyticsError";
  }
}

// --- Logging Infrastructure ---

// Internal log level for the current process, settable for testing
let currentLogLevel: "debug" | "info" | "warn" | "error" = "info";

/**
 * Set the log level for the current process.
 */
export function setLogLevel(
  level: "debug" | "info" | "warn" | "error"
): void {
  currentLogLevel = level;
}

/**
 * Get the current log level.
 */
export function getLogLevel(): "debug" | "info" | "warn" | "error" {
  return currentLogLevel;
}

/**
 * Resolves the log directory path. Uses the state dir as base.
 */
function getLogDir(): string {
  try {
    return join(getStateDir(), "logs");
  } catch {
    return DEFAULT_LOG_PATH;
  }
}

/**
 * Ensures the log directory exists.
 */
function ensureLogDir(): void {
  const logDir = getLogDir();
  if (!existsSync(logDir)) {
    mkdirSync(logDir, { recursive: true, mode: 0o700 });
  }
}

/**
 * Rotate a log file if it exceeds MAX_LOG_SIZE_BYTES.
 * Keeps up to MAX_LOG_ROTATIONS rotated files.
 *
 * error.log -> error.log.1 -> error.log.2 -> error.log.3 (deleted)
 */
function rotateLogIfNeeded(logPath: string): void {
  try {
    if (!existsSync(logPath)) return;
    const stats = statSync(logPath);
    if (stats.size < MAX_LOG_SIZE_BYTES) return;

    // Rotate existing rotated files (from oldest to newest)
    for (let i = MAX_LOG_ROTATIONS; i >= 1; i--) {
      const from = i === 1 ? logPath : `${logPath}.${i - 1}`;
      const to = `${logPath}.${i}`;
      if (i === MAX_LOG_ROTATIONS) {
        // Delete the oldest rotation to make room
        try {
          if (existsSync(to)) unlinkSync(to);
        } catch {
          // Ignore deletion errors
        }
      }
      if (existsSync(from)) {
        renameSync(from, to);
      }
    }
  } catch {
    // If rotation fails, continue — logging should never throw
  }
}

/**
 * Format a log entry with timestamp and optional context.
 */
function formatLogEntry(
  level: string,
  message: string,
  data?: unknown
): string {
  const timestamp = new Date().toISOString();
  let entry = `[${timestamp}] [${level.toUpperCase()}] ${message}`;
  if (data !== undefined) {
    try {
      entry += ` ${JSON.stringify(data)}`;
    } catch {
      entry += ` [unserializable data]`;
    }
  }
  return entry + "\n";
}

/**
 * Log an error to ~/.hookwise/logs/error.log with rotation.
 * Supports optional context metadata.
 *
 * This function never throws — logging failures are silently swallowed.
 */
export function logError(
  error: Error,
  context?: Record<string, unknown>
): void {
  try {
    ensureLogDir();
    const logPath = join(getLogDir(), "error.log");
    rotateLogIfNeeded(logPath);

    const entry = formatLogEntry("ERROR", `${error.name}: ${error.message}`, {
      stack: error.stack,
      ...context,
    });
    appendFileSync(logPath, entry);
  } catch {
    // Logging must never throw
  }
}

/**
 * Log a debug message to ~/.hookwise/logs/debug.log.
 * Only writes when the current log level is "debug".
 *
 * This function never throws.
 */
export function logDebug(message: string, data?: unknown): void {
  if (currentLogLevel !== "debug") return;
  try {
    ensureLogDir();
    const logPath = join(getLogDir(), "debug.log");
    rotateLogIfNeeded(logPath);

    const entry = formatLogEntry("DEBUG", message, data);
    appendFileSync(logPath, entry);
  } catch {
    // Logging must never throw
  }
}

// --- Fail-Open Dispatch Wrapper ---

/**
 * Safe dispatch wrapper that catches ALL exceptions and returns
 * a fail-open result: { stdout: null, stderr: null, exitCode: 0 }.
 *
 * This is the critical contract: hookwise never accidentally blocks
 * a tool call due to internal errors.
 */
export function safeDispatch(
  fn: () => DispatchResult
): DispatchResult {
  try {
    return fn();
  } catch (error: unknown) {
    logError(
      error instanceof Error ? error : new Error(String(error)),
      { context: "safeDispatch" }
    );
    return { stdout: null, stderr: null, exitCode: 0 };
  }
}
