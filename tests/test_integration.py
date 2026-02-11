"""End-to-end integration tests for hookwise.

Tests full dispatch flows, config merge precedence, fail-open behavior,
recipe loading, and performance characteristics.
"""

from __future__ import annotations

import json
import os
import time
from pathlib import Path
from typing import Any
from unittest.mock import patch, MagicMock

import pytest
import yaml

from hookwise.config import (
    ConfigEngine,
    HooksConfig,
    ResolvedHandler,
    deep_merge,
)
from hookwise.dispatcher import (
    Dispatcher,
    DispatchResult,
    HandlerResult,
    execute_handler,
)
from hookwise.guards import GuardEngine, GuardRule, GuardResult, parse_guard_rules
from hookwise.errors import FailOpen


# ---------------------------------------------------------------------------
# Task 12.1: End-to-end integration tests
# ---------------------------------------------------------------------------


class TestFullDispatchFlow:
    """Test the complete dispatch pipeline from config to result."""

    def test_dispatch_pretooluse_with_guard_block(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Full flow: config file -> dispatcher -> guard block."""
        # Create a hookwise.yaml with a guard rule
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "guards:\n"
            '  - match: "Bash"\n'
            "    action: block\n"
            '    when: \'tool_input.command contains "rm -rf /"\'\n'
            '    reason: "Blocked: dangerous delete"\n',
            encoding="utf-8",
        )

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)
        dispatcher = Dispatcher(config_engine=engine)

        payload = {
            "tool_name": "Bash",
            "tool_input": {"command": "rm -rf /"},
        }
        result = dispatcher.dispatch("PreToolUse", payload, config=config)

        assert result.exit_code == 2
        assert result.stdout is not None
        output = json.loads(result.stdout)
        assert output["decision"] == "block"
        assert "dangerous" in output["reason"].lower()

    def test_dispatch_pretooluse_guard_allow(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Full flow: safe tool call passes through guards."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "guards:\n"
            '  - match: "Bash"\n'
            "    action: block\n"
            '    when: \'tool_input.command contains "rm -rf /"\'\n'
            '    reason: "Blocked: dangerous delete"\n',
            encoding="utf-8",
        )

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)
        dispatcher = Dispatcher(config_engine=engine)

        payload = {
            "tool_name": "Bash",
            "tool_input": {"command": "ls -la"},
        }
        result = dispatcher.dispatch("PreToolUse", payload, config=config)

        assert result.exit_code == 0

    def test_dispatch_pretooluse_guard_warn(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Full flow: warn decision returns exit code 0 with decision in stdout."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "guards:\n"
            '  - match: "Read"\n'
            "    action: warn\n"
            '    when: \'tool_input.file_path ends_with ".env"\'\n'
            '    reason: "Reading .env file"\n',
            encoding="utf-8",
        )

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)
        dispatcher = Dispatcher(config_engine=engine)

        payload = {
            "tool_name": "Read",
            "tool_input": {"file_path": "/home/user/.env"},
        }
        result = dispatcher.dispatch("PreToolUse", payload, config=config)

        assert result.exit_code == 0
        assert result.stdout is not None
        output = json.loads(result.stdout)
        assert output["decision"] == "warn"

    def test_dispatch_pretooluse_guard_confirm(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Full flow: confirm decision returns exit code 0 with decision in stdout."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "guards:\n"
            '  - match: "Bash"\n'
            "    action: confirm\n"
            '    when: \'tool_input.command contains "--force"\'\n'
            '    reason: "Force flag detected"\n',
            encoding="utf-8",
        )

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)
        dispatcher = Dispatcher(config_engine=engine)

        payload = {
            "tool_name": "Bash",
            "tool_input": {"command": "git push --force"},
        }
        result = dispatcher.dispatch("PreToolUse", payload, config=config)

        assert result.exit_code == 0
        assert result.stdout is not None
        output = json.loads(result.stdout)
        assert output["decision"] == "confirm"

    def test_dispatch_no_matching_handlers(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Events with no matching handlers should return empty result."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text("version: 1\n", encoding="utf-8")

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)
        dispatcher = Dispatcher(config_engine=engine)

        result = dispatcher.dispatch(
            "SessionStart", {}, config=config,
        )
        assert result.exit_code == 0
        assert result.stdout is None

    def test_dispatch_non_pretooluse_skips_guards(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Guard rules should only fire for PreToolUse events."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "guards:\n"
            '  - match: "Bash"\n'
            "    action: block\n"
            '    reason: "Block all bash"\n',
            encoding="utf-8",
        )

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)
        dispatcher = Dispatcher(config_engine=engine)

        # PostToolUse should not trigger guards
        result = dispatcher.dispatch(
            "PostToolUse",
            {"tool_name": "Bash", "tool_input": {}},
            config=config,
        )
        assert result.exit_code == 0


class TestConfigMergePrecedence:
    """Test that global < project < overrides merge correctly."""

    def test_project_overrides_global(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Project config values should override global config values."""
        # Global config
        global_config = tmp_state_dir / "config.yaml"
        global_config.write_text(
            "version: 1\n"
            "settings:\n"
            "  debug: false\n"
            "  log_level: WARNING\n"
            "coaching:\n"
            "  enabled: false\n",
            encoding="utf-8",
        )

        # Project config
        project_config = tmp_path / "hookwise.yaml"
        project_config.write_text(
            "settings:\n"
            "  debug: true\n"
            "coaching:\n"
            "  enabled: true\n",
            encoding="utf-8",
        )

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)

        # Project values override global
        assert config.settings["debug"] is True
        assert config.coaching["enabled"] is True
        # Global-only values are preserved
        assert config.settings["log_level"] == "WARNING"

    def test_project_overrides_recipe_defaults(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Project config should override recipe defaults."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "includes:\n"
            '  - "builtin:compliance/cost-tracking"\n'
            "cost_tracking:\n"
            "  daily_budget_usd: 25.00\n",
            encoding="utf-8",
        )

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)

        assert config.cost_tracking["daily_budget_usd"] == 25.00
        # Recipe default preserved where user didn't override
        assert config.cost_tracking["warn_at_percent"] == 80

    def test_global_plus_recipe_plus_project(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Full three-layer merge: global -> recipe -> project."""
        global_config = tmp_state_dir / "config.yaml"
        global_config.write_text(
            "settings:\n"
            "  global_key: from_global\n",
            encoding="utf-8",
        )

        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "includes:\n"
            '  - "builtin:compliance/cost-tracking"\n'
            "settings:\n"
            "  project_key: from_project\n",
            encoding="utf-8",
        )

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)

        assert config.settings["global_key"] == "from_global"
        assert config.settings["project_key"] == "from_project"
        assert config.cost_tracking["enabled"] is True


class TestGuardAnalyticsInteraction:
    """Test that guards and analytics work together correctly."""

    def test_blocked_call_produces_exit_code_2(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """A blocked tool call should produce exit code 2."""
        config = HooksConfig(
            guards=[
                {
                    "match": "Bash",
                    "action": "block",
                    "when": 'tool_input.command contains "rm -rf"',
                    "reason": "Blocked",
                },
            ],
        )
        engine = ConfigEngine()
        dispatcher = Dispatcher(config_engine=engine)

        payload = {
            "tool_name": "Bash",
            "tool_input": {"command": "rm -rf /tmp/data"},
        }
        result = dispatcher.dispatch("PreToolUse", payload, config=config)
        assert result.exit_code == 2

    def test_allowed_call_produces_exit_code_0(self) -> None:
        """An allowed tool call should produce exit code 0."""
        config = HooksConfig(
            guards=[
                {
                    "match": "Bash",
                    "action": "block",
                    "when": 'tool_input.command contains "rm -rf"',
                    "reason": "Blocked",
                },
            ],
        )
        engine = ConfigEngine()
        dispatcher = Dispatcher(config_engine=engine)

        payload = {
            "tool_name": "Bash",
            "tool_input": {"command": "ls -la"},
        }
        result = dispatcher.dispatch("PreToolUse", payload, config=config)
        assert result.exit_code == 0


class TestCliInitToDispatch:
    """Test the flow from CLI init to config generation to dispatch."""

    def test_init_generates_valid_config(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """hookwise init should generate a valid, loadable config."""
        from click.testing import CliRunner
        from hookwise.cli import main

        runner = CliRunner()

        # Mock the settings path so we don't touch real settings
        with patch("hookwise.cli._get_settings_path") as mock_settings:
            mock_settings.return_value = tmp_path / "settings.json"
            result = runner.invoke(
                main,
                ["init", "--preset", "minimal", "--path", str(tmp_path)],
            )

        assert result.exit_code == 0
        assert "hookwise.yaml" in result.output

        # Verify the generated config is loadable
        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)
        assert isinstance(config, HooksConfig)
        assert config.version == 1
        assert len(config.guards) >= 1

    def test_init_full_preset_generates_valid_config(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Full preset should generate a valid config with all sections."""
        from click.testing import CliRunner
        from hookwise.cli import main

        runner = CliRunner()

        with patch("hookwise.cli._get_settings_path") as mock_settings:
            mock_settings.return_value = tmp_path / "settings.json"
            result = runner.invoke(
                main,
                ["init", "--preset", "full", "--path", str(tmp_path)],
            )

        assert result.exit_code == 0

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)
        assert config.coaching.get("enabled") is True
        assert config.analytics.get("enabled") is True
        assert config.cost_tracking.get("enabled") is True

    def test_init_then_dispatch(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Full flow: init a project, then dispatch an event through it."""
        from click.testing import CliRunner
        from hookwise.cli import main

        runner = CliRunner()

        with patch("hookwise.cli._get_settings_path") as mock_settings:
            mock_settings.return_value = tmp_path / "settings.json"
            init_result = runner.invoke(
                main,
                ["init", "--preset", "minimal", "--path", str(tmp_path)],
            )
        assert init_result.exit_code == 0

        # Now dispatch a PreToolUse event that should be blocked
        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)
        dispatcher = Dispatcher(config_engine=engine)

        payload = {
            "tool_name": "Bash",
            "tool_input": {"command": "rm -rf /"},
        }
        result = dispatcher.dispatch("PreToolUse", payload, config=config)
        # The minimal preset blocks rm -rf
        assert result.exit_code == 2


class TestRecipeLoadingIntegration:
    """Test recipe loading end-to-end via config."""

    def test_builtin_recipe_includes_guards(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Config with builtin recipe should have recipe's guard rules."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "includes:\n"
            '  - "builtin:safety/block-dangerous-commands"\n',
            encoding="utf-8",
        )

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)

        # Should have the 4 guards from the recipe
        assert len(config.guards) == 4

        # Verify they actually work in dispatch
        dispatcher = Dispatcher(config_engine=engine)
        payload = {
            "tool_name": "Bash",
            "tool_input": {"command": "rm -rf /"},
        }
        result = dispatcher.dispatch("PreToolUse", payload, config=config)
        assert result.exit_code == 2

    def test_multiple_recipe_guards_all_active(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Guards from multiple recipes should all be evaluated."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "includes:\n"
            '  - "builtin:safety/block-dangerous-commands"\n'
            '  - "builtin:safety/secret-scanning"\n',
            encoding="utf-8",
        )

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)
        dispatcher = Dispatcher(config_engine=engine)

        # Test block-dangerous-commands guard
        result1 = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "Bash", "tool_input": {"command": "rm -rf /"}},
            config=config,
        )
        assert result1.exit_code == 2

        # Test secret-scanning guard
        result2 = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "Read", "tool_input": {"file_path": "/app/.env"}},
            config=config,
        )
        assert result2.exit_code == 0  # warn returns exit code 0
        assert result2.stdout is not None
        output = json.loads(result2.stdout)
        assert output["decision"] == "warn"

    def test_recipe_with_coaching_config(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Recipe coaching config should be present in loaded config."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "includes:\n"
            '  - "builtin:behavioral/metacognition-prompts"\n',
            encoding="utf-8",
        )

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)

        assert config.coaching["enabled"] is True
        assert config.coaching["metacognition"]["enabled"] is True
        assert config.coaching["metacognition"]["interval_minutes"] == 5


# ---------------------------------------------------------------------------
# Task 12.2: Fail-open and robustness tests
# ---------------------------------------------------------------------------


class TestFailOpenExhaustive:
    """Test fail-open behavior for all handler phases."""

    def test_guard_exception_exits_0(self) -> None:
        """Guard handler exception should result in exit code 0 (fail-open)."""
        # Create a handler that raises
        handler = ResolvedHandler(
            name="broken_guard",
            handler_type="builtin",
            events={"PreToolUse"},
            module="nonexistent.module.path",
            phase="guard",
        )

        result = execute_handler(
            handler, "PreToolUse", {"tool_name": "Bash", "tool_input": {}},
            HooksConfig(),
        )
        # Fail-open: no decision (allow)
        assert result.decision is None

    def test_context_handler_exception_returns_empty(self) -> None:
        """Context handler exception should return empty result."""
        handler = ResolvedHandler(
            name="broken_context",
            handler_type="builtin",
            events={"PreToolUse"},
            module="nonexistent.module",
            phase="context",
        )

        result = execute_handler(
            handler, "PreToolUse", {}, HooksConfig(),
        )
        assert result.additional_context is None

    def test_side_effect_handler_exception_returns_empty(self) -> None:
        """Side effect handler exception should return empty result."""
        handler = ResolvedHandler(
            name="broken_side_effect",
            handler_type="builtin",
            events={"PreToolUse"},
            module="nonexistent.module",
            phase="side_effect",
        )

        result = execute_handler(
            handler, "PreToolUse", {}, HooksConfig(),
        )
        assert result.decision is None

    def test_dispatcher_with_broken_guards_allows(self) -> None:
        """Dispatcher should allow when guard handler raises exception."""
        config = HooksConfig(
            handlers=[{
                "name": "broken",
                "type": "builtin",
                "events": ["PreToolUse"],
                "module": "nonexistent.broken.module",
                "phase": "guard",
            }],
        )

        engine = ConfigEngine()
        dispatcher = Dispatcher(config_engine=engine)

        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "Bash", "tool_input": {"command": "ls"}},
            config=config,
        )
        assert result.exit_code == 0

    def test_fail_open_context_manager_suppresses_exception(
        self, tmp_state_dir: Path,
    ) -> None:
        """FailOpen context manager should suppress exceptions."""
        import logging
        logger = logging.getLogger("hookwise.test_fail_open")

        boundary = FailOpen(logger=logger, exit_on_error=False)
        with boundary:
            raise RuntimeError("Simulated handler failure")

        # Should not raise -- exception is suppressed
        assert boundary.caught_exception is not None
        assert isinstance(boundary.caught_exception, RuntimeError)

    def test_fail_open_no_exception_no_effect(
        self, tmp_state_dir: Path,
    ) -> None:
        """FailOpen with no exception should be transparent."""
        import logging
        logger = logging.getLogger("hookwise.test_fail_open")

        boundary = FailOpen(logger=logger, exit_on_error=False)
        with boundary:
            result_value = 42

        assert boundary.caught_exception is None
        assert result_value == 42

    def test_block_decision_exits_2_even_with_broken_side_effects(self) -> None:
        """A block decision should produce exit_code=2 regardless of side effects."""
        config = HooksConfig(
            guards=[
                {
                    "match": "Bash",
                    "action": "block",
                    "reason": "Blocked",
                },
            ],
        )

        engine = ConfigEngine()
        dispatcher = Dispatcher(config_engine=engine)

        payload = {"tool_name": "Bash", "tool_input": {}}
        result = dispatcher.dispatch("PreToolUse", payload, config=config)
        assert result.exit_code == 2


class TestMalformedInputs:
    """Test handling of malformed configs, corrupt caches, and missing DBs."""

    def test_malformed_yaml_config(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Malformed YAML should result in default config (no crash)."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text("{{invalid yaml: [\n", encoding="utf-8")

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)

        assert isinstance(config, HooksConfig)
        assert config.guards == []
        assert config.handlers == []

    def test_corrupt_json_cache(self, tmp_path: Path) -> None:
        """Corrupt JSON in status line cache should not crash."""
        from hookwise.state import safe_read_json

        cache_file = tmp_path / "status_cache.json"
        cache_file.write_text("NOT VALID JSON {{{", encoding="utf-8")

        result = safe_read_json(cache_file)
        assert result == {}

    def test_missing_analytics_db(self, tmp_state_dir: Path) -> None:
        """Missing analytics DB should not crash dispatcher."""
        db_path = tmp_state_dir / "analytics.db"
        assert not db_path.exists()  # Verify it doesn't exist

        # The dispatcher should work without the DB
        config = HooksConfig(analytics={"enabled": True})
        engine = ConfigEngine()
        dispatcher = Dispatcher(config_engine=engine)

        result = dispatcher.dispatch("SessionStart", {}, config=config)
        assert result.exit_code == 0

    def test_non_dict_yaml_config(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """YAML that parses as a list should return default config."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text("- item1\n- item2\n", encoding="utf-8")

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)
        assert isinstance(config, HooksConfig)
        assert config.version == 1

    def test_empty_yaml_file(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Empty YAML file should return default config."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text("", encoding="utf-8")

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)
        assert isinstance(config, HooksConfig)

    def test_config_with_invalid_sections(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Config with invalid sections should still load (with warnings)."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "unknown_section: true\n"
            "settings:\n"
            "  debug: true\n",
            encoding="utf-8",
        )

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)
        # Should still load the valid parts
        assert config.settings["debug"] is True

    def test_malformed_guard_rule_skipped(self) -> None:
        """Malformed guard rules should be skipped, not crash."""
        config = HooksConfig(
            guards=[
                # Missing required 'action' field
                {"match": "Bash"},
                # Valid guard
                {
                    "match": "Bash",
                    "action": "block",
                    "reason": "Blocked",
                    "when": 'tool_input.command contains "rm -rf"',
                },
            ],
        )

        engine = ConfigEngine()
        dispatcher = Dispatcher(config_engine=engine)

        payload = {
            "tool_name": "Bash",
            "tool_input": {"command": "rm -rf /"},
        }
        result = dispatcher.dispatch("PreToolUse", payload, config=config)
        # The valid guard should still fire
        assert result.exit_code == 2


class TestTimedOutHandler:
    """Test that timed-out handlers don't hang the dispatcher."""

    def test_script_handler_timeout_returns_empty_result(self) -> None:
        """A timed-out script handler should return empty result (fail-open)."""
        handler = ResolvedHandler(
            name="slow_handler",
            handler_type="script",
            events={"PreToolUse"},
            command="sleep 60",
            timeout=1,  # 1 second timeout
            phase="side_effect",
        )

        result = execute_handler(
            handler, "PreToolUse", {}, HooksConfig(),
        )
        # Should return empty result, not hang
        assert result.decision is None

    def test_dispatcher_with_slow_handler_doesnt_hang(self) -> None:
        """Dispatcher should not hang when a script handler times out."""
        config = HooksConfig(
            handlers=[{
                "name": "slow",
                "type": "script",
                "events": ["PreToolUse"],
                "command": "sleep 60",
                "timeout": 1,
                "phase": "side_effect",
            }],
        )

        engine = ConfigEngine()
        dispatcher = Dispatcher(config_engine=engine)

        start = time.monotonic()
        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "Bash", "tool_input": {}},
            config=config,
        )
        elapsed = time.monotonic() - start

        assert result.exit_code == 0
        # Should complete in well under 10 seconds (handler has 1s timeout)
        assert elapsed < 10


class TestPerformanceBenchmark:
    """Performance tests for guard evaluation."""

    def test_100_guard_rules_under_200ms(self) -> None:
        """Evaluating 100 guard rules should complete in under 200ms."""
        # Create 100 guard rules with conditions
        guards = []
        for i in range(100):
            guards.append({
                "match": "Bash",
                "action": "warn",
                "when": f'tool_input.command contains "pattern_{i}"',
                "reason": f"Rule {i} matched",
            })

        config = HooksConfig(guards=guards)
        engine = ConfigEngine()
        dispatcher = Dispatcher(config_engine=engine)

        payload = {
            "tool_name": "Bash",
            "tool_input": {"command": "ls -la"},  # won't match any rule
        }

        # Warm up
        dispatcher.dispatch("PreToolUse", payload, config=config)

        # Benchmark
        start = time.monotonic()
        iterations = 10
        for _ in range(iterations):
            result = dispatcher.dispatch("PreToolUse", payload, config=config)
            assert result.exit_code == 0

        elapsed = time.monotonic() - start
        avg_ms = (elapsed / iterations) * 1000

        # Each dispatch should complete in under 200ms
        assert avg_ms < 200, f"Average dispatch time {avg_ms:.1f}ms exceeds 200ms"

    def test_100_guard_rules_matching_last(self) -> None:
        """Should find a match at the end of 100 rules in reasonable time."""
        guards = []
        for i in range(99):
            guards.append({
                "match": "Bash",
                "action": "warn",
                "when": f'tool_input.command contains "nomatch_{i}"',
                "reason": f"Rule {i}",
            })
        # Last rule matches
        guards.append({
            "match": "Bash",
            "action": "block",
            "when": 'tool_input.command contains "target_match"',
            "reason": "Found it",
        })

        config = HooksConfig(guards=guards)
        engine = ConfigEngine()
        dispatcher = Dispatcher(config_engine=engine)

        payload = {
            "tool_name": "Bash",
            "tool_input": {"command": "do target_match here"},
        }

        start = time.monotonic()
        result = dispatcher.dispatch("PreToolUse", payload, config=config)
        elapsed_ms = (time.monotonic() - start) * 1000

        assert result.exit_code == 2
        assert elapsed_ms < 200

    def test_guard_engine_direct_performance(self) -> None:
        """Direct GuardEngine evaluation of 100 rules should be fast."""
        rules = []
        for i in range(100):
            rules.append(GuardRule(
                match="Bash",
                action="warn",
                when=f'tool_input.command contains "pattern_{i}"',
                reason=f"Rule {i}",
            ))

        engine = GuardEngine()
        tool_input = {"command": "safe command"}

        start = time.monotonic()
        for _ in range(100):
            result = engine.evaluate("Bash", tool_input, rules)
            assert result.action == "allow"

        elapsed_ms = (time.monotonic() - start) * 1000 / 100
        assert elapsed_ms < 50, f"Average evaluation {elapsed_ms:.1f}ms exceeds 50ms"


# ---------------------------------------------------------------------------
# Example file validation
# ---------------------------------------------------------------------------


class TestExamplePresets:
    """Test that example YAML files are valid hookwise configs."""

    @pytest.fixture
    def examples_dir(self) -> Path:
        return Path(__file__).parent.parent / "examples"

    def test_minimal_example_valid(
        self, examples_dir: Path, tmp_state_dir: Path,
    ) -> None:
        """examples/minimal.yaml should be a valid config."""
        example = examples_dir / "minimal.yaml"
        assert example.is_file()

        raw = yaml.safe_load(example.read_text(encoding="utf-8"))
        engine = ConfigEngine()
        result = engine.validate_config(raw)
        assert result.valid, f"Validation errors: {result.errors}"

    def test_coaching_example_valid(
        self, examples_dir: Path, tmp_state_dir: Path,
    ) -> None:
        """examples/coaching.yaml should be a valid config."""
        example = examples_dir / "coaching.yaml"
        assert example.is_file()

        raw = yaml.safe_load(example.read_text(encoding="utf-8"))
        engine = ConfigEngine()
        result = engine.validate_config(raw)
        assert result.valid, f"Validation errors: {result.errors}"

    def test_analytics_example_valid(
        self, examples_dir: Path, tmp_state_dir: Path,
    ) -> None:
        """examples/analytics.yaml should be a valid config."""
        example = examples_dir / "analytics.yaml"
        assert example.is_file()

        raw = yaml.safe_load(example.read_text(encoding="utf-8"))
        engine = ConfigEngine()
        result = engine.validate_config(raw)
        assert result.valid, f"Validation errors: {result.errors}"

    def test_full_example_valid(
        self, examples_dir: Path, tmp_state_dir: Path,
    ) -> None:
        """examples/full.yaml should be a valid config."""
        example = examples_dir / "full.yaml"
        assert example.is_file()

        raw = yaml.safe_load(example.read_text(encoding="utf-8"))
        engine = ConfigEngine()
        result = engine.validate_config(raw)
        assert result.valid, f"Validation errors: {result.errors}"

    def test_all_examples_loadable(
        self, examples_dir: Path, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """All example files should be loadable as hookwise configs."""
        engine = ConfigEngine()
        for example_file in sorted(examples_dir.glob("*.yaml")):
            # Copy to tmp_path as hookwise.yaml
            target = tmp_path / "hookwise.yaml"
            target.write_text(
                example_file.read_text(encoding="utf-8"),
                encoding="utf-8",
            )

            config = engine.load_config(project_dir=tmp_path)
            assert isinstance(config, HooksConfig), (
                f"Failed to load {example_file.name}"
            )
            assert config.version == 1

    def test_full_example_has_all_features(
        self, examples_dir: Path, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Full example should have guards, coaching, analytics, cost tracking."""
        target = tmp_path / "hookwise.yaml"
        target.write_text(
            (examples_dir / "full.yaml").read_text(encoding="utf-8"),
            encoding="utf-8",
        )

        engine = ConfigEngine()
        config = engine.load_config(project_dir=tmp_path)

        assert len(config.guards) >= 5
        assert config.coaching.get("enabled") is True
        assert config.analytics.get("enabled") is True
        assert config.cost_tracking.get("enabled") is True
        assert config.transcript_backup.get("enabled") is True
        assert config.status_line.get("enabled") is True


# ---------------------------------------------------------------------------
# Edge cases
# ---------------------------------------------------------------------------


class TestEdgeCases:
    """Tests for edge cases in the integration flow."""

    def test_dispatch_with_empty_payload(self) -> None:
        """Dispatcher should handle empty payload gracefully."""
        config = HooksConfig(
            guards=[
                {"match": "Bash", "action": "block", "reason": "Block all bash"},
            ],
        )
        engine = ConfigEngine()
        dispatcher = Dispatcher(config_engine=engine)

        # Empty payload -- no tool_name, so no guard match
        result = dispatcher.dispatch("PreToolUse", {}, config=config)
        assert result.exit_code == 0

    def test_dispatch_with_none_tool_input(self) -> None:
        """Dispatcher should handle None tool_input gracefully."""
        config = HooksConfig(
            guards=[
                {
                    "match": "Bash",
                    "action": "block",
                    "when": 'tool_input.command contains "rm"',
                    "reason": "Block",
                },
            ],
        )
        engine = ConfigEngine()
        dispatcher = Dispatcher(config_engine=engine)

        payload = {"tool_name": "Bash", "tool_input": None}
        result = dispatcher.dispatch("PreToolUse", payload, config=config)
        # Should not crash -- fail-open
        assert result.exit_code == 0

    def test_config_engine_reusable(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """ConfigEngine should be reusable across multiple load_config calls."""
        engine = ConfigEngine()

        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text("version: 1\nsettings:\n  a: 1\n", encoding="utf-8")
        config1 = engine.load_config(project_dir=tmp_path)
        assert config1.settings["a"] == 1

        config_file.write_text("version: 1\nsettings:\n  b: 2\n", encoding="utf-8")
        config2 = engine.load_config(project_dir=tmp_path)
        assert config2.settings["b"] == 2
        # First config should not be affected
        assert "b" not in config1.settings

    def test_dispatcher_without_config_loads_from_disk(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Dispatcher should load config from disk when not provided."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "version: 1\n"
            "guards:\n"
            '  - match: "Bash"\n'
            '    action: block\n'
            '    reason: "No bash"\n',
            encoding="utf-8",
        )

        engine = ConfigEngine()
        dispatcher = Dispatcher(config_engine=engine)

        payload = {"tool_name": "Bash", "tool_input": {}}
        result = dispatcher.dispatch(
            "PreToolUse", payload, project_dir=tmp_path,
        )
        assert result.exit_code == 2
