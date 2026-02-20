/**
 * Notification sounds handler for hookwise v1.0
 *
 * Fire-and-forget sound playback using platform-native commands.
 * Uses detached spawn to avoid blocking the hook dispatch pipeline.
 * Fails silently on missing files, playback errors, or unsupported platforms.
 */

import { spawn } from "node:child_process";
import { platform } from "node:os";

/**
 * Get the platform-appropriate sound playback command.
 *
 * @returns Command and args for playback, or null on unsupported platforms
 */
export function getPlayCommand(): { command: string; args: string[] } | null {
  const os = platform();

  switch (os) {
    case "darwin":
      return { command: "afplay", args: [] };
    case "linux":
      return { command: "aplay", args: [] };
    default:
      return null;
  }
}

/**
 * Play a sound file asynchronously (fire-and-forget).
 *
 * Spawns a detached process with stdio ignored and unrefs it so
 * the Node process is not blocked waiting for playback to complete.
 *
 * Fails silently on any error: missing file, unsupported platform,
 * or spawn failure.
 *
 * @param soundPath - Absolute path to the sound file
 */
export function playSound(soundPath: string): void {
  try {
    const playCmd = getPlayCommand();
    if (!playCmd) return;

    const child = spawn(playCmd.command, [...playCmd.args, soundPath], {
      detached: true,
      stdio: "ignore",
    });

    child.unref();

    // Absorb any error event to prevent unhandled rejection
    child.on("error", () => {
      // Silently ignore playback errors
    });
  } catch {
    // Fail silently
  }
}
