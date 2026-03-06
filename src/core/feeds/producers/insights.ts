/**
 * Insights feed producer: aggregates Claude Code usage data from
 * ~/.claude/usage-data/ and writes metrics to the cache bus.
 *
 * Reads session-meta and facets JSON files, filters by staleness window,
 * and computes aggregated metrics. Returns null when no valid sessions exist.
 *
 * All field reads use defensive access (optional chaining + defaults) since
 * the usage-data schema is owned by Anthropic and may change without notice.
 *
 * Requirements: FR-1.1, FR-1.2, FR-1.3, FR-1.4, FR-1.5, FR-1.6, FR-1.7
 */

import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import type { FeedProducer, InsightsFeedConfig } from "../../types.js";

export interface InsightsData {
  total_sessions: number;
  total_messages: number;
  total_lines_added: number;
  avg_duration_minutes: number;
  top_tools: Array<{ name: string; count: number }>;
  friction_counts: Record<string, number>;
  friction_total: number;
  peak_hour: number;
  days_active: number;
  staleness_days: number;
  recent_msgs_per_day: number;
  recent_messages: number;
  recent_days_active: number;
  recent_session: {
    id: string;
    duration_minutes: number;
    lines_added: number;
    friction_count: number;
    outcome: string;
    tool_errors: number;
  };
}

function resolvePath(p: string): string {
  if (p.startsWith("~/")) {
    return join(homedir(), p.slice(2));
  }
  return p;
}

function readJsonFiles(dirPath: string): Array<Record<string, unknown>> {
  let files: string[];
  try {
    files = readdirSync(dirPath).filter((f) => f.endsWith(".json"));
  } catch {
    return [];
  }

  const results: Array<Record<string, unknown>> = [];
  for (const file of files) {
    try {
      const content = readFileSync(join(dirPath, file), "utf-8");
      const parsed = JSON.parse(content);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        results.push(parsed as Record<string, unknown>);
      }
    } catch {
      // fail-open: skip malformed files
    }
  }
  return results;
}

function isWithinWindow(startTime: string | undefined, cutoffMs: number): boolean {
  if (!startTime || typeof startTime !== "string") return false;
  const ts = Date.parse(startTime);
  if (Number.isNaN(ts)) return false;
  return ts >= cutoffMs;
}

/**
 * Aggregate session-meta and facets data within the staleness window.
 *
 * Exported for direct testing — the producer factory wraps this.
 */
export function aggregateInsights(
  usageDataPath: string,
  stalenessDays: number,
  now: number = Date.now(),
): InsightsData | null {
  const resolvedPath = resolvePath(usageDataPath);
  const sessionMetaDir = join(resolvedPath, "session-meta");
  const facetsDir = join(resolvedPath, "facets");

  const cutoffMs = now - stalenessDays * 24 * 60 * 60 * 1000;

  // Read and filter session-meta files
  const allSessions = readJsonFiles(sessionMetaDir);
  const validSessions = allSessions.filter((s) =>
    isWithinWindow(s.start_time as string | undefined, cutoffMs),
  );

  if (validSessions.length === 0) return null;

  // Build a set of valid session IDs for facets matching
  const validSessionIds = new Set<string>();
  for (const s of validSessions) {
    const id = s.session_id as string | undefined;
    if (id) validSessionIds.add(id);
  }

  // Accumulators
  let totalMessages = 0;
  let totalLinesAdded = 0;
  let totalDuration = 0;
  const toolCounts: Record<string, number> = {};
  const hourCounts: number[] = new Array(24).fill(0);
  const activeDates = new Set<string>();
  let recentSession: Record<string, unknown> | null = null;
  let recentStartTime = 0;

  for (const session of validSessions) {
    // Accumulate totals with defensive reads
    totalMessages += (session.user_message_count as number | undefined) ?? 0;
    totalLinesAdded += (session.lines_added as number | undefined) ?? 0;
    totalDuration += (session.duration_minutes as number | undefined) ?? 0;

    // Tool counts
    const tools = session.tool_counts as Record<string, number> | undefined;
    if (tools && typeof tools === "object") {
      for (const [name, count] of Object.entries(tools)) {
        if (typeof count === "number") {
          toolCounts[name] = (toolCounts[name] ?? 0) + count;
        }
      }
    }

    // Message hours for peak hour
    const hours = session.message_hours as number[] | undefined;
    if (Array.isArray(hours)) {
      for (const h of hours) {
        if (typeof h === "number" && h >= 0 && h < 24) {
          hourCounts[h]++;
        }
      }
    }

    // Days active — unique dates (converted to local timezone)
    const startTime = session.start_time as string | undefined;
    if (startTime) {
      const ts = Date.parse(startTime);
      if (!Number.isNaN(ts)) {
        const localDate = new Date(ts - new Date(ts).getTimezoneOffset() * 60000);
        const dateStr = localDate.toISOString().slice(0, 10);
        if (dateStr.length === 10) activeDates.add(dateStr);

        if (ts > recentStartTime) {
          recentStartTime = ts;
          recentSession = session;
        }
      }
    }
  }

  // Read facets for friction data
  const frictionCounts: Record<string, number> = {};
  const allFacets = readJsonFiles(facetsDir);
  let recentFrictionCount = 0;
  let recentOutcome = "";

  for (const facet of allFacets) {
    const facetSessionId = facet.session_id as string | undefined;
    if (!facetSessionId || !validSessionIds.has(facetSessionId)) continue;

    const friction = facet.friction_counts as Record<string, number> | undefined;
    if (friction && typeof friction === "object") {
      for (const [category, count] of Object.entries(friction)) {
        if (typeof count === "number") {
          frictionCounts[category] = (frictionCounts[category] ?? 0) + count;
        }
      }
    }

    // Track friction for the most recent session
    if (recentSession && facetSessionId === (recentSession.session_id as string)) {
      if (friction) {
        recentFrictionCount = Object.values(friction).reduce(
          (sum, v) => sum + (typeof v === "number" ? v : 0),
          0,
        );
      }
      recentOutcome = (facet.outcome as string | undefined) ?? "";
    }
  }

  // Compute derived metrics
  const avgDuration = totalDuration / validSessions.length;

  const topTools = Object.entries(toolCounts)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 5)
    .map(([name, count]) => ({ name, count }));

  let peakHourUtc = 0;
  let maxHourCount = 0;
  for (let h = 0; h < 24; h++) {
    if (hourCounts[h] > maxHourCount) {
      maxHourCount = hourCounts[h];
      peakHourUtc = h;
    }
  }

  const offsetMinutes = new Date(now).getTimezoneOffset();
  const localPeakMinutes = (peakHourUtc * 60 - offsetMinutes + 24 * 60) % (24 * 60);
  const peakHour = Math.floor(localPeakMinutes / 60);

  const frictionTotal = Object.values(frictionCounts).reduce((sum, v) => sum + v, 0);

  // Compute recent (last 7 days) metrics for trend detection
  const recentCutoffMs = now - 7 * 24 * 60 * 60 * 1000;
  let recentMessages = 0;
  const recentActiveDates = new Set<string>();

  for (const s of validSessions) {
    const st = s.start_time as string | undefined;
    if (!st) continue;
    const sTs = Date.parse(st);
    if (Number.isNaN(sTs) || sTs < recentCutoffMs) continue;

    recentMessages += (s.user_message_count as number | undefined) ?? 0;
    const localDate = new Date(sTs - new Date(sTs).getTimezoneOffset() * 60000);
    const dStr = localDate.toISOString().slice(0, 10);
    if (dStr.length === 10) recentActiveDates.add(dStr);
  }

  const recentDaysActive = recentActiveDates.size;
  const recentMsgsPerDay = recentDaysActive > 0
    ? Math.round(recentMessages / recentDaysActive)
    : 0;

  const result: InsightsData = {
    total_sessions: validSessions.length,
    total_messages: totalMessages,
    total_lines_added: totalLinesAdded,
    avg_duration_minutes: Math.round(avgDuration * 10) / 10,
    top_tools: topTools,
    friction_counts: frictionCounts,
    friction_total: frictionTotal,
    peak_hour: peakHour,
    days_active: activeDates.size,
    staleness_days: stalenessDays,
    recent_msgs_per_day: recentMsgsPerDay,
    recent_messages: recentMessages,
    recent_days_active: recentDaysActive,
    recent_session: {
      id: (recentSession?.session_id as string | undefined) ?? "",
      duration_minutes: (recentSession?.duration_minutes as number | undefined) ?? 0,
      lines_added: (recentSession?.lines_added as number | undefined) ?? 0,
      friction_count: recentFrictionCount,
      outcome: recentOutcome,
      tool_errors: (recentSession?.tool_errors as number | undefined) ?? 0,
    },
  };

  return result;
}

/**
 * Create a FeedProducer for the insights feed.
 *
 * @param config - Insights feed configuration
 */
export function createInsightsProducer(
  config: InsightsFeedConfig,
): FeedProducer {
  return async (): Promise<Record<string, unknown> | null> => {
    const result = aggregateInsights(config.usageDataPath, config.stalenessDays);
    if (!result) return null;
    return result as unknown as Record<string, unknown>;
  };
}
