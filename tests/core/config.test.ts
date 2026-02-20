/**
 * Tests for config loading, validation, handler resolution, and write-back.
 *
 * Covers Tasks 3.1-3.4:
 * - YAML loading and deep merge (project overrides global)
 * - snake_case to camelCase mapping
 * - Environment variable interpolation
 * - Missing config returns sensible defaults
 * - Config validation with path+suggestion errors
 * - Handler type resolution (builtin, script, inline)
 * - Event filtering and phase ordering
 * - Include resolution
 * - Round-trip save/load (camelCase-to-snake_case on write)
 * - v0.1.0 backward compatibility
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import {
  mkdtempSync,
  mkdirSync,
  writeFileSync,
  readFileSync,
  existsSync,
  rmSync,
} from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { tmpdir } from "node:os";
import yaml from "js-yaml";
import {
  loadConfig,
  validateConfig,
  resolveHandlers,
  getHandlersForEvent,
  saveConfig,
  getDefaultConfig,
  deepMerge,
  interpolateEnvVars,
  snakeToCamel,
  camelToSnake,
  deepSnakeToCamel,
  deepCamelToSnake,
} from "../../src/core/config.js";
import type { HooksConfig, EventType } from "../../src/core/types.js";
import { DEFAULT_STATE_DIR, DEFAULT_HANDLER_TIMEOUT } from "../../src/core/constants.js";

// --- Helper: create a temp dir with hookwise.yaml ---

function createTempProjectDir(
  yamlContent: string,
  filename = "hookwise.yaml"
): string {
  const dir = mkdtempSync(join(tmpdir(), "hookwise-config-test-"));
  writeFileSync(join(dir, filename), yamlContent, "utf-8");
  return dir;
}

function createGlobalConfig(
  tempStateDir: string,
  yamlContent: string
): void {
  writeFileSync(join(tempStateDir, "config.yaml"), yamlContent, "utf-8");
}

// --- Task 3.1: YAML config loading and deep merge ---

describe("snakeToCamel / camelToSnake", () => {
  it("converts snake_case to camelCase", () => {
    expect(snakeToCamel("interval_seconds")).toBe("intervalSeconds");
    expect(snakeToCamel("cost_tracking")).toBe("costTracking");
    expect(snakeToCamel("handler_timeout_seconds")).toBe("handlerTimeoutSeconds");
  });

  it("leaves already camelCase unchanged", () => {
    expect(snakeToCamel("intervalSeconds")).toBe("intervalSeconds");
    expect(snakeToCamel("version")).toBe("version");
  });

  it("converts camelCase to snake_case", () => {
    expect(camelToSnake("intervalSeconds")).toBe("interval_seconds");
    expect(camelToSnake("costTracking")).toBe("cost_tracking");
    expect(camelToSnake("handlerTimeoutSeconds")).toBe("handler_timeout_seconds");
  });

  it("leaves already snake_case unchanged", () => {
    expect(camelToSnake("interval_seconds")).toBe("interval_seconds");
    expect(camelToSnake("version")).toBe("version");
  });
});

describe("deepSnakeToCamel / deepCamelToSnake", () => {
  it("converts all keys recursively", () => {
    const input = {
      interval_seconds: 300,
      cost_tracking: { daily_budget: 10, is_enabled: true },
    };
    const result = deepSnakeToCamel(input) as Record<string, unknown>;
    expect(result.intervalSeconds).toBe(300);
    const ct = result.costTracking as Record<string, unknown>;
    expect(ct.dailyBudget).toBe(10);
    expect(ct.isEnabled).toBe(true);
  });

  it("handles arrays (items converted, not keys)", () => {
    const input = { my_list: [{ item_name: "a" }, { item_name: "b" }] };
    const result = deepSnakeToCamel(input) as Record<string, unknown>;
    const list = result.myList as Record<string, unknown>[];
    expect(list[0].itemName).toBe("a");
    expect(list[1].itemName).toBe("b");
  });

  it("round-trips through deepSnakeToCamel then deepCamelToSnake", () => {
    const input = { interval_seconds: 300, log_level: "debug" };
    const camel = deepSnakeToCamel(input);
    const snake = deepCamelToSnake(camel);
    expect(snake).toEqual(input);
  });

  it("handles primitives (pass-through)", () => {
    expect(deepSnakeToCamel("hello")).toBe("hello");
    expect(deepSnakeToCamel(42)).toBe(42);
    expect(deepSnakeToCamel(null)).toBeNull();
    expect(deepSnakeToCamel(undefined)).toBeUndefined();
  });
});

describe("deepMerge", () => {
  it("merges nested objects recursively", () => {
    const target = { coaching: { metacognition: { enabled: false, interval: 300 } } };
    const source = { coaching: { metacognition: { enabled: true } } };
    const result = deepMerge(target, source);
    const mc = (result.coaching as Record<string, unknown>).metacognition as Record<string, unknown>;
    expect(mc.enabled).toBe(true);
    expect(mc.interval).toBe(300); // preserved from target
  });

  it("replaces arrays entirely (no concatenation)", () => {
    const target = { guards: [{ match: "old" }] };
    const source = { guards: [{ match: "new1" }, { match: "new2" }] };
    const result = deepMerge(target, source);
    expect(result.guards).toEqual([{ match: "new1" }, { match: "new2" }]);
  });

  it("source primitive values override target", () => {
    const target = { version: 1, name: "old" };
    const source = { version: 2 };
    const result = deepMerge(target, source);
    expect(result.version).toBe(2);
    expect(result.name).toBe("old");
  });

  it("adds new keys from source", () => {
    const target = { a: 1 };
    const source = { b: 2 };
    const result = deepMerge(target, source);
    expect(result.a).toBe(1);
    expect(result.b).toBe(2);
  });

  it("handles null values in source", () => {
    const target = { key: "value" };
    const source = { key: null };
    const result = deepMerge(target, source);
    expect(result.key).toBeNull();
  });
});

describe("interpolateEnvVars", () => {
  const originalEnv: Record<string, string | undefined> = {};

  beforeEach(() => {
    originalEnv.MY_VAR = process.env.MY_VAR;
    originalEnv.DB_PATH = process.env.DB_PATH;
    process.env.MY_VAR = "resolved_value";
    process.env.DB_PATH = "/custom/path";
  });

  afterEach(() => {
    for (const [key, val] of Object.entries(originalEnv)) {
      if (val === undefined) {
        delete process.env[key];
      } else {
        process.env[key] = val;
      }
    }
  });

  it("substitutes ${VAR} in string values", () => {
    expect(interpolateEnvVars("${MY_VAR}")).toBe("resolved_value");
  });

  it("substitutes multiple vars in one string", () => {
    expect(interpolateEnvVars("path: ${DB_PATH} and ${MY_VAR}")).toBe(
      "path: /custom/path and resolved_value"
    );
  });

  it("leaves literal ${VAR} when env var is undefined", () => {
    expect(interpolateEnvVars("${NONEXISTENT_VAR_12345}")).toBe(
      "${NONEXISTENT_VAR_12345}"
    );
  });

  it("interpolates recursively in objects", () => {
    const input = { path: "${DB_PATH}", nested: { name: "${MY_VAR}" } };
    const result = interpolateEnvVars(input) as Record<string, unknown>;
    expect(result.path).toBe("/custom/path");
    expect((result.nested as Record<string, unknown>).name).toBe("resolved_value");
  });

  it("interpolates in arrays", () => {
    const input = ["${MY_VAR}", "plain"];
    const result = interpolateEnvVars(input);
    expect(result).toEqual(["resolved_value", "plain"]);
  });

  it("passes through non-string primitives", () => {
    expect(interpolateEnvVars(42)).toBe(42);
    expect(interpolateEnvVars(true)).toBe(true);
    expect(interpolateEnvVars(null)).toBeNull();
  });
});

describe("loadConfig", () => {
  let tempDir: string;
  let tempStateDir: string;
  const originalEnv = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-config-test-"));
    tempStateDir = mkdtempSync(join(tmpdir(), "hookwise-state-test-"));
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

  it("returns defaults when no config file exists", () => {
    const config = loadConfig(tempDir);
    const defaults = getDefaultConfig();
    expect(config.version).toBe(defaults.version);
    expect(config.guards).toEqual(defaults.guards);
    expect(config.coaching.metacognition.enabled).toBe(false);
    expect(config.settings.logLevel).toBe("info");
    expect(config.handlers).toEqual([]);
    expect(config.includes).toEqual([]);
  });

  it("loads project-level hookwise.yaml", () => {
    const yamlContent = yaml.dump({
      version: 1,
      coaching: { metacognition: { enabled: true, interval_seconds: 600 } },
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), yamlContent);

    const config = loadConfig(tempDir);
    expect(config.coaching.metacognition.enabled).toBe(true);
    expect(config.coaching.metacognition.intervalSeconds).toBe(600);
  });

  it("loads global config from HOOKWISE_STATE_DIR/config.yaml", () => {
    const yamlContent = yaml.dump({
      version: 1,
      coaching: { metacognition: { enabled: true } },
    });
    createGlobalConfig(tempStateDir, yamlContent);

    const config = loadConfig(tempDir);
    expect(config.coaching.metacognition.enabled).toBe(true);
  });

  it("project config overrides global config (deep merge)", () => {
    const globalYaml = yaml.dump({
      version: 1,
      coaching: {
        metacognition: { enabled: true, interval_seconds: 300 },
        builder_trap: { enabled: true },
      },
      settings: { log_level: "debug" },
    });
    createGlobalConfig(tempStateDir, globalYaml);

    const projectYaml = yaml.dump({
      coaching: {
        metacognition: { interval_seconds: 600 },
      },
      settings: { log_level: "warn" },
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), projectYaml);

    const config = loadConfig(tempDir);
    // Project overrides
    expect(config.coaching.metacognition.intervalSeconds).toBe(600);
    expect(config.settings.logLevel).toBe("warn");
    // Global preserved where not overridden
    expect(config.coaching.metacognition.enabled).toBe(true);
    expect(config.coaching.builderTrap.enabled).toBe(true);
  });

  it("deep merge preserves nested defaults when partial config given", () => {
    const yamlContent = yaml.dump({
      version: 1,
      coaching: { metacognition: { enabled: true } },
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), yamlContent);

    const config = loadConfig(tempDir);
    // Defaults preserved for unspecified fields
    expect(config.coaching.builderTrap.thresholds.yellow).toBe(30);
    expect(config.coaching.communication.tone).toBe("gentle");
    expect(config.settings.handlerTimeoutSeconds).toBe(DEFAULT_HANDLER_TIMEOUT);
  });

  it("converts snake_case YAML keys to camelCase", () => {
    const yamlContent = yaml.dump({
      version: 1,
      cost_tracking: { daily_budget: 20, enforcement: "enforce" },
      transcript_backup: { backup_dir: "/tmp/backups", max_size_mb: 50 },
      settings: { log_level: "debug", handler_timeout_seconds: 30 },
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), yamlContent);

    const config = loadConfig(tempDir);
    expect(config.costTracking.dailyBudget).toBe(20);
    expect(config.costTracking.enforcement).toBe("enforce");
    expect(config.transcriptBackup.backupDir).toBe("/tmp/backups");
    expect(config.transcriptBackup.maxSizeMb).toBe(50);
    expect(config.settings.logLevel).toBe("debug");
    expect(config.settings.handlerTimeoutSeconds).toBe(30);
  });

  it("interpolates environment variables in string values", () => {
    const envKey = "HOOKWISE_TEST_PATH_" + Date.now();
    process.env[envKey] = "/custom/test/path";

    try {
      const yamlContent = yaml.dump({
        version: 1,
        transcript_backup: { backup_dir: `\${${envKey}}` },
      });
      writeFileSync(join(tempDir, "hookwise.yaml"), yamlContent);

      const config = loadConfig(tempDir);
      expect(config.transcriptBackup.backupDir).toBe("/custom/test/path");
    } finally {
      delete process.env[envKey];
    }
  });

  it("leaves undefined env vars as literals", () => {
    const yamlContent = yaml.dump({
      version: 1,
      transcript_backup: { backup_dir: "${DEFINITELY_DOES_NOT_EXIST_XYZ}" },
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), yamlContent);

    const config = loadConfig(tempDir);
    expect(config.transcriptBackup.backupDir).toBe(
      "${DEFINITELY_DOES_NOT_EXIST_XYZ}"
    );
  });

  it("resolves includes from project config", () => {
    const recipeDir = join(tempDir, "recipes");
    mkdirSync(recipeDir);
    const recipeYaml = yaml.dump({
      guards: [{ match: "tool_name:Bash", action: "warn", reason: "Be careful" }],
    });
    writeFileSync(join(recipeDir, "safety.yaml"), recipeYaml);

    const projectYaml = yaml.dump({
      version: 1,
      includes: ["./recipes/safety.yaml"],
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), projectYaml);

    const config = loadConfig(tempDir);
    expect(config.guards.length).toBe(1);
    expect((config.guards[0] as Record<string, unknown>).match).toBe("tool_name:Bash");
  });

  it("resolves directory includes as recipes (hooks.yaml)", () => {
    // Create a recipe directory with hooks.yaml
    const recipeDir = join(tempDir, "recipes", "safety", "block-test");
    mkdirSync(recipeDir, { recursive: true });
    writeFileSync(
      join(recipeDir, "hooks.yaml"),
      yaml.dump({
        name: "block-test",
        version: "1.0.0",
        events: ["PreToolUse"],
        config: {},
        guards: [
          { match: "tool_name:Bash", action: "block", reason: "Blocked by recipe" },
        ],
      }),
      "utf-8"
    );

    const projectYaml = yaml.dump({
      version: 1,
      includes: ["./recipes/safety/block-test"],
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), projectYaml);

    const config = loadConfig(tempDir);
    expect(config.guards.length).toBe(1);
    expect((config.guards[0] as Record<string, unknown>).match).toBe("tool_name:Bash");
    expect((config.guards[0] as Record<string, unknown>).action).toBe("block");
    expect((config.guards[0] as Record<string, unknown>).reason).toBe("Blocked by recipe");
  });

  it("resolves directory includes with config merging", () => {
    // Create a recipe directory with hooks.yaml that has config fields
    const recipeDir = join(tempDir, "recipes", "cost-recipe");
    mkdirSync(recipeDir, { recursive: true });
    writeFileSync(
      join(recipeDir, "hooks.yaml"),
      yaml.dump({
        name: "cost-recipe",
        version: "1.0.0",
        events: ["PostToolUse"],
        config: {
          cost_tracking: { enabled: true, daily_budget: 5 },
        },
      }),
      "utf-8"
    );

    const projectYaml = yaml.dump({
      version: 1,
      includes: ["./recipes/cost-recipe"],
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), projectYaml);

    const config = loadConfig(tempDir);
    // Recipe config fields merged into config
    expect(config.costTracking.dailyBudget).toBe(5);
  });

  it("gracefully handles directory include without hooks.yaml", () => {
    // Create an empty directory (no hooks.yaml or hooks.json)
    const emptyRecipeDir = join(tempDir, "recipes", "empty-recipe");
    mkdirSync(emptyRecipeDir, { recursive: true });

    const projectYaml = yaml.dump({
      version: 1,
      includes: ["./recipes/empty-recipe"],
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), projectYaml);

    // Should not throw, just skip the include
    const config = loadConfig(tempDir);
    expect(config.version).toBe(1);
  });

  it("mixes file and directory includes", () => {
    // File include (adds coaching config via deepMerge)
    const fileDir = join(tempDir, "partials");
    mkdirSync(fileDir, { recursive: true });
    writeFileSync(
      join(fileDir, "extra.yaml"),
      yaml.dump({
        coaching: { metacognition: { enabled: true } },
      }),
      "utf-8"
    );

    // Directory include (recipe with guards)
    const recipeDir = join(tempDir, "recipes", "test-recipe");
    mkdirSync(recipeDir, { recursive: true });
    writeFileSync(
      join(recipeDir, "hooks.yaml"),
      yaml.dump({
        name: "test-recipe",
        version: "1.0.0",
        events: ["PreToolUse"],
        config: {},
        guards: [
          { match: "tool_name:Bash", action: "block", reason: "Recipe guard" },
        ],
      }),
      "utf-8"
    );

    const projectYaml = yaml.dump({
      version: 1,
      includes: ["./recipes/test-recipe", "./partials/extra.yaml"],
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), projectYaml);

    const config = loadConfig(tempDir);
    // Recipe guard prepended
    expect(config.guards.length).toBe(1);
    expect((config.guards[0] as Record<string, unknown>).match).toBe("tool_name:Bash");
    // File include merged coaching config
    expect(config.coaching.metacognition.enabled).toBe(true);
  });
});

// --- Task 3.2: Config validation and handler resolution ---

describe("validateConfig", () => {
  it("returns valid for a correct minimal config", () => {
    const result = validateConfig({ version: 1 });
    expect(result.valid).toBe(true);
    expect(result.errors).toHaveLength(0);
  });

  it("returns valid for an empty config", () => {
    const result = validateConfig({});
    expect(result.valid).toBe(true);
  });

  it("reports unknown top-level sections", () => {
    const result = validateConfig({ version: 1, unknown_section: true });
    expect(result.valid).toBe(false);
    expect(result.errors.length).toBeGreaterThan(0);
    expect(result.errors[0].path).toBe("unknown_section");
    expect(result.errors[0].message).toContain("Unknown config section");
    expect(result.errors[0].suggestion).toBeDefined();
  });

  it("reports invalid version", () => {
    const result = validateConfig({ version: -1 });
    expect(result.valid).toBe(false);
    expect(result.errors[0].path).toBe("version");
  });

  it("reports non-numeric version", () => {
    const result = validateConfig({ version: "abc" });
    expect(result.valid).toBe(false);
    expect(result.errors[0].path).toBe("version");
  });

  it("reports guards that are not an array", () => {
    const result = validateConfig({ guards: "not-array" });
    expect(result.valid).toBe(false);
    expect(result.errors[0].path).toBe("guards");
  });

  it("reports guard missing match field", () => {
    const result = validateConfig({
      guards: [{ action: "block", reason: "test" }],
    });
    expect(result.valid).toBe(false);
    expect(result.errors[0].path).toBe("guards[0].match");
  });

  it("reports guard with invalid action", () => {
    const result = validateConfig({
      guards: [{ match: "Bash", action: "deny", reason: "test" }],
    });
    expect(result.valid).toBe(false);
    expect(result.errors[0].path).toBe("guards[0].action");
  });

  it("reports guard missing reason", () => {
    const result = validateConfig({
      guards: [{ match: "Bash", action: "block" }],
    });
    expect(result.valid).toBe(false);
    expect(result.errors[0].path).toBe("guards[0].reason");
  });

  it("validates correct guard rules", () => {
    const result = validateConfig({
      guards: [{ match: "tool_name:Bash", action: "block", reason: "No bash" }],
    });
    expect(result.valid).toBe(true);
  });

  it("reports handlers that are not an array", () => {
    const result = validateConfig({ handlers: {} });
    expect(result.valid).toBe(false);
    expect(result.errors[0].path).toBe("handlers");
  });

  it("reports handler missing name", () => {
    const result = validateConfig({
      handlers: [{ type: "builtin", events: ["PreToolUse"] }],
    });
    expect(result.valid).toBe(false);
    expect(result.errors[0].path).toBe("handlers[0].name");
  });

  it("reports handler with invalid type", () => {
    const result = validateConfig({
      handlers: [{ name: "test", type: "unknown", events: ["PreToolUse"] }],
    });
    expect(result.valid).toBe(false);
    expect(result.errors[0].path).toBe("handlers[0].type");
  });

  it("reports handler missing events", () => {
    const result = validateConfig({
      handlers: [{ name: "test", type: "builtin" }],
    });
    expect(result.valid).toBe(false);
    expect(result.errors[0].path).toBe("handlers[0].events");
  });

  it("reports invalid coaching.metacognition.interval_seconds", () => {
    const result = validateConfig({
      coaching: { metacognition: { interval_seconds: -10 } },
    });
    expect(result.valid).toBe(false);
    expect(result.errors[0].path).toContain("interval_seconds");
  });

  it("reports invalid settings.log_level", () => {
    const result = validateConfig({
      settings: { log_level: "verbose" },
    });
    expect(result.valid).toBe(false);
    expect(result.errors[0].path).toContain("log_level");
  });

  it("reports includes that is not an array", () => {
    const result = validateConfig({ includes: "file.yaml" });
    expect(result.valid).toBe(false);
    expect(result.errors[0].path).toBe("includes");
  });

  it("accumulates multiple errors", () => {
    const result = validateConfig({
      version: "bad",
      guards: "not-array",
      unknown_key: true,
    });
    expect(result.valid).toBe(false);
    expect(result.errors.length).toBe(3);
  });

  it("accepts all valid snake_case top-level keys", () => {
    const result = validateConfig({
      version: 1,
      guards: [],
      coaching: {},
      analytics: {},
      greeting: {},
      sounds: {},
      status_line: {},
      cost_tracking: {},
      transcript_backup: {},
      handlers: [],
      settings: {},
      includes: [],
    });
    expect(result.valid).toBe(true);
  });
});

describe("resolveHandlers", () => {
  it("resolves builtin handler", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "test-builtin",
        type: "builtin",
        events: ["PreToolUse"],
        module: "guards/safety",
      },
    ];

    const handlers = resolveHandlers(config);
    expect(handlers).toHaveLength(1);
    expect(handlers[0].name).toBe("test-builtin");
    expect(handlers[0].handlerType).toBe("builtin");
    expect(handlers[0].module).toBe("guards/safety");
    expect(handlers[0].events.has("PreToolUse")).toBe(true);
  });

  it("resolves script handler", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "test-script",
        type: "script",
        events: ["Stop"],
        command: "node scripts/backup.js",
        phase: "side_effect",
      },
    ];

    const handlers = resolveHandlers(config);
    expect(handlers).toHaveLength(1);
    expect(handlers[0].handlerType).toBe("script");
    expect(handlers[0].command).toBe("node scripts/backup.js");
    expect(handlers[0].phase).toBe("side_effect");
  });

  it("resolves inline handler", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "test-inline",
        type: "inline",
        events: ["SessionStart"],
        action: { additionalContext: "Welcome!" },
      },
    ];

    const handlers = resolveHandlers(config);
    expect(handlers).toHaveLength(1);
    expect(handlers[0].handlerType).toBe("inline");
    expect(handlers[0].action).toEqual({ additionalContext: "Welcome!" });
  });

  it("wildcard events resolves to all event types", () => {
    const config = getDefaultConfig();
    config.handlers = [
      { name: "log-all", type: "builtin", events: "*", module: "logging" },
    ];

    const handlers = resolveHandlers(config);
    expect(handlers[0].events.size).toBe(13);
    expect(handlers[0].events.has("PreToolUse")).toBe(true);
    expect(handlers[0].events.has("Stop")).toBe(true);
  });

  it("uses configurable timeout", () => {
    const config = getDefaultConfig();
    config.handlers = [
      { name: "slow", type: "script", events: ["Stop"], command: "slow.sh", timeout: 30 },
    ];

    const handlers = resolveHandlers(config);
    expect(handlers[0].timeout).toBe(30000); // seconds -> ms
  });

  it("uses default timeout from settings", () => {
    const config = getDefaultConfig();
    config.settings.handlerTimeoutSeconds = 15;
    config.handlers = [
      { name: "default-timeout", type: "script", events: ["Stop"], command: "test.sh" },
    ];

    const handlers = resolveHandlers(config);
    expect(handlers[0].timeout).toBe(15000);
  });

  it("infers guard phase for PreToolUse handlers", () => {
    const config = getDefaultConfig();
    config.handlers = [
      { name: "guard-check", type: "builtin", events: ["PreToolUse"], module: "guard" },
    ];

    const handlers = resolveHandlers(config);
    expect(handlers[0].phase).toBe("guard");
  });

  it("infers context phase for SessionStart handlers", () => {
    const config = getDefaultConfig();
    config.handlers = [
      { name: "greeting", type: "inline", events: ["SessionStart"], action: { additionalContext: "Hi" } },
    ];

    const handlers = resolveHandlers(config);
    expect(handlers[0].phase).toBe("context");
  });

  it("defaults to side_effect phase", () => {
    const config = getDefaultConfig();
    config.handlers = [
      { name: "analytics", type: "builtin", events: ["Stop"], module: "analytics" },
    ];

    const handlers = resolveHandlers(config);
    expect(handlers[0].phase).toBe("side_effect");
  });

  it("explicit phase overrides inferred", () => {
    const config = getDefaultConfig();
    config.handlers = [
      {
        name: "custom-phase",
        type: "builtin",
        events: ["PreToolUse"],
        module: "custom",
        phase: "side_effect",
      },
    ];

    const handlers = resolveHandlers(config);
    expect(handlers[0].phase).toBe("side_effect");
  });
});

describe("getHandlersForEvent", () => {
  it("filters handlers to matching event type", () => {
    const config = getDefaultConfig();
    config.handlers = [
      { name: "h1", type: "builtin", events: ["PreToolUse"], module: "a" },
      { name: "h2", type: "builtin", events: ["Stop"], module: "b" },
      { name: "h3", type: "builtin", events: ["PreToolUse", "PostToolUse"], module: "c" },
    ];

    const handlers = getHandlersForEvent(config, "PreToolUse");
    expect(handlers).toHaveLength(2);
    expect(handlers.map((h) => h.name)).toContain("h1");
    expect(handlers.map((h) => h.name)).toContain("h3");
  });

  it("orders by phase: guard, context, side_effect", () => {
    const config = getDefaultConfig();
    config.handlers = [
      { name: "analytics", type: "builtin", events: ["PreToolUse"], module: "a", phase: "side_effect" },
      { name: "guard", type: "builtin", events: ["PreToolUse"], module: "b", phase: "guard" },
      { name: "context", type: "builtin", events: ["PreToolUse"], module: "c", phase: "context" },
    ];

    const handlers = getHandlersForEvent(config, "PreToolUse");
    expect(handlers.map((h) => h.name)).toEqual(["guard", "context", "analytics"]);
  });

  it("returns empty array when no handlers match", () => {
    const config = getDefaultConfig();
    config.handlers = [
      { name: "h1", type: "builtin", events: ["Stop"], module: "a" },
    ];

    const handlers = getHandlersForEvent(config, "PreToolUse");
    expect(handlers).toHaveLength(0);
  });

  it("wildcard handler matches any event", () => {
    const config = getDefaultConfig();
    config.handlers = [
      { name: "log-all", type: "builtin", events: "*", module: "logging" },
    ];

    const handlers = getHandlersForEvent(config, "Stop");
    expect(handlers).toHaveLength(1);
    expect(handlers[0].name).toBe("log-all");
  });
});

// --- Task 3.3: Config write-back for TUI persistence ---

describe("saveConfig", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-save-test-"));
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("writes a YAML file", () => {
    const config = getDefaultConfig();
    const filePath = join(tempDir, "output.yaml");
    saveConfig(config, filePath);
    expect(existsSync(filePath)).toBe(true);
  });

  it("converts camelCase to snake_case on write", () => {
    const config = getDefaultConfig();
    config.costTracking.dailyBudget = 25;
    const filePath = join(tempDir, "output.yaml");
    saveConfig(config, filePath);

    const content = readFileSync(filePath, "utf-8");
    expect(content).toContain("daily_budget:");
    expect(content).toContain("25");
    expect(content).not.toContain("dailyBudget:");
  });

  it("round-trip: load -> modify -> save -> reload -> verify", () => {
    // Create an initial config YAML
    const initialYaml = yaml.dump({
      version: 1,
      coaching: { metacognition: { enabled: true, interval_seconds: 300 } },
    });
    const projectDir = mkdtempSync(join(tmpdir(), "hookwise-roundtrip-"));

    try {
      writeFileSync(join(projectDir, "hookwise.yaml"), initialYaml);

      // Load
      const stateDir = mkdtempSync(join(tmpdir(), "hookwise-state-rt-"));
      const origState = process.env.HOOKWISE_STATE_DIR;
      process.env.HOOKWISE_STATE_DIR = stateDir;

      try {
        const config = loadConfig(projectDir);
        expect(config.coaching.metacognition.enabled).toBe(true);

        // Modify
        config.coaching.metacognition.intervalSeconds = 600;
        config.costTracking.dailyBudget = 50;

        // Save
        const savePath = join(projectDir, "hookwise.yaml");
        saveConfig(config, savePath);

        // Reload
        const reloaded = loadConfig(projectDir);
        expect(reloaded.coaching.metacognition.intervalSeconds).toBe(600);
        expect(reloaded.costTracking.dailyBudget).toBe(50);
        // Original values preserved
        expect(reloaded.coaching.metacognition.enabled).toBe(true);
      } finally {
        if (origState !== undefined) {
          process.env.HOOKWISE_STATE_DIR = origState;
        } else {
          delete process.env.HOOKWISE_STATE_DIR;
        }
        rmSync(stateDir, { recursive: true, force: true });
      }
    } finally {
      rmSync(projectDir, { recursive: true, force: true });
    }
  });

  it("creates parent directories if needed", () => {
    const filePath = join(tempDir, "nested", "dir", "config.yaml");
    const config = getDefaultConfig();
    saveConfig(config, filePath);
    expect(existsSync(filePath)).toBe(true);
  });

  it("atomic write: no temp files left behind", () => {
    const config = getDefaultConfig();
    saveConfig(config, join(tempDir, "atomic.yaml"));

    const files = readFileSync(join(tempDir, "atomic.yaml"), "utf-8");
    expect(files).toBeTruthy();

    // Check no temp files
    const { readdirSync } = require("node:fs");
    const dirFiles = readdirSync(tempDir) as string[];
    const tempFiles = dirFiles.filter((f: string) => f.startsWith(".tmp-"));
    expect(tempFiles).toHaveLength(0);
  });
});

// --- Task 3.4: v0.1.0 backward compatibility layer ---

describe("v0.1.0 backward compatibility", () => {
  let tempDir: string;
  let tempStateDir: string;
  const originalEnv = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-compat-test-"));
    tempStateDir = mkdtempSync(join(tmpdir(), "hookwise-state-compat-"));
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

  it("loads v0.1.0 config without modification", () => {
    // Simulate a v0.1.0 config file (uses snake_case, has guards and coaching)
    const v010Yaml = yaml.dump({
      version: 1,
      guards: [
        {
          match: "tool_name:Bash",
          action: "block",
          reason: "Bash blocked for safety",
          when: "tool_input.command contains 'rm -rf'",
        },
        {
          match: "tool_name:Write",
          action: "warn",
          reason: "Write operation detected",
        },
      ],
      coaching: {
        metacognition: { enabled: true, interval_seconds: 300 },
        builder_trap: {
          enabled: true,
          thresholds: { yellow: 30, orange: 60, red: 90 },
        },
      },
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), v010Yaml);

    const config = loadConfig(tempDir);
    expect(config.version).toBe(1);
    expect(config.guards).toHaveLength(2);
    expect(config.coaching.metacognition.enabled).toBe(true);
    expect(config.coaching.metacognition.intervalSeconds).toBe(300);
    expect(config.coaching.builderTrap.enabled).toBe(true);
    expect(config.coaching.builderTrap.thresholds.yellow).toBe(30);
    expect(config.coaching.builderTrap.thresholds.orange).toBe(60);
    expect(config.coaching.builderTrap.thresholds.red).toBe(90);
  });

  it("converts Python .py script handlers to python3 command", () => {
    const v010Yaml = yaml.dump({
      version: 1,
      handlers: [
        {
          name: "old-guard",
          type: "script",
          events: ["PreToolUse"],
          command: "hooks/guard.py",
        },
        {
          name: "already-python",
          type: "script",
          events: ["Stop"],
          command: "python3 hooks/analytics.py",
        },
        {
          name: "node-script",
          type: "script",
          events: ["PostToolUse"],
          command: "node hooks/post.js",
        },
      ],
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), v010Yaml);

    const config = loadConfig(tempDir);
    expect(config.handlers).toHaveLength(3);
    expect(config.handlers[0].command).toBe("python3 hooks/guard.py");
    expect(config.handlers[1].command).toBe("python3 hooks/analytics.py");
    expect(config.handlers[2].command).toBe("node hooks/post.js");
  });

  it("preserves guards from v0.1.0 format", () => {
    const v010Yaml = yaml.dump({
      version: 1,
      guards: [
        {
          match: "tool_name:Bash",
          action: "block",
          reason: "No bash",
          when: "tool_input.command contains 'sudo'",
          unless: "tool_input.command contains 'sudo ls'",
        },
      ],
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), v010Yaml);

    const config = loadConfig(tempDir);
    expect(config.guards).toHaveLength(1);
    const guard = config.guards[0];
    expect(guard.match).toBe("tool_name:Bash");
    expect(guard.action).toBe("block");
    expect(guard.reason).toBe("No bash");
    expect(guard.when).toBe("tool_input.command contains 'sudo'");
    expect(guard.unless).toBe("tool_input.command contains 'sudo ls'");
  });

  it("preserves coaching thresholds from v0.1.0", () => {
    const v010Yaml = yaml.dump({
      version: 1,
      coaching: {
        builder_trap: {
          enabled: true,
          thresholds: { yellow: 20, orange: 40, red: 60 },
          tooling_patterns: ["npm", "pip"],
          practice_tools: ["test", "lint"],
        },
      },
    });
    writeFileSync(join(tempDir, "hookwise.yaml"), v010Yaml);

    const config = loadConfig(tempDir);
    expect(config.coaching.builderTrap.thresholds).toEqual({
      yellow: 20,
      orange: 40,
      red: 60,
    });
    expect(config.coaching.builderTrap.toolingPatterns).toEqual(["npm", "pip"]);
    expect(config.coaching.builderTrap.practiceTools).toEqual(["test", "lint"]);
  });
});

// --- v0.1.0 fixture test ---

describe("v0.1.0 fixture file", () => {
  let tempDir: string;
  let tempStateDir: string;
  const originalEnv = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-fixture-test-"));
    tempStateDir = mkdtempSync(join(tmpdir(), "hookwise-state-fixture-"));
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

  it("loads v010-compat.yaml fixture correctly", () => {
    // Load the ACTUAL fixture file (source of truth for v0.1.0 compat)
    const currentDir = dirname(fileURLToPath(import.meta.url));
    const fixturePath = join(currentDir, "..", "fixtures", "v010-compat.yaml");
    const fixture = readFileSync(fixturePath, "utf-8");
    writeFileSync(join(tempDir, "hookwise.yaml"), fixture);

    const config = loadConfig(tempDir);

    // Guards preserved
    expect(config.guards).toHaveLength(2);
    expect(config.guards[0].match).toBe("tool_name:Bash");
    expect(config.guards[0].action).toBe("block");
    expect(config.guards[1].match).toBe("tool_name:Write");
    expect(config.guards[1].action).toBe("warn");

    // Coaching thresholds preserved
    expect(config.coaching.metacognition.enabled).toBe(true);
    expect(config.coaching.metacognition.intervalSeconds).toBe(300);
    expect(config.coaching.builderTrap.thresholds).toEqual({
      yellow: 30,
      orange: 60,
      red: 90,
    });

    // Python handlers converted
    expect(config.handlers[0].command).toBe("python3 hooks/guard.py");
    expect(config.handlers[1].command).toBe("python3 hooks/analytics.py");

    // Settings mapped
    expect(config.settings.logLevel).toBe("debug");
    expect(config.settings.handlerTimeoutSeconds).toBe(15);
  });
});
