/**
 * Shared integration test helpers for hookwise.
 *
 * Provides reusable setup/teardown and assertion utilities for
 * pipeline-wiring, dispatcher-wiring, and status-line-flow tests.
 *
 * Key design decisions:
 * - ARCH-1: No mocking of internal modules — imports real cache-bus, real AnalyticsDB
 * - ARCH-2: Temp dir isolation — every test gets its own temp directory
 * - All helpers are pure utilities with no global state
 *
 * Tasks: 1.1 (test env setup/teardown, config/payload builders)
 *        1.2 (cache seeding, analytics reading, fresh/stale helpers)
 */

import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import yaml from "js-yaml";
import Database from "better-sqlite3";
import { mergeKey } from "../../src/core/feeds/cache-bus.js";
import { getDefaultConfig } from "../../src/core/config.js";
import type { HooksConfig, HookPayload, CacheEntry } from "../../src/core/types.js";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface TestEnv {
  tmpDir: string;
  cachePath: string;
  dbPath: string;
  configPath: string;
  config: HooksConfig;
  cleanup: () => void;
}

export interface CacheSeedEntry {
  key: string;
  data: Record<string, unknown>;
  ttlSeconds?: number; // default: 300
}

// ---------------------------------------------------------------------------
// Task 1.1 — Test environment setup/teardown
// ---------------------------------------------------------------------------

/**
 * Simple recursive merge: for each key in source, if both target[key] and
 * source[key] are plain objects, recurse; otherwise source wins.
 */
function deepMerge(target: Record<string, unknown>, source: Record<string, unknown>): Record<string, unknown> {
  const result = { ...target };
  for (const key of Object.keys(source)) {
    if (
      source[key] && typeof source[key] === "object" && !Array.isArray(source[key]) &&
      target[key] && typeof target[key] === "object" && !Array.isArray(target[key])
    ) {
      result[key] = deepMerge(
        target[key] as Record<string, unknown>,
        source[key] as Record<string, unknown>,
      );
    } else {
      result[key] = source[key];
    }
  }
  return result;
}

/**
 * Build a minimal valid HooksConfig with all features enabled and paths
 * pointing to a given temp directory. Merges any caller overrides on top.
 */
export function makeIntegrationConfig(
  overrides: Partial<HooksConfig> = {},
  tmpDir?: string,
): HooksConfig {
  const base = getDefaultConfig();

  // Enable all major features
  base.analytics.enabled = true;
  base.statusLine.enabled = true;
  base.coaching.metacognition.enabled = true;
  base.feeds.pulse.enabled = true;
  base.feeds.project.enabled = true;
  base.feeds.insights.enabled = true;

  // Point paths to temp dir if provided
  if (tmpDir) {
    base.statusLine.cachePath = join(tmpDir, "status-line-cache.json");
    base.analytics.dbPath = join(tmpDir, "analytics.db");
    base.settings.stateDir = tmpDir;
  }

  // Apply caller overrides with deep merge to preserve nested defaults
  return deepMerge(base, overrides) as HooksConfig;
}

/**
 * Create an isolated test environment with a valid hookwise config,
 * cache path, analytics DB path, and cleanup function.
 *
 * The temp directory is created via `mkdtempSync` per ARCH-2.
 */
export function createTestEnv(
  configOverrides: Partial<HooksConfig> = {},
): TestEnv {
  const tmpDir = mkdtempSync(join(tmpdir(), "hookwise-integ-"));
  const cachePath = join(tmpDir, "status-line-cache.json");
  const dbPath = join(tmpDir, "analytics.db");
  const configPath = join(tmpDir, "hookwise.yaml");

  const config = makeIntegrationConfig(configOverrides, tmpDir);

  // Write a real hookwise.yaml so loadConfig() can find it
  writeFileSync(
    configPath,
    yaml.dump(
      {
        version: config.version,
        analytics: { enabled: config.analytics.enabled, db_path: dbPath },
        status_line: {
          enabled: config.statusLine.enabled,
          segments: config.statusLine.segments,
          delimiter: config.statusLine.delimiter,
          cache_path: cachePath,
        },
        coaching: {
          metacognition: {
            enabled: config.coaching.metacognition.enabled,
            interval_seconds: config.coaching.metacognition.intervalSeconds,
          },
        },
        feeds: {
          pulse: {
            enabled: config.feeds.pulse.enabled,
            interval_seconds: config.feeds.pulse.intervalSeconds,
          },
          project: {
            enabled: config.feeds.project.enabled,
            interval_seconds: config.feeds.project.intervalSeconds,
          },
          insights: {
            enabled: config.feeds.insights.enabled,
            interval_seconds: config.feeds.insights.intervalSeconds,
          },
        },
        handlers: [],
        settings: {
          log_level: config.settings.logLevel,
          handler_timeout_seconds: config.settings.handlerTimeoutSeconds,
          state_dir: tmpDir,
        },
      },
      { indent: 2, noRefs: true },
    ),
    "utf-8",
  );

  const cleanup = () => {
    rmSync(tmpDir, { recursive: true, force: true });
  };

  return { tmpDir, cachePath, dbPath, configPath, config, cleanup };
}

/**
 * Build a valid HookPayload with sensible defaults and optional overrides.
 */
export function makePayload(
  overrides: Partial<HookPayload> = {},
): HookPayload {
  return {
    session_id: "integ-test-session",
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Task 1.2 — Cache seeding and analytics reading
// ---------------------------------------------------------------------------

/**
 * Seed a real cache file with multiple entries via the actual cache-bus
 * `mergeKey()` function. Each entry is written with proper freshness metadata.
 *
 * ARCH-1: Uses the real mergeKey implementation — no mocking.
 */
export function seedCache(
  cachePath: string,
  entries: CacheSeedEntry[],
): void {
  for (const entry of entries) {
    mergeKey(cachePath, entry.key, entry.data, entry.ttlSeconds ?? 300);
  }
}

/**
 * Create a fresh CacheEntry (within TTL) for use in assertions and
 * in-memory cache objects passed to segment renderers.
 *
 * @param data - Payload fields to include in the entry
 * @param ttl  - TTL in seconds (default: 300)
 */
export function freshEntry(
  data: Record<string, unknown>,
  ttl = 300,
): CacheEntry {
  return {
    updated_at: new Date().toISOString(),
    ttl_seconds: ttl,
    ...data,
  };
}

/**
 * Create a stale CacheEntry (TTL expired) for use in assertions.
 * The `updated_at` timestamp is set far enough in the past that the
 * entry's TTL of 60 seconds is guaranteed to have expired.
 */
export function staleEntry(
  data: Record<string, unknown>,
): CacheEntry {
  return {
    updated_at: new Date(Date.now() - 120_000).toISOString(), // 2 min ago
    ttl_seconds: 60, // expired
    ...data,
  };
}

/**
 * Open a real SQLite analytics database file and return the contents of
 * the sessions, events, and authorship_ledger tables for assertion.
 *
 * The database connection is properly closed after reading.
 *
 * ARCH-1: Uses real better-sqlite3 — no mocking.
 */
export function readAnalyticsDB(dbPath: string): {
  sessions: unknown[];
  events: unknown[];
  authorship: unknown[];
} {
  const db = new Database(dbPath, { readonly: true });
  try {
    const sessions = db.prepare("SELECT * FROM sessions").all();
    const events = db.prepare("SELECT * FROM events ORDER BY timestamp").all();
    const authorship = db
      .prepare("SELECT * FROM authorship_ledger ORDER BY timestamp")
      .all();
    return { sessions, events, authorship };
  } finally {
    db.close();
  }
}
