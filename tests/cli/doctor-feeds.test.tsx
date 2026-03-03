/**
 * Tests for doctor command feed-related checks (Task 8.2).
 *
 * Verifies:
 * - Daemon check: warns when feeds configured but daemon not running
 * - Daemon check: passes when daemon is running
 * - Calendar check: warns when calendar enabled but no credentials
 * - Python3 check: warns when calendar enabled but python3 not found
 * - No feed checks when feeds are not enabled
 *
 * Requirements: FR-6.8, FR-11.5, FR-6.4, NFR-4
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import React from "react";
import { render } from "ink-testing-library";
import {
  mkdtempSync,
  rmSync,
  mkdirSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

// Mock daemon-manager before importing DoctorCommand
vi.mock("../../src/core/feeds/daemon-manager.js", () => ({
  isRunning: vi.fn(),
  startDaemon: vi.fn(),
  stopDaemon: vi.fn(),
  getDaemonStatus: vi.fn(),
}));

// Mock child_process.execSync for python3 check
vi.mock("node:child_process", async () => {
  const actual = await vi.importActual<typeof import("node:child_process")>("node:child_process");
  return {
    ...actual,
    execSync: vi.fn(actual.execSync),
  };
});

import { DoctorCommand } from "../../src/cli/commands/doctor.js";
import { isRunning } from "../../src/core/feeds/daemon-manager.js";
import { execSync } from "node:child_process";

const mockedIsRunning = vi.mocked(isRunning);
const mockedExecSync = vi.mocked(execSync);

describe("DoctorCommand — feed checks", () => {
  let tempDir: string;
  const origStateDir = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-doctor-feeds-test-"));
    process.env.HOOKWISE_STATE_DIR = join(tempDir, "state");
    mkdirSync(join(tempDir, "state"), { recursive: true, mode: 0o700 });
    vi.clearAllMocks();
    // Default: daemon not running
    mockedIsRunning.mockReturnValue(false);
    // Default: python3 available (pass through to real execSync for python3, mock for anything else)
    mockedExecSync.mockImplementation((cmd, opts) => {
      if (typeof cmd === "string" && cmd === "python3 --version") {
        return Buffer.from("Python 3.12.0");
      }
      throw new Error("Unexpected execSync call");
    });
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
    if (origStateDir) {
      process.env.HOOKWISE_STATE_DIR = origStateDir;
    } else {
      delete process.env.HOOKWISE_STATE_DIR;
    }
  });

  /**
   * Helper: write a hookwise.yaml config with feeds enabled.
   */
  function writeConfigWithFeeds(overrides: {
    pulseEnabled?: boolean;
    projectEnabled?: boolean;
    calendarEnabled?: boolean;
    newsEnabled?: boolean;
    insightsEnabled?: boolean;
    practiceEnabled?: boolean;
  } = {}): void {
    const {
      pulseEnabled = true,
      projectEnabled = true,
      calendarEnabled = false,
      newsEnabled = false,
      insightsEnabled = false,
      practiceEnabled = false,
    } = overrides;

    const configYaml = [
      "version: 1",
      "feeds:",
      "  pulse:",
      `    enabled: ${pulseEnabled}`,
      "    interval_seconds: 30",
      "    thresholds:",
      "      green: 0",
      "      yellow: 30",
      "      orange: 60",
      "      red: 120",
      "      skull: 180",
      "  project:",
      `    enabled: ${projectEnabled}`,
      "    interval_seconds: 60",
      "    show_branch: true",
      "    show_last_commit: true",
      "  calendar:",
      `    enabled: ${calendarEnabled}`,
      "    interval_seconds: 300",
      "    lookahead_minutes: 120",
      "    calendars:",
      "      - primary",
      "  news:",
      `    enabled: ${newsEnabled}`,
      "    source: hackernews",
      "    rss_url: null",
      "    interval_seconds: 1800",
      "    max_stories: 5",
      "    rotation_minutes: 30",
      "  insights:",
      `    enabled: ${insightsEnabled}`,
      "    interval_seconds: 120",
      "    staleness_days: 30",
      "    usage_data_path: ~/.claude/usage-data",
      "  practice:",
      `    enabled: ${practiceEnabled}`,
      "    interval_seconds: 120",
      "    db_path: ~/.practice-tracker/practice-tracker.db",
    ].join("\n") + "\n";
    writeFileSync(join(tempDir, "hookwise.yaml"), configYaml, "utf-8");
  }

  // --- Check 6: Daemon status ---

  it("warns when feeds configured but daemon not running", () => {
    writeConfigWithFeeds({ pulseEnabled: true });
    mockedIsRunning.mockReturnValue(false);

    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Feed daemon");
    expect(frame).toContain("not running");
  });

  it("passes when daemon is running", () => {
    writeConfigWithFeeds({ pulseEnabled: true });
    mockedIsRunning.mockReturnValue(true);

    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Feed daemon");
    expect(frame).toContain("Daemon is running");
  });

  it("does NOT show daemon check when no feeds are enabled", () => {
    writeConfigWithFeeds({
      pulseEnabled: false,
      projectEnabled: false,
      calendarEnabled: false,
      newsEnabled: false,
    });

    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).not.toContain("Feed daemon");
  });

  // --- Check 7: Calendar credentials ---

  it("warns when calendar enabled but no credentials", () => {
    writeConfigWithFeeds({ calendarEnabled: true });

    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Calendar");
    expect(frame).toContain("no credentials");
  });

  it("does NOT show calendar check when calendar is disabled", () => {
    writeConfigWithFeeds({ calendarEnabled: false });

    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    // Should not have calendar credential warning
    expect(frame).not.toContain("no credentials");
  });

  // --- Check 8: Python3 availability ---

  it("warns when calendar enabled but python3 not found", () => {
    writeConfigWithFeeds({ calendarEnabled: true });
    mockedExecSync.mockImplementation(() => {
      throw new Error("python3 not found");
    });

    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Python3");
    expect(frame).toContain("not found");
  });

  it("passes python3 check when python3 is available", () => {
    writeConfigWithFeeds({ calendarEnabled: true });
    mockedExecSync.mockReturnValue(Buffer.from("Python 3.12.0") as any);

    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Python3");
    expect(frame).toContain("available");
  });

  it("does NOT show python3 check when calendar is disabled", () => {
    writeConfigWithFeeds({ calendarEnabled: false });

    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).not.toContain("Python3");
  });
});
