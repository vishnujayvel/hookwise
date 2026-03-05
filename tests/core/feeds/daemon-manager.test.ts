/**
 * Tests for daemon-manager: lifecycle management (start, stop, status, isRunning).
 *
 * All child_process.spawn, process.kill, and filesystem operations are mocked.
 * No real processes are spawned.
 *
 * Covers Task 6.1:
 * - startDaemon: writes PID file, returns pid
 * - startDaemon: duplicate detection (already running returns started: false)
 * - stopDaemon: sends SIGTERM, cleans PID file
 * - stopDaemon: returns stopped: false when not running
 * - isRunning: returns true when process alive
 * - isRunning: cleans stale PID (process dead)
 * - isRunning: returns false when no PID file
 * - getDaemonStatus: reports feed health from cache timestamps
 *
 * Requirements: FR-2.1, FR-2.2, FR-2.6, FR-9.1, FR-9.2, FR-9.3, NFR-2
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  isRunning,
  startDaemon,
  stopDaemon,
  getDaemonStatus,
} from "../../../src/core/feeds/daemon-manager.js";
import type { HooksConfig } from "../../../src/core/types.js";
import { getDefaultConfig } from "../../../src/core/config.js";

// --- Mock node:child_process ---
vi.mock("node:child_process", () => ({
  spawn: vi.fn(),
}));

// --- Mock node:fs ---
vi.mock("node:fs", async () => {
  const actual = await vi.importActual<typeof import("node:fs")>("node:fs");
  return {
    ...actual,
    existsSync: vi.fn(),
    readFileSync: vi.fn(),
    writeFileSync: vi.fn(),
    unlinkSync: vi.fn(),
    statSync: vi.fn(),
    mkdirSync: vi.fn(),
  };
});

// --- Mock cache-bus ---
vi.mock("../../../src/core/feeds/cache-bus.js", () => ({
  readAll: vi.fn(),
  mergeKey: vi.fn(),
  readKey: vi.fn(),
  isFresh: vi.fn(),
}));

import { spawn } from "node:child_process";
import { existsSync, readFileSync, writeFileSync, unlinkSync, statSync, mkdirSync } from "node:fs";
import { readAll } from "../../../src/core/feeds/cache-bus.js";

const mockedSpawn = vi.mocked(spawn);
const mockedExistsSync = vi.mocked(existsSync);
const mockedReadFileSync = vi.mocked(readFileSync);
const mockedWriteFileSync = vi.mocked(writeFileSync);
const mockedUnlinkSync = vi.mocked(unlinkSync);
const mockedStatSync = vi.mocked(statSync);
const mockedMkdirSync = vi.mocked(mkdirSync);
const mockedReadAll = vi.mocked(readAll);

// --- Test Helpers ---

const TEST_PID_PATH = "/tmp/test-hookwise/daemon.pid";
const TEST_CACHE_PATH = "/tmp/test-hookwise/cache.json";
const TEST_CONFIG_PATH = "/tmp/test-hookwise/hookwise.yaml";

function makeTestConfig(overrides?: Partial<HooksConfig>): HooksConfig {
  return {
    ...getDefaultConfig(),
    ...overrides,
  };
}

// Save original process.kill
const originalKill = process.kill;

beforeEach(() => {
  vi.clearAllMocks();
  // Default: no files exist
  mockedExistsSync.mockReturnValue(false);
  mockedMkdirSync.mockReturnValue(undefined);
});

afterEach(() => {
  vi.restoreAllMocks();
  // Restore process.kill
  process.kill = originalKill;
});

// --- isRunning ---

describe("isRunning", () => {
  it("returns false when no PID file exists", () => {
    mockedExistsSync.mockReturnValue(false);

    expect(isRunning(TEST_PID_PATH)).toBe(false);
  });

  it("returns true when process is alive (FR-9.1)", () => {
    mockedExistsSync.mockReturnValue(true);
    mockedReadFileSync.mockReturnValue("12345");

    // Mock process.kill(pid, 0) — signal 0 = existence check
    process.kill = vi.fn(() => true) as unknown as typeof process.kill;

    expect(isRunning(TEST_PID_PATH)).toBe(true);
  });

  it("cleans stale PID when process is dead (NFR-2)", () => {
    mockedExistsSync.mockReturnValue(true);
    mockedReadFileSync.mockReturnValue("99999");

    // Mock process.kill to throw (process not found)
    process.kill = vi.fn(() => {
      throw new Error("ESRCH");
    }) as unknown as typeof process.kill;

    expect(isRunning(TEST_PID_PATH)).toBe(false);

    // Should have cleaned up the stale PID file
    expect(mockedUnlinkSync).toHaveBeenCalledWith(TEST_PID_PATH);
  });

  it("returns false for invalid PID file content", () => {
    mockedExistsSync.mockReturnValue(true);
    mockedReadFileSync.mockReturnValue("not-a-number");

    expect(isRunning(TEST_PID_PATH)).toBe(false);
  });

  it("returns false for empty PID file", () => {
    mockedExistsSync.mockReturnValue(true);
    mockedReadFileSync.mockReturnValue("");

    expect(isRunning(TEST_PID_PATH)).toBe(false);
  });

  it("returns false for negative PID", () => {
    mockedExistsSync.mockReturnValue(true);
    mockedReadFileSync.mockReturnValue("-1");

    expect(isRunning(TEST_PID_PATH)).toBe(false);
  });
});

// --- startDaemon ---

describe("startDaemon", () => {
  it("spawns daemon and returns pid (FR-2.1)", () => {
    // Not currently running
    mockedExistsSync.mockReturnValue(false);

    const mockChild = {
      pid: 42,
      unref: vi.fn(),
      on: vi.fn(),
    };
    mockedSpawn.mockReturnValue(mockChild as any);

    const result = startDaemon(TEST_CONFIG_PATH, TEST_PID_PATH);

    expect(result.started).toBe(true);
    expect(result.pid).toBe(42);
    expect(mockChild.unref).toHaveBeenCalled();
    expect(mockedWriteFileSync).toHaveBeenCalledWith(
      TEST_PID_PATH,
      "42",
      { encoding: "utf-8", flag: "wx" },
    );
  });

  it("prevents duplicate starts when already running (FR-2.2)", () => {
    // Already running: PID file exists, process alive
    mockedExistsSync.mockReturnValue(true);
    mockedReadFileSync.mockReturnValue("12345");
    process.kill = vi.fn(() => true) as unknown as typeof process.kill;

    const result = startDaemon(TEST_CONFIG_PATH, TEST_PID_PATH);

    expect(result.started).toBe(false);
    expect(result.pid).toBe(12345);
    expect(mockedSpawn).not.toHaveBeenCalled();
  });

  it("spawns with correct arguments", () => {
    mockedExistsSync.mockReturnValue(false);

    const mockChild = {
      pid: 100,
      unref: vi.fn(),
      on: vi.fn(),
    };
    mockedSpawn.mockReturnValue(mockChild as any);

    startDaemon(TEST_CONFIG_PATH, TEST_PID_PATH);

    expect(mockedSpawn).toHaveBeenCalledTimes(1);
    const [cmd, args, opts] = mockedSpawn.mock.calls[0];
    expect(cmd).toBe("node");
    expect(args[0]).toBe("--import");
    expect(args[1]).toBe("tsx");
    expect(args[2]).toMatch(/daemon-process\.js$/);
    expect(args[3]).toBe(TEST_CONFIG_PATH);
    expect(opts).toMatchObject({
      detached: true,
      stdio: "ignore",
    });
  });

  it("handles spawn failure (pid is undefined)", () => {
    mockedExistsSync.mockReturnValue(false);

    const mockChild = {
      pid: undefined,
      unref: vi.fn(),
      on: vi.fn(),
    };
    mockedSpawn.mockReturnValue(mockChild as any);

    const result = startDaemon(TEST_CONFIG_PATH, TEST_PID_PATH);

    expect(result.started).toBe(false);
    expect(result.pid).toBeNull();
  });
});

// --- stopDaemon ---

describe("stopDaemon", () => {
  it("sends SIGTERM and cleans PID file (FR-2.6)", () => {
    // PID file exists with valid PID
    mockedExistsSync.mockReturnValue(true);
    mockedReadFileSync.mockReturnValue("12345");

    // Process is alive
    const killFn = vi.fn(() => true);
    process.kill = killFn as unknown as typeof process.kill;

    const result = stopDaemon(TEST_PID_PATH);

    expect(result.stopped).toBe(true);

    // Should have sent SIGTERM
    expect(killFn).toHaveBeenCalledWith(12345, "SIGTERM");

    // Should have cleaned up PID file
    expect(mockedUnlinkSync).toHaveBeenCalledWith(TEST_PID_PATH);
  });

  it("returns stopped: false when not running (no PID file)", () => {
    mockedExistsSync.mockReturnValue(false);

    const result = stopDaemon(TEST_PID_PATH);

    expect(result.stopped).toBe(false);
  });

  it("returns stopped: false when process is already dead", () => {
    mockedExistsSync.mockReturnValue(true);
    mockedReadFileSync.mockReturnValue("99999");

    // First call (signal 0 check): throw ESRCH
    // Second call (SIGTERM): should not be reached
    const killFn = vi.fn(() => {
      throw new Error("ESRCH");
    });
    process.kill = killFn as unknown as typeof process.kill;

    const result = stopDaemon(TEST_PID_PATH);

    expect(result.stopped).toBe(false);
    // PID file should be cleaned up even though process was already dead
    expect(mockedUnlinkSync).toHaveBeenCalled();
  });
});

// --- getDaemonStatus ---

describe("getDaemonStatus", () => {
  it("reports not running when no PID file", () => {
    mockedExistsSync.mockReturnValue(false);
    mockedReadAll.mockReturnValue({});

    const config = makeTestConfig();
    const status = getDaemonStatus(config, TEST_PID_PATH, TEST_CACHE_PATH);

    expect(status.running).toBe(false);
    expect(status.pid).toBeNull();
    expect(status.uptime).toBeNull();
  });

  it("reports feed health from cache timestamps (FR-9.2)", () => {
    mockedExistsSync.mockReturnValue(false);

    const now = new Date().toISOString();
    const staleTime = new Date(Date.now() - 300_000).toISOString(); // 5 min ago

    mockedReadAll.mockReturnValue({
      pulse: { updated_at: now, ttl_seconds: 30 },
      project: { updated_at: staleTime, ttl_seconds: 60 },
    });

    const config = makeTestConfig();
    const status = getDaemonStatus(config, TEST_PID_PATH, TEST_CACHE_PATH);

    expect(status.feeds).toHaveLength(8); // 8 built-in, 0 custom

    const pulseFeed = status.feeds.find((f) => f.name === "pulse");
    expect(pulseFeed).toBeDefined();
    expect(pulseFeed!.lastUpdate).toBe(now);
    expect(pulseFeed!.healthy).toBe(true); // updated within 2x30s = 60s

    const projectFeed = status.feeds.find((f) => f.name === "project");
    expect(projectFeed).toBeDefined();
    expect(projectFeed!.lastUpdate).toBe(staleTime);
    expect(projectFeed!.healthy).toBe(false); // 5 min > 2x60s = 120s
  });

  it("marks disabled feeds as healthy", () => {
    mockedExistsSync.mockReturnValue(false);
    mockedReadAll.mockReturnValue({});

    const config = makeTestConfig({
      feeds: {
        ...getDefaultConfig().feeds,
        calendar: { ...getDefaultConfig().feeds.calendar, enabled: false },
      },
    });

    const status = getDaemonStatus(config, TEST_PID_PATH, TEST_CACHE_PATH);

    const calendarFeed = status.feeds.find((f) => f.name === "calendar");
    expect(calendarFeed).toBeDefined();
    expect(calendarFeed!.enabled).toBe(false);
    expect(calendarFeed!.healthy).toBe(true);
  });

  it("includes custom feeds in health report", () => {
    mockedExistsSync.mockReturnValue(false);
    mockedReadAll.mockReturnValue({
      my_custom_feed: { updated_at: new Date().toISOString(), ttl_seconds: 120 },
    });

    const config = makeTestConfig({
      feeds: {
        ...getDefaultConfig().feeds,
        custom: [
          { name: "my_custom_feed", command: "echo '{}'", intervalSeconds: 120, enabled: true, timeoutSeconds: 10 },
        ],
      },
    });

    const status = getDaemonStatus(config, TEST_PID_PATH, TEST_CACHE_PATH);

    expect(status.feeds).toHaveLength(9); // 8 built-in + 1 custom
    const customFeed = status.feeds.find((f) => f.name === "my_custom_feed");
    expect(customFeed).toBeDefined();
    expect(customFeed!.enabled).toBe(true);
    expect(customFeed!.healthy).toBe(true);
  });

  it("reports running with uptime when daemon is alive (FR-9.3)", () => {
    // PID file exists
    mockedExistsSync.mockReturnValue(true);
    mockedReadFileSync.mockReturnValue("12345");

    // Process is alive
    process.kill = vi.fn(() => true) as unknown as typeof process.kill;

    // PID file was created 60 seconds ago
    const sixtySecondsAgo = Date.now() - 60_000;
    mockedStatSync.mockReturnValue({ mtimeMs: sixtySecondsAgo } as any);

    mockedReadAll.mockReturnValue({});

    const config = makeTestConfig();
    const status = getDaemonStatus(config, TEST_PID_PATH, TEST_CACHE_PATH);

    expect(status.running).toBe(true);
    expect(status.pid).toBe(12345);
    expect(status.uptime).toBeGreaterThanOrEqual(59);
    expect(status.uptime).toBeLessThanOrEqual(62);
  });
});
