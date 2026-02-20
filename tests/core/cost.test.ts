/**
 * Tests for cost tracking and budget enforcement.
 *
 * Verifies:
 * - Token estimation from input/output sizes
 * - Cost accumulation updates session and daily totals
 * - Budget check: under budget = ok, over budget = not ok
 * - Warn vs enforce modes in BudgetStatus
 * - Daily reset on date boundary change
 * - Load/save cost state
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdtempSync, existsSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import {
  estimateCost,
  checkBudget,
  accumulateCost,
  loadCostState,
  saveCostState,
} from "../../src/core/cost.js";
import type { CostState, CostTrackingConfig } from "../../src/core/types.js";

describe("estimateCost", () => {
  it("estimates tokens from input and output sizes", () => {
    const result = estimateCost("Bash", { command: "ls -la" }, 1000);
    expect(result.estimatedTokens).toBeGreaterThan(0);
    expect(result.estimatedCostUsd).toBeGreaterThan(0);
    expect(result.model).toBe("claude-sonnet");
  });

  it("larger inputs produce higher token estimates", () => {
    const small = estimateCost("Bash", { command: "ls" }, 100);
    const large = estimateCost(
      "Bash",
      { command: "x".repeat(10000) },
      10000
    );
    expect(large.estimatedTokens).toBeGreaterThan(small.estimatedTokens);
  });

  it("uses config rates when provided", () => {
    const config: CostTrackingConfig = {
      enabled: true,
      rates: { "claude-sonnet": 0.01 }, // Expensive rate
      dailyBudget: 10,
      enforcement: "warn",
    };
    const result = estimateCost("Bash", { command: "ls" }, 100, config);
    expect(result.estimatedCostUsd).toBeGreaterThan(0);
  });

  it("returns zero cost on error (empty input)", () => {
    // Should still work with empty input
    const result = estimateCost("Bash", {}, 0);
    expect(result.estimatedTokens).toBeGreaterThanOrEqual(0);
    expect(result.model).toBe("claude-sonnet");
  });
});

describe("checkBudget", () => {
  it("returns ok when under budget", () => {
    const state: CostState = {
      dailyCosts: {},
      sessionCosts: {},
      today: "2026-02-20",
      totalToday: 5.0,
    };
    const config: CostTrackingConfig = {
      enabled: true,
      rates: {},
      dailyBudget: 10,
      enforcement: "warn",
    };

    const result = checkBudget(state, config);
    expect(result.ok).toBe(true);
  });

  it("returns not-ok when at budget limit", () => {
    const state: CostState = {
      dailyCosts: {},
      sessionCosts: {},
      today: "2026-02-20",
      totalToday: 10.0,
    };
    const config: CostTrackingConfig = {
      enabled: true,
      rates: {},
      dailyBudget: 10,
      enforcement: "warn",
    };

    const result = checkBudget(state, config);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.enforcement).toBe("warn");
      expect(result.message).toContain("10.00");
    }
  });

  it("returns not-ok when over budget", () => {
    const state: CostState = {
      dailyCosts: {},
      sessionCosts: {},
      today: "2026-02-20",
      totalToday: 15.0,
    };
    const config: CostTrackingConfig = {
      enabled: true,
      rates: {},
      dailyBudget: 10,
      enforcement: "enforce",
    };

    const result = checkBudget(state, config);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.enforcement).toBe("enforce");
    }
  });

  it("returns ok when tracking is disabled", () => {
    const state: CostState = {
      dailyCosts: {},
      sessionCosts: {},
      today: "2026-02-20",
      totalToday: 999,
    };
    const config: CostTrackingConfig = {
      enabled: false,
      rates: {},
      dailyBudget: 10,
      enforcement: "enforce",
    };

    const result = checkBudget(state, config);
    expect(result.ok).toBe(true);
  });

  it("warn vs enforce modes are preserved in BudgetStatus", () => {
    const state: CostState = {
      dailyCosts: {},
      sessionCosts: {},
      today: "2026-02-20",
      totalToday: 100,
    };

    const warnConfig: CostTrackingConfig = {
      enabled: true,
      rates: {},
      dailyBudget: 10,
      enforcement: "warn",
    };
    const enforceConfig: CostTrackingConfig = {
      enabled: true,
      rates: {},
      dailyBudget: 10,
      enforcement: "enforce",
    };

    const warnResult = checkBudget(state, warnConfig);
    const enforceResult = checkBudget(state, enforceConfig);

    expect(warnResult.ok).toBe(false);
    expect(enforceResult.ok).toBe(false);

    if (!warnResult.ok) expect(warnResult.enforcement).toBe("warn");
    if (!enforceResult.ok) expect(enforceResult.enforcement).toBe("enforce");
  });
});

describe("accumulateCost", () => {
  it("adds cost to session and daily totals", () => {
    const state: CostState = {
      dailyCosts: {},
      sessionCosts: {},
      today: new Date().toISOString().slice(0, 10),
      totalToday: 0,
    };

    const total = accumulateCost(state, "session-1", 2.5);
    expect(total).toBe(2.5);
    expect(state.sessionCosts["session-1"]).toBe(2.5);
    expect(state.totalToday).toBe(2.5);
  });

  it("accumulates multiple costs to same session", () => {
    const today = new Date().toISOString().slice(0, 10);
    const state: CostState = {
      dailyCosts: {},
      sessionCosts: {},
      today,
      totalToday: 0,
    };

    accumulateCost(state, "session-1", 1.0);
    accumulateCost(state, "session-1", 2.0);
    expect(state.sessionCosts["session-1"]).toBe(3.0);
    expect(state.totalToday).toBe(3.0);
  });

  it("tracks costs across different sessions", () => {
    const today = new Date().toISOString().slice(0, 10);
    const state: CostState = {
      dailyCosts: {},
      sessionCosts: {},
      today,
      totalToday: 0,
    };

    accumulateCost(state, "session-1", 1.0);
    accumulateCost(state, "session-2", 2.0);
    expect(state.sessionCosts["session-1"]).toBe(1.0);
    expect(state.sessionCosts["session-2"]).toBe(2.0);
    expect(state.totalToday).toBe(3.0);
  });

  it("resets daily totals on date boundary", () => {
    const state: CostState = {
      dailyCosts: { "2026-01-01": 50 },
      sessionCosts: {},
      today: "2026-01-01",
      totalToday: 50,
    };

    // This will detect a date boundary since today != state.today
    const today = new Date().toISOString().slice(0, 10);
    if (today !== "2026-01-01") {
      const total = accumulateCost(state, "session-new", 1.0);
      expect(state.today).toBe(today);
      expect(total).toBe(1.0);
    }
  });
});

describe("loadCostState / saveCostState", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-cost-"));
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("returns fresh state when no file exists", () => {
    const state = loadCostState(tempDir);
    expect(state.dailyCosts).toEqual({});
    expect(state.sessionCosts).toEqual({});
    expect(state.totalToday).toBe(0);
  });

  it("round-trips state through save and load", () => {
    const state: CostState = {
      dailyCosts: { "2026-02-20": 5.5 },
      sessionCosts: { "sess-1": 3.0, "sess-2": 2.5 },
      today: "2026-02-20",
      totalToday: 5.5,
    };

    saveCostState(tempDir, state);
    const loaded = loadCostState(tempDir);
    expect(loaded).toEqual(state);
  });

  it("creates state directory if missing", () => {
    const nestedDir = join(tempDir, "deep", "nested");
    const state: CostState = {
      dailyCosts: {},
      sessionCosts: {},
      today: "2026-02-20",
      totalToday: 0,
    };

    saveCostState(nestedDir, state);
    expect(existsSync(join(nestedDir, "state", "cost-state.json"))).toBe(true);
  });
});
