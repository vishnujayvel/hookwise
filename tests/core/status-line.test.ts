/**
 * Tests for the Status Line Renderer and Segments.
 *
 * Verifies:
 * - Segments composed in order
 * - Delimiter applied
 * - Missing cache uses fallbacks
 * - Unknown segment name skipped
 * - 8 built-in segments with populated cache data
 * - Each segment with missing/empty cache (fallback)
 * - Edge cases (zero values)
 * - Custom segment: command output, timeout, command failure
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render } from "../../src/core/status-line/renderer.js";
import { BUILTIN_SEGMENTS } from "../../src/core/status-line/segments.js";
import type { StatusLineConfig, SegmentConfig } from "../../src/core/types.js";

function makeStatusConfig(
  overrides: Partial<StatusLineConfig> = {}
): StatusLineConfig {
  return {
    enabled: true,
    segments: [],
    delimiter: " | ",
    cachePath: "/tmp/hookwise-test-status-cache.json",
    ...overrides,
  };
}

// --- Renderer: Composition ---

describe("render - composition", () => {
  beforeEach(() => {
    const fs = require("node:fs");
    const cache = {
      mantra: { text: "Ship it" },
      session: { startedAt: "2026-02-20T10:00:00Z", toolCalls: 42, aiRatio: 0.73 },
      cost: { sessionCostUsd: 1.23 },
    };
    fs.writeFileSync(
      "/tmp/hookwise-test-status-cache.json",
      JSON.stringify(cache)
    );
  });

  afterEach(() => {
    const fs = require("node:fs");
    try { fs.unlinkSync("/tmp/hookwise-test-status-cache.json"); } catch {}
  });

  it("composes segments in order", () => {
    const config = makeStatusConfig({
      segments: [
        { builtin: "mantra" },
        { builtin: "cost" },
      ],
    });
    const result = render(config);
    expect(result).toContain("Ship it");
    expect(result).toContain("$1.23");
    // mantra should come before cost
    const mantraIdx = result.indexOf("Ship it");
    const costIdx = result.indexOf("$1.23");
    expect(mantraIdx).toBeLessThan(costIdx);
  });

  it("applies delimiter between segments", () => {
    const config = makeStatusConfig({
      delimiter: " :: ",
      segments: [
        { builtin: "mantra" },
        { builtin: "cost" },
      ],
    });
    const result = render(config);
    expect(result).toContain(" :: ");
  });

  it("skips empty segments", () => {
    const config = makeStatusConfig({
      segments: [
        { builtin: "mantra" },
        { builtin: "streak" }, // not in cache -> empty
        { builtin: "cost" },
      ],
    });
    const result = render(config);
    // Should not have double delimiter
    expect(result).not.toContain(" |  | ");
  });

  it("skips unknown segment names", () => {
    const config = makeStatusConfig({
      segments: [
        { builtin: "mantra" },
        { builtin: "nonexistent_segment" },
        { builtin: "cost" },
      ],
    });
    const result = render(config);
    expect(result).toContain("Ship it");
    expect(result).toContain("$1.23");
  });

  it("returns empty string when no segments produce output", () => {
    const config = makeStatusConfig({
      segments: [
        { builtin: "streak" }, // not in cache
      ],
    });
    const result = render(config);
    expect(result).toBe("");
  });
});

// --- Renderer: Missing/Corrupt Cache ---

describe("render - missing cache", () => {
  it("handles missing cache file gracefully", () => {
    const config = makeStatusConfig({
      cachePath: "/tmp/nonexistent-cache-file.json",
      segments: [
        { builtin: "mantra" },
        { builtin: "cost" },
      ],
    });
    const result = render(config);
    expect(result).toBe("");
  });

  it("handles corrupt cache file gracefully", () => {
    const fs = require("node:fs");
    fs.writeFileSync("/tmp/hookwise-test-corrupt-cache.json", "not valid json{{{");
    const config = makeStatusConfig({
      cachePath: "/tmp/hookwise-test-corrupt-cache.json",
      segments: [
        { builtin: "mantra" },
      ],
    });
    const result = render(config);
    expect(result).toBe("");
    fs.unlinkSync("/tmp/hookwise-test-corrupt-cache.json");
  });
});

// --- Built-in Segments ---

describe("BUILTIN_SEGMENTS", () => {
  it("has all 8 required segments registered", () => {
    const names = ["clock", "mantra", "builder_trap", "session", "practice", "ai_ratio", "cost", "streak"];
    for (const name of names) {
      expect(BUILTIN_SEGMENTS[name]).toBeDefined();
      expect(typeof BUILTIN_SEGMENTS[name]).toBe("function");
    }
  });
});

// --- clock segment ---

describe("clock segment", () => {
  it("returns a time string", () => {
    const result = BUILTIN_SEGMENTS.clock({}, {});
    expect(result).toMatch(/\d{1,2}:\d{2}/);
  });
});

// --- mantra segment ---

describe("mantra segment", () => {
  it("returns mantra text when present", () => {
    const result = BUILTIN_SEGMENTS.mantra({ mantra: { text: "Stay focused" } }, {});
    expect(result).toBe("Stay focused");
  });

  it("returns empty when no mantra in cache", () => {
    const result = BUILTIN_SEGMENTS.mantra({}, {});
    expect(result).toBe("");
  });

  it("returns empty when mantra.text is empty", () => {
    const result = BUILTIN_SEGMENTS.mantra({ mantra: { text: "" } }, {});
    expect(result).toBe("");
  });
});

// --- builder_trap segment ---

describe("builder_trap segment", () => {
  it("returns empty for alert level none", () => {
    const result = BUILTIN_SEGMENTS.builder_trap(
      { builderTrap: { alertLevel: "none", toolingMinutes: 10 } }, {}
    );
    expect(result).toBe("");
  });

  it("returns yellow warning", () => {
    const result = BUILTIN_SEGMENTS.builder_trap(
      { builderTrap: { alertLevel: "yellow", toolingMinutes: 30 } }, {}
    );
    expect(result).toContain("30m tooling");
  });

  it("returns orange warning", () => {
    const result = BUILTIN_SEGMENTS.builder_trap(
      { builderTrap: { alertLevel: "orange", toolingMinutes: 60 } }, {}
    );
    expect(result).toContain("60m tooling");
  });

  it("returns red warning", () => {
    const result = BUILTIN_SEGMENTS.builder_trap(
      { builderTrap: { alertLevel: "red", toolingMinutes: 95 } }, {}
    );
    expect(result).toContain("95m tooling");
  });

  it("returns empty when no builderTrap in cache", () => {
    const result = BUILTIN_SEGMENTS.builder_trap({}, {});
    expect(result).toBe("");
  });
});

// --- session segment ---

describe("session segment", () => {
  it("formats session duration and tool calls", () => {
    const startedAt = new Date(Date.now() - 83 * 60 * 1000).toISOString(); // 1h23m ago
    const result = BUILTIN_SEGMENTS.session(
      { session: { startedAt, toolCalls: 42 } }, {}
    );
    expect(result).toMatch(/1h23m/);
    expect(result).toContain("42 calls");
  });

  it("returns empty when no session in cache", () => {
    const result = BUILTIN_SEGMENTS.session({}, {});
    expect(result).toBe("");
  });

  it("handles zero tool calls", () => {
    const startedAt = new Date(Date.now() - 5 * 60 * 1000).toISOString(); // 5m ago
    const result = BUILTIN_SEGMENTS.session(
      { session: { startedAt, toolCalls: 0 } }, {}
    );
    expect(result).toContain("0 calls");
  });

  it("formats less than an hour", () => {
    const startedAt = new Date(Date.now() - 15 * 60 * 1000).toISOString(); // 15m ago
    const result = BUILTIN_SEGMENTS.session(
      { session: { startedAt, toolCalls: 10 } }, {}
    );
    expect(result).toMatch(/15m/);
  });
});

// --- practice segment ---

describe("practice segment", () => {
  it("shows today's practice count", () => {
    const result = BUILTIN_SEGMENTS.practice(
      { practice: { todayTotal: 3 } }, {}
    );
    expect(result).toContain("3 today");
  });

  it("returns empty when no practice in cache", () => {
    const result = BUILTIN_SEGMENTS.practice({}, {});
    expect(result).toBe("");
  });

  it("handles zero practice count", () => {
    const result = BUILTIN_SEGMENTS.practice(
      { practice: { todayTotal: 0 } }, {}
    );
    expect(result).toBe("");
  });
});

// --- ai_ratio segment ---

describe("ai_ratio segment", () => {
  it("renders AI ratio as percentage and bar", () => {
    const result = BUILTIN_SEGMENTS.ai_ratio(
      { session: { aiRatio: 0.73 } }, {}
    );
    expect(result).toContain("AI: 73%");
    expect(result.length).toBeGreaterThan(8); // has bar chars
  });

  it("returns empty when no session in cache", () => {
    const result = BUILTIN_SEGMENTS.ai_ratio({}, {});
    expect(result).toBe("");
  });

  it("renders 0% ratio", () => {
    const result = BUILTIN_SEGMENTS.ai_ratio(
      { session: { aiRatio: 0 } }, {}
    );
    expect(result).toContain("AI: 0%");
  });

  it("renders 100% ratio", () => {
    const result = BUILTIN_SEGMENTS.ai_ratio(
      { session: { aiRatio: 1.0 } }, {}
    );
    expect(result).toContain("AI: 100%");
  });
});

// --- cost segment ---

describe("cost segment", () => {
  it("formats session cost", () => {
    const result = BUILTIN_SEGMENTS.cost(
      { cost: { sessionCostUsd: 1.23 } }, {}
    );
    expect(result).toBe("$1.23");
  });

  it("returns empty when no cost in cache", () => {
    const result = BUILTIN_SEGMENTS.cost({}, {});
    expect(result).toBe("");
  });

  it("formats zero cost", () => {
    const result = BUILTIN_SEGMENTS.cost(
      { cost: { sessionCostUsd: 0 } }, {}
    );
    expect(result).toBe("$0.00");
  });

  it("formats high cost with 2 decimal places", () => {
    const result = BUILTIN_SEGMENTS.cost(
      { cost: { sessionCostUsd: 42.567 } }, {}
    );
    expect(result).toBe("$42.57");
  });
});

// --- streak segment ---

describe("streak segment", () => {
  it("shows coding streak", () => {
    const result = BUILTIN_SEGMENTS.streak(
      { streak: { coding: 5 } }, {}
    );
    expect(result).toContain("5d streak");
  });

  it("returns empty when no streak in cache", () => {
    const result = BUILTIN_SEGMENTS.streak({}, {});
    expect(result).toBe("");
  });

  it("returns empty for zero streak", () => {
    const result = BUILTIN_SEGMENTS.streak(
      { streak: { coding: 0 } }, {}
    );
    expect(result).toBe("");
  });
});

// --- Custom Segments ---

describe("render - custom segments", () => {
  afterEach(() => {
    const fs = require("node:fs");
    try { fs.unlinkSync("/tmp/hookwise-test-status-cache.json"); } catch {}
  });

  it("executes custom command and uses output", () => {
    const fs = require("node:fs");
    fs.writeFileSync("/tmp/hookwise-test-status-cache.json", "{}");
    const config = makeStatusConfig({
      segments: [
        { custom: { command: 'echo "hello custom"' } },
      ],
    });
    const result = render(config);
    expect(result).toBe("hello custom");
  });

  it("applies label prefix to custom segment", () => {
    const fs = require("node:fs");
    fs.writeFileSync("/tmp/hookwise-test-status-cache.json", "{}");
    const config = makeStatusConfig({
      segments: [
        { custom: { command: 'echo "42"', label: "Count" } },
      ],
    });
    const result = render(config);
    expect(result).toBe("Count: 42");
  });

  it("returns empty on command timeout", () => {
    const fs = require("node:fs");
    fs.writeFileSync("/tmp/hookwise-test-status-cache.json", "{}");
    const config = makeStatusConfig({
      segments: [
        { custom: { command: "sleep 10", timeout: 100 } },
      ],
    });
    const result = render(config);
    expect(result).toBe("");
  });

  it("returns empty on command failure", () => {
    const fs = require("node:fs");
    fs.writeFileSync("/tmp/hookwise-test-status-cache.json", "{}");
    const config = makeStatusConfig({
      segments: [
        { custom: { command: "exit 1" } },
      ],
    });
    const result = render(config);
    expect(result).toBe("");
  });

  it("trims whitespace from command output", () => {
    const fs = require("node:fs");
    fs.writeFileSync("/tmp/hookwise-test-status-cache.json", "{}");
    const config = makeStatusConfig({
      segments: [
        { custom: { command: 'echo "  trimmed  "' } },
      ],
    });
    const result = render(config);
    expect(result).toBe("trimmed");
  });

  it("mixes builtin and custom segments", () => {
    const fs = require("node:fs");
    const cache = { mantra: { text: "Focus" } };
    fs.writeFileSync("/tmp/hookwise-test-status-cache.json", JSON.stringify(cache));
    const config = makeStatusConfig({
      segments: [
        { builtin: "mantra" },
        { custom: { command: 'echo "v1.0"', label: "Ver" } },
      ],
    });
    const result = render(config);
    expect(result).toContain("Focus");
    expect(result).toContain("Ver: v1.0");
  });
});
