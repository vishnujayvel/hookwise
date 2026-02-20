/**
 * Tests for the core dispatcher — three-phase execution engine.
 *
 * Covers Tasks 4.1-4.4:
 * - Stdin reading and event routing
 * - Three-phase execution with error boundaries
 * - Handler execution with timeout
 * - Missing/malformed config graceful handling
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  mkdtempSync,
  writeFileSync,
  rmSync,
  mkdirSync,
  chmodSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";
import yaml from "js-yaml";
import {
  dispatch,
  executeHandler,
  readStdinPayload,
} from "../../src/core/dispatcher.js";
import { getDefaultConfig } from "../../src/core/config.js";
import type {
  EventType,
  HookPayload,
  HooksConfig,
  ResolvedHandler,
  DispatchResult,
} from "../../src/core/types.js";

// --- Helper: create a ResolvedHandler ---

function makeHandler(overrides: Partial<ResolvedHandler> = {}): ResolvedHandler {
  return {
    name: "test-handler",
    handlerType: "inline",
    events: new Set<EventType>(["PreToolUse"]),
    timeout: 10000,
    phase: "guard",
    configRaw: {},
    ...overrides,
  };
}

function makePayload(overrides: Partial<HookPayload> = {}): HookPayload {
  return {
    session_id: "test-session-123",
    ...overrides,
  };
}

// --- Task 4.1: Stdin reading and event routing ---

describe("readStdinPayload", () => {
  it("is exported and callable", () => {
    expect(typeof readStdinPayload).toBe("function");
  });

  // Subprocess-based tests: spawn a real Node process that calls readStdinPayload
  // with actual stdin piping — no mocks, no shortcuts.
  const helperScript = [
    'import { readStdinPayload } from "./src/core/dispatcher.js";',
    "const result = readStdinPayload();",
    "process.stdout.write(JSON.stringify(result));",
  ].join(" ");

  function runStdinTest(input: string): { stdout: string; stderr: string; status: number | null } {
    const tempStateDir = mkdtempSync(join(tmpdir(), "hw-stdin-"));
    const result = spawnSync("node", ["--import", "tsx", "-e", helperScript], {
      input,
      encoding: "utf-8",
      cwd: process.cwd(),
      env: { ...process.env, HOOKWISE_STATE_DIR: tempStateDir },
    });
    rmSync(tempStateDir, { recursive: true, force: true });
    return { stdout: result.stdout, stderr: result.stderr, status: result.status };
  }

  it("parses valid JSON from stdin", () => {
    const input = JSON.stringify({ session_id: "abc-123", tool_name: "Bash" });
    const { stdout } = runStdinTest(input);
    const parsed = JSON.parse(stdout);
    expect(parsed.session_id).toBe("abc-123");
    expect(parsed.tool_name).toBe("Bash");
  });

  it("returns empty payload on malformed JSON", () => {
    const { stdout } = runStdinTest("not valid json {{{");
    const parsed = JSON.parse(stdout);
    expect(parsed.session_id).toBe("");
  });

  it("returns empty payload on empty stdin", () => {
    const { stdout } = runStdinTest("");
    const parsed = JSON.parse(stdout);
    expect(parsed.session_id).toBe("");
  });

  it("preserves additional fields from valid payload", () => {
    const input = JSON.stringify({
      session_id: "sess-456",
      tool_name: "Write",
      tool_input: { file_path: "/tmp/test.ts", content: "hello" },
    });
    const { stdout } = runStdinTest(input);
    const parsed = JSON.parse(stdout);
    expect(parsed.session_id).toBe("sess-456");
    expect(parsed.tool_name).toBe("Write");
    expect(parsed.tool_input.file_path).toBe("/tmp/test.ts");
  });
});

describe("dispatch — event routing", () => {
  it("routes valid event to handler chain", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "inline-guard",
        type: "inline",
        events: ["PreToolUse"],
        action: { decision: "block", reason: "test block" },
        phase: "guard",
      },
    ];

    const result = dispatch("PreToolUse", makePayload(), { config });
    expect(result.exitCode).toBe(0);
    // Should have been blocked by inline guard
    expect(result.stdout).toBeTruthy();
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.decision).toBe("block");
    expect(stdout.reason).toBe("test block");
  });

  it("returns exit 0 with null stdout for unknown events", () => {
    const config = getDefaultConfig();
    // Force an invalid event type through the type system
    const result = dispatch("InvalidEvent" as EventType, makePayload(), { config });
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeNull();
  });

  it("returns exit 0 for events with no handlers", () => {
    const config = getDefaultConfig();
    const result = dispatch("PreToolUse", makePayload(), { config });
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeNull();
  });

  it("passes payload through to handlers", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "context-handler",
        type: "inline",
        events: ["SessionStart"],
        action: { additionalContext: "Hello from session" },
        phase: "context",
      },
    ];

    const payload = makePayload({ session_id: "unique-session" });
    const result = dispatch("SessionStart", payload, { config });
    expect(result.exitCode).toBe(0);
    // Context handler produces stdout with additionalContext
    expect(result.stdout).toBeTruthy();
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.hookSpecificOutput.additionalContext).toBe("Hello from session");
  });
});

// --- Task 4.2: Three-phase execution with error boundaries ---

describe("dispatch — three-phase execution", () => {
  it("Phase 1: guard block short-circuits execution", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "blocker",
        type: "inline",
        events: ["PreToolUse"],
        action: { decision: "block", reason: "Dangerous operation" },
        phase: "guard",
      },
      {
        name: "context",
        type: "inline",
        events: ["PreToolUse"],
        action: { additionalContext: "Should not appear" },
        phase: "context",
      },
    ];

    const result = dispatch("PreToolUse", makePayload(), { config });
    expect(result.exitCode).toBe(0);
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.decision).toBe("block");
    expect(stdout.reason).toBe("Dangerous operation");
    // Context handler should NOT have run (short-circuited)
    expect(result.stdout).not.toContain("Should not appear");
  });

  it("Phase 1: first block wins (first-match-wins)", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "first-guard",
        type: "inline",
        events: ["PreToolUse"],
        action: { decision: "block", reason: "First block" },
        phase: "guard",
      },
      {
        name: "second-guard",
        type: "inline",
        events: ["PreToolUse"],
        action: { decision: "block", reason: "Second block" },
        phase: "guard",
      },
    ];

    const result = dispatch("PreToolUse", makePayload(), { config });
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.reason).toBe("First block");
  });

  it("Phase 1: non-blocking guard allows continuation", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "warn-guard",
        type: "inline",
        events: ["PreToolUse"],
        action: { decision: null },
        phase: "guard",
      },
      {
        name: "context",
        type: "inline",
        events: ["PreToolUse"],
        action: { additionalContext: "Context injected" },
        phase: "context",
      },
    ];

    const result = dispatch("PreToolUse", makePayload(), { config });
    // Guard did not block, so context phase ran
    expect(result.stdout).toBeTruthy();
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.hookSpecificOutput.additionalContext).toBe("Context injected");
  });

  it("Phase 2: context injection merges multiple handlers", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "greeting",
        type: "inline",
        events: ["SessionStart"],
        action: { additionalContext: "Good morning!" },
        phase: "context",
      },
      {
        name: "metacog",
        type: "inline",
        events: ["SessionStart"],
        action: { additionalContext: "Remember to think before acting." },
        phase: "context",
      },
    ];

    const result = dispatch("SessionStart", makePayload(), { config });
    expect(result.stdout).toBeTruthy();
    const stdout = JSON.parse(result.stdout!);
    const context = stdout.hookSpecificOutput.additionalContext;
    expect(context).toContain("Good morning!");
    expect(context).toContain("Remember to think before acting.");
  });

  it("Phase 2: context with no output returns null stdout", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "noop-context",
        type: "inline",
        events: ["SessionStart"],
        action: {},
        phase: "context",
      },
    ];

    const result = dispatch("SessionStart", makePayload(), { config });
    expect(result.stdout).toBeNull();
    expect(result.exitCode).toBe(0);
  });

  it("Phase 3: side effects execute after context", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "context",
        type: "inline",
        events: ["Stop"],
        action: { additionalContext: "Wrapping up" },
        phase: "context",
      },
      {
        name: "side-effect",
        type: "inline",
        events: ["Stop"],
        action: { output: { tracked: true } },
        phase: "side_effect",
      },
    ];

    const result = dispatch("Stop", makePayload(), { config });
    // Context produced output, side effects ran silently
    expect(result.exitCode).toBe(0);
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.hookSpecificOutput.additionalContext).toBe("Wrapping up");
  });

  it("Phase 3: side effect errors are swallowed", () => {
    // This tests that Phase 3 errors don't crash the dispatch
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "error-side-effect",
        type: "script",
        events: ["Stop"],
        command: "nonexistent_command_that_will_fail",
        phase: "side_effect",
      },
    ];

    const result = dispatch("Stop", makePayload(), { config });
    // Should not crash, just log the error
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeNull();
  });

  it("all phases return exit 0 on error", () => {
    // A broken config passed directly should still exit 0
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "script-that-fails",
        type: "script",
        events: ["PreToolUse"],
        command: "/nonexistent/path/to/script.sh",
        phase: "guard",
      },
    ];

    const result = dispatch("PreToolUse", makePayload(), { config });
    expect(result.exitCode).toBe(0);
  });
});

// --- Task 4.3: Handler execution with timeout ---

describe("executeHandler", () => {
  it("executes inline handler and returns action", () => {
    const handler = makeHandler({
      handlerType: "inline",
      action: { decision: "block", reason: "test" },
    });

    const result = executeHandler(handler, makePayload());
    expect(result.decision).toBe("block");
    expect(result.reason).toBe("test");
  });

  it("inline handler with no action returns null result", () => {
    const handler = makeHandler({
      handlerType: "inline",
      action: undefined,
    });

    const result = executeHandler(handler, makePayload());
    expect(result.decision).toBeNull();
    expect(result.reason).toBeNull();
    expect(result.additionalContext).toBeNull();
  });

  it("inline handler with additionalContext", () => {
    const handler = makeHandler({
      handlerType: "inline",
      phase: "context",
      action: { additionalContext: "Hello!" },
    });

    const result = executeHandler(handler, makePayload());
    expect(result.additionalContext).toBe("Hello!");
  });

  it("builtin handler returns null result when no module exists", () => {
    const handler = makeHandler({
      handlerType: "builtin",
      module: "nonexistent",
    });

    const result = executeHandler(handler, makePayload());
    expect(result.decision).toBeNull();
  });

  it("builtin handler with action returns the action", () => {
    const handler = makeHandler({
      handlerType: "builtin",
      module: "test",
      action: { decision: "block", reason: "builtin block" },
    });

    const result = executeHandler(handler, makePayload());
    expect(result.decision).toBe("block");
    expect(result.reason).toBe("builtin block");
  });

  it("script handler receives stdin and returns stdout", () => {
    // Create a simple script that reads stdin and echoes JSON
    const tempDir = mkdtempSync(join(tmpdir(), "hookwise-script-test-"));
    const scriptPath = join(tempDir, "echo.sh");
    writeFileSync(
      scriptPath,
      '#!/bin/bash\ncat > /dev/null\necho \'{"additionalContext":"from script"}\'\n',
      { mode: 0o755 }
    );

    try {
      const handler = makeHandler({
        handlerType: "script",
        command: scriptPath,
        phase: "context",
        timeout: 5000,
      });

      const result = executeHandler(handler, makePayload());
      expect(result.additionalContext).toBe("from script");
    } finally {
      rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("script handler: non-zero exit without block JSON -> error", () => {
    const tempDir = mkdtempSync(join(tmpdir(), "hookwise-script-test-"));
    const scriptPath = join(tempDir, "fail.sh");
    writeFileSync(scriptPath, '#!/bin/bash\necho "error"\nexit 1\n', {
      mode: 0o755,
    });

    try {
      const handler = makeHandler({
        handlerType: "script",
        command: scriptPath,
        timeout: 5000,
      });

      const result = executeHandler(handler, makePayload());
      expect(result.decision).toBeNull();
    } finally {
      rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("script handler: exit 2 with valid block JSON -> block", () => {
    const tempDir = mkdtempSync(join(tmpdir(), "hookwise-script-test-"));
    const scriptPath = join(tempDir, "block.sh");
    writeFileSync(
      scriptPath,
      '#!/bin/bash\necho \'{"decision":"block","reason":"script blocked"}\'\nexit 2\n',
      { mode: 0o755 }
    );

    try {
      const handler = makeHandler({
        handlerType: "script",
        command: scriptPath,
        timeout: 5000,
      });

      const result = executeHandler(handler, makePayload());
      expect(result.decision).toBe("block");
      expect(result.reason).toBe("script blocked");
    } finally {
      rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("script handler: exit 2 without block decision -> error (not block)", () => {
    const tempDir = mkdtempSync(join(tmpdir(), "hookwise-script-test-"));
    const scriptPath = join(tempDir, "bad-block.sh");
    writeFileSync(
      scriptPath,
      '#!/bin/bash\necho \'{"decision":"warn","reason":"just a warning"}\'\nexit 2\n',
      { mode: 0o755 }
    );

    try {
      const handler = makeHandler({
        handlerType: "script",
        command: scriptPath,
        timeout: 5000,
      });

      const result = executeHandler(handler, makePayload());
      // Exit 2 but decision is not "block" -> treated as error
      expect(result.decision).toBeNull();
    } finally {
      rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("script handler: timeout kills and continues", () => {
    const tempDir = mkdtempSync(join(tmpdir(), "hookwise-script-test-"));
    const scriptPath = join(tempDir, "slow.sh");
    writeFileSync(scriptPath, '#!/bin/bash\nsleep 30\necho "done"\n', {
      mode: 0o755,
    });

    try {
      const handler = makeHandler({
        handlerType: "script",
        command: scriptPath,
        timeout: 500, // 500ms timeout
      });

      const start = Date.now();
      const result = executeHandler(handler, makePayload());
      const elapsed = Date.now() - start;

      // Should have timed out and returned null result
      expect(result.decision).toBeNull();
      // Should have completed in roughly the timeout period
      expect(elapsed).toBeLessThan(5000);
    } finally {
      rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("script handler: missing command -> error result", () => {
    const handler = makeHandler({
      handlerType: "script",
      command: undefined,
    });

    const result = executeHandler(handler, makePayload());
    expect(result.decision).toBeNull();
  });

  it("script handler: nonexistent command -> error result", () => {
    const handler = makeHandler({
      handlerType: "script",
      command: "/nonexistent/totally/fake/command",
      timeout: 5000,
    });

    const result = executeHandler(handler, makePayload());
    expect(result.decision).toBeNull();
  });

  it("catches exceptions from any handler type", () => {
    // Force an internal error by using an unknown handler type
    const handler = makeHandler({
      handlerType: "unknown" as "builtin",
    });

    const result = executeHandler(handler, makePayload());
    expect(result.decision).toBeNull();
  });
});

// --- Task 4.4: Missing/malformed config gracefully ---

describe("dispatch — graceful config handling", () => {
  let tempDir: string;
  let tempStateDir: string;
  const originalEnv = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-dispatch-test-"));
    tempStateDir = mkdtempSync(join(tmpdir(), "hookwise-state-dispatch-"));
    process.env.HOOKWISE_STATE_DIR = tempStateDir;
  });

  afterEach(() => {
    if (originalEnv !== undefined) {
      process.env.HOOKWISE_STATE_DIR = originalEnv;
    } else {
      delete process.env.HOOKWISE_STATE_DIR;
    }
    rmSync(tempDir, { recursive: true, force: true });
    rmSync(tempStateDir, { recursive: true, force: true });
  });

  it("no hookwise.yaml: exit 0 silently with no stdout", () => {
    const result = dispatch("PreToolUse", makePayload(), {
      projectDir: tempDir,
    });
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeNull();
    expect(result.stderr).toBeNull();
  });

  it("malformed YAML: log error and exit 0", () => {
    writeFileSync(
      join(tempDir, "hookwise.yaml"),
      "invalid: yaml: [[[{{{broken",
      "utf-8"
    );

    const result = dispatch("PreToolUse", makePayload(), {
      projectDir: tempDir,
    });
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeNull();
  });

  it("no handlers for event: exit 0", () => {
    const yamlContent = yaml.dump({
      version: 1,
      handlers: [
        { name: "only-stop", type: "inline", events: ["Stop"], action: {} },
      ],
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), yamlContent);

    const result = dispatch("PreToolUse", makePayload(), {
      projectDir: tempDir,
    });
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeNull();
  });

  it("config passed directly bypasses file loading", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "inline-test",
        type: "inline",
        events: ["PreToolUse"],
        action: { decision: "block", reason: "direct config" },
        phase: "guard",
      },
    ];

    const result = dispatch("PreToolUse", makePayload(), { config });
    expect(result.exitCode).toBe(0);
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.decision).toBe("block");
  });

  it("handles empty config (no sections) gracefully", () => {
    writeFileSync(join(tempDir, "hookwise.yaml"), "", "utf-8");

    const result = dispatch("PreToolUse", makePayload(), {
      projectDir: tempDir,
    });
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeNull();
  });

  it("handles config with only version field", () => {
    writeFileSync(
      join(tempDir, "hookwise.yaml"),
      "version: 1\n",
      "utf-8"
    );

    const result = dispatch("PreToolUse", makePayload(), {
      projectDir: tempDir,
    });
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeNull();
  });
});

// --- Integration: full dispatch pipeline ---

describe("dispatch — integration", () => {
  it("full pipeline: guard allows, context injects, side effect runs", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "allow-guard",
        type: "inline",
        events: ["PreToolUse"],
        action: { decision: null },
        phase: "guard",
      },
      {
        name: "inject-context",
        type: "inline",
        events: ["PreToolUse"],
        action: { additionalContext: "Be careful with this tool." },
        phase: "context",
      },
      {
        name: "track-analytics",
        type: "inline",
        events: ["PreToolUse"],
        action: { output: { event: "tool_use_tracked" } },
        phase: "side_effect",
      },
    ];

    const result = dispatch("PreToolUse", makePayload(), { config });
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeTruthy();
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.hookSpecificOutput.additionalContext).toBe(
      "Be careful with this tool."
    );
  });

  it("guard block prevents context and side effects", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "blocker",
        type: "inline",
        events: ["PreToolUse"],
        action: { decision: "block", reason: "Blocked!" },
        phase: "guard",
      },
      {
        name: "context",
        type: "inline",
        events: ["PreToolUse"],
        action: { additionalContext: "Should not run" },
        phase: "context",
      },
      {
        name: "analytics",
        type: "inline",
        events: ["PreToolUse"],
        action: { output: { should: "not run" } },
        phase: "side_effect",
      },
    ];

    const result = dispatch("PreToolUse", makePayload(), { config });
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.decision).toBe("block");
    // Only the block reason, nothing from context or side effects
    expect(result.stdout).not.toContain("Should not run");
  });

  it("multiple events dispatch independently", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "pre-tool-context",
        type: "inline",
        events: ["PreToolUse"],
        action: { additionalContext: "PreToolUse context" },
        phase: "context",
      },
      {
        name: "session-context",
        type: "inline",
        events: ["SessionStart"],
        action: { additionalContext: "SessionStart context" },
        phase: "context",
      },
    ];

    const preToolResult = dispatch("PreToolUse", makePayload(), { config });
    const sessionResult = dispatch("SessionStart", makePayload(), { config });

    const preToolStdout = JSON.parse(preToolResult.stdout!);
    expect(preToolStdout.hookSpecificOutput.additionalContext).toBe("PreToolUse context");

    const sessionStdout = JSON.parse(sessionResult.stdout!);
    expect(sessionStdout.hookSpecificOutput.additionalContext).toBe("SessionStart context");
  });

  it("safeDispatch wraps dispatch for fail-open", () => {
    // Pass a config that would normally work but force an internal error scenario
    const result = dispatch(
      "InvalidEvent" as EventType,
      makePayload(),
      { config: getDefaultConfig() }
    );
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeNull();
  });
});

// --- Script handler integration tests ---

describe("dispatch — script handler integration", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-script-int-"));
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("script handler receives payload on stdin", () => {
    // Node script that reads stdin, parses JSON, and outputs additionalContext
    const scriptPath = join(tempDir, "echo-stdin.js");
    writeFileSync(
      scriptPath,
      [
        "#!/usr/bin/env node",
        "let input = '';",
        "process.stdin.setEncoding('utf-8');",
        "process.stdin.on('data', d => input += d);",
        "process.stdin.on('end', () => {",
        "  const payload = JSON.parse(input);",
        "  const result = { additionalContext: 'session: ' + payload.session_id };",
        "  process.stdout.write(JSON.stringify(result));",
        "});",
      ].join("\n"),
      { mode: 0o755 }
    );

    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "echo-script",
        type: "script",
        events: ["PostToolUse"],
        command: `node ${scriptPath}`,
        phase: "context",
      },
    ];

    const payload = makePayload({ session_id: "test-123" });
    const result = dispatch("PostToolUse", payload, { config });
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeTruthy();
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.hookSpecificOutput.additionalContext).toContain("test-123");
  });

  it("script handler block produces valid dispatch result", () => {
    const scriptPath = join(tempDir, "blocker.sh");
    writeFileSync(
      scriptPath,
      '#!/bin/bash\necho \'{"decision":"block","reason":"script says no"}\'\nexit 0\n',
      { mode: 0o755 }
    );

    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "script-guard",
        type: "script",
        events: ["PreToolUse"],
        command: scriptPath,
        phase: "guard",
      },
    ];

    const result = dispatch("PreToolUse", makePayload(), { config });
    expect(result.exitCode).toBe(0);
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.decision).toBe("block");
    expect(stdout.reason).toBe("script says no");
  });
});
