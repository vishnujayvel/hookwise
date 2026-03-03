/**
 * Tests for Feed Platform status line segments (v1.1).
 *
 * Covers Tasks 5.1 and 5.2:
 *
 * Task 5.1 (New segment renderers):
 * - pulse: renders emoji, empty on missing, empty on stale
 * - project: formats repo/branch/age, relative time ranges, detached HEAD, empty on missing
 * - calendar: proximity thresholds (>60min, 15-60, 5-15, <5, during event),
 *   multiple events (+N more), no events (Free), empty on stale
 * - news: truncation at 45 chars, RSS (score=0) omits score, short title no ellipsis,
 *   empty on missing/stale
 *
 * Task 5.2 (TTL freshness):
 * - All 4 segments return "" when cache entry is stale (TTL expired)
 *
 * Requirements: FR-8.1, FR-8.2, FR-8.3, FR-8.4, FR-8.5
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { BUILTIN_SEGMENTS } from "../../../src/core/status-line/segments.js";

// Fixed "now" for deterministic tests
const NOW = new Date("2026-02-22T12:00:00Z").getTime();

/**
 * Helper: create a fresh cache entry with given data.
 */
function freshEntry(data: Record<string, unknown>) {
  return {
    updated_at: new Date(NOW - 5_000).toISOString(), // 5s ago
    ttl_seconds: 60,
    ...data,
  };
}

/**
 * Helper: create a stale cache entry with given data.
 */
function staleEntry(data: Record<string, unknown>) {
  return {
    updated_at: new Date(NOW - 120_000).toISOString(), // 2 min ago
    ttl_seconds: 60, // TTL expired
    ...data,
  };
}

describe("pulse segment", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders the emoji value directly", () => {
    const cache = { pulse: freshEntry({ value: "\uD83D\uDFE2" }) };
    const result = BUILTIN_SEGMENTS.pulse(cache, {});
    expect(result).toBe("\uD83D\uDFE2");
  });

  it("renders a different emoji value", () => {
    const cache = { pulse: freshEntry({ value: "\uD83D\uDD34" }) };
    const result = BUILTIN_SEGMENTS.pulse(cache, {});
    expect(result).toBe("\uD83D\uDD34");
  });

  it("returns empty string when pulse is missing from cache", () => {
    const result = BUILTIN_SEGMENTS.pulse({}, {});
    expect(result).toBe("");
  });

  it("returns empty string when value is missing", () => {
    const cache = { pulse: freshEntry({}) };
    const result = BUILTIN_SEGMENTS.pulse(cache, {});
    expect(result).toBe("");
  });

  it("returns empty string when cache entry is stale", () => {
    const cache = { pulse: staleEntry({ value: "\uD83D\uDFE2" }) };
    const result = BUILTIN_SEGMENTS.pulse(cache, {});
    expect(result).toBe("");
  });

  it("returns empty string when value is empty string", () => {
    const cache = { pulse: freshEntry({ value: "" }) };
    const result = BUILTIN_SEGMENTS.pulse(cache, {});
    expect(result).toBe("");
  });
});

describe("project segment", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("formats repo, branch, and commit age", () => {
    const lastCommitTs = Math.floor(NOW / 1000) - 300; // 5 minutes ago
    const cache = {
      project: freshEntry({
        repo: "hookwise",
        branch: "main",
        last_commit_ts: lastCommitTs,
        detached: false,
        has_commits: true,
      }),
    };
    const result = BUILTIN_SEGMENTS.project(cache, {});
    expect(result).toBe("\uD83D\uDCE6 hookwise (main) \u2022 5m ago");
  });

  it("formats relative time in minutes", () => {
    const lastCommitTs = Math.floor(NOW / 1000) - 45 * 60; // 45 minutes ago
    const cache = {
      project: freshEntry({
        repo: "myapp",
        branch: "feature",
        last_commit_ts: lastCommitTs,
      }),
    };
    const result = BUILTIN_SEGMENTS.project(cache, {});
    expect(result).toContain("45m ago");
  });

  it("formats relative time in hours", () => {
    const lastCommitTs = Math.floor(NOW / 1000) - 3 * 3600; // 3 hours ago
    const cache = {
      project: freshEntry({
        repo: "myapp",
        branch: "main",
        last_commit_ts: lastCommitTs,
      }),
    };
    const result = BUILTIN_SEGMENTS.project(cache, {});
    expect(result).toContain("3h ago");
  });

  it("formats relative time in days", () => {
    const lastCommitTs = Math.floor(NOW / 1000) - 2 * 86400; // 2 days ago
    const cache = {
      project: freshEntry({
        repo: "myapp",
        branch: "main",
        last_commit_ts: lastCommitTs,
      }),
    };
    const result = BUILTIN_SEGMENTS.project(cache, {});
    expect(result).toContain("2d ago");
  });

  it("shows detached HEAD", () => {
    const lastCommitTs = Math.floor(NOW / 1000) - 60;
    const cache = {
      project: freshEntry({
        repo: "hookwise",
        branch: "abc1234",
        last_commit_ts: lastCommitTs,
        detached: true,
      }),
    };
    const result = BUILTIN_SEGMENTS.project(cache, {});
    expect(result).toContain("(detached)");
    expect(result).not.toContain("abc1234");
  });

  it("returns empty string when project is missing from cache", () => {
    const result = BUILTIN_SEGMENTS.project({}, {});
    expect(result).toBe("");
  });

  it("returns empty string when repo is missing", () => {
    const cache = {
      project: freshEntry({ branch: "main" }),
    };
    const result = BUILTIN_SEGMENTS.project(cache, {});
    expect(result).toBe("");
  });

  it("returns empty string when cache entry is stale", () => {
    const cache = {
      project: staleEntry({
        repo: "hookwise",
        branch: "main",
        last_commit_ts: Math.floor(NOW / 1000) - 60,
      }),
    };
    const result = BUILTIN_SEGMENTS.project(cache, {});
    expect(result).toBe("");
  });

  it("omits age when last_commit_ts is missing", () => {
    const cache = {
      project: freshEntry({
        repo: "hookwise",
        branch: "main",
      }),
    };
    const result = BUILTIN_SEGMENTS.project(cache, {});
    expect(result).toBe("\uD83D\uDCE6 hookwise (main)");
    expect(result).not.toContain("\u2022");
  });
});

describe("calendar segment", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows 'Free for Xh' when next event is >60min away", () => {
    const start = new Date(NOW + 90 * 60_000).toISOString(); // 90 min from now
    const end = new Date(NOW + 120 * 60_000).toISOString();
    const cache = {
      calendar: freshEntry({
        events: [{ title: "Standup", start, end, is_current: false }],
        next_event: { title: "Standup", start, end, is_current: false },
      }),
    };
    const result = BUILTIN_SEGMENTS.calendar(cache, {});
    // 90 min rounds to 2h
    expect(result).toBe("\uD83D\uDCC5 Free for 2h");
  });

  it("shows 'title in Xmin' when next event is 15-60min away", () => {
    const start = new Date(NOW + 30 * 60_000).toISOString(); // 30 min from now
    const end = new Date(NOW + 60 * 60_000).toISOString();
    const cache = {
      calendar: freshEntry({
        events: [{ title: "1:1 Meeting", start, end, is_current: false }],
        next_event: { title: "1:1 Meeting", start, end, is_current: false },
      }),
    };
    const result = BUILTIN_SEGMENTS.calendar(cache, {});
    expect(result).toBe("\uD83D\uDCC5 1:1 Meeting in 30min");
  });

  it("shows 'title in Xmin ⚡' when next event is 5-15min away", () => {
    const start = new Date(NOW + 10 * 60_000).toISOString(); // 10 min from now
    const end = new Date(NOW + 40 * 60_000).toISOString();
    const cache = {
      calendar: freshEntry({
        events: [{ title: "Design Review", start, end, is_current: false }],
        next_event: { title: "Design Review", start, end, is_current: false },
      }),
    };
    const result = BUILTIN_SEGMENTS.calendar(cache, {});
    expect(result).toBe("\uD83D\uDCC5 Design Review in 10min \u26A1");
  });

  it("shows 'title NOW' when next event is <5min away", () => {
    const start = new Date(NOW + 3 * 60_000).toISOString(); // 3 min from now
    const end = new Date(NOW + 33 * 60_000).toISOString();
    const cache = {
      calendar: freshEntry({
        events: [{ title: "Standup", start, end, is_current: false }],
        next_event: { title: "Standup", start, end, is_current: false },
      }),
    };
    const result = BUILTIN_SEGMENTS.calendar(cache, {});
    expect(result).toBe("\uD83D\uDCC5 Standup NOW");
  });

  it("shows current event with 'ends in Xmin'", () => {
    const start = new Date(NOW - 20 * 60_000).toISOString(); // started 20 min ago
    const end = new Date(NOW + 10 * 60_000).toISOString(); // ends in 10 min
    const cache = {
      calendar: freshEntry({
        events: [{ title: "Sprint Planning", start, end, is_current: true }],
        next_event: null,
      }),
    };
    const result = BUILTIN_SEGMENTS.calendar(cache, {});
    expect(result).toBe("\uD83D\uDCC5 Sprint Planning (ends in 10min)");
  });

  it("shows (+N more) with multiple events and a current event", () => {
    const start1 = new Date(NOW - 10 * 60_000).toISOString();
    const end1 = new Date(NOW + 20 * 60_000).toISOString();
    const start2 = new Date(NOW + 60 * 60_000).toISOString();
    const end2 = new Date(NOW + 90 * 60_000).toISOString();
    const start3 = new Date(NOW + 120 * 60_000).toISOString();
    const end3 = new Date(NOW + 150 * 60_000).toISOString();
    const cache = {
      calendar: freshEntry({
        events: [
          { title: "Current Meeting", start: start1, end: end1, is_current: true },
          { title: "Next Meeting", start: start2, end: end2, is_current: false },
          { title: "Later Meeting", start: start3, end: end3, is_current: false },
        ],
        next_event: { title: "Next Meeting", start: start2, end: end2, is_current: false },
      }),
    };
    const result = BUILTIN_SEGMENTS.calendar(cache, {});
    expect(result).toBe("\uD83D\uDCC5 Current Meeting (ends in 20min) (+2 more)");
  });

  it("shows (+N more) with multiple events and no current event", () => {
    const start1 = new Date(NOW + 30 * 60_000).toISOString();
    const end1 = new Date(NOW + 60 * 60_000).toISOString();
    const start2 = new Date(NOW + 90 * 60_000).toISOString();
    const end2 = new Date(NOW + 120 * 60_000).toISOString();
    const cache = {
      calendar: freshEntry({
        events: [
          { title: "Meeting A", start: start1, end: end1, is_current: false },
          { title: "Meeting B", start: start2, end: end2, is_current: false },
        ],
        next_event: { title: "Meeting A", start: start1, end: end1, is_current: false },
      }),
    };
    const result = BUILTIN_SEGMENTS.calendar(cache, {});
    expect(result).toBe("\uD83D\uDCC5 Meeting A in 30min (+1 more)");
  });

  it("shows 'Free' when no events and no next_event", () => {
    const cache = {
      calendar: freshEntry({
        events: [],
        next_event: null,
      }),
    };
    const result = BUILTIN_SEGMENTS.calendar(cache, {});
    expect(result).toBe("\uD83D\uDCC5 Free");
  });

  it("returns empty string when calendar is missing from cache", () => {
    const result = BUILTIN_SEGMENTS.calendar({}, {});
    expect(result).toBe("");
  });

  it("returns empty string when cache entry is stale", () => {
    const start = new Date(NOW + 30 * 60_000).toISOString();
    const end = new Date(NOW + 60 * 60_000).toISOString();
    const cache = {
      calendar: staleEntry({
        events: [{ title: "Standup", start, end, is_current: false }],
        next_event: { title: "Standup", start, end, is_current: false },
      }),
    };
    const result = BUILTIN_SEGMENTS.calendar(cache, {});
    expect(result).toBe("");
  });

  it("boundary: exactly 5 minutes shows lightning bolt", () => {
    const start = new Date(NOW + 5 * 60_000).toISOString();
    const end = new Date(NOW + 35 * 60_000).toISOString();
    const cache = {
      calendar: freshEntry({
        events: [{ title: "Test", start, end, is_current: false }],
        next_event: { title: "Test", start, end, is_current: false },
      }),
    };
    const result = BUILTIN_SEGMENTS.calendar(cache, {});
    expect(result).toBe("\uD83D\uDCC5 Test in 5min \u26A1");
  });

  it("boundary: exactly 15 minutes shows title in Xmin (no bolt)", () => {
    const start = new Date(NOW + 15 * 60_000).toISOString();
    const end = new Date(NOW + 45 * 60_000).toISOString();
    const cache = {
      calendar: freshEntry({
        events: [{ title: "Test", start, end, is_current: false }],
        next_event: { title: "Test", start, end, is_current: false },
      }),
    };
    const result = BUILTIN_SEGMENTS.calendar(cache, {});
    expect(result).toBe("\uD83D\uDCC5 Test in 15min");
  });

  it("boundary: exactly 60 minutes shows title in Xmin", () => {
    const start = new Date(NOW + 60 * 60_000).toISOString();
    const end = new Date(NOW + 90 * 60_000).toISOString();
    const cache = {
      calendar: freshEntry({
        events: [{ title: "Test", start, end, is_current: false }],
        next_event: { title: "Test", start, end, is_current: false },
      }),
    };
    const result = BUILTIN_SEGMENTS.calendar(cache, {});
    expect(result).toBe("\uD83D\uDCC5 Test in 60min");
  });
});

describe("news segment", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders story with title and score", () => {
    const cache = {
      news: freshEntry({
        current_story: {
          title: "Show HN: Cool Project",
          score: 142,
          url: "https://example.com",
          id: 1,
        },
      }),
    };
    const result = BUILTIN_SEGMENTS.news(cache, {});
    expect(result).toBe("\uD83D\uDCF0 Show HN: Cool Project (142pts)");
  });

  it("truncates title at 45 characters with ellipsis", () => {
    const longTitle = "This is a very long title that exceeds the forty-five character limit";
    expect(longTitle.length).toBeGreaterThan(45);
    const cache = {
      news: freshEntry({
        current_story: {
          title: longTitle,
          score: 100,
          url: "https://example.com",
          id: 1,
        },
      }),
    };
    const result = BUILTIN_SEGMENTS.news(cache, {});
    expect(result).toBe(`\uD83D\uDCF0 ${longTitle.slice(0, 45)}\u2026 (100pts)`);
  });

  it("does not add ellipsis for title exactly 45 characters", () => {
    const exactTitle = "A".repeat(45);
    const cache = {
      news: freshEntry({
        current_story: {
          title: exactTitle,
          score: 50,
          url: "https://example.com",
          id: 1,
        },
      }),
    };
    const result = BUILTIN_SEGMENTS.news(cache, {});
    expect(result).toBe(`\uD83D\uDCF0 ${exactTitle} (50pts)`);
    expect(result).not.toContain("\u2026");
  });

  it("does not add ellipsis for short title", () => {
    const cache = {
      news: freshEntry({
        current_story: {
          title: "Short",
          score: 10,
          url: "https://example.com",
          id: 1,
        },
      }),
    };
    const result = BUILTIN_SEGMENTS.news(cache, {});
    expect(result).toBe("\uD83D\uDCF0 Short (10pts)");
    expect(result).not.toContain("\u2026");
  });

  it("omits score for RSS items (score=0)", () => {
    const cache = {
      news: freshEntry({
        current_story: {
          title: "RSS Feed Item Title",
          score: 0,
          url: "https://example.com/feed",
          id: 42,
        },
      }),
    };
    const result = BUILTIN_SEGMENTS.news(cache, {});
    expect(result).toBe("\uD83D\uDCF0 RSS Feed Item Title");
    expect(result).not.toContain("pts");
  });

  it("returns empty string when news is missing from cache", () => {
    const result = BUILTIN_SEGMENTS.news({}, {});
    expect(result).toBe("");
  });

  it("returns empty string when current_story is missing", () => {
    const cache = {
      news: freshEntry({}),
    };
    const result = BUILTIN_SEGMENTS.news(cache, {});
    expect(result).toBe("");
  });

  it("returns empty string when cache entry is stale", () => {
    const cache = {
      news: staleEntry({
        current_story: {
          title: "Stale Story",
          score: 100,
          url: "https://example.com",
          id: 1,
        },
      }),
    };
    const result = BUILTIN_SEGMENTS.news(cache, {});
    expect(result).toBe("");
  });
});

describe("BUILTIN_SEGMENTS registry", () => {
  it("has all segments registered (original + two-tier + feed + insights)", () => {
    const expected = [
      "clock", "mantra", "builder_trap", "session", "practice", "ai_ratio", "cost", "streak",
      "context_bar", "mode_badge", "duration", "practice_breadcrumb",
      "pulse", "project", "calendar", "news",
      "insights_friction", "insights_pace", "insights_trend",
      "weather", "memories",
    ];
    for (const name of expected) {
      expect(BUILTIN_SEGMENTS[name]).toBeDefined();
      expect(typeof BUILTIN_SEGMENTS[name]).toBe("function");
    }
    expect(Object.keys(BUILTIN_SEGMENTS)).toHaveLength(21);
  });
});
