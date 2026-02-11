"""Built-in recipe library for hookwise.

Recipes are pre-configured YAML fragments that provide sensible defaults
for common use cases (safety guards, coaching, analytics, etc.). Users
include recipes in their hookwise.yaml via the ``includes`` directive:

.. code-block:: yaml

    includes:
      - "builtin:safety/block-dangerous-commands"
      - "builtin:behavioral/builder-trap-detection"

Recipe configs are merged BEFORE the user's config, so user settings
always take precedence over recipe defaults.
"""

from hookwise.recipes.loader import load_recipe, resolve_recipe_path, BUILTIN_RECIPES_DIR

__all__ = [
    "load_recipe",
    "resolve_recipe_path",
    "BUILTIN_RECIPES_DIR",
]
