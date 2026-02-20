/**
 * Coaching module barrel export.
 *
 * Re-exports the builder's trap detector, metacognition engine,
 * and communication coach.
 */

export {
  classifyMode,
  accumulateTime,
  computeAlertLevel,
} from "./builder-trap.js";

export { checkAndEmit } from "./metacognition.js";

export { analyze as analyzeGrammar } from "./communication.js";
