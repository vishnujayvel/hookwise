/**
 * Multi-agent observability handler for hookwise v1.0
 *
 * Tracks sub-agent lifecycle spans in the analytics database,
 * detects file conflicts between overlapping agents, and generates
 * Mermaid diagrams of agent hierarchies.
 */

import type Database from "better-sqlite3";
import type { FileConflict } from "./types.js";

/**
 * Agent span row from the database.
 */
interface AgentSpanRow {
  id: number;
  session_id: string;
  agent_id: string;
  parent_agent_id: string | null;
  agent_type: string | null;
  started_at: string;
  stopped_at: string | null;
  files_modified: string | null;
}

/**
 * Observer for multi-agent session activity.
 *
 * Records agent start/stop spans, detects file conflicts between
 * overlapping agents, and generates Mermaid diagrams of agent trees.
 *
 * Takes a raw better-sqlite3 Database instance for direct SQL access.
 */
export class AgentObserver {
  private db: Database.Database;

  constructor(db: Database.Database) {
    this.db = db;
    this.ensureSchema();
  }

  /**
   * Ensure the agent_spans table exists.
   */
  private ensureSchema(): void {
    try {
      this.db.exec(`
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
      `);
    } catch {
      // Fail-open: schema may already exist
    }
  }

  /**
   * Record the start of an agent span.
   *
   * @param sessionId - Session this agent belongs to
   * @param agentId - Unique identifier for this agent
   * @param parentAgentId - Parent agent ID (null for root agents)
   * @param agentType - Type of agent (e.g., "researcher", "coder")
   */
  recordStart(
    sessionId: string,
    agentId: string,
    parentAgentId?: string,
    agentType?: string
  ): void {
    try {
      this.db
        .prepare(
          `INSERT INTO agent_spans (session_id, agent_id, parent_agent_id, agent_type, started_at)
           VALUES (?, ?, ?, ?, ?)`
        )
        .run(
          sessionId,
          agentId,
          parentAgentId ?? null,
          agentType ?? null,
          new Date().toISOString()
        );
    } catch {
      // Fail-open
    }
  }

  /**
   * Record the stop of an agent span.
   *
   * @param sessionId - Session this agent belongs to
   * @param agentId - Unique identifier for this agent
   * @param filesModified - List of file paths modified by this agent
   */
  recordStop(
    sessionId: string,
    agentId: string,
    filesModified?: string[]
  ): void {
    try {
      const filesJson = filesModified ? JSON.stringify(filesModified) : null;
      this.db
        .prepare(
          `UPDATE agent_spans
           SET stopped_at = ?, files_modified = ?
           WHERE session_id = ? AND agent_id = ? AND stopped_at IS NULL`
        )
        .run(new Date().toISOString(), filesJson, sessionId, agentId);
    } catch {
      // Fail-open
    }
  }

  /**
   * Detect file conflicts: files modified by multiple agents with overlapping time spans.
   *
   * @param sessionId - Session to check for conflicts
   * @returns Array of file conflicts with involved agents and overlap periods
   */
  detectFileConflicts(sessionId: string): FileConflict[] {
    try {
      const spans = this.db
        .prepare(
          `SELECT * FROM agent_spans
           WHERE session_id = ? AND files_modified IS NOT NULL`
        )
        .all(sessionId) as AgentSpanRow[];

      // Build a map of file -> list of agent spans that touched it
      const fileAgents = new Map<
        string,
        Array<{ agentId: string; startedAt: string; stoppedAt: string }>
      >();

      for (const span of spans) {
        if (!span.files_modified) continue;
        let files: string[];
        try {
          files = JSON.parse(span.files_modified) as string[];
        } catch {
          continue;
        }

        const stoppedAt = span.stopped_at ?? new Date().toISOString();

        for (const filePath of files) {
          if (!fileAgents.has(filePath)) {
            fileAgents.set(filePath, []);
          }
          fileAgents.get(filePath)!.push({
            agentId: span.agent_id,
            startedAt: span.started_at,
            stoppedAt,
          });
        }
      }

      // Find files with overlapping spans from different agents
      const conflicts: FileConflict[] = [];

      for (const [filePath, agents] of fileAgents) {
        if (agents.length < 2) continue;

        // Check all pairs for overlap
        const uniqueAgents = [...new Set(agents.map((a) => a.agentId))];
        if (uniqueAgents.length < 2) continue;

        // Find the overlap period
        const starts = agents.map((a) => a.startedAt);
        const stops = agents.map((a) => a.stoppedAt);
        const overlapStart = starts.sort().reverse()[0]; // latest start
        const overlapEnd = stops.sort()[0]; // earliest end

        if (overlapStart <= overlapEnd) {
          conflicts.push({
            filePath,
            agents: uniqueAgents,
            overlapPeriod: {
              start: overlapStart,
              end: overlapEnd,
            },
          });
        }
      }

      return conflicts;
    } catch {
      return [];
    }
  }

  /**
   * Generate a Mermaid graph diagram of the agent hierarchy for a session.
   *
   * @param sessionId - Session to generate the tree for
   * @returns Mermaid graph string, or empty string on error
   */
  generateAgentTree(sessionId: string): string {
    try {
      const spans = this.db
        .prepare(
          `SELECT * FROM agent_spans WHERE session_id = ? ORDER BY started_at`
        )
        .all(sessionId) as AgentSpanRow[];

      if (spans.length === 0) return "";

      const lines: string[] = ["graph TD"];

      for (const span of spans) {
        const label = span.agent_type
          ? `${span.agent_id}[${span.agent_type}]`
          : span.agent_id;

        if (span.parent_agent_id) {
          lines.push(`    ${span.parent_agent_id} --> ${label}`);
        } else {
          // Root node: just declare it
          lines.push(`    ${label}`);
        }
      }

      return lines.join("\n");
    } catch {
      return "";
    }
  }
}
