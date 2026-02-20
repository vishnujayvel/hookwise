/**
 * Recipe loader for hookwise v1.0
 *
 * Recipes are self-contained hook configurations that can be included
 * via `include: recipes/<category>/<name>` directives in hookwise.yaml.
 *
 * Resolution order for recipe paths:
 * 1. node_modules/hookwise/recipes/ (npm install)
 * 2. ./recipes/ (local project)
 * 3. Absolute path
 *
 * Each recipe directory must contain:
 * - hooks.yaml (primary) or hooks.json (fallback) with config
 * - handler.ts with the recipe implementation
 * - README.md with documentation
 *
 * Recipe configs have lowest priority: project config overrides recipe config.
 */

import { existsSync, readFileSync } from "node:fs";
import { join, isAbsolute, dirname } from "node:path";
import yaml from "js-yaml";
import { logError, logDebug } from "./errors.js";
import { deepMerge } from "./config.js";
import type {
  HooksConfig,
  ValidationResult,
  ValidationError,
  EventType,
} from "./types.js";
import { isEventType } from "./types.js";

// --- Recipe Config Type ---

/**
 * Configuration loaded from a recipe's hooks.yaml or hooks.json.
 */
export interface RecipeConfig {
  name: string;
  version: string;
  description?: string;
  events: EventType[];
  config: Record<string, unknown>;
  guards?: unknown[];
  handlers?: unknown[];
  [key: string]: unknown;
}

// --- Recipe Path Resolution ---

/**
 * Resolve a recipe name to an absolute directory path.
 *
 * Checks in order:
 * 1. node_modules/hookwise/recipes/<name> (npm install)
 * 2. ./recipes/<name> (local project)
 * 3. Absolute path (if the name is already absolute)
 *
 * @param name - Recipe name like "safety/block-dangerous-commands" or absolute path
 * @param baseDir - Base directory for relative resolution (project root)
 * @returns Absolute path to the recipe directory, or null if not found
 */
export function resolveRecipePath(
  name: string,
  baseDir: string
): string | null {
  // Absolute path: check directly
  if (isAbsolute(name)) {
    if (existsSync(name)) return name;
    return null;
  }

  // 1. node_modules/hookwise/recipes/<name>
  const npmPath = join(baseDir, "node_modules", "hookwise", "recipes", name);
  if (existsSync(npmPath)) return npmPath;

  // 2. ./recipes/<name> (local project)
  const localPath = join(baseDir, "recipes", name);
  if (existsSync(localPath)) return localPath;

  // Not found
  logDebug("Recipe path not found", { name, baseDir });
  return null;
}

// --- Recipe Loading ---

/**
 * Load a recipe configuration from a directory path.
 *
 * Looks for hooks.yaml first (primary), falls back to hooks.json.
 * Returns null if neither file exists or if parsing fails.
 *
 * @param recipePath - Absolute path to the recipe directory
 * @returns Parsed RecipeConfig, or null if loading fails
 */
export function loadRecipe(recipePath: string): RecipeConfig | null {
  try {
    let candidate: Record<string, unknown> | null = null;

    // Try hooks.yaml first (primary)
    const yamlPath = join(recipePath, "hooks.yaml");
    if (existsSync(yamlPath)) {
      const content = readFileSync(yamlPath, "utf-8");
      const parsed = yaml.load(content);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        candidate = parsed as Record<string, unknown>;
      }
    }

    // Fall back to hooks.json
    if (!candidate) {
      const jsonPath = join(recipePath, "hooks.json");
      if (existsSync(jsonPath)) {
        const content = readFileSync(jsonPath, "utf-8");
        const parsedJson = JSON.parse(content);
        if (parsedJson && typeof parsedJson === "object" && !Array.isArray(parsedJson)) {
          candidate = parsedJson as Record<string, unknown>;
        }
      }
    }

    if (!candidate) {
      logDebug("No hooks.yaml or hooks.json found in recipe", { recipePath });
      return null;
    }

    // Validate the parsed recipe config
    const validation = validateRecipe(candidate);
    if (!validation.valid) {
      logError(
        new Error(`Invalid recipe config: ${validation.errors.map((e) => e.message).join(", ")}`),
        { context: "loadRecipe", recipePath }
      );
      return null;
    }

    return candidate as RecipeConfig;
  } catch (error) {
    logError(
      error instanceof Error ? error : new Error(String(error)),
      { context: "loadRecipe", recipePath }
    );
    return null;
  }
}

// --- Recipe Validation ---

/**
 * Validate a recipe configuration object.
 *
 * Checks that the recipe contains the required fields:
 * - name (string)
 * - version (string)
 * - events (array of valid EventType strings)
 * - config (object)
 *
 * @param recipe - The object to validate
 * @returns ValidationResult with any errors found
 */
export function validateRecipe(recipe: unknown): ValidationResult {
  const errors: ValidationError[] = [];

  if (recipe === null || recipe === undefined || typeof recipe !== "object" || Array.isArray(recipe)) {
    errors.push({
      path: "",
      message: "Recipe must be a non-null object",
    });
    return { valid: false, errors };
  }

  const obj = recipe as Record<string, unknown>;

  // name: required string
  if (!obj.name || typeof obj.name !== "string") {
    errors.push({
      path: "name",
      message: "Recipe must have a 'name' string field",
      suggestion: "Add name: 'my-recipe'",
    });
  }

  // version: required string
  if (!obj.version || typeof obj.version !== "string") {
    errors.push({
      path: "version",
      message: "Recipe must have a 'version' string field",
      suggestion: "Add version: '1.0.0'",
    });
  }

  // events: required array of EventType strings
  if (!Array.isArray(obj.events)) {
    errors.push({
      path: "events",
      message: "Recipe must have an 'events' array",
      suggestion: "Add events: ['PreToolUse', 'PostToolUse']",
    });
  } else {
    for (let i = 0; i < obj.events.length; i++) {
      const event = obj.events[i];
      if (!isEventType(event)) {
        errors.push({
          path: `events[${i}]`,
          message: `Invalid event type: "${String(event)}"`,
          suggestion: "Use one of: UserPromptSubmit, PreToolUse, PostToolUse, etc.",
        });
      }
    }
  }

  // config: required object
  if (obj.config === undefined || obj.config === null || typeof obj.config !== "object" || Array.isArray(obj.config)) {
    errors.push({
      path: "config",
      message: "Recipe must have a 'config' object",
      suggestion: "Add config: { enabled: true }",
    });
  }

  return {
    valid: errors.length === 0,
    errors,
  };
}

// --- Recipe Merging ---

/**
 * Merge a recipe config into a base HooksConfig.
 *
 * Recipe configs have the LOWEST priority: the base (project) config
 * always overrides recipe values. This means the recipe serves as
 * a defaults layer.
 *
 * Merge strategy:
 * - Recipe values are used as defaults (base layer)
 * - Project/base config values override recipe values
 * - Guards from recipes are prepended to the guards array
 * - Handlers from recipes are appended to the handlers array
 *
 * @param base - The project's HooksConfig (higher priority)
 * @param recipe - The recipe's config (lower priority)
 * @returns Merged HooksConfig with recipe as defaults layer
 */
export function mergeRecipeConfig(
  base: HooksConfig,
  recipe: RecipeConfig
): HooksConfig {
  try {
    const baseRecord = base as unknown as Record<string, unknown>;

    // Extract recipe-level config fields that map to HooksConfig sections
    const recipeConfigFields = recipe.config as Record<string, unknown>;

    // Merge: recipe config is the base layer, project config overrides
    // deepMerge(target, source) means source overrides target
    // We want base to override recipe, so: deepMerge(recipeConfig, base)
    const merged = deepMerge(
      recipeConfigFields,
      baseRecord
    ) as unknown as HooksConfig;

    // Handle guards: prepend recipe guards to existing guards
    if (Array.isArray(recipe.guards) && recipe.guards.length > 0) {
      const existingGuards = Array.isArray(merged.guards) ? merged.guards : [];
      merged.guards = [...(recipe.guards as typeof existingGuards), ...existingGuards];
    }

    // Handle handlers: append recipe handlers to existing handlers
    if (Array.isArray(recipe.handlers) && recipe.handlers.length > 0) {
      const existingHandlers = Array.isArray(merged.handlers) ? merged.handlers : [];
      merged.handlers = [...existingHandlers, ...(recipe.handlers as typeof existingHandlers)];
    }

    return merged;
  } catch (error) {
    logError(
      error instanceof Error ? error : new Error(String(error)),
      { context: "mergeRecipeConfig", recipe: recipe.name }
    );
    // Fail-open: return base unchanged
    return base;
  }
}
