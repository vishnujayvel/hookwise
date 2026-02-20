/**
 * Authorship Ledger with AI Confidence Scoring for hookwise analytics.
 *
 * Implements heuristic-based AI confidence scoring that estimates
 * whether code changes were AI-generated or human-authored based on:
 * - Time since last user prompt
 * - Number of lines changed
 * - Tool name (Edit vs. Write vs. Bash)
 *
 * Scoring tiers:
 * - < 10s AND > 50 lines → 0.9+ (high_probability_ai)
 * - < 10s AND 10-50 lines → 0.6-0.8 (likely_ai)
 * - > 30s OR < 5 lines → 0.2-0.4 (mixed_verified)
 * - Edit AND < 3 lines → 0.1 (human_authored)
 */

import type { AnalyticsDB } from "./db.js";
import type { AIConfidenceScore, AIClassification, AuthorshipEntry } from "../types.js";
import { logDebug, logError } from "../errors.js";

/**
 * In-memory store for prompt timestamps per session.
 * Used to compute time-since-prompt for AI scoring.
 */
const promptTimestamps: Map<string, { timestamp: string; charCount: number }> =
  new Map();

/**
 * Authorship ledger: tracks AI vs. human authorship of code changes.
 */
export class AuthorshipLedger {
  private db: AnalyticsDB;

  constructor(db: AnalyticsDB) {
    this.db = db;
  }

  /**
   * Record the timestamp and character count of a user prompt.
   *
   * This data is used by computeAIScore to estimate the time gap
   * between prompt submission and the resulting tool call.
   *
   * Privacy: only timestamp and char count are stored, never content.
   *
   * @param sessionId - The session that submitted the prompt
   * @param timestamp - ISO 8601 timestamp of the prompt
   * @param charCount - Number of characters in the prompt (not content)
   */
  recordPromptTimestamp(
    sessionId: string,
    timestamp: string,
    charCount: number
  ): void {
    promptTimestamps.set(sessionId, { timestamp, charCount });
    logDebug("Recorded prompt timestamp", { sessionId, charCount });
  }

  /**
   * Compute the AI confidence score for a code change.
   *
   * Uses timing heuristics relative to the most recent user prompt:
   * - < 10s AND > 50 lines → 0.95 (high_probability_ai)
   * - < 10s AND 10-50 lines → 0.7 (likely_ai)
   * - > 30s OR < 5 lines → 0.3 (mixed_verified)
   * - Edit AND < 3 lines → 0.1 (human_authored)
   *
   * If no prior prompt is recorded, defaults to mixed_verified (0.3).
   *
   * @param sessionId - Session context
   * @param toolName - The tool that made the change (e.g., "Write", "Edit", "Bash")
   * @param linesChanged - Total lines added + removed
   * @param timestamp - ISO 8601 timestamp of the tool call
   * @param filePath - Optional file path for the change
   */
  computeAIScore(
    sessionId: string,
    toolName: string,
    linesChanged: number,
    timestamp: string,
    filePath?: string
  ): AIConfidenceScore {
    // Special case: Edit with < 3 lines is almost certainly human
    if (toolName === "Edit" && linesChanged < 3) {
      return { score: 0.1, classification: "human_authored" };
    }

    // Get time since last prompt
    const promptData = promptTimestamps.get(sessionId);
    if (!promptData) {
      // No prompt recorded — default to mixed
      return { score: 0.3, classification: "mixed_verified" };
    }

    const promptTime = new Date(promptData.timestamp).getTime();
    const toolTime = new Date(timestamp).getTime();
    const timeSincePromptSeconds = (toolTime - promptTime) / 1000;

    // Tier 1: Fast response + many lines = high probability AI
    if (timeSincePromptSeconds < 10 && linesChanged > 50) {
      return { score: 0.95, classification: "high_probability_ai" };
    }

    // Tier 2: Fast response + moderate lines = likely AI
    if (timeSincePromptSeconds < 10 && linesChanged >= 10) {
      return { score: 0.7, classification: "likely_ai" };
    }

    // Tier 3: Slow response OR few lines = mixed/verified
    if (timeSincePromptSeconds > 30 || linesChanged < 5) {
      return { score: 0.3, classification: "mixed_verified" };
    }

    // Default: moderate time + moderate lines = likely AI (lower confidence)
    return { score: 0.6, classification: "likely_ai" };
  }

  /**
   * Record a code change in the authorship ledger with AI confidence scoring.
   *
   * Computes the AI score and inserts an entry into the authorship_ledger table.
   *
   * @param sessionId - Session context
   * @param toolName - The tool that made the change
   * @param linesChanged - Total lines changed
   * @param timestamp - ISO 8601 timestamp
   * @param filePath - File that was modified
   */
  recordChange(
    sessionId: string,
    toolName: string,
    linesChanged: number,
    timestamp: string,
    filePath: string
  ): AuthorshipEntry {
    const score = this.computeAIScore(
      sessionId,
      toolName,
      linesChanged,
      timestamp,
      filePath
    );

    const entry: AuthorshipEntry = {
      sessionId,
      filePath,
      toolName,
      linesChanged,
      aiScore: score.score,
      classification: score.classification,
      timestamp,
    };

    try {
      const stmts = this.db.getStatements();
      stmts.insertAuthorshipEntry.run({
        sessionId: entry.sessionId,
        filePath: entry.filePath,
        toolName: entry.toolName,
        linesChanged: entry.linesChanged,
        aiScore: entry.aiScore,
        classification: entry.classification,
        timestamp: entry.timestamp,
      });

      logDebug("Recorded authorship entry", {
        sessionId,
        filePath,
        aiScore: score.score,
        classification: score.classification,
      });
    } catch (error) {
      logError(
        error instanceof Error ? error : new Error(String(error)),
        { context: "AuthorshipLedger.recordChange" }
      );
    }

    return entry;
  }

  /**
   * Get the weighted average AI score for a session.
   *
   * Each entry is weighted by the number of lines changed.
   * Returns 0 if no entries exist for the session.
   *
   * @param sessionId - Session to query
   */
  getSessionAIRatio(sessionId: string): number {
    try {
      const rawDb = this.db.getDatabase();
      const result = rawDb
        .prepare(
          `SELECT SUM(ai_score * lines_changed) as weighted_sum, SUM(lines_changed) as total_lines
           FROM authorship_ledger WHERE session_id = ?`
        )
        .get(sessionId) as
        | { weighted_sum: number | null; total_lines: number | null }
        | undefined;

      if (!result || !result.total_lines || result.total_lines === 0) {
        return 0;
      }

      return result.weighted_sum! / result.total_lines;
    } catch (error) {
      logError(
        error instanceof Error ? error : new Error(String(error)),
        { context: "AuthorshipLedger.getSessionAIRatio" }
      );
      return 0;
    }
  }

  /**
   * Clear the in-memory prompt timestamp cache.
   * Useful for testing.
   */
  static clearPromptCache(): void {
    promptTimestamps.clear();
  }
}
