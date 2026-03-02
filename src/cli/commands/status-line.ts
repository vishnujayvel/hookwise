/**
 * CLI command: hookwise status-line
 *
 * Reads Claude Code's stdin JSON, merges with hookwise cache,
 * and outputs a two-tier ANSI-colored status line to stdout.
 *
 * Uses plain process.stdin/stdout (not React/Ink) for speed.
 * This is the hot path — Claude Code calls it on every render tick.
 */

import { readFileSync } from "node:fs";
import { safeReadJSON, atomicWriteJSON } from "../../core/state.js";
import { DEFAULT_CACHE_PATH } from "../../core/constants.js";
import { renderTwoTier, DEFAULT_TWO_TIER_CONFIG } from "../../core/status-line/two-tier.js";

/**
 * Stdin data shape from Claude Code's status line protocol.
 */
interface ClaudeCodeStdinData {
  session_id?: string;
  context_window?: {
    used_percentage?: number;
  };
  cost?: {
    total_cost_usd?: number;
    total_duration_ms?: number;
  };
  transcript_summary?: string;
}

/**
 * Read all of stdin synchronously.
 * Returns empty string if stdin is not piped or empty.
 */
function readStdin(): string {
  try {
    if (process.stdin.isTTY) return "";
    return readFileSync(0, "utf-8");
  } catch {
    return "";
  }
}

/**
 * Run the status-line command.
 * Reads stdin JSON from Claude Code, merges with hookwise cache,
 * renders two-tier status line, and writes to stdout.
 */
export async function runStatusLineCommand(): Promise<void> {
  try {
    // Read and parse stdin from Claude Code
    const raw = readStdin();
    let stdinData: ClaudeCodeStdinData = {};
    if (raw.trim()) {
      try {
        stdinData = JSON.parse(raw) as ClaudeCodeStdinData;
      } catch {
        // Invalid JSON — continue with empty stdin data
      }
    }

    // Load hookwise cache
    const cache = safeReadJSON<Record<string, unknown>>(DEFAULT_CACHE_PATH, {});

    // Merge stdin data into cache as _stdin key
    cache._stdin = stdinData;

    // Also merge stdin cost data into the cache cost key for the existing cost segment
    if (stdinData.cost?.total_cost_usd !== undefined) {
      const existingCost = (cache.cost ?? {}) as Record<string, unknown>;
      cache.cost = {
        ...existingCost,
        sessionCostUsd: stdinData.cost.total_cost_usd,
      };
    }

    // Advance rotation index for line 2 cycling
    const prevIndex = (cache._rotation_index as number) ?? 0;
    const nextIndex = prevIndex + 1;
    cache._rotation_index = nextIndex;

    // Persist rotation index back to cache
    try {
      const diskCache = safeReadJSON<Record<string, unknown>>(DEFAULT_CACHE_PATH, {});
      diskCache._rotation_index = nextIndex;
      atomicWriteJSON(DEFAULT_CACHE_PATH, diskCache);
    } catch {
      // Non-critical — rotation index just won't persist
    }

    // Render and output
    const output = renderTwoTier(DEFAULT_TWO_TIER_CONFIG, cache);
    if (output) {
      process.stdout.write(output);
    }
  } catch {
    // Fail-open: output nothing rather than break Claude Code's UI
  }
}
