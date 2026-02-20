/**
 * Commit Without Tests Guard recipe handler.
 *
 * Tracks whether test commands have been run in the session and
 * blocks/warns on git commit if no tests have been executed.
 */

import type { HookPayload, GuardResult } from "../../../src/core/types.js";

export interface CommitWithoutTestsConfig {
  enabled: boolean;
  testPatterns: string[];
  action: "block" | "warn";
}

export interface TestTrackingState {
  testsRunThisSession: boolean;
  lastTestResult: "pass" | "fail" | null;
  lastTestAt: string | null;
  testPatterns: string[];
}

const DEFAULT_TEST_PATTERNS = ["vitest", "jest", "pytest", "npm test", "bun test"];

/**
 * Check if a command matches any test pattern.
 *
 * @param command - The shell command being run
 * @param patterns - List of test command patterns to match
 * @returns True if the command matches a test pattern
 */
function isTestCommand(command: string, patterns: string[]): boolean {
  const lowerCommand = command.toLowerCase();
  return patterns.some((p) => lowerCommand.includes(p.toLowerCase()));
}

/**
 * Track a test run from a PostToolUse event.
 *
 * @param event - Hook payload from PostToolUse
 * @param state - Mutable test tracking state
 * @param config - Recipe configuration
 */
export function trackTestRun(
  event: HookPayload,
  state: TestTrackingState,
  config: CommitWithoutTestsConfig
): void {
  if (!config.enabled) return;
  if (event.tool_name !== "Bash") return;

  const command = (event.tool_input?.command as string) ?? "";
  const patterns = config.testPatterns ?? DEFAULT_TEST_PATTERNS;

  if (isTestCommand(command, patterns)) {
    state.testsRunThisSession = true;
    state.lastTestAt = new Date().toISOString();

    // Check exit code to determine pass/fail
    const exitCode = event.tool_input?.exit_code as number | undefined;
    state.lastTestResult = exitCode === 0 ? "pass" : "fail";
  }
}

/**
 * Check if a git commit should be blocked because no tests were run.
 *
 * @param event - Hook payload from PreToolUse
 * @param state - Test tracking state
 * @param config - Recipe configuration (action defaults to "block" if not provided)
 * @returns GuardResult — block/warn if no tests run, allow otherwise
 */
export function checkCommit(
  event: HookPayload,
  state: TestTrackingState,
  config?: CommitWithoutTestsConfig
): GuardResult {
  const action = config?.action ?? "block";

  if (event.tool_name !== "Bash") {
    return { action: "allow" };
  }

  const command = (event.tool_input?.command as string) ?? "";

  // Only check git commit commands
  if (!command.includes("git commit")) {
    return { action: "allow" };
  }

  if (!state.testsRunThisSession) {
    return {
      action,
      reason: "No tests have been run this session. Run tests before committing.",
    };
  }

  if (state.lastTestResult === "fail") {
    return {
      action: "warn",
      reason: `Last test run failed (at ${state.lastTestAt}). Consider fixing tests before committing.`,
    };
  }

  return { action: "allow" };
}
