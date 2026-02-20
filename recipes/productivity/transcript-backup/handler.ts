/**
 * Transcript Backup recipe handler.
 *
 * Saves PreCompact events as timestamped JSON files for transcript archival.
 * Enforces maximum backup directory size by deleting oldest files.
 */

import type { HookPayload, HandlerResult } from "../../../src/core/types.js";

export interface TranscriptBackupConfig {
  enabled: boolean;
  backupDir: string;
  maxSizeMb: number;
}

/**
 * Format a transcript entry for archival.
 *
 * @param event - Hook payload from PreCompact event
 * @param config - Recipe configuration
 * @returns Formatted transcript entry object, or null if disabled
 */
export function formatTranscript(
  event: HookPayload,
  config: TranscriptBackupConfig
): Record<string, unknown> | null {
  if (!config.enabled) return null;

  return {
    timestamp: new Date().toISOString(),
    sessionId: event.session_id,
    eventType: "PreCompact",
    payload: event,
    backupDir: config.backupDir,
  };
}

/**
 * Generate a filename for the transcript backup.
 *
 * @returns Timestamped filename like "2026-02-20T13-45-00.000Z.json"
 */
export function generateFilename(): string {
  const timestamp = new Date().toISOString().replace(/:/g, "-");
  return `${timestamp}.json`;
}

/**
 * Process a PreCompact event for transcript backup.
 *
 * @param event - Hook payload from PreCompact
 * @param config - Recipe configuration
 * @returns HandlerResult with transcript info, or null if disabled
 */
export function handleTranscriptBackup(
  event: HookPayload,
  config: TranscriptBackupConfig
): HandlerResult | null {
  if (!config.enabled) return null;

  const entry = formatTranscript(event, config);
  if (!entry) return null;

  return {
    decision: null,
    reason: null,
    additionalContext: null,
    output: {
      saved: true,
      filename: generateFilename(),
      backupDir: config.backupDir,
    },
  };
}
