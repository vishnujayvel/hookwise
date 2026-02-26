import { describe, it, expect } from "vitest";
import { BUILTIN_SEGMENTS } from "../../../src/core/status-line/segments.js";
import type { SegmentConfig } from "../../../src/core/types.js";

const defaultSegmentConfig: SegmentConfig = {
  fixed: [],
  rotating: [],
  rotationIntervalSeconds: 10,
};

function makeCache(insightsData: Record<string, unknown> | null): Record<string, unknown> {
  if (!insightsData) return {};
  return {
    insights: {
      ...insightsData,
      updated_at: new Date().toISOString(),
      ttl_seconds: 120,
    },
  };
}

function makeStaleCacheEntry(insightsData: Record<string, unknown>): Record<string, unknown> {
  return {
    insights: {
      ...insightsData,
      updated_at: "2020-01-01T00:00:00Z", // way in the past
      ttl_seconds: 120,
    },
  };
}

// --- insights_friction ---

describe("insights_friction", () => {
  const render = BUILTIN_SEGMENTS.insights_friction;

  it("renders friction warning when recent session has friction > 0", () => {
    const cache = makeCache({
      recent_session: { friction_count: 3 },
      friction_total: 12,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toContain("3 friction in last session");
    expect(result).toContain("\u26A0\uFE0F");
  });

  it("renders clean session message when zero recent but historical friction", () => {
    const cache = makeCache({
      recent_session: { friction_count: 0 },
      friction_total: 12,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toContain("Clean session");
    expect(result).toContain("12 total friction");
    expect(result).toContain("\u2705");
  });

  it("renders no-friction message when total friction is zero", () => {
    const cache = makeCache({
      recent_session: { friction_count: 0 },
      friction_total: 0,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toContain("No friction detected");
    expect(result).toContain("\u2705");
  });

  it("returns empty string when cache is stale", () => {
    const cache = makeStaleCacheEntry({
      recent_session: { friction_count: 5 },
      friction_total: 20,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toBe("");
  });

  it("returns empty string when insights cache is missing", () => {
    const result = render({}, defaultSegmentConfig);
    expect(result).toBe("");
  });
});

// --- insights_pace ---

describe("insights_pace", () => {
  const render = BUILTIN_SEGMENTS.insights_pace;

  it("renders formatted metrics correctly", () => {
    const cache = makeCache({
      total_messages: 470,
      days_active: 10,
      total_lines_added: 5400,
      total_sessions: 42,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toContain("47 msgs/day");
    expect(result).toContain("5.4k+ lines");
    expect(result).toContain("42 sessions");
  });

  it("formats large numbers correctly (28000 → 28k)", () => {
    const cache = makeCache({
      total_messages: 100,
      days_active: 1,
      total_lines_added: 28000,
      total_sessions: 10,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toContain("28k+ lines");
  });

  it("formats numbers under 1000 without suffix", () => {
    const cache = makeCache({
      total_messages: 50,
      days_active: 5,
      total_lines_added: 340,
      total_sessions: 3,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toContain("340+ lines");
  });

  it("returns empty string when cache is stale", () => {
    const cache = makeStaleCacheEntry({
      total_messages: 100,
      days_active: 5,
      total_lines_added: 1000,
      total_sessions: 10,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toBe("");
  });
});

// --- insights_trend ---

describe("insights_trend", () => {
  const render = BUILTIN_SEGMENTS.insights_trend;

  it("renders top tools and afternoon peak correctly", () => {
    const cache = makeCache({
      top_tools: [
        { name: "Bash", count: 50 },
        { name: "Read", count: 30 },
        { name: "Edit", count: 20 },
      ],
      peak_hour: 14,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toContain("Top: Bash, Read");
    expect(result).toContain("Peak: afternoon");
  });

  it("maps peak_hour 8 to morning", () => {
    const cache = makeCache({
      top_tools: [{ name: "Read", count: 10 }],
      peak_hour: 8,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toContain("Peak: morning");
  });

  it("maps peak_hour 20 to evening", () => {
    const cache = makeCache({
      top_tools: [{ name: "Read", count: 10 }],
      peak_hour: 20,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toContain("Peak: evening");
  });

  it("maps peak_hour 3 to night", () => {
    const cache = makeCache({
      top_tools: [{ name: "Read", count: 10 }],
      peak_hour: 3,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toContain("Peak: night");
  });

  it("returns empty string when cache is stale", () => {
    const cache = makeStaleCacheEntry({
      top_tools: [{ name: "Bash", count: 50 }],
      peak_hour: 14,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toBe("");
  });

  it("returns empty string when no tools data", () => {
    const cache = makeCache({
      top_tools: [],
      peak_hour: 14,
    });
    const result = render(cache, defaultSegmentConfig);
    expect(result).toBe("");
  });
});
