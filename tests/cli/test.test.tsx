/**
 * Tests for the test command.
 *
 * Verifies:
 * - "Running tests..." loading state appears initially
 * - Result rendering for various vitest output formats
 * - Handling of test failures
 * - Timeout/error scenarios
 * - Async exec pattern (non-blocking)
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import React from "react";
import { render } from "ink-testing-library";
import type { ChildProcess } from "node:child_process";
import { TestCommand } from "../../src/cli/commands/test.js";

type ExecCallback = (
  error: Error | null,
  stdout: string,
  stderr: string
) => void;

/**
 * Creates a mock exec function that captures the callback
 * and allows us to resolve it later with controlled output.
 */
function createMockExec() {
  let capturedCallback: ExecCallback | null = null;
  let resolveReady: (() => void) | null = null;
  const readyPromise = new Promise<void>((resolve) => {
    resolveReady = resolve;
  });

  const mockExec = vi.fn(
    (
      _cmd: string,
      _opts: unknown,
      cb: ExecCallback
    ) => {
      capturedCallback = cb;
      if (resolveReady) resolveReady();
      const fakeChild = {
        kill: vi.fn(),
        pid: 12345,
      } as unknown as ChildProcess;
      return fakeChild;
    }
  );

  return {
    mockExec: mockExec as unknown as typeof import("node:child_process").exec,
    getCapturedCallback: () => capturedCallback,
    readyPromise,
  };
}

/**
 * Creates a mock exec that immediately invokes the callback.
 */
function createImmediateExec(stdout: string, error: Error | null = null) {
  const mockExec = vi.fn(
    (
      _cmd: string,
      _opts: unknown,
      cb: ExecCallback
    ) => {
      // Use setTimeout(0) to allow React to flush the initial render
      setTimeout(() => cb(error, stdout, ""), 0);
      const fakeChild = {
        kill: vi.fn(),
        pid: 12345,
      } as unknown as ChildProcess;
      return fakeChild;
    }
  );
  return mockExec as unknown as typeof import("node:child_process").exec;
}

describe("TestCommand", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows 'Running tests...' loading state initially", () => {
    const { mockExec } = createMockExec();
    const { lastFrame } = render(
      <TestCommand projectDir="/tmp" execFn={mockExec} />
    );
    const frame = lastFrame()!;
    expect(frame).toContain("Running tests...");
  });

  it("renders passed test results", async () => {
    vi.useRealTimers();
    const execFn = createImmediateExec(
      "Tests  30 passed (30)\nDuration  1.5s"
    );
    const { lastFrame } = render(
      <TestCommand projectDir="/tmp" execFn={execFn} />
    );

    // Wait for the setTimeout(0) callback to fire
    await new Promise((r) => setTimeout(r, 50));

    const frame = lastFrame()!;
    expect(frame).toContain("Test Results");
    expect(frame).toContain("30 passed");
    expect(frame).toContain("0 failed");
  });

  it("renders mixed pass/fail results", async () => {
    vi.useRealTimers();
    const execFn = createImmediateExec(
      "Tests  25 passed | 5 failed (30)\nDuration  2.0s"
    );
    const { lastFrame } = render(
      <TestCommand projectDir="/tmp" execFn={execFn} />
    );

    await new Promise((r) => setTimeout(r, 50));

    const frame = lastFrame()!;
    expect(frame).toContain("Test Results");
    expect(frame).toContain("25 passed");
    expect(frame).toContain("5 failed");
    expect(frame).toContain("30 total");
  });

  it("shows error state on exec failure", async () => {
    vi.useRealTimers();
    const execFn = createImmediateExec("", new Error("Command not found"));
    const { lastFrame } = render(
      <TestCommand projectDir="/tmp" execFn={execFn} />
    );

    await new Promise((r) => setTimeout(r, 50));

    const frame = lastFrame()!;
    expect(frame).toContain("Test runner error");
    expect(frame).toContain("Command not found");
  });

  it("handles output with no matching patterns", async () => {
    vi.useRealTimers();
    const execFn = createImmediateExec("Some unexpected output format");
    const { lastFrame } = render(
      <TestCommand projectDir="/tmp" execFn={execFn} />
    );

    await new Promise((r) => setTimeout(r, 50));

    const frame = lastFrame()!;
    expect(frame).toContain("Test Results");
    expect(frame).toContain("0 passed");
    expect(frame).toContain("0 failed");
  });

  it("renders header", () => {
    const { mockExec } = createMockExec();
    const { lastFrame } = render(
      <TestCommand projectDir="/tmp" execFn={mockExec} />
    );
    expect(lastFrame()!).toContain("hookwise");
  });

  it("passes projectDir to exec", () => {
    const { mockExec } = createMockExec();
    render(<TestCommand projectDir="/my/project" execFn={mockExec} />);

    expect(mockExec).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ cwd: "/my/project" }),
      expect.any(Function)
    );
  });

  it("renders all-failures result", async () => {
    vi.useRealTimers();
    const execFn = createImmediateExec(
      "Tests  0 passed | 10 failed (10)\nDuration  3.0s"
    );
    const { lastFrame } = render(
      <TestCommand projectDir="/tmp" execFn={execFn} />
    );

    await new Promise((r) => setTimeout(r, 50));

    const frame = lastFrame()!;
    expect(frame).toContain("0 passed");
    expect(frame).toContain("10 failed");
  });

  it("calls exec with vitest run command", () => {
    const { mockExec } = createMockExec();
    render(<TestCommand projectDir="/tmp" execFn={mockExec} />);

    expect(mockExec).toHaveBeenCalledWith(
      expect.stringContaining("vitest run"),
      expect.any(Object),
      expect.any(Function)
    );
  });
});
