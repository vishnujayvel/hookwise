/**
 * Atomic state management utilities for hookwise v1.0
 *
 * All functions are synchronous to match the dispatch hot-path model.
 * Uses temp-file-plus-rename for atomic writes (safe on POSIX).
 */

import {
  existsSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
  renameSync,
  unlinkSync,
} from "node:fs";
import { join, dirname } from "node:path";
import { homedir } from "node:os";
import { randomBytes } from "node:crypto";
import { DEFAULT_STATE_DIR, DEFAULT_DIR_MODE } from "./constants.js";

/**
 * Resolves the hookwise state directory path.
 *
 * Priority:
 * 1. HOOKWISE_STATE_DIR env var
 * 2. ~/.hookwise/ (default)
 */
export function getStateDir(): string {
  const envDir = process.env.HOOKWISE_STATE_DIR;
  if (envDir) return envDir;
  return DEFAULT_STATE_DIR;
}

/**
 * Create a directory recursively with specified permissions.
 * Idempotent: does nothing if the directory already exists.
 *
 * @param dirPath - Absolute path to the directory
 * @param mode - POSIX permissions (default: 0o700, owner-only)
 */
export function ensureDir(
  dirPath: string,
  mode: number = DEFAULT_DIR_MODE
): void {
  if (!existsSync(dirPath)) {
    mkdirSync(dirPath, { recursive: true, mode });
  }
}

/**
 * Atomically write JSON data to a file.
 *
 * Writes to a temporary file in the same directory first, then renames
 * it to the target path. This guarantees atomic writes on POSIX systems:
 * readers will see either the old or new content, never a partial write.
 *
 * @param filePath - Absolute path to the target JSON file
 * @param data - Data to serialize and write
 */
export function atomicWriteJSON(filePath: string, data: unknown): void {
  const dir = dirname(filePath);
  ensureDir(dir);

  const suffix = randomBytes(6).toString("hex");
  const tempPath = join(dir, `.tmp-${suffix}`);

  try {
    writeFileSync(tempPath, JSON.stringify(data, null, 2) + "\n", "utf-8");
    renameSync(tempPath, filePath);
  } catch (error) {
    // Clean up temp file if rename failed
    try {
      if (existsSync(tempPath)) {
        unlinkSync(tempPath);
      }
    } catch {
      // Ignore cleanup errors
    }
    throw error;
  }
}

/**
 * Safely read and parse a JSON file with a fallback value.
 *
 * Returns the fallback if the file does not exist, is unreadable,
 * or contains invalid JSON.
 *
 * @param filePath - Absolute path to the JSON file
 * @param fallback - Default value to return on failure
 */
export function safeReadJSON<T>(filePath: string, fallback: T): T {
  try {
    if (!existsSync(filePath)) return fallback;
    const content = readFileSync(filePath, "utf-8");
    return JSON.parse(content) as T;
  } catch {
    return fallback;
  }
}
