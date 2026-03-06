/**
 * Tests for the 4 new two-tier segments:
 * context_bar, mode_badge, duration.
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

describe("BUILTIN_SEGMENTS registry", () => {
  it("includes all 3 new segments", () => {
    const newSegments = ["context_bar", "mode_badge", "duration"];
    for (const name of newSegments) {
      expect(BUILTIN_SEGMENTS[name]).toBeDefined();
      expect(typeof BUILTIN_SEGMENTS[name]).toBe("function");
    }
  });

  it("still includes all original segments", () => {
    const original = [
      "clock", "mantra", "builder_trap", "session", "practice",
      "cost", "streak", "pulse", "project", "calendar", "news",
    ];
    for (const name of original) {
      expect(BUILTIN_SEGMENTS[name]).toBeDefined();
    }
  });

  it("has 21 total segments", () => {
    expect(Object.keys(BUILTIN_SEGMENTS)).toHaveLength(21);
  });
});

// --- Insights segment regression tests ---

describe("insights_pace trend arrow (RC-2)", () => {
  const freshEntry = {
    updated_at: new Date().toISOString(),
    ttl_seconds: 9999,
  };

  it("shows up arrow when recent pace exceeds average by >20%", () => {
    const cache = {
      insights: {
        ...freshEntry,
        total_messages: 100,
        days_active: 10,
        total_lines_added: 500,
        total_sessions: 5,
        recent_msgs_per_day: 15, // 15 > 10 * 1.2 = 12
      },
    };
    const result = BUILTIN_SEGMENTS.insights_pace(cache, {});
    expect(result).toContain("\u2191"); // ↑
    expect(result).toContain("10 msgs/day");
  });

  it("shows down arrow when recent pace is below average by >20%", () => {
    const cache = {
      insights: {
        ...freshEntry,
        total_messages: 100,
        days_active: 10,
        total_lines_added: 500,
        total_sessions: 5,
        recent_msgs_per_day: 5, // 5 < 10 * 0.8 = 8
      },
    };
    const result = BUILTIN_SEGMENTS.insights_pace(cache, {});
    expect(result).toContain("\u2193"); // ↓
  });

  it("shows stable arrow when recent pace is within 20% of average", () => {
    const cache = {
      insights: {
        ...freshEntry,
        total_messages: 100,
        days_active: 10,
        total_lines_added: 500,
        total_sessions: 5,
        recent_msgs_per_day: 10, // exactly equal
      },
    };
    const result = BUILTIN_SEGMENTS.insights_pace(cache, {});
    expect(result).toContain("\u2192"); // →
  });

  it("does not include hardcoded (30d) in output", () => {
    const cache = {
      insights: {
        ...freshEntry,
        total_messages: 100,
        days_active: 10,
        total_lines_added: 500,
        total_sessions: 5,
        recent_msgs_per_day: 10,
      },
    };
    const result = BUILTIN_SEGMENTS.insights_pace(cache, {});
    expect(result).not.toContain("(30d)");
  });
});

describe("insights_friction staleness_days (RC-4)", () => {
  const freshEntry = {
    updated_at: new Date().toISOString(),
    ttl_seconds: 9999,
  };

  it("uses staleness_days from cache data instead of hardcoded 30d", () => {
    const cache = {
      insights: {
        ...freshEntry,
        friction_total: 7,
        staleness_days: 14,
        recent_session: { friction_count: 0 },
      },
    };
    const result = BUILTIN_SEGMENTS.insights_friction(cache, {});
    expect(result).toContain("in 14d");
    expect(result).not.toContain("in 30d");
  });

  it("falls back to 30d when staleness_days is not provided", () => {
    const cache = {
      insights: {
        ...freshEntry,
        friction_total: 3,
        recent_session: { friction_count: 0 },
      },
    };
    const result = BUILTIN_SEGMENTS.insights_friction(cache, {});
    expect(result).toContain("in 30d");
  });

  it("shows friction count with custom window", () => {
    const cache = {
      insights: {
        ...freshEntry,
        friction_total: 12,
        staleness_days: 90,
        recent_session: { friction_count: 0 },
      },
    };
    const result = BUILTIN_SEGMENTS.insights_friction(cache, {});
    expect(result).toContain("12 in 90d");
  });
});

describe("daemon_health segment", () => {
  it("returns empty string when no heartbeat exists", () => {
    const cache = {};
    const result = BUILTIN_SEGMENTS.daemon_health(cache, {});
    expect(result).toBe("");
  });

  it("returns 'daemon ok' when heartbeat is fresh", () => {
    const cache = { _daemon_heartbeat: { value: Date.now() - 30_000 } };
    const result = BUILTIN_SEGMENTS.daemon_health(cache, {});
    expect(result).toContain("daemon ok");
  });

  it("returns warning when heartbeat is stale (>90s)", () => {
    const cache = { _daemon_heartbeat: { value: Date.now() - 120_000 } };
    const result = BUILTIN_SEGMENTS.daemon_health(cache, {});
    expect(result).toContain("daemon stale");
    expect(result).toContain("2m");
  });

  it("returns empty string when heartbeat value is missing", () => {
    const cache = { _daemon_heartbeat: {} };
    const result = BUILTIN_SEGMENTS.daemon_health(cache, {});
    expect(result).toBe("");
  });
});
