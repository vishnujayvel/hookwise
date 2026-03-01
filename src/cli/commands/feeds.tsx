/**
 * Feeds command — live auto-refreshing feed dashboard.
 *
 * Shows daemon status, feed health, and cache bus contents.
 * Auto-refreshes every 3 seconds until user presses q/Escape/Ctrl+C.
 */

import React, { useState, useEffect } from "react";
import { Text, Box, useInput, useApp } from "ink";
import { Header } from "../components/header.js";
import { StatusBadge } from "../components/status-badge.js";
import { loadConfig } from "../../core/config.js";
import { getDaemonStatus } from "../../core/feeds/daemon-manager.js";
import { readAll } from "../../core/feeds/cache-bus.js";
import { DEFAULT_CACHE_PATH, DEFAULT_PID_PATH } from "../../core/constants.js";
import type { HooksConfig } from "../../core/types.js";
import type { FeedHealth, DaemonStatus } from "../../core/feeds/daemon-manager.js";

const POLL_INTERVAL_MS = 3_000;

export interface FeedsCommandProps {
  configPath?: string;
  once?: boolean;
}

function formatUptime(seconds: number | null): string {
  if (seconds === null) return "—";
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return `${h}h ${m}m`;
}

function formatAge(isoDate: string | null): string {
  if (!isoDate) return "never";
  const ms = Date.now() - Date.parse(isoDate);
  if (Number.isNaN(ms)) return "?";
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s ago`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`;
  return `${Math.floor(sec / 3600)}h ago`;
}

export function FeedsCommand({ configPath, once }: FeedsCommandProps): React.ReactElement {
  const { exit } = useApp();
  const [config] = useState<HooksConfig | null>(() => {
    try {
      return loadConfig(configPath);
    } catch {
      return null;
    }
  });
  const [status, setStatus] = useState<DaemonStatus | null>(null);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (!config) return;

    // Initial fetch
    setStatus(getDaemonStatus(config, DEFAULT_PID_PATH, DEFAULT_CACHE_PATH));

    if (once) return;

    const interval = setInterval(() => {
      setStatus(getDaemonStatus(config, DEFAULT_PID_PATH, DEFAULT_CACHE_PATH));
      setTick((t) => t + 1);
    }, POLL_INTERVAL_MS);

    return () => clearInterval(interval);
  }, [config, once]);

  useInput((input, key) => {
    if (input === "q" || key.escape) {
      exit();
    }
  });

  useEffect(() => {
    if (!config) {
      process.exitCode = 1;
    }
  }, [config]);

  if (!config) {
    return (
      <Box flexDirection="column">
        <Header />
        <Text color="red">No hookwise.yaml found. Run "hookwise init" first.</Text>
      </Box>
    );
  }

  if (!status) {
    return (
      <Box flexDirection="column">
        <Header />
        <Text dimColor>Loading...</Text>
      </Box>
    );
  }

  const enabledFeeds = status.feeds.filter((f) => f.enabled);
  const healthyCount = enabledFeeds.filter((f) => f.healthy).length;
  const cache = readAll(DEFAULT_CACHE_PATH);
  const cacheKeys = Object.keys(cache).filter((k) => !k.startsWith("_"));

  return (
    <Box flexDirection="column">
      <Header />
      <Text bold>Feed Dashboard {once ? "" : "(live)"}</Text>
      {!once && <Text dimColor>Refreshes every {POLL_INTERVAL_MS / 1000}s — press q to quit</Text>}

      {/* Daemon */}
      <Box flexDirection="column" marginTop={1}>
        <Box gap={1}>
          <StatusBadge status={status.running ? "pass" : "fail"} />
          <Text bold>Daemon: </Text>
          <Text>
            {status.running ? "running" : "stopped"}
            {status.pid ? ` (PID ${status.pid})` : ""}
            {status.uptime !== null ? ` — ${formatUptime(status.uptime)}` : ""}
          </Text>
        </Box>
      </Box>

      {/* Feed health table */}
      <Box flexDirection="column" marginTop={1}>
        <Text bold>Feeds ({healthyCount}/{enabledFeeds.length} healthy)</Text>
        <Box flexDirection="column" paddingLeft={2}>
          {status.feeds.map((feed) => (
            <Box key={feed.name} gap={1}>
              {!feed.enabled ? (
                <>
                  <Text dimColor>○</Text>
                  <Text dimColor>{feed.name.padEnd(10)} disabled</Text>
                </>
              ) : (
                <>
                  <StatusBadge status={feed.healthy ? "pass" : "fail"} />
                  <Text bold>{feed.name.padEnd(10)}</Text>
                  <Text dimColor>
                    {formatAge(feed.lastUpdate)} | {feed.intervalSeconds}s interval
                  </Text>
                </>
              )}
            </Box>
          ))}
        </Box>
      </Box>

      {/* Cache keys */}
      <Box flexDirection="column" marginTop={1}>
        <Text bold>Cache Bus ({cacheKeys.length} keys)</Text>
        <Box flexDirection="column" paddingLeft={2}>
          {cacheKeys.length === 0 ? (
            <Text dimColor>(empty)</Text>
          ) : (
            cacheKeys.map((key) => {
              const entry = cache[key] as Record<string, unknown>;
              const updatedAt = entry?.updated_at as string | undefined;
              return (
                <Text key={key} dimColor>
                  {key.padEnd(10)} {formatAge(updatedAt ?? null)}
                </Text>
              );
            })
          )}
        </Box>
      </Box>

      {!once && (
        <Box marginTop={1}>
          <Text dimColor>Refresh #{tick}</Text>
        </Box>
      )}
    </Box>
  );
}
