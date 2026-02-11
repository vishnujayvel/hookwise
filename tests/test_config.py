"""Tests for hookwise.config -- configuration loading and validation."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any

import pytest

from hookwise.config import (
    ConfigEngine,
    HooksConfig,
    ResolvedHandler,
    ValidationResult,
    deep_merge,
    interpolate_env_vars,
    VALID_EVENT_TYPES,
    VALID_HANDLER_TYPES,
    VALID_SECTIONS,
    DEFAULT_HANDLER_TIMEOUT,
)


# ---------------------------------------------------------------------------
# HooksConfig dataclass
# ---------------------------------------------------------------------------


class TestHooksConfig:
    """Tests for the HooksConfig dataclass."""

    def test_default_values(self) -> None:
        """Default HooksConfig should have safe empty values."""
        config = HooksConfig()
        assert config.version == 1
        assert config.guards == []
        assert config.coaching == {}
        assert config.analytics == {}
        assert config.greeting == {}
        assert config.sounds == {}
        assert config.status_line == {}
        assert config.cost_tracking == {}
        assert config.transcript_backup == {}
        assert config.handlers == []
        assert config.settings == {}
        assert config.includes == []

    def test_custom_values(self) -> None:
        """Should accept custom values for all fields."""
        config = HooksConfig(
            version=1,
            guards=[{"name": "test"}],
            coaching={"enabled": True},
            handlers=[{"type": "inline"}],
            includes=["recipe.yaml"],
        )
        assert config.guards == [{"name": "test"}]
        assert config.coaching == {"enabled": True}
        assert config.handlers == [{"type": "inline"}]
        assert config.includes == ["recipe.yaml"]

    def test_mutable_defaults_are_independent(self) -> None:
        """Each instance should have its own list/dict instances."""
        a = HooksConfig()
        b = HooksConfig()
        a.guards.append({"x": 1})
        assert b.guards == []


# ---------------------------------------------------------------------------
# deep_merge
# ---------------------------------------------------------------------------


class TestDeepMerge:
    """Tests for the deep_merge utility function."""

    def test_empty_dicts(self) -> None:
        """Merging two empty dicts returns empty dict."""
        assert deep_merge({}, {}) == {}

    def test_override_adds_new_keys(self) -> None:
        """Override keys not in base are added."""
        result = deep_merge({"a": 1}, {"b": 2})
        assert result == {"a": 1, "b": 2}

    def test_override_replaces_scalar(self) -> None:
        """Override scalar values replace base values."""
        result = deep_merge({"a": 1}, {"a": 2})
        assert result == {"a": 2}

    def test_override_replaces_list(self) -> None:
        """Override lists replace base lists (no concatenation)."""
        result = deep_merge({"a": [1, 2]}, {"a": [3, 4]})
        assert result == {"a": [3, 4]}

    def test_recursive_dict_merge(self) -> None:
        """Dict values are recursively merged."""
        base = {"settings": {"timeout": 10, "debug": False}}
        override = {"settings": {"timeout": 30}}
        result = deep_merge(base, override)
        assert result == {"settings": {"timeout": 30, "debug": False}}

    def test_deep_nested_merge(self) -> None:
        """Multiple levels of nesting should merge correctly."""
        base = {"a": {"b": {"c": 1, "d": 2}}}
        override = {"a": {"b": {"c": 99}}}
        result = deep_merge(base, override)
        assert result == {"a": {"b": {"c": 99, "d": 2}}}

    def test_override_dict_replaces_non_dict(self) -> None:
        """When override has a dict but base has a scalar, override wins."""
        result = deep_merge({"a": "string"}, {"a": {"nested": True}})
        assert result == {"a": {"nested": True}}

    def test_override_scalar_replaces_dict(self) -> None:
        """When override has a scalar but base has a dict, override wins."""
        result = deep_merge({"a": {"nested": True}}, {"a": "string"})
        assert result == {"a": "string"}

    def test_does_not_mutate_base(self) -> None:
        """Base dict should not be modified."""
        base = {"a": {"b": 1}}
        override = {"a": {"b": 2}}
        deep_merge(base, override)
        assert base == {"a": {"b": 1}}

    def test_does_not_mutate_override(self) -> None:
        """Override dict should not be modified."""
        base = {"a": 1}
        override = {"a": 2, "b": {"c": 3}}
        deep_merge(base, override)
        assert override == {"a": 2, "b": {"c": 3}}


# ---------------------------------------------------------------------------
# interpolate_env_vars
# ---------------------------------------------------------------------------


class TestInterpolateEnvVars:
    """Tests for environment variable interpolation."""

    def test_simple_string_replacement(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """Should replace ${VAR} with the env var value."""
        monkeypatch.setenv("TEST_VAR", "hello")
        assert interpolate_env_vars("${TEST_VAR}") == "hello"

    def test_multiple_vars_in_one_string(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """Should replace multiple ${VAR} references in a single string."""
        monkeypatch.setenv("HOST", "localhost")
        monkeypatch.setenv("PORT", "8080")
        result = interpolate_env_vars("http://${HOST}:${PORT}/api")
        assert result == "http://localhost:8080/api"

    def test_missing_var_becomes_empty_string(self) -> None:
        """Missing env vars should be replaced with empty string."""
        # Ensure the var does not exist
        os.environ.pop("DEFINITELY_NOT_SET_XYZ", None)
        assert interpolate_env_vars("${DEFINITELY_NOT_SET_XYZ}") == ""

    def test_no_interpolation_needed(self) -> None:
        """Strings without ${} should be returned unchanged."""
        assert interpolate_env_vars("plain string") == "plain string"

    def test_dict_values_interpolated(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """Should recurse into dict values."""
        monkeypatch.setenv("DB_HOST", "db.example.com")
        result = interpolate_env_vars({"host": "${DB_HOST}", "port": 5432})
        assert result == {"host": "db.example.com", "port": 5432}

    def test_list_values_interpolated(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """Should recurse into list elements."""
        monkeypatch.setenv("ITEM", "resolved")
        result = interpolate_env_vars(["${ITEM}", "static"])
        assert result == ["resolved", "static"]

    def test_nested_dict_and_list(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """Should handle deeply nested structures."""
        monkeypatch.setenv("DEEP", "found")
        value = {"a": [{"b": "${DEEP}"}]}
        result = interpolate_env_vars(value)
        assert result == {"a": [{"b": "found"}]}

    def test_non_string_types_unchanged(self) -> None:
        """Ints, floats, bools, None should pass through unchanged."""
        assert interpolate_env_vars(42) == 42
        assert interpolate_env_vars(3.14) == 3.14
        assert interpolate_env_vars(True) is True
        assert interpolate_env_vars(None) is None


# ---------------------------------------------------------------------------
# ConfigEngine.load_config
# ---------------------------------------------------------------------------


class TestConfigEngineLoadConfig:
    """Tests for ConfigEngine.load_config()."""

    @pytest.fixture
    def engine(self) -> ConfigEngine:
        return ConfigEngine()

    def test_no_config_files_returns_default(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """When no config files exist, should return default HooksConfig."""
        config = engine.load_config(project_dir=tmp_path)
        assert isinstance(config, HooksConfig)
        assert config.version == 1
        assert config.handlers == []

    def test_project_config_only(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Should load project-level config when only it exists."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\nsettings:\n  debug: true\n",
            encoding="utf-8",
        )
        config = engine.load_config(project_dir=tmp_path)
        assert config.settings == {"debug": True}

    def test_global_config_only(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Should load global config when only it exists."""
        global_config = tmp_state_dir / "config.yaml"
        global_config.write_text(
            "version: 1\nsettings:\n  log_level: WARNING\n",
            encoding="utf-8",
        )
        # Use a project dir with no config
        config = engine.load_config(project_dir=tmp_path)
        assert config.settings == {"log_level": "WARNING"}

    def test_project_overrides_global(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Project config should deep-merge over global config."""
        global_config = tmp_state_dir / "config.yaml"
        global_config.write_text(
            "version: 1\nsettings:\n  debug: false\n  log_level: INFO\n",
            encoding="utf-8",
        )
        project_config = tmp_path / "hookwise.yaml"
        project_config.write_text(
            "settings:\n  debug: true\n",
            encoding="utf-8",
        )
        config = engine.load_config(project_dir=tmp_path)
        # debug overridden by project, log_level inherited from global
        assert config.settings == {"debug": True, "log_level": "INFO"}

    def test_env_var_interpolation_in_loaded_config(
        self,
        engine: ConfigEngine,
        tmp_path: Path,
        tmp_state_dir: Path,
        monkeypatch: pytest.MonkeyPatch,
    ) -> None:
        """Should interpolate ${ENV_VAR} in config values."""
        monkeypatch.setenv("MY_TOKEN", "secret123")
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\nsettings:\n  token: '${MY_TOKEN}'\n",
            encoding="utf-8",
        )
        config = engine.load_config(project_dir=tmp_path)
        assert config.settings["token"] == "secret123"

    def test_malformed_yaml_returns_default(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Malformed YAML should return default config (fail-open)."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text("{{invalid yaml: [", encoding="utf-8")
        config = engine.load_config(project_dir=tmp_path)
        assert isinstance(config, HooksConfig)
        assert config.handlers == []

    def test_empty_yaml_returns_default(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Empty YAML file should return default config."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text("", encoding="utf-8")
        config = engine.load_config(project_dir=tmp_path)
        assert isinstance(config, HooksConfig)

    def test_non_dict_yaml_returns_default(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """YAML that parses as a non-dict should return default config."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text("- just\n- a\n- list\n", encoding="utf-8")
        config = engine.load_config(project_dir=tmp_path)
        assert isinstance(config, HooksConfig)
        assert config.handlers == []

    def test_includes_parsed(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """include: directives should be parsed into the includes list."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\nincludes:\n  - recipes/security.yaml\n  - recipes/coaching.yaml\n",
            encoding="utf-8",
        )
        config = engine.load_config(project_dir=tmp_path)
        assert config.includes == ["recipes/security.yaml", "recipes/coaching.yaml"]

    def test_handlers_loaded(
        self, engine: ConfigEngine, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Handlers list should be preserved in config."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "handlers:\n"
            "  - name: test_handler\n"
            "    type: inline\n"
            "    events: [PreToolUse]\n"
            "    action:\n"
            "      type: context\n"
            "      message: hello\n",
            encoding="utf-8",
        )
        config = engine.load_config(project_dir=tmp_path)
        assert len(config.handlers) == 1
        assert config.handlers[0]["name"] == "test_handler"


# ---------------------------------------------------------------------------
# ConfigEngine.validate_config
# ---------------------------------------------------------------------------


class TestConfigEngineValidateConfig:
    """Tests for ConfigEngine.validate_config()."""

    @pytest.fixture
    def engine(self) -> ConfigEngine:
        return ConfigEngine()

    def test_valid_minimal_config(self, engine: ConfigEngine) -> None:
        """Minimal valid config should pass validation."""
        result = engine.validate_config({"version": 1})
        assert result.valid
        assert result.errors == []

    def test_valid_full_config(self, engine: ConfigEngine) -> None:
        """Config with all valid sections should pass."""
        raw = {
            "version": 1,
            "guards": [],
            "coaching": {},
            "analytics": {},
            "greeting": {},
            "sounds": {},
            "status_line": {},
            "cost_tracking": {},
            "transcript_backup": {},
            "handlers": [],
            "settings": {},
            "includes": [],
        }
        result = engine.validate_config(raw)
        assert result.valid

    def test_unknown_section_flagged(self, engine: ConfigEngine) -> None:
        """Unknown top-level keys should produce validation errors."""
        result = engine.validate_config({"version": 1, "unknown_key": "value"})
        assert not result.valid
        assert any("unknown_key" in e.path for e in result.errors)

    def test_unsupported_version(self, engine: ConfigEngine) -> None:
        """Non-1 version should produce a validation error."""
        result = engine.validate_config({"version": 2})
        assert not result.valid
        assert any("version" in e.path for e in result.errors)

    def test_handlers_not_a_list(self, engine: ConfigEngine) -> None:
        """handlers as a non-list should fail."""
        result = engine.validate_config({"handlers": "not_a_list"})
        assert not result.valid
        assert any("handlers" in e.path for e in result.errors)

    def test_handler_missing_type(self, engine: ConfigEngine) -> None:
        """Handler without type should fail."""
        result = engine.validate_config({
            "handlers": [{"name": "test", "events": ["PreToolUse"]}],
        })
        assert not result.valid
        assert any("type" in e.path for e in result.errors)

    def test_handler_invalid_type(self, engine: ConfigEngine) -> None:
        """Handler with invalid type should fail."""
        result = engine.validate_config({
            "handlers": [{"type": "invalid", "events": ["PreToolUse"]}],
        })
        assert not result.valid
        assert any("type" in e.path for e in result.errors)

    def test_handler_missing_events(self, engine: ConfigEngine) -> None:
        """Handler without events should fail."""
        result = engine.validate_config({
            "handlers": [{"type": "inline", "name": "test"}],
        })
        assert not result.valid
        assert any("events" in e.path for e in result.errors)

    def test_handler_valid(self, engine: ConfigEngine) -> None:
        """Handler with all required fields should pass."""
        result = engine.validate_config({
            "handlers": [{
                "type": "inline",
                "events": ["PreToolUse"],
                "action": {"type": "context", "message": "hello"},
            }],
        })
        assert result.valid

    def test_guards_not_a_list(self, engine: ConfigEngine) -> None:
        """guards as a non-list should fail."""
        result = engine.validate_config({"guards": "not_a_list"})
        assert not result.valid
        assert any("guards" in e.path for e in result.errors)

    def test_includes_not_a_list(self, engine: ConfigEngine) -> None:
        """includes as a non-list should fail."""
        result = engine.validate_config({"includes": "not_a_list"})
        assert not result.valid
        assert any("includes" in e.path for e in result.errors)

    def test_empty_config_is_valid(self, engine: ConfigEngine) -> None:
        """Completely empty config should be valid."""
        result = engine.validate_config({})
        assert result.valid

    def test_handler_not_dict_flagged(self, engine: ConfigEngine) -> None:
        """Handler that is not a dict should be flagged."""
        result = engine.validate_config({
            "handlers": ["not_a_dict"],
        })
        assert not result.valid
        assert any("handlers[0]" in e.path for e in result.errors)

    def test_validation_errors_have_suggestions(self, engine: ConfigEngine) -> None:
        """Validation errors should include fix suggestions where available."""
        result = engine.validate_config({"unknown_key": "value"})
        assert not result.valid
        assert result.errors[0].suggestion is not None


# ---------------------------------------------------------------------------
# ConfigEngine.resolve_handlers
# ---------------------------------------------------------------------------


class TestConfigEngineResolveHandlers:
    """Tests for handler resolution."""

    @pytest.fixture
    def engine(self) -> ConfigEngine:
        return ConfigEngine()

    def test_resolve_inline_handler(self, engine: ConfigEngine) -> None:
        """Should resolve inline handler with correct fields."""
        config = HooksConfig(handlers=[{
            "name": "my_inline",
            "type": "inline",
            "events": ["PreToolUse"],
            "phase": "context",
            "action": {"type": "context", "message": "test"},
        }])
        resolved = engine.resolve_handlers(config)
        assert len(resolved) == 1
        h = resolved[0]
        assert h.name == "my_inline"
        assert h.handler_type == "inline"
        assert "PreToolUse" in h.events
        assert h.phase == "context"
        assert h.action == {"type": "context", "message": "test"}

    def test_resolve_script_handler(self, engine: ConfigEngine) -> None:
        """Should resolve script handler with command."""
        config = HooksConfig(handlers=[{
            "name": "my_script",
            "type": "script",
            "events": ["PostToolUse"],
            "command": "python3 my_hook.py",
            "phase": "side_effect",
        }])
        resolved = engine.resolve_handlers(config)
        assert len(resolved) == 1
        h = resolved[0]
        assert h.handler_type == "script"
        assert h.command == "python3 my_hook.py"

    def test_resolve_builtin_handler(self, engine: ConfigEngine) -> None:
        """Should resolve builtin handler with module path."""
        config = HooksConfig(handlers=[{
            "name": "my_builtin",
            "type": "builtin",
            "events": ["SessionStart"],
            "module": "hookwise.guards",
            "phase": "guard",
        }])
        resolved = engine.resolve_handlers(config)
        assert len(resolved) == 1
        h = resolved[0]
        assert h.handler_type == "builtin"
        assert h.module == "hookwise.guards"

    def test_resolve_guard_always_phase_guard(self, engine: ConfigEngine) -> None:
        """Guards should always be resolved with phase='guard'."""
        config = HooksConfig(guards=[{
            "name": "my_guard",
            "type": "builtin",
            "events": ["PreToolUse"],
            "module": "hookwise.guards",
        }])
        resolved = engine.resolve_handlers(config)
        assert len(resolved) == 1
        assert resolved[0].phase == "guard"

    def test_wildcard_events(self, engine: ConfigEngine) -> None:
        """events: '*' should match all event types."""
        config = HooksConfig(handlers=[{
            "type": "inline",
            "events": "*",
            "action": {"type": "context", "message": "global"},
        }])
        resolved = engine.resolve_handlers(config)
        assert len(resolved) == 1
        assert resolved[0].events == set(VALID_EVENT_TYPES)

    def test_handler_default_timeout(self, engine: ConfigEngine) -> None:
        """Handlers without explicit timeout should get DEFAULT_HANDLER_TIMEOUT."""
        config = HooksConfig(handlers=[{
            "type": "inline",
            "events": ["PreToolUse"],
        }])
        resolved = engine.resolve_handlers(config)
        assert resolved[0].timeout == DEFAULT_HANDLER_TIMEOUT

    def test_handler_custom_timeout(self, engine: ConfigEngine) -> None:
        """Handlers with explicit timeout should use that value."""
        config = HooksConfig(handlers=[{
            "type": "inline",
            "events": ["PreToolUse"],
            "timeout": 30,
        }])
        resolved = engine.resolve_handlers(config)
        assert resolved[0].timeout == 30

    def test_invalid_handler_type_skipped(self, engine: ConfigEngine) -> None:
        """Handlers with unsupported type should be skipped."""
        config = HooksConfig(handlers=[{
            "type": "unknown_type",
            "events": ["PreToolUse"],
        }])
        resolved = engine.resolve_handlers(config)
        assert resolved == []

    def test_handler_default_name(self, engine: ConfigEngine) -> None:
        """Handlers without name should get a default name."""
        config = HooksConfig(handlers=[{
            "type": "inline",
            "events": ["PreToolUse"],
        }])
        resolved = engine.resolve_handlers(config)
        assert resolved[0].name.startswith("handler_")

    def test_unknown_event_type_skipped(self, engine: ConfigEngine) -> None:
        """Unknown event types should be skipped from handler's events."""
        config = HooksConfig(handlers=[{
            "type": "inline",
            "events": ["PreToolUse", "FakeEvent"],
        }])
        resolved = engine.resolve_handlers(config)
        assert "PreToolUse" in resolved[0].events
        assert "FakeEvent" not in resolved[0].events

    def test_invalid_phase_defaults_to_side_effect(self, engine: ConfigEngine) -> None:
        """Invalid phase should default to 'side_effect'."""
        config = HooksConfig(handlers=[{
            "type": "inline",
            "events": ["PreToolUse"],
            "phase": "invalid_phase",
        }])
        resolved = engine.resolve_handlers(config)
        assert resolved[0].phase == "side_effect"


# ---------------------------------------------------------------------------
# ConfigEngine.get_handlers_for_event
# ---------------------------------------------------------------------------


class TestGetHandlersForEvent:
    """Tests for filtering handlers by event type."""

    @pytest.fixture
    def engine(self) -> ConfigEngine:
        return ConfigEngine()

    def test_filters_by_event_type(self, engine: ConfigEngine) -> None:
        """Should only return handlers matching the given event type."""
        config = HooksConfig(handlers=[
            {"type": "inline", "events": ["PreToolUse"], "phase": "context"},
            {"type": "inline", "events": ["PostToolUse"], "phase": "context"},
            {"type": "inline", "events": ["PreToolUse", "PostToolUse"], "phase": "side_effect"},
        ])
        handlers = engine.get_handlers_for_event(config, "PreToolUse")
        assert len(handlers) == 2
        handlers = engine.get_handlers_for_event(config, "PostToolUse")
        assert len(handlers) == 2

    def test_no_matching_handlers(self, engine: ConfigEngine) -> None:
        """Should return empty list when no handlers match."""
        config = HooksConfig(handlers=[
            {"type": "inline", "events": ["PreToolUse"]},
        ])
        handlers = engine.get_handlers_for_event(config, "SessionStart")
        assert handlers == []

    def test_preserves_config_order(self, engine: ConfigEngine) -> None:
        """Handlers should be returned in config file order."""
        config = HooksConfig(
            guards=[
                {"type": "inline", "events": ["PreToolUse"], "name": "guard_1"},
            ],
            handlers=[
                {"type": "inline", "events": ["PreToolUse"], "phase": "context", "name": "ctx_1"},
                {"type": "inline", "events": ["PreToolUse"], "phase": "side_effect", "name": "se_1"},
            ],
        )
        handlers = engine.get_handlers_for_event(config, "PreToolUse")
        assert len(handlers) == 3
        # Guards come first, then handlers in order
        assert handlers[0].name == "guard_1"
        assert handlers[1].name == "ctx_1"
        assert handlers[2].name == "se_1"


# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------


class TestConstants:
    """Tests for configuration constants."""

    def test_all_13_event_types(self) -> None:
        """Should have all 13 Claude Code hook event types."""
        assert len(VALID_EVENT_TYPES) == 13
        expected = {
            "UserPromptSubmit", "PreToolUse", "PostToolUse",
            "PostToolUseFailure", "Notification", "Stop",
            "SubagentStart", "SubagentStop", "PreCompact",
            "SessionStart", "SessionEnd", "PermissionRequest", "Setup",
        }
        assert VALID_EVENT_TYPES == expected

    def test_handler_types(self) -> None:
        """Should have all three handler types."""
        assert VALID_HANDLER_TYPES == {"builtin", "script", "inline"}

    def test_valid_sections(self) -> None:
        """Should contain all expected config sections."""
        expected = {
            "version", "guards", "coaching", "analytics", "greeting",
            "sounds", "status_line", "cost_tracking", "transcript_backup",
            "handlers", "settings", "includes",
        }
        assert VALID_SECTIONS == expected
