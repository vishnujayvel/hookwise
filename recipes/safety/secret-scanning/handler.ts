/**
 * Secret Scanning recipe handler.
 *
 * Detects when Write tool creates or modifies files that may contain
 * sensitive data: .env files, credentials, PEM keys, and embedded API keys.
 */

import type { HookPayload, GuardResult } from "../../../src/core/types.js";

export interface SecretScanningConfig {
  enabled: boolean;
  sensitiveFilePatterns: string[];
  apiKeyPatterns: string[];
}

const DEFAULT_FILE_PATTERNS = [
  ".env",
  ".env.local",
  ".env.production",
  "credentials",
  "credentials.json",
  ".pem",
  ".key",
  ".p12",
  ".pfx",
  "id_rsa",
  "id_ed25519",
];

const DEFAULT_API_KEY_PATTERNS = [
  "AKIA[0-9A-Z]{16}",        // AWS Access Key
  "sk-[a-zA-Z0-9]{48}",      // OpenAI/Anthropic key
  "ghp_[a-zA-Z0-9]{36}",     // GitHub PAT
  "glpat-[a-zA-Z0-9_\\-]{20}", // GitLab PAT
];

/**
 * Check a Write tool invocation for potential secret exposure.
 *
 * @param event - Hook payload with tool_name and tool_input
 * @param config - Recipe configuration with file/key patterns
 * @returns GuardResult — warn if secrets detected, allow otherwise
 */
export function checkSecrets(
  event: HookPayload,
  config: SecretScanningConfig
): GuardResult {
  if (!config.enabled) {
    return { action: "allow" };
  }

  if (event.tool_name !== "Write" && event.tool_name !== "Edit") {
    return { action: "allow" };
  }

  const filePath = (event.tool_input?.file_path as string) ?? "";
  const content = (event.tool_input?.content as string) ??
    (event.tool_input?.new_string as string) ?? "";

  // Check file name against sensitive patterns
  const filePatterns = config.sensitiveFilePatterns ?? DEFAULT_FILE_PATTERNS;
  for (const pattern of filePatterns) {
    if (filePath.endsWith(pattern) || filePath.includes(`/${pattern}`)) {
      return {
        action: "warn",
        reason: `Warning: writing to sensitive file matching pattern "${pattern}"`,
      };
    }
  }

  // Check content for API key patterns
  const keyPatterns = config.apiKeyPatterns ?? DEFAULT_API_KEY_PATTERNS;
  for (const pattern of keyPatterns) {
    try {
      const regex = new RegExp(pattern);
      if (regex.test(content)) {
        return {
          action: "warn",
          reason: `Warning: content may contain an API key or secret (pattern: ${pattern})`,
        };
      }
    } catch {
      // Invalid regex: skip
    }
  }

  return { action: "allow" };
}
