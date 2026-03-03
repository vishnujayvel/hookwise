/**
 * Practice feed producer: queries the practice-tracker SQLite database
 * for today's practice count, due SRS reviews, and last practice time.
 *
 * Reads from the practice-tracker database (default: ~/.practice-tracker/practice-tracker.db)
 * and writes to the "practice" cache key consumed by two segments:
 *   - practice: displays "TARGET todayTotal today"
 *   - practice_breadcrumb: displays "Last practice: Xm ago"
 *
 * Returns null when:
 *   - The database file does not exist
 *   - The database cannot be opened or queried
 *
 * GH#8: This producer was missing — the practice segment existed with no data source.
 */

import { existsSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import Database from "better-sqlite3";
import type { FeedProducer, PracticeFeedConfig } from "../../types.js";

export interface PracticeData {
  todayTotal: number;     // Number of practice reps completed today
  dueReviews: number;     // Number of SRS reviews due today or earlier
  last_at: string | null; // ISO 8601 timestamp of most recent practice rep
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
 * Query the practice-tracker database for practice metrics.
 *
 * Opens the database read-only and runs three queries:
 *   1. Count of today's practice reps
 *   2. Most recent practiced_at timestamp
 *   3. Count of SRS reviews due today or earlier
 *
 * Exported for direct testing — the producer factory wraps this.
 *
 * @param dbPath - Absolute path to the practice-tracker SQLite database
 * @returns PracticeData or null if the database is inaccessible
 */
export function queryPracticeData(dbPath: string): PracticeData | null {
  const resolvedPath = resolvePath(dbPath);

  if (!existsSync(resolvedPath)) return null;

  let db: InstanceType<typeof Database> | undefined;
  try {
    db = new Database(resolvedPath, { readonly: true });

    // UTC date: toISOString() returns UTC, and practice_reps.practiced_at
    // is stored as ISO 8601 with Z suffix (UTC), so UTC-based "today" is
    // the correct boundary for day-level comparisons.
    const today = new Date().toISOString().slice(0, 10); // YYYY-MM-DD (UTC)

    // 1. Count today's practice reps
    const todayCountRow = db
      .prepare(
        "SELECT COUNT(*) as cnt FROM practice_reps WHERE practiced_at >= ?",
      )
      .get(`${today}T00:00:00`) as { cnt: number };
    const todayTotal = todayCountRow?.cnt ?? 0;

    // 2. Most recent practiced_at
    const lastRow = db
      .prepare(
        "SELECT MAX(practiced_at) as last_at FROM practice_reps",
      )
      .get() as { last_at: string | null };
    const lastAt = lastRow?.last_at ?? null;

    // 3. Count due SRS reviews (next_review_date <= today)
    const dueRow = db
      .prepare(
        "SELECT COUNT(*) as cnt FROM question_srs_state WHERE next_review_date <= ?",
      )
      .get(today) as { cnt: number };
    const dueReviews = dueRow?.cnt ?? 0;

    return {
      todayTotal,
      dueReviews,
      last_at: lastAt,
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
 * Create a FeedProducer for the practice feed.
 *
 * @param config - Practice feed configuration including database path
 */
export function createPracticeProducer(
  config: PracticeFeedConfig,
): FeedProducer {
  return async (): Promise<Record<string, unknown> | null> => {
    const result = queryPracticeData(config.dbPath);
    if (!result) return null;
    return result as unknown as Record<string, unknown>;
  };
}
