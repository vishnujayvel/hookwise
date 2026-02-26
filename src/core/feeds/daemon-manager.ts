/**
 * Daemon lifecycle management: start, stop, status, isRunning.
 *
 * Manages a detached background process that runs the feed daemon.
 * The daemon polls feed producers on staggered intervals and writes
 * results to the shared cache bus.
 *
 * PID file: ~/.hookwise/daemon.pid (plain text, single number)
 * Log file: ~/.hookwise/daemon.log
 *
 * Requirements: FR-2.1, FR-2.2, FR-2.6, FR-9.1, FR-9.2, FR-9.3, NFR-2
 */

import { spawn } from "node:child_process";
import {
  existsSync,
  readFileSync,
  writeFileSync,
  unlinkSync,
  statSync,
} from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  DEFAULT_PID_PATH,
  DEFAULT_CACHE_PATH,
} from "../constants.js";
import { ensureDir } from "../state.js";
import { readAll } from "./cache-bus.js";
import type { HooksConfig, FeedsConfig, DaemonConfig } from "../types.js";

// --- Public interfaces ---

export interface DaemonStartResult {
  started: boolean;
  pid: number | null;
}

export interface DaemonStopResult {
  stopped: boolean;
}

export interface FeedHealth {
  name: string;
  enabled: boolean;
  lastUpdate: string | null;
  intervalSeconds: number;
  healthy: boolean;
}

export interface DaemonStatus {
  running: boolean;
  pid: number | null;
  uptime: number | null;
  feeds: FeedHealth[];
}

// --- Internal helpers ---

/**
 * Read the PID from the PID file.
 * Returns null if the file does not exist or contains invalid content.
 */
function readPid(pidPath: string = DEFAULT_PID_PATH): number | null {
  try {
    if (!existsSync(pidPath)) return null;
    const content = readFileSync(pidPath, "utf-8").trim();
    const pid = parseInt(content, 10);
    if (Number.isNaN(pid) || pid <= 0) return null;
    return pid;
  } catch {
    return null;
  }
}

/**
 * Write a PID to the PID file.
 */
function writePid(pid: number, pidPath: string = DEFAULT_PID_PATH): void {
  ensureDir(dirname(pidPath));
  writeFileSync(pidPath, String(pid), "utf-8");
}

/**
 * Remove the PID file if it exists.
 */
function removePid(pidPath: string = DEFAULT_PID_PATH): void {
  try {
    if (existsSync(pidPath)) {
      unlinkSync(pidPath);
    }
  } catch {
    // Ignore cleanup errors
  }
}

/**
 * Check if a process with the given PID is alive.
 * Uses signal 0 (existence check) — does not actually send a signal.
 */
function isProcessAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

/**
 * Resolve the path to the daemon-process module for spawning.
 */
function getDaemonProcessPath(): string {
  const thisFile = fileURLToPath(import.meta.url);
  return resolve(dirname(thisFile), "daemon-process.js");
}

// --- Public API ---

/**
 * Check if the daemon is currently running.
 *
 * Reads the PID file and checks process liveness via signal 0.
 * Cleans up stale PID files (process dead but PID file exists).
 */
export function isRunning(pidPath: string = DEFAULT_PID_PATH): boolean {
  const pid = readPid(pidPath);
  if (pid === null) return false;

  if (isProcessAlive(pid)) {
    return true;
  }

  // Stale PID file — process is dead but file remains
  removePid(pidPath);
  return false;
}

/**
 * Start the daemon as a detached child process.
 *
 * Prevents duplicate starts by checking isRunning() first.
 * Spawns the daemon-process module with `node --import tsx`.
 * The child process writes its own PID file on startup.
 *
 * @param projectDir - Project directory containing hookwise.yaml
 * @param pidPath - Path to the PID file (default: ~/.hookwise/daemon.pid)
 */
export function startDaemon(
  projectDir: string,
  pidPath: string = DEFAULT_PID_PATH,
): DaemonStartResult {
  // Prevent duplicate starts
  if (isRunning(pidPath)) {
    const existingPid = readPid(pidPath);
    return { started: false, pid: existingPid };
  }

  const daemonProcessPath = getDaemonProcessPath();

  const child = spawn(
    "node",
    ["--import", "tsx", daemonProcessPath, projectDir],
    {
      detached: true,
      stdio: "ignore",
      env: { ...process.env, HOOKWISE_PID_PATH: pidPath },
    },
  );

  const pid = child.pid ?? null;

  // Unref so the parent process can exit without waiting
  child.unref();

  if (pid !== null) {
    // Write PID file immediately from the parent side as well,
    // so isRunning() works right away before the child's init completes.
    writePid(pid, pidPath);
  }

  return { started: pid !== null, pid };
}

/**
 * Stop the daemon by sending SIGTERM.
 *
 * Reads the PID from file, sends SIGTERM, and cleans up the PID file.
 * Returns { stopped: false } if the daemon is not running.
 */
export function stopDaemon(pidPath: string = DEFAULT_PID_PATH): DaemonStopResult {
  const pid = readPid(pidPath);
  if (pid === null) {
    return { stopped: false };
  }

  if (!isProcessAlive(pid)) {
    // Process already dead — clean up stale PID file
    removePid(pidPath);
    return { stopped: false };
  }

  try {
    process.kill(pid, "SIGTERM");
  } catch {
    // Process may have exited between the check and the kill
  }

  removePid(pidPath);
  return { stopped: true };
}

/**
 * Get comprehensive daemon status including feed health.
 *
 * Reads the PID file, checks process liveness, reads cache timestamps
 * for each configured feed, and computes health (updated within 2x interval).
 *
 * @param config - The hookwise config to enumerate feeds
 * @param pidPath - Path to the PID file
 * @param cachePath - Path to the status-line cache file
 */
export function getDaemonStatus(
  config: HooksConfig,
  pidPath: string = DEFAULT_PID_PATH,
  cachePath: string = DEFAULT_CACHE_PATH,
): DaemonStatus {
  const running = isRunning(pidPath);
  const pid = running ? readPid(pidPath) : null;

  // Compute uptime from PID file mtime (when the daemon was started)
  let uptime: number | null = null;
  if (running && existsSync(pidPath)) {
    try {
      const { mtimeMs } = statSync(pidPath);
      uptime = Math.floor((Date.now() - mtimeMs) / 1000);
    } catch {
      uptime = null;
    }
  }

  // Build feed health from cache
  const cache = readAll(cachePath);
  const feeds = buildFeedHealth(config.feeds, cache);

  return { running, pid, uptime, feeds };
}

/**
 * Build feed health entries from config and cache data.
 */
function buildFeedHealth(
  feedsConfig: FeedsConfig,
  cache: Record<string, unknown>,
): FeedHealth[] {
  const health: FeedHealth[] = [];

  // Built-in feeds
  const builtinFeeds = [
    { name: "pulse", enabled: feedsConfig.pulse.enabled, intervalSeconds: feedsConfig.pulse.intervalSeconds },
    { name: "project", enabled: feedsConfig.project.enabled, intervalSeconds: feedsConfig.project.intervalSeconds },
    { name: "calendar", enabled: feedsConfig.calendar.enabled, intervalSeconds: feedsConfig.calendar.intervalSeconds },
    { name: "news", enabled: feedsConfig.news.enabled, intervalSeconds: feedsConfig.news.intervalSeconds },
    { name: "insights", enabled: feedsConfig.insights.enabled, intervalSeconds: feedsConfig.insights.intervalSeconds },
  ];

  for (const feed of builtinFeeds) {
    health.push(computeFeedHealth(feed.name, feed.enabled, feed.intervalSeconds, cache));
  }

  // Custom feeds
  for (const custom of feedsConfig.custom) {
    health.push(computeFeedHealth(custom.name, custom.enabled, custom.intervalSeconds, cache));
  }

  return health;
}

/**
 * Compute the health of a single feed based on its cache entry.
 * A feed is healthy if it was updated within 2x its interval.
 */
function computeFeedHealth(
  name: string,
  enabled: boolean,
  intervalSeconds: number,
  cache: Record<string, unknown>,
): FeedHealth {
  const entry = cache[name] as Record<string, unknown> | undefined;
  const updatedAt = entry?.updated_at as string | undefined;
  const lastUpdate = updatedAt ?? null;

  let healthy = false;
  if (!enabled) {
    // Disabled feeds are considered healthy (not expected to update)
    healthy = true;
  } else if (updatedAt) {
    const updatedMs = Date.parse(updatedAt);
    if (!Number.isNaN(updatedMs)) {
      const staleCutoff = Date.now() - intervalSeconds * 2 * 1000;
      healthy = updatedMs > staleCutoff;
    }
  }

  return { name, enabled, lastUpdate, intervalSeconds, healthy };
}
