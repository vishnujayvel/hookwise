/**
 * YAML config loading, validation, and handler resolution for hookwise v1.0
 *
 * Implements ConfigEngine:
 * - loadConfig: reads project + global YAML, deep-merges, interpolates env vars
 * - validateConfig: validates top-level sections with path+suggestion errors
 * - resolveHandlers: resolves builtin/script/inline handler types
 * - getHandlersForEvent: filters and orders handlers for event type
 * - saveConfig: serializes back to YAML with camelCase-to-snake_case
 *
 * Config resolution order:
 * 1. ./hookwise.yaml (project-level)
 * 2. ~/.hookwise/config.yaml (global)
 * 3. Deep-merge: project values override global values
 * 4. include: directives resolved and merged after base config
 * 5. Environment variable interpolation applied last
 */

import { existsSync, readFileSync, writeFileSync, renameSync, unlinkSync, statSync } from "node:fs";
import { join, dirname, resolve, isAbsolute } from "node:path";
import { homedir } from "node:os";
import { randomBytes } from "node:crypto";
import yaml from "js-yaml";
import {
  DEFAULT_STATE_DIR,
  DEFAULT_CACHE_PATH,
  DEFAULT_HANDLER_TIMEOUT,
  DEFAULT_STATUS_DELIMITER,
  DEFAULT_TRANSCRIPT_DIR,
  DEFAULT_DAEMON_LOG_PATH,
  PROJECT_CONFIG_FILE,
  GLOBAL_CONFIG_PATH,
} from "./constants.js";
import { ensureDir, getStateDir } from "./state.js";
import { ConfigError, logError, logDebug } from "./errors.js";
import { loadRecipe } from "./recipes.js";
import type {
  HooksConfig,
  ValidationResult,
  ValidationError,
  ResolvedHandler,
  CustomHandlerConfig,
  EventType,
  GuardRuleConfig,
} from "./types.js";
import { EVENT_TYPES, isEventType } from "./types.js";

// --- Snake-case / camelCase mapping ---

/**
 * Convert a snake_case string to camelCase.
 */
export function snakeToCamel(s: string): string {
  return s.replace(/_([a-z])/g, (_, c: string) => c.toUpperCase());
}

/**
 * Convert a camelCase string to snake_case.
 */
export function camelToSnake(s: string): string {
  return s.replace(/[A-Z]/g, (c) => `_${c.toLowerCase()}`);
}

/**
 * Recursively convert all keys in an object from snake_case to camelCase.
 * Arrays are traversed but their items are converted recursively.
 */
export function deepSnakeToCamel(obj: unknown): unknown {
  if (Array.isArray(obj)) {
    return obj.map((item) => deepSnakeToCamel(item));
  }
  if (obj !== null && typeof obj === "object") {
    const result: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(obj as Record<string, unknown>)) {
      result[snakeToCamel(key)] = deepSnakeToCamel(value);
    }
    return result;
  }
  return obj;
}

/**
 * Recursively convert all keys in an object from camelCase to snake_case.
 */
export function deepCamelToSnake(obj: unknown): unknown {
  if (Array.isArray(obj)) {
    return obj.map((item) => deepCamelToSnake(item));
  }
  if (obj !== null && typeof obj === "object") {
    const result: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(obj as Record<string, unknown>)) {
      result[camelToSnake(key)] = deepCamelToSnake(value);
    }
    return result;
  }
  return obj;
}

// --- Environment Variable Interpolation ---

/** Regex for ${ENV_VAR} patterns in string values */
const ENV_VAR_PATTERN = /\$\{([^}]+)\}/g;

/**
 * Interpolate environment variables in string values throughout a config object.
 * Substitutes ${VAR_NAME} with process.env[VAR_NAME].
 * Leaves the literal ${VAR_NAME} if the variable is not defined.
 */
export function interpolateEnvVars(obj: unknown): unknown {
  if (typeof obj === "string") {
    return obj.replace(ENV_VAR_PATTERN, (_match, varName: string) => {
      const value = process.env[varName];
      return value !== undefined ? value : _match;
    });
  }
  if (Array.isArray(obj)) {
    return obj.map((item) => interpolateEnvVars(item));
  }
  if (obj !== null && typeof obj === "object") {
    const result: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(obj as Record<string, unknown>)) {
      result[key] = interpolateEnvVars(value);
    }
    return result;
  }
  return obj;
}

// --- Deep Merge ---

/**
 * Deep merge two objects. Source values override target values.
 * - For nested objects: merge recursively
 * - For arrays: replace entirely (no concatenation)
 * - For primitives: source wins
 */
export function deepMerge(
  target: Record<string, unknown>,
  source: Record<string, unknown>
): Record<string, unknown> {
  const result: Record<string, unknown> = { ...target };

  for (const [key, sourceValue] of Object.entries(source)) {
    const targetValue = result[key];

    if (
      sourceValue !== null &&
      typeof sourceValue === "object" &&
      !Array.isArray(sourceValue) &&
      targetValue !== null &&
      typeof targetValue === "object" &&
      !Array.isArray(targetValue)
    ) {
      // Both are plain objects: merge recursively
      result[key] = deepMerge(
        targetValue as Record<string, unknown>,
        sourceValue as Record<string, unknown>
      );
    } else {
      // Arrays, primitives, null: source replaces target
      result[key] = sourceValue;
    }
  }

  return result;
}

// --- Default Config ---

/**
 * Returns sensible default HooksConfig when no config file exists.
 */
export function getDefaultConfig(): HooksConfig {
  return {
    version: 1,
    guards: [],
    coaching: {
      metacognition: { enabled: false, intervalSeconds: 300 },
      builderTrap: {
        enabled: false,
        thresholds: { yellow: 30, orange: 60, red: 90 },
        toolingPatterns: [],
        practiceTools: [],
      },
      communication: {
        enabled: false,
        frequency: 3,
        minLength: 50,
        rules: [],
        tone: "gentle",
      },
    },
    analytics: { enabled: false },
    greeting: { enabled: false },
    sounds: { enabled: false },
    statusLine: {
      enabled: false,
      segments: [],
      delimiter: DEFAULT_STATUS_DELIMITER,
      cachePath: DEFAULT_CACHE_PATH,
    },
    costTracking: {
      enabled: false,
      rates: {},
      dailyBudget: 10,
      enforcement: "warn",
    },
    transcriptBackup: {
      enabled: false,
      backupDir: DEFAULT_TRANSCRIPT_DIR,
      maxSizeMb: 100,
    },
    handlers: [],
    settings: {
      logLevel: "info",
      handlerTimeoutSeconds: DEFAULT_HANDLER_TIMEOUT,
      stateDir: DEFAULT_STATE_DIR,
    },
    includes: [],
    feeds: {
      pulse: {
        enabled: true,
        intervalSeconds: 30,
        thresholds: { green: 0, yellow: 30, orange: 60, red: 120, skull: 180 },
      },
      project: {
        enabled: true,
        intervalSeconds: 60,
        showBranch: true,
        showLastCommit: true,
      },
      calendar: {
        enabled: false,
        intervalSeconds: 300,
        lookaheadMinutes: 120,
        calendars: ["primary"],
      },
      news: {
        enabled: false,
        source: "hackernews",
        rssUrl: null,
        intervalSeconds: 1800,
        maxStories: 5,
        rotationMinutes: 30,
      },
      insights: {
        enabled: true,
        intervalSeconds: 120,
        stalenessDays: 30,
        usageDataPath: "~/.claude/usage-data",
      },
      custom: [],
    },
    daemon: {
      autoStart: true,
      inactivityTimeoutMinutes: 120,
      logFile: DEFAULT_DAEMON_LOG_PATH,
    },
  };
}

// --- YAML Parsing ---

/**
 * Parse a YAML string into a raw object, throwing ConfigError on failure.
 */
function parseYaml(content: string, filePath: string): Record<string, unknown> {
  try {
    const parsed = yaml.load(content);
    if (parsed === null || parsed === undefined) {
      return {};
    }
    if (typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new ConfigError(`Config file must be a YAML mapping: ${filePath}`);
    }
    return parsed as Record<string, unknown>;
  } catch (error) {
    if (error instanceof ConfigError) throw error;
    throw new ConfigError(
      `Failed to parse YAML in ${filePath}: ${error instanceof Error ? error.message : String(error)}`
    );
  }
}

/**
 * Read and parse a YAML config file. Returns null if the file does not exist.
 * Throws ConfigError on malformed YAML.
 */
function readYamlFile(filePath: string): Record<string, unknown> | null {
  if (!existsSync(filePath)) return null;
  const content = readFileSync(filePath, "utf-8");
  return parseYaml(content, filePath);
}

// --- Include Resolution ---

/**
 * Resolve include directives: load referenced YAML files and merge them.
 * Include paths are resolved relative to the config file directory.
 * Also supports recipe shorthand: "recipes/<name>" resolves to built-in recipe paths.
 */
function resolveIncludes(
  config: Record<string, unknown>,
  baseDir: string
): Record<string, unknown> {
  const includes = config.includes as string[] | undefined;
  if (!includes || !Array.isArray(includes) || includes.length === 0) {
    return config;
  }

  let merged = { ...config };
  for (const includePath of includes) {
    const resolvedPath = isAbsolute(includePath)
      ? includePath
      : resolve(baseDir, includePath);

    try {
      // Check if the resolved path is a directory (recipe)
      if (existsSync(resolvedPath) && statSync(resolvedPath).isDirectory()) {
        const recipe = loadRecipe(resolvedPath);
        if (recipe) {
          // Extract mergeable fields from the recipe config
          const { name: _name, version: _version, description: _desc, events: _events, ...rest } = recipe;
          // Merge recipe config fields as defaults (project overrides recipe)
          if (rest.config && typeof rest.config === "object") {
            merged = deepMerge(merged, rest.config as Record<string, unknown>);
          }
          // Merge guards: prepend recipe guards
          if (Array.isArray(rest.guards) && rest.guards.length > 0) {
            const existingGuards = Array.isArray(merged.guards) ? merged.guards : [];
            merged.guards = [...rest.guards, ...existingGuards];
          }
          // Merge handlers: append recipe handlers
          if (Array.isArray(rest.handlers) && rest.handlers.length > 0) {
            const existingHandlers = Array.isArray(merged.handlers) ? merged.handlers : [];
            merged.handlers = [...existingHandlers, ...rest.handlers];
          }
        }
      } else {
        // File include: load as raw YAML
        const includeRaw = readYamlFile(resolvedPath);
        if (includeRaw) {
          // Don't merge the includes key from included files to avoid cycles
          const { includes: _nested, ...rest } = includeRaw;
          merged = deepMerge(merged, rest);
        }
      }
    } catch (error) {
      logError(
        error instanceof Error ? error : new Error(String(error)),
        { context: "resolveIncludes", path: includePath }
      );
    }
  }

  return merged;
}

// --- v0.1.0 Backward Compatibility ---

/**
 * Transform v0.1.0 Python-era config to v1.0 format.
 * - Converts script handlers referencing .py files to command: "python3 <path>"
 * - Preserves guards and coaching thresholds as-is
 */
function applyV010Compat(raw: Record<string, unknown>): Record<string, unknown> {
  const result = { ...raw };

  // Handle handlers array: convert .py script references
  const handlers = result.handlers as Record<string, unknown>[] | undefined;
  if (Array.isArray(handlers)) {
    result.handlers = handlers.map((handler) => {
      const h = { ...handler };
      if (h.type === "script" && typeof h.command === "string") {
        const cmd = h.command as string;
        if (cmd.endsWith(".py") && !cmd.startsWith("python")) {
          h.command = `python3 ${cmd}`;
        }
      }
      return h;
    });
  }

  return result;
}

// --- Config Loading ---

/**
 * Load hookwise configuration with full resolution pipeline.
 *
 * Resolution order:
 * 1. Read global config (~/.hookwise/config.yaml)
 * 2. Read project config (./hookwise.yaml)
 * 3. Deep-merge: project values override global values
 * 4. Resolve includes
 * 5. Apply v0.1.0 backward compatibility
 * 6. Convert snake_case to camelCase
 * 7. Interpolate environment variables
 * 8. Merge with defaults
 *
 * @param projectDir - Project directory to search for hookwise.yaml. Defaults to cwd.
 * @returns Fully resolved HooksConfig
 */
export function loadConfig(projectDir?: string): HooksConfig {
  const effectiveProjectDir = projectDir ?? process.cwd();
  const projectConfigPath = join(effectiveProjectDir, PROJECT_CONFIG_FILE);
  // Resolve global config path dynamically so HOOKWISE_STATE_DIR env var is respected
  const globalConfigPath = join(getStateDir(), "config.yaml");

  // Step 1: Read raw YAML files
  const globalRaw = readYamlFile(globalConfigPath);
  const projectRaw = readYamlFile(projectConfigPath);

  // If neither exists, return defaults
  if (!globalRaw && !projectRaw) {
    return getDefaultConfig();
  }

  // Step 2: Deep merge global + project (project wins)
  let merged: Record<string, unknown> = {};
  if (globalRaw) {
    merged = { ...globalRaw };
  }
  if (projectRaw) {
    merged = deepMerge(merged, projectRaw);
  }

  // Step 3: Resolve includes (relative to project dir if available, else global dir)
  const includeBaseDir = projectRaw
    ? effectiveProjectDir
    : dirname(globalConfigPath);
  merged = resolveIncludes(merged, includeBaseDir);

  // Step 4: Apply v0.1.0 backward compatibility
  merged = applyV010Compat(merged);

  // Step 5: Convert snake_case to camelCase
  const camelCased = deepSnakeToCamel(merged) as Record<string, unknown>;

  // Step 6: Interpolate environment variables
  const interpolated = interpolateEnvVars(camelCased) as Record<string, unknown>;

  // Step 7: Merge with defaults (defaults fill missing fields)
  const defaults = getDefaultConfig();
  const defaultRecord = defaults as unknown as Record<string, unknown>;
  const final = deepMerge(defaultRecord, interpolated);

  return final as unknown as HooksConfig;
}

// --- Config Validation ---

/** Known top-level config sections */
const KNOWN_SECTIONS = new Set([
  "version",
  "guards",
  "coaching",
  "analytics",
  "greeting",
  "sounds",
  "statusLine",
  "costTracking",
  "transcriptBackup",
  "handlers",
  "settings",
  "includes",
  "feeds",
  "daemon",
]);

/** Known top-level sections in snake_case (for YAML) */
const KNOWN_SECTIONS_SNAKE = new Set([
  "version",
  "guards",
  "coaching",
  "analytics",
  "greeting",
  "sounds",
  "status_line",
  "cost_tracking",
  "transcript_backup",
  "handlers",
  "settings",
  "includes",
  "feeds",
  "daemon",
]);

/**
 * Validate a raw config object (after YAML parsing, before camelCase conversion).
 * Reports errors with JSON path and suggestion.
 */
export function validateConfig(raw: Record<string, unknown>): ValidationResult {
  const errors: ValidationError[] = [];

  // Check for unknown top-level keys
  for (const key of Object.keys(raw)) {
    const camelKey = snakeToCamel(key);
    if (!KNOWN_SECTIONS.has(camelKey) && !KNOWN_SECTIONS_SNAKE.has(key)) {
      errors.push({
        path: key,
        message: `Unknown config section: "${key}"`,
        suggestion: `Known sections: ${[...KNOWN_SECTIONS_SNAKE].join(", ")}`,
      });
    }
  }

  // Validate version
  if (raw.version !== undefined) {
    if (typeof raw.version !== "number" || raw.version < 1) {
      errors.push({
        path: "version",
        message: "version must be a positive number",
        suggestion: "Set version: 1",
      });
    }
  }

  // Validate guards
  if (raw.guards !== undefined) {
    if (!Array.isArray(raw.guards)) {
      errors.push({
        path: "guards",
        message: "guards must be an array",
        suggestion: "Use: guards: [{match: '...', action: 'block', reason: '...'}]",
      });
    } else {
      for (let i = 0; i < raw.guards.length; i++) {
        const guard = raw.guards[i] as Record<string, unknown> | undefined;
        if (!guard || typeof guard !== "object") {
          errors.push({
            path: `guards[${i}]`,
            message: "guard rule must be an object",
          });
          continue;
        }
        if (!guard.match || typeof guard.match !== "string") {
          errors.push({
            path: `guards[${i}].match`,
            message: "guard rule must have a 'match' string",
            suggestion: "Add match: 'tool_name:Bash' or similar glob pattern",
          });
        }
        if (!guard.action || !["block", "warn", "confirm"].includes(guard.action as string)) {
          errors.push({
            path: `guards[${i}].action`,
            message: "guard rule action must be 'block', 'warn', or 'confirm'",
            suggestion: "Set action: 'block' | 'warn' | 'confirm'",
          });
        }
        if (!guard.reason || typeof guard.reason !== "string") {
          errors.push({
            path: `guards[${i}].reason`,
            message: "guard rule must have a 'reason' string",
          });
        }
      }
    }
  }

  // Validate handlers
  if (raw.handlers !== undefined) {
    if (!Array.isArray(raw.handlers)) {
      errors.push({
        path: "handlers",
        message: "handlers must be an array",
        suggestion: "Use: handlers: [{name: '...', type: 'builtin', events: ['PreToolUse']}]",
      });
    } else {
      for (let i = 0; i < raw.handlers.length; i++) {
        const handler = raw.handlers[i] as Record<string, unknown> | undefined;
        if (!handler || typeof handler !== "object") {
          errors.push({
            path: `handlers[${i}]`,
            message: "handler must be an object",
          });
          continue;
        }
        if (!handler.name || typeof handler.name !== "string") {
          errors.push({
            path: `handlers[${i}].name`,
            message: "handler must have a 'name' string",
          });
        }
        if (!handler.type || !["builtin", "script", "inline"].includes(handler.type as string)) {
          errors.push({
            path: `handlers[${i}].type`,
            message: "handler type must be 'builtin', 'script', or 'inline'",
            suggestion: "Set type: 'builtin' | 'script' | 'inline'",
          });
        }
        if (handler.events === undefined) {
          errors.push({
            path: `handlers[${i}].events`,
            message: "handler must specify events",
            suggestion: "Set events: ['PreToolUse'] or events: '*'",
          });
        }
      }
    }
  }

  // Validate coaching
  if (raw.coaching !== undefined && typeof raw.coaching === "object" && raw.coaching !== null) {
    const coaching = raw.coaching as Record<string, unknown>;
    const metacog = coaching.metacognition as Record<string, unknown> | undefined;
    if (metacog && typeof metacog === "object") {
      const interval = (metacog.interval_seconds ?? metacog.intervalSeconds) as number | undefined;
      if (interval !== undefined && (typeof interval !== "number" || interval < 0)) {
        errors.push({
          path: "coaching.metacognition.interval_seconds",
          message: "interval_seconds must be a non-negative number",
          suggestion: "Set interval_seconds: 300 (5 minutes)",
        });
      }
    }
  }

  // Validate settings
  if (raw.settings !== undefined && typeof raw.settings === "object" && raw.settings !== null) {
    const settings = raw.settings as Record<string, unknown>;
    const logLevel = (settings.log_level ?? settings.logLevel) as string | undefined;
    if (logLevel !== undefined && !["debug", "info", "warn", "error"].includes(logLevel)) {
      errors.push({
        path: "settings.log_level",
        message: `Invalid log level: "${logLevel}"`,
        suggestion: "Use one of: debug, info, warn, error",
      });
    }
  }

  // Validate includes
  if (raw.includes !== undefined) {
    if (!Array.isArray(raw.includes)) {
      errors.push({
        path: "includes",
        message: "includes must be an array of file paths",
        suggestion: "Use: includes: ['./recipes/safety.yaml']",
      });
    }
  }

  // Validate feeds
  if (raw.feeds !== undefined && typeof raw.feeds === "object" && raw.feeds !== null) {
    const feeds = raw.feeds as Record<string, unknown>;

    // Validate feeds.pulse
    if (feeds.pulse !== undefined && typeof feeds.pulse === "object" && feeds.pulse !== null) {
      const pulse = feeds.pulse as Record<string, unknown>;
      const interval = (pulse.interval_seconds ?? pulse.intervalSeconds) as number | undefined;
      if (interval !== undefined && (typeof interval !== "number" || interval <= 0)) {
        errors.push({
          path: "feeds.pulse.interval_seconds",
          message: "interval_seconds must be a positive number",
          suggestion: "Set interval_seconds: 30",
        });
      }
      // Validate thresholds are ascending
      if (pulse.thresholds !== undefined && typeof pulse.thresholds === "object" && pulse.thresholds !== null) {
        const t = pulse.thresholds as Record<string, unknown>;
        const green = t.green as number | undefined;
        const yellow = t.yellow as number | undefined;
        const orange = t.orange as number | undefined;
        const red = t.red as number | undefined;
        const skull = t.skull as number | undefined;
        if (
          green !== undefined && yellow !== undefined && orange !== undefined &&
          red !== undefined && skull !== undefined
        ) {
          if (!(green < yellow && yellow < orange && orange < red && red < skull)) {
            errors.push({
              path: "feeds.pulse.thresholds",
              message: "Pulse thresholds must be ascending: green < yellow < orange < red < skull",
              suggestion: "Example: green: 0, yellow: 30, orange: 60, red: 120, skull: 180",
            });
          }
        }
      }
    }

    // Validate feeds.project
    if (feeds.project !== undefined && typeof feeds.project === "object" && feeds.project !== null) {
      const project = feeds.project as Record<string, unknown>;
      const interval = (project.interval_seconds ?? project.intervalSeconds) as number | undefined;
      if (interval !== undefined && (typeof interval !== "number" || interval <= 0)) {
        errors.push({
          path: "feeds.project.interval_seconds",
          message: "interval_seconds must be a positive number",
          suggestion: "Set interval_seconds: 60",
        });
      }
    }

    // Validate feeds.calendar
    if (feeds.calendar !== undefined && typeof feeds.calendar === "object" && feeds.calendar !== null) {
      const calendar = feeds.calendar as Record<string, unknown>;
      const interval = (calendar.interval_seconds ?? calendar.intervalSeconds) as number | undefined;
      if (interval !== undefined && (typeof interval !== "number" || interval <= 0)) {
        errors.push({
          path: "feeds.calendar.interval_seconds",
          message: "interval_seconds must be a positive number",
          suggestion: "Set interval_seconds: 300",
        });
      }
    }

    // Validate feeds.news
    if (feeds.news !== undefined && typeof feeds.news === "object" && feeds.news !== null) {
      const news = feeds.news as Record<string, unknown>;
      const interval = (news.interval_seconds ?? news.intervalSeconds) as number | undefined;
      if (interval !== undefined && (typeof interval !== "number" || interval <= 0)) {
        errors.push({
          path: "feeds.news.interval_seconds",
          message: "interval_seconds must be a positive number",
          suggestion: "Set interval_seconds: 1800",
        });
      }
      if (news.source !== undefined && news.source !== "hackernews" && news.source !== "rss") {
        errors.push({
          path: "feeds.news.source",
          message: `Invalid news source: "${news.source}"`,
          suggestion: "Use one of: hackernews, rss",
        });
      }
      // Cross-field: source "rss" requires rss_url
      if (news.source === "rss") {
        const rssUrl = (news.rss_url ?? news.rssUrl) as string | null | undefined;
        if (!rssUrl || typeof rssUrl !== "string") {
          errors.push({
            path: "feeds.news.rss_url",
            message: "rss_url is required when source is 'rss'",
            suggestion: "Set rss_url: 'https://example.com/feed.xml'",
          });
        }
      }
    }

    // Validate feeds.insights
    if (feeds.insights !== undefined && typeof feeds.insights === "object" && feeds.insights !== null) {
      const insights = feeds.insights as Record<string, unknown>;
      const interval = (insights.interval_seconds ?? insights.intervalSeconds) as number | undefined;
      if (interval !== undefined && (typeof interval !== "number" || interval <= 0)) {
        errors.push({
          path: "feeds.insights.interval_seconds",
          message: "interval_seconds must be a positive number",
          suggestion: "Set interval_seconds: 120",
        });
      }
      const stalenessDays = (insights.staleness_days ?? insights.stalenessDays) as number | undefined;
      if (stalenessDays !== undefined && (typeof stalenessDays !== "number" || stalenessDays <= 0)) {
        errors.push({
          path: "feeds.insights.staleness_days",
          message: "staleness_days must be a positive number",
          suggestion: "Set staleness_days: 30",
        });
      }
    }

    // Validate feeds.custom
    if (feeds.custom !== undefined) {
      if (!Array.isArray(feeds.custom)) {
        errors.push({
          path: "feeds.custom",
          message: "feeds.custom must be an array",
          suggestion: "Use: custom: [{name: '...', command: '...', interval_seconds: 60}]",
        });
      } else {
        for (let i = 0; i < feeds.custom.length; i++) {
          const entry = feeds.custom[i] as Record<string, unknown> | undefined;
          if (!entry || typeof entry !== "object") {
            errors.push({
              path: `feeds.custom[${i}]`,
              message: "custom feed entry must be an object",
            });
            continue;
          }
          if (!entry.name || typeof entry.name !== "string") {
            errors.push({
              path: `feeds.custom[${i}].name`,
              message: "custom feed must have a 'name' string",
            });
          }
          if (!entry.command || typeof entry.command !== "string") {
            errors.push({
              path: `feeds.custom[${i}].command`,
              message: "custom feed must have a 'command' string",
            });
          }
          const customInterval = (entry.interval_seconds ?? entry.intervalSeconds) as number | undefined;
          if (customInterval !== undefined && (typeof customInterval !== "number" || customInterval <= 0)) {
            errors.push({
              path: `feeds.custom[${i}].interval_seconds`,
              message: "interval_seconds must be a positive number",
            });
          }
        }
      }
    }
  }

  // Validate daemon
  if (raw.daemon !== undefined && typeof raw.daemon === "object" && raw.daemon !== null) {
    const daemon = raw.daemon as Record<string, unknown>;
    const timeout = (daemon.inactivity_timeout_minutes ?? daemon.inactivityTimeoutMinutes) as number | undefined;
    if (timeout !== undefined && (typeof timeout !== "number" || timeout <= 0)) {
      errors.push({
        path: "daemon.inactivity_timeout_minutes",
        message: "inactivity_timeout_minutes must be a positive number",
        suggestion: "Set inactivity_timeout_minutes: 120",
      });
    }
  }

  return {
    valid: errors.length === 0,
    errors,
  };
}

// --- Handler Resolution ---

/**
 * Infer the execution phase for a handler based on its type and event list.
 */
function inferPhase(handler: CustomHandlerConfig): "guard" | "context" | "side_effect" {
  if (handler.phase) return handler.phase;

  // Handlers listening to PreToolUse or UserPromptSubmit with guard-like names
  const guardEvents: EventType[] = ["PreToolUse", "UserPromptSubmit", "PermissionRequest"];
  const events = handler.events === "*" ? [] : handler.events;
  const isGuardEvent = events.some((e) => guardEvents.includes(e));

  if (isGuardEvent && handler.type !== "inline") {
    return "guard";
  }

  // Context injection events
  const contextEvents: EventType[] = ["SessionStart", "SubagentStart"];
  const isContextEvent = events.some((e) => contextEvents.includes(e));
  if (isContextEvent) {
    return "context";
  }

  return "side_effect";
}

/**
 * Resolve all handlers from a HooksConfig into ResolvedHandler instances.
 * Combines explicit handlers[] with implicit handlers from guards/coaching/etc.
 */
export function resolveHandlers(config: HooksConfig): ResolvedHandler[] {
  const resolved: ResolvedHandler[] = [];
  const defaultTimeout = config.settings.handlerTimeoutSeconds * 1000;

  // Resolve explicit custom handlers
  for (const handler of config.handlers) {
    const events = handler.events === "*"
      ? new Set(EVENT_TYPES as unknown as EventType[])
      : new Set(handler.events);

    resolved.push({
      name: handler.name,
      handlerType: handler.type,
      events,
      module: handler.module,
      command: handler.command,
      action: handler.action,
      timeout: handler.timeout ? handler.timeout * 1000 : defaultTimeout,
      phase: inferPhase(handler),
      configRaw: handler as unknown as Record<string, unknown>,
    });
  }

  return resolved;
}

/**
 * Get handlers for a specific event type, ordered by phase:
 * guards first, then context, then side effects.
 */
export function getHandlersForEvent(
  config: HooksConfig,
  eventType: EventType
): ResolvedHandler[] {
  const all = resolveHandlers(config);

  const matching = all.filter((h) => h.events.has(eventType));

  // Sort by phase: guard < context < side_effect
  const phaseOrder = { guard: 0, context: 1, side_effect: 2 };
  matching.sort((a, b) => phaseOrder[a.phase] - phaseOrder[b.phase]);

  return matching;
}

// --- Config Write-back ---

/**
 * Serialize a HooksConfig back to YAML with camelCase-to-snake_case keys.
 * Uses atomic write (temp file + rename) for safety.
 *
 * Note: YAML comments are not preserved (round-trip limitation).
 */
export function saveConfig(config: HooksConfig, filePath: string): void {
  // Convert camelCase to snake_case
  const snakeCased = deepCamelToSnake(config);

  // Serialize to YAML
  const yamlContent = yaml.dump(snakeCased, {
    indent: 2,
    lineWidth: 120,
    noRefs: true,
    sortKeys: false,
  });

  // Atomic write: temp file + rename
  const dir = dirname(filePath);
  ensureDir(dir);

  const suffix = randomBytes(6).toString("hex");
  const tempPath = join(dir, `.tmp-config-${suffix}`);

  try {
    writeFileSync(tempPath, yamlContent, "utf-8");
    renameSync(tempPath, filePath);
  } catch (error) {
    try {
      if (existsSync(tempPath)) {
        unlinkSync(tempPath);
      }
    } catch {
      // Ignore cleanup errors
    }
    throw error;
  }
}
