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

import { execSync, spawn } from "node:child_process";
import {
  existsSync,
  openSync,
  closeSync,
  readFileSync,
  writeFileSync,
  unlinkSync,
  constants as fsConstants,
} from "node:fs";
import { join, dirname } from "node:path";
import { platform } from "node:os";
import { fileURLToPath } from "node:url";
import { ensureDir } from "./state.js";
import { logError, logDebug } from "./errors.js";
import type { TuiConfig } from "./types.js";
import { getStateDir } from "./state.js";

/**
 * Resolve the Python command for running the TUI.
 * Checks bundled venv first (relative to this module), then falls back to system python3.
 */
function resolveTuiPython(): string {
  try {
    const thisDir = dirname(fileURLToPath(import.meta.url));
    const venvPython = join(thisDir, "..", "..", "tui", ".venv", "bin", "python3");
    if (existsSync(venvPython)) {
      logDebug(`TUI using bundled venv: ${venvPython}`);
      return venvPython;
    }
  } catch {
    // Fall through to system python
  }
  logDebug("TUI using system python3");
  return "python3";
}

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
 * Atomically reserve the PID file by creating it with O_EXCL.
 * Writes a sentinel value ("0") to indicate "launching".
 * Returns true if reservation succeeded, false if file already exists
 * (another launch is in progress or a live TUI holds it).
 */
function reserveTuiPid(pidPath: string): boolean {
  ensureDir(dirname(pidPath));
  try {
    const fd = openSync(pidPath, fsConstants.O_WRONLY | fsConstants.O_CREAT | fsConstants.O_EXCL);
    writeFileSync(fd, "0", "utf-8");
    closeSync(fd);
    return true;
  } catch (err: unknown) {
    if ((err as NodeJS.ErrnoException).code === "EEXIST") {
      // Check if the existing file is from a live process
      const existingPid = readTuiPid(pidPath);
      if (existingPid !== null && existingPid > 0 && isProcessAlive(existingPid)) {
        return false; // Another TUI instance is running
      }
      // Stale PID file (dead process or sentinel "0") — remove and retry once
      try {
        unlinkSync(pidPath);
        const fd = openSync(pidPath, fsConstants.O_WRONLY | fsConstants.O_CREAT | fsConstants.O_EXCL);
        writeFileSync(fd, "0", "utf-8");
        closeSync(fd);
        return true;
      } catch {
        return false;
      }
    }
    return false;
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

    if (config.launchMethod === "newWindow") {
      // macOS: use osascript to open a new Terminal.app window running the TUI.
      // osascript is transient — its PID is not the python3 PID, so we
      // use pgrep to check if a hookwise_tui process is already running.
      try {
        const result = execSync("pgrep -f hookwise_tui", { encoding: "utf-8", timeout: 3000 });
        if (result.trim()) {
          logDebug("TUI already running (pgrep found hookwise_tui process), skipping newWindow launch");
          return false;
        }
      } catch {
        // pgrep returns exit code 1 when no process found — that's fine, proceed
      }
      const pythonCmd = resolveTuiPython();
      // Shell-quote the path with single quotes, escaping any embedded single quotes
      const shellQuoted = "'" + pythonCmd.replace(/'/g, "'\\''") + "'";
      const child = spawn(
        "osascript",
        ["-e", `tell application "Terminal" to do script "${shellQuoted} -m hookwise_tui"`],
        {
          detached: true,
          stdio: "ignore",
        },
      );
      child.unref();
      const pid = child.pid ?? null;
      if (pid !== null) {
        logDebug(`TUI launched with PID ${pid} (method: newWindow)`);
        return true;
      }
      logDebug("TUI launch failed: no PID returned from spawn");
      return false;
    }

    // Background mode: reserve PID file atomically BEFORE spawning
    // to prevent TOCTOU race where two concurrent calls both pass isTuiRunning()
    if (!reserveTuiPid(effectivePath)) {
      logDebug("TUI PID file reservation failed (another instance launching or running)");
      return false;
    }

    const bgPythonCmd = resolveTuiPython();
    const child = spawn(
      bgPythonCmd,
      ["-m", "hookwise_tui"],
      {
        detached: true,
        stdio: "ignore",
      },
    );

    const pid = child.pid ?? null;
    child.unref();

    if (pid !== null) {
      // Update sentinel with actual PID
      writeTuiPid(pid, effectivePath);
      logDebug(`TUI launched with PID ${pid} (method: background)`);
      return true;
    }

    // Spawn failed — release the reservation
    removeTuiPid(effectivePath);
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
