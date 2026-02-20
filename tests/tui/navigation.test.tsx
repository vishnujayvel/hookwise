/**
 * Tests for TUI shell navigation.
 *
 * Verifies:
 * - Tab switching with number keys
 * - Tab bar rendering
 * - Active tab content rendering
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import React from "react";
import { render } from "ink-testing-library";
import {
  mkdtempSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { TuiApp } from "../../src/cli/tui/app.js";

/** Wait for React state update to propagate */
function delay(ms: number = 50): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

describe("TUI Navigation", () => {
  let tempDir: string;
  const origStateDir = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-tui-test-"));
    process.env.HOOKWISE_STATE_DIR = join(tempDir, "state");
    // Create a minimal config
    writeFileSync(
      join(tempDir, "hookwise.yaml"),
      "version: 1\nguards: []\n",
      "utf-8"
    );
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
    if (origStateDir) {
      process.env.HOOKWISE_STATE_DIR = origStateDir;
    } else {
      delete process.env.HOOKWISE_STATE_DIR;
    }
  });

  it("renders tab bar", () => {
    const { lastFrame } = render(<TuiApp configPath={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Dashboard");
    expect(frame).toContain("Guards");
    expect(frame).toContain("Coaching");
    expect(frame).toContain("Analytics");
    expect(frame).toContain("Recipes");
    expect(frame).toContain("Status");
  });

  it("renders hookwise TUI title", () => {
    const { lastFrame } = render(<TuiApp configPath={tempDir} />);
    expect(lastFrame()!).toContain("hookwise TUI");
  });

  it("starts on Dashboard tab", () => {
    const { lastFrame } = render(<TuiApp configPath={tempDir} />);
    const frame = lastFrame()!;
    // Dashboard content should be visible
    expect(frame).toContain("Dashboard");
  });

  it("switches to Guards tab on key 2", async () => {
    const { lastFrame, stdin } = render(<TuiApp configPath={tempDir} />);
    stdin.write("2");
    await delay();
    const frame = lastFrame()!;
    expect(frame).toContain("Guard Rules");
  });

  it("switches to Coaching tab on key 3", async () => {
    const { lastFrame, stdin } = render(<TuiApp configPath={tempDir} />);
    stdin.write("3");
    await delay();
    const frame = lastFrame()!;
    expect(frame).toContain("Coaching Configuration");
  });

  it("switches to Analytics tab on key 4", async () => {
    const { lastFrame, stdin } = render(<TuiApp configPath={tempDir} />);
    stdin.write("4");
    await delay();
    const frame = lastFrame()!;
    expect(frame).toContain("Analytics");
  });

  it("switches to Recipes tab on key 5", async () => {
    const { lastFrame, stdin } = render(<TuiApp configPath={tempDir} />);
    stdin.write("5");
    await delay();
    const frame = lastFrame()!;
    expect(frame).toContain("Recipes");
  });

  it("switches to Status tab on key 6", async () => {
    const { lastFrame, stdin } = render(<TuiApp configPath={tempDir} />);
    stdin.write("6");
    await delay();
    const frame = lastFrame()!;
    expect(frame).toContain("Status Line Preview");
  });

  it("shows help bar with shortcuts", () => {
    const { lastFrame } = render(<TuiApp configPath={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("1-6");
    expect(frame).toContain("quit");
  });

  it("can switch between tabs", async () => {
    const { lastFrame, stdin } = render(<TuiApp configPath={tempDir} />);

    // Start on Dashboard
    expect(lastFrame()!).toContain("Dashboard");

    // Switch to Guards
    stdin.write("2");
    await delay();
    expect(lastFrame()!).toContain("Guard Rules");

    // Switch back to Dashboard
    stdin.write("1");
    await delay();
    expect(lastFrame()!).toContain("Dashboard");
  });
});
