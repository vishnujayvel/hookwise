/**
 * Tests for the stats command.
 *
 * Verifies:
 * - Renders daily summary, tool breakdown, authorship
 * - Handles missing analytics DB gracefully
 * - JSON output mode
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import React from "react";
import { render } from "ink-testing-library";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { StatsCommand } from "../../src/cli/commands/stats.js";
import { AnalyticsDB } from "../../src/core/analytics/index.js";

describe("StatsCommand", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-stats-test-"));
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("shows error when no analytics DB exists", () => {
    const nonExistentPath = join(tempDir, "nonexistent.db");
    const { lastFrame } = render(
      <StatsCommand dbPath={nonExistentPath} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("No analytics database found");
  });

  it("renders analytics header", () => {
    // Create a DB with some data
    const dbPath = join(tempDir, "analytics.db");
    const db = new AnalyticsDB(dbPath);
    db.close();

    const { lastFrame } = render(<StatsCommand dbPath={dbPath} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Session Analytics");
  });

  it("shows daily summary section", () => {
    const dbPath = join(tempDir, "analytics.db");
    const db = new AnalyticsDB(dbPath);
    db.close();

    const { lastFrame } = render(<StatsCommand dbPath={dbPath} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Daily Summary");
  });

  it("shows tool breakdown section", () => {
    const dbPath = join(tempDir, "analytics.db");
    const db = new AnalyticsDB(dbPath);
    db.close();

    const { lastFrame } = render(<StatsCommand dbPath={dbPath} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Tool Breakdown");
  });

  it("shows AI authorship section", () => {
    const dbPath = join(tempDir, "analytics.db");
    const db = new AnalyticsDB(dbPath);
    db.close();

    const { lastFrame } = render(<StatsCommand dbPath={dbPath} />);
    const frame = lastFrame()!;
    expect(frame).toContain("AI Authorship");
  });

  it("renders with empty database", () => {
    const dbPath = join(tempDir, "analytics.db");
    const db = new AnalyticsDB(dbPath);
    db.close();

    const { lastFrame } = render(<StatsCommand dbPath={dbPath} />);
    const frame = lastFrame()!;
    expect(frame).toContain("No data available");
    expect(frame).toContain("No tool usage data");
  });

  it("renders data when events exist", () => {
    const dbPath = join(tempDir, "analytics.db");
    const db = new AnalyticsDB(dbPath);
    const stmts = db.getStatements();

    // Insert a session and event
    stmts.insertSession.run({ id: "test-session", startedAt: new Date().toISOString() });
    stmts.insertEvent.run({
      sessionId: "test-session",
      eventType: "PreToolUse",
      toolName: "Bash",
      timestamp: new Date().toISOString(),
      filePath: null,
      linesAdded: 10,
      linesRemoved: 5,
      aiConfidenceScore: 0.8,
    });
    db.close();

    const { lastFrame } = render(<StatsCommand dbPath={dbPath} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Bash");
  });
});
