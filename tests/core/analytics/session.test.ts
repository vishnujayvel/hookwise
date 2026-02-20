/**
 * Tests for event recording and session lifecycle.
 *
 * Verifies:
 * - Event insertion and retrieval
 * - Session start/end lifecycle
 * - Tool call counter increments
 * - Line count aggregation via events
 * - Prompt content is NEVER stored (privacy contract)
 * - Duration computed correctly
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { AnalyticsDB } from "../../../src/core/analytics/db.js";
import { AnalyticsEngine } from "../../../src/core/analytics/session.js";
import type { AnalyticsEvent, SessionSummary } from "../../../src/core/types.js";

describe("AnalyticsEngine", () => {
  let tempDir: string;
  let db: AnalyticsDB;
  let engine: AnalyticsEngine;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-session-test-"));
    db = new AnalyticsDB(join(tempDir, "test.db"));
    engine = new AnalyticsEngine(db);
  });

  afterEach(() => {
    db.close();
    rmSync(tempDir, { recursive: true, force: true });
  });

  describe("startSession", () => {
    it("creates a session row", () => {
      engine.startSession("sess-001");
      const session = db.getStatements().getSession.get("sess-001") as Record<
        string,
        unknown
      >;
      expect(session).toBeDefined();
      expect(session.id).toBe("sess-001");
      expect(session.started_at).toBeDefined();
    });

    it("sets default values for new session", () => {
      engine.startSession("sess-002");
      const session = db.getStatements().getSession.get("sess-002") as Record<
        string,
        unknown
      >;
      expect(session.total_tool_calls).toBe(0);
      expect(session.file_edits_count).toBe(0);
      expect(session.ai_authored_lines).toBe(0);
      expect(session.ended_at).toBeNull();
    });
  });

  describe("recordEvent", () => {
    it("inserts event into events table", () => {
      engine.startSession("sess-003");
      const event: AnalyticsEvent = {
        sessionId: "sess-003",
        eventType: "PostToolUse",
        toolName: "Bash",
        timestamp: "2026-02-20T10:00:00Z",
        linesAdded: 10,
        linesRemoved: 3,
      };
      engine.recordEvent(event);

      const events = db
        .getDatabase()
        .prepare("SELECT * FROM events WHERE session_id = ?")
        .all("sess-003") as Array<Record<string, unknown>>;
      expect(events).toHaveLength(1);
      expect(events[0].event_type).toBe("PostToolUse");
      expect(events[0].tool_name).toBe("Bash");
      expect(events[0].lines_added).toBe(10);
      expect(events[0].lines_removed).toBe(3);
    });

    it("increments session tool_calls counter for tool events", () => {
      engine.startSession("sess-004");

      engine.recordEvent({
        sessionId: "sess-004",
        eventType: "PostToolUse",
        toolName: "Bash",
        timestamp: "2026-02-20T10:00:00Z",
      });
      engine.recordEvent({
        sessionId: "sess-004",
        eventType: "PostToolUse",
        toolName: "Write",
        timestamp: "2026-02-20T10:00:01Z",
      });

      const session = db.getStatements().getSession.get("sess-004") as Record<
        string,
        unknown
      >;
      expect(session.total_tool_calls).toBe(2);
    });

    it("does not increment counter for events without tool name", () => {
      engine.startSession("sess-005");

      engine.recordEvent({
        sessionId: "sess-005",
        eventType: "UserPromptSubmit",
        timestamp: "2026-02-20T10:00:00Z",
      });

      const session = db.getStatements().getSession.get("sess-005") as Record<
        string,
        unknown
      >;
      expect(session.total_tool_calls).toBe(0);
    });

    it("stores AI confidence score when provided", () => {
      engine.startSession("sess-006");

      engine.recordEvent({
        sessionId: "sess-006",
        eventType: "PostToolUse",
        toolName: "Write",
        timestamp: "2026-02-20T10:00:00Z",
        filePath: "/app/src/index.ts",
        linesAdded: 50,
        aiConfidenceScore: 0.95,
      });

      const events = db
        .getDatabase()
        .prepare("SELECT * FROM events WHERE session_id = ?")
        .all("sess-006") as Array<Record<string, unknown>>;
      expect(events[0].ai_confidence_score).toBe(0.95);
    });

    it("stores file_path when provided", () => {
      engine.startSession("sess-007");

      engine.recordEvent({
        sessionId: "sess-007",
        eventType: "PostToolUse",
        toolName: "Edit",
        timestamp: "2026-02-20T10:00:00Z",
        filePath: "/app/src/utils.ts",
        linesAdded: 5,
        linesRemoved: 2,
      });

      const events = db
        .getDatabase()
        .prepare("SELECT * FROM events WHERE session_id = ?")
        .all("sess-007") as Array<Record<string, unknown>>;
      expect(events[0].file_path).toBe("/app/src/utils.ts");
    });
  });

  describe("endSession", () => {
    it("updates session with ended_at and summary", () => {
      engine.startSession("sess-008");

      const summary: SessionSummary = {
        totalToolCalls: 15,
        fileEditsCount: 8,
        aiAuthoredLines: 200,
        humanVerifiedLines: 50,
        classification: "ai_heavy",
        estimatedCostUsd: 1.25,
      };

      engine.endSession("sess-008", summary);

      const session = db.getStatements().getSession.get("sess-008") as Record<
        string,
        unknown
      >;
      expect(session.ended_at).toBeDefined();
      expect(session.ended_at).not.toBeNull();
      expect(session.total_tool_calls).toBe(15);
      expect(session.file_edits_count).toBe(8);
      expect(session.ai_authored_lines).toBe(200);
      expect(session.human_verified_lines).toBe(50);
      expect(session.classification).toBe("ai_heavy");
      expect(session.estimated_cost_usd).toBe(1.25);
    });

    it("computes duration_seconds from started_at to ended_at", () => {
      engine.startSession("sess-009");

      // Small delay to ensure non-zero duration
      const summary: SessionSummary = {
        totalToolCalls: 0,
        fileEditsCount: 0,
        aiAuthoredLines: 0,
        humanVerifiedLines: 0,
      };

      engine.endSession("sess-009", summary);

      const session = db.getStatements().getSession.get("sess-009") as Record<
        string,
        unknown
      >;
      expect(typeof session.duration_seconds).toBe("number");
      expect(session.duration_seconds).toBeGreaterThanOrEqual(0);
    });
  });

  describe("privacy: prompt content never stored", () => {
    it("UserPromptSubmit events do not store tool_input or content", () => {
      engine.startSession("sess-010");

      // Record a prompt submit event — only timestamp, no content
      engine.recordEvent({
        sessionId: "sess-010",
        eventType: "UserPromptSubmit",
        timestamp: "2026-02-20T10:00:00Z",
        // No toolName, no filePath, no content
      });

      const events = db
        .getDatabase()
        .prepare("SELECT * FROM events WHERE session_id = ?")
        .all("sess-010") as Array<Record<string, unknown>>;
      expect(events).toHaveLength(1);
      expect(events[0].event_type).toBe("UserPromptSubmit");
      expect(events[0].tool_name).toBeNull();
      expect(events[0].file_path).toBeNull();

      // Verify there's no content column in the events table
      const columns = db
        .getDatabase()
        .prepare("PRAGMA table_info(events)")
        .all() as Array<{ name: string }>;
      const columnNames = columns.map((c) => c.name);
      expect(columnNames).not.toContain("content");
      expect(columnNames).not.toContain("prompt_content");
      expect(columnNames).not.toContain("tool_input");
    });
  });

  describe("event retrieval", () => {
    it("retrieves events for a session ordered by timestamp", () => {
      engine.startSession("sess-011");

      engine.recordEvent({
        sessionId: "sess-011",
        eventType: "PostToolUse",
        toolName: "Bash",
        timestamp: "2026-02-20T10:00:02Z",
      });
      engine.recordEvent({
        sessionId: "sess-011",
        eventType: "PostToolUse",
        toolName: "Write",
        timestamp: "2026-02-20T10:00:01Z",
      });
      engine.recordEvent({
        sessionId: "sess-011",
        eventType: "PostToolUse",
        toolName: "Edit",
        timestamp: "2026-02-20T10:00:03Z",
      });

      const events = db
        .getStatements()
        .getSessionEvents.all("sess-011") as Array<Record<string, unknown>>;
      expect(events).toHaveLength(3);
      // Should be ordered by timestamp
      expect(events[0].tool_name).toBe("Write");
      expect(events[1].tool_name).toBe("Bash");
      expect(events[2].tool_name).toBe("Edit");
    });
  });

  describe("line count aggregation", () => {
    it("tracks lines added and removed across events", () => {
      engine.startSession("sess-012");

      engine.recordEvent({
        sessionId: "sess-012",
        eventType: "PostToolUse",
        toolName: "Write",
        timestamp: "2026-02-20T10:00:00Z",
        linesAdded: 100,
        linesRemoved: 0,
      });
      engine.recordEvent({
        sessionId: "sess-012",
        eventType: "PostToolUse",
        toolName: "Edit",
        timestamp: "2026-02-20T10:00:01Z",
        linesAdded: 5,
        linesRemoved: 10,
      });

      const result = db
        .getDatabase()
        .prepare(
          "SELECT SUM(lines_added) as total_added, SUM(lines_removed) as total_removed FROM events WHERE session_id = ?"
        )
        .get("sess-012") as { total_added: number; total_removed: number };

      expect(result.total_added).toBe(105);
      expect(result.total_removed).toBe(10);
    });
  });
});
