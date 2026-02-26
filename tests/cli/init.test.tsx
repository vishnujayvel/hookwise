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

  it("shows error for unknown preset name", () => {
    const { lastFrame } = render(
      <InitCommand preset="unknown" projectDir={tempDir} />
    );

    const frame = lastFrame()!;
    expect(frame).toContain('Unknown preset "unknown"');
    expect(frame).toContain("Valid presets: minimal, coaching, analytics, full");
    expect(frame).toContain("\u2717"); // fail badge
  });

  it("does NOT create hookwise.yaml for invalid preset", () => {
    render(<InitCommand preset="bogus" projectDir={tempDir} />);

    const configPath = join(tempDir, "hookwise.yaml");
    expect(existsSync(configPath)).toBe(false);
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

  it("minimal preset generates concise YAML", () => {
    render(<InitCommand preset="minimal" projectDir={tempDir} />);

    const configPath = join(tempDir, "hookwise.yaml");
    const content = readFileSync(configPath, "utf-8");
    const lines = content.split("\n").filter((l) => l.length > 0);

    // Should be <= 10 non-empty lines
    expect(lines.length).toBeLessThanOrEqual(10);

    // Must contain version
    expect(content).toContain("version: 1");

    // Must NOT contain verbose sections
    expect(content).not.toContain("coaching:");
    expect(content).not.toContain("analytics:");
    expect(content).not.toContain("status_line:");
    expect(content).not.toContain("greeting:");
  });

  it("minimal preset YAML is loadable by loadConfig", async () => {
    render(<InitCommand preset="minimal" projectDir={tempDir} />);

    // Roundtrip: the generated YAML must parse successfully
    const { loadConfig } = await import("../../src/core/config.js");
    const config = loadConfig(tempDir);
    expect(config.version).toBe(1);
    expect(config.guards).toEqual([]);
  });

  // --- Feed preset tests (Task 8.2) ---

  it("full preset enables all 4 feeds", async () => {
    render(<InitCommand preset="full" projectDir={tempDir} />);

    const { loadConfig } = await import("../../src/core/config.js");
    const config = loadConfig(tempDir);
    expect(config.feeds.pulse.enabled).toBe(true);
    expect(config.feeds.project.enabled).toBe(true);
    expect(config.feeds.calendar.enabled).toBe(true);
    expect(config.feeds.news.enabled).toBe(true);
  });

  it("full preset enables daemon autoStart", async () => {
    render(<InitCommand preset="full" projectDir={tempDir} />);

    const { loadConfig } = await import("../../src/core/config.js");
    const config = loadConfig(tempDir);
    expect(config.daemon.autoStart).toBe(true);
  });

  it("minimal preset does NOT add feeds section to YAML", () => {
    render(<InitCommand preset="minimal" projectDir={tempDir} />);

    const configPath = join(tempDir, "hookwise.yaml");
    const content = readFileSync(configPath, "utf-8");
    // Minimal preset uses concise YAML, no feeds section
    expect(content).not.toContain("feeds:");
  });

  it("coaching preset does NOT enable calendar or news feeds", async () => {
    render(<InitCommand preset="coaching" projectDir={tempDir} />);

    const { loadConfig } = await import("../../src/core/config.js");
    const config = loadConfig(tempDir);
    // Calendar and news should remain at their defaults (disabled)
    expect(config.feeds.calendar.enabled).toBe(false);
    expect(config.feeds.news.enabled).toBe(false);
  });

  it("analytics preset does NOT enable calendar or news feeds", async () => {
    render(<InitCommand preset="analytics" projectDir={tempDir} />);

    const { loadConfig } = await import("../../src/core/config.js");
    const config = loadConfig(tempDir);
    expect(config.feeds.calendar.enabled).toBe(false);
    expect(config.feeds.news.enabled).toBe(false);
  });
});
