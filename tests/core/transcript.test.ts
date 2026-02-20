/**
 * Tests for transcript backup handler.
 *
 * Verifies:
 * - Save creates file with correct timestamp format
 * - Max size enforcement deletes oldest files first
 * - Missing backup dir is created automatically
 * - Errors return null (don't throw)
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import {
  mkdtempSync,
  existsSync,
  readFileSync,
  writeFileSync,
  readdirSync,
  rmSync,
  statSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { saveTranscript, enforceMaxSize } from "../../src/core/transcript.js";
import type { TranscriptConfig } from "../../src/core/types.js";

describe("saveTranscript", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-transcript-"));
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("saves transcript and returns file path", () => {
    const config: TranscriptConfig = {
      enabled: true,
      backupDir: tempDir,
      maxSizeMb: 100,
    };
    const payload = { session_id: "test-123", event_type: "PreToolUse" };

    const result = saveTranscript(payload, config);
    expect(result).not.toBeNull();
    expect(result!.startsWith(tempDir)).toBe(true);
    expect(result!.endsWith(".json")).toBe(true);
    expect(existsSync(result!)).toBe(true);
  });

  it("saved file contains the payload data", () => {
    const config: TranscriptConfig = {
      enabled: true,
      backupDir: tempDir,
      maxSizeMb: 100,
    };
    const payload = { session_id: "test-456", tool_name: "Bash" };

    const filePath = saveTranscript(payload, config)!;
    const content = JSON.parse(readFileSync(filePath, "utf-8"));
    expect(content).toEqual(payload);
  });

  it("timestamp format uses dashes instead of colons", () => {
    const config: TranscriptConfig = {
      enabled: true,
      backupDir: tempDir,
      maxSizeMb: 100,
    };
    const filePath = saveTranscript({ data: "test" }, config)!;

    // File name should not contain colons (filesystem-safe ISO 8601)
    const fileName = filePath.split("/").pop()!;
    expect(fileName).not.toContain(":");
    expect(fileName).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}/);
    expect(fileName.endsWith(".json")).toBe(true);
  });

  it("creates backup dir if it does not exist", () => {
    const nestedDir = join(tempDir, "nested", "backup");
    const config: TranscriptConfig = {
      enabled: true,
      backupDir: nestedDir,
      maxSizeMb: 100,
    };

    expect(existsSync(nestedDir)).toBe(false);
    const result = saveTranscript({ data: "test" }, config);
    expect(result).not.toBeNull();
    expect(existsSync(nestedDir)).toBe(true);
  });

  it("returns null on error (read-only dir simulation)", () => {
    // Use a path that's likely to fail on write
    const config: TranscriptConfig = {
      enabled: true,
      backupDir: "/dev/null/impossible-path",
      maxSizeMb: 100,
    };
    const result = saveTranscript({ data: "test" }, config);
    expect(result).toBeNull();
  });
});

describe("enforceMaxSize", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-transcript-"));
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("does not delete files when under the limit", () => {
    // Create a small file
    writeFileSync(join(tempDir, "2026-01-01T00-00-00.000Z.json"), '{"small":true}');

    const config: TranscriptConfig = {
      enabled: true,
      backupDir: tempDir,
      maxSizeMb: 100,
    };

    enforceMaxSize(config);

    const files = readdirSync(tempDir).filter((f) => f.endsWith(".json"));
    expect(files).toHaveLength(1);
  });

  it("deletes oldest files first when over the limit", () => {
    // Create files with known sizes and times
    const oldFile = join(tempDir, "2026-01-01T00-00-00.000Z.json");
    const newFile = join(tempDir, "2026-01-02T00-00-00.000Z.json");

    // Write 600KB to each file (total 1.2MB)
    const bigData = "x".repeat(600 * 1024);
    writeFileSync(oldFile, bigData);

    // Small delay to ensure different mtime
    writeFileSync(newFile, bigData);

    const config: TranscriptConfig = {
      enabled: true,
      backupDir: tempDir,
      maxSizeMb: 1, // 1MB limit — total is ~1.2MB, should delete oldest
    };

    enforceMaxSize(config);

    // Old file should be deleted, new file should remain
    expect(existsSync(oldFile)).toBe(false);
    expect(existsSync(newFile)).toBe(true);
  });

  it("deletes multiple files until under limit", () => {
    // Create 5 files, each ~300KB (total ~1.5MB, limit 0.5MB)
    for (let i = 0; i < 5; i++) {
      const path = join(tempDir, `2026-01-0${i + 1}T00-00-00.000Z.json`);
      writeFileSync(path, "x".repeat(300 * 1024));
    }

    const config: TranscriptConfig = {
      enabled: true,
      backupDir: tempDir,
      maxSizeMb: 0.5,
    };

    enforceMaxSize(config);

    const remaining = readdirSync(tempDir).filter((f) => f.endsWith(".json"));
    // At 300KB each, max 1 file fits under 500KB
    expect(remaining.length).toBeLessThanOrEqual(1);
  });

  it("does not throw on empty directory", () => {
    const config: TranscriptConfig = {
      enabled: true,
      backupDir: tempDir,
      maxSizeMb: 100,
    };

    expect(() => enforceMaxSize(config)).not.toThrow();
  });

  it("does not throw on missing directory", () => {
    const config: TranscriptConfig = {
      enabled: true,
      backupDir: join(tempDir, "nonexistent"),
      maxSizeMb: 100,
    };

    expect(() => enforceMaxSize(config)).not.toThrow();
  });

  it("ignores non-JSON files", () => {
    writeFileSync(join(tempDir, "readme.txt"), "not a transcript");
    writeFileSync(join(tempDir, "2026-01-01T00-00-00.000Z.json"), '{"data":true}');

    const config: TranscriptConfig = {
      enabled: true,
      backupDir: tempDir,
      maxSizeMb: 100,
    };

    enforceMaxSize(config);

    // Both files should remain (under limit)
    expect(existsSync(join(tempDir, "readme.txt"))).toBe(true);
    expect(existsSync(join(tempDir, "2026-01-01T00-00-00.000Z.json"))).toBe(true);
  });
});
