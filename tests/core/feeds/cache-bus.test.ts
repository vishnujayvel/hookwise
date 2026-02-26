/**
 * Tests for CacheBus: per-key cache merge and TTL-aware reads.
 *
 * Covers Tasks 2.1 and 2.2:
 *
 * Task 2.1 (mergeKey):
 * - Basic merge writes key with data + freshness metadata
 * - Preserving existing keys (dispatch-written session, cost, streak survive)
 * - Overwriting same key updates data and timestamp
 * - Corrupt cache file: fail-open, treated as empty
 * - Missing cache file: creates new file with single key
 *
 * Task 2.2 (readKey, isFresh, readAll):
 * - Fresh entry returned correctly
 * - Stale entry (TTL expired) returns null
 * - Missing entry returns null
 * - Missing updated_at treated as stale
 * - readAll returns full cache object
 * - isFresh utility edge cases
 *
 * Requirements: FR-3.1, FR-3.2, FR-3.3, FR-3.4, FR-3.5, FR-3.6, FR-12.3, FR-12.4
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  mkdtempSync,
  mkdirSync,
  writeFileSync,
  readFileSync,
  rmSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { isFresh, mergeKey, readKey, readAll } from "../../../src/core/feeds/cache-bus.js";
import type { CacheEntry } from "../../../src/core/types.js";

describe("isFresh", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns true for an entry within its TTL", () => {
    const entry: CacheEntry = {
      updated_at: new Date().toISOString(),
      ttl_seconds: 60,
    };
    expect(isFresh(entry)).toBe(true);
  });

  it("returns false for an entry past its TTL", () => {
    // 120 seconds ago, TTL = 60 seconds → stale
    const past = new Date(Date.now() - 120_000).toISOString();
    const entry: CacheEntry = {
      updated_at: past,
      ttl_seconds: 60,
    };
    expect(isFresh(entry)).toBe(false);
  });

  it("returns false when updated_at is missing", () => {
    const entry = { ttl_seconds: 60 } as unknown as CacheEntry;
    expect(isFresh(entry)).toBe(false);
  });

  it("returns false when updated_at is not a string", () => {
    const entry = { updated_at: 12345, ttl_seconds: 60 } as unknown as CacheEntry;
    expect(isFresh(entry)).toBe(false);
  });

  it("returns false when updated_at is unparseable", () => {
    const entry: CacheEntry = {
      updated_at: "not-a-date",
      ttl_seconds: 60,
    };
    expect(isFresh(entry)).toBe(false);
  });

  it("returns false when ttl_seconds is missing", () => {
    const entry = { updated_at: new Date().toISOString() } as unknown as CacheEntry;
    expect(isFresh(entry)).toBe(false);
  });

  it("returns false when ttl_seconds is zero", () => {
    const entry: CacheEntry = {
      updated_at: new Date().toISOString(),
      ttl_seconds: 0,
    };
    expect(isFresh(entry)).toBe(false);
  });

  it("returns false when ttl_seconds is negative", () => {
    const entry: CacheEntry = {
      updated_at: new Date().toISOString(),
      ttl_seconds: -10,
    };
    expect(isFresh(entry)).toBe(false);
  });

  it("returns false at exact expiration boundary", () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const entry: CacheEntry = {
      updated_at: new Date(now - 60_000).toISOString(),
      ttl_seconds: 60,
    };
    // now === updated_at + ttl_seconds * 1000 → not strictly less than → stale
    expect(isFresh(entry)).toBe(false);
  });

  it("returns true 1ms before expiration", () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    const entry: CacheEntry = {
      updated_at: new Date(now - 59_999).toISOString(),
      ttl_seconds: 60,
    };
    expect(isFresh(entry)).toBe(true);
  });
});

describe("mergeKey", () => {
  let tempRoot: string;
  let cachePath: string;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-cachebus-"));
    cachePath = join(tempRoot, "state", "status-line-cache.json");
  });

  afterEach(() => {
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("writes a key with data and freshness metadata (FR-3.1, FR-3.2)", () => {
    mergeKey(cachePath, "pulse", { idle_minutes: 5 }, 30);

    const cache = JSON.parse(readFileSync(cachePath, "utf-8"));
    expect(cache.pulse).toBeDefined();
    expect(cache.pulse.idle_minutes).toBe(5);
    expect(cache.pulse.ttl_seconds).toBe(30);
    expect(typeof cache.pulse.updated_at).toBe("string");

    // Verify updated_at is a valid ISO 8601 timestamp
    const parsed = Date.parse(cache.pulse.updated_at);
    expect(Number.isNaN(parsed)).toBe(false);
  });

  it("preserves existing keys — dispatch-written data survives (FR-3.3)", () => {
    // Simulate dispatch writing session + cost + streak data
    const existingCache = {
      session: { id: "abc-123", startedAt: "2026-02-22T10:00:00Z" },
      cost: { totalToday: 1.23, enforcement: "warn" },
      streak: { current: 5, lastDate: "2026-02-22" },
    };
    // Pre-create the directory and file
    mkdirSync(join(tempRoot, "state"), { recursive: true });
    writeFileSync(cachePath, JSON.stringify(existingCache));

    // Feed writes pulse key
    mergeKey(cachePath, "pulse", { idle_minutes: 3 }, 30);

    const cache = JSON.parse(readFileSync(cachePath, "utf-8"));

    // All existing keys preserved
    expect(cache.session).toEqual({ id: "abc-123", startedAt: "2026-02-22T10:00:00Z" });
    expect(cache.cost).toEqual({ totalToday: 1.23, enforcement: "warn" });
    expect(cache.streak).toEqual({ current: 5, lastDate: "2026-02-22" });

    // New key also present
    expect(cache.pulse.idle_minutes).toBe(3);
    expect(cache.pulse.ttl_seconds).toBe(30);
  });

  it("overwrites same key with new data and fresh timestamp (FR-3.4)", () => {
    mergeKey(cachePath, "pulse", { idle_minutes: 3 }, 30);
    const firstCache = JSON.parse(readFileSync(cachePath, "utf-8"));
    const firstTimestamp = firstCache.pulse.updated_at;

    // Small delay to ensure different timestamp
    const later = Date.now() + 10;
    while (Date.now() < later) {
      /* busy wait */
    }

    mergeKey(cachePath, "pulse", { idle_minutes: 10, extra: "new" }, 60);
    const secondCache = JSON.parse(readFileSync(cachePath, "utf-8"));

    expect(secondCache.pulse.idle_minutes).toBe(10);
    expect(secondCache.pulse.extra).toBe("new");
    expect(secondCache.pulse.ttl_seconds).toBe(60);
    // Timestamp should be updated
    expect(secondCache.pulse.updated_at).not.toBe(firstTimestamp);
  });

  it("handles corrupt cache file — fail-open, starts fresh (FR-3.6)", () => {
    mkdirSync(join(tempRoot, "state"), { recursive: true });
    writeFileSync(cachePath, "{{not valid json at all!!!");

    // Should not throw — corrupt cache treated as empty
    expect(() => {
      mergeKey(cachePath, "pulse", { idle_minutes: 1 }, 30);
    }).not.toThrow();

    const cache = JSON.parse(readFileSync(cachePath, "utf-8"));
    expect(cache.pulse).toBeDefined();
    expect(cache.pulse.idle_minutes).toBe(1);
  });

  it("handles missing cache file — creates new (FR-3.6)", () => {
    // cachePath does not exist yet
    mergeKey(cachePath, "project", { branch: "main" }, 60);

    const cache = JSON.parse(readFileSync(cachePath, "utf-8"));
    expect(cache.project.branch).toBe("main");
    expect(cache.project.ttl_seconds).toBe(60);
  });

  it("freshness metadata overwrites user-supplied updated_at and ttl_seconds", () => {
    // Even if data includes these fields, mergeKey's own values take precedence
    mergeKey(
      cachePath,
      "pulse",
      { updated_at: "1999-01-01T00:00:00Z", ttl_seconds: 9999 } as Record<string, unknown>,
      30,
    );

    const cache = JSON.parse(readFileSync(cachePath, "utf-8"));
    expect(cache.pulse.ttl_seconds).toBe(30);
    expect(cache.pulse.updated_at).not.toBe("1999-01-01T00:00:00Z");
  });

  it("handles non-object JSON root (null) — fail-open (FR-3.6)", () => {
    mkdirSync(join(tempRoot, "state"), { recursive: true });
    writeFileSync(cachePath, "null");

    expect(() => {
      mergeKey(cachePath, "pulse", { idle_minutes: 1 }, 30);
    }).not.toThrow();

    const cache = JSON.parse(readFileSync(cachePath, "utf-8"));
    expect(cache.pulse).toBeDefined();
    expect(cache.pulse.idle_minutes).toBe(1);
  });

  it("handles non-object JSON root (array) — fail-open (FR-3.6)", () => {
    mkdirSync(join(tempRoot, "state"), { recursive: true });
    writeFileSync(cachePath, "[1, 2, 3]");

    expect(() => {
      mergeKey(cachePath, "pulse", { idle_minutes: 2 }, 30);
    }).not.toThrow();

    const cache = JSON.parse(readFileSync(cachePath, "utf-8"));
    expect(cache.pulse).toBeDefined();
    expect(cache.pulse.idle_minutes).toBe(2);
  });

  it("merging multiple different keys accumulates all entries", () => {
    mergeKey(cachePath, "pulse", { idle_minutes: 1 }, 30);
    mergeKey(cachePath, "project", { branch: "feature" }, 60);
    mergeKey(cachePath, "calendar", { next: "standup" }, 300);

    const cache = JSON.parse(readFileSync(cachePath, "utf-8"));
    expect(Object.keys(cache)).toHaveLength(3);
    expect(cache.pulse.idle_minutes).toBe(1);
    expect(cache.project.branch).toBe("feature");
    expect(cache.calendar.next).toBe("standup");
  });
});

describe("readKey", () => {
  let tempRoot: string;
  let cachePath: string;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-cachebus-"));
    cachePath = join(tempRoot, "cache.json");
  });

  afterEach(() => {
    vi.useRealTimers();
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("returns fresh entry with all data (FR-3.5)", () => {
    const entry = {
      idle_minutes: 5,
      updated_at: new Date().toISOString(),
      ttl_seconds: 60,
    };
    writeFileSync(cachePath, JSON.stringify({ pulse: entry }));

    const result = readKey<CacheEntry & { idle_minutes: number }>(cachePath, "pulse");
    expect(result).not.toBeNull();
    expect(result!.idle_minutes).toBe(5);
    expect(result!.ttl_seconds).toBe(60);
    expect(result!.updated_at).toBe(entry.updated_at);
  });

  it("returns null for stale entry (TTL expired) (FR-12.3)", () => {
    const staleEntry = {
      idle_minutes: 5,
      updated_at: new Date(Date.now() - 120_000).toISOString(),
      ttl_seconds: 60,
    };
    writeFileSync(cachePath, JSON.stringify({ pulse: staleEntry }));

    const result = readKey(cachePath, "pulse");
    expect(result).toBeNull();
  });

  it("returns null for missing key (FR-12.3)", () => {
    writeFileSync(cachePath, JSON.stringify({ other: { updated_at: new Date().toISOString(), ttl_seconds: 60 } }));
    const result = readKey(cachePath, "pulse");
    expect(result).toBeNull();
  });

  it("returns null when cache file does not exist (FR-3.6)", () => {
    const result = readKey(join(tempRoot, "nonexistent.json"), "pulse");
    expect(result).toBeNull();
  });

  it("returns null when cache file is corrupt (FR-3.6)", () => {
    writeFileSync(cachePath, "not json!!!");
    const result = readKey(cachePath, "pulse");
    expect(result).toBeNull();
  });

  it("returns null when entry has missing updated_at (treated as stale) (FR-12.4)", () => {
    const entry = { idle_minutes: 5, ttl_seconds: 60 };
    writeFileSync(cachePath, JSON.stringify({ pulse: entry }));

    const result = readKey(cachePath, "pulse");
    expect(result).toBeNull();
  });

  it("returns null when entry is not an object (e.g., string value)", () => {
    writeFileSync(cachePath, JSON.stringify({ pulse: "just a string" }));
    const result = readKey(cachePath, "pulse");
    expect(result).toBeNull();
  });

  it("returns null when entry is null", () => {
    writeFileSync(cachePath, JSON.stringify({ pulse: null }));
    const result = readKey(cachePath, "pulse");
    expect(result).toBeNull();
  });

  it("round-trips with mergeKey — written entry is immediately readable", () => {
    mergeKey(cachePath, "project", { branch: "main", sha: "abc123" }, 120);

    const result = readKey<CacheEntry & { branch: string; sha: string }>(cachePath, "project");
    expect(result).not.toBeNull();
    expect(result!.branch).toBe("main");
    expect(result!.sha).toBe("abc123");
    expect(result!.ttl_seconds).toBe(120);
  });

  it("round-trips with mergeKey — entry becomes stale after TTL", () => {
    vi.useFakeTimers();
    const now = Date.now();
    vi.setSystemTime(now);

    mergeKey(cachePath, "pulse", { idle_minutes: 1 }, 30);

    // Still fresh
    expect(readKey(cachePath, "pulse")).not.toBeNull();

    // Advance past TTL
    vi.setSystemTime(now + 31_000);
    expect(readKey(cachePath, "pulse")).toBeNull();
  });
});

describe("readAll", () => {
  let tempRoot: string;
  let cachePath: string;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-cachebus-"));
    cachePath = join(tempRoot, "cache.json");
  });

  afterEach(() => {
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("returns full cache object with all keys (FR-12.4)", () => {
    const cache = {
      session: { id: "abc-123" },
      pulse: { idle_minutes: 5, updated_at: new Date().toISOString(), ttl_seconds: 30 },
      cost: { totalToday: 0.50 },
    };
    writeFileSync(cachePath, JSON.stringify(cache));

    const result = readAll(cachePath);
    expect(result).toEqual(cache);
  });

  it("returns empty object for missing cache file", () => {
    const result = readAll(join(tempRoot, "nonexistent.json"));
    expect(result).toEqual({});
  });

  it("returns empty object for corrupt cache file", () => {
    writeFileSync(cachePath, "{{corrupt");
    const result = readAll(cachePath);
    expect(result).toEqual({});
  });

  it("returns stale entries too — no TTL filtering", () => {
    const staleEntry = {
      idle_minutes: 5,
      updated_at: new Date(Date.now() - 120_000).toISOString(),
      ttl_seconds: 60,
    };
    writeFileSync(cachePath, JSON.stringify({ pulse: staleEntry }));

    const result = readAll(cachePath);
    expect(result.pulse).toEqual(staleEntry);
  });

  it("reflects changes from mergeKey", () => {
    mergeKey(cachePath, "pulse", { idle: 1 }, 30);
    mergeKey(cachePath, "project", { branch: "dev" }, 60);

    const result = readAll(cachePath);
    expect(Object.keys(result)).toContain("pulse");
    expect(Object.keys(result)).toContain("project");
    expect((result.pulse as Record<string, unknown>).idle).toBe(1);
    expect((result.project as Record<string, unknown>).branch).toBe("dev");
  });
});
