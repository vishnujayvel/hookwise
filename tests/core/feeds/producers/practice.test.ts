/**
 * Tests for the practice feed producer.
 *
 * Covers:
 * - Today's practice count from practice_reps table
 * - Last practice timestamp (last_at)
 * - Due reviews count from question_srs_state
 * - Missing database returns null
 * - Empty database returns zero counts
 * - Database path resolution (~/ expansion)
 *
 * GH#8: Practice segment exists but had no producer
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import Database from "better-sqlite3";
import {
  createPracticeProducer,
  queryPracticeData,
} from "../../../../src/core/feeds/producers/practice.js";
import type { PracticeFeedConfig } from "../../../../src/core/types.js";

// --- Helpers ---

function makeConfig(overrides: Partial<PracticeFeedConfig> = {}): PracticeFeedConfig {
  return {
    enabled: true,
    intervalSeconds: 120,
    dbPath: "", // will be overridden per-test
    ...overrides,
  };
}

/**
 * Create a minimal practice-tracker schema in a test database.
 * Only creates the tables the producer needs to query.
 */
function createTestSchema(db: Database.Database): void {
  db.exec(`
    CREATE TABLE IF NOT EXISTS practice_reps (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      question_id INTEGER NOT NULL,
      rep_number INTEGER NOT NULL,
      practiced_at TEXT NOT NULL DEFAULT (datetime('now')),
      time_spent_minutes REAL NOT NULL CHECK(time_spent_minutes > 0),
      quality INTEGER NOT NULL CHECK(quality BETWEEN 0 AND 5),
      created_at TEXT NOT NULL DEFAULT (datetime('now'))
    );
  `);

  db.exec(`
    CREATE TABLE IF NOT EXISTS question_srs_state (
      question_id INTEGER PRIMARY KEY,
      next_review_date TEXT NOT NULL,
      last_quality INTEGER NOT NULL CHECK(last_quality BETWEEN 0 AND 5),
      updated_at TEXT NOT NULL DEFAULT (datetime('now'))
    );
  `);
}

// --- queryPracticeData unit tests ---

describe("queryPracticeData", () => {
  let tempRoot: string;
  let dbPath: string;
  let db: Database.Database;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-practice-"));
    dbPath = join(tempRoot, "practice-tracker.db");
    db = new Database(dbPath);
    createTestSchema(db);
  });

  afterEach(() => {
    db.close();
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("returns zero counts for an empty database", () => {
    const result = queryPracticeData(dbPath);
    expect(result).not.toBeNull();
    expect(result!.todayTotal).toBe(0);
    expect(result!.dueReviews).toBe(0);
    expect(result!.last_at).toBeNull();
  });

  it("counts today's practice reps correctly", () => {
    const today = new Date().toISOString().slice(0, 10); // YYYY-MM-DD
    const todayTs = `${today}T10:00:00Z`;
    const todayTs2 = `${today}T14:00:00Z`;

    db.prepare(
      "INSERT INTO practice_reps (question_id, rep_number, practiced_at, time_spent_minutes, quality) VALUES (?, ?, ?, ?, ?)",
    ).run(1, 1, todayTs, 15, 4);
    db.prepare(
      "INSERT INTO practice_reps (question_id, rep_number, practiced_at, time_spent_minutes, quality) VALUES (?, ?, ?, ?, ?)",
    ).run(2, 1, todayTs2, 20, 3);

    const result = queryPracticeData(dbPath);
    expect(result).not.toBeNull();
    expect(result!.todayTotal).toBe(2);
  });

  it("does not count yesterday's practice reps in todayTotal", () => {
    const yesterday = new Date(Date.now() - 86_400_000)
      .toISOString()
      .slice(0, 10);
    const yesterdayTs = `${yesterday}T10:00:00Z`;

    db.prepare(
      "INSERT INTO practice_reps (question_id, rep_number, practiced_at, time_spent_minutes, quality) VALUES (?, ?, ?, ?, ?)",
    ).run(1, 1, yesterdayTs, 15, 4);

    const result = queryPracticeData(dbPath);
    expect(result).not.toBeNull();
    expect(result!.todayTotal).toBe(0);
  });

  it("returns last_at as the most recent practiced_at timestamp", () => {
    const today = new Date().toISOString().slice(0, 10);
    const earlyTs = `${today}T08:00:00Z`;
    const lateTs = `${today}T16:00:00Z`;

    db.prepare(
      "INSERT INTO practice_reps (question_id, rep_number, practiced_at, time_spent_minutes, quality) VALUES (?, ?, ?, ?, ?)",
    ).run(1, 1, earlyTs, 15, 4);
    db.prepare(
      "INSERT INTO practice_reps (question_id, rep_number, practiced_at, time_spent_minutes, quality) VALUES (?, ?, ?, ?, ?)",
    ).run(2, 1, lateTs, 20, 3);

    const result = queryPracticeData(dbPath);
    expect(result).not.toBeNull();
    expect(result!.last_at).toBe(lateTs);
  });

  it("counts due reviews (next_review_date <= today)", () => {
    const today = new Date().toISOString().slice(0, 10);
    const yesterday = new Date(Date.now() - 86_400_000)
      .toISOString()
      .slice(0, 10);
    const tomorrow = new Date(Date.now() + 86_400_000)
      .toISOString()
      .slice(0, 10);

    // Due: today and yesterday
    db.prepare(
      "INSERT INTO question_srs_state (question_id, next_review_date, last_quality) VALUES (?, ?, ?)",
    ).run(1, today, 3);
    db.prepare(
      "INSERT INTO question_srs_state (question_id, next_review_date, last_quality) VALUES (?, ?, ?)",
    ).run(2, yesterday, 2);
    // Not due: tomorrow
    db.prepare(
      "INSERT INTO question_srs_state (question_id, next_review_date, last_quality) VALUES (?, ?, ?)",
    ).run(3, tomorrow, 4);

    const result = queryPracticeData(dbPath);
    expect(result).not.toBeNull();
    expect(result!.dueReviews).toBe(2);
  });

  it("returns null when database file does not exist", () => {
    const result = queryPracticeData(join(tempRoot, "nonexistent.db"));
    expect(result).toBeNull();
  });
});

// --- createPracticeProducer integration tests ---

describe("createPracticeProducer", () => {
  let tempRoot: string;
  let dbPath: string;
  let db: Database.Database;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-practice-prod-"));
    dbPath = join(tempRoot, "practice-tracker.db");
    db = new Database(dbPath);
    createTestSchema(db);
  });

  afterEach(() => {
    db.close();
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("returns practice data from a real database", async () => {
    const today = new Date().toISOString().slice(0, 10);
    const todayTs = `${today}T10:00:00Z`;

    db.prepare(
      "INSERT INTO practice_reps (question_id, rep_number, practiced_at, time_spent_minutes, quality) VALUES (?, ?, ?, ?, ?)",
    ).run(1, 1, todayTs, 15, 4);

    db.prepare(
      "INSERT INTO question_srs_state (question_id, next_review_date, last_quality) VALUES (?, ?, ?)",
    ).run(1, today, 3);
    db.prepare(
      "INSERT INTO question_srs_state (question_id, next_review_date, last_quality) VALUES (?, ?, ?)",
    ).run(2, today, 2);

    const config = makeConfig({ dbPath });
    const producer = createPracticeProducer(config);
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.todayTotal).toBe(1);
    expect(result!.dueReviews).toBe(2);
    expect(result!.last_at).toBe(todayTs);
  });

  it("returns null when database does not exist", async () => {
    const config = makeConfig({ dbPath: join(tempRoot, "nope.db") });
    const producer = createPracticeProducer(config);
    const result = await producer();
    expect(result).toBeNull();
  });

  it("returns data even when all counts are zero", async () => {
    const config = makeConfig({ dbPath });
    const producer = createPracticeProducer(config);
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.todayTotal).toBe(0);
    expect(result!.dueReviews).toBe(0);
    expect(result!.last_at).toBeNull();
  });
});
