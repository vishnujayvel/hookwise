/**
 * Stats query interface for hookwise analytics.
 *
 * Provides aggregation queries against the analytics database:
 * - Daily summaries: events and line counts per date
 * - Tool breakdown: groups by tool, sums lines and counts
 * - Authorship summary: AI score distribution and classification breakdown
 * - Combined stats: master query combining all of the above
 */

import type { AnalyticsDB } from "./db.js";
import type {
  DailySummary,
  ToolBreakdownEntry,
  AuthorshipSummary,
  StatsOptions,
  StatsResult,
  AIClassification,
} from "../types.js";
import { logError } from "../errors.js";

/**
 * Query daily summary aggregates.
 *
 * Groups events by date for the last N days (default 7).
 * Returns total events, tool calls, lines added/removed, and session count.
 *
 * @param db - Analytics database instance
 * @param days - Number of days to look back (default 7)
 * @param options - Optional session/date filters
 */
export function queryDailySummary(
  db: AnalyticsDB,
  days: number = 7,
  options?: StatsOptions
): DailySummary[] {
  try {
    const rawDb = db.getDatabase();

    let sql = `
      SELECT
        DATE(e.timestamp) as date,
        COUNT(*) as total_events,
        COUNT(e.tool_name) as total_tool_calls,
        COALESCE(SUM(e.lines_added), 0) as lines_added,
        COALESCE(SUM(e.lines_removed), 0) as lines_removed,
        COUNT(DISTINCT e.session_id) as sessions
      FROM events e
    `;

    const conditions: string[] = [];
    const params: unknown[] = [];

    if (options?.sessionId) {
      conditions.push("e.session_id = ?");
      params.push(options.sessionId);
    }

    if (options?.from) {
      conditions.push("e.timestamp >= ?");
      params.push(options.from);
    } else {
      conditions.push("e.timestamp >= DATE('now', ?)");
      params.push(`-${days} days`);
    }

    if (options?.to) {
      conditions.push("e.timestamp <= ?");
      params.push(options.to);
    }

    if (conditions.length > 0) {
      sql += " WHERE " + conditions.join(" AND ");
    }

    sql += " GROUP BY DATE(e.timestamp) ORDER BY date DESC";

    const rows = rawDb.prepare(sql).all(...params) as Array<{
      date: string;
      total_events: number;
      total_tool_calls: number;
      lines_added: number;
      lines_removed: number;
      sessions: number;
    }>;

    return rows.map((row) => ({
      date: row.date,
      totalEvents: row.total_events,
      totalToolCalls: row.total_tool_calls,
      linesAdded: row.lines_added,
      linesRemoved: row.lines_removed,
      sessions: row.sessions,
    }));
  } catch (error) {
    logError(
      error instanceof Error ? error : new Error(String(error)),
      { context: "queryDailySummary" }
    );
    return [];
  }
}

/**
 * Query tool usage breakdown.
 *
 * Groups events by tool name, summing line changes and call counts.
 * Only includes events with a tool_name.
 *
 * @param db - Analytics database instance
 * @param options - Optional session/date filters
 */
export function queryToolBreakdown(
  db: AnalyticsDB,
  options?: StatsOptions
): ToolBreakdownEntry[] {
  try {
    const rawDb = db.getDatabase();

    let sql = `
      SELECT
        e.tool_name as tool_name,
        COUNT(*) as count,
        COALESCE(SUM(e.lines_added), 0) as lines_added,
        COALESCE(SUM(e.lines_removed), 0) as lines_removed
      FROM events e
      WHERE e.tool_name IS NOT NULL
    `;

    const params: unknown[] = [];

    if (options?.sessionId) {
      sql += " AND e.session_id = ?";
      params.push(options.sessionId);
    }

    if (options?.from) {
      sql += " AND e.timestamp >= ?";
      params.push(options.from);
    }

    if (options?.to) {
      sql += " AND e.timestamp <= ?";
      params.push(options.to);
    }

    sql += " GROUP BY e.tool_name ORDER BY count DESC";

    const rows = rawDb.prepare(sql).all(...params) as Array<{
      tool_name: string;
      count: number;
      lines_added: number;
      lines_removed: number;
    }>;

    return rows.map((row) => ({
      toolName: row.tool_name,
      count: row.count,
      linesAdded: row.lines_added,
      linesRemoved: row.lines_removed,
    }));
  } catch (error) {
    logError(
      error instanceof Error ? error : new Error(String(error)),
      { context: "queryToolBreakdown" }
    );
    return [];
  }
}

/**
 * Query authorship summary.
 *
 * Returns total entries, total lines changed, weighted AI score,
 * and a breakdown by classification.
 *
 * @param db - Analytics database instance
 * @param options - Optional session/date filters
 */
export function queryAuthorshipSummary(
  db: AnalyticsDB,
  options?: StatsOptions
): AuthorshipSummary {
  const empty: AuthorshipSummary = {
    totalEntries: 0,
    totalLinesChanged: 0,
    weightedAIScore: 0,
    classificationBreakdown: {
      high_probability_ai: 0,
      likely_ai: 0,
      mixed_verified: 0,
      human_authored: 0,
    },
  };

  try {
    const rawDb = db.getDatabase();

    // Aggregate totals
    let totalSql = `
      SELECT
        COUNT(*) as total_entries,
        COALESCE(SUM(lines_changed), 0) as total_lines_changed,
        COALESCE(SUM(ai_score * lines_changed), 0) as weighted_sum
      FROM authorship_ledger
    `;

    const conditions: string[] = [];
    const params: unknown[] = [];

    if (options?.sessionId) {
      conditions.push("session_id = ?");
      params.push(options.sessionId);
    }

    if (options?.from) {
      conditions.push("timestamp >= ?");
      params.push(options.from);
    }

    if (options?.to) {
      conditions.push("timestamp <= ?");
      params.push(options.to);
    }

    if (conditions.length > 0) {
      totalSql += " WHERE " + conditions.join(" AND ");
    }

    const totals = rawDb.prepare(totalSql).get(...params) as {
      total_entries: number;
      total_lines_changed: number;
      weighted_sum: number;
    };

    if (totals.total_entries === 0) return empty;

    // Classification breakdown
    let breakdownSql = `
      SELECT classification, COUNT(*) as count
      FROM authorship_ledger
    `;

    if (conditions.length > 0) {
      breakdownSql += " WHERE " + conditions.join(" AND ");
    }

    breakdownSql += " GROUP BY classification";

    const breakdownRows = rawDb.prepare(breakdownSql).all(...params) as Array<{
      classification: string;
      count: number;
    }>;

    const breakdown: Record<AIClassification, number> = {
      high_probability_ai: 0,
      likely_ai: 0,
      mixed_verified: 0,
      human_authored: 0,
    };

    for (const row of breakdownRows) {
      const cls = row.classification as AIClassification;
      if (cls in breakdown) {
        breakdown[cls] = row.count;
      }
    }

    const weightedScore =
      totals.total_lines_changed > 0
        ? totals.weighted_sum / totals.total_lines_changed
        : 0;

    return {
      totalEntries: totals.total_entries,
      totalLinesChanged: totals.total_lines_changed,
      weightedAIScore: weightedScore,
      classificationBreakdown: breakdown,
    };
  } catch (error) {
    logError(
      error instanceof Error ? error : new Error(String(error)),
      { context: "queryAuthorshipSummary" }
    );
    return empty;
  }
}

/**
 * Master stats query combining daily, tool, and authorship summaries.
 *
 * @param db - Analytics database instance
 * @param options - Optional session/date/day filters
 */
export function queryStats(
  db: AnalyticsDB,
  options?: StatsOptions
): StatsResult {
  const days = options?.days ?? 7;

  return {
    daily: queryDailySummary(db, days, options),
    toolBreakdown: queryToolBreakdown(db, options),
    authorship: queryAuthorshipSummary(db, options),
  };
}
