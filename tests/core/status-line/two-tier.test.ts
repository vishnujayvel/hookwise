/**
 * Tests for the multi-tier status line renderer.
 */

import { describe, it, expect } from "vitest";
import { renderTwoTier, DEFAULT_TWO_TIER_CONFIG } from "../../../src/core/status-line/two-tier.js";
import type { TwoTierConfig } from "../../../src/core/status-line/two-tier.js";
import { strip } from "../../../src/core/status-line/ansi.js";

function makeConfig(overrides: Partial<TwoTierConfig> = {}): TwoTierConfig {
  return { ...DEFAULT_TWO_TIER_CONFIG, ...overrides };
}

describe("renderTwoTier - fixed lines", () => {
  it("renders context_bar and cost on line 1", () => {
    const cache = {
      _stdin: {
        context_window: { used_percentage: 50 },
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

  it("skips empty fixed segments within a line", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 30 } },
      // No builder_trap, no cost, no duration
    };
    const config = makeConfig({
      fixedLines: [["context_bar", "mode_badge", "cost"]],
      rotatingSegments: [],
    });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result);
    expect(stripped).toContain("30%");
    expect(stripped).not.toContain("[");
    expect(stripped).not.toContain("$");
  });

  it("skips entirely empty fixed lines", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 30 } },
    };
    const config = makeConfig({
      fixedLines: [
        ["context_bar"],
        ["mode_badge", "cost"], // all empty — should be skipped
      ],
      rotatingSegments: [],
    });
    const result = renderTwoTier(config, cache);
    const lines = result.split("\n");
    expect(lines.length).toBe(1);
    const stripped = strip(result);
    expect(stripped).toContain("30%");
  });

  it("renders multiple fixed lines", () => {
    const cache = {
      _stdin: {
        context_window: { used_percentage: 50 },
        cost: { total_cost_usd: 2.00, total_duration_ms: 600_000 },
      },
      cost: { sessionCostUsd: 2.00 },
      mantra: { text: "Ship it" },
    };
    const config = makeConfig({
      fixedLines: [
        ["context_bar", "cost"],
        ["mantra"],
      ],
      rotatingSegments: [],
    });
    const result = renderTwoTier(config, cache);
    const lines = result.split("\n");
    expect(lines.length).toBe(2);
    expect(strip(lines[0])).toContain("50%");
    expect(strip(lines[0])).toContain("$2.00");
    expect(strip(lines[1])).toContain("Ship it");
  });

  it("uses configured delimiter between segments on a line", () => {
    const cache = {
      _stdin: {
        context_window: { used_percentage: 50 },
        cost: { total_duration_ms: 600_000 },
      },
      builder_trap: { current_mode: "practice" },
    };
    const config = makeConfig({
      fixedLines: [["context_bar", "mode_badge"]],
      delimiter: " :: ",
      rotatingSegments: [],
    });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result.split("\n")[0]);
    expect(stripped).toContain(" :: ");
  });
});

describe("renderTwoTier - rotating line", () => {
  it("picks the first non-empty rotating segment", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 30 } },
      mantra: { text: "Stay focused" },
      _rotation_index: 0,
    };
    const config = makeConfig({
      fixedLines: [["context_bar"]],
      rotatingSegments: ["mantra"],
    });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result);
    expect(stripped).toContain("Stay focused");
  });

  it("skips empty rotating segments and finds next", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 30 } },
      mantra: { text: "Ship it" },
      _rotation_index: 0,
    };
    const config = makeConfig({
      fixedLines: [["context_bar"]],
      rotatingSegments: ["news", "mantra"],
    });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result);
    expect(stripped).toContain("Ship it");
  });

  it("wraps around rotation index", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 30 } },
      mantra: { text: "Focus" },
      _rotation_index: 100,
    };
    const config = makeConfig({
      fixedLines: [["context_bar"]],
      rotatingSegments: ["mantra"],
    });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result);
    expect(stripped).toContain("Focus");
  });

  it("renders without rotating line when all rotating segments empty", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 50 } },
      _rotation_index: 0,
    };
    const config = makeConfig({
      fixedLines: [["context_bar"]],
      rotatingSegments: ["news", "calendar"],
    });
    const result = renderTwoTier(config, cache);
    expect(result).not.toContain("\n");
    const stripped = strip(result);
    expect(stripped).toContain("50%");
  });
});

describe("renderTwoTier - edge cases", () => {
  it("returns empty string when no segments produce output", () => {
    const cache = {};
    const config = makeConfig({
      fixedLines: [],
      rotatingSegments: [],
    });
    const result = renderTwoTier(config, cache);
    expect(result).toBe("");
  });

  it("handles empty cache gracefully", () => {
    // Default config includes weather (renders fallback) and clock (always renders),
    // so use a config with no always-on segments to test the empty path
    const config = makeConfig({
      fixedLines: [["context_bar", "mode_badge"]],
      rotatingSegments: ["news"],
    });
    const result = renderTwoTier(config, {});
    expect(result).toBe("");
  });

  it("returns only rotating line when all fixed lines are empty", () => {
    const cache = {
      mantra: { text: "Hello" },
      _rotation_index: 0,
    };
    const config = makeConfig({
      fixedLines: [],
      rotatingSegments: ["mantra"],
    });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result);
    expect(stripped).toBe("Hello");
    expect(result).not.toContain("\n");
  });

  it("handles unknown segment names in config", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 30 } },
    };
    const config = makeConfig({
      fixedLines: [["nonexistent", "context_bar"]],
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
      _stdin: { context_window: { used_percentage: 30 } },
    };
    const config = makeConfig({ fixedLines: [["context_bar"]], rotatingSegments: [] });
    const result = renderTwoTier(config, cache);
    expect(result).toContain("\x1b[32m");
  });

  it("colors context_bar yellow at 50-75%", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 60 } },
    };
    const config = makeConfig({ fixedLines: [["context_bar"]], rotatingSegments: [] });
    const result = renderTwoTier(config, cache);
    expect(result).toContain("\x1b[33m");
  });

  it("colors context_bar red at 75%+", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 80 } },
    };
    const config = makeConfig({ fixedLines: [["context_bar"]], rotatingSegments: [] });
    const result = renderTwoTier(config, cache);
    expect(result).toContain("\x1b[31m");
  });

  it("colors mode_badge by mode type", () => {
    const cache = { builder_trap: { current_mode: "practice" } };
    const config = makeConfig({ fixedLines: [["mode_badge"]], rotatingSegments: [] });
    const result = renderTwoTier(config, cache);
    expect(result).toContain("\x1b[32m");
  });
});

describe("renderTwoTier - middle segments (N-tier)", () => {
  it("renders middle segment lines between fixed and rotating", () => {
    const now = Math.floor(Date.now() / 1000);
    const cache = {
      _stdin: { context_window: { used_percentage: 30 } },
      mantra: { text: "Focus" },
      agents: {
        agents: [
          { agent_id: "a1", name: "builder", status: "working", started_at: now - 120 },
        ],
        team_name: "test-team",
        strategy: "parallel",
      },
      _rotation_index: 0,
    };
    const config = makeConfig({
      fixedLines: [["context_bar"]],
      middleSegments: ["agents"],
      showSeparator: true,
      rotatingSegments: ["mantra"],
    });
    const result = renderTwoTier(config, cache);
    const lines = result.split("\n");

    expect(lines.length).toBeGreaterThanOrEqual(4);

    const stripped = strip(result);
    expect(stripped).toContain("TEAM: test-team");
    expect(stripped).toContain("builder");
    expect(stripped).toContain("Focus");
  });

  it("renders separator between fixed and middle segments", () => {
    const now = Math.floor(Date.now() / 1000);
    const cache = {
      _stdin: { context_window: { used_percentage: 30 } },
      agents: {
        agents: [
          { agent_id: "a1", name: "worker", status: "working", started_at: now - 60 },
        ],
        team_name: "",
        strategy: "",
      },
    };
    const config = makeConfig({
      fixedLines: [["context_bar"]],
      middleSegments: ["agents"],
      showSeparator: true,
      rotatingSegments: [],
    });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result);
    expect(stripped).toContain("---");
  });

  it("skips separator when showSeparator is false", () => {
    const now = Math.floor(Date.now() / 1000);
    const cache = {
      _stdin: { context_window: { used_percentage: 30 } },
      agents: {
        agents: [
          { agent_id: "a1", name: "worker", status: "working", started_at: now - 60 },
        ],
        team_name: "",
        strategy: "",
      },
    };
    const config = makeConfig({
      fixedLines: [["context_bar"]],
      middleSegments: ["agents"],
      showSeparator: false,
      rotatingSegments: [],
    });
    const result = renderTwoTier(config, cache);
    const stripped = strip(result);
    expect(stripped).not.toContain("---");
  });

  it("collapses middle when all middle segments are empty", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 30 } },
      mantra: { text: "Ship it" },
      _rotation_index: 0,
    };
    const config = makeConfig({
      fixedLines: [["context_bar"]],
      middleSegments: ["agents"],
      showSeparator: true,
      rotatingSegments: ["mantra"],
    });
    const result = renderTwoTier(config, cache);
    const lines = result.split("\n");

    // Should be exactly 2 lines (fixed + rotating), no separator
    expect(lines.length).toBe(2);
    const stripped = strip(result);
    expect(stripped).not.toContain("---");
    expect(stripped).toContain("30%");
    expect(stripped).toContain("Ship it");
  });

  it("handles empty middleSegments array gracefully", () => {
    const cache = {
      _stdin: { context_window: { used_percentage: 50 } },
      mantra: { text: "Go" },
      _rotation_index: 0,
    };
    const config = makeConfig({
      fixedLines: [["context_bar"]],
      middleSegments: [],
      showSeparator: true,
      rotatingSegments: ["mantra"],
    });
    const result = renderTwoTier(config, cache);
    const lines = result.split("\n");
    expect(lines.length).toBe(2);
  });
});

describe("DEFAULT_TWO_TIER_CONFIG", () => {
  it("has 4 fixed lines for 5-line minimum layout", () => {
    expect(DEFAULT_TWO_TIER_CONFIG.fixedLines).toEqual([
      ["context_bar", "mode_badge", "cost", "duration", "daemon_health"],
      ["project", "calendar", "weather"],
      ["insights_friction", "insights_pace"],
      ["insights_trend"],
    ]);
  });

  it("has expected rotating segments", () => {
    expect(DEFAULT_TWO_TIER_CONFIG.rotatingSegments).toEqual([
      "news", "mantra", "memories", "pulse", "streak", "builder_trap", "clock",
    ]);
  });

  it("uses pipe delimiter", () => {
    expect(DEFAULT_TWO_TIER_CONFIG.delimiter).toBe(" | ");
  });

  it("has agents as default middle segment", () => {
    expect(DEFAULT_TWO_TIER_CONFIG.middleSegments).toEqual(["agents"]);
  });

  it("has separator enabled by default", () => {
    expect(DEFAULT_TWO_TIER_CONFIG.showSeparator).toBe(true);
  });
});
