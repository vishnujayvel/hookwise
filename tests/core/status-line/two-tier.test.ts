/**
 * Tests for the two-tier status line renderer.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderTwoTier, DEFAULT_TWO_TIER_CONFIG } from "../../../src/core/status-line/two-tier.js";
import type { TwoTierConfig } from "../../../src/core/status-line/two-tier.js";
import { strip } from "../../../src/core/status-line/ansi.js";

function makeConfig(overrides: Partial<TwoTierConfig> = {}): TwoTierConfig {
  return { ...DEFAULT_TWO_TIER_CONFIG, ...overrides };
}

describe("renderTwoTier - line 1 (fixed)", () => {
  it("renders context_bar and cost on line 1", () => {
    const cache = {
      _stdin: {
        context_window: { used_percentage: 0.5 },
        cost: { total_cost_usd: 3.45, total_duration_ms: 2_700_000 },
      },
      cost: { sessionCostUsd: 3.45 },
      builder_trap: { current_mode: "tooling" },
    };
    const config = makeConfig();
    const result = renderTwoTier(config, cache);
    const lines = result.split("\n");

    const line1 = strip(lines[0]);
    expect(line1).toContain("50%");
    expect(line1).toContain("[tooling]");
    expect(line1).toContain("$3.45");
    expect(line1).toContain("45m");
  });

  it("skips empty fixed segments", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 0.3 } },
      // No builder_trap, no cost, no duration
    };
    const config = makeConfig();
    const result = renderTwoTier(config, cache);
    const stripped = strip(result);
    // Should only have context_bar
    expect(stripped).toContain("30%");
    expect(stripped).not.toContain("[");
    expect(stripped).not.toContain("$");
  });

  it("uses configured delimiter between fixed segments", () => {
    const cache = {
      _stdin: {
        context_window: { used_percentage: 0.5 },
        cost: { total_duration_ms: 600_000 },
      },
      builder_trap: { current_mode: "practice" },
    };
    const config = makeConfig({ delimiter: " :: " });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result.split("\n")[0]);
    expect(stripped).toContain(" :: ");
  });
});

describe("renderTwoTier - line 2 (rotating)", () => {
  it("picks the first non-empty rotating segment", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 0.3 } },
      mantra: { text: "Stay focused" },
      _rotation_index: 0,
    };
    // Configure rotation to start with mantra
    const config = makeConfig({
      rotatingSegments: ["mantra"],
    });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result);
    expect(stripped).toContain("Stay focused");
  });

  it("skips empty rotating segments and finds next", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 0.3 } },
      // news is empty (no data), but mantra has data
      mantra: { text: "Ship it" },
      _rotation_index: 0,
    };
    const config = makeConfig({
      rotatingSegments: ["news", "mantra"],
    });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result);
    expect(stripped).toContain("Ship it");
  });

  it("wraps around rotation index", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 0.3 } },
      mantra: { text: "Focus" },
      _rotation_index: 100, // Way past array bounds, should wrap
    };
    const config = makeConfig({
      rotatingSegments: ["mantra"],
    });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result);
    expect(stripped).toContain("Focus");
  });

  it("renders single line when all rotating segments empty", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 0.5 } },
      _rotation_index: 0,
    };
    const config = makeConfig({
      rotatingSegments: ["news", "calendar"],
    });
    const result = renderTwoTier(config, cache);
    // Should have no newline (single line)
    expect(result).not.toContain("\n");
    const stripped = strip(result);
    expect(stripped).toContain("50%");
  });
});

describe("renderTwoTier - edge cases", () => {
  it("returns empty string when no segments produce output", () => {
    const cache = {};
    const config = makeConfig({
      fixedSegments: [],
      rotatingSegments: [],
    });
    const result = renderTwoTier(config, cache);
    expect(result).toBe("");
  });

  it("handles empty cache gracefully", () => {
    const config = makeConfig();
    const result = renderTwoTier(config, {});
    expect(result).toBe("");
  });

  it("returns only line 2 when all fixed segments are empty", () => {
    const cache = {
      mantra: { text: "Hello" },
      _rotation_index: 0,
    };
    const config = makeConfig({
      fixedSegments: [],
      rotatingSegments: ["mantra"],
    });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result);
    expect(stripped).toBe("Hello");
    expect(result).not.toContain("\n");
  });

  it("handles unknown segment names in config", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 0.3 } },
    };
    const config = makeConfig({
      fixedSegments: ["nonexistent", "context_bar"],
      rotatingSegments: ["also_nonexistent"],
    });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result);
    expect(stripped).toContain("30%");
  });
});

describe("renderTwoTier - ANSI coloring", () => {
  it("colors context_bar green when under 50%", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 0.3 } },
    };
    const config = makeConfig({ fixedSegments: ["context_bar"], rotatingSegments: [] });
    const result = renderTwoTier(config, cache);
    // Should contain green ANSI code
    expect(result).toContain("\x1b[32m");
  });

  it("colors context_bar yellow at 50-75%", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 0.6 } },
    };
    const config = makeConfig({ fixedSegments: ["context_bar"], rotatingSegments: [] });
    const result = renderTwoTier(config, cache);
    expect(result).toContain("\x1b[33m");
  });

  it("colors context_bar red at 75%+", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 0.8 } },
    };
    const config = makeConfig({ fixedSegments: ["context_bar"], rotatingSegments: [] });
    const result = renderTwoTier(config, cache);
    expect(result).toContain("\x1b[31m");
  });

  it("colors mode_badge by mode type", () => {
    const cache = { builder_trap: { current_mode: "practice" } };
    const config = makeConfig({ fixedSegments: ["mode_badge"], rotatingSegments: [] });
    const result = renderTwoTier(config, cache);
    // Practice mode should be green
    expect(result).toContain("\x1b[32m");
  });
});

describe("DEFAULT_TWO_TIER_CONFIG", () => {
  it("has expected fixed segments", () => {
    expect(DEFAULT_TWO_TIER_CONFIG.fixedSegments).toEqual([
      "context_bar", "mode_badge", "cost", "duration",
    ]);
  });

  it("has expected rotating segments", () => {
    expect(DEFAULT_TWO_TIER_CONFIG.rotatingSegments).toEqual([
      "insights_friction", "insights_pace", "insights_trend",
      "news", "calendar", "practice_breadcrumb", "mantra", "project", "pulse",
    ]);
  });

  it("uses pipe delimiter", () => {
    expect(DEFAULT_TWO_TIER_CONFIG.delimiter).toBe(" | ");
  });
});
