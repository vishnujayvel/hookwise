/**
 * Core TypeScript types for hookwise v1.0
 *
 * All interfaces from the design doc. YAML uses snake_case,
 * TypeScript uses camelCase, with mapping in ConfigEngine.
 */

// All 13 event types supported by Claude Code hooks
export const EVENT_TYPES = [
  "UserPromptSubmit",
  "PreToolUse",
  "PostToolUse",
  "PostToolUseFailure",
  "Notification",
  "Stop",
  "SubagentStart",
  "SubagentStop",
  "PreCompact",
  "SessionStart",
  "SessionEnd",
  "PermissionRequest",
  "Setup",
] as const;

export type EventType = (typeof EVENT_TYPES)[number];

/**
 * Type guard: returns true if the value is a valid EventType.
 */
export function isEventType(value: unknown): value is EventType {
  return typeof value === "string" && EVENT_TYPES.includes(value as EventType);
}

// --- Hook Payload ---

export interface HookPayload {
  session_id: string;
  tool_name?: string;
  tool_input?: Record<string, unknown>;
  [key: string]: unknown;
}

/**
 * Type guard: returns true if the value has the required HookPayload shape.
 */
export function isHookPayload(value: unknown): value is HookPayload {
  if (typeof value !== "object" || value === null) return false;
  const obj = value as Record<string, unknown>;
  return typeof obj.session_id === "string";
}

// --- Dispatch Result ---

export interface DispatchResult {
  stdout: string | null;
  stderr: string | null;
  exitCode: 0 | 2;
}

// --- Handler Result ---

export interface HandlerResult {
  decision: "block" | "warn" | "confirm" | null;
  reason: string | null;
  additionalContext: string | null;
  output: Record<string, unknown> | null;
}

// --- Config Interfaces ---

export interface HooksConfig {
  version: number;
  guards: GuardRuleConfig[];
  coaching: CoachingConfig;
  analytics: AnalyticsConfig;
  greeting: GreetingConfig;
  sounds: SoundsConfig;
  statusLine: StatusLineConfig;
  costTracking: CostTrackingConfig;
  transcriptBackup: TranscriptConfig;
  handlers: CustomHandlerConfig[];
  settings: SettingsConfig;
  includes: string[];
  feeds: FeedsConfig;
  daemon: DaemonConfig;
}

export interface CoachingConfig {
  metacognition: {
    enabled: boolean;
    intervalSeconds: number;
    promptsFile?: string;
  };
  builderTrap: {
    enabled: boolean;
    thresholds: { yellow: number; orange: number; red: number };
    toolingPatterns: string[];
    practiceTools: string[];
  };
  communication: {
    enabled: boolean;
    frequency: number;
    minLength: number;
    rules: string[];
    tone: "gentle" | "direct" | "silent";
  };
}

export interface AnalyticsConfig {
  enabled: boolean;
  dbPath?: string;
}

export interface GreetingConfig {
  enabled: boolean;
  quotesFile?: string;
  categories?: Record<string, { weight: number; quotes: QuoteEntry[] }>;
}

export interface QuoteEntry {
  text: string;
  author?: string;
}

export interface SoundsConfig {
  enabled: boolean;
  notification?: string;
  completion?: string;
}

export interface StatusLineConfig {
  enabled: boolean;
  segments: SegmentConfig[];
  delimiter: string;
  cachePath: string;
}

export interface CostTrackingConfig {
  enabled: boolean;
  rates: Record<string, number>;
  dailyBudget: number;
  enforcement: "warn" | "enforce";
}

export interface TranscriptConfig {
  enabled: boolean;
  backupDir: string;
  maxSizeMb: number;
}

export interface SettingsConfig {
  logLevel: "debug" | "info" | "warn" | "error";
  handlerTimeoutSeconds: number;
  stateDir: string;
}

// --- Validation ---

export interface ValidationResult {
  valid: boolean;
  errors: ValidationError[];
}

export interface ValidationError {
  path: string;
  message: string;
  suggestion?: string;
}

// --- Handler Resolution ---

export interface ResolvedHandler {
  name: string;
  handlerType: "builtin" | "script" | "inline";
  events: Set<EventType>;
  module?: string;
  command?: string;
  action?: Record<string, unknown>;
  timeout: number;
  phase: "guard" | "context" | "side_effect";
  configRaw: Record<string, unknown>;
}

export interface CustomHandlerConfig {
  name: string;
  type: "builtin" | "script" | "inline";
  events: EventType[] | "*";
  phase?: "guard" | "context" | "side_effect";
  timeout?: number;
  module?: string;
  command?: string;
  action?: Record<string, unknown>;
}

// --- Guard Types ---

export interface GuardRule {
  match: string;
  action: "block" | "warn" | "confirm";
  reason: string;
  when?: string;
  unless?: string;
}

export interface GuardResult {
  action: "allow" | "block" | "warn" | "confirm";
  reason?: string;
  matchedRule?: GuardRule;
}

export type GuardRuleConfig = {
  match: string;
  action: "block" | "warn" | "confirm";
  reason: string;
  when?: string;
  unless?: string;
};

// --- Condition Parser ---

export interface ParsedCondition {
  fieldPath: string;
  operator:
    | "contains"
    | "starts_with"
    | "ends_with"
    | "=="
    | "equals"
    | "matches";
  value: string;
}

// --- Segment Config ---

export interface SegmentConfig {
  builtin?: string;
  custom?: {
    command: string;
    label?: string;
    timeout?: number;
  };
  [key: string]: unknown;
}

// --- Analytics Types ---

/**
 * Classification label for AI confidence scoring.
 */
export type AIClassification =
  | "high_probability_ai"
  | "likely_ai"
  | "mixed_verified"
  | "human_authored";

/**
 * AI Confidence Score with classification.
 */
export interface AIConfidenceScore {
  score: number;
  classification: AIClassification;
}

/**
 * Event recorded in the analytics database.
 */
export interface AnalyticsEvent {
  sessionId: string;
  eventType: string;
  toolName?: string;
  timestamp: string;
  filePath?: string;
  linesAdded?: number;
  linesRemoved?: number;
  aiConfidenceScore?: number;
}

/**
 * Summary stats for ending a session.
 */
export interface SessionSummary {
  totalToolCalls: number;
  fileEditsCount: number;
  aiAuthoredLines: number;
  humanVerifiedLines: number;
  classification?: string;
  estimatedCostUsd?: number;
}

/**
 * Daily summary aggregate returned by stats queries.
 */
export interface DailySummary {
  date: string;
  totalEvents: number;
  totalToolCalls: number;
  linesAdded: number;
  linesRemoved: number;
  sessions: number;
}

/**
 * Tool breakdown entry returned by stats queries.
 */
export interface ToolBreakdownEntry {
  toolName: string;
  count: number;
  linesAdded: number;
  linesRemoved: number;
}

/**
 * Authorship summary returned by stats queries.
 */
export interface AuthorshipSummary {
  totalEntries: number;
  totalLinesChanged: number;
  weightedAIScore: number;
  classificationBreakdown: Record<AIClassification, number>;
}

/**
 * Options for stats queries.
 */
export interface StatsOptions {
  sessionId?: string;
  days?: number;
  from?: string;
  to?: string;
}

/**
 * Combined stats result.
 */
export interface StatsResult {
  daily: DailySummary[];
  toolBreakdown: ToolBreakdownEntry[];
  authorship: AuthorshipSummary;
}

/**
 * Authorship ledger entry stored in the database.
 */
export interface AuthorshipEntry {
  sessionId: string;
  filePath: string;
  toolName: string;
  linesChanged: number;
  aiScore: number;
  classification: AIClassification;
  timestamp: string;
}

// --- Coaching Types ---

/**
 * Builder's Trap mode classification for tool calls.
 */
export type Mode = "coding" | "tooling" | "practice" | "prep" | "neutral";

/**
 * Alert level for Builder's Trap detector.
 */
export type AlertLevel = "none" | "yellow" | "orange" | "red";

/**
 * Record of a large AI-generated change that was accepted quickly.
 */
export interface LargeChangeRecord {
  timestamp: string;
  toolName: string;
  linesChanged: number;
  acceptedWithinSeconds: number;
}

/**
 * Mutable coaching cache stored as JSON on disk.
 * Shared across coaching subsystems.
 */
export interface CoachingCache {
  lastPromptAt: string;
  promptHistory: string[];
  currentMode: Mode;
  modeStartedAt: string;
  toolingMinutes: number;
  alertLevel: AlertLevel;
  todayDate: string;
  practiceCount: number;
  lastLargeChange: LargeChangeRecord | null;
}

/**
 * Result from the metacognition reminder engine.
 */
export interface MetacognitionResult {
  shouldEmit: boolean;
  promptText?: string;
  promptId?: string;
  category?: string;
  triggerType?: "interval" | "rapid_acceptance" | "mode_change" | "builder_trap";
}

/**
 * Result from the communication coach grammar analysis.
 */
export interface GrammarResult {
  shouldCorrect: boolean;
  issues: GrammarIssue[];
  correctedText?: string;
  improvementScore?: number;
}

/**
 * Individual grammar issue found by the communication coach.
 */
export interface GrammarIssue {
  rule: string;
  original: string;
  suggestion: string;
  position: number;
}

// --- Cost Types ---

/**
 * Estimated cost for a tool invocation.
 */
export interface CostEstimate {
  estimatedTokens: number;
  estimatedCostUsd: number;
  model: string;
}

/**
 * Budget status: ok if under budget, not-ok with enforcement mode if over.
 */
export type BudgetStatus =
  | { ok: true }
  | { ok: false; message: string; enforcement: "warn" | "enforce" };

/**
 * Mutable cost state persisted to disk.
 */
export interface CostState {
  dailyCosts: Record<string, number>;
  sessionCosts: Record<string, number>;
  today: string;
  totalToday: number;
}

// --- Agent Types ---

/**
 * File conflict detected between overlapping agent spans.
 */
export interface FileConflict {
  filePath: string;
  agents: string[];
  overlapPeriod: { start: string; end: string };
}

// --- Testing Types ---

/**
 * Test scenario for batch guard testing.
 */
export interface TestScenario {
  toolName: string;
  toolInput?: Record<string, unknown>;
  expected: "block" | "allow" | "warn" | "confirm";
}

/**
 * Result of a single scenario in batch guard testing.
 */
export interface ScenarioResult {
  scenario: TestScenario;
  guardResult: GuardResult;
  passed: boolean;
}

// --- Feed Platform Types (v1.1) ---

export interface CacheEntry {
  updated_at: string;  // ISO 8601
  ttl_seconds: number;
  [key: string]: unknown;
}

/**
 * A feed producer is an async function that returns feed data as a
 * key-value object, or null if the feed could not be produced
 * (e.g. command failure, timeout, or disabled source).
 */
export type FeedProducer = () => Promise<Record<string, unknown> | null>;

/**
 * A feed definition registered in the FeedRegistry.
 * Each feed has a unique name, a polling interval, a producer function,
 * and an enabled flag.
 */
export interface FeedDefinition {
  name: string;
  intervalSeconds: number;
  producer: FeedProducer;
  enabled: boolean;
}

export interface PulseFeedConfig {
  enabled: boolean;
  intervalSeconds: number;
  thresholds: {
    green: number;
    yellow: number;
    orange: number;
    red: number;
    skull: number;
  };
}

export interface ProjectFeedConfig {
  enabled: boolean;
  intervalSeconds: number;
  showBranch: boolean;
  showLastCommit: boolean;
}

export interface CalendarFeedConfig {
  enabled: boolean;
  intervalSeconds: number;
  lookaheadMinutes: number;
  calendars: string[];
}

export interface NewsFeedConfig {
  enabled: boolean;
  source: "hackernews" | "rss";
  rssUrl: string | null;
  intervalSeconds: number;
  maxStories: number;
  rotationMinutes: number;
}

export interface CustomFeedConfig {
  name: string;
  command: string;
  intervalSeconds: number;
  enabled: boolean;
  timeoutSeconds: number;
}

export interface InsightsFeedConfig {
  enabled: boolean;
  intervalSeconds: number;
  stalenessDays: number;
  usageDataPath: string;
}

export interface FeedsConfig {
  pulse: PulseFeedConfig;
  project: ProjectFeedConfig;
  calendar: CalendarFeedConfig;
  news: NewsFeedConfig;
  insights: InsightsFeedConfig;
  custom: CustomFeedConfig[];
}

export interface DaemonConfig {
  autoStart: boolean;
  inactivityTimeoutMinutes: number;
  logFile: string;
}
