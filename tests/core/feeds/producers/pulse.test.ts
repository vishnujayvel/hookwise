/**
 * Tests for the pulse feed producer.
 *
 * Covers Task 4.1:
 * - Each threshold boundary (green, yellow, orange, red, skull)
 * - Custom thresholds override the defaults
 * - Missing session data returns null
 * - Exact boundary values (boundary belongs to the higher tier)
 * - Invalid startedAt returns null
 * - Elapsed minutes rounding
 *
 * Requirements: FR-4.1, FR-4.2, FR-4.3, FR-4.4, FR-4.5
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { mkdtempSync, writeFileSync, rmSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { createPulseProducer, mapElapsedToEmoji } from "../../../../src/core/feeds/producers/pulse.js";
import type { PulseFeedConfig } from "../../../../src/core/types.js";

// --- Default thresholds matching the spec ---

const DEFAULT_THRESHOLDS: PulseFeedConfig["thresholds"] = {
  green: 0,
  yellow: 30,
  orange: 60,
  red: 120,
  skull: 180,
};

function makeConfig(
  overrides?: Partial<PulseFeedConfig["thresholds"]>,
): PulseFeedConfig {
  return {
    enabled: true,
    intervalSeconds: 30,
    thresholds: { ...DEFAULT_THRESHOLDS, ...overrides },
  };
}

// --- mapElapsedToEmoji unit tests ---

describe("mapElapsedToEmoji", () => {
  const thresholds = DEFAULT_THRESHOLDS;

  it("returns green circle for 0 minutes", () => {
    expect(mapElapsedToEmoji(0, thresholds)).toBe("\u{1F7E2}");
  });

  it("returns green circle for 15 minutes (mid-green range)", () => {
    expect(mapElapsedToEmoji(15, thresholds)).toBe("\u{1F7E2}");
  });

  it("returns green circle for 29.9 minutes (just under yellow)", () => {
    expect(mapElapsedToEmoji(29.9, thresholds)).toBe("\u{1F7E2}");
  });

  it("returns yellow circle at exactly 30 minutes (FR-4.2 boundary)", () => {
    expect(mapElapsedToEmoji(30, thresholds)).toBe("\u{1F7E1}");
  });

  it("returns yellow circle for 45 minutes (mid-yellow range)", () => {
    expect(mapElapsedToEmoji(45, thresholds)).toBe("\u{1F7E1}");
  });

  it("returns yellow circle for 59.9 minutes (just under orange)", () => {
    expect(mapElapsedToEmoji(59.9, thresholds)).toBe("\u{1F7E1}");
  });

  it("returns orange circle at exactly 60 minutes (FR-4.2 boundary)", () => {
    expect(mapElapsedToEmoji(60, thresholds)).toBe("\u{1F7E0}");
  });

  it("returns orange circle for 90 minutes (mid-orange range)", () => {
    expect(mapElapsedToEmoji(90, thresholds)).toBe("\u{1F7E0}");
  });

  it("returns orange circle for 119.9 minutes (just under red)", () => {
    expect(mapElapsedToEmoji(119.9, thresholds)).toBe("\u{1F7E0}");
  });

  it("returns red circle at exactly 120 minutes (FR-4.2 boundary)", () => {
    expect(mapElapsedToEmoji(120, thresholds)).toBe("\u{1F534}");
  });

  it("returns red circle for 150 minutes (mid-red range)", () => {
    expect(mapElapsedToEmoji(150, thresholds)).toBe("\u{1F534}");
  });

  it("returns red circle for 179.9 minutes (just under skull)", () => {
    expect(mapElapsedToEmoji(179.9, thresholds)).toBe("\u{1F534}");
  });

  it("returns skull at exactly 180 minutes (FR-4.2 boundary)", () => {
    expect(mapElapsedToEmoji(180, thresholds)).toBe("\u{1F480}");
  });

  it("returns skull for 300 minutes (well past skull)", () => {
    expect(mapElapsedToEmoji(300, thresholds)).toBe("\u{1F480}");
  });

  it("handles negative elapsed gracefully (returns green)", () => {
    expect(mapElapsedToEmoji(-5, thresholds)).toBe("\u{1F7E2}");
  });
});

// --- Custom thresholds ---

describe("mapElapsedToEmoji with custom thresholds", () => {
  it("respects custom threshold overrides (FR-4.3)", () => {
    const custom: PulseFeedConfig["thresholds"] = {
      green: 0,
      yellow: 10,
      orange: 20,
      red: 40,
      skull: 60,
    };

    // 15 minutes is orange with custom (>= 10 yellow, >= 20 orange)
    expect(mapElapsedToEmoji(15, custom)).toBe("\u{1F7E1}");
    // 25 minutes is orange with custom
    expect(mapElapsedToEmoji(25, custom)).toBe("\u{1F7E0}");
    // 45 minutes is red with custom
    expect(mapElapsedToEmoji(45, custom)).toBe("\u{1F534}");
    // 60 minutes is skull with custom
    expect(mapElapsedToEmoji(60, custom)).toBe("\u{1F480}");
  });

  it("tight thresholds (all at same value) pick the highest tier", () => {
    const tight: PulseFeedConfig["thresholds"] = {
      green: 0,
      yellow: 10,
      orange: 10,
      red: 10,
      skull: 10,
    };

    // At 10 minutes, all thresholds match -- skull wins (checked first)
    expect(mapElapsedToEmoji(10, tight)).toBe("\u{1F480}");
    // At 9.9 minutes, none match except green
    expect(mapElapsedToEmoji(9.9, tight)).toBe("\u{1F7E2}");
  });
});

// --- createPulseProducer integration tests ---

describe("createPulseProducer", () => {
  let tempRoot: string;
  let cachePath: string;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-pulse-"));
    cachePath = join(tempRoot, "cache.json");
  });

  afterEach(() => {
    vi.useRealTimers();
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("returns null when cache is empty (no session) (FR-4.4)", async () => {
    writeFileSync(cachePath, JSON.stringify({}));
    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();
    expect(result).toBeNull();
  });

  it("returns null when session exists but has no startedAt (FR-4.4)", async () => {
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { id: "abc-123" } }),
    );
    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();
    expect(result).toBeNull();
  });

  it("returns null when startedAt is null (FR-4.4)", async () => {
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt: null } }),
    );
    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();
    expect(result).toBeNull();
  });

  it("returns null when startedAt is an unparseable string", async () => {
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt: "not-a-date" } }),
    );
    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();
    expect(result).toBeNull();
  });

  it("returns null when cache file does not exist", async () => {
    const producer = createPulseProducer(
      join(tempRoot, "nonexistent.json"),
      makeConfig(),
    );
    const result = await producer();
    expect(result).toBeNull();
  });

  it("returns green for a session started 5 minutes ago (FR-4.1)", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const startedAt = new Date(now - 5 * 60_000).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt } }),
    );

    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.value).toBe("\u{1F7E2}");
    expect(result!.elapsed_minutes).toBe(5);
    expect(result!.session_start).toBe(startedAt);
  });

  it("returns yellow for a session started 35 minutes ago", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const startedAt = new Date(now - 35 * 60_000).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt } }),
    );

    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.value).toBe("\u{1F7E1}");
    expect(result!.elapsed_minutes).toBe(35);
  });

  it("returns orange for a session started 90 minutes ago", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const startedAt = new Date(now - 90 * 60_000).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt } }),
    );

    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.value).toBe("\u{1F7E0}");
    expect(result!.elapsed_minutes).toBe(90);
  });

  it("returns red for a session started 150 minutes ago", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const startedAt = new Date(now - 150 * 60_000).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt } }),
    );

    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.value).toBe("\u{1F534}");
    expect(result!.elapsed_minutes).toBe(150);
  });

  it("returns skull for a session started 200 minutes ago", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const startedAt = new Date(now - 200 * 60_000).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt } }),
    );

    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.value).toBe("\u{1F480}");
    expect(result!.elapsed_minutes).toBe(200);
  });

  it("returns yellow at exact 30-minute boundary (FR-4.2)", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const startedAt = new Date(now - 30 * 60_000).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt } }),
    );

    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.value).toBe("\u{1F7E1}");
    expect(result!.elapsed_minutes).toBe(30);
  });

  it("returns orange at exact 60-minute boundary", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const startedAt = new Date(now - 60 * 60_000).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt } }),
    );

    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.value).toBe("\u{1F7E0}");
  });

  it("returns red at exact 120-minute boundary", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const startedAt = new Date(now - 120 * 60_000).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt } }),
    );

    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.value).toBe("\u{1F534}");
  });

  it("returns skull at exact 180-minute boundary", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const startedAt = new Date(now - 180 * 60_000).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt } }),
    );

    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.value).toBe("\u{1F480}");
  });

  it("uses custom thresholds when provided (FR-4.3)", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    // Custom: skull at 60 minutes
    const config = makeConfig({ skull: 60 });
    const startedAt = new Date(now - 65 * 60_000).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt } }),
    );

    const producer = createPulseProducer(cachePath, config);
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.value).toBe("\u{1F480}");
  });

  it("rounds elapsed_minutes to nearest integer (FR-4.5)", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    // 7.5 minutes ago
    const startedAt = new Date(now - 7.5 * 60_000).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt } }),
    );

    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();

    expect(result).not.toBeNull();
    // Math.round(7.5) === 8
    expect(result!.elapsed_minutes).toBe(8);
  });

  it("includes session_start in the output (FR-4.1)", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const startedAt = new Date(now - 10 * 60_000).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt } }),
    );

    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.session_start).toBe(startedAt);
  });

  it("preserves other session data and only reads startedAt", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const startedAt = new Date(now - 10 * 60_000).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({
        session: { startedAt, id: "sess-xyz", extra: "data" },
        cost: { totalToday: 1.50 },
      }),
    );

    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.value).toBe("\u{1F7E2}");
    // Should not include extra session fields in pulse output
    expect(result!.session_start).toBe(startedAt);
    expect(result).not.toHaveProperty("id");
    expect(result).not.toHaveProperty("extra");
  });

  it("returns green for a session started 0 minutes ago", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const startedAt = new Date(now).toISOString();
    writeFileSync(
      cachePath,
      JSON.stringify({ session: { startedAt } }),
    );

    const producer = createPulseProducer(cachePath, makeConfig());
    const result = await producer();

    expect(result).not.toBeNull();
    expect(result!.value).toBe("\u{1F7E2}");
    expect(result!.elapsed_minutes).toBe(0);
  });
});
