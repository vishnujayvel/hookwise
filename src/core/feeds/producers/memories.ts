/**
 * Memories feed producer: queries the hookwise analytics SQLite database
 * for past sessions on the same calendar date in previous years, and also
 * for notable sessions from 7 and 30 days ago.
 *
 * Shows "On This Day" nostalgia items in the status line, inspired by
 * photo apps' memories features.
 *
 * Reads from the analytics database (default: ~/.hookwise/analytics.db)
 * and writes to the "memories" cache key consumed by the memories segment.
 *
 * Returns null when:
 *   - The database file does not exist
 *   - The database cannot be opened or queried
 *   - No matching sessions are found
 *
 * ARCH-3: Fail-open — returns null on any error, never throws.
 */

import { existsSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import Database from "better-sqlite3";
import type { FeedProducer, MemoriesFeedConfig } from "../../types.js";

export interface MemoryItem {
  date: string;         // YYYY-MM-DD of the remembered session
  daysSince: number;    // Days between that date and today
  label: string;        // Human-readable label, e.g. "1 year ago" or "7 days ago"
  toolCalls: number;    // Total tool calls in sessions on that date
  filesEdited: number;  // Total files edited in sessions on that date
}

export interface MemoriesData {
  memories: MemoryItem[];
  hasMemories: boolean;
}

/**
 * Resolve a path that may start with ~/ to an absolute path.
 */
function resolvePath(p: string): string {
  if (p.startsWith("~/")) {
    return join(homedir(), p.slice(2));
  }
  return p;
}

/**
 * Format a human-readable label for a memory based on the number of days since.
 */
function formatLabel(daysSince: number): string {
  if (daysSince === 7) return "1 week ago";
  if (daysSince === 30) return "1 month ago";

  const years = Math.round(daysSince / 365);
  if (years === 1) return "1 year ago";
  if (years > 1) return `${years} years ago`;

  const months = Math.round(daysSince / 30);
  if (months === 1) return "1 month ago";
  if (months > 1) return `${months} months ago`;

  return `${daysSince} days ago`;
}

/**
 * Query the analytics database for memory items.
 *
 * Looks for sessions from:
 *   1. The same month+day in previous years
 *   2. Exactly 7 days ago
 *   3. Exactly 30 days ago
 *
 * For each matching date, aggregates tool call and file edit counts
 * across all sessions on that date.
 *
 * Exported for direct testing — the producer factory wraps this.
 *
 * @param dbPath - Absolute path to the hookwise analytics SQLite database
 * @returns MemoriesData or null if the database is inaccessible or no memories found
 */
export function queryMemoriesData(dbPath: string): MemoriesData | null {
  const resolvedPath = resolvePath(dbPath);

  if (!existsSync(resolvedPath)) return null;

  let db: InstanceType<typeof Database> | undefined;
  try {
    db = new Database(resolvedPath, { readonly: true });

    const now = new Date();
    const todayStr = now.toISOString().slice(0, 10); // YYYY-MM-DD
    const month = String(now.getMonth() + 1).padStart(2, "0");
    const day = String(now.getDate()).padStart(2, "0");
    const monthDay = `-${month}-${day}`; // e.g. "-03-03"

    // Collect all matching dates
    const targetDates = new Set<string>();

    // 1. Same month+day in previous years
    //    Query sessions where started_at matches the current month-day but not today's date
    const sameMonthDayRows = db
      .prepare(
        `SELECT DISTINCT substr(started_at, 1, 10) as session_date
         FROM sessions
         WHERE substr(started_at, 5, 6) = ?
           AND substr(started_at, 1, 10) != ?`,
      )
      .all(monthDay, todayStr) as Array<{ session_date: string }>;

    for (const row of sameMonthDayRows) {
      targetDates.add(row.session_date);
    }

    // 2. Exactly 7 days ago
    const sevenDaysAgo = new Date(now);
    sevenDaysAgo.setDate(sevenDaysAgo.getDate() - 7);
    const sevenDaysAgoStr = sevenDaysAgo.toISOString().slice(0, 10);
    targetDates.add(sevenDaysAgoStr);

    // 3. Exactly 30 days ago
    const thirtyDaysAgo = new Date(now);
    thirtyDaysAgo.setDate(thirtyDaysAgo.getDate() - 30);
    const thirtyDaysAgoStr = thirtyDaysAgo.toISOString().slice(0, 10);
    targetDates.add(thirtyDaysAgoStr);

    // For each target date, aggregate sessions
    const memories: MemoryItem[] = [];

    for (const dateStr of targetDates) {
      const sessionAgg = db
        .prepare(
          `SELECT
             COUNT(*) as session_count,
             COALESCE(SUM(total_tool_calls), 0) as total_tool_calls,
             COALESCE(SUM(file_edits_count), 0) as total_files_edited
           FROM sessions
           WHERE substr(started_at, 1, 10) = ?`,
        )
        .get(dateStr) as {
          session_count: number;
          total_tool_calls: number;
          total_files_edited: number;
        };

      if (!sessionAgg || sessionAgg.session_count === 0) continue;

      // Use UTC date-only math to avoid timezone rounding issues
      const todayUtc = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate());
      const [y, m, d] = dateStr.split("-").map(Number);
      const targetUtc = Date.UTC(y, m - 1, d);
      const daysSince = Math.round((todayUtc - targetUtc) / (1000 * 60 * 60 * 24));

      if (daysSince <= 0) continue;

      memories.push({
        date: dateStr,
        daysSince,
        label: formatLabel(daysSince),
        toolCalls: sessionAgg.total_tool_calls,
        filesEdited: sessionAgg.total_files_edited,
      });
    }

    if (memories.length === 0) return null;

    // Sort by daysSince descending (oldest memory first, most nostalgic)
    memories.sort((a, b) => b.daysSince - a.daysSince);

    return {
      memories,
      hasMemories: true,
    };
  } catch {
    // Database is corrupt, schema mismatch, or other error — fail-open
    return null;
  } finally {
    try {
      db?.close();
    } catch {
      // Ignore close errors
    }
  }
}

/**
 * Create a FeedProducer for the memories feed.
 *
 * @param config - Memories feed configuration including database path
 */
export function createMemoriesProducer(
  config: MemoriesFeedConfig,
): FeedProducer {
  return async (): Promise<Record<string, unknown> | null> => {
    const result = queryMemoriesData(config.dbPath);
    if (!result) return null;
    return result as unknown as Record<string, unknown>;
  };
}
