/**
 * Tests for the daemon CLI command: start, stop, status.
 *
 * Mocks daemon-manager functions and captures console.log output.
 * Does NOT mock React/Ink — daemon commands use plain stdout.
 *
 * Covers Task 7.1:
 * - daemon start: prints PID when started
 * - daemon start: prints already running message
 * - daemon stop: prints stopped
 * - daemon stop: prints not running
 * - daemon status: formats running output with feed health
 * - daemon status: formats not running
 * - unknown subcommand: prints usage
 *
 * Requirements: FR-9.1, FR-9.2, FR-9.3, FR-9.4
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// --- Mock daemon-manager ---
vi.mock("../../src/core/feeds/daemon-manager.js", () => ({
  startDaemon: vi.fn(),
  stopDaemon: vi.fn(),
  getDaemonStatus: vi.fn(),
  isRunning: vi.fn(),
}));

// --- Mock config ---
vi.mock("../../src/core/config.js", () => ({
  loadConfig: vi.fn(),
}));

import { runDaemonCommand, formatUptime, formatRelativeTime, formatDaemonStatus } from "../../src/cli/commands/daemon.js";
import { startDaemon, stopDaemon, getDaemonStatus } from "../../src/core/feeds/daemon-manager.js";
import { loadConfig } from "../../src/core/config.js";
import type { DaemonStatus } from "../../src/core/feeds/daemon-manager.js";

const mockedStartDaemon = vi.mocked(startDaemon);
const mockedStopDaemon = vi.mocked(stopDaemon);
const mockedGetDaemonStatus = vi.mocked(getDaemonStatus);
const mockedLoadConfig = vi.mocked(loadConfig);

describe("daemon CLI command", () => {
  let logSpy: ReturnType<typeof vi.spyOn>;
  let errorSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    logSpy = vi.spyOn(console, "log").mockImplementation(() => {});
    errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    process.exitCode = undefined;
  });

  afterEach(() => {
    logSpy.mockRestore();
    errorSpy.mockRestore();
    vi.restoreAllMocks();
    process.exitCode = undefined;
  });

  describe("start", () => {
    it("prints PID when daemon is started", async () => {
      mockedStartDaemon.mockReturnValue({ started: true, pid: 42891 });

      await runDaemonCommand("start");

      expect(logSpy).toHaveBeenCalledWith("Hookwise daemon started (PID 42891)");
    });

    it("prints already running message when daemon is already running", async () => {
      mockedStartDaemon.mockReturnValue({ started: false, pid: 42891 });

      await runDaemonCommand("start");

      expect(logSpy).toHaveBeenCalledWith("Hookwise daemon is already running (PID 42891)");
    });

    it("passes configPath to startDaemon when provided", async () => {
      mockedStartDaemon.mockReturnValue({ started: true, pid: 100 });

      await runDaemonCommand("start", "/custom/config.yaml");

      expect(mockedStartDaemon).toHaveBeenCalledWith("/custom/config.yaml");
    });
  });

  describe("stop", () => {
    it("prints stopped when daemon was running", async () => {
      mockedStopDaemon.mockReturnValue({ stopped: true });

      await runDaemonCommand("stop");

      expect(logSpy).toHaveBeenCalledWith("Hookwise daemon stopped");
    });

    it("prints not running when daemon was not running", async () => {
      mockedStopDaemon.mockReturnValue({ stopped: false });

      await runDaemonCommand("stop");

      expect(logSpy).toHaveBeenCalledWith("Hookwise daemon is not running");
    });
  });

  describe("status", () => {
    it("formats running output with feed health", async () => {
      const now = new Date();
      const status: DaemonStatus = {
        running: true,
        pid: 42891,
        uptime: 4980, // 1h 23m
        feeds: [
          { name: "pulse", enabled: true, lastUpdate: new Date(now.getTime() - 12000).toISOString(), intervalSeconds: 30, healthy: true },
          { name: "project", enabled: true, lastUpdate: new Date(now.getTime() - 45000).toISOString(), intervalSeconds: 60, healthy: true },
          { name: "calendar", enabled: true, lastUpdate: null, intervalSeconds: 300, healthy: false },
          { name: "news", enabled: true, lastUpdate: new Date(now.getTime() - 480000).toISOString(), intervalSeconds: 1800, healthy: true },
        ],
      };

      mockedLoadConfig.mockReturnValue({} as any);
      mockedGetDaemonStatus.mockReturnValue(status);

      await runDaemonCommand("status");

      const output = logSpy.mock.calls[0][0] as string;
      expect(output).toContain("Hookwise Daemon: running (PID 42891, uptime 1h 23m)");
      expect(output).toContain("Feeds:");
      expect(output).toContain("pulse");
      expect(output).toContain("project");
      expect(output).toContain("calendar");
      expect(output).toContain("news");
      expect(output).toContain("\u2713"); // healthy check mark
      expect(output).toContain("\u2717"); // unhealthy cross mark
    });

    it("formats not running status", async () => {
      const status: DaemonStatus = {
        running: false,
        pid: null,
        uptime: null,
        feeds: [],
      };

      mockedLoadConfig.mockReturnValue({} as any);
      mockedGetDaemonStatus.mockReturnValue(status);

      await runDaemonCommand("status");

      expect(logSpy).toHaveBeenCalledWith("Hookwise Daemon: not running");
    });
  });

  describe("unknown subcommand", () => {
    it("prints usage error for unknown subcommand", async () => {
      await runDaemonCommand("restart");

      expect(errorSpy).toHaveBeenCalledWith("Unknown daemon subcommand: restart");
      expect(errorSpy).toHaveBeenCalledWith("Usage: hookwise daemon <start|stop|status>");
      expect(process.exitCode).toBe(1);
    });
  });
});

describe("formatUptime", () => {
  it("formats seconds under an hour", () => {
    expect(formatUptime(0)).toBe("0m");
    expect(formatUptime(59)).toBe("0m");
    expect(formatUptime(60)).toBe("1m");
    expect(formatUptime(2700)).toBe("45m");
  });

  it("formats hours and minutes", () => {
    expect(formatUptime(3600)).toBe("1h 0m");
    expect(formatUptime(4980)).toBe("1h 23m");
    expect(formatUptime(90000)).toBe("25h 0m");
  });
});

describe("formatRelativeTime", () => {
  it("formats seconds ago", () => {
    const now = new Date();
    const timestamp = new Date(now.getTime() - 12000).toISOString();
    expect(formatRelativeTime(timestamp)).toBe("12s ago");
  });

  it("formats minutes ago", () => {
    const now = new Date();
    const timestamp = new Date(now.getTime() - 300000).toISOString();
    expect(formatRelativeTime(timestamp)).toBe("5m ago");
  });

  it("formats hours ago", () => {
    const now = new Date();
    const timestamp = new Date(now.getTime() - 7200000).toISOString();
    expect(formatRelativeTime(timestamp)).toBe("2h ago");
  });

  it("returns unknown for invalid timestamp", () => {
    expect(formatRelativeTime("invalid")).toBe("unknown");
  });
});

describe("formatDaemonStatus", () => {
  it("returns not running message when daemon is not running", () => {
    const status: DaemonStatus = {
      running: false,
      pid: null,
      uptime: null,
      feeds: [],
    };
    expect(formatDaemonStatus(status)).toBe("Hookwise Daemon: not running");
  });

  it("includes feeds section when running with feeds", () => {
    const now = new Date();
    const status: DaemonStatus = {
      running: true,
      pid: 42891,
      uptime: 4980,
      feeds: [
        { name: "pulse", enabled: true, lastUpdate: new Date(now.getTime() - 12000).toISOString(), intervalSeconds: 30, healthy: true },
      ],
    };
    const output = formatDaemonStatus(status);
    expect(output).toContain("Hookwise Daemon: running (PID 42891, uptime 1h 23m)");
    expect(output).toContain("Feeds:");
    expect(output).toContain("pulse");
  });

  it("shows unknown uptime when uptime is null", () => {
    const status: DaemonStatus = {
      running: true,
      pid: 100,
      uptime: null,
      feeds: [],
    };
    const output = formatDaemonStatus(status);
    expect(output).toContain("uptime unknown");
  });
});
