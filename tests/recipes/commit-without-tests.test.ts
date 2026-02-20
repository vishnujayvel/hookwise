/**
 * Tests for the Commit Without Tests Guard recipe handler.
 */

import { describe, it, expect } from "vitest";
import type { HookPayload } from "../../src/core/types.js";
import {
  trackTestRun,
  checkCommit,
} from "../../recipes/quality/commit-without-tests/handler.js";
import type {
  CommitWithoutTestsConfig,
  TestTrackingState,
} from "../../recipes/quality/commit-without-tests/handler.js";

function makePayload(overrides: Partial<HookPayload> = {}): HookPayload {
  return {
    session_id: "test-session",
    ...overrides,
  };
}

function freshState(): TestTrackingState {
  return {
    testsRunThisSession: false,
    lastTestResult: null,
    lastTestAt: null,
    testPatterns: [],
  };
}

const config: CommitWithoutTestsConfig = {
  enabled: true,
  testPatterns: ["vitest", "jest", "pytest", "npm test"],
  action: "block",
};

describe("trackTestRun", () => {
  it("detects vitest command", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "npx vitest run", exit_code: 0 },
    });

    trackTestRun(event, state, config);
    expect(state.testsRunThisSession).toBe(true);
    expect(state.lastTestResult).toBe("pass");
    expect(state.lastTestAt).not.toBeNull();
  });

  it("detects jest command", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "npx jest --coverage", exit_code: 0 },
    });

    trackTestRun(event, state, config);
    expect(state.testsRunThisSession).toBe(true);
  });

  it("detects pytest command", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "python -m pytest tests/", exit_code: 0 },
    });

    trackTestRun(event, state, config);
    expect(state.testsRunThisSession).toBe(true);
  });

  it("records failed test result", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "npx vitest run", exit_code: 1 },
    });

    trackTestRun(event, state, config);
    expect(state.testsRunThisSession).toBe(true);
    expect(state.lastTestResult).toBe("fail");
  });

  it("ignores non-Bash tools", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "vitest" },
    });

    trackTestRun(event, state, config);
    expect(state.testsRunThisSession).toBe(false);
  });

  it("ignores non-test commands", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "ls -la" },
    });

    trackTestRun(event, state, config);
    expect(state.testsRunThisSession).toBe(false);
  });

  it("does nothing when disabled", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "npx vitest run" },
    });

    trackTestRun(event, state, { ...config, enabled: false });
    expect(state.testsRunThisSession).toBe(false);
  });
});

describe("checkCommit", () => {
  it("blocks commit when no tests run", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: 'git commit -m "feature"' },
    });

    const result = checkCommit(event, state);
    expect(result.action).toBe("block");
    expect(result.reason).toContain("No tests");
  });

  it("allows commit when tests passed", () => {
    const state = freshState();
    state.testsRunThisSession = true;
    state.lastTestResult = "pass";

    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: 'git commit -m "feature"' },
    });

    const result = checkCommit(event, state);
    expect(result.action).toBe("allow");
  });

  it("warns when tests failed", () => {
    const state = freshState();
    state.testsRunThisSession = true;
    state.lastTestResult = "fail";
    state.lastTestAt = new Date().toISOString();

    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: 'git commit -m "fix"' },
    });

    const result = checkCommit(event, state);
    expect(result.action).toBe("warn");
    expect(result.reason).toContain("failed");
  });

  it("allows non-commit commands", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "git status" },
    });

    const result = checkCommit(event, state);
    expect(result.action).toBe("allow");
  });

  it("allows non-Bash tools", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Write",
      tool_input: { content: "git commit" },
    });

    const result = checkCommit(event, state);
    expect(result.action).toBe("allow");
  });

  it("detects git commit amend", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: "git commit --amend" },
    });

    const result = checkCommit(event, state);
    expect(result.action).toBe("block");
  });

  it("uses config.action when provided", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: 'git commit -m "feature"' },
    });

    const warnConfig: CommitWithoutTestsConfig = {
      enabled: true,
      testPatterns: ["vitest"],
      action: "warn",
    };

    const result = checkCommit(event, state, warnConfig);
    expect(result.action).toBe("warn");
    expect(result.reason).toContain("No tests");
  });

  it("defaults to block when no config provided", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: 'git commit -m "feature"' },
    });

    const result = checkCommit(event, state);
    expect(result.action).toBe("block");
  });

  it("defaults to block when config.action is undefined", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: 'git commit -m "feature"' },
    });

    const partialConfig = { enabled: true, testPatterns: ["vitest"] } as CommitWithoutTestsConfig;
    const result = checkCommit(event, state, partialConfig);
    expect(result.action).toBe("block");
  });

  it("config.action=block blocks commit without tests", () => {
    const state = freshState();
    const event = makePayload({
      tool_name: "Bash",
      tool_input: { command: 'git commit -m "feature"' },
    });

    const result = checkCommit(event, state, config);
    expect(result.action).toBe("block");
  });
});
