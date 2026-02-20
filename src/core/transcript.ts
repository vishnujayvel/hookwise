/**
 * Transcript backup handler for hookwise v1.0
 *
 * Saves hook payloads as timestamped JSON files with atomic writes.
 * Enforces max backup directory size by deleting oldest files first.
 * Fail-open: returns null on errors instead of throwing.
 */

import { readdirSync, statSync, unlinkSync } from "node:fs";
import { join } from "node:path";
import { atomicWriteJSON, ensureDir } from "./state.js";
import type { TranscriptConfig } from "./types.js";

/**
 * Save a transcript payload to the backup directory.
 *
 * Creates a file named `<timestamp>.json` where the timestamp is
 * ISO 8601 format sanitized for filesystem (colons replaced with dashes).
 *
 * @param payload - Hook payload data to save
 * @param config - Transcript backup configuration
 * @returns The file path of the saved transcript, or null on error
 */
export function saveTranscript(
  payload: Record<string, unknown>,
  config: TranscriptConfig
): string | null {
  try {
    ensureDir(config.backupDir);

    const timestamp = new Date()
      .toISOString()
      .replace(/:/g, "-");
    const fileName = `${timestamp}.json`;
    const filePath = join(config.backupDir, fileName);

    atomicWriteJSON(filePath, payload);
    return filePath;
  } catch {
    // Fail-open: return null on errors
    return null;
  }
}

/**
 * Enforce maximum backup directory size by deleting oldest files.
 *
 * Calculates total size of all files in the backup directory.
 * If total exceeds maxSizeMb, deletes files oldest-first until
 * the total is under the limit.
 *
 * @param config - Transcript backup configuration with maxSizeMb
 */
export function enforceMaxSize(config: TranscriptConfig): void {
  try {
    if (!config.backupDir) return;

    const maxBytes = config.maxSizeMb * 1024 * 1024;
    const files = readdirSync(config.backupDir)
      .filter((f) => f.endsWith(".json"))
      .map((f) => {
        const fullPath = join(config.backupDir, f);
        const stats = statSync(fullPath);
        return {
          path: fullPath,
          name: f,
          size: stats.size,
          mtimeMs: stats.mtimeMs,
        };
      })
      .sort((a, b) => a.mtimeMs - b.mtimeMs); // oldest first

    let totalSize = files.reduce((sum, f) => sum + f.size, 0);

    for (const file of files) {
      if (totalSize <= maxBytes) break;
      try {
        unlinkSync(file.path);
        totalSize -= file.size;
      } catch {
        // Skip files that can't be deleted
      }
    }
  } catch {
    // Fail-open: silently ignore enforcement errors
  }
}
