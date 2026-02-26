/**
 * CacheBus: Per-key cache merge and TTL-aware reads for the Feed Platform.
 *
 * All functions are synchronous to match the dispatch hot-path model.
 * Uses atomicWriteJSON / safeReadJSON from state.ts for safe file I/O.
 *
 * Cache structure on disk:
 * {
 *   "session": { ... dispatch data ... },
 *   "pulse":   { updated_at: "...", ttl_seconds: 30, idle_minutes: 5 },
 *   "project": { updated_at: "...", ttl_seconds: 60, branch: "main" }
 * }
 *
 * Requirements: FR-3.1, FR-3.2, FR-3.3, FR-3.4, FR-3.5, FR-3.6, FR-12.3, FR-12.4
 */

import type { CacheEntry } from "../types.js";
import { atomicWriteJSON, safeReadJSON } from "../state.js";

/**
 * Check if a cache entry is fresh (within its TTL).
 *
 * An entry is fresh when:
 *   Date.now() < Date.parse(updated_at) + ttl_seconds * 1000
 *
 * Returns false (stale) if updated_at is missing, unparseable, or
 * ttl_seconds is missing/non-positive.
 */
export function isFresh(entry: CacheEntry): boolean {
  if (!entry.updated_at || typeof entry.updated_at !== "string") return false;
  if (typeof entry.ttl_seconds !== "number" || entry.ttl_seconds <= 0) return false;

  const updatedAt = Date.parse(entry.updated_at);
  if (Number.isNaN(updatedAt)) return false;

  const expiresAt = updatedAt + entry.ttl_seconds * 1000;
  return Date.now() < expiresAt;
}

/**
 * Read the full cache, merge a single key with freshness metadata,
 * and write back atomically. All other keys are preserved untouched.
 *
 * The written entry always includes:
 *   - All properties from `data`
 *   - `updated_at`: current ISO 8601 timestamp
 *   - `ttl_seconds`: the caller-specified TTL
 *
 * Corrupt or missing cache files are treated as empty objects (fail-open per FR-3.6).
 */
export function mergeKey(
  cachePath: string,
  key: string,
  data: Record<string, unknown>,
  ttlSeconds: number,
): void {
  const raw = safeReadJSON<unknown>(cachePath, {});
  const cache: Record<string, unknown> =
    raw !== null && typeof raw === "object" && !Array.isArray(raw)
      ? (raw as Record<string, unknown>)
      : {};
  cache[key] = {
    ...data,
    updated_at: new Date().toISOString(),
    ttl_seconds: ttlSeconds,
  };
  atomicWriteJSON(cachePath, cache);
}

/**
 * Read a single key from cache. Returns null if:
 *   - The cache file is missing or corrupt (fail-open)
 *   - The key does not exist
 *   - The entry is stale (TTL expired)
 */
export function readKey<T extends CacheEntry>(
  cachePath: string,
  key: string,
): T | null {
  const raw = safeReadJSON<unknown>(cachePath, {});
  const cache: Record<string, unknown> =
    raw !== null && typeof raw === "object" && !Array.isArray(raw)
      ? (raw as Record<string, unknown>)
      : {};
  const entry = cache[key];
  if (!entry || typeof entry !== "object") return null;

  const cacheEntry = entry as CacheEntry;
  if (!isFresh(cacheEntry)) return null;

  return cacheEntry as T;
}

/**
 * Read the entire cache object (for backward compat with status line renderer).
 *
 * Returns {} if the file is missing or corrupt.
 */
export function readAll(cachePath: string): Record<string, unknown> {
  const raw = safeReadJSON<unknown>(cachePath, {});
  if (raw !== null && typeof raw === "object" && !Array.isArray(raw)) {
    return raw as Record<string, unknown>;
  }
  return {};
}
