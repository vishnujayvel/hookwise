/**
 * Feed Platform barrel export.
 *
 * Re-exports the CacheBus utilities, FeedRegistry, and feed producers
 * so consumers can import from "@/core/feeds" without reaching into
 * internal modules.
 */

export { isFresh, mergeKey, readKey, readAll } from "./cache-bus.js";
export { createFeedRegistry, createCommandProducer } from "./registry.js";
export type { FeedRegistry } from "./registry.js";
export { createPulseProducer, mapElapsedToEmoji } from "./producers/pulse.js";
export type { PulseData } from "./producers/pulse.js";
export { createProjectProducer } from "./producers/project.js";
export type { ProjectData } from "./producers/project.js";
export { createNewsProducer } from "./producers/news.js";
export type { NewsStory, NewsData } from "./producers/news.js";
export { createCalendarProducer, stripHtmlTags } from "./producers/calendar.js";
export type { CalendarEvent, CalendarData } from "./producers/calendar.js";
export { createInsightsProducer, aggregateInsights } from "./producers/insights.js";
export type { InsightsData } from "./producers/insights.js";
export {
  isRunning,
  startDaemon,
  stopDaemon,
  getDaemonStatus,
} from "./daemon-manager.js";
export type {
  DaemonStartResult,
  DaemonStopResult,
  DaemonStatus,
  FeedHealth,
} from "./daemon-manager.js";
export {
  runDaemon,
  daemonLog,
  rotateLog,
  registerBuiltinFeeds,
  registerCustomFeeds,
} from "./daemon-process.js";
