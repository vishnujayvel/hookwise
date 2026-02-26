/**
 * Daemon CLI command: start, stop, status.
 *
 * Uses plain stdout (not React/Ink) so it works in scripts and pipelines.
 * Called directly from runCli() in app.tsx, bypassing the render() path.
 *
 * Requirements: FR-9.1, FR-9.2, FR-9.3, FR-9.4
 */

import {
  startDaemon,
  stopDaemon,
  getDaemonStatus,
} from "../../core/feeds/daemon-manager.js";
import { loadConfig } from "../../core/config.js";
import type { DaemonStatus, FeedHealth } from "../../core/feeds/daemon-manager.js";

/**
 * Format seconds into a human-readable uptime string.
 * Examples: "0m", "45m", "1h 23m", "25h 0m"
 */
export function formatUptime(seconds: number): string {
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (hours === 0) {
    return `${minutes}m`;
  }
  return `${hours}h ${minutes}m`;
}

/**
 * Format a timestamp into a relative time string.
 * Examples: "12s ago", "5m ago", "2h ago"
 */
export function formatRelativeTime(isoTimestamp: string): string {
  const updatedMs = Date.parse(isoTimestamp);
  if (Number.isNaN(updatedMs)) return "unknown";

  const diffSeconds = Math.floor((Date.now() - updatedMs) / 1000);
  if (diffSeconds < 60) {
    return `${diffSeconds}s ago`;
  }
  if (diffSeconds < 3600) {
    return `${Math.floor(diffSeconds / 60)}m ago`;
  }
  return `${Math.floor(diffSeconds / 3600)}h ago`;
}

/**
 * Format interval seconds into a human-readable string.
 * Examples: "every 30s", "every 60s", "every 5m", "every 30m"
 */
function formatInterval(seconds: number): string {
  if (seconds < 60) {
    return `every ${seconds}s`;
  }
  return `every ${Math.floor(seconds / 60)}m`;
}

/**
 * Format a single feed health entry for the status display.
 */
function formatFeedLine(feed: FeedHealth): string {
  const name = feed.name.padEnd(10);
  const icon = feed.healthy ? "\u2713" : "\u2717";
  const status = feed.lastUpdate
    ? `last update ${formatRelativeTime(feed.lastUpdate)}`
    : "stale";
  const interval = `(${formatInterval(feed.intervalSeconds)})`;
  return `  ${name}${icon}  ${status.padEnd(22)}${interval}`;
}

/**
 * Format the full daemon status output.
 */
export function formatDaemonStatus(status: DaemonStatus): string {
  if (!status.running) {
    return "Hookwise Daemon: not running";
  }

  const uptimeStr = status.uptime !== null ? formatUptime(status.uptime) : "unknown";
  const lines: string[] = [
    `Hookwise Daemon: running (PID ${status.pid}, uptime ${uptimeStr})`,
  ];

  if (status.feeds.length > 0) {
    lines.push("");
    lines.push("Feeds:");
    for (const feed of status.feeds) {
      lines.push(formatFeedLine(feed));
    }
  }

  return lines.join("\n");
}

/**
 * Run the daemon CLI command.
 *
 * @param subcommand - "start", "stop", or "status"
 * @param configPath - Optional path to config file (for start/status)
 */
export async function runDaemonCommand(
  subcommand: string,
  configPath?: string,
): Promise<void> {
  try {
    switch (subcommand) {
      case "start": {
        const effectivePath = configPath ?? process.cwd();
        const result = startDaemon(effectivePath);
        if (result.started) {
          console.log(`Hookwise daemon started (PID ${result.pid})`);
        } else {
          console.log(`Hookwise daemon is already running (PID ${result.pid})`);
        }
        break;
      }

      case "stop": {
        const result = stopDaemon();
        if (result.stopped) {
          console.log("Hookwise daemon stopped");
        } else {
          console.log("Hookwise daemon is not running");
        }
        break;
      }

      case "status": {
        const config = loadConfig(configPath);
        const status = getDaemonStatus(config);
        console.log(formatDaemonStatus(status));
        break;
      }

      default:
        console.error(`Unknown daemon subcommand: ${subcommand}`);
        console.error("Usage: hookwise daemon <start|stop|status>");
        process.exitCode = 1;
        break;
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    console.error(`Daemon ${subcommand} failed: ${message}`);
    process.exitCode = 1;
  }
}
