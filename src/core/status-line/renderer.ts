/**
 * Status line renderer for hookwise v1.0.
 *
 * Composes segments from a mix of built-in and custom renderers,
 * reading cached state and joining with a configurable delimiter.
 *
 * All operations are synchronous and fail-open.
 */

import { spawnSync } from "node:child_process";
import { safeReadJSON } from "../state.js";
import { BUILTIN_SEGMENTS } from "./segments.js";
import type { StatusLineConfig, SegmentConfig } from "../types.js";

/**
 * Default timeout for custom segment commands in milliseconds.
 */
const DEFAULT_CUSTOM_TIMEOUT = 2000;

/**
 * Render a single segment, either builtin or custom.
 */
function renderSegment(
  segment: SegmentConfig,
  cache: Record<string, unknown>
): string {
  try {
    // Built-in segment
    if (segment.builtin) {
      const renderer = BUILTIN_SEGMENTS[segment.builtin];
      if (!renderer) return "";
      return renderer(cache, segment);
    }

    // Custom segment
    if (segment.custom) {
      const timeout = segment.custom.timeout ?? DEFAULT_CUSTOM_TIMEOUT;
      const result = spawnSync(segment.custom.command, {
        shell: true,
        timeout,
        encoding: "utf-8",
      });

      if (result.status !== 0 || result.error) return "";

      const output = (result.stdout ?? "").trim();
      if (!output) return "";

      if (segment.custom.label) {
        return `${segment.custom.label}: ${output}`;
      }
      return output;
    }

    return "";
  } catch {
    return "";
  }
}

/**
 * Render the full status line from configuration.
 *
 * Reads the cache file, processes each segment, and joins
 * non-empty results with the configured delimiter.
 *
 * @param config - Status line configuration
 * @returns The rendered status line string
 */
export function render(config: StatusLineConfig): string {
  try {
    const cache = safeReadJSON<Record<string, unknown>>(config.cachePath, {});

    const parts: string[] = [];
    for (const segment of config.segments) {
      const rendered = renderSegment(segment, cache);
      if (rendered) {
        parts.push(rendered);
      }
    }

    return parts.join(config.delimiter);
  } catch {
    return "";
  }
}
