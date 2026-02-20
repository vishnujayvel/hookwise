/**
 * Tests for stats query interface.
 *
 * Verifies:
 * - Correct aggregates for seeded data
 * - Empty DB returns zeros
 * - Date filtering
 * - JSON shape of all query results
 * - Daily summary, tool breakdown, authorship summary, combined stats
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { AnalyticsDB } from "../../../src/core/analytics/db.js";
import {
  queryStats,
  queryDailySummary,
  queryToolBreakdown,
  queryAuthorshipSummary,
} from "../../../src/core/analytics/stats.js";

describe("Stats queries", () => {
  let tempDir: string;
  let db: AnalyticsDB;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-stats-test-"));
    db = new AnalyticsDB(join(tempDir, "test.db"));
  });

  afterEach(() => {
    db.close();
    rmSync(tempDir, { recursive: true, force: true });
  });

  /**
   * Seed the database with test data for aggregate queries.
   */
  function seedTestData() {
    const rawDb = db.getDatabase();

    // Insert sessions
    rawDb
      .prepare("INSERT INTO sessions (id, started_at) VALUES (?, ?)")
      .run("sess-a", "2026-02-20T09:00:00Z");
    rawDb
      .prepare("INSERT INTO sessions (id, started_at) VALUES (?, ?)")
      .run("sess-b", "2026-02-19T09:00:00Z");

    // Insert events for sess-a (Feb 20)
    rawDb
      .prepare(
        "INSERT INTO events (session_id, event_type, tool_name, timestamp, lines_added, lines_removed) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run("sess-a", "PostToolUse", "Bash", "2026-02-20T09:00:01Z", 10, 0);
    rawDb
      .prepare(
        "INSERT INTO events (session_id, event_type, tool_name, timestamp, lines_added, lines_removed) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run("sess-a", "PostToolUse", "Write", "2026-02-20T09:00:02Z", 50, 0);
    rawDb
      .prepare(
        "INSERT INTO events (session_id, event_type, tool_name, timestamp, lines_added, lines_removed) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run("sess-a", "PostToolUse", "Bash", "2026-02-20T09:00:03Z", 0, 5);
    rawDb
      .prepare(
        "INSERT INTO events (session_id, event_type, tool_name, timestamp, lines_added, lines_removed) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run("sess-a", "UserPromptSubmit", null, "2026-02-20T09:00:00Z", 0, 0);

    // Insert events for sess-b (Feb 19)
    rawDb
      .prepare(
        "INSERT INTO events (session_id, event_type, tool_name, timestamp, lines_added, lines_removed) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run("sess-b", "PostToolUse", "Edit", "2026-02-19T10:00:00Z", 3, 2);
    rawDb
      .prepare(
        "INSERT INTO events (session_id, event_type, tool_name, timestamp, lines_added, lines_removed) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run("sess-b", "PostToolUse", "Write", "2026-02-19T10:00:01Z", 80, 0);

    // Insert authorship entries
    rawDb
      .prepare(
        "INSERT INTO authorship_ledger (session_id, file_path, tool_name, lines_changed, ai_score, classification, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?)"
      )
      .run(
        "sess-a",
        "/app/src/a.ts",
        "Write",
        50,
        0.95,
        "high_probability_ai",
        "2026-02-20T09:00:02Z"
      );
    rawDb
      .prepare(
        "INSERT INTO authorship_ledger (session_id, file_path, tool_name, lines_changed, ai_score, classification, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?)"
      )
      .run(
        "sess-b",
        "/app/src/b.ts",
        "Edit",
        2,
        0.1,
        "human_authored",
        "2026-02-19T10:00:00Z"
      );
    rawDb
      .prepare(
        "INSERT INTO authorship_ledger (session_id, file_path, tool_name, lines_changed, ai_score, classification, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?)"
      )
      .run(
        "sess-b",
        "/app/src/c.ts",
        "Write",
        80,
        0.7,
        "likely_ai",
        "2026-02-19T10:00:01Z"
      );
  }

  describe("queryDailySummary", () => {
    it("returns empty array for empty database", () => {
      const result = queryDailySummary(db, 7);
      expect(result).toEqual([]);
    });

    it("aggregates events by date", () => {
      seedTestData();

      const result = queryDailySummary(db, 7, {
        from: "2026-02-18T00:00:00Z",
        to: "2026-02-21T00:00:00Z",
      });

      expect(result).toHaveLength(2);

      // Feb 20 (most recent, first due to DESC)
      const feb20 = result.find((d) => d.date === "2026-02-20");
      expect(feb20).toBeDefined();
      expect(feb20!.totalEvents).toBe(4); // 3 PostToolUse + 1 UserPromptSubmit
      expect(feb20!.totalToolCalls).toBe(3); // 3 events with tool_name
      expect(feb20!.linesAdded).toBe(60); // 10 + 50 + 0
      expect(feb20!.linesRemoved).toBe(5);
      expect(feb20!.sessions).toBe(1);

      // Feb 19
      const feb19 = result.find((d) => d.date === "2026-02-19");
      expect(feb19).toBeDefined();
      expect(feb19!.totalEvents).toBe(2);
      expect(feb19!.totalToolCalls).toBe(2);
      expect(feb19!.linesAdded).toBe(83); // 3 + 80
      expect(feb19!.linesRemoved).toBe(2);
    });

    it("filters by session ID", () => {
      seedTestData();

      const result = queryDailySummary(db, 7, {
        sessionId: "sess-a",
        from: "2026-02-18T00:00:00Z",
      });

      expect(result).toHaveLength(1);
      expect(result[0].date).toBe("2026-02-20");
    });

    it("has correct DailySummary shape", () => {
      seedTestData();

      const result = queryDailySummary(db, 7, {
        from: "2026-02-18T00:00:00Z",
      });

      for (const day of result) {
        expect(day).toHaveProperty("date");
        expect(day).toHaveProperty("totalEvents");
        expect(day).toHaveProperty("totalToolCalls");
        expect(day).toHaveProperty("linesAdded");
        expect(day).toHaveProperty("linesRemoved");
        expect(day).toHaveProperty("sessions");
        expect(typeof day.date).toBe("string");
        expect(typeof day.totalEvents).toBe("number");
      }
    });
  });

  describe("queryToolBreakdown", () => {
    it("returns empty array for empty database", () => {
      const result = queryToolBreakdown(db);
      expect(result).toEqual([]);
    });

    it("groups by tool name and sums counts", () => {
      seedTestData();

      const result = queryToolBreakdown(db);

      // Should have 3 distinct tools: Bash, Write, Edit
      expect(result).toHaveLength(3);

      const bash = result.find((t) => t.toolName === "Bash");
      expect(bash).toBeDefined();
      expect(bash!.count).toBe(2); // 2 Bash events
      expect(bash!.linesAdded).toBe(10); // 10 + 0
      expect(bash!.linesRemoved).toBe(5); // 0 + 5

      const write = result.find((t) => t.toolName === "Write");
      expect(write).toBeDefined();
      expect(write!.count).toBe(2); // 2 Write events
      expect(write!.linesAdded).toBe(130); // 50 + 80

      const edit = result.find((t) => t.toolName === "Edit");
      expect(edit).toBeDefined();
      expect(edit!.count).toBe(1);
    });

    it("filters by session ID", () => {
      seedTestData();

      const result = queryToolBreakdown(db, { sessionId: "sess-a" });

      // sess-a has Bash(2) + Write(1)
      expect(result).toHaveLength(2);
      const toolNames = result.map((t) => t.toolName);
      expect(toolNames).toContain("Bash");
      expect(toolNames).toContain("Write");
    });

    it("has correct ToolBreakdownEntry shape", () => {
      seedTestData();

      const result = queryToolBreakdown(db);

      for (const entry of result) {
        expect(entry).toHaveProperty("toolName");
        expect(entry).toHaveProperty("count");
        expect(entry).toHaveProperty("linesAdded");
        expect(entry).toHaveProperty("linesRemoved");
        expect(typeof entry.toolName).toBe("string");
        expect(typeof entry.count).toBe("number");
      }
    });
  });

  describe("queryAuthorshipSummary", () => {
    it("returns zeros for empty database", () => {
      const result = queryAuthorshipSummary(db);
      expect(result.totalEntries).toBe(0);
      expect(result.totalLinesChanged).toBe(0);
      expect(result.weightedAIScore).toBe(0);
      expect(result.classificationBreakdown.high_probability_ai).toBe(0);
      expect(result.classificationBreakdown.likely_ai).toBe(0);
      expect(result.classificationBreakdown.mixed_verified).toBe(0);
      expect(result.classificationBreakdown.human_authored).toBe(0);
    });

    it("computes correct totals", () => {
      seedTestData();

      const result = queryAuthorshipSummary(db);

      expect(result.totalEntries).toBe(3);
      expect(result.totalLinesChanged).toBe(132); // 50 + 2 + 80
    });

    it("computes correct weighted AI score", () => {
      seedTestData();

      const result = queryAuthorshipSummary(db);

      // Weighted: (0.95 * 50 + 0.1 * 2 + 0.7 * 80) / (50 + 2 + 80)
      // = (47.5 + 0.2 + 56) / 132 = 103.7 / 132 ≈ 0.7856
      expect(result.weightedAIScore).toBeCloseTo(0.7856, 2);
    });

    it("computes correct classification breakdown", () => {
      seedTestData();

      const result = queryAuthorshipSummary(db);

      expect(result.classificationBreakdown.high_probability_ai).toBe(1);
      expect(result.classificationBreakdown.human_authored).toBe(1);
      expect(result.classificationBreakdown.likely_ai).toBe(1);
      expect(result.classificationBreakdown.mixed_verified).toBe(0);
    });

    it("filters by session ID", () => {
      seedTestData();

      const result = queryAuthorshipSummary(db, { sessionId: "sess-a" });

      expect(result.totalEntries).toBe(1);
      expect(result.totalLinesChanged).toBe(50);
      expect(result.weightedAIScore).toBeCloseTo(0.95, 2);
    });

    it("has correct AuthorshipSummary shape", () => {
      seedTestData();

      const result = queryAuthorshipSummary(db);

      expect(result).toHaveProperty("totalEntries");
      expect(result).toHaveProperty("totalLinesChanged");
      expect(result).toHaveProperty("weightedAIScore");
      expect(result).toHaveProperty("classificationBreakdown");
      expect(result.classificationBreakdown).toHaveProperty("high_probability_ai");
      expect(result.classificationBreakdown).toHaveProperty("likely_ai");
      expect(result.classificationBreakdown).toHaveProperty("mixed_verified");
      expect(result.classificationBreakdown).toHaveProperty("human_authored");
    });
  });

  describe("queryStats (combined)", () => {
    it("returns all three summaries for empty DB", () => {
      const result = queryStats(db);

      expect(result.daily).toEqual([]);
      expect(result.toolBreakdown).toEqual([]);
      expect(result.authorship.totalEntries).toBe(0);
    });

    it("returns combined results for seeded data", () => {
      seedTestData();

      const result = queryStats(db, {
        from: "2026-02-18T00:00:00Z",
        to: "2026-02-21T00:00:00Z",
      });

      expect(result.daily.length).toBeGreaterThan(0);
      expect(result.toolBreakdown.length).toBeGreaterThan(0);
      expect(result.authorship.totalEntries).toBeGreaterThan(0);
    });

    it("has correct StatsResult shape", () => {
      seedTestData();

      const result = queryStats(db, {
        from: "2026-02-18T00:00:00Z",
      });

      expect(result).toHaveProperty("daily");
      expect(result).toHaveProperty("toolBreakdown");
      expect(result).toHaveProperty("authorship");
      expect(Array.isArray(result.daily)).toBe(true);
      expect(Array.isArray(result.toolBreakdown)).toBe(true);
      expect(typeof result.authorship).toBe("object");
    });

    it("respects days parameter", () => {
      seedTestData();

      // Query with from/to that captures all data
      const result = queryStats(db, {
        from: "2026-02-18T00:00:00Z",
        to: "2026-02-21T00:00:00Z",
      });

      expect(result.daily).toHaveLength(2);
    });

    it("filters by session ID across all queries", () => {
      seedTestData();

      const result = queryStats(db, {
        sessionId: "sess-b",
        from: "2026-02-18T00:00:00Z",
      });

      // Daily should only have Feb 19
      expect(result.daily).toHaveLength(1);
      expect(result.daily[0].date).toBe("2026-02-19");

      // Tool breakdown should have Edit + Write only
      const toolNames = result.toolBreakdown.map((t) => t.toolName);
      expect(toolNames).not.toContain("Bash");

      // Authorship should have 2 entries
      expect(result.authorship.totalEntries).toBe(2);
    });
  });

  describe("date filtering", () => {
    it("filters daily summary by from date", () => {
      seedTestData();

      const result = queryDailySummary(db, 7, {
        from: "2026-02-20T00:00:00Z",
      });

      // Should only include Feb 20
      expect(result).toHaveLength(1);
      expect(result[0].date).toBe("2026-02-20");
    });

    it("filters daily summary by to date", () => {
      seedTestData();

      const result = queryDailySummary(db, 7, {
        from: "2026-02-18T00:00:00Z",
        to: "2026-02-19T23:59:59Z",
      });

      // Should only include Feb 19
      expect(result).toHaveLength(1);
      expect(result[0].date).toBe("2026-02-19");
    });

    it("filters tool breakdown by date range", () => {
      seedTestData();

      const result = queryToolBreakdown(db, {
        from: "2026-02-20T00:00:00Z",
        to: "2026-02-20T23:59:59Z",
      });

      // Feb 20 only has Bash + Write from sess-a
      const toolNames = result.map((t) => t.toolName);
      expect(toolNames).toContain("Bash");
      expect(toolNames).toContain("Write");
      expect(toolNames).not.toContain("Edit");
    });

    it("filters authorship by date range", () => {
      seedTestData();

      const result = queryAuthorshipSummary(db, {
        from: "2026-02-20T00:00:00Z",
      });

      expect(result.totalEntries).toBe(1);
      expect(result.classificationBreakdown.high_probability_ai).toBe(1);
    });
  });
});
