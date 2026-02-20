/**
 * SQLite database layer for hookwise analytics.
 *
 * Uses better-sqlite3 for synchronous, zero-dependency SQLite operations.
 * Database is initialized at ~/.hookwise/analytics.db with WAL mode
 * and foreign keys enabled. File permissions are set to 0o600 (user-only).
 *
 * Provides prepared statements for high-frequency operations to avoid
 * repeated SQL compilation.
 */

import Database from "better-sqlite3";
import { chmodSync, existsSync } from "node:fs";
import { dirname } from "node:path";
import { DEFAULT_DB_PATH, DEFAULT_DB_MODE } from "../constants.js";
import { ensureDir } from "../state.js";
import { AnalyticsError, logError, logDebug } from "../errors.js";

// --- Schema ---

const SCHEMA_SQL = `
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

CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);

CREATE TABLE IF NOT EXISTS authorship_ledger (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    file_path TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    lines_changed INTEGER NOT NULL,
    ai_score REAL NOT NULL,
    classification TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE INDEX IF NOT EXISTS idx_authorship_session ON authorship_ledger(session_id);

CREATE TABLE IF NOT EXISTS metacognition_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    trigger_type TEXT NOT NULL,
    prompt_shown TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE TABLE IF NOT EXISTS agent_spans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    parent_agent_id TEXT,
    agent_type TEXT,
    started_at TEXT NOT NULL,
    stopped_at TEXT,
    files_modified TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE INDEX IF NOT EXISTS idx_agents_session ON agent_spans(session_id);

CREATE TABLE IF NOT EXISTS schema_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`;

const SCHEMA_VERSION = "1";

// --- Prepared Statement Types ---

export interface PreparedStatements {
  insertEvent: Database.Statement;
  insertSession: Database.Statement;
  updateSession: Database.Statement;
  insertAuthorshipEntry: Database.Statement;
  getSession: Database.Statement;
  getSessionEvents: Database.Statement;
  updateSessionToolCalls: Database.Statement;
}

// --- AnalyticsDB Class ---

/**
 * SQLite database wrapper for hookwise analytics.
 *
 * Manages the database lifecycle: initialization, schema creation,
 * prepared statements, and clean shutdown.
 */
export class AnalyticsDB {
  private db: Database.Database;
  private statements: PreparedStatements;

  /**
   * Open or create the analytics database.
   *
   * @param dbPath - Path to the SQLite database file. Defaults to ~/.hookwise/analytics.db.
   */
  constructor(dbPath?: string) {
    const effectivePath = dbPath ?? DEFAULT_DB_PATH;

    // Ensure parent directory exists
    ensureDir(dirname(effectivePath));

    try {
      this.db = new Database(effectivePath);
    } catch (error) {
      throw new AnalyticsError(
        `Failed to open analytics database at ${effectivePath}: ${error instanceof Error ? error.message : String(error)}`
      );
    }

    // Enable WAL mode for concurrent reads and better write performance
    this.db.pragma("journal_mode = WAL");

    // Enable foreign key constraints
    this.db.pragma("foreign_keys = ON");

    // Create schema
    this.db.exec(SCHEMA_SQL);

    // Store schema version
    this.db
      .prepare(
        "INSERT OR REPLACE INTO schema_meta (key, value) VALUES ('version', ?)"
      )
      .run(SCHEMA_VERSION);

    // Set file permissions to user-only (0o600)
    try {
      chmodSync(effectivePath, DEFAULT_DB_MODE);
    } catch {
      // Non-fatal: permissions may not be settable in all environments
      logDebug("Could not set DB file permissions", { path: effectivePath });
    }

    // Prepare high-frequency statements
    this.statements = this.prepareStatements();

    logDebug("Analytics database initialized", { path: effectivePath });
  }

  /**
   * Prepare all high-frequency SQL statements.
   */
  private prepareStatements(): PreparedStatements {
    return {
      insertEvent: this.db.prepare(`
        INSERT INTO events (session_id, event_type, tool_name, timestamp, file_path, lines_added, lines_removed, ai_confidence_score)
        VALUES (@sessionId, @eventType, @toolName, @timestamp, @filePath, @linesAdded, @linesRemoved, @aiConfidenceScore)
      `),

      insertSession: this.db.prepare(`
        INSERT INTO sessions (id, started_at)
        VALUES (@id, @startedAt)
      `),

      updateSession: this.db.prepare(`
        UPDATE sessions SET
          ended_at = @endedAt,
          duration_seconds = @durationSeconds,
          total_tool_calls = @totalToolCalls,
          file_edits_count = @fileEditsCount,
          ai_authored_lines = @aiAuthoredLines,
          human_verified_lines = @humanVerifiedLines,
          classification = @classification,
          estimated_cost_usd = @estimatedCostUsd
        WHERE id = @id
      `),

      insertAuthorshipEntry: this.db.prepare(`
        INSERT INTO authorship_ledger (session_id, file_path, tool_name, lines_changed, ai_score, classification, timestamp)
        VALUES (@sessionId, @filePath, @toolName, @linesChanged, @aiScore, @classification, @timestamp)
      `),

      getSession: this.db.prepare(`
        SELECT * FROM sessions WHERE id = ?
      `),

      getSessionEvents: this.db.prepare(`
        SELECT * FROM events WHERE session_id = ? ORDER BY timestamp
      `),

      updateSessionToolCalls: this.db.prepare(`
        UPDATE sessions SET total_tool_calls = total_tool_calls + 1 WHERE id = ?
      `),
    };
  }

  /**
   * Get the underlying better-sqlite3 Database instance.
   * Exposed for advanced queries and testing.
   */
  getDatabase(): Database.Database {
    return this.db;
  }

  /**
   * Get prepared statements for direct use.
   */
  getStatements(): PreparedStatements {
    return this.statements;
  }

  /**
   * Close the database connection cleanly.
   * Must be called when the analytics engine is shutting down.
   */
  close(): void {
    try {
      this.db.close();
      logDebug("Analytics database closed");
    } catch (error) {
      logError(
        error instanceof Error ? error : new Error(String(error)),
        { context: "AnalyticsDB.close" }
      );
    }
  }
}
