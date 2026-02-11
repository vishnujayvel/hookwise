"""Recipe loader for hookwise.

Resolves recipe paths, loads recipe hooks.yaml files, and merges them
into the active configuration. Recipes provide defaults that the user's
own config can override.

Two path syntaxes are supported:

- ``builtin:<recipe-name>`` -- loads from the package's built-in
  ``hookwise/recipes/<recipe-name>/hooks.yaml``
- A relative file path -- resolved relative to the project directory,
  expected to be a directory containing ``hooks.yaml``

Merge semantics:
    Recipes are loaded in order. Each recipe's config is used as a base,
    and the accumulated user config is merged on top (user overrides recipe).
    This means later includes can also be overridden by the user's config.
"""

from __future__ import annotations

import logging
from pathlib import Path
from typing import Any

import yaml

logger = logging.getLogger("hookwise")

# Path to the built-in recipes directory (sibling of this file)
BUILTIN_RECIPES_DIR = Path(__file__).parent


def resolve_recipe_path(
    include: str,
    project_dir: Path | None = None,
) -> Path | None:
    """Resolve an include directive to an absolute directory path.

    Supports two syntaxes:
    - ``builtin:<name>`` -- resolves to ``hookwise/recipes/<name>/``
    - Anything else -- treated as a path relative to ``project_dir``

    Args:
        include: The include directive string from config.
        project_dir: The project directory for resolving relative paths.
            If None, uses the current working directory.

    Returns:
        Absolute Path to the recipe directory, or None if resolution fails.
    """
    if include.startswith("builtin:"):
        recipe_name = include[len("builtin:"):]
        recipe_dir = BUILTIN_RECIPES_DIR / recipe_name
        if recipe_dir.is_dir():
            return recipe_dir
        logger.warning(
            "Built-in recipe %r not found at %s", recipe_name, recipe_dir,
        )
        return None

    # Relative path -- resolve against project_dir
    if project_dir is None:
        project_dir = Path.cwd()
    recipe_dir = (Path(project_dir) / include).resolve()
    if recipe_dir.is_dir():
        return recipe_dir

    logger.warning("Recipe directory not found: %s", recipe_dir)
    return None


def load_recipe(recipe_dir: Path) -> dict[str, Any] | None:
    """Load a recipe's hooks.yaml file from a directory.

    Args:
        recipe_dir: Path to the recipe directory (must contain hooks.yaml).

    Returns:
        Parsed YAML dict, or None on any failure (missing file,
        malformed YAML, non-dict content).
    """
    hooks_file = recipe_dir / "hooks.yaml"
    if not hooks_file.is_file():
        logger.warning("Recipe at %s has no hooks.yaml file", recipe_dir)
        return None

    try:
        content = hooks_file.read_text(encoding="utf-8")
        parsed = yaml.safe_load(content)
        if parsed is None:
            return {}
        if not isinstance(parsed, dict):
            logger.warning(
                "Recipe hooks.yaml at %s is not a YAML mapping, ignoring",
                hooks_file,
            )
            return None
        return parsed
    except yaml.YAMLError as exc:
        logger.warning("Malformed YAML in recipe %s: %s", hooks_file, exc)
        return None
    except OSError as exc:
        logger.warning("Could not read recipe %s: %s", hooks_file, exc)
        return None


def load_and_merge_recipes(
    includes: list[str],
    user_config: dict[str, Any],
    project_dir: Path | None = None,
) -> dict[str, Any]:
    """Load all recipe includes and merge them under the user config.

    For each include directive:
    1. Resolve the recipe directory path
    2. Load its hooks.yaml
    3. Deep-merge: recipe as base, accumulated config as override

    This means the user's config always takes precedence over recipes,
    and later recipes can add new keys but cannot override earlier
    recipes or user config.

    Args:
        includes: List of include directive strings from config.
        user_config: The user's merged config (global + project).
        project_dir: Project directory for resolving relative paths.

    Returns:
        The final merged config dict (recipes + user config).
    """
    from hookwise.config import deep_merge

    # Start with an empty base; we'll layer recipes then user config
    accumulated = {}

    for include in includes:
        if not isinstance(include, str):
            logger.warning("Include directive is not a string: %r, skipping", include)
            continue

        recipe_dir = resolve_recipe_path(include, project_dir)
        if recipe_dir is None:
            continue

        recipe_config = load_recipe(recipe_dir)
        if recipe_config is None:
            continue

        # Special handling for list fields (guards, handlers):
        # Lists from recipes should be concatenated, not replaced.
        # We handle this by pre-merging list fields before deep_merge.
        accumulated = _merge_with_list_concat(accumulated, recipe_config)

    # Finally, user config overrides everything.
    # But for list fields, we want user lists to REPLACE recipe lists
    # (standard deep_merge behavior: override replaces base for lists).
    result = deep_merge(accumulated, user_config)
    return result


def _merge_with_list_concat(
    base: dict[str, Any],
    addition: dict[str, Any],
) -> dict[str, Any]:
    """Merge two recipe configs, concatenating list fields.

    For guard and handler lists, we concatenate rather than replace,
    so multiple recipes can each contribute guard rules.

    For dict fields, we use standard deep_merge (addition values are
    used as base, existing base overrides).

    Args:
        base: The accumulated config so far.
        addition: The new recipe config to merge in.

    Returns:
        Merged config dict.
    """
    import copy
    from hookwise.config import deep_merge

    result = deep_merge(base, addition)

    # For list-type fields, concatenate instead of replacing
    list_fields = ("guards", "handlers")
    for field in list_fields:
        base_list = base.get(field, [])
        addition_list = addition.get(field, [])
        if base_list or addition_list:
            if not isinstance(base_list, list):
                base_list = []
            if not isinstance(addition_list, list):
                addition_list = []
            result[field] = copy.deepcopy(base_list) + copy.deepcopy(addition_list)

    return result


def list_builtin_recipes() -> list[str]:
    """List all available built-in recipe names.

    Scans the built-in recipes directory for subdirectories that
    contain a hooks.yaml file.

    Returns:
        Sorted list of recipe names (e.g., "safety/block-dangerous-commands").
    """
    recipes = []
    if not BUILTIN_RECIPES_DIR.is_dir():
        return recipes

    for category_dir in sorted(BUILTIN_RECIPES_DIR.iterdir()):
        if not category_dir.is_dir() or category_dir.name.startswith("_"):
            continue
        for recipe_dir in sorted(category_dir.iterdir()):
            if not recipe_dir.is_dir():
                continue
            if (recipe_dir / "hooks.yaml").is_file():
                recipes.append(f"{category_dir.name}/{recipe_dir.name}")

    return recipes
