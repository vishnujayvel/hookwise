/**
 * Daemon child process: main loop for the feed platform background worker.
 *
 * This is the entry point spawned by the daemon manager as a detached process.
 * It:
 *   1. Loads config from a provided path
 *   2. Registers built-in and custom feeds in a FeedRegistry
 *   3. Writes its PID to the PID file
 *   4. Starts staggered polling intervals for each enabled feed
 *   5. Monitors inactivity via heartbeat checks
 *   6. Handles SIGTERM/SIGINT for graceful shutdown
 *   7. Performs log rotation on startup
 *
 * Requirements: FR-2.3, FR-2.4, FR-2.5, FR-2.7, FR-2.8, FR-2.9, NFR-1
 */

import {
  existsSync,
  readFileSync,
  writeFileSync,
  appendFileSync,
  unlinkSync,
} from "node:fs";
import { dirname } from "node:path";
import {
  DEFAULT_PID_PATH,
  DEFAULT_CACHE_PATH,
  DEFAULT_DAEMON_LOG_PATH,
  DEFAULT_CALENDAR_CREDENTIALS_PATH,
} from "../constants.js";
import { ensureDir } from "../state.js";
import { mergeKey, readAll } from "./cache-bus.js";
import { createFeedRegistry, createCommandProducer } from "./registry.js";
import type { FeedRegistry } from "./registry.js";
import { createPulseProducer } from "./producers/pulse.js";
import { createProjectProducer } from "./producers/project.js";
import { createNewsProducer } from "./producers/news.js";
import { createCalendarProducer } from "./producers/calendar.js";
import { createInsightsProducer } from "./producers/insights.js";
import { createPracticeProducer } from "./producers/practice.js";
import { createWeatherProducer } from "./producers/weather.js";
import { createMemoriesProducer } from "./producers/memories.js";
import { loadConfig } from "../config.js";
import type { HooksConfig, FeedDefinition } from "../types.js";

/** Stagger offset between feed intervals: 2 seconds per feed index. */
const STAGGER_OFFSET_MS = 2000;

/** Inactivity check interval: every 60 seconds. */
const INACTIVITY_CHECK_INTERVAL_MS = 60_000;

/** Max log lines before rotation. */
const MAX_LOG_LINES = 1000;

/** Lines to keep after rotation. */
const KEEP_LOG_LINES = 500;

/** Heartbeat TTL: effectively infinite (used as a sentinel, not for freshness). */
const HEARTBEAT_TTL = 999999;

// --- Logging ---

/**
 * Append a timestamped log line to the daemon log file.
 */
export function daemonLog(message: string, logFile: string = DEFAULT_DAEMON_LOG_PATH): void {
  try {
    ensureDir(dirname(logFile));
    const line = `[${new Date().toISOString()}] ${message}\n`;
    appendFileSync(logFile, line, "utf-8");
  } catch {
    // Logging must never crash the daemon
  }
}

/**
 * Rotate the log file if it exceeds MAX_LOG_LINES.
 * Truncates to the last KEEP_LOG_LINES lines.
 */
export function rotateLog(logFile: string = DEFAULT_DAEMON_LOG_PATH): void {
  try {
    if (!existsSync(logFile)) return;

    const content = readFileSync(logFile, "utf-8");
    const lines = content.split("\n");

    // lines array includes a trailing empty string from the final \n
    // so the actual line count is lines.length - 1 (if last element is empty)
    const actualLines = lines[lines.length - 1] === "" ? lines.length - 1 : lines.length;

    if (actualLines > MAX_LOG_LINES) {
      const kept = lines.slice(actualLines - KEEP_LOG_LINES);
      writeFileSync(logFile, kept.join("\n"), "utf-8");
    }
  } catch {
    // Log rotation failure is non-fatal
  }
}

// --- Feed Registration ---

/**
 * Register the built-in feeds (pulse, project, calendar, news, insights, practice, weather, memories)
 * based on the config.
 */
export function registerBuiltinFeeds(
  registry: FeedRegistry,
  config: HooksConfig,
  cachePath: string = DEFAULT_CACHE_PATH,
): void {
  // Pulse feed
  registry.register({
    name: "pulse",
    intervalSeconds: config.feeds.pulse.intervalSeconds,
    producer: createPulseProducer(cachePath, config.feeds.pulse),
    enabled: config.feeds.pulse.enabled,
  });

  // Project feed
  registry.register({
    name: "project",
    intervalSeconds: config.feeds.project.intervalSeconds,
    producer: createProjectProducer(cachePath),
    enabled: config.feeds.project.enabled,
  });

  // Calendar feed
  registry.register({
    name: "calendar",
    intervalSeconds: config.feeds.calendar.intervalSeconds,
    producer: createCalendarProducer(DEFAULT_CALENDAR_CREDENTIALS_PATH, config.feeds.calendar),
    enabled: config.feeds.calendar.enabled,
  });

  // News feed
  registry.register({
    name: "news",
    intervalSeconds: config.feeds.news.intervalSeconds,
    producer: createNewsProducer(config.feeds.news, cachePath),
    enabled: config.feeds.news.enabled,
  });

  // Insights feed
  registry.register({
    name: "insights",
    intervalSeconds: config.feeds.insights.intervalSeconds,
    producer: createInsightsProducer(config.feeds.insights),
    enabled: config.feeds.insights.enabled,
  });

  // Practice feed
  registry.register({
    name: "practice",
    intervalSeconds: config.feeds.practice.intervalSeconds,
    producer: createPracticeProducer(config.feeds.practice),
    enabled: config.feeds.practice.enabled,
  });

  // Weather feed
  registry.register({
    name: "weather",
    intervalSeconds: config.feeds.weather.intervalSeconds,
    producer: createWeatherProducer(config.feeds.weather),
    enabled: config.feeds.weather.enabled,
  });

  // Memories feed
  registry.register({
    name: "memories",
    intervalSeconds: config.feeds.memories.intervalSeconds,
    producer: createMemoriesProducer(config.feeds.memories),
    enabled: config.feeds.memories.enabled,
  });
}

/**
 * Register custom feeds from the config's feeds.custom array.
 */
export function registerCustomFeeds(
  registry: FeedRegistry,
  config: HooksConfig,
): void {
  for (const custom of config.feeds.custom) {
    if (!custom.name || !custom.command) continue;

    registry.register({
      name: custom.name,
      intervalSeconds: custom.intervalSeconds,
      producer: createCommandProducer(custom.command, (custom.timeoutSeconds ?? 10) * 1000),
      enabled: custom.enabled,
    });
  }
}

// --- Main Daemon Loop ---

/**
 * Run a single feed poll: invoke the producer and write the result to cache.
 * Catches and logs errors per-feed so one failure cannot crash others.
 */
async function pollFeed(
  feed: FeedDefinition,
  cachePath: string,
  logFile: string,
): Promise<void> {
  try {
    const result = await feed.producer();
    if (result !== null) {
      mergeKey(cachePath, feed.name, result, feed.intervalSeconds);
    }
  } catch (error) {
    daemonLog(
      `Feed "${feed.name}" error: ${error instanceof Error ? error.message : String(error)}`,
      logFile,
    );
  }
}

/**
 * Run the daemon main loop.
 *
 * This is the primary entry point for the detached child process:
 *   - Load config
 *   - Register feeds
 *   - Write PID file
 *   - Start staggered intervals
 *   - Monitor inactivity
 *   - Handle signals
 *
 * @param projectDir - Project directory containing hookwise.yaml
 */
export async function runDaemon(projectDir: string): Promise<void> {
  const pidPath = process.env.HOOKWISE_PID_PATH ?? DEFAULT_PID_PATH;
  const daemonStartTime = Date.now();

  // Step 1: Load config
  const config = loadConfig(projectDir);
  const cachePath = config.statusLine.cachePath ?? DEFAULT_CACHE_PATH;
  const logFile = config.daemon.logFile ?? DEFAULT_DAEMON_LOG_PATH;

  // Step 7: Log rotation on startup
  rotateLog(logFile);

  daemonLog("Daemon starting", logFile);

  // Step 2: Register feeds
  const registry = createFeedRegistry();
  registerBuiltinFeeds(registry, config, cachePath);
  registerCustomFeeds(registry, config);

  // Step 3: Write PID file
  ensureDir(dirname(pidPath));
  writeFileSync(pidPath, String(process.pid), "utf-8");
  daemonLog(`PID ${process.pid} written to ${pidPath}`, logFile);

  // Step 5 (partial): Write initial heartbeat
  mergeKey(cachePath, "_heartbeat", { value: Date.now() }, HEARTBEAT_TTL);

  // Step 4: Start staggered intervals for enabled feeds
  const enabledFeeds = registry.getEnabled();
  const intervalTimers: NodeJS.Timeout[] = [];

  for (let i = 0; i < enabledFeeds.length; i++) {
    const feed = enabledFeeds[i];
    const intervalMs = feed.intervalSeconds * 1000;
    const staggerMs = i * STAGGER_OFFSET_MS;

    // Initial poll after stagger delay
    const initialTimeout = setTimeout(() => {
      pollFeed(feed, cachePath, logFile);

      // Then start the repeating interval
      const timer = setInterval(() => {
        pollFeed(feed, cachePath, logFile);
      }, intervalMs);

      intervalTimers.push(timer);
    }, staggerMs);

    // Track the timeout as well for cleanup
    intervalTimers.push(initialTimeout);
  }

  daemonLog(`Started ${enabledFeeds.length} feed(s) with staggered intervals`, logFile);

  // Step 5: Inactivity monitoring
  const inactivityTimer = setInterval(() => {
    try {
      const parsed = readAll(cachePath);
      const heartbeat = (parsed?._heartbeat as Record<string, unknown>)?.value as number | undefined;

      const referenceTime = heartbeat ?? daemonStartTime;
      const timeoutMs = config.daemon.inactivityTimeoutMinutes * 60 * 1000;

      if (Date.now() - referenceTime > timeoutMs) {
        daemonLog("Inactivity timeout reached — shutting down", logFile);
        cleanup();
        process.exit(0);
      }
    } catch {
      // Cache missing or corrupt — use daemon start time as fallback
      const timeoutMs = config.daemon.inactivityTimeoutMinutes * 60 * 1000;
      if (Date.now() - daemonStartTime > timeoutMs) {
        daemonLog("Inactivity timeout (fallback) — shutting down", logFile);
        cleanup();
        process.exit(0);
      }
    }
  }, INACTIVITY_CHECK_INTERVAL_MS);

  intervalTimers.push(inactivityTimer);

  // Step 6: Signal handling
  function cleanup(): void {
    daemonLog("Cleaning up...", logFile);

    // Clear all intervals and timeouts
    for (const timer of intervalTimers) {
      clearInterval(timer);
      clearTimeout(timer);
    }
    intervalTimers.length = 0;

    // Remove PID file
    try {
      if (existsSync(pidPath)) {
        unlinkSync(pidPath);
      }
    } catch {
      // Ignore cleanup errors
    }

    daemonLog("Daemon stopped", logFile);
  }

  process.on("SIGTERM", () => {
    cleanup();
    process.exit(0);
  });

  process.on("SIGINT", () => {
    cleanup();
    process.exit(0);
  });
}

// --- Direct invocation entry point ---

// When run directly (not imported), start the daemon with the config path
// passed as the first CLI argument.
const isDirectRun =
  typeof process !== "undefined" &&
  process.argv[1] &&
  (process.argv[1].endsWith("daemon-process.js") ||
    process.argv[1].endsWith("daemon-process.ts"));

if (isDirectRun) {
  const projectDir = process.argv[2];
  if (!projectDir) {
    console.error("Usage: daemon-process <project-dir>");
    process.exit(1);
  }
  runDaemon(projectDir).catch((error) => {
    console.error("Daemon failed:", error);
    process.exit(1);
  });
}
