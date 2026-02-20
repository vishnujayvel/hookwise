/**
 * Backward compatibility and distribution tests for hookwise v1.0
 *
 * Task 14.3: Verifies v0.1.0 config loading, example config validation,
 * and npm package structure.
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import {
  mkdtempSync,
  writeFileSync,
  readFileSync,
  copyFileSync,
  rmSync,
  existsSync,
} from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";
import yaml from "js-yaml";
import { loadConfig, validateConfig, getDefaultConfig } from "../../src/core/config.js";
import { evaluate } from "../../src/core/guards.js";
import type { HooksConfig } from "../../src/core/types.js";

const PROJECT_ROOT = resolve(process.cwd());

// --- v0.1.0 Backward Compatibility ---

describe("compat: v0.1.0 config loading", () => {
  let tempDir: string;
  let tempStateDir: string;
  const originalEnv = process.env.HOOKWISE_STATE_DIR;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-compat-"));
    tempStateDir = mkdtempSync(join(tmpdir(), "hookwise-compat-state-"));
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

  it("loads v010-compat.yaml fixture with all sections parsed correctly", () => {
    // Copy the fixture to our temp dir
    const fixturePath = join(PROJECT_ROOT, "tests", "fixtures", "v010-compat.yaml");
    expect(existsSync(fixturePath)).toBe(true);

    copyFileSync(fixturePath, join(tempDir, "hookwise.yaml"));
    const loaded = loadConfig(tempDir);

    // Guards section
    expect(loaded.guards).toHaveLength(2);
    expect(loaded.guards[0].match).toBe("tool_name:Bash");
    expect(loaded.guards[0].action).toBe("block");
    expect(loaded.guards[0].reason).toBe("Bash blocked for safety");
    expect(loaded.guards[0].when).toBe("tool_input.command contains 'rm -rf'");
    expect(loaded.guards[1].match).toBe("tool_name:Write");
    expect(loaded.guards[1].action).toBe("warn");

    // Coaching section with snake_case -> camelCase conversion
    expect(loaded.coaching.metacognition.enabled).toBe(true);
    expect(loaded.coaching.metacognition.intervalSeconds).toBe(300);
    expect(loaded.coaching.builderTrap.enabled).toBe(true);
    expect(loaded.coaching.builderTrap.thresholds.yellow).toBe(30);
    expect(loaded.coaching.builderTrap.thresholds.orange).toBe(60);
    expect(loaded.coaching.builderTrap.thresholds.red).toBe(90);
    expect(loaded.coaching.builderTrap.toolingPatterns).toEqual(["npm", "pip"]);
    expect(loaded.coaching.builderTrap.practiceTools).toEqual(["vitest", "pytest"]);

    // Settings section with snake_case -> camelCase
    expect(loaded.settings.logLevel).toBe("debug");
    expect(loaded.settings.handlerTimeoutSeconds).toBe(15);
  });

  it("v0.1.0 .py handlers get python3 prefix via compat transform", () => {
    const fixturePath = join(PROJECT_ROOT, "tests", "fixtures", "v010-compat.yaml");
    copyFileSync(fixturePath, join(tempDir, "hookwise.yaml"));
    const loaded = loadConfig(tempDir);

    // handlers array should have the .py handler with python3 prefix
    expect(loaded.handlers).toHaveLength(2);

    const pyHandler = loaded.handlers.find((h) => h.name === "old-python-guard");
    expect(pyHandler).toBeTruthy();
    expect(pyHandler!.command).toBe("python3 hooks/guard.py");

    // Handler that already has python3 prefix should be unchanged
    const analyticsHandler = loaded.handlers.find(
      (h) => h.name === "analytics-collector"
    );
    expect(analyticsHandler).toBeTruthy();
    expect(analyticsHandler!.command).toBe("python3 hooks/analytics.py");
  });

  it("v0.1.0 config validates without errors after loading", () => {
    const fixturePath = join(PROJECT_ROOT, "tests", "fixtures", "v010-compat.yaml");
    const content = readFileSync(fixturePath, "utf-8");
    const raw = yaml.load(content) as Record<string, unknown>;

    const validation = validateConfig(raw);
    expect(validation.valid).toBe(true);
    expect(validation.errors).toHaveLength(0);
  });

  it("v0.1.0 version field is preserved", () => {
    const fixturePath = join(PROJECT_ROOT, "tests", "fixtures", "v010-compat.yaml");
    copyFileSync(fixturePath, join(tempDir, "hookwise.yaml"));
    const loaded = loadConfig(tempDir);

    expect(loaded.version).toBe(1);
  });

  it("v0.1.0 guard with single-quoted when condition evaluates to block", () => {
    const fixturePath = join(PROJECT_ROOT, "tests", "fixtures", "v010-compat.yaml");
    copyFileSync(fixturePath, join(tempDir, "hookwise.yaml"));
    const loaded = loadConfig(tempDir);

    // The first guard: match "tool_name:Bash", when "tool_input.command contains 'rm -rf'"
    const result = evaluate(
      "tool_name:Bash",
      { tool_input: { command: "rm -rf /tmp/dangerous" } },
      loaded.guards
    );
    expect(result.action).toBe("block");
    expect(result.reason).toBe("Bash blocked for safety");
  });
});

// --- Example Configs ---

describe("compat: example configs are valid YAML and parseable", () => {
  const examplesDir = join(PROJECT_ROOT, "examples");

  const exampleFiles = ["minimal.yaml", "coaching.yaml", "analytics.yaml", "full.yaml"];

  for (const file of exampleFiles) {
    it(`examples/${file} is valid YAML`, () => {
      const filePath = join(examplesDir, file);
      expect(existsSync(filePath)).toBe(true);

      const content = readFileSync(filePath, "utf-8");
      const parsed = yaml.load(content);

      expect(parsed).toBeTruthy();
      expect(typeof parsed).toBe("object");
    });

    it(`examples/${file} has required 'version' field`, () => {
      const filePath = join(examplesDir, file);
      const content = readFileSync(filePath, "utf-8");
      const parsed = yaml.load(content) as Record<string, unknown>;

      expect(parsed.version).toBe(1);
    });

    it(`examples/${file} has a guards array`, () => {
      const filePath = join(examplesDir, file);
      const content = readFileSync(filePath, "utf-8");
      const parsed = yaml.load(content) as Record<string, unknown>;

      expect(Array.isArray(parsed.guards)).toBe(true);
      expect((parsed.guards as unknown[]).length).toBeGreaterThan(0);
    });
  }

  it("minimal.yaml loads as a valid HooksConfig", () => {
    let tempDir: string | null = null;
    let tempStateDir: string | null = null;
    const originalEnv = process.env.HOOKWISE_STATE_DIR;

    try {
      tempDir = mkdtempSync(join(tmpdir(), "hookwise-compat-example-"));
      tempStateDir = mkdtempSync(join(tmpdir(), "hookwise-compat-example-state-"));
      process.env.HOOKWISE_STATE_DIR = tempStateDir;

      copyFileSync(
        join(examplesDir, "minimal.yaml"),
        join(tempDir, "hookwise.yaml")
      );

      const loaded = loadConfig(tempDir);
      expect(loaded.version).toBe(1);
      expect(loaded.guards.length).toBeGreaterThan(0);
    } finally {
      if (originalEnv !== undefined) {
        process.env.HOOKWISE_STATE_DIR = originalEnv;
      } else {
        delete process.env.HOOKWISE_STATE_DIR;
      }
      if (tempDir) rmSync(tempDir, { recursive: true, force: true });
      if (tempStateDir) rmSync(tempStateDir, { recursive: true, force: true });
    }
  });
});

// --- npm Pack Structure ---

describe("compat: npm package structure", () => {
  it("package.json has correct files field", () => {
    const pkgPath = join(PROJECT_ROOT, "package.json");
    const pkg = JSON.parse(readFileSync(pkgPath, "utf-8"));

    expect(pkg.files).toBeDefined();
    expect(Array.isArray(pkg.files)).toBe(true);

    // Must include dist/, recipes/, and examples/
    expect(pkg.files).toContain("dist/");
    expect(pkg.files).toContain("recipes/");
    expect(pkg.files).toContain("examples/");
  });

  it("package.json has correct bin field", () => {
    const pkgPath = join(PROJECT_ROOT, "package.json");
    const pkg = JSON.parse(readFileSync(pkgPath, "utf-8"));

    expect(pkg.bin).toBeDefined();
    expect(pkg.bin.hookwise).toBe("./dist/bin/hookwise.js");
  });

  it("package.json has correct exports field", () => {
    const pkgPath = join(PROJECT_ROOT, "package.json");
    const pkg = JSON.parse(readFileSync(pkgPath, "utf-8"));

    expect(pkg.exports).toBeDefined();
    expect(pkg.exports["."]).toBe("./dist/index.js");
    expect(pkg.exports["./testing"]).toBe("./dist/testing/index.js");
  });

  it("package.json has type: module", () => {
    const pkgPath = join(PROJECT_ROOT, "package.json");
    const pkg = JSON.parse(readFileSync(pkgPath, "utf-8"));

    expect(pkg.type).toBe("module");
  });

  it("package.json has MIT license", () => {
    const pkgPath = join(PROJECT_ROOT, "package.json");
    const pkg = JSON.parse(readFileSync(pkgPath, "utf-8"));

    expect(pkg.license).toBe("MIT");
  });

  it("dist/ directory exists (build output)", () => {
    const distDir = join(PROJECT_ROOT, "dist");
    // dist/ should exist since we have tsup configured
    expect(existsSync(distDir)).toBe(true);
  });

  it("recipes/ directory exists with expected structure", () => {
    const recipesDir = join(PROJECT_ROOT, "recipes");
    expect(existsSync(recipesDir)).toBe(true);

    // Check for expected recipe categories
    const expectedCategories = [
      "safety",
      "behavioral",
      "compliance",
      "productivity",
    ];
    for (const cat of expectedCategories) {
      expect(existsSync(join(recipesDir, cat))).toBe(true);
    }

    // Check specific recipes have the required structure
    const expectedRecipes = [
      "safety/block-dangerous-commands",
      "safety/secret-scanning",
      "behavioral/ai-dependency-tracker",
      "behavioral/metacognition-prompts",
      "behavioral/builder-trap-detection",
      "compliance/cost-tracking",
      "productivity/transcript-backup",
      "gamification/streak-tracker",
      "productivity/context-window-monitor",
      "quality/commit-without-tests",
      "quality/file-creation-police",
    ];

    for (const recipe of expectedRecipes) {
      const recipePath = join(recipesDir, recipe);
      expect(existsSync(recipePath)).toBe(true);
      expect(existsSync(join(recipePath, "hooks.yaml"))).toBe(true);
      expect(existsSync(join(recipePath, "handler.ts"))).toBe(true);
      expect(existsSync(join(recipePath, "README.md"))).toBe(true);
    }
  });

  it("examples/ directory exists with expected YAML files", () => {
    const examplesDir = join(PROJECT_ROOT, "examples");
    expect(existsSync(examplesDir)).toBe(true);

    const expectedExamples = [
      "minimal.yaml",
      "coaching.yaml",
      "analytics.yaml",
      "full.yaml",
    ];
    for (const example of expectedExamples) {
      expect(existsSync(join(examplesDir, example))).toBe(true);
    }
  });

  it("engines field requires Node.js >= 20", () => {
    const pkgPath = join(PROJECT_ROOT, "package.json");
    const pkg = JSON.parse(readFileSync(pkgPath, "utf-8"));

    expect(pkg.engines).toBeDefined();
    expect(pkg.engines.node).toMatch(/>=\s*20/);
  });
});
