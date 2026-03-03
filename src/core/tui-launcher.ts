/**
 * TUI auto-launch utility for hookwise.
 *
 * Manages launching the Python-based Textual TUI app as a detached process.
 * Uses a PID file to prevent duplicate instances.
 *
 * Fail-open: All errors are caught and logged, never thrown (ARCH-3).
 * Cross-platform: macOS (Darwin) is the primary target; other platforms
 * log a warning and skip the launch.
 */

import { spawn } from "node:child_process";
import {
  existsSync,
  readFileSync,
  writeFileSync,
  unlinkSync,
} from "node:fs";
import { join, dirname } from "node:path";
import { platform } from "node:os";
import { ensureDir } from "./state.js";
import { logError, logDebug } from "./errors.js";
import type { TuiConfig } from "./types.js";
import { getStateDir } from "./state.js";

/** Default TUI PID file path: ~/.hookwise/tui.pid */
function getDefaultTuiPidPath(): string {
  return join(getStateDir(), "tui.pid");
}

// --- Internal helpers ---

/**
 * Read the PID from the TUI PID file.
 * Returns null if the file does not exist or contains invalid content.
 */
function readTuiPid(pidPath: string): number | null {
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
 * Write a PID to the TUI PID file.
 * Uses O_EXCL flag to prevent TOCTOU races.
 * If the file already exists (EEXIST), re-checks process liveness before overwriting.
 */
function writeTuiPid(pid: number, pidPath: string): void {
  ensureDir(dirname(pidPath));
  try {
    writeFileSync(pidPath, String(pid), { encoding: "utf-8", flag: "wx" });
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "EEXIST") {
      const existingPid = readTuiPid(pidPath);
      if (existingPid !== null && isProcessAlive(existingPid)) {
        return; // Another TUI instance is running
      }
      // Stale PID file -- safe to overwrite
      writeFileSync(pidPath, String(pid), "utf-8");
    } else {
      throw err;
    }
  }
}

/**
 * Remove the TUI PID file if it exists.
 */
function removeTuiPid(pidPath: string): void {
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
 * Uses signal 0 (existence check).
 */
function isProcessAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

// --- Public API ---

/**
 * Check if the TUI is currently running.
 *
 * Reads the PID file and checks process liveness via signal 0.
 * Cleans up stale PID files (process dead but PID file exists).
 *
 * @param pidPath - Path to the TUI PID file (default: ~/.hookwise/tui.pid)
 */
export function isTuiRunning(pidPath?: string): boolean {
  const effectivePath = pidPath ?? getDefaultTuiPidPath();
  const pid = readTuiPid(effectivePath);
  if (pid === null) return false;

  if (isProcessAlive(pid)) {
    return true;
  }

  // Stale PID file -- process is dead but file remains
  removeTuiPid(effectivePath);
  return false;
}

/**
 * Launch the TUI as a detached process.
 *
 * - Checks if TUI is already running (duplicate prevention)
 * - On macOS: spawns via `open -a Terminal.app` for newWindow, or detached for background
 * - Writes PID to ~/.hookwise/tui.pid
 * - Fail-open: all errors caught and logged, never thrown
 *
 * @param config - TUI configuration
 * @param pidPath - Path to the TUI PID file (default: ~/.hookwise/tui.pid)
 * @returns true if the TUI was launched, false otherwise
 */
export function launchTui(config: TuiConfig, pidPath?: string): boolean {
  const effectivePath = pidPath ?? getDefaultTuiPidPath();

  try {
    // Duplicate prevention
    if (isTuiRunning(effectivePath)) {
      logDebug("TUI already running, skipping launch");
      return false;
    }

    const currentPlatform = platform();

    // Cross-platform: only macOS is supported
    if (currentPlatform !== "darwin") {
      logDebug(`TUI auto-launch not supported on platform: ${currentPlatform}`);
      return false;
    }

    let child;

    if (config.launchMethod === "newWindow") {
      // macOS: use osascript to open a new Terminal.app window running the TUI.
      // Note: osascript is transient — its PID is not the python3 PID, so we
      // skip PID-based duplicate prevention for newWindow mode.
      child = spawn(
        "osascript",
        ["-e", 'tell application "Terminal" to do script "python3 -m hookwise_tui"'],
        {
          detached: true,
          stdio: "ignore",
        },
      );
    } else {
      // background: spawn detached python process directly
      child = spawn(
        "python3",
        ["-m", "hookwise_tui"],
        {
          detached: true,
          stdio: "ignore",
        },
      );
    }

    const pid = child.pid ?? null;

    // Unref so the parent process can exit without waiting
    child.unref();

    if (pid !== null) {
      // Only write PID for background mode — newWindow uses a transient
      // osascript process whose PID goes stale immediately.
      if (config.launchMethod !== "newWindow") {
        writeTuiPid(pid, effectivePath);
      }
      logDebug(`TUI launched with PID ${pid} (method: ${config.launchMethod})`);
      return true;
    }

    logDebug("TUI launch failed: no PID returned from spawn");
    return false;
  } catch (error) {
    // ARCH-3: Fail-open -- TUI launch failure must never affect dispatch
    logError(
      error instanceof Error ? error : new Error(String(error)),
      { context: "launchTui" },
    );
    return false;
  }
}
