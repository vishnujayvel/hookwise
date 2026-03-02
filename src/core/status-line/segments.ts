/**
 * Built-in status line segments for hookwise v1.0.
 *
 * Each segment is a pure function: (cache, config) => string.
 * Returns empty string when data is unavailable.
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
  const btData = cache.builder_trap as
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

// --- Two-Tier Segments (v1.2) ---
// These read from _stdin (Claude Code's piped JSON merged into cache by the CLI command).

const context_bar: SegmentRenderer = (cache) => {
  const stdin = cache._stdin as
    | { context_window?: { used_percentage?: number } }
    | undefined;
  const pct = stdin?.context_window?.used_percentage;
  if (pct === undefined || pct === null) return "";

  const ratio = Math.min(1, Math.max(0, pct));
  const bar = renderBar(ratio, 10);
  const display = Math.round(ratio * 100);
  return `${bar} ${display}%`;
};

const mode_badge: SegmentRenderer = (cache) => {
  const btData = cache.builder_trap as
    | { current_mode?: string }
    | undefined;
  const mode = btData?.current_mode;
  if (!mode) return "";

  return `[${mode}]`;
};

const duration: SegmentRenderer = (cache) => {
  const stdin = cache._stdin as
    | { cost?: { total_duration_ms?: number } }
    | undefined;
  const ms = stdin?.cost?.total_duration_ms;
  if (ms === undefined || ms === null) return "";

  const minutes = Math.round(ms / 60_000);
  return formatDuration(minutes);
};

const practice_breadcrumb: SegmentRenderer = (cache) => {
  const practiceData = cache.practice as
    | { last_at?: string }
    | undefined;
  if (!practiceData?.last_at) return "";

  const lastAtMs = Date.parse(practiceData.last_at);
  if (Number.isNaN(lastAtMs)) return "";

  const ts = Math.floor(lastAtMs / 1000);
  return `Last practice: ${formatRelativeTime(ts)}`;
};

// --- Feed Platform Segments (v1.1) ---

/**
 * Format a unix timestamp into a human-readable relative time string.
 * Returns "Xm ago", "Xh ago", or "Xd ago".
 */
function formatRelativeTime(unixTimestamp: number): string {
  const diffMs = Date.now() - unixTimestamp * 1000;
  const diffMinutes = Math.floor(diffMs / 60_000);

  if (diffMinutes < 60) return `${diffMinutes}m ago`;

  const diffHours = Math.floor(diffMinutes / 60);
  if (diffHours < 24) return `${diffHours}h ago`;

  const diffDays = Math.floor(diffHours / 24);
  return `${diffDays}d ago`;
}

const pulse: SegmentRenderer = (cache) => {
  const pulseData = cache.pulse as
    | (CacheEntry & { value?: string })
    | undefined;
  if (!pulseData || !isFresh(pulseData)) return "";
  if (!pulseData.value) return "";

  return pulseData.value;
};

const project: SegmentRenderer = (cache) => {
  const projectData = cache.project as
    | (CacheEntry & { repo?: string; branch?: string; last_commit_ts?: number; detached?: boolean })
    | undefined;
  if (!projectData || !isFresh(projectData)) return "";
  if (!projectData.repo) return "";

  const branchDisplay = projectData.detached ? "detached" : (projectData.branch ?? "unknown");
  const age = projectData.last_commit_ts != null
    ? formatRelativeTime(projectData.last_commit_ts)
    : "";

  const parts = [`\uD83D\uDCE6 ${projectData.repo} (${branchDisplay})`];
  if (age) parts.push(age);
  return parts.join(" \u2022 ");
};

const calendar: SegmentRenderer = (cache) => {
  interface CalendarEvent {
    title: string;
    start: string;
    end: string;
    is_current: boolean;
  }
  const calData = cache.calendar as
    | (CacheEntry & { events?: CalendarEvent[]; next_event?: CalendarEvent | null })
    | undefined;
  if (!calData || !isFresh(calData)) return "";

  const events = calData.events ?? [];
  const nextEvent = calData.next_event ?? null;

  // Check for current event first
  const currentEvent = events.find((e) => e.is_current);
  if (currentEvent) {
    const endMs = new Date(currentEvent.end).getTime();
    const nowMs = Date.now();
    const endsInMin = Math.round((endMs - nowMs) / 60_000);
    const suffix = events.length > 1 ? ` (+${events.length - 1} more)` : "";
    return `\uD83D\uDCC5 ${currentEvent.title} (ends in ${endsInMin}min)${suffix}`;
  }

  // No current event and no next event
  if (!nextEvent) {
    return "\uD83D\uDCC5 Free";
  }

  const startMs = new Date(nextEvent.start).getTime();
  const nowMs = Date.now();
  const diffMin = Math.round((startMs - nowMs) / 60_000);

  let text: string;
  if (diffMin < 5) {
    text = `\uD83D\uDCC5 ${nextEvent.title} NOW`;
  } else if (diffMin < 15) {
    text = `\uD83D\uDCC5 ${nextEvent.title} in ${diffMin}min \u26A1`;
  } else if (diffMin <= 60) {
    text = `\uD83D\uDCC5 ${nextEvent.title} in ${diffMin}min`;
  } else {
    const hours = Math.round(diffMin / 60);
    text = `\uD83D\uDCC5 Free for ${hours}h`;
  }

  const otherCount = events.length > 1 ? events.length - 1 : 0;
  if (otherCount > 0) {
    text += ` (+${otherCount} more)`;
  }
  return text;
};

const news: SegmentRenderer = (cache) => {
  interface NewsStory {
    title: string;
    score: number;
    url: string;
    id: number;
  }
  const newsData = cache.news as
    | (CacheEntry & { current_story?: NewsStory })
    | undefined;
  if (!newsData || !isFresh(newsData)) return "";
  if (!newsData.current_story) return "";

  const story = newsData.current_story;
  const truncatedTitle =
    story.title.length > 45
      ? story.title.slice(0, 45) + "\u2026"
      : story.title;

  if (story.score === 0) {
    return `\uD83D\uDCF0 ${truncatedTitle}`;
  }
  return `\uD83D\uDCF0 ${truncatedTitle} (${story.score}pts)`;
};

// --- Insights Segments ---

/**
 * Map an hour (0-23) to a time-of-day label.
 */
function hourToLabel(hour: number): string {
  if (hour >= 6 && hour < 12) return "morning";
  if (hour >= 12 && hour < 18) return "afternoon";
  if (hour >= 18 && hour < 24) return "evening";
  return "night";
}

/**
 * Format a number with k suffix for thousands (e.g., 28000 → "28k").
 */
function formatLargeNumber(n: number): string {
  if (n >= 1000) {
    const k = n / 1000;
    return k === Math.floor(k) ? `${Math.floor(k)}k` : `${k.toFixed(1)}k`;
  }
  return String(n);
}

interface InsightsCacheEntry extends CacheEntry {
  total_sessions?: number;
  total_messages?: number;
  total_lines_added?: number;
  days_active?: number;
  top_tools?: Array<{ name: string; count: number }>;
  peak_hour?: number;
  friction_total?: number;
  recent_session?: {
    friction_count?: number;
  };
}

const insights_friction: SegmentRenderer = (cache) => {
  const data = cache.insights as InsightsCacheEntry | undefined;
  if (!data || !isFresh(data)) return "";

  const recentFriction = data.recent_session?.friction_count ?? 0;
  const totalFriction = data.friction_total ?? 0;

  if (recentFriction > 0) {
    return `\u26A0\uFE0F ${recentFriction} friction in last session`;
  }
  if (totalFriction > 0) {
    return `\u2705 Clean session | ${totalFriction} total friction`;
  }
  return "\u2705 No friction detected";
};

const insights_pace: SegmentRenderer = (cache) => {
  const data = cache.insights as InsightsCacheEntry | undefined;
  if (!data || !isFresh(data)) return "";

  const totalMessages = data.total_messages ?? 0;
  const daysActive = data.days_active || 1;
  const linesAdded = data.total_lines_added ?? 0;
  const sessions = data.total_sessions ?? 0;

  const msgsPerDay = Math.round(totalMessages / daysActive);
  const formattedLines = formatLargeNumber(linesAdded);

  return `\uD83D\uDCCA ${msgsPerDay} msgs/day | ${formattedLines}+ lines | ${sessions} sessions`;
};

const insights_trend: SegmentRenderer = (cache) => {
  const data = cache.insights as InsightsCacheEntry | undefined;
  if (!data || !isFresh(data)) return "";

  const topTools = data.top_tools ?? [];
  const peakHour = data.peak_hour ?? 0;

  const toolNames = topTools
    .slice(0, 2)
    .map((t) => t.name)
    .join(", ");
  const peakLabel = hourToLabel(peakHour);

  if (!toolNames) return "";

  return `\uD83D\uDD27 Top: ${toolNames} | Peak: ${peakLabel}`;
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
  context_bar,
  mode_badge,
  duration,
  practice_breadcrumb,
  pulse,
  project,
  calendar,
  news,
  insights_friction,
  insights_pace,
  insights_trend,
};
