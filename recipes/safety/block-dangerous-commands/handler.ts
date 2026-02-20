/**
 * Block Dangerous Commands recipe handler.
 *
 * Checks Bash tool invocations against a list of dangerous command patterns.
 * Returns a block result if a dangerous pattern is detected.
 *
 * This handler supplements the guard rules defined in hooks.yaml
 * with programmatic pattern matching for more complex scenarios.
 */

import type { HookPayload, GuardResult } from "../../../src/core/types.js";

export interface BlockDangerousCommandsConfig {
  enabled: boolean;
  patterns: string[];
}

const DEFAULT_PATTERNS = [
  "rm -rf",
  "rm -fr",
  "--force",
  "force push",
  "git reset --hard",
  "git clean -fd",
  "git checkout .",
  "DROP TABLE",
  "DROP DATABASE",
  "TRUNCATE TABLE",
];

/**
 * Check a Bash tool invocation for dangerous command patterns.
 *
 * @param event - Hook payload with tool_name and tool_input
 * @param config - Recipe configuration with patterns list
 * @returns GuardResult — block if dangerous, allow otherwise
 */
export function checkDangerousCommand(
  event: HookPayload,
  config: BlockDangerousCommandsConfig
): GuardResult {
  if (!config.enabled) {
    return { action: "allow" };
  }

  if (event.tool_name !== "Bash") {
    return { action: "allow" };
  }

  const command = (event.tool_input?.command as string) ?? "";
  if (!command) {
    return { action: "allow" };
  }

  const patterns = config.patterns ?? DEFAULT_PATTERNS;

  for (const pattern of patterns) {
    if (command.includes(pattern)) {
      return {
        action: "block",
        reason: `Blocked: command contains dangerous pattern "${pattern}"`,
      };
    }
  }

  return { action: "allow" };
}
