/**
 * Tests for multi-agent observability handler.
 *
 * Verifies:
 * - Record start and stop spans
 * - File conflict detection with overlapping spans
 * - Mermaid diagram output format
 * - Missing parent agent handled gracefully
 * - Uses in-memory SQLite for tests
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import Database from "better-sqlite3";
import { AgentObserver } from "../../src/core/agents.js";

describe("AgentObserver", () => {
  let db: Database.Database;
  let observer: AgentObserver;

  beforeEach(() => {
    db = new Database(":memory:");
    observer = new AgentObserver(db);
  });

  afterEach(() => {
    db.close();
  });

  describe("recordStart", () => {
    it("inserts a span into agent_spans", () => {
      // Create a session first for FK
      db.exec(
        `INSERT INTO sessions (id, started_at) VALUES ('sess-1', '2026-01-01T00:00:00Z')`
      );

      observer.recordStart("sess-1", "agent-a");

      const rows = db
        .prepare("SELECT * FROM agent_spans WHERE agent_id = ?")
        .all("agent-a") as any[];
      expect(rows).toHaveLength(1);
      expect(rows[0].session_id).toBe("sess-1");
      expect(rows[0].agent_id).toBe("agent-a");
      expect(rows[0].started_at).toBeTruthy();
      expect(rows[0].stopped_at).toBeNull();
    });

    it("records parent_agent_id when provided", () => {
      db.exec(
        `INSERT INTO sessions (id, started_at) VALUES ('sess-1', '2026-01-01T00:00:00Z')`
      );

      observer.recordStart("sess-1", "agent-b", "agent-a");

      const row = db
        .prepare("SELECT * FROM agent_spans WHERE agent_id = ?")
        .get("agent-b") as any;
      expect(row.parent_agent_id).toBe("agent-a");
    });

    it("records agent_type when provided", () => {
      db.exec(
        `INSERT INTO sessions (id, started_at) VALUES ('sess-1', '2026-01-01T00:00:00Z')`
      );

      observer.recordStart("sess-1", "agent-c", undefined, "researcher");

      const row = db
        .prepare("SELECT * FROM agent_spans WHERE agent_id = ?")
        .get("agent-c") as any;
      expect(row.agent_type).toBe("researcher");
    });

    it("handles missing parent gracefully (null)", () => {
      db.exec(
        `INSERT INTO sessions (id, started_at) VALUES ('sess-1', '2026-01-01T00:00:00Z')`
      );

      observer.recordStart("sess-1", "root-agent");

      const row = db
        .prepare("SELECT * FROM agent_spans WHERE agent_id = ?")
        .get("root-agent") as any;
      expect(row.parent_agent_id).toBeNull();
    });
  });

  describe("recordStop", () => {
    it("updates stopped_at for the agent span", () => {
      db.exec(
        `INSERT INTO sessions (id, started_at) VALUES ('sess-1', '2026-01-01T00:00:00Z')`
      );
      observer.recordStart("sess-1", "agent-a");
      observer.recordStop("sess-1", "agent-a");

      const row = db
        .prepare("SELECT * FROM agent_spans WHERE agent_id = ?")
        .get("agent-a") as any;
      expect(row.stopped_at).toBeTruthy();
    });

    it("stores files_modified as JSON array", () => {
      db.exec(
        `INSERT INTO sessions (id, started_at) VALUES ('sess-1', '2026-01-01T00:00:00Z')`
      );
      observer.recordStart("sess-1", "agent-a");
      observer.recordStop("sess-1", "agent-a", ["/src/app.ts", "/src/index.ts"]);

      const row = db
        .prepare("SELECT * FROM agent_spans WHERE agent_id = ?")
        .get("agent-a") as any;
      const files = JSON.parse(row.files_modified);
      expect(files).toEqual(["/src/app.ts", "/src/index.ts"]);
    });

    it("sets files_modified to null when not provided", () => {
      db.exec(
        `INSERT INTO sessions (id, started_at) VALUES ('sess-1', '2026-01-01T00:00:00Z')`
      );
      observer.recordStart("sess-1", "agent-a");
      observer.recordStop("sess-1", "agent-a");

      const row = db
        .prepare("SELECT * FROM agent_spans WHERE agent_id = ?")
        .get("agent-a") as any;
      expect(row.files_modified).toBeNull();
    });
  });

  describe("detectFileConflicts", () => {
    it("detects conflicts when two agents modify the same file", () => {
      db.exec(
        `INSERT INTO sessions (id, started_at) VALUES ('sess-1', '2026-01-01T00:00:00Z')`
      );

      // Manually insert overlapping spans
      db.prepare(
        `INSERT INTO agent_spans (session_id, agent_id, started_at, stopped_at, files_modified)
         VALUES (?, ?, ?, ?, ?)`
      ).run(
        "sess-1",
        "agent-a",
        "2026-01-01T00:00:00Z",
        "2026-01-01T00:10:00Z",
        JSON.stringify(["/src/shared.ts"])
      );
      db.prepare(
        `INSERT INTO agent_spans (session_id, agent_id, started_at, stopped_at, files_modified)
         VALUES (?, ?, ?, ?, ?)`
      ).run(
        "sess-1",
        "agent-b",
        "2026-01-01T00:05:00Z",
        "2026-01-01T00:15:00Z",
        JSON.stringify(["/src/shared.ts"])
      );

      const conflicts = observer.detectFileConflicts("sess-1");
      expect(conflicts).toHaveLength(1);
      expect(conflicts[0].filePath).toBe("/src/shared.ts");
      expect(conflicts[0].agents).toContain("agent-a");
      expect(conflicts[0].agents).toContain("agent-b");
    });

    it("returns empty array when no file conflicts exist", () => {
      db.exec(
        `INSERT INTO sessions (id, started_at) VALUES ('sess-1', '2026-01-01T00:00:00Z')`
      );

      db.prepare(
        `INSERT INTO agent_spans (session_id, agent_id, started_at, stopped_at, files_modified)
         VALUES (?, ?, ?, ?, ?)`
      ).run(
        "sess-1",
        "agent-a",
        "2026-01-01T00:00:00Z",
        "2026-01-01T00:10:00Z",
        JSON.stringify(["/src/a.ts"])
      );
      db.prepare(
        `INSERT INTO agent_spans (session_id, agent_id, started_at, stopped_at, files_modified)
         VALUES (?, ?, ?, ?, ?)`
      ).run(
        "sess-1",
        "agent-b",
        "2026-01-01T00:00:00Z",
        "2026-01-01T00:10:00Z",
        JSON.stringify(["/src/b.ts"])
      );

      const conflicts = observer.detectFileConflicts("sess-1");
      expect(conflicts).toHaveLength(0);
    });

    it("returns empty array for empty session", () => {
      const conflicts = observer.detectFileConflicts("nonexistent-session");
      expect(conflicts).toHaveLength(0);
    });

    it("ignores spans without files_modified", () => {
      db.exec(
        `INSERT INTO sessions (id, started_at) VALUES ('sess-1', '2026-01-01T00:00:00Z')`
      );

      db.prepare(
        `INSERT INTO agent_spans (session_id, agent_id, started_at, stopped_at, files_modified)
         VALUES (?, ?, ?, ?, ?)`
      ).run("sess-1", "agent-a", "2026-01-01T00:00:00Z", "2026-01-01T00:10:00Z", null);

      const conflicts = observer.detectFileConflicts("sess-1");
      expect(conflicts).toHaveLength(0);
    });
  });

  describe("generateAgentTree", () => {
    it("generates Mermaid graph for parent-child agents", () => {
      db.exec(
        `INSERT INTO sessions (id, started_at) VALUES ('sess-1', '2026-01-01T00:00:00Z')`
      );

      db.prepare(
        `INSERT INTO agent_spans (session_id, agent_id, parent_agent_id, agent_type, started_at)
         VALUES (?, ?, ?, ?, ?)`
      ).run("sess-1", "root", null, "coordinator", "2026-01-01T00:00:00Z");
      db.prepare(
        `INSERT INTO agent_spans (session_id, agent_id, parent_agent_id, agent_type, started_at)
         VALUES (?, ?, ?, ?, ?)`
      ).run("sess-1", "child-1", "root", "researcher", "2026-01-01T00:01:00Z");
      db.prepare(
        `INSERT INTO agent_spans (session_id, agent_id, parent_agent_id, agent_type, started_at)
         VALUES (?, ?, ?, ?, ?)`
      ).run("sess-1", "child-2", "root", "coder", "2026-01-01T00:02:00Z");

      const mermaid = observer.generateAgentTree("sess-1");
      expect(mermaid).toContain("graph TD");
      expect(mermaid).toContain("root[coordinator]");
      expect(mermaid).toContain("root --> child-1[researcher]");
      expect(mermaid).toContain("root --> child-2[coder]");
    });

    it("returns empty string for empty session", () => {
      const mermaid = observer.generateAgentTree("nonexistent");
      expect(mermaid).toBe("");
    });

    it("handles agents without type", () => {
      db.exec(
        `INSERT INTO sessions (id, started_at) VALUES ('sess-1', '2026-01-01T00:00:00Z')`
      );

      db.prepare(
        `INSERT INTO agent_spans (session_id, agent_id, parent_agent_id, agent_type, started_at)
         VALUES (?, ?, ?, ?, ?)`
      ).run("sess-1", "solo-agent", null, null, "2026-01-01T00:00:00Z");

      const mermaid = observer.generateAgentTree("sess-1");
      expect(mermaid).toContain("graph TD");
      expect(mermaid).toContain("solo-agent");
      // Should not have brackets if no type
      expect(mermaid).not.toContain("[");
    });

    it("handles deep nesting", () => {
      db.exec(
        `INSERT INTO sessions (id, started_at) VALUES ('sess-1', '2026-01-01T00:00:00Z')`
      );

      db.prepare(
        `INSERT INTO agent_spans (session_id, agent_id, parent_agent_id, agent_type, started_at)
         VALUES (?, ?, ?, ?, ?)`
      ).run("sess-1", "l0", null, "root", "2026-01-01T00:00:00Z");
      db.prepare(
        `INSERT INTO agent_spans (session_id, agent_id, parent_agent_id, agent_type, started_at)
         VALUES (?, ?, ?, ?, ?)`
      ).run("sess-1", "l1", "l0", "mid", "2026-01-01T00:01:00Z");
      db.prepare(
        `INSERT INTO agent_spans (session_id, agent_id, parent_agent_id, agent_type, started_at)
         VALUES (?, ?, ?, ?, ?)`
      ).run("sess-1", "l2", "l1", "leaf", "2026-01-01T00:02:00Z");

      const mermaid = observer.generateAgentTree("sess-1");
      expect(mermaid).toContain("l0 --> l1[mid]");
      expect(mermaid).toContain("l1 --> l2[leaf]");
    });
  });
});
