/**
 * Tests for SQLite database layer.
 *
 * Verifies:
 * - Schema creation (all tables and indexes)
 * - WAL journal mode
 * - Foreign key enforcement
 * - Prepared statements compile and run
 * - File permissions (0o600)
 * - Clean close
 * - Schema version stored
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdtempSync, statSync, rmSync, existsSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { AnalyticsDB } from "../../../src/core/analytics/db.js";

describe("AnalyticsDB", () => {
  let tempDir: string;
  let dbPath: string;
  let db: AnalyticsDB;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-db-test-"));
    dbPath = join(tempDir, "test-analytics.db");
    db = new AnalyticsDB(dbPath);
  });

  afterEach(() => {
    try {
      db.close();
    } catch {
      // Already closed
    }
    rmSync(tempDir, { recursive: true, force: true });
  });

  describe("schema creation", () => {
    it("creates sessions table", () => {
      const rawDb = db.getDatabase();
      const tables = rawDb
        .prepare(
          "SELECT name FROM sqlite_master WHERE type='table' AND name='sessions'"
        )
        .get() as { name: string } | undefined;
      expect(tables).toBeDefined();
      expect(tables!.name).toBe("sessions");
    });

    it("creates events table", () => {
      const rawDb = db.getDatabase();
      const tables = rawDb
        .prepare(
          "SELECT name FROM sqlite_master WHERE type='table' AND name='events'"
        )
        .get() as { name: string } | undefined;
      expect(tables).toBeDefined();
      expect(tables!.name).toBe("events");
    });

    it("creates authorship_ledger table", () => {
      const rawDb = db.getDatabase();
      const tables = rawDb
        .prepare(
          "SELECT name FROM sqlite_master WHERE type='table' AND name='authorship_ledger'"
        )
        .get() as { name: string } | undefined;
      expect(tables).toBeDefined();
    });

    it("creates metacognition_logs table", () => {
      const rawDb = db.getDatabase();
      const tables = rawDb
        .prepare(
          "SELECT name FROM sqlite_master WHERE type='table' AND name='metacognition_logs'"
        )
        .get() as { name: string } | undefined;
      expect(tables).toBeDefined();
    });

    it("creates agent_spans table", () => {
      const rawDb = db.getDatabase();
      const tables = rawDb
        .prepare(
          "SELECT name FROM sqlite_master WHERE type='table' AND name='agent_spans'"
        )
        .get() as { name: string } | undefined;
      expect(tables).toBeDefined();
    });

    it("creates schema_meta table", () => {
      const rawDb = db.getDatabase();
      const tables = rawDb
        .prepare(
          "SELECT name FROM sqlite_master WHERE type='table' AND name='schema_meta'"
        )
        .get() as { name: string } | undefined;
      expect(tables).toBeDefined();
    });

    it("creates all expected indexes", () => {
      const rawDb = db.getDatabase();
      const indexes = rawDb
        .prepare("SELECT name FROM sqlite_master WHERE type='index'")
        .all() as Array<{ name: string }>;

      const indexNames = indexes.map((i) => i.name);
      expect(indexNames).toContain("idx_events_session");
      expect(indexNames).toContain("idx_events_timestamp");
      expect(indexNames).toContain("idx_authorship_session");
      expect(indexNames).toContain("idx_agents_session");
    });

    it("stores schema version in schema_meta", () => {
      const rawDb = db.getDatabase();
      const version = rawDb
        .prepare("SELECT value FROM schema_meta WHERE key = 'version'")
        .get() as { value: string } | undefined;
      expect(version).toBeDefined();
      expect(version!.value).toBe("1");
    });
  });

  describe("WAL mode", () => {
    it("enables WAL journal mode", () => {
      const rawDb = db.getDatabase();
      const mode = rawDb.pragma("journal_mode") as Array<{
        journal_mode: string;
      }>;
      expect(mode[0].journal_mode).toBe("wal");
    });
  });

  describe("foreign keys", () => {
    it("enables foreign key constraints", () => {
      const rawDb = db.getDatabase();
      const fk = rawDb.pragma("foreign_keys") as Array<{
        foreign_keys: number;
      }>;
      expect(fk[0].foreign_keys).toBe(1);
    });
  });

  describe("file permissions", () => {
    it("sets database file to 0o600 (user-only)", () => {
      const stats = statSync(dbPath);
      // Check that the file mode includes 0o600 (owner rw)
      const mode = stats.mode & 0o777;
      expect(mode).toBe(0o600);
    });
  });

  describe("prepared statements", () => {
    it("compiles all prepared statements", () => {
      const stmts = db.getStatements();
      expect(stmts.insertEvent).toBeDefined();
      expect(stmts.insertSession).toBeDefined();
      expect(stmts.updateSession).toBeDefined();
      expect(stmts.insertAuthorshipEntry).toBeDefined();
      expect(stmts.getSession).toBeDefined();
      expect(stmts.getSessionEvents).toBeDefined();
      expect(stmts.updateSessionToolCalls).toBeDefined();
    });

    it("insertSession runs without error", () => {
      const stmts = db.getStatements();
      expect(() => {
        stmts.insertSession.run({
          id: "test-session",
          startedAt: new Date().toISOString(),
        });
      }).not.toThrow();
    });

    it("insertEvent runs after session exists", () => {
      const stmts = db.getStatements();
      stmts.insertSession.run({
        id: "test-session",
        startedAt: new Date().toISOString(),
      });

      expect(() => {
        stmts.insertEvent.run({
          sessionId: "test-session",
          eventType: "PostToolUse",
          toolName: "Bash",
          timestamp: new Date().toISOString(),
          filePath: null,
          linesAdded: 10,
          linesRemoved: 5,
          aiConfidenceScore: 0.8,
        });
      }).not.toThrow();
    });

    it("getSession retrieves inserted session", () => {
      const stmts = db.getStatements();
      const now = new Date().toISOString();
      stmts.insertSession.run({ id: "sess-123", startedAt: now });

      const session = stmts.getSession.get("sess-123") as Record<
        string,
        unknown
      >;
      expect(session).toBeDefined();
      expect(session.id).toBe("sess-123");
      expect(session.started_at).toBe(now);
    });

    it("insertAuthorshipEntry runs after session exists", () => {
      const stmts = db.getStatements();
      stmts.insertSession.run({
        id: "test-session",
        startedAt: new Date().toISOString(),
      });

      expect(() => {
        stmts.insertAuthorshipEntry.run({
          sessionId: "test-session",
          filePath: "/app/src/index.ts",
          toolName: "Write",
          linesChanged: 50,
          aiScore: 0.9,
          classification: "high_probability_ai",
          timestamp: new Date().toISOString(),
        });
      }).not.toThrow();
    });
  });

  describe("close", () => {
    it("closes database without error", () => {
      expect(() => db.close()).not.toThrow();
    });

    it("database file still exists after close", () => {
      db.close();
      expect(existsSync(dbPath)).toBe(true);
    });

    it("cannot run queries after close", () => {
      db.close();
      expect(() => {
        db.getDatabase().prepare("SELECT 1").get();
      }).toThrow();
    });
  });

  describe("idempotent initialization", () => {
    it("opening same path twice does not throw", () => {
      db.close();
      const db2 = new AnalyticsDB(dbPath);
      expect(() => db2.close()).not.toThrow();
    });

    it("schema is preserved on re-open", () => {
      const stmts = db.getStatements();
      stmts.insertSession.run({
        id: "persist-test",
        startedAt: new Date().toISOString(),
      });
      db.close();

      const db2 = new AnalyticsDB(dbPath);
      const session = db2
        .getStatements()
        .getSession.get("persist-test") as Record<string, unknown>;
      expect(session).toBeDefined();
      expect(session.id).toBe("persist-test");
      db2.close();
    });
  });
});
