/**
 * ANSI color utilities for the two-tier status line renderer.
 *
 * Lightweight escape-code helpers — no external dependencies.
 * Mirrors the raw ANSI approach used in the original bash status line.
 */

// Named ANSI color codes
export const RED = "31";
export const GREEN = "32";
export const YELLOW = "33";
export const BLUE = "34";
export const CYAN = "36";
export const DIM = "2";
export const BOLD = "1";

/**
 * Wrap text in ANSI escape codes.
 * Returns the text unchanged if colorCode is empty.
 */
export function color(text: string, colorCode: string): string {
  if (!colorCode) return text;
  return `\x1b[${colorCode}m${text}\x1b[0m`;
}

/**
 * Strip all ANSI escape codes from a string.
 * Useful for testing and calculating visible string length.
 */
export function strip(text: string): string {
  // eslint-disable-next-line no-control-regex
  return text.replace(/\x1b\[\d+m/g, "");
}
