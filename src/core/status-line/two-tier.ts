/**
 * Two-tier status line renderer for hookwise v1.2.
 *
 * Line 1 (fixed): Always-visible metrics (context bar, mode, cost, duration).
 * Line 2 (rotating): Cycles through contextual feeds, skipping empty segments.
 *
 * All operations are synchronous and fail-open.
 */

import { BUILTIN_SEGMENTS } from "./segments.js";
import { color, GREEN, YELLOW, RED, DIM, CYAN } from "./ansi.js";

/**
 * Configuration for the two-tier renderer.
 */
export interface TwoTierConfig {
  /** Segment names for line 1 (always shown). */
  fixedSegments: string[];
  /** Segment names for line 2 (rotating). */
  rotatingSegments: string[];
  /** Delimiter between segments on each line. */
  delimiter: string;
}

/** Default configuration. */
export const DEFAULT_TWO_TIER_CONFIG: TwoTierConfig = {
  fixedSegments: ["context_bar", "mode_badge", "cost", "duration"],
  rotatingSegments: ["insights_friction", "insights_pace", "insights_trend", "news", "calendar", "practice_breadcrumb", "mantra", "project", "pulse"],
  delimiter: " | ",
};

/**
 * Apply ANSI color to a segment based on its name and the cache data.
 */
function colorizeSegment(name: string, text: string, cache: Record<string, unknown>): string {
  if (!text) return text;

  switch (name) {
    case "context_bar": {
      const stdin = cache._stdin as { context_window?: { used_percentage?: number } } | undefined;
      const pct = stdin?.context_window?.used_percentage ?? 0;
      if (pct >= 75) return color(text, RED);
      if (pct >= 50) return color(text, YELLOW);
      return color(text, GREEN);
    }
    case "mode_badge": {
      const bt = cache.builder_trap as { current_mode?: string } | undefined;
      const mode = bt?.current_mode ?? "";
      if (mode === "practice") return color(text, GREEN);
      if (mode === "tooling") return color(text, YELLOW);
      if (mode === "prep") return color(text, CYAN);
      return color(text, DIM);
    }
    case "cost":
      return color(text, DIM);
    case "duration":
      return color(text, DIM);
    case "builder_trap": {
      const btData = cache.builder_trap as { alertLevel?: string } | undefined;
      if (btData?.alertLevel === "red") return color(text, RED);
      if (btData?.alertLevel === "orange") return color(text, YELLOW);
      if (btData?.alertLevel === "yellow") return color(text, YELLOW);
      return text;
    }
    case "insights_friction": {
      const insData = cache.insights as { recent_session?: { friction_count?: number } } | undefined;
      const friction = insData?.recent_session?.friction_count ?? 0;
      return friction > 0 ? color(text, YELLOW) : color(text, GREEN);
    }
    case "insights_pace":
      return color(text, CYAN);
    case "insights_trend":
      return color(text, DIM);
    case "calendar":
      return color(text, CYAN);
    default:
      return text;
  }
}

/**
 * Render a two-tier status line.
 *
 * @param config - Two-tier layout configuration
 * @param cache - Merged cache object (includes _stdin data from Claude Code)
 * @returns Two lines joined by \n, or single line if line 2 is empty
 */
export function renderTwoTier(
  config: TwoTierConfig,
  cache: Record<string, unknown>,
): string {
  try {
    // Line 1: fixed segments
    const line1Parts: string[] = [];
    for (const name of config.fixedSegments) {
      const renderer = BUILTIN_SEGMENTS[name];
      if (!renderer) continue;
      try {
        const raw = renderer(cache, {});
        if (raw) {
          line1Parts.push(colorizeSegment(name, raw, cache));
        }
      } catch {
        // Skip failing segment — don't blank the entire line
      }
    }

    // Line 2: rotating segment — pick the next non-empty one
    const rotatingNames = config.rotatingSegments;
    const rotationIndex = (cache._rotation_index as number) ?? 0;
    let line2 = "";

    if (rotatingNames.length > 0) {
      // Try each segment starting from the rotation index, wrap around
      for (let attempt = 0; attempt < rotatingNames.length; attempt++) {
        const idx = (rotationIndex + attempt) % rotatingNames.length;
        const name = rotatingNames[idx];
        const renderer = BUILTIN_SEGMENTS[name];
        if (!renderer) continue;
        try {
          const raw = renderer(cache, {});
          if (raw) {
            line2 = colorizeSegment(name, raw, cache);
            break;
          }
        } catch {
          // Skip failing segment — try next one
        }
      }
    }

    const line1 = line1Parts.join(config.delimiter);

    if (!line1 && !line2) return "";
    if (!line2) return line1;
    if (!line1) return line2;

    return `${line1}\n${line2}`;
  } catch {
    return "";
  }
}
