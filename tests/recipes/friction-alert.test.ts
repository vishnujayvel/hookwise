import { describe, it, expect, beforeEach, afterEach } from "vitest";
import {
  mkdtempSync,
  writeFileSync,
  rmSync,
  mkdirSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { checkFriction } from "../../recipes/productivity/friction-alert/handler.js";
import type { FrictionAlertConfig } from "../../recipes/productivity/friction-alert/handler.js";

function writeCache(
  cachePath: string,
  insightsData: Record<string, unknown> | null,
  stale = false,
): void {
  const cache: Record<string, unknown> = {};
  if (insightsData) {
    cache.insights = {
      ...insightsData,
      updated_at: stale ? "2020-01-01T00:00:00Z" : new Date().toISOString(),
      ttl_seconds: 120,
    };
  }
  mkdirSync(join(cachePath, ".."), { recursive: true });
  writeFileSync(cachePath, JSON.stringify(cache), "utf-8");
}

const defaultConfig: FrictionAlertConfig = {
  enabled: true,
  threshold: 3,
};

describe("checkFriction", () => {
  let tempRoot: string;
  let cachePath: string;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-friction-alert-"));
    cachePath = join(tempRoot, "state", "cache.json");
  });

  afterEach(() => {
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("fires warning when friction count equals threshold", () => {
    writeCache(cachePath, {
      recent_session: { friction_count: 3 },
      friction_counts: { wrong_approach: 2, misunderstood_request: 1 },
    });

    const result = checkFriction(defaultConfig, cachePath);
    expect(result).not.toBeNull();
    expect(result).toContain("3 friction events");
    expect(result).toContain("\u26A1");
  });

  it("fires warning when friction count exceeds threshold", () => {
    writeCache(cachePath, {
      recent_session: { friction_count: 7 },
      friction_counts: { wrong_approach: 5, misunderstood_request: 2 },
    });

    const result = checkFriction(defaultConfig, cachePath);
    expect(result).not.toBeNull();
    expect(result).toContain("7 friction events");
  });

  it("is silent when friction count is below threshold", () => {
    writeCache(cachePath, {
      recent_session: { friction_count: 1 },
      friction_counts: { wrong_approach: 1 },
    });

    const result = checkFriction(defaultConfig, cachePath);
    expect(result).toBeNull();
  });

  it("is silent when cache is missing", () => {
    // Don't write any cache file
    const result = checkFriction(defaultConfig, join(tempRoot, "nonexistent.json"));
    expect(result).toBeNull();
  });

  it("is silent when cache is stale", () => {
    writeCache(
      cachePath,
      {
        recent_session: { friction_count: 10 },
        friction_counts: { wrong_approach: 10 },
      },
      true, // stale
    );

    const result = checkFriction(defaultConfig, cachePath);
    expect(result).toBeNull();
  });

  it("respects configurable threshold", () => {
    writeCache(cachePath, {
      recent_session: { friction_count: 5 },
      friction_counts: { wrong_approach: 5 },
    });

    // Higher threshold: 5 < 10, should be silent
    const highThresholdResult = checkFriction(
      { enabled: true, threshold: 10 },
      cachePath,
    );
    expect(highThresholdResult).toBeNull();

    // Lower threshold: 5 >= 2, should fire
    const lowThresholdResult = checkFriction(
      { enabled: true, threshold: 2 },
      cachePath,
    );
    expect(lowThresholdResult).not.toBeNull();
  });

  it("warning message includes the top friction category", () => {
    writeCache(cachePath, {
      recent_session: { friction_count: 5 },
      friction_counts: { misunderstood_request: 1, wrong_approach: 4 },
    });

    const result = checkFriction(defaultConfig, cachePath);
    expect(result).not.toBeNull();
    expect(result).toContain("Top friction: wrong_approach");
  });

  it("is silent when disabled", () => {
    writeCache(cachePath, {
      recent_session: { friction_count: 10 },
      friction_counts: { wrong_approach: 10 },
    });

    const result = checkFriction({ enabled: false, threshold: 3 }, cachePath);
    expect(result).toBeNull();
  });
});
