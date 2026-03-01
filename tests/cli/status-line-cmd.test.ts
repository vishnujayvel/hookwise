/**
 * Tests for the hookwise status-line CLI command.
 *
 * Verifies the command reads stdin, merges with cache, and outputs
 * a two-tier status line. Uses process-level isolation where needed.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { writeFileSync, unlinkSync, existsSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { execSync } from "node:child_process";
import { tmpdir } from "node:os";
import { randomBytes } from "node:crypto";
import { strip } from "../../src/core/status-line/ansi.js";

function tempDir(): string {
  const dir = join(tmpdir(), `hookwise-test-${randomBytes(6).toString("hex")}`);
  mkdirSync(dir, { recursive: true });
  return dir;
}

describe("status-line CLI command - integration", () => {
  let stateDir: string;
  let cachePath: string;
  const hookwiseBin = join(process.cwd(), "dist", "bin", "hookwise.js");

  beforeEach(() => {
    stateDir = tempDir();
    cachePath = join(stateDir, "state", "status-line-cache.json");
    mkdirSync(join(stateDir, "state"), { recursive: true });
  });

  afterEach(() => {
    try {
      if (existsSync(cachePath)) unlinkSync(cachePath);
    } catch {}
  });

  it("outputs status line from piped stdin JSON", () => {
    // Write a cache file with some data
    writeFileSync(cachePath, JSON.stringify({
      builder_trap: { current_mode: "tooling" },
      mantra: { text: "Ship it" },
    }));

    const stdinJson = JSON.stringify({
      session_id: "test-session",
      context_window: { used_percentage: 67 },
      cost: { total_cost_usd: 5.23, total_duration_ms: 2_700_000 },
    });

    // Run via node directly with env override for cache path
    const result = execSync(
      `echo '${stdinJson}' | HOOKWISE_STATE_DIR="${stateDir}" node ${hookwiseBin} status-line`,
      { encoding: "utf-8", timeout: 5000 },
    );

    // Should contain context percentage
    // Strip ANSI for assertion
    const stripped = strip(result);
    expect(stripped).toContain("67%");
  });

  it("handles empty stdin gracefully", () => {
    writeFileSync(cachePath, JSON.stringify({}));

    const result = execSync(
      `echo '' | HOOKWISE_STATE_DIR="${stateDir}" node ${hookwiseBin} status-line`,
      { encoding: "utf-8", timeout: 5000 },
    );

    // Should not throw — may output empty or minimal line
    expect(typeof result).toBe("string");
  });

  it("handles missing cache file gracefully", () => {
    // Don't create cache file

    const stdinJson = JSON.stringify({
      session_id: "test",
      context_window: { used_percentage: 30 },
    });

    const result = execSync(
      `echo '${stdinJson}' | HOOKWISE_STATE_DIR="${stateDir}" node ${hookwiseBin} status-line`,
      { encoding: "utf-8", timeout: 5000 },
    );

    const stripped = strip(result);
    expect(stripped).toContain("30%");
  });
});
