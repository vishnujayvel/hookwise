/**
 * Tests for the memories feed producer.
 *
 * Covers:
 * - Finding sessions from the same month+day in previous years
 * - Finding sessions from exactly 7 days ago
 * - Finding sessions from exactly 30 days ago
 * - Missing database returns null (fail-open)
 * - No matching sessions returns null
 * - Memory item has correct shape (date, daysSince, label, toolCalls, filesEdited)
 * - Most interesting memory selection by tool call count
 *
 * GH#80: Memories/On This Day feed producer
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import Database from "better-sqlite3";
import {
  createMemoriesProducer,
  queryMemoriesData,
} from "../../../../src/core/feeds/producers/memories.js";
import type { MemoriesFeedConfig } from "../../../../src/core/types.js";

// --- Helpers ---

function makeConfig(overrides: Partial<MemoriesFeedConfig> = {}): MemoriesFeedConfig {
  return {
    enabled: true,
    intervalSeconds: 3600,
    dbPath: "", // will be overridden per-test
    ...overrides,
  };
}

/**
 * Create the minimal hookwise analytics schema in a test database.
 * Only creates the tables the memories producer needs to query.
 */
function createTestSchema(db: Database.Database): void {
  db.exec(`
    CREATE TABLE IF NOT EXISTS sessions (
      id TEXT PRIMARY KEY,
      started_at TEXT NOT NULL,
      ended_at TEXT,
      duration_seconds INTEGER,
      total_tool_calls INTEGER DEFAULT 0,
      file_edits_count INTEGER DEFAULT 0,
      ai_authored_lines INTEGER DEFAULT 0,
      human_verified_lines INTEGER DEFAULT 0,
      classification TEXT,
      estimated_cost_usd REAL DEFAULT 0.0
    );

    CREATE TABLE IF NOT EXISTS events (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      session_id TEXT NOT NULL,
      event_type TEXT NOT NULL,
      tool_name TEXT,
      timestamp TEXT NOT NULL,
      file_path TEXT,
      lines_added INTEGER DEFAULT 0,
      lines_removed INTEGER DEFAULT 0,
      ai_confidence_score REAL,
      FOREIGN KEY (session_id) REFERENCES sessions(id)
    );
  `);
}

/**
 * Insert a session with given date and stats.
 */
function insertSession(
  db: Database.Database,
  id: string,
  date: string,
  toolCalls: number = 0,
  fileEdits: number = 0,
): void {
  db.prepare(
    "INSERT INTO sessions (id, started_at, total_tool_calls, file_edits_count) VALUES (?, ?, ?, ?)",
  ).run(id, `${date}T10:00:00Z`, toolCalls, fileEdits);
}

/**
 * Get today's date as YYYY-MM-DD.
 */
function todayStr(): string {
  return new Date().toISOString().slice(0, 10);
}

/**
 * Get the same month+day as today but in a previous year.
 */
function sameMonthDayPreviousYear(yearsAgo: number): string {
  const now = new Date();
  const year = now.getUTCFullYear() - yearsAgo;
  const month = String(now.getUTCMonth() + 1).padStart(2, "0");
  const day = String(now.getUTCDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

/**
 * Get a date string N days ago.
 */
function daysAgoStr(n: number): string {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return d.toISOString().slice(0, 10);
}

// --- queryMemoriesData unit tests ---

describe("queryMemoriesData", () => {
  let tempRoot: string;
  let dbPath: string;
  let db: Database.Database;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-memories-"));
    dbPath = join(tempRoot, "analytics.db");
    db = new Database(dbPath);
    createTestSchema(db);
  });

  afterEach(() => {
    db.close();
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("returns null when database file does not exist", () => {
    const result = queryMemoriesData(join(tempRoot, "nonexistent.db"));
    expect(result).toBeNull();
  });

  it("returns null when no matching sessions found (empty DB)", () => {
    const result = queryMemoriesData(dbPath);
    expect(result).toBeNull();
  });

  it("returns null when only today's sessions exist (no nostalgia)", () => {
    insertSession(db, "session-today", todayStr(), 10, 5);
    const result = queryMemoriesData(dbPath);
    expect(result).toBeNull();
  });

  it("finds sessions from the same month+day in a previous year", () => {
    const lastYear = sameMonthDayPreviousYear(1);
    insertSession(db, "session-last-year", lastYear, 15, 8);

    const result = queryMemoriesData(dbPath);
    expect(result).not.toBeNull();
    expect(result!.hasMemories).toBe(true);
    expect(result!.memories.length).toBeGreaterThanOrEqual(1);

    const yearAgoMemory = result!.memories.find(
      (m) => m.date === lastYear,
    );
    expect(yearAgoMemory).toBeDefined();
    expect(yearAgoMemory!.toolCalls).toBe(15);
    expect(yearAgoMemory!.filesEdited).toBe(8);
    // daysSince should be approximately 365
    expect(yearAgoMemory!.daysSince).toBeGreaterThanOrEqual(364);
    expect(yearAgoMemory!.daysSince).toBeLessThanOrEqual(366);
  });

  it("finds sessions from exactly 7 days ago", () => {
    const sevenDaysAgo = daysAgoStr(7);
    insertSession(db, "session-7d", sevenDaysAgo, 20, 12);

    const result = queryMemoriesData(dbPath);
    expect(result).not.toBeNull();
    expect(result!.hasMemories).toBe(true);

    const weekAgoMemory = result!.memories.find(
      (m) => m.date === sevenDaysAgo,
    );
    expect(weekAgoMemory).toBeDefined();
    expect(weekAgoMemory!.daysSince).toBe(7);
    expect(weekAgoMemory!.label).toBe("1 week ago");
    expect(weekAgoMemory!.toolCalls).toBe(20);
  });

  it("finds sessions from exactly 30 days ago", () => {
    const thirtyDaysAgo = daysAgoStr(30);
    insertSession(db, "session-30d", thirtyDaysAgo, 25, 10);

    const result = queryMemoriesData(dbPath);
    expect(result).not.toBeNull();
    expect(result!.hasMemories).toBe(true);

    const monthAgoMemory = result!.memories.find(
      (m) => m.date === thirtyDaysAgo,
    );
    expect(monthAgoMemory).toBeDefined();
    expect(monthAgoMemory!.daysSince).toBe(30);
    expect(monthAgoMemory!.label).toBe("1 month ago");
    expect(monthAgoMemory!.toolCalls).toBe(25);
  });

  it("memory item has correct shape", () => {
    const sevenDaysAgo = daysAgoStr(7);
    insertSession(db, "session-shape", sevenDaysAgo, 5, 3);

    const result = queryMemoriesData(dbPath);
    expect(result).not.toBeNull();

    const memory = result!.memories.find((m) => m.date === sevenDaysAgo);
    expect(memory).toBeDefined();
    expect(memory).toEqual(
      expect.objectContaining({
        date: expect.any(String),
        daysSince: expect.any(Number),
        label: expect.any(String),
        toolCalls: expect.any(Number),
        filesEdited: expect.any(Number),
      }),
    );
  });

  it("aggregates tool calls across multiple sessions on the same date", () => {
    const sevenDaysAgo = daysAgoStr(7);
    insertSession(db, "session-7d-1", sevenDaysAgo, 10, 3);
    insertSession(db, "session-7d-2", sevenDaysAgo, 15, 7);

    const result = queryMemoriesData(dbPath);
    expect(result).not.toBeNull();

    const memory = result!.memories.find((m) => m.date === sevenDaysAgo);
    expect(memory).toBeDefined();
    expect(memory!.toolCalls).toBe(25); // 10 + 15
    expect(memory!.filesEdited).toBe(10); // 3 + 7
  });

  it("returns memories sorted by daysSince descending (oldest first)", () => {
    const sevenDaysAgo = daysAgoStr(7);
    const thirtyDaysAgo = daysAgoStr(30);
    insertSession(db, "session-7", sevenDaysAgo, 5, 2);
    insertSession(db, "session-30", thirtyDaysAgo, 8, 4);

    const result = queryMemoriesData(dbPath);
    expect(result).not.toBeNull();
    expect(result!.memories.length).toBeGreaterThanOrEqual(2);

    // 30 days should come before 7 days (descending order)
    const thirty = result!.memories.find((m) => m.date === thirtyDaysAgo);
    const seven = result!.memories.find((m) => m.date === sevenDaysAgo);
    expect(thirty).toBeDefined();
    expect(seven).toBeDefined();

    const thirtyIndex = result!.memories.indexOf(thirty!);
    const sevenIndex = result!.memories.indexOf(seven!);
    expect(thirtyIndex).toBeLessThan(sevenIndex);
  });

  it("returns null when 7d and 30d lookbacks have no sessions and no same-month-day matches", () => {
    // Freeze time so 7d/30d/same-month-day are deterministic and don't match 2025-01-15
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-02-22T12:00:00Z"));
    insertSession(db, "session-random", "2025-01-15", 10, 5);
    const result = queryMemoriesData(dbPath);
    expect(result).toBeNull();
    vi.useRealTimers();
  });
});

// --- createMemoriesProducer integration tests ---

describe("createMemoriesProducer", () => {
  let tempRoot: string;
  let dbPath: string;
  let db: Database.Database;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-memories-prod-"));
    dbPath = join(tempRoot, "analytics.db");
    db = new Database(dbPath);
    createTestSchema(db);
  });

  afterEach(() => {
    db.close();
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("returns memories data from a real database", async () => {
    const sevenDaysAgo = daysAgoStr(7);
    insertSession(db, "session-7d", sevenDaysAgo, 42, 18);

    const config = makeConfig({ dbPath });
    const producer = createMemoriesProducer(config);
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.hasMemories).toBe(true);
    expect(result!.memories).toBeDefined();
    expect(Array.isArray(result!.memories)).toBe(true);
  });

  it("returns null when database does not exist", async () => {
    const config = makeConfig({ dbPath: join(tempRoot, "nope.db") });
    const producer = createMemoriesProducer(config);
    const result = await producer();
    expect(result).toBeNull();
  });

  it("returns null when no matching sessions exist", async () => {
    // Empty DB — no sessions at all
    const config = makeConfig({ dbPath });
    const producer = createMemoriesProducer(config);
    const result = await producer();
    expect(result).toBeNull();
  });

  it("selects most interesting memory by tool call count for segment rendering", async () => {
    const sevenDaysAgo = daysAgoStr(7);
    const thirtyDaysAgo = daysAgoStr(30);
    // 7 days ago: many tool calls
    insertSession(db, "session-7d", sevenDaysAgo, 100, 30);
    // 30 days ago: fewer tool calls
    insertSession(db, "session-30d", thirtyDaysAgo, 10, 2);

    const config = makeConfig({ dbPath });
    const producer = createMemoriesProducer(config);
    const result = await producer();

    expect(result).not.toBeNull();
    const memories = result!.memories as Array<{
      toolCalls: number;
      date: string;
    }>;

    // Find the memory with the most tool calls
    const best = memories.reduce((a, b) =>
      b.toolCalls > a.toolCalls ? b : a,
    );
    expect(best.toolCalls).toBe(100);
    expect(best.date).toBe(sevenDaysAgo);
  });
});
