import { describe, it, expect, beforeEach, afterEach } from "vitest";
import {
  mkdtempSync,
  rmSync,
  mkdirSync,
  copyFileSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import {
  aggregateInsights,
  createInsightsProducer,
} from "../../../../src/core/feeds/producers/insights.js";
import type { InsightsFeedConfig } from "../../../../src/core/types.js";

const FIXTURES_DIR = join(
  __dirname,
  "..",
  "..",
  "..",
  "fixtures",
  "usage-data",
);

function makeConfig(overrides?: Partial<InsightsFeedConfig>): InsightsFeedConfig {
  return {
    enabled: true,
    intervalSeconds: 120,
    stalenessDays: 30,
    usageDataPath: "",
    ...overrides,
  };
}

/**
 * Copy fixture files to a temp directory for isolated testing.
 * Accepts a list of session-meta filenames and facets filenames to include.
 */
function setupFixtures(
  tempRoot: string,
  sessionFiles: string[] = [],
  facetsFiles: string[] = [],
): string {
  const usageDir = join(tempRoot, "usage-data");
  const sessionMetaDir = join(usageDir, "session-meta");
  const facetsDir = join(usageDir, "facets");
  mkdirSync(sessionMetaDir, { recursive: true });
  mkdirSync(facetsDir, { recursive: true });

  for (const file of sessionFiles) {
    copyFileSync(
      join(FIXTURES_DIR, "session-meta", file),
      join(sessionMetaDir, file),
    );
  }

  for (const file of facetsFiles) {
    copyFileSync(
      join(FIXTURES_DIR, "facets", file),
      join(facetsDir, file),
    );
  }

  return usageDir;
}

// Use a "now" that makes fresh-clean and fresh-friction non-stale
// but stale.json IS stale (start_time: 2025-12-01)
const NOW = new Date("2026-02-23T12:00:00Z").getTime();

describe("aggregateInsights", () => {
  let tempRoot: string;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-insights-"));
  });

  afterEach(() => {
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("aggregates correctly from multiple fresh fixture files", () => {
    const usageDir = setupFixtures(
      tempRoot,
      ["fresh-clean.json", "fresh-friction.json", "minimal.json"],
      ["fresh-clean-001.json", "fresh-friction-002.json"],
    );

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).not.toBeNull();
    expect(result!.total_sessions).toBe(3);
    // fresh-clean: 12, fresh-friction: 25, minimal: 3
    expect(result!.total_messages).toBe(40);
    // fresh-clean: 340, fresh-friction: 580, minimal: 20
    expect(result!.total_lines_added).toBe(940);
    expect(result!.days_active).toBe(3); // Feb 22, Feb 21, Feb 20
  });

  it("excludes sessions older than staleness window", () => {
    const usageDir = setupFixtures(
      tempRoot,
      ["fresh-clean.json", "stale.json"],
      ["fresh-clean-001.json"],
    );

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).not.toBeNull();
    // Only fresh-clean should be included
    expect(result!.total_sessions).toBe(1);
    expect(result!.total_messages).toBe(12);
  });

  it("handles malformed JSON files gracefully (skip and continue)", () => {
    const usageDir = setupFixtures(
      tempRoot,
      ["fresh-clean.json", "malformed.json"],
      ["fresh-clean-001.json"],
    );

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).not.toBeNull();
    expect(result!.total_sessions).toBe(1);
    expect(result!.total_messages).toBe(12);
  });

  it("returns null when usage-data directory does not exist", () => {
    const result = aggregateInsights(
      join(tempRoot, "nonexistent"),
      30,
      NOW,
    );
    expect(result).toBeNull();
  });

  it("returns null when session-meta directory is empty", () => {
    const usageDir = join(tempRoot, "usage-data");
    mkdirSync(join(usageDir, "session-meta"), { recursive: true });
    mkdirSync(join(usageDir, "facets"), { recursive: true });

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).toBeNull();
  });

  it("returns null when all sessions are stale", () => {
    const usageDir = setupFixtures(tempRoot, ["stale.json"], []);

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).toBeNull();
  });

  it("merges friction_counts from matching facets", () => {
    const usageDir = setupFixtures(
      tempRoot,
      ["fresh-friction.json"],
      ["fresh-friction-002.json"],
    );

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).not.toBeNull();
    expect(result!.friction_counts).toEqual({
      wrong_approach: 3,
      misunderstood_request: 2,
    });
    expect(result!.friction_total).toBe(5);
  });

  it("handles sessions without matching facets (empty friction)", () => {
    const usageDir = setupFixtures(
      tempRoot,
      ["fresh-clean.json", "minimal.json"],
      [], // no facets at all
    );

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).not.toBeNull();
    expect(result!.total_sessions).toBe(2);
    expect(result!.friction_counts).toEqual({});
    expect(result!.friction_total).toBe(0);
  });

  it("computes top_tools correctly (sorted, top 5)", () => {
    const usageDir = setupFixtures(
      tempRoot,
      ["fresh-clean.json", "fresh-friction.json"],
      [],
    );

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).not.toBeNull();
    // Combined tool counts: Bash: 28, Read: 45, Edit: 18, Glob: 3, Write: 5, Grep: 8
    expect(result!.top_tools[0].name).toBe("Read");
    expect(result!.top_tools[0].count).toBe(45);
    expect(result!.top_tools[1].name).toBe("Bash");
    expect(result!.top_tools[1].count).toBe(28);
    expect(result!.top_tools.length).toBeLessThanOrEqual(5);
  });

  it("computes peak_hour correctly (UTC→local conversion)", () => {
    const usageDir = setupFixtures(
      tempRoot,
      ["fresh-clean.json", "fresh-friction.json"],
      [],
    );

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).not.toBeNull();
    // fresh-clean: hours 14,14,14,14,14,14,15,15,15,15,15,15 → 14:6, 15:6
    // fresh-friction: hours 10:5, 11:20
    // Total: 10:5, 11:20, 14:6, 15:6 → UTC peak = 11
    // Converted to local time using system timezone offset
    const offsetMinutes = new Date(NOW).getTimezoneOffset();
    const localPeakMinutes = (11 * 60 - offsetMinutes + 24 * 60) % (24 * 60);
    const expectedLocalPeak = Math.floor(localPeakMinutes / 60);
    expect(result!.peak_hour).toBe(expectedLocalPeak);
  });

  it("identifies recent_session correctly (latest start_time)", () => {
    const usageDir = setupFixtures(
      tempRoot,
      ["fresh-clean.json", "fresh-friction.json", "minimal.json"],
      ["fresh-clean-001.json"],
    );

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).not.toBeNull();
    // fresh-clean: 2026-02-22 (most recent), fresh-friction: 2026-02-21, minimal: 2026-02-20
    expect(result!.recent_session.id).toBe("fresh-clean-001");
    expect(result!.recent_session.duration_minutes).toBe(45);
    expect(result!.recent_session.lines_added).toBe(340);
  });

  it("recent_session includes friction from matching facets", () => {
    const usageDir = setupFixtures(
      tempRoot,
      ["fresh-friction.json"],
      ["fresh-friction-002.json"],
    );

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).not.toBeNull();
    expect(result!.recent_session.id).toBe("fresh-friction-002");
    expect(result!.recent_session.friction_count).toBe(5);
    expect(result!.recent_session.outcome).toBe("partially_achieved");
    expect(result!.recent_session.tool_errors).toBe(4);
  });

  it("respects configurable stalenessDays", () => {
    const usageDir = setupFixtures(
      tempRoot,
      ["fresh-clean.json", "stale.json"],
      [],
    );

    // With a 365-day window, the stale fixture (2025-12-01) should be included
    const result = aggregateInsights(usageDir, 365, NOW);
    expect(result).not.toBeNull();
    expect(result!.total_sessions).toBe(2);

    // With a 1-day window, even fresh-clean (2026-02-22) might be excluded
    // depending on timing. Let's use a super-narrow window.
    const narrow = aggregateInsights(usageDir, 1, NOW);
    // fresh-clean is ~1 day old from NOW, may or may not be included
    // stale is definitely excluded
    if (narrow) {
      expect(narrow.total_sessions).toBeLessThanOrEqual(1);
    }
  });

  it("computes avg_duration_minutes correctly", () => {
    const usageDir = setupFixtures(
      tempRoot,
      ["fresh-clean.json", "fresh-friction.json"],
      [],
    );

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).not.toBeNull();
    // fresh-clean: 45, fresh-friction: 90 → avg = 67.5
    expect(result!.avg_duration_minutes).toBe(67.5);
  });

  it("handles minimal session with only required fields", () => {
    const usageDir = setupFixtures(tempRoot, ["minimal.json"], []);

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).not.toBeNull();
    expect(result!.total_sessions).toBe(1);
    expect(result!.total_messages).toBe(3);
    expect(result!.total_lines_added).toBe(20);
    expect(result!.top_tools).toEqual([]);
    expect(result!.recent_session.id).toBe("minimal-005");
  });

  it("days_active uses local timezone, not UTC date boundaries (RC-1)", () => {
    // Create two sessions near UTC midnight that fall on different UTC dates
    // but the SAME local date for UTC-5 timezone.
    // Session 1: 2026-02-22T04:30:00Z (Feb 22 UTC, but Feb 21 at UTC-5)
    // Session 2: 2026-02-22T03:00:00Z (Feb 22 UTC, but Feb 21 at UTC-5)
    // Without local conversion, these are both Feb 22 UTC → 1 day.
    // With proper conversion for UTC-5, both are Feb 21 local → still 1 day.
    // The key test: a session at 2026-02-21T23:30:00Z (Feb 21 UTC)
    // is Feb 21 at UTC-5 too → same day.
    // But a session at 2026-02-22T06:00:00Z (Feb 22 UTC) is also Feb 22 at UTC-5 → different day.

    // We use writeFileSync to create custom sessions with specific timestamps
    const usageDir = join(tempRoot, "usage-data-tz");
    const sessionMetaDir = join(usageDir, "session-meta");
    const facetsDir = join(usageDir, "facets");
    mkdirSync(sessionMetaDir, { recursive: true });
    mkdirSync(facetsDir, { recursive: true });

    // Two sessions both on Feb 22 UTC but potentially different local dates
    const session1 = {
      session_id: "tz-test-1",
      start_time: "2026-02-22T00:30:00Z", // Feb 22 UTC, Feb 21 local (UTC-5 to UTC-1)
      user_message_count: 5,
      lines_added: 10,
      duration_minutes: 10,
    };
    const session2 = {
      session_id: "tz-test-2",
      start_time: "2026-02-22T14:00:00Z", // Feb 22 UTC and Feb 22 local for most timezones
      user_message_count: 5,
      lines_added: 10,
      duration_minutes: 10,
    };

    writeFileSync(join(sessionMetaDir, "tz-1.json"), JSON.stringify(session1));
    writeFileSync(join(sessionMetaDir, "tz-2.json"), JSON.stringify(session2));

    const testNow = new Date("2026-02-23T12:00:00Z").getTime();
    const result = aggregateInsights(usageDir, 30, testNow);
    expect(result).not.toBeNull();

    // The key assertion: days_active should use local timezone conversion.
    // We verify by computing expected days the same way the code does.
    const offsetMinutes = new Date(testNow).getTimezoneOffset();
    const localDate1 = new Date(Date.parse("2026-02-22T00:30:00Z") - offsetMinutes * 60000);
    const localDate2 = new Date(Date.parse("2026-02-22T14:00:00Z") - offsetMinutes * 60000);
    const expectedDates = new Set([
      localDate1.toISOString().slice(0, 10),
      localDate2.toISOString().slice(0, 10),
    ]);
    expect(result!.days_active).toBe(expectedDates.size);
  });

  it("includes staleness_days and recent_msgs_per_day in output (RC-2)", () => {
    const usageDir = setupFixtures(
      tempRoot,
      ["fresh-clean.json", "fresh-friction.json"],
      [],
    );

    const result = aggregateInsights(usageDir, 30, NOW);
    expect(result).not.toBeNull();
    expect(result!.staleness_days).toBe(30);
    expect(typeof result!.recent_msgs_per_day).toBe("number");
    expect(typeof result!.recent_messages).toBe("number");
    expect(typeof result!.recent_days_active).toBe("number");
  });
});

describe("createInsightsProducer", () => {
  let tempRoot: string;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-insights-producer-"));
  });

  afterEach(() => {
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("returns a FeedProducer function", () => {
    const config = makeConfig({ usageDataPath: join(tempRoot, "nope") });
    const producer = createInsightsProducer(config);
    expect(typeof producer).toBe("function");
  });

  it("producer returns null when no data exists", async () => {
    const config = makeConfig({ usageDataPath: join(tempRoot, "nope") });
    const producer = createInsightsProducer(config);
    const result = await producer();
    expect(result).toBeNull();
  });

  it("producer returns aggregated data from fixtures", async () => {
    const usageDir = setupFixtures(
      tempRoot,
      ["fresh-clean.json"],
      ["fresh-clean-001.json"],
    );

    const config = makeConfig({ usageDataPath: usageDir });
    const producer = createInsightsProducer(config);
    const result = await producer();
    expect(result).not.toBeNull();
    expect((result as Record<string, unknown>).total_sessions).toBe(1);
  });
});
