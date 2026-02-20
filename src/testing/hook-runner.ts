/**
 * HookRunner — subprocess test helper for hookwise v1.0
 *
 * Spawns hook commands as child processes with JSON payloads piped to stdin.
 * Captures stdout, stderr, and exit code for assertion via HookResult.
 */

import { spawnSync } from "node:child_process";
import { HookResult } from "./hook-result.js";
import type { EventType } from "../core/types.js";

/** Default timeout for hook execution in milliseconds */
const DEFAULT_TIMEOUT_MS = 10_000;

/**
 * Test helper that runs hook commands as subprocesses.
 *
 * Pipes a JSON payload (with event_type and optional fields) to the
 * command's stdin and captures the result as a HookResult.
 */
export class HookRunner {
  private hookCommand: string;
  private defaultTimeout: number;

  /**
   * Create a new HookRunner for a specific hook command.
   *
   * @param hookCommand - Shell command to execute (e.g., "node my-hook.js")
   * @param options - Optional configuration
   * @param options.timeout - Default timeout in ms (default: 10000)
   */
  constructor(
    hookCommand: string,
    options?: { timeout?: number }
  ) {
    this.hookCommand = hookCommand;
    this.defaultTimeout = options?.timeout ?? DEFAULT_TIMEOUT_MS;
  }

  /**
   * Run the hook command with a given event type and payload.
   *
   * Pipes `{"event_type": eventType, ...payload}` as JSON to stdin.
   * Returns a HookResult with stdout, stderr, exit code, and duration.
   *
   * @param eventType - The hook event type
   * @param payload - Additional payload fields to include
   * @param options - Per-run options
   * @returns HookResult with captured output
   */
  run(
    eventType: EventType,
    payload?: Record<string, unknown>,
    options?: { timeout?: number }
  ): HookResult {
    const timeout = options?.timeout ?? this.defaultTimeout;
    const input = JSON.stringify({
      event_type: eventType,
      ...payload,
    });

    const startTime = Date.now();

    const result = spawnSync(this.hookCommand, {
      input,
      timeout,
      shell: true,
      encoding: "utf-8",
      stdio: ["pipe", "pipe", "pipe"],
    });

    const durationMs = Date.now() - startTime;
    const exitCode = result.status ?? 1;
    const stdout = (result.stdout ?? "").toString();
    const stderr = (result.stderr ?? "").toString();

    return new HookResult(stdout, stderr, exitCode, durationMs);
  }

  /**
   * Static convenience method: create a runner and execute in one call.
   *
   * @param hookCommand - Shell command to execute
   * @param eventType - The hook event type
   * @param payload - Additional payload fields
   * @param options - Execution options
   * @returns HookResult
   */
  static execute(
    hookCommand: string,
    eventType: EventType,
    payload?: Record<string, unknown>,
    options?: { timeout?: number }
  ): HookResult {
    const runner = new HookRunner(hookCommand, options);
    return runner.run(eventType, payload, options);
  }
}
