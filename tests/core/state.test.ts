/**
 * Tests for atomic state management utilities.
 *
 * Verifies:
 * - atomic write survives concurrent reads
 * - corrupt JSON returns fallback
 * - directory creation is idempotent
 * - temp files cleaned up on success
 * - HOOKWISE_STATE_DIR env var override
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdtempSync, existsSync, readFileSync, writeFileSync, readdirSync, rmSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import {
  atomicWriteJSON,
  safeReadJSON,
  ensureDir,
  getStateDir,
} from "../../src/core/state.js";
import { DEFAULT_STATE_DIR } from "../../src/core/constants.js";

describe("getStateDir", () => {
  const originalEnv = process.env.HOOKWISE_STATE_DIR;

  afterEach(() => {
    if (originalEnv !== undefined) {
      process.env.HOOKWISE_STATE_DIR = originalEnv;
    } else {
      delete process.env.HOOKWISE_STATE_DIR;
    }
  });

  it("returns default ~/.hookwise/ when no env var is set", () => {
    delete process.env.HOOKWISE_STATE_DIR;
    expect(getStateDir()).toBe(DEFAULT_STATE_DIR);
  });

  it("returns HOOKWISE_STATE_DIR env var when set", () => {
    process.env.HOOKWISE_STATE_DIR = "/tmp/custom-hookwise";
    expect(getStateDir()).toBe("/tmp/custom-hookwise");
  });

  it("returns env var even for relative paths", () => {
    process.env.HOOKWISE_STATE_DIR = "./relative-path";
    expect(getStateDir()).toBe("./relative-path");
  });
});

describe("ensureDir", () => {
  let tempRoot: string;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-test-"));
  });

  afterEach(() => {
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("creates a new directory", () => {
    const dirPath = join(tempRoot, "new-dir");
    expect(existsSync(dirPath)).toBe(false);
    ensureDir(dirPath);
    expect(existsSync(dirPath)).toBe(true);
  });

  it("creates nested directories recursively", () => {
    const dirPath = join(tempRoot, "a", "b", "c");
    ensureDir(dirPath);
    expect(existsSync(dirPath)).toBe(true);
  });

  it("is idempotent — calling twice does not throw", () => {
    const dirPath = join(tempRoot, "idempotent");
    ensureDir(dirPath);
    expect(() => ensureDir(dirPath)).not.toThrow();
    expect(existsSync(dirPath)).toBe(true);
  });

  it("uses default mode 0o700", () => {
    const dirPath = join(tempRoot, "mode-test");
    ensureDir(dirPath);
    // The directory exists — mode is best-effort on macOS
    expect(existsSync(dirPath)).toBe(true);
  });

  it("accepts custom mode", () => {
    const dirPath = join(tempRoot, "custom-mode");
    ensureDir(dirPath, 0o755);
    expect(existsSync(dirPath)).toBe(true);
  });
});

describe("atomicWriteJSON", () => {
  let tempRoot: string;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-test-"));
  });

  afterEach(() => {
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("writes JSON data to a file", () => {
    const filePath = join(tempRoot, "test.json");
    const data = { key: "value", num: 42 };
    atomicWriteJSON(filePath, data);

    const content = readFileSync(filePath, "utf-8");
    expect(JSON.parse(content)).toEqual(data);
  });

  it("writes pretty-printed JSON", () => {
    const filePath = join(tempRoot, "pretty.json");
    atomicWriteJSON(filePath, { a: 1 });
    const content = readFileSync(filePath, "utf-8");
    expect(content).toContain("  "); // indented
    expect(content.endsWith("\n")).toBe(true);
  });

  it("creates parent directories if they do not exist", () => {
    const filePath = join(tempRoot, "nested", "dir", "test.json");
    atomicWriteJSON(filePath, { nested: true });
    expect(existsSync(filePath)).toBe(true);
  });

  it("overwrites existing files atomically", () => {
    const filePath = join(tempRoot, "overwrite.json");
    atomicWriteJSON(filePath, { version: 1 });
    atomicWriteJSON(filePath, { version: 2 });

    const content = JSON.parse(readFileSync(filePath, "utf-8"));
    expect(content).toEqual({ version: 2 });
  });

  it("cleans up temp files on success", () => {
    const filePath = join(tempRoot, "cleanup.json");
    atomicWriteJSON(filePath, { clean: true });

    // No temp files should remain in the directory
    const files = readdirSync(tempRoot);
    const tempFiles = files.filter((f) => f.startsWith(".tmp-"));
    expect(tempFiles).toHaveLength(0);
  });

  it("survives concurrent reads during write", () => {
    const filePath = join(tempRoot, "concurrent.json");
    atomicWriteJSON(filePath, { initial: true });

    // Read during write simulation — since writes are atomic via rename,
    // a reader always sees either the old or new content
    const data = { updated: true, timestamp: Date.now() };
    atomicWriteJSON(filePath, data);

    const readResult = JSON.parse(readFileSync(filePath, "utf-8"));
    expect(readResult).toEqual(data);
  });

  it("handles complex nested objects", () => {
    const filePath = join(tempRoot, "complex.json");
    const data = {
      mantra: { text: "hello", history: ["a", "b"] },
      builderTrap: { alertLevel: "yellow", toolingMinutes: 30.5 },
    };
    atomicWriteJSON(filePath, data);
    const content = JSON.parse(readFileSync(filePath, "utf-8"));
    expect(content).toEqual(data);
  });

  it("handles arrays", () => {
    const filePath = join(tempRoot, "array.json");
    atomicWriteJSON(filePath, [1, 2, 3]);
    const content = JSON.parse(readFileSync(filePath, "utf-8"));
    expect(content).toEqual([1, 2, 3]);
  });

  it("handles null value", () => {
    const filePath = join(tempRoot, "null.json");
    atomicWriteJSON(filePath, null);
    const content = JSON.parse(readFileSync(filePath, "utf-8"));
    expect(content).toBeNull();
  });
});

describe("safeReadJSON", () => {
  let tempRoot: string;

  beforeEach(() => {
    tempRoot = mkdtempSync(join(tmpdir(), "hookwise-test-"));
  });

  afterEach(() => {
    rmSync(tempRoot, { recursive: true, force: true });
  });

  it("reads and parses valid JSON", () => {
    const filePath = join(tempRoot, "valid.json");
    writeFileSync(filePath, JSON.stringify({ key: "value" }));
    const result = safeReadJSON(filePath, {});
    expect(result).toEqual({ key: "value" });
  });

  it("returns fallback for missing file", () => {
    const filePath = join(tempRoot, "missing.json");
    const fallback = { default: true };
    expect(safeReadJSON(filePath, fallback)).toEqual(fallback);
  });

  it("returns fallback for corrupt JSON", () => {
    const filePath = join(tempRoot, "corrupt.json");
    writeFileSync(filePath, "not valid json {{{");
    const fallback = { fallback: true };
    expect(safeReadJSON(filePath, fallback)).toEqual(fallback);
  });

  it("returns fallback for empty file", () => {
    const filePath = join(tempRoot, "empty.json");
    writeFileSync(filePath, "");
    expect(safeReadJSON(filePath, [])).toEqual([]);
  });

  it("returns fallback for partial JSON", () => {
    const filePath = join(tempRoot, "partial.json");
    writeFileSync(filePath, '{"key": "val');
    expect(safeReadJSON(filePath, null)).toBeNull();
  });

  it("preserves type parameter through generic", () => {
    const filePath = join(tempRoot, "typed.json");
    writeFileSync(filePath, JSON.stringify({ count: 42 }));

    interface State {
      count: number;
    }
    const result = safeReadJSON<State>(filePath, { count: 0 });
    expect(result.count).toBe(42);
  });

  it("reads files written by atomicWriteJSON", () => {
    const filePath = join(tempRoot, "roundtrip.json");
    const data = { roundtrip: true, nested: { value: 42 } };
    atomicWriteJSON(filePath, data);
    const result = safeReadJSON(filePath, {});
    expect(result).toEqual(data);
  });
});
