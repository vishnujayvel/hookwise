/**
 * Status line module barrel export.
 *
 * Re-exports the renderer, segment registry, ANSI utilities,
 * and two-tier renderer.
 */

export { render } from "./renderer.js";
export { BUILTIN_SEGMENTS } from "./segments.js";
export { color, strip, RED, GREEN, YELLOW, BLUE, CYAN, DIM, BOLD } from "./ansi.js";
export { renderTwoTier, DEFAULT_TWO_TIER_CONFIG } from "./two-tier.js";
export type { TwoTierConfig } from "./two-tier.js";
