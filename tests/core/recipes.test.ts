/**
 * Tests for the recipe loader module.
 *
 * Verifies:
 * - Include resolution from multiple paths (node_modules, local, absolute)
 * - Config merging priority (project overrides recipe)
 * - Invalid recipe structure reported
 * - Missing recipe path handled gracefully
 * - hooks.yaml preferred over hooks.json
 * - Recipe validation with required fields
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import {
  mkdtempSync,
  mkdirSync,
  writeFileSync,
  rmSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import yaml from "js-yaml";
import {
  resolveRecipePath,
  loadRecipe,
  validateRecipe,
  mergeRecipeConfig,
} from "../../src/core/recipes.js";
import type { RecipeConfig } from "../../src/core/recipes.js";
import { getDefaultConfig } from "../../src/core/config.js";

// --- Helper: create temp directories ---

function createTempDir(): string {
  return mkdtempSync(join(tmpdir(), "hookwise-recipe-test-"));
}

function createRecipeDir(
  baseDir: string,
  recipeName: string,
  hooksConfig: Record<string, unknown>,
  format: "yaml" | "json" = "yaml"
): string {
  const recipeDir = join(baseDir, "recipes", recipeName);
  mkdirSync(recipeDir, { recursive: true });

  if (format === "yaml") {
    writeFileSync(
      join(recipeDir, "hooks.yaml"),
      yaml.dump(hooksConfig),
      "utf-8"
    );
  } else {
    writeFileSync(
      join(recipeDir, "hooks.json"),
      JSON.stringify(hooksConfig, null, 2),
      "utf-8"
    );
  }

  return recipeDir;
}

// --- resolveRecipePath ---

describe("resolveRecipePath", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = createTempDir();
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("resolves from local ./recipes/ directory", () => {
    const recipeDir = join(tempDir, "recipes", "safety", "block-dangerous");
    mkdirSync(recipeDir, { recursive: true });

    const result = resolveRecipePath("safety/block-dangerous", tempDir);
    expect(result).toBe(recipeDir);
  });

  it("resolves from node_modules/hookwise/recipes/", () => {
    const npmRecipeDir = join(
      tempDir,
      "node_modules",
      "hookwise",
      "recipes",
      "safety",
      "secret-scan"
    );
    mkdirSync(npmRecipeDir, { recursive: true });

    const result = resolveRecipePath("safety/secret-scan", tempDir);
    expect(result).toBe(npmRecipeDir);
  });

  it("prefers node_modules over local when both exist", () => {
    // Create both
    const npmDir = join(
      tempDir,
      "node_modules",
      "hookwise",
      "recipes",
      "safety",
      "test-recipe"
    );
    mkdirSync(npmDir, { recursive: true });

    const localDir = join(tempDir, "recipes", "safety", "test-recipe");
    mkdirSync(localDir, { recursive: true });

    const result = resolveRecipePath("safety/test-recipe", tempDir);
    expect(result).toBe(npmDir);
  });

  it("resolves absolute paths directly", () => {
    const absDir = join(tempDir, "custom-recipe");
    mkdirSync(absDir, { recursive: true });

    const result = resolveRecipePath(absDir, tempDir);
    expect(result).toBe(absDir);
  });

  it("returns null for non-existent recipe", () => {
    const result = resolveRecipePath("nonexistent/recipe", tempDir);
    expect(result).toBeNull();
  });

  it("returns null for non-existent absolute path", () => {
    const result = resolveRecipePath("/nonexistent/path/recipe", tempDir);
    expect(result).toBeNull();
  });
});

// --- loadRecipe ---

describe("loadRecipe", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = createTempDir();
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("loads hooks.yaml from recipe directory", () => {
    const recipeDir = createRecipeDir(tempDir, "test-recipe", {
      name: "test-recipe",
      version: "1.0.0",
      events: ["PreToolUse"],
      config: { enabled: true },
    });

    const recipe = loadRecipe(recipeDir);
    expect(recipe).not.toBeNull();
    expect(recipe!.name).toBe("test-recipe");
    expect(recipe!.version).toBe("1.0.0");
    expect(recipe!.events).toEqual(["PreToolUse"]);
  });

  it("falls back to hooks.json when hooks.yaml is absent", () => {
    const recipeDir = createRecipeDir(
      tempDir,
      "json-recipe",
      {
        name: "json-recipe",
        version: "2.0.0",
        events: ["PostToolUse"],
        config: { enabled: false },
      },
      "json"
    );

    const recipe = loadRecipe(recipeDir);
    expect(recipe).not.toBeNull();
    expect(recipe!.name).toBe("json-recipe");
    expect(recipe!.version).toBe("2.0.0");
  });

  it("prefers hooks.yaml over hooks.json when both exist", () => {
    const recipeDir = join(tempDir, "recipes", "both-formats");
    mkdirSync(recipeDir, { recursive: true });

    writeFileSync(
      join(recipeDir, "hooks.yaml"),
      yaml.dump({ name: "from-yaml", version: "1.0.0", events: ["PreToolUse"], config: {} }),
      "utf-8"
    );
    writeFileSync(
      join(recipeDir, "hooks.json"),
      JSON.stringify({ name: "from-json", version: "1.0.0", events: ["PreToolUse"], config: {} }),
      "utf-8"
    );

    const recipe = loadRecipe(recipeDir);
    expect(recipe).not.toBeNull();
    expect(recipe!.name).toBe("from-yaml");
  });

  it("returns null when no config files exist", () => {
    const emptyDir = join(tempDir, "empty-recipe");
    mkdirSync(emptyDir, { recursive: true });

    const recipe = loadRecipe(emptyDir);
    expect(recipe).toBeNull();
  });

  it("returns null for malformed YAML", () => {
    const recipeDir = join(tempDir, "recipes", "bad-yaml");
    mkdirSync(recipeDir, { recursive: true });
    writeFileSync(join(recipeDir, "hooks.yaml"), "{{invalid yaml", "utf-8");

    const recipe = loadRecipe(recipeDir);
    expect(recipe).toBeNull();
  });

  it("returns null for malformed JSON", () => {
    const recipeDir = join(tempDir, "recipes", "bad-json");
    mkdirSync(recipeDir, { recursive: true });
    writeFileSync(join(recipeDir, "hooks.json"), "{invalid json", "utf-8");

    const recipe = loadRecipe(recipeDir);
    expect(recipe).toBeNull();
  });

  it("returns null for JSON array (type guard)", () => {
    const recipeDir = join(tempDir, "recipes", "json-array");
    mkdirSync(recipeDir, { recursive: true });
    writeFileSync(
      join(recipeDir, "hooks.json"),
      JSON.stringify([{ name: "test" }]),
      "utf-8"
    );

    const recipe = loadRecipe(recipeDir);
    expect(recipe).toBeNull();
  });

  it("returns null for JSON primitive (type guard)", () => {
    const recipeDir = join(tempDir, "recipes", "json-primitive");
    mkdirSync(recipeDir, { recursive: true });
    writeFileSync(join(recipeDir, "hooks.json"), '"just a string"', "utf-8");

    const recipe = loadRecipe(recipeDir);
    expect(recipe).toBeNull();
  });

  it("returns null for recipe missing required fields (validation)", () => {
    const recipeDir = join(tempDir, "recipes", "incomplete");
    mkdirSync(recipeDir, { recursive: true });
    // Missing name, version, events, config
    writeFileSync(
      join(recipeDir, "hooks.yaml"),
      yaml.dump({ description: "incomplete recipe" }),
      "utf-8"
    );

    const recipe = loadRecipe(recipeDir);
    expect(recipe).toBeNull();
  });

  it("returns null for recipe with invalid events (validation)", () => {
    const recipeDir = join(tempDir, "recipes", "bad-events");
    mkdirSync(recipeDir, { recursive: true });
    writeFileSync(
      join(recipeDir, "hooks.yaml"),
      yaml.dump({
        name: "bad-events",
        version: "1.0.0",
        events: ["InvalidEvent"],
        config: {},
      }),
      "utf-8"
    );

    const recipe = loadRecipe(recipeDir);
    expect(recipe).toBeNull();
  });

  it("returns valid recipe after validation passes", () => {
    const recipeDir = createRecipeDir(tempDir, "valid-recipe", {
      name: "valid-recipe",
      version: "1.0.0",
      events: ["PreToolUse"],
      config: { enabled: true },
    });

    const recipe = loadRecipe(recipeDir);
    expect(recipe).not.toBeNull();
    expect(recipe!.name).toBe("valid-recipe");
  });
});

// --- validateRecipe ---

describe("validateRecipe", () => {
  it("validates a correct recipe", () => {
    const result = validateRecipe({
      name: "test-recipe",
      version: "1.0.0",
      events: ["PreToolUse", "PostToolUse"],
      config: { enabled: true },
    });
    expect(result.valid).toBe(true);
    expect(result.errors).toHaveLength(0);
  });

  it("reports missing name", () => {
    const result = validateRecipe({
      version: "1.0.0",
      events: ["PreToolUse"],
      config: {},
    });
    expect(result.valid).toBe(false);
    expect(result.errors.some((e) => e.path === "name")).toBe(true);
  });

  it("reports missing version", () => {
    const result = validateRecipe({
      name: "test",
      events: ["PreToolUse"],
      config: {},
    });
    expect(result.valid).toBe(false);
    expect(result.errors.some((e) => e.path === "version")).toBe(true);
  });

  it("reports missing events", () => {
    const result = validateRecipe({
      name: "test",
      version: "1.0.0",
      config: {},
    });
    expect(result.valid).toBe(false);
    expect(result.errors.some((e) => e.path === "events")).toBe(true);
  });

  it("reports invalid event types", () => {
    const result = validateRecipe({
      name: "test",
      version: "1.0.0",
      events: ["PreToolUse", "InvalidEvent"],
      config: {},
    });
    expect(result.valid).toBe(false);
    expect(result.errors.some((e) => e.path === "events[1]")).toBe(true);
  });

  it("reports missing config", () => {
    const result = validateRecipe({
      name: "test",
      version: "1.0.0",
      events: ["PreToolUse"],
    });
    expect(result.valid).toBe(false);
    expect(result.errors.some((e) => e.path === "config")).toBe(true);
  });

  it("reports null input", () => {
    const result = validateRecipe(null);
    expect(result.valid).toBe(false);
  });

  it("reports undefined input", () => {
    const result = validateRecipe(undefined);
    expect(result.valid).toBe(false);
  });

  it("reports array input", () => {
    const result = validateRecipe([]);
    expect(result.valid).toBe(false);
  });

  it("reports non-string name", () => {
    const result = validateRecipe({
      name: 123,
      version: "1.0.0",
      events: ["PreToolUse"],
      config: {},
    });
    expect(result.valid).toBe(false);
    expect(result.errors.some((e) => e.path === "name")).toBe(true);
  });

  it("reports non-string version", () => {
    const result = validateRecipe({
      name: "test",
      version: 1,
      events: ["PreToolUse"],
      config: {},
    });
    expect(result.valid).toBe(false);
    expect(result.errors.some((e) => e.path === "version")).toBe(true);
  });

  it("reports non-object config", () => {
    const result = validateRecipe({
      name: "test",
      version: "1.0.0",
      events: ["PreToolUse"],
      config: "not-object",
    });
    expect(result.valid).toBe(false);
    expect(result.errors.some((e) => e.path === "config")).toBe(true);
  });

  it("accumulates multiple errors", () => {
    const result = validateRecipe({});
    expect(result.valid).toBe(false);
    expect(result.errors.length).toBeGreaterThanOrEqual(4);
  });
});

// --- mergeRecipeConfig ---

describe("mergeRecipeConfig", () => {
  it("recipe config serves as defaults layer", () => {
    const base = getDefaultConfig();
    const recipe: RecipeConfig = {
      name: "test",
      version: "1.0.0",
      events: ["PreToolUse"],
      config: {
        costTracking: {
          enabled: true,
          dailyBudget: 20,
        },
      },
    };

    const merged = mergeRecipeConfig(base, recipe);
    // Recipe enabled costTracking since base had it disabled
    expect(merged.costTracking.enabled).toBe(false); // base overrides recipe
  });

  it("project config overrides recipe config", () => {
    const base = getDefaultConfig();
    base.costTracking.dailyBudget = 50;

    const recipe: RecipeConfig = {
      name: "test",
      version: "1.0.0",
      events: ["PreToolUse"],
      config: {
        costTracking: {
          dailyBudget: 10,
        },
      },
    };

    const merged = mergeRecipeConfig(base, recipe);
    expect(merged.costTracking.dailyBudget).toBe(50); // base wins
  });

  it("prepends recipe guards to existing guards", () => {
    const base = getDefaultConfig();
    base.guards = [{ match: "Write", action: "warn", reason: "Project guard" }];

    const recipe: RecipeConfig = {
      name: "test",
      version: "1.0.0",
      events: ["PreToolUse"],
      config: {},
      guards: [
        { match: "Bash", action: "block", reason: "Recipe guard" },
      ],
    };

    const merged = mergeRecipeConfig(base, recipe);
    expect(merged.guards).toHaveLength(2);
    // Recipe guards come first
    expect(merged.guards[0].match).toBe("Bash");
    expect(merged.guards[1].match).toBe("Write");
  });

  it("appends recipe handlers to existing handlers", () => {
    const base = getDefaultConfig();
    base.handlers = [
      { name: "existing", type: "builtin", events: ["Stop"], module: "a" },
    ];

    const recipe: RecipeConfig = {
      name: "test",
      version: "1.0.0",
      events: ["PreToolUse"],
      config: {},
      handlers: [
        { name: "recipe-handler", type: "builtin", events: ["PreToolUse"] },
      ],
    };

    const merged = mergeRecipeConfig(base, recipe);
    expect(merged.handlers).toHaveLength(2);
    // Existing handlers come first
    expect(merged.handlers[0].name).toBe("existing");
    expect(merged.handlers[1].name).toBe("recipe-handler");
  });

  it("handles recipe with no guards or handlers", () => {
    const base = getDefaultConfig();
    base.guards = [{ match: "Bash", action: "block", reason: "test" }];

    const recipe: RecipeConfig = {
      name: "test",
      version: "1.0.0",
      events: ["PreToolUse"],
      config: {},
    };

    const merged = mergeRecipeConfig(base, recipe);
    expect(merged.guards).toHaveLength(1);
    expect(merged.guards[0].match).toBe("Bash");
  });

  it("handles empty base gracefully", () => {
    const base = getDefaultConfig();
    const recipe: RecipeConfig = {
      name: "test",
      version: "1.0.0",
      events: ["PreToolUse"],
      config: {
        settings: { logLevel: "debug" },
      },
    };

    // Should not throw
    const merged = mergeRecipeConfig(base, recipe);
    expect(merged).toBeDefined();
  });
});
