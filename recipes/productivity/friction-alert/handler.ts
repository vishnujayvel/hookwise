/**
 * Friction Alert recipe handler.
 *
 * Reads the insights cache key and prints a non-blocking warning
 * when recent session friction meets or exceeds the configured threshold.
 *
 * Requirements: FR-4.1, FR-4.2, FR-4.3, FR-4.4, FR-4.5
 */

import { readKey } from "../../../src/core/feeds/cache-bus.js";
import { DEFAULT_CACHE_PATH } from "../../../src/core/constants.js";
import type { CacheEntry } from "../../../src/core/types.js";

export interface FrictionAlertConfig {
  enabled: boolean;
  threshold: number;
}

interface InsightsCacheEntry extends CacheEntry {
  friction_counts?: Record<string, number>;
  recent_session?: {
    friction_count?: number;
  };
}

/**
 * Check the insights cache and return a warning message if friction exceeds threshold.
 *
 * @param config - Recipe configuration
 * @param cachePath - Path to cache file (injectable for testing)
 * @returns Warning message string, or null if no warning needed
 */
export function checkFriction(
  config: FrictionAlertConfig,
  cachePath: string = DEFAULT_CACHE_PATH,
): string | null {
  if (!config.enabled) return null;

  const insights = readKey<InsightsCacheEntry>(cachePath, "insights");
  if (!insights) return null;

  const frictionCount = insights.recent_session?.friction_count ?? 0;
  const threshold = config.threshold ?? 3;

  if (frictionCount < threshold) return null;

  // Find top friction category
  const frictionCounts = insights.friction_counts ?? {};
  const topCategory = Object.entries(frictionCounts)
    .sort((a, b) => b[1] - a[1])
    .map(([name]) => name)[0] ?? "unknown";

  return `\u26A1 Pattern detected: ${frictionCount} friction events in last session. Top friction: ${topCategory}. Consider research-first prompting.`;
}
