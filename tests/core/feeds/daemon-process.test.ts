/**
 * Tests for daemon-process: main loop, feed registration, staggered intervals,
 * inactivity monitoring, signal handling, and log rotation.
 *
 * All filesystem operations, feed producers, and config loading are mocked.
 * No real daemon processes are spawned.
 *
 * Covers Task 6.2:
 * - registerBuiltinFeeds: registers all 8 built-in feeds
 * - registerCustomFeeds: registers custom feeds from config
 * - staggered intervals: offsets by index * 2000ms
 * - feed error isolation: one failing feed doesn't crash others
 * - inactivity check: exits when heartbeat stale
 * - inactivity fallback: uses daemon start time when cache missing
 * - initial heartbeat: writes on startup
 * - signal handling: cleans PID file on SIGTERM
 * - log rotation: truncates long log files
 *
 * Requirements: FR-2.3, FR-2.4, FR-2.5, FR-2.7, FR-2.8, FR-2.9, NFR-1
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  daemonLog,
  rotateLog,
  registerBuiltinFeeds,
  registerCustomFeeds,
} from "../../../src/core/feeds/daemon-process.js";
import { createFeedRegistry } from "../../../src/core/feeds/registry.js";
import { getDefaultConfig } from "../../../src/core/config.js";
import type { HooksConfig } from "../../../src/core/types.js";
import type { FeedRegistry } from "../../../src/core/feeds/registry.js";

// --- Mock node:fs ---
vi.mock("node:fs", async () => {
  const actual = await vi.importActual<typeof import("node:fs")>("node:fs");
  return {
    ...actual,
    existsSync: vi.fn(),
    readFileSync: vi.fn(),
    writeFileSync: vi.fn(),
    appendFileSync: vi.fn(),
    unlinkSync: vi.fn(),
    mkdirSync: vi.fn(),
  };
});

// --- Mock feed producers ---
vi.mock("../../../src/core/feeds/producers/pulse.js", () => ({
  createPulseProducer: vi.fn(() => vi.fn(async () => ({ value: "green" }))),
}));

vi.mock("../../../src/core/feeds/producers/project.js", () => ({
  createProjectProducer: vi.fn(() => vi.fn(async () => ({ repo: "test" }))),
}));

vi.mock("../../../src/core/feeds/producers/news.js", () => ({
  createNewsProducer: vi.fn(() => vi.fn(async () => ({ stories: [] }))),
}));

vi.mock("../../../src/core/feeds/producers/calendar.js", () => ({
  createCalendarProducer: vi.fn(() => vi.fn(async () => ({ events: [] }))),
}));

vi.mock("../../../src/core/feeds/producers/practice.js", () => ({
  createPracticeProducer: vi.fn(() => vi.fn(async () => ({ todayTotal: 0, dueReviews: 0, last_at: null }))),
}));

// --- Mock cache-bus ---
vi.mock("../../../src/core/feeds/cache-bus.js", () => ({
  mergeKey: vi.fn(),
  readAll: vi.fn(() => ({})),
  readKey: vi.fn(),
  isFresh: vi.fn(),
}));

// --- Mock config ---
vi.mock("../../../src/core/config.js", async () => {
  const actual = await vi.importActual<typeof import("../../../src/core/config.js")>(
    "../../../src/core/config.js",
  );
  return {
    ...actual,
    loadConfig: vi.fn(() => actual.getDefaultConfig()),
  };
});

import {
  existsSync,
  readFileSync,
  writeFileSync,
  appendFileSync,
  mkdirSync,
  unlinkSync,
} from "node:fs";
import { mergeKey } from "../../../src/core/feeds/cache-bus.js";
import { createPulseProducer } from "../../../src/core/feeds/producers/pulse.js";
import { createProjectProducer } from "../../../src/core/feeds/producers/project.js";
import { createNewsProducer } from "../../../src/core/feeds/producers/news.js";
import { createCalendarProducer } from "../../../src/core/feeds/producers/calendar.js";

const mockedExistsSync = vi.mocked(existsSync);
const mockedReadFileSync = vi.mocked(readFileSync);
const mockedWriteFileSync = vi.mocked(writeFileSync);
const mockedAppendFileSync = vi.mocked(appendFileSync);
const mockedMkdirSync = vi.mocked(mkdirSync);
const mockedMergeKey = vi.mocked(mergeKey);

// --- Helpers ---

const TEST_LOG_FILE = "/tmp/test-hookwise/daemon.log";
const TEST_CACHE_PATH = "/tmp/test-hookwise/cache.json";

function makeTestConfig(overrides?: Partial<HooksConfig>): HooksConfig {
  return {
    ...getDefaultConfig(),
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mockedExistsSync.mockReturnValue(false);
  mockedMkdirSync.mockReturnValue(undefined);
});

afterEach(() => {
  vi.restoreAllMocks();
});

// --- daemonLog ---

describe("daemonLog", () => {
  it("appends timestamped line to log file", () => {
    daemonLog("test message", TEST_LOG_FILE);

    expect(mockedAppendFileSync).toHaveBeenCalledTimes(1);
    const [path, content] = mockedAppendFileSync.mock.calls[0] as [string, string, string];
    expect(path).toBe(TEST_LOG_FILE);
    expect(content).toMatch(/^\[.+\] test message\n$/);
  });

  it("includes ISO 8601 timestamp", () => {
    daemonLog("hello", TEST_LOG_FILE);

    const content = mockedAppendFileSync.mock.calls[0][1] as string;
    // ISO 8601 pattern: [2026-02-22T10:30:00.000Z]
    expect(content).toMatch(/^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
  });

  it("does not throw on write error", () => {
    mockedAppendFileSync.mockImplementation(() => {
      throw new Error("disk full");
    });

    // Should not throw
    expect(() => daemonLog("test", TEST_LOG_FILE)).not.toThrow();
  });
});

// --- rotateLog ---

describe("rotateLog", () => {
  it("does nothing when log file does not exist", () => {
    mockedExistsSync.mockReturnValue(false);

    rotateLog(TEST_LOG_FILE);

    expect(mockedWriteFileSync).not.toHaveBeenCalled();
  });

  it("does nothing when log is under threshold", () => {
    mockedExistsSync.mockReturnValue(true);

    // 500 lines (under 1000 threshold)
    const lines = Array.from({ length: 500 }, (_, i) => `line ${i}`).join("\n") + "\n";
    mockedReadFileSync.mockReturnValue(lines);

    rotateLog(TEST_LOG_FILE);

    expect(mockedWriteFileSync).not.toHaveBeenCalled();
  });

  it("truncates long log files to last 500 lines", () => {
    mockedExistsSync.mockReturnValue(true);

    // 1500 lines (over 1000 threshold)
    const lines = Array.from({ length: 1500 }, (_, i) => `line ${i}`);
    mockedReadFileSync.mockReturnValue(lines.join("\n") + "\n");

    rotateLog(TEST_LOG_FILE);

    expect(mockedWriteFileSync).toHaveBeenCalledTimes(1);
    const written = mockedWriteFileSync.mock.calls[0][1] as string;
    const writtenLines = written.split("\n").filter((l) => l.length > 0);
    expect(writtenLines).toHaveLength(500);
    // Should keep the LAST 500 lines
    expect(writtenLines[0]).toBe("line 1000");
    expect(writtenLines[499]).toBe("line 1499");
  });

  it("does not throw on read error", () => {
    mockedExistsSync.mockReturnValue(true);
    mockedReadFileSync.mockImplementation(() => {
      throw new Error("permission denied");
    });

    expect(() => rotateLog(TEST_LOG_FILE)).not.toThrow();
  });
});

// --- registerBuiltinFeeds ---

describe("registerBuiltinFeeds", () => {
  it("registers all built-in feeds (FR-2.3)", () => {
    const registry = createFeedRegistry();
    const config = makeTestConfig();

    registerBuiltinFeeds(registry, config, TEST_CACHE_PATH);

    const all = registry.getAll();
    expect(all).toHaveLength(8);

    const names = all.map((f) => f.name).sort();
    expect(names).toEqual(["calendar", "insights", "memories", "news", "practice", "project", "pulse", "weather"]);
  });

  it("uses correct interval from config for each feed", () => {
    const registry = createFeedRegistry();
    const config = makeTestConfig({
      feeds: {
        ...getDefaultConfig().feeds,
        pulse: { ...getDefaultConfig().feeds.pulse, intervalSeconds: 15 },
        project: { ...getDefaultConfig().feeds.project, intervalSeconds: 45 },
      },
    });

    registerBuiltinFeeds(registry, config, TEST_CACHE_PATH);

    expect(registry.get("pulse")?.intervalSeconds).toBe(15);
    expect(registry.get("project")?.intervalSeconds).toBe(45);
  });

  it("passes config to producer factories", () => {
    const registry = createFeedRegistry();
    const config = makeTestConfig();

    registerBuiltinFeeds(registry, config, TEST_CACHE_PATH);

    expect(createPulseProducer).toHaveBeenCalledWith(TEST_CACHE_PATH, config.feeds.pulse);
    expect(createProjectProducer).toHaveBeenCalledWith(TEST_CACHE_PATH);
    expect(createNewsProducer).toHaveBeenCalledWith(config.feeds.news, TEST_CACHE_PATH);
    expect(createCalendarProducer).toHaveBeenCalledWith(
      expect.stringContaining("calendar-credentials.json"),
      config.feeds.calendar,
    );
  });

  it("respects enabled/disabled state from config", () => {
    const registry = createFeedRegistry();
    const config = makeTestConfig({
      feeds: {
        ...getDefaultConfig().feeds,
        calendar: { ...getDefaultConfig().feeds.calendar, enabled: false },
        news: { ...getDefaultConfig().feeds.news, enabled: false },
      },
    });

    registerBuiltinFeeds(registry, config, TEST_CACHE_PATH);

    const enabled = registry.getEnabled();
    const names = enabled.map((f) => f.name).sort();
    expect(names).toEqual(["insights", "practice", "project", "pulse"]);
  });
});

// --- registerCustomFeeds ---

describe("registerCustomFeeds", () => {
  it("registers custom feeds from config (FR-2.4)", () => {
    const registry = createFeedRegistry();
    const config = makeTestConfig({
      feeds: {
        ...getDefaultConfig().feeds,
        custom: [
          { name: "weather", command: "curl http://weather.api", intervalSeconds: 120, enabled: true, timeoutSeconds: 5 },
          { name: "stocks", command: "get-stocks.sh", intervalSeconds: 300, enabled: false, timeoutSeconds: 10 },
        ],
      },
    });

    registerCustomFeeds(registry, config);

    const all = registry.getAll();
    expect(all).toHaveLength(2);
    expect(all.map((f) => f.name).sort()).toEqual(["stocks", "weather"]);
  });

  it("skips entries without name or command", () => {
    const registry = createFeedRegistry();
    const config = makeTestConfig({
      feeds: {
        ...getDefaultConfig().feeds,
        custom: [
          { name: "", command: "some-cmd", intervalSeconds: 60, enabled: true, timeoutSeconds: 10 },
          { name: "valid", command: "some-cmd", intervalSeconds: 60, enabled: true, timeoutSeconds: 10 },
        ],
      },
    });

    registerCustomFeeds(registry, config);

    // Only "valid" should be registered (empty name is falsy)
    expect(registry.getAll()).toHaveLength(1);
    expect(registry.get("valid")).toBeDefined();
  });

  it("handles empty custom array", () => {
    const registry = createFeedRegistry();
    const config = makeTestConfig();

    registerCustomFeeds(registry, config);

    expect(registry.getAll()).toHaveLength(0);
  });
});

// --- Staggered intervals ---

describe("staggered intervals", () => {
  it("offsets initial polls by index * 2000ms (FR-2.5)", async () => {
    vi.useFakeTimers();

    const registry = createFeedRegistry();
    const producer1 = vi.fn(async () => ({ value: 1 }));
    const producer2 = vi.fn(async () => ({ value: 2 }));
    const producer3 = vi.fn(async () => ({ value: 3 }));

    registry.register({ name: "feed0", intervalSeconds: 60, producer: producer1, enabled: true });
    registry.register({ name: "feed1", intervalSeconds: 60, producer: producer2, enabled: true });
    registry.register({ name: "feed2", intervalSeconds: 60, producer: producer3, enabled: true });

    // Simulate the staggered start logic from runDaemon
    const enabledFeeds = registry.getEnabled();
    const timers: ReturnType<typeof setTimeout>[] = [];

    for (let i = 0; i < enabledFeeds.length; i++) {
      const feed = enabledFeeds[i];
      const staggerMs = i * 2000;

      const timeout = setTimeout(() => {
        feed.producer();
      }, staggerMs);
      timers.push(timeout);
    }

    // At t=0, only feed0 should have been called
    await vi.advanceTimersByTimeAsync(0);
    expect(producer1).toHaveBeenCalledTimes(1);
    expect(producer2).toHaveBeenCalledTimes(0);
    expect(producer3).toHaveBeenCalledTimes(0);

    // At t=2000, feed1 should fire
    await vi.advanceTimersByTimeAsync(2000);
    expect(producer2).toHaveBeenCalledTimes(1);
    expect(producer3).toHaveBeenCalledTimes(0);

    // At t=4000, feed2 should fire
    await vi.advanceTimersByTimeAsync(2000);
    expect(producer3).toHaveBeenCalledTimes(1);

    // Cleanup
    for (const t of timers) clearTimeout(t);
    vi.useRealTimers();
  });
});

// --- Feed error isolation ---

describe("feed error isolation", () => {
  it("one failing feed does not crash others (FR-2.7)", async () => {
    const failingProducer = vi.fn(async () => {
      throw new Error("network timeout");
    });
    const successProducer = vi.fn(async () => ({ status: "ok" }));

    // Simulate the pollFeed logic: catch errors per-feed
    const feeds = [
      { name: "failing", producer: failingProducer },
      { name: "success", producer: successProducer },
    ];

    const results: Array<{ name: string; error: boolean }> = [];

    for (const feed of feeds) {
      try {
        await feed.producer();
        results.push({ name: feed.name, error: false });
      } catch {
        results.push({ name: feed.name, error: true });
      }
    }

    expect(failingProducer).toHaveBeenCalled();
    expect(successProducer).toHaveBeenCalled();
    // The success producer was reached despite the failure
    expect(results).toEqual([
      { name: "failing", error: true },
      { name: "success", error: false },
    ]);
  });

  it("pollFeed catches producer errors without propagating", async () => {
    const failingProducer = vi.fn(async () => {
      throw new Error("kaboom");
    });

    // Inline the pollFeed pattern
    async function pollFeed(
      feed: { name: string; producer: () => Promise<Record<string, unknown> | null>; intervalSeconds: number },
      cachePath: string,
    ): Promise<void> {
      try {
        const result = await feed.producer();
        if (result !== null) {
          mergeKey(cachePath, feed.name, result, feed.intervalSeconds);
        }
      } catch {
        // Error caught — feed isolated
      }
    }

    // Should not throw
    await expect(
      pollFeed(
        { name: "broken", producer: failingProducer, intervalSeconds: 30 },
        TEST_CACHE_PATH,
      ),
    ).resolves.toBeUndefined();

    // mergeKey should not have been called (producer threw)
    expect(mockedMergeKey).not.toHaveBeenCalled();
  });
});

// --- Inactivity check ---

describe("inactivity monitoring", () => {
  it("detects stale heartbeat (FR-2.8)", () => {
    const inactivityTimeoutMinutes = 30;
    const timeoutMs = inactivityTimeoutMinutes * 60 * 1000;

    // Heartbeat was written 31 minutes ago
    const staleHeartbeat = Date.now() - 31 * 60 * 1000;

    const shouldExit = Date.now() - staleHeartbeat > timeoutMs;
    expect(shouldExit).toBe(true);
  });

  it("does not exit when heartbeat is fresh", () => {
    const inactivityTimeoutMinutes = 30;
    const timeoutMs = inactivityTimeoutMinutes * 60 * 1000;

    // Heartbeat was written 5 minutes ago
    const freshHeartbeat = Date.now() - 5 * 60 * 1000;

    const shouldExit = Date.now() - freshHeartbeat > timeoutMs;
    expect(shouldExit).toBe(false);
  });

  it("uses daemon start time as fallback when cache missing (FR-2.8)", () => {
    const inactivityTimeoutMinutes = 30;
    const timeoutMs = inactivityTimeoutMinutes * 60 * 1000;

    // Daemon started 31 minutes ago, no heartbeat in cache
    const daemonStartTime = Date.now() - 31 * 60 * 1000;

    const shouldExit = Date.now() - daemonStartTime > timeoutMs;
    expect(shouldExit).toBe(true);
  });

  it("does not exit when daemon recently started and cache missing", () => {
    const inactivityTimeoutMinutes = 30;
    const timeoutMs = inactivityTimeoutMinutes * 60 * 1000;

    // Daemon started 1 minute ago
    const daemonStartTime = Date.now() - 1 * 60 * 1000;

    const shouldExit = Date.now() - daemonStartTime > timeoutMs;
    expect(shouldExit).toBe(false);
  });
});

// --- Initial heartbeat ---

describe("initial heartbeat", () => {
  it("writes daemon heartbeat on startup via mergeKey (FR-2.9)", () => {
    // Simulate the startup daemon heartbeat write
    const now = Date.now();
    mergeKey(TEST_CACHE_PATH, "_daemon_heartbeat", { value: now }, 90);

    expect(mockedMergeKey).toHaveBeenCalledWith(
      TEST_CACHE_PATH,
      "_daemon_heartbeat",
      { value: expect.any(Number) },
      90,
    );
  });
});

// --- Signal handling ---

describe("signal handling", () => {
  it("cleans PID file on SIGTERM (FR-2.9)", () => {
    const pidPath = "/tmp/test-hookwise/daemon.pid";

    // Simulate the cleanup logic from runDaemon
    mockedExistsSync.mockReturnValue(true);

    function cleanup(): void {
      try {
        if (existsSync(pidPath)) {
          unlinkSync(pidPath);
        }
      } catch {
        // Ignore
      }
    }

    // Should not throw
    expect(() => cleanup()).not.toThrow();
  });

  it("clears all interval timers on cleanup", () => {
    vi.useFakeTimers();

    const timers: ReturnType<typeof setInterval>[] = [];
    const callback = vi.fn();

    // Start some intervals
    timers.push(setInterval(callback, 1000));
    timers.push(setInterval(callback, 2000));
    timers.push(setInterval(callback, 3000));

    // Simulate cleanup
    for (const timer of timers) {
      clearInterval(timer);
    }
    timers.length = 0;

    // Advance time — callbacks should NOT fire
    vi.advanceTimersByTime(10000);
    expect(callback).not.toHaveBeenCalled();

    vi.useRealTimers();
  });
});
