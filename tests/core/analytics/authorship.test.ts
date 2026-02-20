/**
 * Tests for Authorship Ledger with AI Confidence Scoring.
 *
 * Verifies:
 * - Scoring for each tier (high_probability_ai, likely_ai, mixed_verified, human_authored)
 * - Edge cases: no prior prompt, zero lines
 * - Session AI ratio (weighted average)
 * - Entries stored in authorship_ledger table
 * - Prompt timestamp recording
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { AnalyticsDB } from "../../../src/core/analytics/db.js";
import { AuthorshipLedger } from "../../../src/core/analytics/authorship.js";

describe("AuthorshipLedger", () => {
  let tempDir: string;
  let db: AnalyticsDB;
  let ledger: AuthorshipLedger;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-authorship-test-"));
    db = new AnalyticsDB(join(tempDir, "test.db"));
    ledger = new AuthorshipLedger(db);
    AuthorshipLedger.clearPromptCache();

    // Create a test session
    db.getStatements().insertSession.run({
      id: "test-session",
      startedAt: "2026-02-20T10:00:00Z",
    });
  });

  afterEach(() => {
    db.close();
    rmSync(tempDir, { recursive: true, force: true });
  });

  describe("recordPromptTimestamp", () => {
    it("stores prompt timestamp for session", () => {
      // Should not throw
      expect(() => {
        ledger.recordPromptTimestamp(
          "test-session",
          "2026-02-20T10:00:00Z",
          150
        );
      }).not.toThrow();
    });
  });

  describe("computeAIScore", () => {
    describe("high_probability_ai tier", () => {
      it("scores 0.95 for fast response + many lines (< 10s, > 50 lines)", () => {
        ledger.recordPromptTimestamp(
          "test-session",
          "2026-02-20T10:00:00Z",
          100
        );

        const score = ledger.computeAIScore(
          "test-session",
          "Write",
          100, // > 50 lines
          "2026-02-20T10:00:05Z" // 5 seconds later (< 10s)
        );

        expect(score.score).toBe(0.95);
        expect(score.classification).toBe("high_probability_ai");
      });
    });

    describe("likely_ai tier", () => {
      it("scores 0.7 for fast response + moderate lines (< 10s, 10-50 lines)", () => {
        ledger.recordPromptTimestamp(
          "test-session",
          "2026-02-20T10:00:00Z",
          80
        );

        const score = ledger.computeAIScore(
          "test-session",
          "Write",
          25, // 10-50 lines
          "2026-02-20T10:00:05Z" // 5 seconds (< 10s)
        );

        expect(score.score).toBe(0.7);
        expect(score.classification).toBe("likely_ai");
      });

      it("scores 0.7 at exactly 10 lines", () => {
        ledger.recordPromptTimestamp(
          "test-session",
          "2026-02-20T10:00:00Z",
          50
        );

        const score = ledger.computeAIScore(
          "test-session",
          "Write",
          10, // exactly 10 lines
          "2026-02-20T10:00:03Z" // 3 seconds
        );

        expect(score.score).toBe(0.7);
        expect(score.classification).toBe("likely_ai");
      });
    });

    describe("mixed_verified tier", () => {
      it("scores 0.3 for slow response (> 30s)", () => {
        ledger.recordPromptTimestamp(
          "test-session",
          "2026-02-20T10:00:00Z",
          100
        );

        const score = ledger.computeAIScore(
          "test-session",
          "Write",
          100,
          "2026-02-20T10:00:35Z" // 35 seconds (> 30s)
        );

        expect(score.score).toBe(0.3);
        expect(score.classification).toBe("mixed_verified");
      });

      it("scores 0.3 for few lines (< 5)", () => {
        ledger.recordPromptTimestamp(
          "test-session",
          "2026-02-20T10:00:00Z",
          20
        );

        const score = ledger.computeAIScore(
          "test-session",
          "Write",
          3, // < 5 lines
          "2026-02-20T10:00:15Z" // 15 seconds (moderate time)
        );

        expect(score.score).toBe(0.3);
        expect(score.classification).toBe("mixed_verified");
      });
    });

    describe("human_authored tier", () => {
      it("scores 0.1 for Edit with < 3 lines", () => {
        ledger.recordPromptTimestamp(
          "test-session",
          "2026-02-20T10:00:00Z",
          50
        );

        const score = ledger.computeAIScore(
          "test-session",
          "Edit",
          2, // < 3 lines
          "2026-02-20T10:00:02Z" // fast response, but Edit + few lines
        );

        expect(score.score).toBe(0.1);
        expect(score.classification).toBe("human_authored");
      });

      it("scores 0.1 for Edit with 0 lines", () => {
        const score = ledger.computeAIScore(
          "test-session",
          "Edit",
          0,
          "2026-02-20T10:00:02Z"
        );

        expect(score.score).toBe(0.1);
        expect(score.classification).toBe("human_authored");
      });

      it("does NOT apply human_authored for Edit with >= 3 lines", () => {
        ledger.recordPromptTimestamp(
          "test-session",
          "2026-02-20T10:00:00Z",
          50
        );

        const score = ledger.computeAIScore(
          "test-session",
          "Edit",
          3, // exactly 3 lines — not < 3
          "2026-02-20T10:00:15Z"
        );

        // Falls through to mixed_verified due to < 5 lines
        expect(score.classification).not.toBe("human_authored");
      });
    });

    describe("edge cases", () => {
      it("defaults to mixed_verified when no prior prompt recorded", () => {
        // No recordPromptTimestamp called
        const score = ledger.computeAIScore(
          "test-session",
          "Write",
          50,
          "2026-02-20T10:00:00Z"
        );

        expect(score.score).toBe(0.3);
        expect(score.classification).toBe("mixed_verified");
      });

      it("handles zero lines changed for non-Edit tools", () => {
        ledger.recordPromptTimestamp(
          "test-session",
          "2026-02-20T10:00:00Z",
          50
        );

        const score = ledger.computeAIScore(
          "test-session",
          "Bash",
          0,
          "2026-02-20T10:00:05Z"
        );

        // 0 lines < 5 -> mixed_verified
        expect(score.score).toBe(0.3);
        expect(score.classification).toBe("mixed_verified");
      });

      it("handles moderate time + moderate lines as likely_ai", () => {
        ledger.recordPromptTimestamp(
          "test-session",
          "2026-02-20T10:00:00Z",
          100
        );

        const score = ledger.computeAIScore(
          "test-session",
          "Write",
          20, // >= 5 lines (not few)
          "2026-02-20T10:00:15Z" // 15 seconds (not < 10, not > 30)
        );

        expect(score.score).toBe(0.6);
        expect(score.classification).toBe("likely_ai");
      });
    });
  });

  describe("recordChange", () => {
    it("inserts entry into authorship_ledger table", () => {
      ledger.recordPromptTimestamp(
        "test-session",
        "2026-02-20T10:00:00Z",
        100
      );

      const entry = ledger.recordChange(
        "test-session",
        "Write",
        60,
        "2026-02-20T10:00:05Z",
        "/app/src/index.ts"
      );

      // Verify entry returned
      expect(entry.sessionId).toBe("test-session");
      expect(entry.filePath).toBe("/app/src/index.ts");
      expect(entry.toolName).toBe("Write");
      expect(entry.linesChanged).toBe(60);
      expect(entry.aiScore).toBe(0.95);
      expect(entry.classification).toBe("high_probability_ai");

      // Verify stored in DB
      const rows = db
        .getDatabase()
        .prepare("SELECT * FROM authorship_ledger WHERE session_id = ?")
        .all("test-session") as Array<Record<string, unknown>>;
      expect(rows).toHaveLength(1);
      expect(rows[0].file_path).toBe("/app/src/index.ts");
      expect(rows[0].ai_score).toBe(0.95);
    });

    it("stores multiple entries for a session", () => {
      ledger.recordPromptTimestamp(
        "test-session",
        "2026-02-20T10:00:00Z",
        100
      );

      ledger.recordChange(
        "test-session",
        "Write",
        100,
        "2026-02-20T10:00:05Z",
        "/app/src/a.ts"
      );
      ledger.recordChange(
        "test-session",
        "Edit",
        2,
        "2026-02-20T10:00:10Z",
        "/app/src/b.ts"
      );

      const rows = db
        .getDatabase()
        .prepare("SELECT * FROM authorship_ledger WHERE session_id = ?")
        .all("test-session") as Array<Record<string, unknown>>;
      expect(rows).toHaveLength(2);
    });
  });

  describe("getSessionAIRatio", () => {
    it("returns 0 for session with no entries", () => {
      const ratio = ledger.getSessionAIRatio("test-session");
      expect(ratio).toBe(0);
    });

    it("returns weighted average AI score", () => {
      ledger.recordPromptTimestamp(
        "test-session",
        "2026-02-20T10:00:00Z",
        100
      );

      // Entry 1: 100 lines, score 0.95
      ledger.recordChange(
        "test-session",
        "Write",
        100,
        "2026-02-20T10:00:05Z",
        "/app/src/a.ts"
      );

      // Entry 2: 2 lines, score 0.1 (Edit + < 3 lines)
      ledger.recordChange(
        "test-session",
        "Edit",
        2,
        "2026-02-20T10:00:10Z",
        "/app/src/b.ts"
      );

      const ratio = ledger.getSessionAIRatio("test-session");

      // Weighted: (0.95 * 100 + 0.1 * 2) / (100 + 2) = (95 + 0.2) / 102 ≈ 0.9333
      expect(ratio).toBeCloseTo(0.9333, 2);
    });

    it("returns exact score for single entry", () => {
      ledger.recordPromptTimestamp(
        "test-session",
        "2026-02-20T10:00:00Z",
        100
      );

      ledger.recordChange(
        "test-session",
        "Write",
        60,
        "2026-02-20T10:00:05Z",
        "/app/src/a.ts"
      );

      const ratio = ledger.getSessionAIRatio("test-session");
      expect(ratio).toBe(0.95);
    });

    it("returns 0 for non-existent session", () => {
      const ratio = ledger.getSessionAIRatio("nonexistent-session");
      expect(ratio).toBe(0);
    });
  });
});
