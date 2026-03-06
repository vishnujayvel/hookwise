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
 * Configuration for the multi-tier renderer.
 *
 * Supports N fixed lines, collapsible middle segments, and a rotating last line.
 * Default layout produces a minimum of 5 visible lines.
 */
export interface TwoTierConfig {
  /** Array of fixed lines — each inner array is segment names for one line. */
  fixedLines: string[][];
  /** Segment names for the last line (rotating). */
  rotatingSegments: string[];
  /** Segment names for middle section (multi-line, between fixed and rotating). */
  middleSegments?: string[];
  /** Whether to show a separator line before middle segments. */
  showSeparator?: boolean;
  /** Delimiter between segments on each line. */
  delimiter: string;
}

/** Default 5-line configuration. */
export const DEFAULT_TWO_TIER_CONFIG: TwoTierConfig = {
  fixedLines: [
    ["context_bar", "mode_badge", "cost", "duration", "daemon_health"],
    ["project", "calendar", "weather"],
    ["insights_friction", "insights_pace"],
    ["insights_trend"],
  ],
  rotatingSegments: ["news", "mantra", "memories", "pulse", "streak", "builder_trap", "clock"],
  middleSegments: ["agents"],
  showSeparator: true,
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
    case "daemon_health": {
      if (text.includes("stale")) return color(text, YELLOW);
      return color(text, GREEN);
    }
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
    // Fixed lines: render each fixedLines entry as one output line
    const fixedOutputLines: string[] = [];
    for (const lineSegments of config.fixedLines) {
      const parts: string[] = [];
      for (const name of lineSegments) {
        const renderer = BUILTIN_SEGMENTS[name];
        if (!renderer) continue;
        try {
          const raw = renderer(cache, {});
          if (raw) {
            parts.push(colorizeSegment(name, raw, cache));
          }
        } catch {
          // Skip failing segment — don't blank the entire line
        }
      }
      if (parts.length > 0) {
        fixedOutputLines.push(parts.join(config.delimiter));
      }
    }

    // Rotating line: pick the next non-empty one
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

    // Middle segments: multi-line content between fixed and rotating
    const middleLines: string[] = [];
    const middleSegmentNames = config.middleSegments ?? [];
    for (const name of middleSegmentNames) {
      const renderer = BUILTIN_SEGMENTS[name];
      if (!renderer) continue;
      try {
        const raw = renderer(cache, {});
        if (raw) {
          // Split multi-line segment content into individual lines
          const segLines = raw.split("\n");
          for (const segLine of segLines) {
            if (segLine) middleLines.push(segLine);
          }
        }
      } catch {
        // Skip failing segment
      }
    }

    const hasMiddle = middleLines.length > 0;
    const showSep = config.showSeparator ?? true;

    if (fixedOutputLines.length === 0 && !hasMiddle && !line2) return "";

    const outputLines: string[] = [];
    outputLines.push(...fixedOutputLines);

    if (hasMiddle) {
      if (showSep && outputLines.length > 0) {
        outputLines.push(color("---", DIM));
      }
      outputLines.push(...middleLines);
    }

    if (line2) outputLines.push(line2);

    return outputLines.join("\n");
  } catch {
    return "";
  }
}
