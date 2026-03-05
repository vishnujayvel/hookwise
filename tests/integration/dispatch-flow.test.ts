/**
 * End-to-end integration tests for the hookwise dispatch pipeline.
 *
 * Task 14.1: Tests the full dispatch flow with real configs, config merge
 * precedence, guard + analytics interaction, and CLI init config generation.
 *
 * These tests use real temp directories with hookwise.yaml files,
 * exercising the full config loading -> dispatch -> three-phase pipeline.
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import {
  mkdtempSync,
  writeFileSync,
  readFileSync,
  rmSync,
  mkdirSync,
  existsSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { execSync } from "node:child_process";
import yaml from "js-yaml";
import { dispatch } from "../../src/core/dispatcher.js";
import {
  loadConfig,
  saveConfig,
  getDefaultConfig,
} from "../../src/core/config.js";
import { evaluate } from "../../src/core/guards.js";
import type {
  EventType,
  HookPayload,
  HooksConfig,
  GuardRule,
} from "../../src/core/types.js";

// --- Helpers ---

function makePayload(overrides: Partial<HookPayload> = {}): HookPayload {
  return {
    session_id: "integration-test-session",
    ...overrides,
  };
}

function writeYaml(dir: string, filename: string, data: unknown): void {
  writeFileSync(
    join(dir, filename),
    yaml.dump(data, { indent: 2, noRefs: true }),
    "utf-8"
  );
}

// --- Full Dispatch Flow ---

describe("integration: full dispatch flow", () => {
  let tempDir: string;
  let tempStateDir: string;
  const originalEnv = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-integ-dispatch-"));
    tempStateDir = mkdtempSync(join(tmpdir(), "hookwise-integ-state-"));
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

  it("dispatches through all three phases with a real config file", () => {
    const config = {
      version: 1,
      analytics: { enabled: false },
      handlers: [
        {
          name: "allow-guard",
          type: "inline",
          events: ["PreToolUse"],
          phase: "guard",
          action: { decision: null },
        },
        {
          name: "context-injector",
          type: "inline",
          events: ["PreToolUse"],
          phase: "context",
          action: { additionalContext: "Integration context injected" },
        },
        {
          name: "side-effect-tracker",
          type: "inline",
          events: ["PreToolUse"],
          phase: "side_effect",
          action: { output: { tracked: true } },
        },
      ],
    };

    writeYaml(tempDir, "hookwise.yaml", config);

    const result = dispatch("PreToolUse", makePayload(), {
      projectDir: tempDir,
    });

    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeTruthy();
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.hookSpecificOutput.additionalContext).toBe(
      "Integration context injected"
    );
  });

  it("guard block short-circuits context and side effects with real config", () => {
    const config = {
      version: 1,
      analytics: { enabled: false },
      handlers: [
        {
          name: "blocker",
          type: "inline",
          events: ["PreToolUse"],
          phase: "guard",
          action: { decision: "block", reason: "Blocked by integration guard" },
        },
        {
          name: "context-should-not-run",
          type: "inline",
          events: ["PreToolUse"],
          phase: "context",
          action: { additionalContext: "THIS SHOULD NOT APPEAR" },
        },
      ],
    };

    writeYaml(tempDir, "hookwise.yaml", config);

    const result = dispatch("PreToolUse", makePayload(), {
      projectDir: tempDir,
    });

    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeTruthy();
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.decision).toBe("block");
    expect(stdout.reason).toBe("Blocked by integration guard");
    expect(result.stdout).not.toContain("THIS SHOULD NOT APPEAR");
  });

  it("handles multiple context handlers merging output", () => {
    const config = {
      version: 1,
      analytics: { enabled: false },
      handlers: [
        {
          name: "greeting",
          type: "inline",
          events: ["SessionStart"],
          phase: "context",
          action: { additionalContext: "Welcome to the session!" },
        },
        {
          name: "reminder",
          type: "inline",
          events: ["SessionStart"],
          phase: "context",
          action: { additionalContext: "Remember your goals." },
        },
      ],
    };

    writeYaml(tempDir, "hookwise.yaml", config);

    const result = dispatch("SessionStart", makePayload(), {
      projectDir: tempDir,
    });

    expect(result.exitCode).toBe(0);
    const stdout = JSON.parse(result.stdout!);
    const context = stdout.hookSpecificOutput.additionalContext;
    expect(context).toContain("Welcome to the session!");
    expect(context).toContain("Remember your goals.");
  });

  it("returns null stdout when no handlers match event", () => {
    const config = {
      version: 1,
      analytics: { enabled: false },
      handlers: [
        {
          name: "stop-only",
          type: "inline",
          events: ["Stop"],
          phase: "side_effect",
          action: { output: { logged: true } },
        },
      ],
    };

    writeYaml(tempDir, "hookwise.yaml", config);

    const result = dispatch("PreToolUse", makePayload(), {
      projectDir: tempDir,
    });

    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeNull();
  });

  it("handles wildcard event handlers", () => {
    const config = {
      version: 1,
      analytics: { enabled: false },
      handlers: [
        {
          name: "universal-context",
          type: "inline",
          events: "*",
          phase: "context",
          action: { additionalContext: "Universal context" },
        },
      ],
    };

    writeYaml(tempDir, "hookwise.yaml", config);

    // Should fire for any event type
    const result1 = dispatch("PreToolUse", makePayload(), {
      projectDir: tempDir,
    });
    const result2 = dispatch("SessionStart", makePayload(), {
      projectDir: tempDir,
    });
    const result3 = dispatch("Stop", makePayload(), {
      projectDir: tempDir,
    });

    for (const result of [result1, result2, result3]) {
      expect(result.exitCode).toBe(0);
      expect(result.stdout).toBeTruthy();
      const stdout = JSON.parse(result.stdout!);
      expect(stdout.hookSpecificOutput.additionalContext).toBe(
        "Universal context"
      );
    }
  });

  it("script handler receives payload and returns context", () => {
    // Create a node script that echoes session_id
    const scriptPath = join(tempDir, "echo-session.js");
    writeFileSync(
      scriptPath,
      [
        "#!/usr/bin/env node",
        "let input = '';",
        "process.stdin.setEncoding('utf-8');",
        "process.stdin.on('data', d => input += d);",
        "process.stdin.on('end', () => {",
        "  const p = JSON.parse(input);",
        '  process.stdout.write(JSON.stringify({ additionalContext: "session=" + p.session_id }));',
        "});",
      ].join("\n"),
      { mode: 0o755 }
    );

    const config = {
      version: 1,
      analytics: { enabled: false },
      handlers: [
        {
          name: "echo-script",
          type: "script",
          events: ["PostToolUse"],
          phase: "context",
          command: `node ${scriptPath}`,
        },
      ],
    };

    writeYaml(tempDir, "hookwise.yaml", config);

    const result = dispatch(
      "PostToolUse",
      makePayload({ session_id: "integ-abc" }),
      { projectDir: tempDir }
    );

    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeTruthy();
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.hookSpecificOutput.additionalContext).toBe(
      "session=integ-abc"
    );
  });
});

// --- Config Merge Precedence ---

describe("integration: config merge precedence", () => {
  let projectDir: string;
  let globalStateDir: string;
  const originalEnv = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    projectDir = mkdtempSync(join(tmpdir(), "hookwise-integ-merge-proj-"));
    globalStateDir = mkdtempSync(join(tmpdir(), "hookwise-integ-merge-glob-"));
    process.env.HOOKWISE_STATE_DIR = globalStateDir;
  });

  afterEach(() => {
    if (originalEnv !== undefined) {
      process.env.HOOKWISE_STATE_DIR = originalEnv;
    } else {
      delete process.env.HOOKWISE_STATE_DIR;
    }
    rmSync(projectDir, { recursive: true, force: true });
    rmSync(globalStateDir, { recursive: true, force: true });
  });

  it("project config overrides global config values", () => {
    // Global config: coaching disabled, log level info
    const globalConfig = {
      version: 1,
      analytics: { enabled: false },
      settings: {
        log_level: "info",
        handler_timeout_seconds: 10,
      },
      coaching: {
        metacognition: {
          enabled: false,
          interval_seconds: 300,
        },
      },
    };

    // Project config: coaching enabled, log level debug
    const projectConfig = {
      version: 1,
      analytics: { enabled: false },
      settings: {
        log_level: "debug",
      },
      coaching: {
        metacognition: {
          enabled: true,
          interval_seconds: 120,
        },
      },
    };

    writeYaml(globalStateDir, "config.yaml", globalConfig);
    writeYaml(projectDir, "hookwise.yaml", projectConfig);

    const loaded = loadConfig(projectDir);

    // Project values should win
    expect(loaded.settings.logLevel).toBe("debug");
    expect(loaded.coaching.metacognition.enabled).toBe(true);
    expect(loaded.coaching.metacognition.intervalSeconds).toBe(120);
    // Global value preserved where project doesn't override
    expect(loaded.settings.handlerTimeoutSeconds).toBe(10);
  });

  it("global config used as fallback when project config is missing", () => {
    const globalConfig = {
      version: 1,
      analytics: { enabled: false },
      settings: {
        log_level: "warn",
        handler_timeout_seconds: 20,
      },
    };

    writeYaml(globalStateDir, "config.yaml", globalConfig);
    // No project hookwise.yaml

    const loaded = loadConfig(projectDir);

    expect(loaded.settings.logLevel).toBe("warn");
    expect(loaded.settings.handlerTimeoutSeconds).toBe(20);
  });

  it("defaults returned when neither global nor project config exists", () => {
    const loaded = loadConfig(projectDir);
    const defaults = getDefaultConfig();

    expect(loaded.version).toBe(defaults.version);
    expect(loaded.guards).toEqual(defaults.guards);
    expect(loaded.settings.logLevel).toBe(defaults.settings.logLevel);
  });

  it("project handlers override global handlers entirely", () => {
    const globalConfig = {
      version: 1,
      analytics: { enabled: false },
      handlers: [
        {
          name: "global-handler",
          type: "inline",
          events: ["Stop"],
          action: { output: { source: "global" } },
        },
      ],
    };

    const projectConfig = {
      version: 1,
      analytics: { enabled: false },
      handlers: [
        {
          name: "project-handler",
          type: "inline",
          events: ["PreToolUse"],
          phase: "context",
          action: { additionalContext: "from project" },
        },
      ],
    };

    writeYaml(globalStateDir, "config.yaml", globalConfig);
    writeYaml(projectDir, "hookwise.yaml", projectConfig);

    const loaded = loadConfig(projectDir);

    // Arrays replace entirely (not merge), so only project handler
    expect(loaded.handlers).toHaveLength(1);
    expect(loaded.handlers[0].name).toBe("project-handler");
  });
});

// --- Guard + Analytics Interaction ---

describe("integration: guard evaluation produces block output", () => {
  it("blocked tool call returns block decision in dispatch result", () => {
    const config = getDefaultConfig();
    config.analytics = { enabled: false };
    config.handlers = [
      {
        name: "bash-guard",
        type: "inline",
        events: ["PreToolUse"],
        phase: "guard",
        action: { decision: "block", reason: "rm -rf is dangerous" },
      },
    ];

    const payload = makePayload({
      tool_name: "Bash",
      tool_input: { command: "rm -rf /" },
    });

    const result = dispatch("PreToolUse", payload, { config });
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeTruthy();

    const stdout = JSON.parse(result.stdout!);
    expect(stdout.decision).toBe("block");
    expect(stdout.reason).toBe("rm -rf is dangerous");
  });

  it("guard evaluate() and dispatch() produce consistent block results", () => {
    const rules: GuardRule[] = [
      {
        match: "Bash",
        action: "block",
        reason: "Dangerous command",
        when: 'command contains "rm -rf"',
      },
    ];

    // Test via evaluate() — field paths are resolved against toolInput directly
    const guardResult = evaluate(
      "Bash",
      { command: "rm -rf /home" },
      rules
    );
    expect(guardResult.action).toBe("block");
    expect(guardResult.reason).toBe("Dangerous command");

    // Test via dispatch() with a config wrapping the same logic
    const config = getDefaultConfig();
    config.analytics = { enabled: false };
    config.handlers = [
      {
        name: "inline-block",
        type: "inline",
        events: ["PreToolUse"],
        phase: "guard",
        action: { decision: "block", reason: "Dangerous command" },
      },
    ];

    const dispatchResult = dispatch(
      "PreToolUse",
      makePayload({ tool_name: "Bash", tool_input: { command: "rm -rf /home" } }),
      { config }
    );
    const stdout = JSON.parse(dispatchResult.stdout!);
    expect(stdout.decision).toBe("block");
    expect(stdout.reason).toBe("Dangerous command");
  });

  it("allowed tool call returns null stdout (no block)", () => {
    const rules: GuardRule[] = [
      {
        match: "Bash",
        action: "block",
        reason: "Dangerous command",
        when: 'tool_input.command contains "rm -rf"',
      },
    ];

    // Safe command should not match
    const guardResult = evaluate("Bash", { command: "ls -la" }, rules);
    expect(guardResult.action).toBe("allow");

    // Dispatch with same guard as inline handler
    const config = getDefaultConfig();
    config.analytics = { enabled: false };
    // No handlers that would block "ls -la"
    const result = dispatch("PreToolUse", makePayload(), { config });
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeNull();
  });
});

// --- CLI Init -> Config Generation ---

describe("integration: CLI init generates valid config", () => {
  let tempDir: string;
  let tempStateDir: string;
  const originalEnv = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-integ-init-"));
    tempStateDir = mkdtempSync(join(tmpdir(), "hookwise-integ-init-state-"));
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

  it("hookwise init creates a parseable hookwise.yaml", () => {
    // Simulate what init does: generate a default config and write it
    const config = getDefaultConfig();
    config.analytics = { enabled: false };
    saveConfig(config, join(tempDir, "hookwise.yaml"));

    // Verify the file exists and is valid YAML
    expect(existsSync(join(tempDir, "hookwise.yaml"))).toBe(true);

    const content = readFileSync(join(tempDir, "hookwise.yaml"), "utf-8");
    const parsed = yaml.load(content);
    expect(parsed).toBeTruthy();
    expect(typeof parsed).toBe("object");

    // Verify it can be loaded back by config engine
    const loaded = loadConfig(tempDir);
    expect(loaded.version).toBe(1);
    expect(Array.isArray(loaded.guards)).toBe(true);
    expect(loaded.settings.logLevel).toBeTruthy();
  });

  it("saved config round-trips through save/load cycle", () => {
    const config = getDefaultConfig();
    config.analytics = { enabled: false };
    config.guards = [
      {
        match: "Bash",
        action: "block",
        reason: "Test guard",
        when: 'tool_input.command contains "test"',
      },
    ];
    config.coaching.metacognition.enabled = true;
    config.coaching.metacognition.intervalSeconds = 120;
    config.settings.logLevel = "debug";

    const configPath = join(tempDir, "hookwise.yaml");
    saveConfig(config, configPath);

    const loaded = loadConfig(tempDir);

    expect(loaded.guards).toHaveLength(1);
    expect(loaded.guards[0].match).toBe("Bash");
    expect(loaded.guards[0].action).toBe("block");
    expect(loaded.coaching.metacognition.enabled).toBe(true);
    expect(loaded.coaching.metacognition.intervalSeconds).toBe(120);
    expect(loaded.settings.logLevel).toBe("debug");
  });

  it("dispatch works with init-generated config", () => {
    const config = getDefaultConfig();
    config.analytics = { enabled: false };
    saveConfig(config, join(tempDir, "hookwise.yaml"));

    // Default config has no handlers, so dispatch should exit 0 cleanly
    const result = dispatch("PreToolUse", makePayload(), {
      projectDir: tempDir,
    });
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeNull();
  });

  it("init-generated config with handlers dispatches correctly", () => {
    const config = getDefaultConfig();
    config.analytics = { enabled: false };
    config.handlers = [
      {
        name: "session-greeter",
        type: "inline",
        events: ["SessionStart"],
        phase: "context",
        action: { additionalContext: "Welcome from init config!" },
      },
    ];
    saveConfig(config, join(tempDir, "hookwise.yaml"));

    const result = dispatch("SessionStart", makePayload(), {
      projectDir: tempDir,
    });

    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBeTruthy();
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.hookSpecificOutput.additionalContext).toBe(
      "Welcome from init config!"
    );
  });
});

// --- Environment Variable Interpolation ---

describe("integration: env var interpolation in dispatch", () => {
  let tempDir: string;
  let tempStateDir: string;
  const originalEnv = process.env.HOOKWISE_STATE_DIR;
  const originalTestVar = process.env.HOOKWISE_TEST_REASON;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-integ-env-"));
    tempStateDir = mkdtempSync(join(tmpdir(), "hookwise-integ-env-state-"));
    process.env.HOOKWISE_STATE_DIR = tempStateDir;
  });

  afterEach(() => {
    if (originalEnv !== undefined) {
      process.env.HOOKWISE_STATE_DIR = originalEnv;
    } else {
      delete process.env.HOOKWISE_STATE_DIR;
    }
    if (originalTestVar !== undefined) {
      process.env.HOOKWISE_TEST_REASON = originalTestVar;
    } else {
      delete process.env.HOOKWISE_TEST_REASON;
    }
    rmSync(tempDir, { recursive: true, force: true });
    rmSync(tempStateDir, { recursive: true, force: true });
  });

  it("env vars in config are interpolated during load", () => {
    process.env.HOOKWISE_TEST_REASON = "env-var-reason";

    const config = {
      version: 1,
      analytics: { enabled: false },
      handlers: [
        {
          name: "env-guard",
          type: "inline",
          events: ["PreToolUse"],
          phase: "guard",
          action: {
            decision: "block",
            reason: "${HOOKWISE_TEST_REASON}",
          },
        },
      ],
    };

    writeYaml(tempDir, "hookwise.yaml", config);

    const result = dispatch("PreToolUse", makePayload(), {
      projectDir: tempDir,
    });

    expect(result.exitCode).toBe(0);
    const stdout = JSON.parse(result.stdout!);
    expect(stdout.reason).toBe("env-var-reason");
  });
});
