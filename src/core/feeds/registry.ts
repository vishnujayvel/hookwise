/**
 * FeedRegistry: registration and lookup for feed definitions.
 *
 * Provides a simple Map-backed registry with:
 * - register(): add a feed definition (rejects duplicates)
 * - get(): look up a feed by name
 * - getAll(): list all registered feeds
 * - getEnabled(): list only feeds with enabled === true
 *
 * Also exports createCommandProducer() to wrap a shell command as a FeedProducer
 * for custom feeds defined in hookwise.yaml.
 *
 * Requirements: FR-1.1, FR-1.2, FR-1.3, FR-1.4, FR-1.5
 */

import { exec } from "node:child_process";
import type { FeedDefinition, FeedProducer } from "../types.js";
import { DEFAULT_FEED_TIMEOUT } from "../constants.js";

/**
 * Public interface for the feed registry.
 */
export interface FeedRegistry {
  register(feed: FeedDefinition): void;
  get(name: string): FeedDefinition | undefined;
  getAll(): FeedDefinition[];
  getEnabled(): FeedDefinition[];
}

/**
 * Create a new FeedRegistry instance backed by a Map.
 *
 * Not a singleton -- callers can create multiple independent registries
 * (useful for testing and for isolating built-in vs. custom feeds).
 */
export function createFeedRegistry(): FeedRegistry {
  const feeds = new Map<string, FeedDefinition>();

  return {
    /**
     * Register a feed definition.
     * Throws if a feed with the same name is already registered.
     */
    register(feed: FeedDefinition): void {
      if (feeds.has(feed.name)) {
        throw new Error(
          `Feed "${feed.name}" is already registered. Each feed name must be unique.`,
        );
      }
      feeds.set(feed.name, feed);
    },

    /**
     * Look up a feed definition by name.
     * Returns undefined if the feed is not registered.
     */
    get(name: string): FeedDefinition | undefined {
      const feed = feeds.get(name);
      return feed ? { ...feed } : undefined;
    },

    /**
     * Return all registered feed definitions (enabled and disabled).
     * Returns shallow copies to prevent external mutation of registry state.
     */
    getAll(): FeedDefinition[] {
      return Array.from(feeds.values()).map((f) => ({ ...f }));
    },

    /**
     * Return only the feed definitions that are enabled.
     * Returns shallow copies to prevent external mutation of registry state.
     */
    getEnabled(): FeedDefinition[] {
      return Array.from(feeds.values())
        .filter((f) => f.enabled)
        .map((f) => ({ ...f }));
    },
  };
}

/**
 * Create a FeedProducer that spawns a shell command, captures stdout,
 * parses it as JSON, and returns the result.
 *
 * Returns null when:
 * - The command exits with a non-zero code
 * - The command times out
 * - stdout is not valid JSON or exceeds maxBuffer
 *
 * Security: The command string is user-authored YAML config, treated as trusted
 * (same trust model as existing custom segments and script handlers in hookwise).
 *
 * @param command  - The shell command to execute
 * @param timeoutMs - Maximum time to wait for the command (default: DEFAULT_FEED_TIMEOUT * 1000)
 */
export function createCommandProducer(
  command: string,
  timeoutMs: number = DEFAULT_FEED_TIMEOUT * 1000,
): FeedProducer {
  return () =>
    new Promise<Record<string, unknown> | null>((resolve) => {
      const child = exec(command, { timeout: timeoutMs, maxBuffer: 1024 * 1024 }, (error, stdout) => {
        if (error) {
          // Command failed or timed out
          resolve(null);
          return;
        }

        try {
          const parsed = JSON.parse(stdout);
          // Only accept plain objects (not arrays, nulls, primitives)
          if (
            parsed !== null &&
            typeof parsed === "object" &&
            !Array.isArray(parsed)
          ) {
            resolve(parsed as Record<string, unknown>);
          } else {
            resolve(null);
          }
        } catch {
          // stdout was not valid JSON
          resolve(null);
        }
      });

      // Safety net: if the child process hangs beyond the timeout,
      // Node's exec timeout will kill it and the error callback fires.
      // This is just a defensive no-op reference to suppress
      // unhandled-rejection on the child process.
      child.on("error", () => {
        // Already handled in the exec callback
      });
    });
}
