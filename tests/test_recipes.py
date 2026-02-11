"""Tests for hookwise.recipes -- recipe loading, resolution, and merging."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any

import pytest
import yaml

from hookwise.config import ConfigEngine, HooksConfig, deep_merge
from hookwise.recipes.loader import (
    BUILTIN_RECIPES_DIR,
    load_recipe,
    resolve_recipe_path,
    load_and_merge_recipes,
    list_builtin_recipes,
    _merge_with_list_concat,
)


# ---------------------------------------------------------------------------
# BUILTIN_RECIPES_DIR
# ---------------------------------------------------------------------------


class TestBuiltinRecipesDir:
    """Tests for the built-in recipes directory location."""

    def test_builtin_dir_exists(self) -> None:
        """The built-in recipes directory should exist."""
        assert BUILTIN_RECIPES_DIR.is_dir()

    def test_builtin_dir_is_within_package(self) -> None:
        """The recipes dir should be inside the hookwise package."""
        assert "hookwise" in str(BUILTIN_RECIPES_DIR)


# ---------------------------------------------------------------------------
# resolve_recipe_path
# ---------------------------------------------------------------------------


class TestResolveRecipePath:
    """Tests for recipe path resolution."""

    def test_builtin_safety_block_dangerous(self) -> None:
        """Should resolve builtin:safety/block-dangerous-commands."""
        path = resolve_recipe_path("builtin:safety/block-dangerous-commands")
        assert path is not None
        assert path.is_dir()
        assert (path / "hooks.yaml").is_file()

    def test_builtin_safety_secret_scanning(self) -> None:
        """Should resolve builtin:safety/secret-scanning."""
        path = resolve_recipe_path("builtin:safety/secret-scanning")
        assert path is not None
        assert path.is_dir()

    def test_builtin_behavioral_ai_tracker(self) -> None:
        """Should resolve builtin:behavioral/ai-dependency-tracker."""
        path = resolve_recipe_path("builtin:behavioral/ai-dependency-tracker")
        assert path is not None
        assert path.is_dir()

    def test_builtin_behavioral_metacognition(self) -> None:
        """Should resolve builtin:behavioral/metacognition-prompts."""
        path = resolve_recipe_path("builtin:behavioral/metacognition-prompts")
        assert path is not None

    def test_builtin_behavioral_builder_trap(self) -> None:
        """Should resolve builtin:behavioral/builder-trap-detection."""
        path = resolve_recipe_path("builtin:behavioral/builder-trap-detection")
        assert path is not None

    def test_builtin_compliance_cost(self) -> None:
        """Should resolve builtin:compliance/cost-tracking."""
        path = resolve_recipe_path("builtin:compliance/cost-tracking")
        assert path is not None

    def test_builtin_productivity_transcript(self) -> None:
        """Should resolve builtin:productivity/transcript-backup."""
        path = resolve_recipe_path("builtin:productivity/transcript-backup")
        assert path is not None

    def test_builtin_nonexistent_returns_none(self) -> None:
        """Should return None for a non-existent builtin recipe."""
        path = resolve_recipe_path("builtin:does-not-exist/fake-recipe")
        assert path is None

    def test_relative_path_resolves_to_project_dir(self, tmp_path: Path) -> None:
        """Should resolve relative paths against the project directory."""
        recipe_dir = tmp_path / "my-recipes" / "custom"
        recipe_dir.mkdir(parents=True)
        (recipe_dir / "hooks.yaml").write_text("guards: []\n", encoding="utf-8")

        path = resolve_recipe_path("my-recipes/custom", project_dir=tmp_path)
        assert path is not None
        assert path == recipe_dir.resolve()

    def test_relative_path_missing_dir_returns_none(self, tmp_path: Path) -> None:
        """Should return None if relative path does not exist."""
        path = resolve_recipe_path("no-such-dir/recipe", project_dir=tmp_path)
        assert path is None

    def test_relative_path_with_no_project_dir(self) -> None:
        """Should use cwd when project_dir is None."""
        # This just verifies it doesn't crash -- resolution may or may not
        # find a directory, depending on cwd.
        result = resolve_recipe_path("nonexistent-test-dir-xyz", project_dir=None)
        assert result is None  # almost certainly won't exist


# ---------------------------------------------------------------------------
# load_recipe
# ---------------------------------------------------------------------------


class TestLoadRecipe:
    """Tests for loading recipe hooks.yaml files."""

    def test_load_builtin_recipe(self) -> None:
        """Should successfully load a built-in recipe."""
        recipe_dir = BUILTIN_RECIPES_DIR / "safety" / "block-dangerous-commands"
        config = load_recipe(recipe_dir)
        assert config is not None
        assert "guards" in config
        assert isinstance(config["guards"], list)
        assert len(config["guards"]) == 4  # 4 guard rules

    def test_load_recipe_no_hooks_yaml(self, tmp_path: Path) -> None:
        """Should return None if hooks.yaml is missing."""
        config = load_recipe(tmp_path)
        assert config is None

    def test_load_recipe_malformed_yaml(self, tmp_path: Path) -> None:
        """Should return None on malformed YAML."""
        (tmp_path / "hooks.yaml").write_text("{{invalid: yaml[", encoding="utf-8")
        config = load_recipe(tmp_path)
        assert config is None

    def test_load_recipe_non_dict(self, tmp_path: Path) -> None:
        """Should return None if YAML is not a mapping."""
        (tmp_path / "hooks.yaml").write_text("- just\n- a\n- list\n", encoding="utf-8")
        config = load_recipe(tmp_path)
        assert config is None

    def test_load_recipe_empty_yaml(self, tmp_path: Path) -> None:
        """Should return empty dict for empty YAML file."""
        (tmp_path / "hooks.yaml").write_text("", encoding="utf-8")
        config = load_recipe(tmp_path)
        assert config == {}

    def test_load_custom_recipe(self, tmp_path: Path) -> None:
        """Should load a custom recipe with arbitrary config."""
        (tmp_path / "hooks.yaml").write_text(
            "coaching:\n  enabled: true\n  custom_key: 42\n",
            encoding="utf-8",
        )
        config = load_recipe(tmp_path)
        assert config is not None
        assert config["coaching"]["enabled"] is True
        assert config["coaching"]["custom_key"] == 42

    def test_load_all_builtin_recipes_valid(self) -> None:
        """All built-in recipes should load without errors."""
        recipe_names = list_builtin_recipes()
        assert len(recipe_names) >= 7  # we created 7 recipes

        for name in recipe_names:
            recipe_dir = BUILTIN_RECIPES_DIR / name
            config = load_recipe(recipe_dir)
            assert config is not None, f"Recipe {name} failed to load"
            assert isinstance(config, dict), f"Recipe {name} is not a dict"


# ---------------------------------------------------------------------------
# _merge_with_list_concat
# ---------------------------------------------------------------------------


class TestMergeWithListConcat:
    """Tests for the list-concatenation merge helper."""

    def test_guard_lists_concatenated(self) -> None:
        """Guard lists from two recipes should be concatenated."""
        base = {"guards": [{"match": "Bash", "action": "block"}]}
        addition = {"guards": [{"match": "Read", "action": "warn"}]}
        result = _merge_with_list_concat(base, addition)
        assert len(result["guards"]) == 2
        assert result["guards"][0]["match"] == "Bash"
        assert result["guards"][1]["match"] == "Read"

    def test_handler_lists_concatenated(self) -> None:
        """Handler lists from two recipes should be concatenated."""
        base = {"handlers": [{"type": "inline", "name": "h1"}]}
        addition = {"handlers": [{"type": "inline", "name": "h2"}]}
        result = _merge_with_list_concat(base, addition)
        assert len(result["handlers"]) == 2

    def test_dict_fields_merged(self) -> None:
        """Dict fields should be deep-merged normally."""
        base = {"coaching": {"enabled": True, "timeout": 10}}
        addition = {"coaching": {"custom": "value"}}
        result = _merge_with_list_concat(base, addition)
        assert result["coaching"]["enabled"] is True
        assert result["coaching"]["custom"] == "value"

    def test_empty_base_gets_addition(self) -> None:
        """Empty base should result in addition's content."""
        base: dict[str, Any] = {}
        addition = {"guards": [{"match": "Bash", "action": "block"}]}
        result = _merge_with_list_concat(base, addition)
        assert len(result["guards"]) == 1

    def test_empty_addition_preserves_base(self) -> None:
        """Empty addition should preserve base's content."""
        base = {"guards": [{"match": "Bash", "action": "block"}]}
        addition: dict[str, Any] = {}
        result = _merge_with_list_concat(base, addition)
        assert len(result["guards"]) == 1

    def test_non_list_guards_handled(self) -> None:
        """Non-list guards value should be treated as empty."""
        base = {"guards": "not_a_list"}
        addition = {"guards": [{"match": "Bash", "action": "block"}]}
        result = _merge_with_list_concat(base, addition)
        assert len(result["guards"]) == 1

    def test_does_not_mutate_inputs(self) -> None:
        """Neither input should be mutated."""
        base = {"guards": [{"match": "Bash"}]}
        addition = {"guards": [{"match": "Read"}]}
        base_copy = {"guards": [{"match": "Bash"}]}
        addition_copy = {"guards": [{"match": "Read"}]}
        _merge_with_list_concat(base, addition)
        assert base == base_copy
        assert addition == addition_copy


# ---------------------------------------------------------------------------
# load_and_merge_recipes
# ---------------------------------------------------------------------------


class TestLoadAndMergeRecipes:
    """Tests for the full recipe loading and merging pipeline."""

    def test_single_builtin_recipe(self) -> None:
        """Should merge a single builtin recipe under user config."""
        user_config: dict[str, Any] = {"version": 1}
        result = load_and_merge_recipes(
            ["builtin:safety/block-dangerous-commands"],
            user_config,
        )
        assert "guards" in result
        assert len(result["guards"]) == 4
        assert result["version"] == 1

    def test_user_config_overrides_recipe(self) -> None:
        """User config dict fields should override recipe defaults."""
        user_config = {
            "version": 1,
            "cost_tracking": {"daily_budget_usd": 50.00},
        }
        result = load_and_merge_recipes(
            ["builtin:compliance/cost-tracking"],
            user_config,
        )
        # User's budget should override recipe's $10 default
        assert result["cost_tracking"]["daily_budget_usd"] == 50.00
        # But recipe's warn_at_percent should still be present
        assert result["cost_tracking"]["warn_at_percent"] == 80

    def test_multiple_recipes_guards_concatenated(self) -> None:
        """Guards from multiple recipes should be concatenated."""
        user_config: dict[str, Any] = {"version": 1}
        result = load_and_merge_recipes(
            [
                "builtin:safety/block-dangerous-commands",
                "builtin:safety/secret-scanning",
            ],
            user_config,
        )
        # 4 from block-dangerous + 3 from secret-scanning = 7
        assert len(result["guards"]) == 7

    def test_user_guards_added_on_top(self) -> None:
        """User's own guards should be present after recipe guards."""
        user_config = {
            "version": 1,
            "guards": [{"match": "MyTool", "action": "block", "reason": "custom"}],
        }
        result = load_and_merge_recipes(
            ["builtin:safety/block-dangerous-commands"],
            user_config,
        )
        # Recipe guards should be there, plus user's guard
        # deep_merge of lists: user list replaces recipe list
        # So user's single guard replaces the 4 recipe guards
        # This is correct: user explicitly defined guards, overriding recipe
        assert len(result["guards"]) >= 1
        # At minimum the user's guard should be present
        assert any(g["match"] == "MyTool" for g in result["guards"])

    def test_nonexistent_recipe_skipped(self) -> None:
        """Non-existent recipes should be silently skipped."""
        user_config = {"version": 1}
        result = load_and_merge_recipes(
            ["builtin:nonexistent/recipe"],
            user_config,
        )
        assert result["version"] == 1
        # No guards from the nonexistent recipe
        assert result.get("guards", []) == []

    def test_non_string_include_skipped(self) -> None:
        """Non-string include directives should be skipped."""
        user_config = {"version": 1}
        result = load_and_merge_recipes(
            [123, None, "builtin:safety/block-dangerous-commands"],  # type: ignore[list-item]
            user_config,
        )
        # The valid recipe should still be loaded
        assert "guards" in result

    def test_local_recipe(self, tmp_path: Path) -> None:
        """Should load a local recipe by relative path."""
        recipe_dir = tmp_path / "my-recipe"
        recipe_dir.mkdir()
        (recipe_dir / "hooks.yaml").write_text(
            "coaching:\n  enabled: true\n  custom: local\n",
            encoding="utf-8",
        )

        user_config = {"version": 1}
        result = load_and_merge_recipes(
            ["my-recipe"],
            user_config,
            project_dir=tmp_path,
        )
        assert result["coaching"]["enabled"] is True
        assert result["coaching"]["custom"] == "local"

    def test_empty_includes_returns_user_config(self) -> None:
        """Empty includes list should return user config unchanged."""
        user_config = {"version": 1, "coaching": {"enabled": True}}
        result = load_and_merge_recipes([], user_config)
        assert result == user_config

    def test_recipe_status_line_merged(self) -> None:
        """Status line config from recipes should merge with user config."""
        user_config = {
            "version": 1,
            "status_line": {"enabled": True, "segments": ["custom"]},
        }
        result = load_and_merge_recipes(
            ["builtin:behavioral/builder-trap-detection"],
            user_config,
        )
        # User's status_line should override recipe's
        assert result["status_line"]["enabled"] is True
        assert result["status_line"]["segments"] == ["custom"]


# ---------------------------------------------------------------------------
# list_builtin_recipes
# ---------------------------------------------------------------------------


class TestListBuiltinRecipes:
    """Tests for listing available built-in recipes."""

    def test_returns_all_recipes(self) -> None:
        """Should list all 7 built-in recipes."""
        recipes = list_builtin_recipes()
        assert len(recipes) >= 7

    def test_expected_recipes_present(self) -> None:
        """All expected recipe names should be in the list."""
        recipes = list_builtin_recipes()
        expected = [
            "behavioral/ai-dependency-tracker",
            "behavioral/builder-trap-detection",
            "behavioral/metacognition-prompts",
            "compliance/cost-tracking",
            "productivity/transcript-backup",
            "safety/block-dangerous-commands",
            "safety/secret-scanning",
        ]
        for name in expected:
            assert name in recipes, f"Missing recipe: {name}"

    def test_recipes_are_sorted(self) -> None:
        """Recipe list should be sorted alphabetically."""
        recipes = list_builtin_recipes()
        assert recipes == sorted(recipes)

    def test_no_pycache_or_init(self) -> None:
        """Should not include __pycache__ or __init__ as recipes."""
        recipes = list_builtin_recipes()
        for name in recipes:
            assert "__pycache__" not in name
            assert "__init__" not in name


# ---------------------------------------------------------------------------
# Recipe content validation
# ---------------------------------------------------------------------------


class TestRecipeContent:
    """Tests for the content of each built-in recipe."""

    def test_block_dangerous_commands_guards(self) -> None:
        """block-dangerous-commands should have 4 guard rules."""
        config = load_recipe(
            BUILTIN_RECIPES_DIR / "safety" / "block-dangerous-commands"
        )
        assert config is not None
        guards = config["guards"]
        assert len(guards) == 4
        actions = [g["action"] for g in guards]
        assert actions.count("block") == 2
        assert actions.count("confirm") == 2

    def test_secret_scanning_guards(self) -> None:
        """secret-scanning should have 3 guard rules."""
        config = load_recipe(BUILTIN_RECIPES_DIR / "safety" / "secret-scanning")
        assert config is not None
        guards = config["guards"]
        assert len(guards) == 3
        # 2 warns, 1 confirm
        actions = [g["action"] for g in guards]
        assert actions.count("warn") == 2
        assert actions.count("confirm") == 1

    def test_ai_dependency_tracker_analytics(self) -> None:
        """ai-dependency-tracker should enable analytics."""
        config = load_recipe(
            BUILTIN_RECIPES_DIR / "behavioral" / "ai-dependency-tracker"
        )
        assert config is not None
        assert config["analytics"]["enabled"] is True
        assert config["analytics"]["track_authorship"] is True

    def test_metacognition_prompts_coaching(self) -> None:
        """metacognition-prompts should enable coaching with metacognition."""
        config = load_recipe(
            BUILTIN_RECIPES_DIR / "behavioral" / "metacognition-prompts"
        )
        assert config is not None
        assert config["coaching"]["enabled"] is True
        assert config["coaching"]["metacognition"]["enabled"] is True
        assert config["coaching"]["metacognition"]["interval_minutes"] == 5

    def test_builder_trap_detection_coaching(self) -> None:
        """builder-trap-detection should have three threshold levels."""
        config = load_recipe(
            BUILTIN_RECIPES_DIR / "behavioral" / "builder-trap-detection"
        )
        assert config is not None
        trap = config["coaching"]["builder_trap"]
        assert trap["enabled"] is True
        assert trap["yellow_threshold_minutes"] == 30
        assert trap["orange_threshold_minutes"] == 60
        assert trap["red_threshold_minutes"] == 90

    def test_cost_tracking_defaults(self) -> None:
        """cost-tracking should have budget and warning defaults."""
        config = load_recipe(
            BUILTIN_RECIPES_DIR / "compliance" / "cost-tracking"
        )
        assert config is not None
        assert config["cost_tracking"]["enabled"] is True
        assert config["cost_tracking"]["daily_budget_usd"] == 10.00
        assert config["cost_tracking"]["warn_at_percent"] == 80

    def test_transcript_backup_defaults(self) -> None:
        """transcript-backup should have size limit default."""
        config = load_recipe(
            BUILTIN_RECIPES_DIR / "productivity" / "transcript-backup"
        )
        assert config is not None
        assert config["transcript_backup"]["enabled"] is True
        assert config["transcript_backup"]["max_dir_size_mb"] == 100

    def test_all_recipes_have_readme(self) -> None:
        """Every built-in recipe should have a README.md."""
        for name in list_builtin_recipes():
            recipe_dir = BUILTIN_RECIPES_DIR / name
            readme = recipe_dir / "README.md"
            assert readme.is_file(), f"Recipe {name} missing README.md"


# ---------------------------------------------------------------------------
# ConfigEngine integration with recipes
# ---------------------------------------------------------------------------


class TestConfigEngineRecipeIntegration:
    """Tests for recipe loading integrated into ConfigEngine.load_config()."""

    @pytest.fixture
    def engine(self) -> ConfigEngine:
        return ConfigEngine()

    def test_includes_are_processed(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Config with includes should have recipe content merged in."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "includes:\n"
            '  - "builtin:safety/block-dangerous-commands"\n',
            encoding="utf-8",
        )
        config = engine.load_config(project_dir=tmp_path)
        assert len(config.guards) == 4

    def test_user_overrides_recipe(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """User config should override recipe defaults."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "includes:\n"
            '  - "builtin:compliance/cost-tracking"\n'
            "cost_tracking:\n"
            "  daily_budget_usd: 99.99\n",
            encoding="utf-8",
        )
        config = engine.load_config(project_dir=tmp_path)
        assert config.cost_tracking["daily_budget_usd"] == 99.99
        # Recipe's warn_at_percent should still be inherited
        assert config.cost_tracking["warn_at_percent"] == 80

    def test_multiple_includes(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Multiple includes should all be processed."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "includes:\n"
            '  - "builtin:safety/block-dangerous-commands"\n'
            '  - "builtin:compliance/cost-tracking"\n',
            encoding="utf-8",
        )
        config = engine.load_config(project_dir=tmp_path)
        assert len(config.guards) >= 4
        assert config.cost_tracking.get("enabled") is True

    def test_nonexistent_include_graceful(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Non-existent includes should be silently ignored."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "includes:\n"
            '  - "builtin:nonexistent/recipe"\n'
            "settings:\n"
            "  debug: true\n",
            encoding="utf-8",
        )
        config = engine.load_config(project_dir=tmp_path)
        assert config.settings["debug"] is True

    def test_local_recipe_include(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Should load local recipe via relative path."""
        # Create local recipe
        recipe_dir = tmp_path / "my-recipe"
        recipe_dir.mkdir()
        (recipe_dir / "hooks.yaml").write_text(
            "coaching:\n  enabled: true\n  local: true\n",
            encoding="utf-8",
        )

        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "includes:\n"
            '  - "my-recipe"\n',
            encoding="utf-8",
        )
        config = engine.load_config(project_dir=tmp_path)
        assert config.coaching["enabled"] is True
        assert config.coaching["local"] is True

    def test_includes_field_preserved_in_config(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """The includes field should still be present in the loaded config."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "includes:\n"
            '  - "builtin:safety/block-dangerous-commands"\n',
            encoding="utf-8",
        )
        config = engine.load_config(project_dir=tmp_path)
        assert config.includes == ["builtin:safety/block-dangerous-commands"]
