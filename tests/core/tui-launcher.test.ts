/**
 * Tests for the TUI auto-launcher utility.
 *
 * Covers:
 * - PID file creation/reading
 * - isTuiRunning() returns false when no PID file
 * - isTuiRunning() returns false when PID file has dead process
 * - launchTui() creates PID file on success
 * - launchTui() skips if TUI already running (duplicate prevention)
 * - launchTui() returns false on unsupported platforms
 * - Fail-open: errors are caught and logged, never thrown
 * - Dispatcher integration: SessionStart with tui.autoLaunch doesn't crash
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  mkdtempSync,
  writeFileSync,
  readFileSync,
  rmSync,
  existsSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

// Mock child_process before importing the module under test
vi.mock("node:child_process", () => ({
  spawn: vi.fn(),
  spawnSync: vi.fn(),
  execSync: vi.fn(() => { throw new Error("no process found"); }),
}));

// Mock node:os platform() so we can control cross-platform behavior
vi.mock("node:os", async () => {
  const actual = await vi.importActual<typeof import("node:os")>("node:os");
  return {
    ...actual,
    platform: vi.fn(() => "darwin"),
  };
});

import { isTuiRunning, launchTui } from "../../src/core/tui-launcher.js";
import { spawn, execSync } from "node:child_process";
import { platform } from "node:os";
import { dispatch } from "../../src/core/dispatcher.js";
import { getDefaultConfig } from "../../src/core/config.js";
import type { TuiConfig, HookPayload } from "../../src/core/types.js";

const mockedSpawn = vi.mocked(spawn);
const mockedPlatform = vi.mocked(platform);
const mockedExecSync = vi.mocked(execSync);

function makePayload(overrides: Partial<HookPayload> = {}): HookPayload {
  return {
    session_id: "test-session-tui",
    ...overrides,
  };
}

describe("isTuiRunning", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-tui-test-"));
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("returns false when no PID file exists", () => {
    const pidPath = join(tempDir, "tui.pid");
    expect(isTuiRunning(pidPath)).toBe(false);
  });

  it("returns false when PID file has dead process", () => {
    const pidPath = join(tempDir, "tui.pid");
    // Use a PID that is very unlikely to exist (max PID)
    writeFileSync(pidPath, "999999999", "utf-8");
    expect(isTuiRunning(pidPath)).toBe(false);
  });

  it("cleans up stale PID file when process is dead", () => {
    const pidPath = join(tempDir, "tui.pid");
    writeFileSync(pidPath, "999999999", "utf-8");

    expect(isTuiRunning(pidPath)).toBe(false);
    // PID file should have been cleaned up
    expect(existsSync(pidPath)).toBe(false);
  });

  it("returns true when PID file points to a live process", () => {
    const pidPath = join(tempDir, "tui.pid");
    // Use the current process PID (guaranteed alive)
    writeFileSync(pidPath, String(process.pid), "utf-8");
    expect(isTuiRunning(pidPath)).toBe(true);
  });

  it("returns false when PID file contains invalid content", () => {
    const pidPath = join(tempDir, "tui.pid");
    writeFileSync(pidPath, "not-a-number", "utf-8");
    expect(isTuiRunning(pidPath)).toBe(false);
  });

  it("returns false when PID file is empty", () => {
    const pidPath = join(tempDir, "tui.pid");
    writeFileSync(pidPath, "", "utf-8");
    expect(isTuiRunning(pidPath)).toBe(false);
  });
});

describe("launchTui", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-tui-launch-"));
    vi.clearAllMocks();
    mockedPlatform.mockReturnValue("darwin");
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("creates PID file on successful launch", () => {
    const pidPath = join(tempDir, "tui.pid");
    const config: TuiConfig = { autoLaunch: true, launchMethod: "background" };

    // Mock spawn to return a fake child process
    const fakeChild = {
      pid: 12345,
      unref: vi.fn(),
    };
    mockedSpawn.mockReturnValue(fakeChild as unknown as ReturnType<typeof spawn>);

    const result = launchTui(config, pidPath);

    expect(result).toBe(true);
    expect(existsSync(pidPath)).toBe(true);
    const writtenPid = readFileSync(pidPath, "utf-8").trim();
    expect(writtenPid).toBe("12345");
  });

  it("skips launch if TUI is already running (duplicate prevention)", () => {
    const pidPath = join(tempDir, "tui.pid");
    // Write a PID file with the current process PID (alive)
    writeFileSync(pidPath, String(process.pid), "utf-8");

    const config: TuiConfig = { autoLaunch: true, launchMethod: "background" };
    const result = launchTui(config, pidPath);

    expect(result).toBe(false);
    // spawn should NOT have been called
    expect(mockedSpawn).not.toHaveBeenCalled();
  });

  it("returns false on unsupported platform", () => {
    const pidPath = join(tempDir, "tui.pid");
    const config: TuiConfig = { autoLaunch: true, launchMethod: "background" };

    mockedPlatform.mockReturnValue("linux");

    const result = launchTui(config, pidPath);

    expect(result).toBe(false);
    expect(mockedSpawn).not.toHaveBeenCalled();
  });

  it("spawns with correct args for newWindow method on macOS", () => {
    const pidPath = join(tempDir, "tui.pid");
    const config: TuiConfig = { autoLaunch: true, launchMethod: "newWindow" };

    const fakeChild = {
      pid: 54321,
      unref: vi.fn(),
    };
    mockedSpawn.mockReturnValue(fakeChild as unknown as ReturnType<typeof spawn>);

    launchTui(config, pidPath);

    expect(mockedSpawn).toHaveBeenCalledWith(
      "osascript",
      ["-e", expect.stringContaining("-m hookwise_tui")],
      expect.objectContaining({ detached: true, stdio: "ignore" }),
    );
  });

  it("does not write PID file for newWindow method (transient osascript PID)", () => {
    const pidPath = join(tempDir, "tui.pid");
    const config: TuiConfig = { autoLaunch: true, launchMethod: "newWindow" };

    const fakeChild = {
      pid: 54321,
      unref: vi.fn(),
    };
    mockedSpawn.mockReturnValue(fakeChild as unknown as ReturnType<typeof spawn>);

    const result = launchTui(config, pidPath);

    expect(result).toBe(true);
    // No PID file for newWindow — osascript PID is transient
    expect(existsSync(pidPath)).toBe(false);
  });

  it("skips newWindow launch when pgrep finds existing hookwise_tui process", () => {
    const pidPath = join(tempDir, "tui.pid");
    const config: TuiConfig = { autoLaunch: true, launchMethod: "newWindow" };

    // pgrep returns a PID (process found) — one-shot to avoid shared-state leak
    mockedExecSync.mockReturnValueOnce("12345\n");

    const result = launchTui(config, pidPath);

    expect(result).toBe(false);
    expect(mockedSpawn).not.toHaveBeenCalled();
  });

  it("spawns with correct args for background method", () => {
    const pidPath = join(tempDir, "tui.pid");
    const config: TuiConfig = { autoLaunch: true, launchMethod: "background" };

    const fakeChild = {
      pid: 67890,
      unref: vi.fn(),
    };
    mockedSpawn.mockReturnValue(fakeChild as unknown as ReturnType<typeof spawn>);

    launchTui(config, pidPath);

    expect(mockedSpawn).toHaveBeenCalledWith(
      expect.stringContaining("python3"),
      ["-m", "hookwise_tui"],
      expect.objectContaining({ detached: true, stdio: "ignore" }),
    );
  });

  it("calls unref() on the child process to allow parent exit", () => {
    const pidPath = join(tempDir, "tui.pid");
    const config: TuiConfig = { autoLaunch: true, launchMethod: "background" };

    const fakeChild = {
      pid: 11111,
      unref: vi.fn(),
    };
    mockedSpawn.mockReturnValue(fakeChild as unknown as ReturnType<typeof spawn>);

    launchTui(config, pidPath);

    expect(fakeChild.unref).toHaveBeenCalled();
  });

  it("returns false when spawn returns no PID", () => {
    const pidPath = join(tempDir, "tui.pid");
    const config: TuiConfig = { autoLaunch: true, launchMethod: "background" };

    const fakeChild = {
      pid: undefined,
      unref: vi.fn(),
    };
    mockedSpawn.mockReturnValue(fakeChild as unknown as ReturnType<typeof spawn>);

    const result = launchTui(config, pidPath);

    expect(result).toBe(false);
    // Should not have created a PID file
    expect(existsSync(pidPath)).toBe(false);
  });

  it("fail-open: spawn error does not throw", () => {
    const pidPath = join(tempDir, "tui.pid");
    const config: TuiConfig = { autoLaunch: true, launchMethod: "background" };

    mockedSpawn.mockImplementation(() => {
      throw new Error("spawn ENOENT");
    });

    // Should NOT throw
    const result = launchTui(config, pidPath);
    expect(result).toBe(false);
  });

  it("overwrites stale PID file and launches new instance", () => {
    const pidPath = join(tempDir, "tui.pid");
    // Write a stale PID (dead process)
    writeFileSync(pidPath, "999999999", "utf-8");

    const config: TuiConfig = { autoLaunch: true, launchMethod: "background" };
    const fakeChild = {
      pid: 22222,
      unref: vi.fn(),
    };
    mockedSpawn.mockReturnValue(fakeChild as unknown as ReturnType<typeof spawn>);

    const result = launchTui(config, pidPath);

    expect(result).toBe(true);
    const writtenPid = readFileSync(pidPath, "utf-8").trim();
    expect(writtenPid).toBe("22222");
  });
});

describe("dispatch — TUI auto-launch integration", () => {
  it("SessionStart with tui.autoLaunch:true does not crash (fail-open)", () => {
    const config = getDefaultConfig();
    config.tui = { autoLaunch: true, launchMethod: "newWindow" };

    // spawn is mocked, so it won't actually launch anything
    const fakeChild = {
      pid: 33333,
      unref: vi.fn(),
    };
    mockedSpawn.mockReturnValue(fakeChild as unknown as ReturnType<typeof spawn>);

    const result = dispatch("SessionStart", makePayload(), { config });

    // Must not crash — fail-open
    expect(result.exitCode).toBe(0);
  });

  it("SessionStart with tui.autoLaunch:false does not call launchTui", () => {
    const config = getDefaultConfig();
    config.tui = { autoLaunch: false, launchMethod: "newWindow" };

    mockedSpawn.mockClear();

    const result = dispatch("SessionStart", makePayload(), { config });

    expect(result.exitCode).toBe(0);
    // spawn should not have been called for TUI (daemon may still call it)
    // We check that no spawn was called with osascript or python3 -m for TUI
    const tuiSpawnCalls = mockedSpawn.mock.calls.filter(
      (call) => call[0] === "osascript" || (call[0] === "python3" && call[1]?.includes("-m")),
    );
    expect(tuiSpawnCalls.length).toBe(0);
  });

  it("non-SessionStart events do not trigger TUI launch", () => {
    const config = getDefaultConfig();
    config.tui = { autoLaunch: true, launchMethod: "background" };

    mockedSpawn.mockClear();

    const result = dispatch("PreToolUse", makePayload(), { config });

    expect(result.exitCode).toBe(0);
    // No TUI-related spawn calls for non-SessionStart events
    const tuiSpawnCalls = mockedSpawn.mock.calls.filter(
      (call) => call[0] === "python3" && call[1]?.includes("-m"),
    );
    expect(tuiSpawnCalls.length).toBe(0);
  });
});
