/**
 * Tests for the init command.
 *
 * Verifies:
 * - Generates hookwise.yaml with correct preset
 * - Creates state directory
 * - Shows success messages
 * - Handles already-existing config
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import React from "react";
import { render } from "ink-testing-library";
import {
  mkdtempSync,
  existsSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { InitCommand } from "../../src/cli/commands/init.js";

describe("InitCommand", () => {
  let tempDir: string;
  const origStateDir = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-init-test-"));
    // Redirect state dir to avoid touching real ~/.hookwise
    process.env.HOOKWISE_STATE_DIR = join(tempDir, "state");
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
    if (origStateDir) {
      process.env.HOOKWISE_STATE_DIR = origStateDir;
    } else {
      delete process.env.HOOKWISE_STATE_DIR;
    }
  });

  it("creates hookwise.yaml with minimal preset", () => {
    const { lastFrame } = render(
      <InitCommand preset="minimal" projectDir={tempDir} />
    );

    // File should be created
    const configPath = join(tempDir, "hookwise.yaml");
    expect(existsSync(configPath)).toBe(true);

    const content = readFileSync(configPath, "utf-8");
    expect(content).toContain("version: 1");
    expect(content).toContain("Preset: minimal");

    const frame = lastFrame()!;
    expect(frame).toContain("hookwise.yaml");
    expect(frame).toContain("\u2713");
  });

  it("creates hookwise.yaml with full preset", () => {
    const { lastFrame } = render(
      <InitCommand preset="full" projectDir={tempDir} />
    );

    const configPath = join(tempDir, "hookwise.yaml");
    expect(existsSync(configPath)).toBe(true);

    const content = readFileSync(configPath, "utf-8");
    expect(content).toContain("version: 1");
    expect(content).toContain("Preset: full");
  });

  it("creates hookwise.yaml with coaching preset", () => {
    render(<InitCommand preset="coaching" projectDir={tempDir} />);

    const configPath = join(tempDir, "hookwise.yaml");
    expect(existsSync(configPath)).toBe(true);

    const content = readFileSync(configPath, "utf-8");
    expect(content).toContain("Preset: coaching");
  });

  it("creates hookwise.yaml with analytics preset", () => {
    render(<InitCommand preset="analytics" projectDir={tempDir} />);

    const configPath = join(tempDir, "hookwise.yaml");
    expect(existsSync(configPath)).toBe(true);

    const content = readFileSync(configPath, "utf-8");
    expect(content).toContain("Preset: analytics");
  });

  it("creates state directory", () => {
    render(<InitCommand preset="minimal" projectDir={tempDir} />);

    const stateDir = join(tempDir, "state");
    expect(existsSync(stateDir)).toBe(true);
  });

  it("warns when hookwise.yaml already exists", () => {
    writeFileSync(join(tempDir, "hookwise.yaml"), "version: 1\n", "utf-8");

    const { lastFrame } = render(
      <InitCommand preset="minimal" projectDir={tempDir} />
    );

    const frame = lastFrame()!;
    expect(frame).toContain("already exists");
    expect(frame).toContain("\u26A0");
  });

  it("falls back to minimal for unknown preset", () => {
    render(<InitCommand preset="unknown" projectDir={tempDir} />);

    const configPath = join(tempDir, "hookwise.yaml");
    expect(existsSync(configPath)).toBe(true);

    const content = readFileSync(configPath, "utf-8");
    expect(content).toContain("Preset: minimal");
  });

  it("shows done message", () => {
    const { lastFrame } = render(
      <InitCommand preset="minimal" projectDir={tempDir} />
    );

    expect(lastFrame()!).toContain("Done!");
  });

  it("shows doctor hint", () => {
    const { lastFrame } = render(
      <InitCommand preset="minimal" projectDir={tempDir} />
    );

    expect(lastFrame()!).toContain("hookwise doctor");
  });
});
