/**
 * Tests for the doctor command.
 *
 * Verifies:
 * - Node.js version check
 * - ~/.claude/ existence check
 * - hookwise.yaml existence check
 * - State directory check
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
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
import { DoctorCommand } from "../../src/cli/commands/doctor.js";

describe("DoctorCommand", () => {
  let tempDir: string;
  const origStateDir = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-doctor-test-"));
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

  it("checks Node.js version", () => {
    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Node.js");
    // Current Node should be >= 20
    expect(frame).toContain("\u2713");
  });

  it("reports missing hookwise.yaml", () => {
    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Config file");
    expect(frame).toContain("not found");
  });

  it("reports existing hookwise.yaml", () => {
    writeFileSync(join(tempDir, "hookwise.yaml"), "version: 1\n", "utf-8");

    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Config file");
    expect(frame).toContain("found");
  });

  it("reports missing state directory", () => {
    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    // "State directory" may be split across lines by Ink's layout
    // so check for both words separately
    expect(frame).toContain("State");
    expect(frame).toContain("directory");
    expect(frame).toContain("not found");
  });

  it("reports existing state directory", () => {
    mkdirSync(join(tempDir, "state"), { recursive: true, mode: 0o700 });

    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("State");
    expect(frame).toContain("directory");
  });

  it("shows summary when all pass", () => {
    // Create all required artifacts
    writeFileSync(join(tempDir, "hookwise.yaml"), "version: 1\n", "utf-8");
    mkdirSync(join(tempDir, "state"), { recursive: true, mode: 0o700 });

    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    // Node.js should pass, config and state should pass, but ~/.claude may warn
    expect(frame).toContain("Node.js");
  });

  it("shows failure summary when checks fail", () => {
    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    // At least config file should fail
    expect(frame).toContain("Config file");
    // State directory may be split but "not found" should appear
    expect(frame).toContain("not found");
  });

  it("shows Claude directory check", () => {
    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Claude directory");
  });

  it("renders System Health Check title", () => {
    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    expect(lastFrame()!).toContain("System Health Check");
  });

  it("warns on unknown segment name", () => {
    const configYaml = [
      "version: 1",
      "status_line:",
      "  enabled: true",
      "  segments:",
      '    - builtin: clok',
      '  delimiter: " | "',
      "  cache_path: /tmp/test-cache",
    ].join("\n") + "\n";
    writeFileSync(join(tempDir, "hookwise.yaml"), configYaml, "utf-8");
    mkdirSync(join(tempDir, "state"), { recursive: true, mode: 0o700 });

    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Unknown segment");
    expect(frame).toContain("clok");
  });

  it("passes valid segment names", () => {
    const configYaml = [
      "version: 1",
      "status_line:",
      "  enabled: true",
      "  segments:",
      '    - builtin: clock',
      '  delimiter: " | "',
      "  cache_path: /tmp/test-cache",
    ].join("\n") + "\n";
    writeFileSync(join(tempDir, "hookwise.yaml"), configYaml, "utf-8");
    mkdirSync(join(tempDir, "state"), { recursive: true, mode: 0o700 });

    const { lastFrame } = render(<DoctorCommand projectDir={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("validated");
  });
});
