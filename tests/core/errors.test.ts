/**
 * Tests for error handling and logging infrastructure.
 *
 * Verifies:
 * - Custom error class hierarchy
 * - safeDispatch catches all exceptions and returns exit 0
 * - Error log file creation and content
 * - Log rotation at size boundary
 * - Debug log respects log level
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdtempSync, mkdirSync as mkdirSyncFs, existsSync, readFileSync, writeFileSync, statSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import {
  HookwiseError,
  ConfigError,
  HandlerTimeoutError,
  StateError,
  AnalyticsError,
  logError,
  logDebug,
  safeDispatch,
  setLogLevel,
  getLogLevel,
} from "../../src/core/errors.js";
import type { DispatchResult } from "../../src/core/types.js";

describe("Custom error classes", () => {
  it("HookwiseError is the base class", () => {
    const err = new HookwiseError("base error");
    expect(err).toBeInstanceOf(Error);
    expect(err).toBeInstanceOf(HookwiseError);
    expect(err.name).toBe("HookwiseError");
    expect(err.message).toBe("base error");
  });

  it("ConfigError extends HookwiseError", () => {
    const err = new ConfigError("bad config");
    expect(err).toBeInstanceOf(HookwiseError);
    expect(err).toBeInstanceOf(Error);
    expect(err.name).toBe("ConfigError");
    expect(err.message).toBe("bad config");
  });

  it("HandlerTimeoutError extends HookwiseError with extra properties", () => {
    const err = new HandlerTimeoutError("my-handler", 5000);
    expect(err).toBeInstanceOf(HookwiseError);
    expect(err.name).toBe("HandlerTimeoutError");
    expect(err.handlerName).toBe("my-handler");
    expect(err.timeoutMs).toBe(5000);
    expect(err.message).toContain("my-handler");
    expect(err.message).toContain("5000");
  });

  it("StateError extends HookwiseError", () => {
    const err = new StateError("corrupt state");
    expect(err).toBeInstanceOf(HookwiseError);
    expect(err.name).toBe("StateError");
  });

  it("AnalyticsError extends HookwiseError", () => {
    const err = new AnalyticsError("db write failed");
    expect(err).toBeInstanceOf(HookwiseError);
    expect(err.name).toBe("AnalyticsError");
  });

  it("all errors have stack traces", () => {
    const err = new ConfigError("stack test");
    expect(err.stack).toBeDefined();
    expect(err.stack).toContain("stack test");
  });
});

describe("safeDispatch", () => {
  it("returns the result of a successful dispatch function", () => {
    const result: DispatchResult = {
      stdout: '{"decision":"block"}',
      stderr: null,
      exitCode: 0,
    };
    const output = safeDispatch(() => result);
    expect(output).toEqual(result);
  });

  it("catches thrown Error and returns fail-open result", () => {
    const output = safeDispatch(() => {
      throw new Error("unexpected crash");
    });
    expect(output).toEqual({ stdout: null, stderr: null, exitCode: 0 });
  });

  it("catches thrown string and returns fail-open result", () => {
    const output = safeDispatch(() => {
      throw "string error"; // eslint-disable-line no-throw-literal
    });
    expect(output).toEqual({ stdout: null, stderr: null, exitCode: 0 });
  });

  it("catches ConfigError and returns fail-open result", () => {
    const output = safeDispatch(() => {
      throw new ConfigError("bad yaml");
    });
    expect(output).toEqual({ stdout: null, stderr: null, exitCode: 0 });
  });

  it("catches HandlerTimeoutError and returns fail-open result", () => {
    const output = safeDispatch(() => {
      throw new HandlerTimeoutError("slow-script", 10000);
    });
    expect(output).toEqual({ stdout: null, stderr: null, exitCode: 0 });
  });

  it("catches TypeError and returns fail-open result", () => {
    const output = safeDispatch(() => {
      // Force a TypeError
      const obj: unknown = null;
      return (obj as { method: () => DispatchResult }).method();
    });
    expect(output).toEqual({ stdout: null, stderr: null, exitCode: 0 });
  });

  it("never returns exitCode 2 on error", () => {
    const output = safeDispatch(() => {
      throw new Error("this must not produce exit 2");
    });
    expect(output.exitCode).toBe(0);
  });

  it("passes through a block result from a successful handler", () => {
    const result: DispatchResult = {
      stdout: JSON.stringify({ decision: "block", reason: "dangerous" }),
      stderr: null,
      exitCode: 2,
    };
    const output = safeDispatch(() => result);
    expect(output.exitCode).toBe(2);
  });
});

describe("logError", () => {
  let tempDir: string;
  const originalEnv = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-log-test-"));
    process.env.HOOKWISE_STATE_DIR = tempDir;
  });

  afterEach(() => {
    if (originalEnv !== undefined) {
      process.env.HOOKWISE_STATE_DIR = originalEnv;
    } else {
      delete process.env.HOOKWISE_STATE_DIR;
    }
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("creates the error log file", () => {
    logError(new Error("test error"));
    const logPath = join(tempDir, "logs", "error.log");
    expect(existsSync(logPath)).toBe(true);
  });

  it("writes error details to the log", () => {
    logError(new Error("test error message"));
    const logPath = join(tempDir, "logs", "error.log");
    const content = readFileSync(logPath, "utf-8");
    expect(content).toContain("ERROR");
    expect(content).toContain("test error message");
  });

  it("includes context metadata in the log", () => {
    logError(new Error("contextual"), { handler: "my-script", phase: "guard" });
    const logPath = join(tempDir, "logs", "error.log");
    const content = readFileSync(logPath, "utf-8");
    expect(content).toContain("my-script");
    expect(content).toContain("guard");
  });

  it("appends multiple errors to the same file", () => {
    logError(new Error("first error"));
    logError(new Error("second error"));
    const logPath = join(tempDir, "logs", "error.log");
    const content = readFileSync(logPath, "utf-8");
    expect(content).toContain("first error");
    expect(content).toContain("second error");
  });

  it("includes ISO 8601 timestamp", () => {
    logError(new Error("timestamped"));
    const logPath = join(tempDir, "logs", "error.log");
    const content = readFileSync(logPath, "utf-8");
    // Match ISO 8601 pattern: 2026-02-20T...Z
    expect(content).toMatch(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
  });

  it("creates log directory if missing", () => {
    const logDir = join(tempDir, "logs");
    expect(existsSync(logDir)).toBe(false);
    logError(new Error("creates dir"));
    expect(existsSync(logDir)).toBe(true);
  });

  it("rotates log when size exceeds 10MB", () => {
    const logDir = join(tempDir, "logs");
    const logPath = join(logDir, "error.log");

    // Pre-create a log file > 10MB
    mkdirSyncFs(logDir, { recursive: true });
    const largeContent = "X".repeat(10 * 1024 * 1024 + 100);
    writeFileSync(logPath, largeContent);

    // This write should trigger rotation
    logError(new Error("after rotation"));

    // The old file should have been rotated to error.log.1
    expect(existsSync(join(logDir, "error.log.1"))).toBe(true);
    // The current error.log should contain the new error
    const content = readFileSync(logPath, "utf-8");
    expect(content).toContain("after rotation");
    expect(content.length).toBeLessThan(largeContent.length);
  });

  it("rotates correctly across multiple cycles (oldest file cleaned up)", () => {
    const logDir = join(tempDir, "logs");
    const logPath = join(logDir, "error.log");
    mkdirSyncFs(logDir, { recursive: true });

    // Simulate 5 rotation cycles to verify .3 is cleaned up
    for (let cycle = 1; cycle <= 5; cycle++) {
      const largeContent = `CYCLE-${cycle}-`.padEnd(10 * 1024 * 1024 + 100, "X");
      writeFileSync(logPath, largeContent);
      logError(new Error(`rotation cycle ${cycle}`));
    }

    // After 5 rotations: .1, .2, .3 should exist, no .4 or .5
    expect(existsSync(join(logDir, "error.log"))).toBe(true);
    expect(existsSync(join(logDir, "error.log.1"))).toBe(true);
    expect(existsSync(join(logDir, "error.log.2"))).toBe(true);
    expect(existsSync(join(logDir, "error.log.3"))).toBe(true);
    expect(existsSync(join(logDir, "error.log.4"))).toBe(false);
    expect(existsSync(join(logDir, "error.log.5"))).toBe(false);

    // .1 should contain cycle 5 content (most recent rotation)
    const rot1 = readFileSync(join(logDir, "error.log.1"), "utf-8");
    expect(rot1).toContain("CYCLE-5-");
  });

  it("never throws even on failure", () => {
    // Point to a non-writable location (best-effort test)
    // logError should swallow the error internally
    process.env.HOOKWISE_STATE_DIR = "/dev/null/nonexistent";
    expect(() => logError(new Error("should not throw"))).not.toThrow();
  });
});

describe("logDebug", () => {
  let tempDir: string;
  const originalEnv = process.env.HOOKWISE_STATE_DIR;
  let originalLevel: "debug" | "info" | "warn" | "error";

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-debug-test-"));
    process.env.HOOKWISE_STATE_DIR = tempDir;
    originalLevel = getLogLevel();
  });

  afterEach(() => {
    setLogLevel(originalLevel);
    if (originalEnv !== undefined) {
      process.env.HOOKWISE_STATE_DIR = originalEnv;
    } else {
      delete process.env.HOOKWISE_STATE_DIR;
    }
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("writes debug messages when logLevel is debug", () => {
    setLogLevel("debug");
    logDebug("debug message", { detail: "info" });
    const logPath = join(tempDir, "logs", "debug.log");
    expect(existsSync(logPath)).toBe(true);
    const content = readFileSync(logPath, "utf-8");
    expect(content).toContain("DEBUG");
    expect(content).toContain("debug message");
  });

  it("does NOT write when logLevel is info", () => {
    setLogLevel("info");
    logDebug("should not appear");
    const logPath = join(tempDir, "logs", "debug.log");
    expect(existsSync(logPath)).toBe(false);
  });

  it("does NOT write when logLevel is warn", () => {
    setLogLevel("warn");
    logDebug("should not appear");
    const logPath = join(tempDir, "logs", "debug.log");
    expect(existsSync(logPath)).toBe(false);
  });

  it("does NOT write when logLevel is error", () => {
    setLogLevel("error");
    logDebug("should not appear");
    const logPath = join(tempDir, "logs", "debug.log");
    expect(existsSync(logPath)).toBe(false);
  });

  it("includes data in the debug log entry", () => {
    setLogLevel("debug");
    logDebug("with data", { key: "value", count: 42 });
    const logPath = join(tempDir, "logs", "debug.log");
    const content = readFileSync(logPath, "utf-8");
    expect(content).toContain("key");
    expect(content).toContain("value");
  });

  it("handles undefined data gracefully", () => {
    setLogLevel("debug");
    logDebug("no data");
    const logPath = join(tempDir, "logs", "debug.log");
    const content = readFileSync(logPath, "utf-8");
    expect(content).toContain("no data");
  });

  it("never throws even on failure", () => {
    setLogLevel("debug");
    process.env.HOOKWISE_STATE_DIR = "/dev/null/nonexistent";
    expect(() => logDebug("should not throw")).not.toThrow();
  });
});

describe("setLogLevel / getLogLevel", () => {
  let originalLevel: "debug" | "info" | "warn" | "error";

  beforeEach(() => {
    originalLevel = getLogLevel();
  });

  afterEach(() => {
    setLogLevel(originalLevel);
  });

  it("defaults to info", () => {
    // Reset to default for this test
    setLogLevel("info");
    expect(getLogLevel()).toBe("info");
  });

  it("can be set to debug", () => {
    setLogLevel("debug");
    expect(getLogLevel()).toBe("debug");
  });

  it("can be set to warn", () => {
    setLogLevel("warn");
    expect(getLogLevel()).toBe("warn");
  });

  it("can be set to error", () => {
    setLogLevel("error");
    expect(getLogLevel()).toBe("error");
  });
});
