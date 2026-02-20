/**
 * Tests for the migrate command.
 *
 * Verifies:
 * - Detects Python hookwise entries
 * - Replaces with TypeScript dispatch commands
 * - Shows before/after diff
 * - Preserves analytics.db
 * - Handles missing settings
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import React from "react";
import { render } from "ink-testing-library";
import {
  mkdtempSync,
  rmSync,
  writeFileSync,
  readFileSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { MigrateCommand } from "../../src/cli/commands/migrate.js";

describe("MigrateCommand", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-migrate-test-"));
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("shows error for missing settings file", () => {
    const settingsPath = join(tempDir, "nonexistent.json");
    const { lastFrame } = render(
      <MigrateCommand settingsPath={settingsPath} />
    );
    expect(lastFrame()!).toContain("not found");
  });

  it("reports no changes when no hooks section exists", () => {
    const settingsPath = join(tempDir, "settings.json");
    writeFileSync(settingsPath, JSON.stringify({ test: true }), "utf-8");

    const { lastFrame } = render(
      <MigrateCommand settingsPath={settingsPath} />
    );
    expect(lastFrame()!).toContain("Nothing to migrate");
  });

  it("reports no changes when no Python hookwise entries exist", () => {
    const settingsPath = join(tempDir, "settings.json");
    writeFileSync(
      settingsPath,
      JSON.stringify({
        hooks: {
          PreToolUse: [
            { type: "command", command: "hookwise dispatch PreToolUse" },
          ],
        },
      }),
      "utf-8"
    );

    const { lastFrame } = render(
      <MigrateCommand settingsPath={settingsPath} />
    );
    expect(lastFrame()!).toContain("Nothing to migrate");
  });

  it("detects and replaces uv run hookwise entries", () => {
    const settingsPath = join(tempDir, "settings.json");
    writeFileSync(
      settingsPath,
      JSON.stringify({
        hooks: {
          PreToolUse: [
            {
              type: "command",
              command: "uv run hookwise dispatch PreToolUse",
            },
          ],
        },
      }),
      "utf-8"
    );

    const { lastFrame } = render(
      <MigrateCommand settingsPath={settingsPath} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("uv run hookwise");
    expect(frame).toContain("hookwise dispatch");
    expect(frame).toContain("migrated");
  });

  it("detects python -m hookwise entries", () => {
    const settingsPath = join(tempDir, "settings.json");
    writeFileSync(
      settingsPath,
      JSON.stringify({
        hooks: {
          PostToolUse: [
            {
              type: "command",
              command: "python -m hookwise dispatch PostToolUse",
            },
          ],
        },
      }),
      "utf-8"
    );

    const { lastFrame } = render(
      <MigrateCommand settingsPath={settingsPath} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("python -m hookwise");
    expect(frame).toContain("migrated");
  });

  it("writes updated settings when not dry-run", () => {
    const settingsPath = join(tempDir, "settings.json");
    writeFileSync(
      settingsPath,
      JSON.stringify({
        hooks: {
          PreToolUse: [
            {
              type: "command",
              command: "uv run hookwise dispatch PreToolUse",
            },
          ],
        },
      }),
      "utf-8"
    );

    render(<MigrateCommand settingsPath={settingsPath} />);

    const updated = JSON.parse(readFileSync(settingsPath, "utf-8"));
    expect(updated.hooks.PreToolUse[0].command).toBe(
      "hookwise dispatch PreToolUse"
    );
  });

  it("does not write in dry-run mode", () => {
    const settingsPath = join(tempDir, "settings.json");
    const original = JSON.stringify({
      hooks: {
        PreToolUse: [
          {
            type: "command",
            command: "uv run hookwise dispatch PreToolUse",
          },
        ],
      },
    });
    writeFileSync(settingsPath, original, "utf-8");

    const { lastFrame } = render(
      <MigrateCommand settingsPath={settingsPath} dryRun={true} />
    );

    // File should not be modified
    expect(readFileSync(settingsPath, "utf-8")).toBe(original);
    expect(lastFrame()!).toContain("dry run");
  });

  it("shows analytics preserved message", () => {
    const settingsPath = join(tempDir, "settings.json");
    writeFileSync(
      settingsPath,
      JSON.stringify({
        hooks: {
          PreToolUse: [
            {
              type: "command",
              command: "uv run hookwise dispatch PreToolUse",
            },
          ],
        },
      }),
      "utf-8"
    );

    const { lastFrame } = render(
      <MigrateCommand settingsPath={settingsPath} />
    );
    expect(lastFrame()!).toContain("analytics.db preserved");
  });

  it("migrates multiple hooks", () => {
    const settingsPath = join(tempDir, "settings.json");
    writeFileSync(
      settingsPath,
      JSON.stringify({
        hooks: {
          PreToolUse: [
            {
              type: "command",
              command: "uv run hookwise dispatch PreToolUse",
            },
          ],
          PostToolUse: [
            {
              type: "command",
              command: "python3 -m hookwise dispatch PostToolUse",
            },
          ],
        },
      }),
      "utf-8"
    );

    const { lastFrame } = render(
      <MigrateCommand settingsPath={settingsPath} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("2 hook(s) migrated");
  });
});
