/**
 * Event recording and session lifecycle management for hookwise analytics.
 *
 * Implements the AnalyticsEngine methods for:
 * - Recording hook events (PostToolUse, UserPromptSubmit, etc.)
 * - Starting and ending sessions with summary statistics
 *
 * Privacy contract: UserPromptSubmit events store timestamp and char count ONLY.
 * Prompt content is NEVER stored.
 */

import type { AnalyticsDB } from "./db.js";
import type { AnalyticsEvent, SessionSummary } from "../types.js";
import { logDebug, logError } from "../errors.js";

/**
 * Analytics engine for event recording and session lifecycle.
 *
 * Delegates storage to AnalyticsDB, handles business logic for
 * event processing and session management.
 */
export class AnalyticsEngine {
  private db: AnalyticsDB;

  constructor(db: AnalyticsDB) {
    this.db = db;
  }

  /**
   * Record an analytics event.
   *
   * Inserts the event into the events table and increments the session's
   * tool_calls counter for tool-related events.
   *
   * Privacy: For UserPromptSubmit events, only timestamp and char count
   * are stored — prompt content is never persisted.
   *
   * @param event - The event to record
   */
  recordEvent(event: AnalyticsEvent): void {
    try {
      const stmts = this.db.getStatements();

      stmts.insertEvent.run({
        sessionId: event.sessionId,
        eventType: event.eventType,
        toolName: event.toolName ?? null,
        timestamp: event.timestamp,
        filePath: event.filePath ?? null,
        linesAdded: event.linesAdded ?? 0,
        linesRemoved: event.linesRemoved ?? 0,
        aiConfidenceScore: event.aiConfidenceScore ?? null,
      });

      // Increment tool call counter for tool-related events
      if (event.toolName) {
        stmts.updateSessionToolCalls.run(event.sessionId);
      }

      logDebug("Recorded analytics event", {
        sessionId: event.sessionId,
        eventType: event.eventType,
        toolName: event.toolName,
      });
    } catch (error) {
      logError(
        error instanceof Error ? error : new Error(String(error)),
        { context: "AnalyticsEngine.recordEvent" }
      );
    }
  }

  /**
   * Start a new analytics session.
   *
   * Creates a session row with the current timestamp as started_at.
   *
   * @param sessionId - Unique session identifier from Claude Code
   */
  startSession(sessionId: string): void {
    try {
      const stmts = this.db.getStatements();
      stmts.insertSession.run({
        id: sessionId,
        startedAt: new Date().toISOString(),
      });

      logDebug("Started analytics session", { sessionId });
    } catch (error) {
      logError(
        error instanceof Error ? error : new Error(String(error)),
        { context: "AnalyticsEngine.startSession" }
      );
    }
  }

  /**
   * End an analytics session with summary statistics.
   *
   * Updates the session row with ended_at, duration, and aggregate stats.
   *
   * @param sessionId - Session to end
   * @param summary - Aggregated session statistics
   */
  endSession(sessionId: string, summary: SessionSummary): void {
    try {
      const stmts = this.db.getStatements();
      const now = new Date().toISOString();

      // Get session to compute duration
      const session = stmts.getSession.get(sessionId) as
        | { started_at: string }
        | undefined;

      let durationSeconds = 0;
      if (session) {
        const startedAt = new Date(session.started_at).getTime();
        const endedAt = new Date(now).getTime();
        durationSeconds = Math.round((endedAt - startedAt) / 1000);
      }

      stmts.updateSession.run({
        id: sessionId,
        endedAt: now,
        durationSeconds,
        totalToolCalls: summary.totalToolCalls,
        fileEditsCount: summary.fileEditsCount,
        aiAuthoredLines: summary.aiAuthoredLines,
        humanVerifiedLines: summary.humanVerifiedLines,
        classification: summary.classification ?? null,
        estimatedCostUsd: summary.estimatedCostUsd ?? 0.0,
      });

      logDebug("Ended analytics session", { sessionId, durationSeconds });
    } catch (error) {
      logError(
        error instanceof Error ? error : new Error(String(error)),
        { context: "AnalyticsEngine.endSession" }
      );
    }
  }

  /**
   * Get the underlying AnalyticsDB instance.
   * Exposed for authorship ledger and stats queries.
   */
  getDB(): AnalyticsDB {
    return this.db;
  }
}
