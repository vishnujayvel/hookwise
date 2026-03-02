/**
 * Tests for feed platform integration in the dispatcher.
 *
 * Covers Task 8.1:
 * - Heartbeat written on every dispatch
 * - CWD written on every dispatch
 * - Daemon auto-started on SessionStart when autoStart is true
 * - Daemon NOT auto-started when autoStart is false
 * - Daemon NOT started on non-SessionStart events
 * - Feed cache write errors are silently swallowed (fail-open)
 * - Daemon start errors are silently swallowed (fail-open)
 *
 * Requirements: FR-2.3, FR-3.8, FR-12.1, FR-12.2, NFR-2
 */

import { describe, it, expect, vi, beforeEach } from "vitest";

// --- Mock cache-bus ---
vi.mock("../../../src/core/feeds/cache-bus.js", () => ({
  mergeKey: vi.fn(),
  readKey: vi.fn(),
  readAll: vi.fn(),
  isFresh: vi.fn(),
}));

// --- Mock daemon-manager ---
vi.mock("../../../src/core/feeds/daemon-manager.js", () => ({
  isRunning: vi.fn(),
  startDaemon: vi.fn(),
  stopDaemon: vi.fn(),
  getDaemonStatus: vi.fn(),
}));

import { dispatch } from "../../../src/core/dispatcher.js";
import { getDefaultConfig } from "../../../src/core/config.js";
import { mergeKey } from "../../../src/core/feeds/cache-bus.js";
import { isRunning, startDaemon } from "../../../src/core/feeds/daemon-manager.js";
import type { HooksConfig, HookPayload, EventType } from "../../../src/core/types.js";

const mockedMergeKey = vi.mocked(mergeKey);
const mockedIsRunning = vi.mocked(isRunning);
const mockedStartDaemon = vi.mocked(startDaemon);

// --- Helpers ---

function makePayload(overrides: Partial<HookPayload> = {}): HookPayload {
  return {
    session_id: "test-session-123",
    ...overrides,
  };
}

function makeConfig(overrides?: Partial<HooksConfig>): HooksConfig {
  return {
    ...getDefaultConfig(),
    ...overrides,
  };
}

// --- Tests ---

describe("dispatch — feed platform integration", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockedIsRunning.mockReturnValue(false);
    mockedStartDaemon.mockReturnValue({ started: true, pid: 12345 });
  });

  describe("heartbeat + CWD writes", () => {
    it("writes heartbeat on every dispatch", () => {
      const config = makeConfig();
      dispatch("PreToolUse", makePayload({ tool_name: "Bash" }), { config });

      // mergeKey should be called for _heartbeat
      const heartbeatCall = mockedMergeKey.mock.calls.find(
        (call) => call[1] === "_heartbeat"
      );
      expect(heartbeatCall).toBeDefined();
      expect(heartbeatCall![0]).toBe(config.statusLine.cachePath);
      expect(heartbeatCall![2]).toHaveProperty("value");
      expect(typeof heartbeatCall![2].value).toBe("number");
      expect(heartbeatCall![3]).toBe(999999);
    });

    it("writes CWD on every dispatch", () => {
      const config = makeConfig();
      dispatch("PostToolUse", makePayload(), { config });

      const cwdCall = mockedMergeKey.mock.calls.find(
        (call) => call[1] === "_cwd"
      );
      expect(cwdCall).toBeDefined();
      expect(cwdCall![0]).toBe(config.statusLine.cachePath);
      expect(cwdCall![2]).toEqual({ value: process.cwd() });
      expect(cwdCall![3]).toBe(999999);
    });

    it("writes heartbeat + CWD for SessionStart events", () => {
      const config = makeConfig({ daemon: { ...getDefaultConfig().daemon, autoStart: false } });
      dispatch("SessionStart", makePayload(), { config });

      const heartbeatCall = mockedMergeKey.mock.calls.find(
        (call) => call[1] === "_heartbeat"
      );
      const cwdCall = mockedMergeKey.mock.calls.find(
        (call) => call[1] === "_cwd"
      );
      expect(heartbeatCall).toBeDefined();
      expect(cwdCall).toBeDefined();
    });

    it("writes heartbeat + CWD for Stop events", () => {
      const config = makeConfig({ daemon: { ...getDefaultConfig().daemon, autoStart: false } });
      dispatch("Stop", makePayload(), { config });

      const heartbeatCall = mockedMergeKey.mock.calls.find(
        (call) => call[1] === "_heartbeat"
      );
      const cwdCall = mockedMergeKey.mock.calls.find(
        (call) => call[1] === "_cwd"
      );
      expect(heartbeatCall).toBeDefined();
      expect(cwdCall).toBeDefined();
    });

    it("silently swallows feed cache write errors (fail-open)", () => {
      const config = makeConfig();
      mockedMergeKey.mockImplementation(() => {
        throw new Error("Disk full");
      });

      // Should NOT throw — dispatch must always succeed
      const result = dispatch("PreToolUse", makePayload({ tool_name: "Read" }), { config });
      expect(result.exitCode).toBe(0);
    });
  });

  describe("daemon auto-start", () => {
    it("auto-starts daemon on SessionStart when autoStart is true", () => {
      const config = makeConfig({
        daemon: { ...getDefaultConfig().daemon, autoStart: true },
      });
      mockedIsRunning.mockReturnValue(false);

      dispatch("SessionStart", makePayload(), { config });

      expect(mockedIsRunning).toHaveBeenCalled();
      expect(mockedStartDaemon).toHaveBeenCalledWith(process.cwd());
    });

    it("passes projectDir to startDaemon when provided", () => {
      const config = makeConfig({
        daemon: { ...getDefaultConfig().daemon, autoStart: true },
      });
      mockedIsRunning.mockReturnValue(false);

      dispatch("SessionStart", makePayload(), { config, projectDir: "/custom/dir" });

      expect(mockedStartDaemon).toHaveBeenCalledWith("/custom/dir");
    });

    it("does NOT start daemon when autoStart is false", () => {
      const config = makeConfig({
        daemon: { ...getDefaultConfig().daemon, autoStart: false },
      });

      dispatch("SessionStart", makePayload(), { config });

      expect(mockedStartDaemon).not.toHaveBeenCalled();
    });

    it("does NOT start daemon on non-SessionStart events", () => {
      const config = makeConfig({
        daemon: { ...getDefaultConfig().daemon, autoStart: true },
      });

      const nonSessionEvents: EventType[] = [
        "PreToolUse",
        "PostToolUse",
        "Stop",
        "Notification",
        "SessionEnd",
      ];

      for (const event of nonSessionEvents) {
        dispatch(event, makePayload(), { config });
      }

      expect(mockedStartDaemon).not.toHaveBeenCalled();
    });

    it("does NOT start daemon if daemon is already running", () => {
      const config = makeConfig({
        daemon: { ...getDefaultConfig().daemon, autoStart: true },
      });
      mockedIsRunning.mockReturnValue(true);

      dispatch("SessionStart", makePayload(), { config });

      expect(mockedIsRunning).toHaveBeenCalled();
      expect(mockedStartDaemon).not.toHaveBeenCalled();
    });

    it("silently swallows daemon start errors (fail-open)", () => {
      const config = makeConfig({
        daemon: { ...getDefaultConfig().daemon, autoStart: true },
      });
      mockedIsRunning.mockReturnValue(false);
      mockedStartDaemon.mockImplementation(() => {
        throw new Error("Spawn failed");
      });

      // Should NOT throw — dispatch must always succeed
      const result = dispatch("SessionStart", makePayload(), { config });
      expect(result.exitCode).toBe(0);
    });
  });

  describe("dispatch result unaffected by feed operations", () => {
    it("context handlers still produce output when feeds are active", () => {
      const config = makeConfig();
      config.handlers = [
        {
          name: "greeter",
          type: "inline",
          events: ["SessionStart"],
          action: { additionalContext: "Welcome!" },
          phase: "context",
        },
      ];

      const result = dispatch("SessionStart", makePayload(), { config });
      expect(result.exitCode).toBe(0);
      expect(result.stdout).toBeTruthy();
      const stdout = JSON.parse(result.stdout!);
      expect(stdout.hookSpecificOutput.additionalContext).toBe("Welcome!");

      // Feed writes should still have happened
      expect(mockedMergeKey).toHaveBeenCalled();
    });

    it("guard blocks still work correctly with feed operations", () => {
      const config = makeConfig();
      config.guards = [
        {
          match: "Bash",
          action: "block",
          reason: "No bash",
        },
      ];

      const result = dispatch("PreToolUse", makePayload({ tool_name: "Bash" }), { config });
      expect(result.exitCode).toBe(0);
      const stdout = JSON.parse(result.stdout!);
      expect(stdout.hookSpecificOutput.permissionDecision).toBe("deny");

      // Feed writes still happen even when guards block
      // (heartbeat/CWD writes run before guard evaluation)
      expect(mockedMergeKey).toHaveBeenCalled();
    });
  });
});
