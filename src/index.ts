/**
 * hookwise v1.1 — Public API exports
 *
 * Config-driven hook framework for Claude Code with guards,
 * analytics, coaching, feed platform, and interactive TUI.
 */

// Core types
export type {
  EventType,
  HookPayload,
  DispatchResult,
  HandlerResult,
  HooksConfig,
  CoachingConfig,
  AnalyticsConfig,
  GreetingConfig,
  QuoteEntry,
  SoundsConfig,
  StatusLineConfig,
  CostTrackingConfig,
  TranscriptConfig,
  SettingsConfig,
  ValidationResult,
  ValidationError,
  ResolvedHandler,
  CustomHandlerConfig,
  GuardRule,
  GuardResult,
  GuardRuleConfig,
  ParsedCondition,
  SegmentConfig,
  AIClassification,
  AIConfidenceScore,
  AnalyticsEvent,
  SessionSummary,
  DailySummary,
  ToolBreakdownEntry,
  AuthorshipSummary,
  StatsOptions,
  StatsResult,
  AuthorshipEntry,
  Mode,
  AlertLevel,
  LargeChangeRecord,
  CoachingCache,
  MetacognitionResult,
  GrammarResult,
  GrammarIssue,
  CostEstimate,
  BudgetStatus,
  CostState,
  FileConflict,
  TestScenario,
  ScenarioResult,
  FeedProducer,
  FeedDefinition,
  FeedsConfig,
  DaemonConfig,
  PulseFeedConfig,
  ProjectFeedConfig,
  CalendarFeedConfig,
  NewsFeedConfig,
  CustomFeedConfig,
  CacheEntry,
} from "./core/types.js";

// Type guards and constants
export { EVENT_TYPES, isEventType, isHookPayload } from "./core/types.js";

// Error classes
export {
  HookwiseError,
  ConfigError,
  HandlerTimeoutError,
  StateError,
  AnalyticsError,
} from "./core/errors.js";

// State utilities
export {
  atomicWriteJSON,
  safeReadJSON,
  ensureDir,
  getStateDir,
} from "./core/state.js";

// Constants
export {
  DEFAULT_STATE_DIR,
  DEFAULT_DB_PATH,
  DEFAULT_CACHE_PATH,
  DEFAULT_LOG_PATH,
  DEFAULT_HANDLER_TIMEOUT,
  DEFAULT_STATUS_DELIMITER,
  DEFAULT_TRANSCRIPT_DIR,
  PROJECT_CONFIG_FILE,
  GLOBAL_CONFIG_PATH,
  DEFAULT_PID_PATH,
  DEFAULT_DAEMON_LOG_PATH,
  DEFAULT_CALENDAR_CREDENTIALS_PATH,
} from "./core/constants.js";

// Config engine
export {
  loadConfig,
  validateConfig,
  resolveHandlers,
  getHandlersForEvent,
  saveConfig,
  getDefaultConfig,
  deepMerge,
  interpolateEnvVars,
  snakeToCamel,
  camelToSnake,
  deepSnakeToCamel,
  deepCamelToSnake,
} from "./core/config.js";

// Dispatcher
export {
  readStdinPayload,
  dispatch,
  executeHandler,
} from "./core/dispatcher.js";

// Guards
export {
  evaluate,
  parseCondition,
  evaluateCondition,
  resolveFieldPath,
} from "./core/guards.js";

// Analytics
export {
  AnalyticsDB,
  AnalyticsEngine,
  AuthorshipLedger,
  queryStats,
  queryDailySummary,
  queryToolBreakdown,
  queryAuthorshipSummary,
} from "./core/analytics/index.js";

// Coaching
export {
  classifyMode,
  accumulateTime,
  computeAlertLevel,
  checkAndEmit,
  analyzeGrammar,
} from "./core/coaching/index.js";

// Status Line
export { render as renderStatusLine, BUILTIN_SEGMENTS } from "./core/status-line/index.js";
export { renderTwoTier, DEFAULT_TWO_TIER_CONFIG } from "./core/status-line/index.js";
export type { TwoTierConfig } from "./core/status-line/index.js";
export { color, strip, RED, GREEN, YELLOW, BLUE, CYAN, DIM, BOLD } from "./core/status-line/index.js";

// Greeting
export { selectQuote } from "./core/greeting.js";

// Sounds
export { playSound, getPlayCommand } from "./core/sounds.js";

// Transcript
export { saveTranscript, enforceMaxSize } from "./core/transcript.js";

// Agents
export { AgentObserver } from "./core/agents.js";

// Cost
export {
  estimateCost,
  checkBudget,
  accumulateCost,
  loadCostState,
  saveCostState,
} from "./core/cost.js";

// Recipes
export type { RecipeConfig } from "./core/recipes.js";
export {
  loadRecipe,
  resolveRecipePath,
  validateRecipe,
  mergeRecipeConfig,
} from "./core/recipes.js";

// Feed Platform — cache bus
export {
  isFresh,
  mergeKey,
  readKey,
  readAll,
} from "./core/feeds/index.js";

// Feed Platform — registry and producers
export {
  createFeedRegistry,
  createCommandProducer,
  createPulseProducer,
  mapElapsedToEmoji,
  createProjectProducer,
  createNewsProducer,
  createCalendarProducer,
  stripHtmlTags,
  createInsightsProducer,
  aggregateInsights,
} from "./core/feeds/index.js";

// Feed Platform — daemon lifecycle
export {
  isRunning,
  startDaemon,
  stopDaemon,
  getDaemonStatus,
} from "./core/feeds/index.js";

// Feed Platform — daemon process internals
export {
  runDaemon,
  daemonLog,
  rotateLog,
  registerBuiltinFeeds,
  registerCustomFeeds,
} from "./core/feeds/index.js";

// Feed Platform — types
export type {
  FeedRegistry,
  PulseData,
  ProjectData,
  NewsStory,
  NewsData,
  CalendarEvent,
  CalendarData,
  DaemonStartResult,
  DaemonStopResult,
  DaemonStatus,
  FeedHealth,
  InsightsData,
} from "./core/feeds/index.js";

// Testing
export { HookRunner } from "./testing/hook-runner.js";
export { HookResult } from "./testing/hook-result.js";
export { GuardTester } from "./testing/guard-tester.js";
