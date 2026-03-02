/**
 * Pulse feed producer: emits a session-duration emoji based on elapsed time.
 *
 * Reads the session start time from the cache (the dispatch-written `session`
 * key), computes elapsed minutes, and maps to an emoji via configurable
 * thresholds:
 *
 *   green (0-30m)  yellow (30-60m)  orange (1-2h)  red (2-3h)  skull (3h+)
 *
 * Returns null when no active session exists (missing session or startedAt).
 *
 * Requirements: FR-4.1, FR-4.2, FR-4.3, FR-4.4, FR-4.5
 */

import type { FeedProducer, PulseFeedConfig } from "../../types.js";
import { readAll } from "../cache-bus.js";

export interface PulseData {
  value: string;           // emoji: green, yellow, orange, red, or skull
  elapsed_minutes: number;
  session_start: string;   // ISO 8601
}

/**
 * Map elapsed minutes to the appropriate emoji based on configured thresholds.
 *
 * Thresholds are checked from highest to lowest (skull -> red -> orange ->
 * yellow -> green fallback). Each threshold value is the number of minutes at
 * which that level activates.
 */
export function mapElapsedToEmoji(
  elapsedMinutes: number,
  thresholds: PulseFeedConfig["thresholds"],
): string {
  if (elapsedMinutes >= thresholds.skull) return "\u{1F480}";   // skull
  if (elapsedMinutes >= thresholds.red) return "\u{1F534}";     // red circle
  if (elapsedMinutes >= thresholds.orange) return "\u{1F7E0}";  // orange circle
  if (elapsedMinutes >= thresholds.yellow) return "\u{1F7E1}";  // yellow circle
  return "\u{1F7E2}";                                           // green circle
}

/**
 * Create a FeedProducer for the pulse feed.
 *
 * @param cachePath - Path to the status-line cache JSON file
 * @param config    - Pulse feed configuration including thresholds
 */
export function createPulseProducer(
  cachePath: string,
  config: PulseFeedConfig,
): FeedProducer {
  return async (): Promise<Record<string, unknown> | null> => {
    const cache = readAll(cachePath);
    const session = cache.session as Record<string, unknown> | undefined;

    if (!session?.startedAt) return null;

    const startedAt = session.startedAt as string;
    const startTime = Date.parse(startedAt);
    if (Number.isNaN(startTime)) return null;

    const elapsedMinutes = (Date.now() - startTime) / 60_000;
    const emoji = mapElapsedToEmoji(elapsedMinutes, config.thresholds);

    const result: PulseData = {
      value: emoji,
      elapsed_minutes: Math.round(elapsedMinutes),
      session_start: startedAt,
    };

    return result as unknown as Record<string, unknown>;
  };
}
