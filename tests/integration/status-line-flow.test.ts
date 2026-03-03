/**
 * Integration tests for the status line rendering pipeline.
 *
 * Verifies that the renderer produces correct output from real cache state,
 * covering single-tier render(), two-tier renderTwoTier(), fault isolation,
 * and TTL-based stale-to-fresh transitions.
 *
 * Tasks: 4.1 (basic rendering from real cache state)
 *        4.2 (two-tier rendering and segment fault isolation)
 *
 * Architecture constraints:
 * - ARCH-1: No mocking of renderer, segments, or cache-bus
 * - ARCH-2: Temp dir isolation via createTestEnv()
 * - ARCH-5: vi.useFakeTimers() for TTL tests
 * - ARCH-6: render(StatusLineConfig) reads cache internally;
 *           renderTwoTier(TwoTierConfig, cache) takes cache as argument
 */

import { describe, it, expect, afterEach, vi, beforeEach } from "vitest";
import { render } from "../../src/core/status-line/renderer.js";
import { renderTwoTier } from "../../src/core/status-line/two-tier.js";
import type { TwoTierConfig } from "../../src/core/status-line/two-tier.js";
import { mergeKey, readAll } from "../../src/core/feeds/cache-bus.js";
import { BUILTIN_SEGMENTS } from "../../src/core/status-line/segments.js";
import { createTestEnv, seedCache } from "./helpers.js";
import type { TestEnv } from "./helpers.js";
import type { StatusLineConfig, SegmentConfig } from "../../src/core/types.js";

// ---------------------------------------------------------------------------
// Shared setup
// ---------------------------------------------------------------------------

let env: TestEnv;

afterEach(() => {
  env?.cleanup();
  vi.useRealTimers();
});

// ---------------------------------------------------------------------------
// Task 4.1 — Basic rendering from real cache state
// ---------------------------------------------------------------------------

describe("status-line-flow: basic rendering from real cache state", () => {
  it("fresh cache with mantra data -> render() output contains segment text", () => {
    env = createTestEnv();

    // Seed the cache file with mantra data via the real cache-bus write path.
    // mantra is a simple segment that does NOT check isFresh(),
    // so we just need the data present in the cache file.
    seedCache(env.cachePath, [
      { key: "mantra", data: { text: "Ship it" }, ttlSeconds: 300 },
    ]);

    // Build a StatusLineConfig that references the real cache file
    const statusLineConfig: StatusLineConfig = {
      enabled: true,
      segments: [{ builtin: "mantra" }],
      delimiter: " | ",
      cachePath: env.cachePath,
    };

    // render() reads cache internally from config.cachePath (ARCH-6)
    const output = render(statusLineConfig);

    expect(output).toContain("Ship it");
  });

  it("empty/missing cache -> render() returns without crashing (fail-open)", () => {
    env = createTestEnv();

    // Do NOT seed any cache data — the file does not exist
    const statusLineConfig: StatusLineConfig = {
      enabled: true,
      segments: [
        { builtin: "pulse" },
        { builtin: "project" },
        { builtin: "mantra" },
      ],
      delimiter: " | ",
      cachePath: env.cachePath,
    };

    // render() should not throw; it returns an empty string when no segments produce output
    const output = render(statusLineConfig);
    expect(output).toBe("");
  });

  it("cache written via mergeKey -> render(config) reads it (full data path)", () => {
    env = createTestEnv();

    // Use vi.useFakeTimers so isFresh() works deterministically for
    // feed-type segments that check freshness (like pulse).
    vi.useFakeTimers();
    const now = new Date("2026-03-03T12:00:00Z").getTime();
    vi.setSystemTime(now);

    // Write cache data through the real mergeKey path
    mergeKey(env.cachePath, "pulse", { value: "Active" }, 300);
    mergeKey(env.cachePath, "mantra", { text: "Focus deeply" }, 300);

    const statusLineConfig: StatusLineConfig = {
      enabled: true,
      segments: [{ builtin: "pulse" }, { builtin: "mantra" }],
      delimiter: " | ",
      cachePath: env.cachePath,
    };

    const output = render(statusLineConfig);

    // Both segments should render — pulse checks isFresh() (just written, so fresh)
    // and mantra reads the text field directly
    expect(output).toContain("Active");
    expect(output).toContain("Focus deeply");
    // The delimiter joins the two segments
    expect(output).toBe("Active | Focus deeply");
  });
});

// ---------------------------------------------------------------------------
// Task 4.2 — Two-tier rendering and segment fault isolation
// ---------------------------------------------------------------------------

describe("status-line-flow: two-tier rendering", () => {
  it("fixed + rotating segments -> renderTwoTier() produces Line 1 + Line 2", () => {
    env = createTestEnv();

    vi.useFakeTimers();
    const now = new Date("2026-03-03T12:00:00Z").getTime();
    vi.setSystemTime(now);

    // Build a cache object with data for both fixed and rotating segments.
    // renderTwoTier takes cache as argument (ARCH-6).
    const cache: Record<string, unknown> = {
      // Fixed segment: cost
      cost: { sessionCostUsd: 1.5 },
      // Fixed segment: context_bar (via _stdin)
      _stdin: { context_window: { used_percentage: 42 } },
      // Rotating segment: pulse (feed segment, checks isFresh)
      pulse: {
        updated_at: new Date(now - 2_000).toISOString(),
        ttl_seconds: 300,
        value: "Shipping",
      },
    };

    const twoTierConfig: TwoTierConfig = {
      fixedSegments: ["context_bar", "cost"],
      rotatingSegments: ["pulse"],
      delimiter: " | ",
    };

    const output = renderTwoTier(twoTierConfig, cache);

    // Should have two lines separated by \n
    const lines = output.split("\n");
    expect(lines.length).toBe(2);

    // Line 1 should contain fixed segments
    // context_bar renders a progress bar with percentage
    expect(lines[0]).toContain("42%");
    // cost renders as $X.XX
    expect(lines[0]).toContain("$1.50");

    // Line 2 should contain the rotating segment
    expect(lines[1]).toContain("Shipping");
  });
});

describe("status-line-flow: segment fault isolation", () => {
  it("one segment errors -> other segments still render", () => {
    env = createTestEnv();

    // Seed cache with data for one valid segment
    seedCache(env.cachePath, [
      { key: "mantra", data: { text: "Stay calm" }, ttlSeconds: 300 },
    ]);

    // Create a config where one segment references a non-existent builtin
    // (which returns "" from renderSegment's `if (!renderer) return ""` path)
    // and another references a valid segment.
    const statusLineConfig: StatusLineConfig = {
      enabled: true,
      segments: [
        { builtin: "nonexistent_segment_that_will_fail" },
        { builtin: "mantra" },
      ],
      delimiter: " | ",
      cachePath: env.cachePath,
    };

    const output = render(statusLineConfig);

    // The valid segment should still render despite the invalid one
    expect(output).toContain("Stay calm");
    // Should not contain delimiter since only one segment produced output
    expect(output).toBe("Stay calm");
  });

  it("renderTwoTier: one fixed segment fails -> others still render", () => {
    env = createTestEnv();

    vi.useFakeTimers();
    const now = new Date("2026-03-03T12:00:00Z").getTime();
    vi.setSystemTime(now);

    const cache: Record<string, unknown> = {
      cost: { sessionCostUsd: 2.75 },
      // No data for context_bar (_stdin missing) — it returns ""
      // Also add a rotating segment
      pulse: {
        updated_at: new Date(now - 1_000).toISOString(),
        ttl_seconds: 300,
        value: "Rolling",
      },
    };

    const twoTierConfig: TwoTierConfig = {
      // context_bar will return "" because _stdin is missing
      // mode_badge will return "" because builder_trap is missing
      fixedSegments: ["context_bar", "mode_badge", "cost"],
      rotatingSegments: ["pulse"],
      delimiter: " | ",
    };

    const output = renderTwoTier(twoTierConfig, cache);
    const lines = output.split("\n");

    // Line 1 should still contain cost even though context_bar and mode_badge returned ""
    expect(lines[0]).toContain("$2.75");

    // Line 2 should contain pulse
    expect(lines[1]).toContain("Rolling");
  });
});

describe("status-line-flow: TTL / fresh-to-stale transition", () => {
  it("fresh -> stale transition -> segment disappears from output", () => {
    env = createTestEnv();

    vi.useFakeTimers();
    const now = new Date("2026-03-03T12:00:00Z").getTime();
    vi.setSystemTime(now);

    // Write pulse data with a short TTL (10 seconds) via real mergeKey
    mergeKey(env.cachePath, "pulse", { value: "Alive" }, 10);

    const statusLineConfig: StatusLineConfig = {
      enabled: true,
      segments: [{ builtin: "pulse" }],
      delimiter: " | ",
      cachePath: env.cachePath,
    };

    // At current time: pulse data is fresh (just written)
    const freshOutput = render(statusLineConfig);
    expect(freshOutput).toContain("Alive");

    // Advance time past the TTL (10 seconds + buffer)
    vi.setSystemTime(now + 15_000);

    // Now pulse data is stale — isFresh() returns false — segment returns ""
    const staleOutput = render(statusLineConfig);
    expect(staleOutput).toBe("");
  });
});
