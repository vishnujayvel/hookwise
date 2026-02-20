/**
 * File Creation Police recipe handler.
 *
 * Tracks Write tool file creation events and warns when too many
 * new files are created. Detects similar-named files using a
 * strip-trailing-digits algorithm.
 */

import { basename, dirname, extname } from "node:path";
import type { HookPayload } from "../../../src/core/types.js";

export interface FileCreationConfig {
  enabled: boolean;
  maxNewFiles: number;
  ignorePatterns: string[];
}

export interface FileCreationState {
  filesCreatedThisSession: string[];
  creationCount: number;
  warningEmitted: boolean;
}

export interface FilePoliceResult {
  action: "allow" | "warn";
  message?: string;
  similarFile?: string;
}

/**
 * Check if a file path matches any ignore pattern.
 * Uses simple glob matching: * matches any characters.
 *
 * @param filePath - Path to check
 * @param patterns - Glob patterns to match against
 * @returns True if the path matches any pattern
 */
function matchesIgnorePattern(filePath: string, patterns: string[]): boolean {
  const name = basename(filePath);
  for (const pattern of patterns) {
    // Convert simple glob to regex
    const regexStr = pattern
      .replace(/\./g, "\\.")
      .replace(/\*/g, ".*");
    try {
      const regex = new RegExp(`^${regexStr}$`);
      if (regex.test(name)) return true;
    } catch {
      // Invalid pattern: skip
    }
  }
  return false;
}

/**
 * Strip trailing digits from a filename (before the extension).
 *
 * Examples:
 * - "utils2.ts" -> "utils"
 * - "helper123.ts" -> "helper"
 * - "config.ts" -> "config"
 * - "test_001.py" -> "test_"
 *
 * @param fileName - The filename (basename) to strip
 * @returns Filename with trailing digits removed from the stem
 */
export function stripTrailingDigits(fileName: string): string {
  const ext = extname(fileName);
  const stem = basename(fileName, ext);
  const stripped = stem.replace(/\d+$/, "");
  return stripped || stem; // If stripping removes everything, keep original
}

/**
 * Find files with similar names in a list of existing files.
 *
 * Uses strip-trailing-digits algorithm: strip trailing digits from the
 * target basename (before extension), then compare case-insensitively
 * against existing files with the same extension in the same directory.
 *
 * @param targetPath - Path of the new file being created
 * @param existingFiles - List of existing file paths
 * @returns Array of similar file paths
 */
export function findSimilarFiles(
  targetPath: string,
  existingFiles: string[]
): string[] {
  const targetDir = dirname(targetPath);
  const targetExt = extname(targetPath);
  const targetStem = stripTrailingDigits(basename(targetPath)).toLowerCase();

  const similar: string[] = [];

  for (const existing of existingFiles) {
    const existingDir = dirname(existing);
    const existingExt = extname(existing);

    // Same directory and same extension
    if (existingDir !== targetDir || existingExt !== targetExt) continue;

    // Skip the exact same file
    if (existing === targetPath) continue;

    const existingStem = stripTrailingDigits(basename(existing)).toLowerCase();

    if (targetStem === existingStem) {
      similar.push(existing);
    }
  }

  return similar;
}

/**
 * Track a file creation event and check if action is needed.
 *
 * @param event - Hook payload from PreToolUse/PostToolUse for Write tool
 * @param state - Mutable file creation tracking state
 * @param config - Recipe configuration
 * @returns FilePoliceResult with action and optional message
 */
export function trackFileCreation(
  event: HookPayload,
  state: FileCreationState,
  config: FileCreationConfig
): FilePoliceResult {
  if (!config.enabled) {
    return { action: "allow" };
  }

  if (event.tool_name !== "Write") {
    return { action: "allow" };
  }

  const filePath = (event.tool_input?.file_path as string) ?? "";
  if (!filePath) {
    return { action: "allow" };
  }

  // Check ignore patterns
  const ignorePatterns = config.ignorePatterns ?? [];
  if (matchesIgnorePattern(filePath, ignorePatterns)) {
    return { action: "allow" };
  }

  // Track the creation
  if (!state.filesCreatedThisSession.includes(filePath)) {
    state.filesCreatedThisSession.push(filePath);
    state.creationCount = state.filesCreatedThisSession.length;
  }

  // Check for similar files
  const similar = findSimilarFiles(filePath, state.filesCreatedThisSession);
  if (similar.length > 0) {
    return {
      action: "warn",
      message: `Similar file already created this session: ${similar[0]}. Consider editing the existing file instead.`,
      similarFile: similar[0],
    };
  }

  // Check max new files threshold
  const maxNewFiles = config.maxNewFiles ?? 5;
  if (state.creationCount > maxNewFiles && !state.warningEmitted) {
    state.warningEmitted = true;
    return {
      action: "warn",
      message: `${state.creationCount} new files created this session (limit: ${maxNewFiles}). Consider editing existing files instead of creating new ones.`,
    };
  }

  return { action: "allow" };
}
