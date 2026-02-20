/**
 * Tests for the status command.
 *
 * Verifies:
 * - Rendering with default config (all features disabled)
 * - Rendering with all features enabled
 * - Guard count, coaching status, handler counts render correctly
 * - Error handling (missing/invalid config)
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
import { StatusCommand } from "../../src/cli/commands/status.js";

describe("StatusCommand", () => {
  let tempDir: string;
  const origStateDir = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-status-test-"));
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

  it("renders with default config (all features disabled)", () => {
    // Write a minimal valid config
    writeFileSync(
      join(tempDir, "hookwise.yaml"),
      "version: 1\n",
      "utf-8"
    );

    const { lastFrame } = render(<StatusCommand configPath={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Configuration Status");
    expect(frame).toContain("Guards");
    expect(frame).toContain("0 rule(s)");
    expect(frame).toContain("disabled");
  });

  it("renders with features enabled", () => {
    const config = `
version: 1
analytics:
  enabled: true
greeting:
  enabled: true
sounds:
  enabled: true
status_line:
  enabled: true
  segments: []
  delimiter: " | "
cost_tracking:
  enabled: true
  daily_budget: 10
  enforcement: warn
transcript_backup:
  enabled: true
  backup_dir: /tmp/transcripts
  max_size_mb: 100
`;
    writeFileSync(join(tempDir, "hookwise.yaml"), config, "utf-8");

    const { lastFrame } = render(<StatusCommand configPath={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Configuration Status");
    expect(frame).toContain("enabled");
  });

  it("renders guard count correctly", () => {
    const config = `
version: 1
guards:
  - match: "Bash"
    action: block
    reason: "Block Bash"
  - match: "mcp__gmail__*"
    action: warn
    reason: "Email warning"
`;
    writeFileSync(join(tempDir, "hookwise.yaml"), config, "utf-8");

    const { lastFrame } = render(<StatusCommand configPath={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("2 rule(s)");
  });

  it("renders coaching status", () => {
    const config = `
version: 1
coaching:
  metacognition:
    enabled: true
    interval_seconds: 300
  builder_trap:
    enabled: false
    thresholds:
      yellow: 30
      orange: 60
      red: 90
  communication:
    enabled: true
    frequency: 3
    min_length: 50
    tone: gentle
`;
    writeFileSync(join(tempDir, "hookwise.yaml"), config, "utf-8");

    const { lastFrame } = render(<StatusCommand configPath={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Metacognition");
    expect(frame).toContain("Communication");
  });

  it("renders handler count", () => {
    const config = `
version: 1
handlers:
  - name: my-handler
    type: builtin
    events:
      - PreToolUse
`;
    writeFileSync(join(tempDir, "hookwise.yaml"), config, "utf-8");

    const { lastFrame } = render(<StatusCommand configPath={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("1 custom handler(s)");
  });

  it("shows error when config is invalid YAML", () => {
    writeFileSync(
      join(tempDir, "hookwise.yaml"),
      "{{invalid yaml content",
      "utf-8"
    );

    const { lastFrame } = render(<StatusCommand configPath={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Error loading config");
  });

  it("renders with no config file (defaults)", () => {
    // No hookwise.yaml in tempDir — loadConfig should return defaults
    const { lastFrame } = render(<StatusCommand configPath={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Configuration Status");
    expect(frame).toContain("0 rule(s)");
  });

  it("renders Features section", () => {
    writeFileSync(
      join(tempDir, "hookwise.yaml"),
      "version: 1\n",
      "utf-8"
    );

    const { lastFrame } = render(<StatusCommand configPath={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Features");
    expect(frame).toContain("Analytics");
    expect(frame).toContain("Greeting");
    expect(frame).toContain("Sounds");
  });

  it("renders Handlers section", () => {
    writeFileSync(
      join(tempDir, "hookwise.yaml"),
      "version: 1\n",
      "utf-8"
    );

    const { lastFrame } = render(<StatusCommand configPath={tempDir} />);
    const frame = lastFrame()!;
    expect(frame).toContain("Handlers");
    expect(frame).toContain("0 custom handler(s)");
  });
});
