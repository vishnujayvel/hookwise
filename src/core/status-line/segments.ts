/**
 * Built-in status line segments for hookwise v1.0.
 *
 * Each segment is a pure function: (cache, config) => string.
 * Returns empty string when data is unavailable.
 */

import type { SegmentConfig } from "../types.js";

type SegmentRenderer = (
  cache: Record<string, unknown>,
  config: SegmentConfig
) => string;

/**
 * Format a duration in minutes into "XhYm" format.
 */
function formatDuration(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = Math.round(minutes % 60);
  if (h > 0) return `${h}h${String(m).padStart(2, "0")}m`;
  return `${m}m`;
}

/**
 * Render a progress bar using block characters.
 * @param ratio - Value between 0 and 1
 * @param width - Total bar width in characters (default 6)
 */
function renderBar(ratio: number, width: number = 6): string {
  const filled = Math.round(ratio * width);
  const empty = width - filled;
  return "\u2588".repeat(filled) + "\u2591".repeat(empty);
}

// --- Segment Implementations ---

const clock: SegmentRenderer = () => {
  return new Date().toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
};

const mantra: SegmentRenderer = (cache) => {
  const mantraData = cache.mantra as { text?: string } | undefined;
  return mantraData?.text || "";
};

const builder_trap: SegmentRenderer = (cache) => {
  const btData = cache.builderTrap as
    | { alertLevel?: string; toolingMinutes?: number }
    | undefined;
  if (!btData?.alertLevel || btData.alertLevel === "none") return "";

  const mins = Math.round(btData.toolingMinutes ?? 0);
  const prefix: Record<string, string> = {
    yellow: "\u26A0\uFE0F",
    orange: "\uD83D\uDFE0",
    red: "\uD83D\uDD34",
  };
  const emoji = prefix[btData.alertLevel] ?? "";
  return `${emoji} ${mins}m tooling`;
};

const session: SegmentRenderer = (cache) => {
  const sessionData = cache.session as
    | { startedAt?: string; toolCalls?: number }
    | undefined;
  if (!sessionData?.startedAt) return "";

  const startMs = new Date(sessionData.startedAt).getTime();
  const nowMs = Date.now();
  const minutes = Math.round((nowMs - startMs) / 60000);
  const calls = sessionData.toolCalls ?? 0;

  return `${formatDuration(minutes)} \u2022 ${calls} calls`;
};

const practice: SegmentRenderer = (cache) => {
  const practiceData = cache.practice as
    | { todayTotal?: number }
    | undefined;
  if (!practiceData?.todayTotal) return "";

  return `\uD83C\uDFAF ${practiceData.todayTotal} today`;
};

const ai_ratio: SegmentRenderer = (cache) => {
  const sessionData = cache.session as
    | { aiRatio?: number }
    | undefined;
  if (sessionData?.aiRatio === undefined) return "";

  const pct = Math.round(sessionData.aiRatio * 100);
  const bar = renderBar(sessionData.aiRatio);
  return `AI: ${pct}% ${bar}`;
};

const cost: SegmentRenderer = (cache) => {
  const costData = cache.cost as
    | { sessionCostUsd?: number }
    | undefined;
  if (costData?.sessionCostUsd === undefined) return "";

  return `$${costData.sessionCostUsd.toFixed(2)}`;
};

const streak: SegmentRenderer = (cache) => {
  const streakData = cache.streak as
    | { coding?: number }
    | undefined;
  if (!streakData?.coding) return "";

  return `\uD83D\uDD25 ${streakData.coding}d streak`;
};

/**
 * Registry of all built-in segments.
 */
export const BUILTIN_SEGMENTS: Record<string, SegmentRenderer> = {
  clock,
  mantra,
  builder_trap,
  session,
  practice,
  ai_ratio,
  cost,
  streak,
};
