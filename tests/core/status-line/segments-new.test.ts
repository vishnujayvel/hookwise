/**
 * Tests for the 4 new two-tier segments:
 * context_bar, mode_badge, duration, practice_breadcrumb.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { BUILTIN_SEGMENTS } from "../../../src/core/status-line/segments.js";

describe("context_bar segment", () => {
  it("renders progress bar from stdin context_window", () => {
    const cache = { _stdin: { context_window: { used_percentage: 67 } } };
    const result = BUILTIN_SEGMENTS.context_bar(cache, {});
    expect(result).toContain("67%");
    // Should have block chars (10-wide bar)
    expect(result).toMatch(/[\u2588\u2591]/);
  });

  it("renders 0% for zero usage", () => {
    const cache = { _stdin: { context_window: { used_percentage: 0 } } };
    const result = BUILTIN_SEGMENTS.context_bar(cache, {});
    expect(result).toContain("0%");
  });

  it("renders 100% for full usage", () => {
    const cache = { _stdin: { context_window: { used_percentage: 100 } } };
    const result = BUILTIN_SEGMENTS.context_bar(cache, {});
    expect(result).toContain("100%");
  });

  it("clamps values above 100", () => {
    const cache = { _stdin: { context_window: { used_percentage: 150 } } };
    const result = BUILTIN_SEGMENTS.context_bar(cache, {});
    expect(result).toContain("100%");
  });

  it("returns empty when no _stdin data", () => {
    const result = BUILTIN_SEGMENTS.context_bar({}, {});
    expect(result).toBe("");
  });

  it("returns empty when context_window is missing", () => {
    const cache = { _stdin: {} };
    const result = BUILTIN_SEGMENTS.context_bar(cache, {});
    expect(result).toBe("");
  });
});

describe("mode_badge segment", () => {
  it("renders mode from builder_trap cache", () => {
    const cache = { builder_trap: { current_mode: "tooling" } };
    const result = BUILTIN_SEGMENTS.mode_badge(cache, {});
    expect(result).toBe("[tooling]");
  });

  it("renders practice mode", () => {
    const cache = { builder_trap: { current_mode: "practice" } };
    const result = BUILTIN_SEGMENTS.mode_badge(cache, {});
    expect(result).toBe("[practice]");
  });

  it("renders prep mode", () => {
    const cache = { builder_trap: { current_mode: "prep" } };
    const result = BUILTIN_SEGMENTS.mode_badge(cache, {});
    expect(result).toBe("[prep]");
  });

  it("renders planning mode", () => {
    const cache = { builder_trap: { current_mode: "planning" } };
    const result = BUILTIN_SEGMENTS.mode_badge(cache, {});
    expect(result).toBe("[planning]");
  });

  it("returns empty when no builder_trap data", () => {
    const result = BUILTIN_SEGMENTS.mode_badge({}, {});
    expect(result).toBe("");
  });

  it("returns empty when current_mode is empty", () => {
    const cache = { builder_trap: { current_mode: "" } };
    const result = BUILTIN_SEGMENTS.mode_badge(cache, {});
    expect(result).toBe("");
  });

  it("returns empty when current_mode is neutral (uninformative)", () => {
    const cache = { builder_trap: { current_mode: "neutral" } };
    const result = BUILTIN_SEGMENTS.mode_badge(cache, {});
    expect(result).toBe("");
  });
});

describe("duration segment", () => {
  it("renders duration from stdin cost data", () => {
    // 45 minutes = 2_700_000 ms
    const cache = { _stdin: { cost: { total_duration_ms: 2_700_000 } } };
    const result = BUILTIN_SEGMENTS.duration(cache, {});
    expect(result).toBe("45m");
  });

  it("renders hours and minutes", () => {
    // 1h30m = 5_400_000 ms
    const cache = { _stdin: { cost: { total_duration_ms: 5_400_000 } } };
    const result = BUILTIN_SEGMENTS.duration(cache, {});
    expect(result).toBe("1h30m");
  });

  it("renders 0m for zero duration", () => {
    const cache = { _stdin: { cost: { total_duration_ms: 0 } } };
    const result = BUILTIN_SEGMENTS.duration(cache, {});
    expect(result).toBe("0m");
  });

  it("returns empty when no _stdin data", () => {
    const result = BUILTIN_SEGMENTS.duration({}, {});
    expect(result).toBe("");
  });

  it("returns empty when cost is missing from stdin", () => {
    const cache = { _stdin: {} };
    const result = BUILTIN_SEGMENTS.duration(cache, {});
    expect(result).toBe("");
  });
});

describe("practice_breadcrumb segment", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-02-23T12:00:00Z"));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders relative time since last practice (hours)", () => {
    // 3 hours ago
    const cache = { practice: { last_at: "2026-02-23T09:00:00Z" } };
    const result = BUILTIN_SEGMENTS.practice_breadcrumb(cache, {});
    expect(result).toBe("Last practice: 3h ago");
  });

  it("renders relative time since last practice (minutes)", () => {
    // 30 minutes ago
    const cache = { practice: { last_at: "2026-02-23T11:30:00Z" } };
    const result = BUILTIN_SEGMENTS.practice_breadcrumb(cache, {});
    expect(result).toBe("Last practice: 30m ago");
  });

  it("renders relative time since last practice (days)", () => {
    // 2 days ago
    const cache = { practice: { last_at: "2026-02-21T12:00:00Z" } };
    const result = BUILTIN_SEGMENTS.practice_breadcrumb(cache, {});
    expect(result).toBe("Last practice: 2d ago");
  });

  it("returns empty when no practice data", () => {
    const result = BUILTIN_SEGMENTS.practice_breadcrumb({}, {});
    expect(result).toBe("");
  });

  it("returns empty when last_at is missing", () => {
    const cache = { practice: {} };
    const result = BUILTIN_SEGMENTS.practice_breadcrumb(cache, {});
    expect(result).toBe("");
  });

  it("returns empty for invalid date", () => {
    const cache = { practice: { last_at: "not-a-date" } };
    const result = BUILTIN_SEGMENTS.practice_breadcrumb(cache, {});
    expect(result).toBe("");
  });
});

describe("BUILTIN_SEGMENTS registry", () => {
  it("includes all 4 new segments", () => {
    const newSegments = ["context_bar", "mode_badge", "duration", "practice_breadcrumb"];
    for (const name of newSegments) {
      expect(BUILTIN_SEGMENTS[name]).toBeDefined();
      expect(typeof BUILTIN_SEGMENTS[name]).toBe("function");
    }
  });

  it("still includes all original 12 segments", () => {
    const original = [
      "clock", "mantra", "builder_trap", "session", "practice",
      "ai_ratio", "cost", "streak", "pulse", "project", "calendar", "news",
    ];
    for (const name of original) {
      expect(BUILTIN_SEGMENTS[name]).toBeDefined();
    }
  });

  it("has 19 total segments", () => {
    expect(Object.keys(BUILTIN_SEGMENTS)).toHaveLength(19);
  });
});
