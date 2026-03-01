/**
 * Built-in status line segments for hookwise v1.1.
 *
 * Each segment is a pure function: (cache, config) => string.
 * Returns empty string when data is unavailable.
 *
 * Feed segments (pulse, project, calendar, news, insights_*)
 * check TTL freshness before rendering — stale entries return "".
 */

import type { SegmentConfig, CacheEntry } from "../types.js";
import { isFresh } from "../feeds/cache-bus.js";

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

// --- Feed Platform Segments (v1.1) ---

/**
 * Check if a feed cache entry is fresh. Returns the entry data if fresh, null otherwise.
 */
function getFreshEntry(cache: Record<string, unknown>, key: string): Record<string, unknown> | null {
  const entry = cache[key] as (CacheEntry & Record<string, unknown>) | undefined;
  if (!entry) return null;
  if (!isFresh(entry)) return null;
  return entry;
}

const pulse: SegmentRenderer = (cache) => {
  const entry = getFreshEntry(cache, "pulse");
  if (!entry) return "";
  const value = entry.value as string | undefined;
  if (!value) return "";
  return value;
};

const project: SegmentRenderer = (cache) => {
  const entry = getFreshEntry(cache, "project");
  if (!entry) return "";
  const repo = entry.repo as string | undefined;
  if (!repo) return "";

  const detached = entry.detached as boolean | undefined;
  const branch = entry.branch as string | undefined;
  const branchDisplay = detached ? "(detached)" : `(${branch})`;

  let result = `\uD83D\uDCE6 ${repo} ${branchDisplay}`;

  const lastCommitTs = entry.last_commit_ts as number | undefined;
  if (lastCommitTs !== undefined) {
    const nowSec = Math.floor(Date.now() / 1000);
    const ageSec = nowSec - lastCommitTs;
    let ageStr: string;
    if (ageSec < 3600) {
      ageStr = `${Math.floor(ageSec / 60)}m ago`;
    } else if (ageSec < 86400) {
      ageStr = `${Math.floor(ageSec / 3600)}h ago`;
    } else {
      ageStr = `${Math.floor(ageSec / 86400)}d ago`;
    }
    result += ` \u2022 ${ageStr}`;
  }

  return result;
};

interface CalendarEventEntry {
  title: string;
  start: string;
  end: string;
  is_current: boolean;
}

const calendar: SegmentRenderer = (cache) => {
  const entry = getFreshEntry(cache, "calendar");
  if (!entry) return "";

  const events = (entry.events as CalendarEventEntry[] | undefined) ?? [];
  const nextEvent = entry.next_event as CalendarEventEntry | null | undefined;

  const currentEvent = events.find((e) => e.is_current);
  const moreCount = events.length > 1 ? events.length - 1 : 0;
  const moreSuffix = moreCount > 0 ? ` (+${moreCount} more)` : "";

  if (currentEvent) {
    const endsInMs = Date.parse(currentEvent.end) - Date.now();
    const endsInMin = Math.round(endsInMs / 60_000);
    return `\uD83D\uDCC5 ${currentEvent.title} (ends in ${endsInMin}min)${moreSuffix}`;
  }

  if (nextEvent) {
    const minutesUntil = Math.round((Date.parse(nextEvent.start) - Date.now()) / 60_000);

    if (minutesUntil > 60) {
      const hours = Math.round(minutesUntil / 60);
      return `\uD83D\uDCC5 Free for ${hours}h${moreSuffix}`;
    }
    if (minutesUntil >= 15) {
      return `\uD83D\uDCC5 ${nextEvent.title} in ${minutesUntil}min${moreSuffix}`;
    }
    if (minutesUntil >= 5) {
      return `\uD83D\uDCC5 ${nextEvent.title} in ${minutesUntil}min \u26A1${moreSuffix}`;
    }
    return `\uD83D\uDCC5 ${nextEvent.title} NOW${moreSuffix}`;
  }

  return "\uD83D\uDCC5 Free";
};

const MAX_NEWS_TITLE_LENGTH = 45;

const news: SegmentRenderer = (cache) => {
  const entry = getFreshEntry(cache, "news");
  if (!entry) return "";
  const story = entry.current_story as { title: string; score: number; url: string; id: number } | undefined;
  if (!story) return "";

  let title = story.title;
  if (title.length > MAX_NEWS_TITLE_LENGTH) {
    title = title.slice(0, MAX_NEWS_TITLE_LENGTH) + "\u2026";
  }

  if (story.score > 0) {
    return `\uD83D\uDCF0 ${title} (${story.score}pts)`;
  }
  return `\uD83D\uDCF0 ${title}`;
};

const insights_friction: SegmentRenderer = (cache) => {
  const entry = getFreshEntry(cache, "insights");
  if (!entry) return "";
  const frictionTotal = entry.friction_total as number | undefined;
  if (frictionTotal === undefined || frictionTotal === 0) return "";
  return `\u26A0\uFE0F ${frictionTotal} friction`;
};

const insights_pace: SegmentRenderer = (cache) => {
  const entry = getFreshEntry(cache, "insights");
  if (!entry) return "";
  const sessions = entry.total_sessions as number | undefined;
  const days = entry.days_active as number | undefined;
  if (!sessions || !days) return "";
  const pace = Math.round((sessions / days) * 10) / 10;
  return `\u23F1\uFE0F ${pace}/day`;
};

const insights_trend: SegmentRenderer = (cache) => {
  const entry = getFreshEntry(cache, "insights");
  if (!entry) return "";
  const avgDuration = entry.avg_duration_minutes as number | undefined;
  if (avgDuration === undefined) return "";
  return `\u231A ${Math.round(avgDuration)}m avg`;
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
  pulse,
  project,
  calendar,
  news,
  insights_friction,
  insights_pace,
  insights_trend,
};
