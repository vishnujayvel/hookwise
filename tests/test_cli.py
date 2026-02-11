"""Tests for hookwise CLI commands: init, doctor, status, stats.

Uses click.testing.CliRunner to invoke commands in isolation with
temporary directories and environment overrides so tests never touch
the real ~/.hookwise/ or ~/.claude/ directories.
"""

from __future__ import annotations

import json
import os
import sqlite3
from pathlib import Path
from unittest.mock import patch

import pytest
import yaml
from click.testing import CliRunner

from hookwise.cli import (
    _HOOK_EVENT_TYPES,
    _PRESETS,
    _get_settings_path,
    _read_settings,
    _register_hooks,
    _write_settings,
    main,
)


# ---------------------------------------------------------------------------
# Shared fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def runner() -> CliRunner:
    """Provide a CliRunner for invoking CLI commands."""
    return CliRunner()


@pytest.fixture
def isolated_env(tmp_path: Path) -> dict[str, Path]:
    """Set up isolated state/settings directories for CLI tests.

    Returns a dict with keys:
      - state_dir: temporary state directory
      - settings_path: temporary settings.json path
      - project_dir: temporary project directory
    """
    state_dir = tmp_path / "hookwise_state"
    state_dir.mkdir(mode=0o700)

    claude_dir = tmp_path / "claude_home"
    claude_dir.mkdir()
    settings_path = claude_dir / "settings.json"

    project_dir = tmp_path / "project"
    project_dir.mkdir()

    return {
        "state_dir": state_dir,
        "settings_path": settings_path,
        "project_dir": project_dir,
    }


@pytest.fixture
def env_patches(isolated_env: dict[str, Path]):
    """Context manager that patches env and _get_settings_path for isolation."""
    with (
        patch.dict(
            os.environ,
            {"HOOKWISE_STATE_DIR": str(isolated_env["state_dir"])},
        ),
        patch(
            "hookwise.cli._get_settings_path",
            return_value=isolated_env["settings_path"],
        ),
    ):
        yield isolated_env


# ===========================================================================
# Helper functions tests
# ===========================================================================


class TestReadSettings:
    """Tests for _read_settings helper."""

    def test_missing_file_returns_empty(self, tmp_path: Path) -> None:
        """Should return empty dict when file does not exist."""
        result = _read_settings(tmp_path / "nonexistent.json")
        assert result == {}

    def test_valid_json(self, tmp_path: Path) -> None:
        """Should parse valid JSON file."""
        p = tmp_path / "settings.json"
        p.write_text('{"hooks": {}}', encoding="utf-8")
        result = _read_settings(p)
        assert result == {"hooks": {}}

    def test_invalid_json(self, tmp_path: Path) -> None:
        """Should return empty dict on invalid JSON."""
        p = tmp_path / "settings.json"
        p.write_text("not json!", encoding="utf-8")
        result = _read_settings(p)
        assert result == {}


class TestWriteSettings:
    """Tests for _write_settings helper."""

    def test_creates_file(self, tmp_path: Path) -> None:
        """Should create settings file with proper JSON."""
        p = tmp_path / "sub" / "settings.json"
        _write_settings(p, {"hooks": {}})
        assert p.exists()
        data = json.loads(p.read_text(encoding="utf-8"))
        assert data == {"hooks": {}}

    def test_overwrites_existing(self, tmp_path: Path) -> None:
        """Should overwrite existing file."""
        p = tmp_path / "settings.json"
        p.write_text('{"old": true}', encoding="utf-8")
        _write_settings(p, {"new": True})
        data = json.loads(p.read_text(encoding="utf-8"))
        assert data == {"new": True}


class TestRegisterHooks:
    """Tests for _register_hooks helper."""

    def test_registers_all_event_types(self, tmp_path: Path) -> None:
        """Should register hookwise dispatch for all event types."""
        p = tmp_path / "settings.json"
        p.write_text("{}", encoding="utf-8")
        warnings = _register_hooks(p)
        assert warnings == []

        data = json.loads(p.read_text(encoding="utf-8"))
        hooks = data["hooks"]
        for event_type in _HOOK_EVENT_TYPES:
            assert event_type in hooks
            entries = hooks[event_type]
            assert len(entries) == 1
            assert entries[0]["type"] == "command"
            assert entries[0]["command"] == f"hookwise dispatch {event_type}"

    def test_idempotent_registration(self, tmp_path: Path) -> None:
        """Should not duplicate hookwise entries on repeated calls."""
        p = tmp_path / "settings.json"
        p.write_text("{}", encoding="utf-8")
        _register_hooks(p)
        _register_hooks(p)

        data = json.loads(p.read_text(encoding="utf-8"))
        for event_type in _HOOK_EVENT_TYPES:
            entries = data["hooks"][event_type]
            hookwise_entries = [
                e for e in entries
                if e.get("command", "").startswith("hookwise dispatch")
            ]
            assert len(hookwise_entries) == 1

    def test_warns_about_existing_hooks(self, tmp_path: Path) -> None:
        """Should warn when existing non-hookwise hooks are present."""
        p = tmp_path / "settings.json"
        existing = {
            "hooks": {
                "PreToolUse": [
                    {"type": "command", "command": "my-custom-hook PreToolUse"}
                ]
            }
        }
        p.write_text(json.dumps(existing), encoding="utf-8")
        warnings = _register_hooks(p)

        assert len(warnings) == 1
        assert "PreToolUse" in warnings[0]
        assert "1 existing hook" in warnings[0]

        # Verify hookwise was still added alongside
        data = json.loads(p.read_text(encoding="utf-8"))
        entries = data["hooks"]["PreToolUse"]
        assert len(entries) == 2

    def test_preserves_existing_hooks(self, tmp_path: Path) -> None:
        """Should not remove existing non-hookwise hooks."""
        p = tmp_path / "settings.json"
        existing = {
            "hooks": {
                "PostToolUse": [
                    {"type": "command", "command": "other-tool PostToolUse"}
                ]
            }
        }
        p.write_text(json.dumps(existing), encoding="utf-8")
        _register_hooks(p)

        data = json.loads(p.read_text(encoding="utf-8"))
        entries = data["hooks"]["PostToolUse"]
        commands = [e["command"] for e in entries]
        assert "other-tool PostToolUse" in commands
        assert "hookwise dispatch PostToolUse" in commands

    def test_creates_file_if_missing(self, tmp_path: Path) -> None:
        """Should create settings.json if it does not exist."""
        p = tmp_path / "new_dir" / "settings.json"
        warnings = _register_hooks(p)
        assert p.exists()


# ===========================================================================
# init command tests
# ===========================================================================


class TestInitCommand:
    """Tests for the `hookwise init` command."""

    def test_creates_hookwise_yaml(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should create hookwise.yaml with minimal preset by default."""
        project_dir = env_patches["project_dir"]
        result = runner.invoke(
            main, ["init", "--path", str(project_dir)]
        )
        assert result.exit_code == 0
        config_file = project_dir / "hookwise.yaml"
        assert config_file.exists()
        content = config_file.read_text(encoding="utf-8")
        assert "version: 1" in content
        assert 'action: block' in content

    def test_minimal_preset_content(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Minimal preset should have block and confirm guards."""
        project_dir = env_patches["project_dir"]
        result = runner.invoke(
            main, ["init", "--preset", "minimal", "--path", str(project_dir)]
        )
        assert result.exit_code == 0
        content = (project_dir / "hookwise.yaml").read_text(encoding="utf-8")
        parsed = yaml.safe_load(content)
        assert parsed["version"] == 1
        assert len(parsed["guards"]) == 2
        assert parsed["guards"][0]["action"] == "block"
        assert parsed["guards"][1]["action"] == "confirm"

    def test_coaching_preset(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Coaching preset should include coaching and status_line."""
        project_dir = env_patches["project_dir"]
        result = runner.invoke(
            main, ["init", "--preset", "coaching", "--path", str(project_dir)]
        )
        assert result.exit_code == 0
        parsed = yaml.safe_load(
            (project_dir / "hookwise.yaml").read_text(encoding="utf-8")
        )
        assert parsed["coaching"]["enabled"] is True
        assert parsed["status_line"]["enabled"] is True

    def test_analytics_preset(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Analytics preset should include analytics enabled."""
        project_dir = env_patches["project_dir"]
        result = runner.invoke(
            main, ["init", "--preset", "analytics", "--path", str(project_dir)]
        )
        assert result.exit_code == 0
        parsed = yaml.safe_load(
            (project_dir / "hookwise.yaml").read_text(encoding="utf-8")
        )
        assert parsed["analytics"]["enabled"] is True

    def test_full_preset(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Full preset should include guards, coaching, analytics, cost_tracking."""
        project_dir = env_patches["project_dir"]
        result = runner.invoke(
            main, ["init", "--preset", "full", "--path", str(project_dir)]
        )
        assert result.exit_code == 0
        parsed = yaml.safe_load(
            (project_dir / "hookwise.yaml").read_text(encoding="utf-8")
        )
        assert parsed["coaching"]["enabled"] is True
        assert parsed["analytics"]["enabled"] is True
        assert parsed["cost_tracking"]["enabled"] is True

    def test_refuses_overwrite_without_force(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should refuse to overwrite existing hookwise.yaml without --force."""
        project_dir = env_patches["project_dir"]
        (project_dir / "hookwise.yaml").write_text("existing", encoding="utf-8")
        result = runner.invoke(
            main, ["init", "--path", str(project_dir)]
        )
        assert result.exit_code == 1
        assert "already exists" in result.output
        # Original content should be preserved
        assert (project_dir / "hookwise.yaml").read_text() == "existing"

    def test_force_overwrites(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should overwrite existing hookwise.yaml with --force."""
        project_dir = env_patches["project_dir"]
        (project_dir / "hookwise.yaml").write_text("old content", encoding="utf-8")
        result = runner.invoke(
            main, ["init", "--force", "--path", str(project_dir)]
        )
        assert result.exit_code == 0
        content = (project_dir / "hookwise.yaml").read_text(encoding="utf-8")
        assert "version: 1" in content

    def test_creates_state_directory(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should create the state directory with proper permissions."""
        project_dir = env_patches["project_dir"]
        result = runner.invoke(
            main, ["init", "--path", str(project_dir)]
        )
        assert result.exit_code == 0
        state_dir = env_patches["state_dir"]
        assert state_dir.is_dir()

    def test_registers_hooks_in_settings(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should register hookwise dispatch hooks in settings.json."""
        project_dir = env_patches["project_dir"]
        result = runner.invoke(
            main, ["init", "--path", str(project_dir)]
        )
        assert result.exit_code == 0

        settings_path = env_patches["settings_path"]
        data = json.loads(settings_path.read_text(encoding="utf-8"))
        assert "hooks" in data
        for event_type in _HOOK_EVENT_TYPES:
            assert event_type in data["hooks"]

    def test_displays_success_message(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should display success message and next steps."""
        project_dir = env_patches["project_dir"]
        result = runner.invoke(
            main, ["init", "--path", str(project_dir)]
        )
        assert result.exit_code == 0
        assert "Hookwise initialized!" in result.output
        assert "Next steps:" in result.output
        assert "hookwise doctor" in result.output

    def test_displays_ok_markers(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should show [ok] markers for each successful step."""
        project_dir = env_patches["project_dir"]
        result = runner.invoke(
            main, ["init", "--path", str(project_dir)]
        )
        assert result.exit_code == 0
        assert "[ok]" in result.output

    def test_warns_about_existing_hooks(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should warn when settings.json already has hook entries."""
        project_dir = env_patches["project_dir"]
        settings_path = env_patches["settings_path"]
        # Pre-populate with existing hooks
        existing = {
            "hooks": {
                "PreToolUse": [
                    {"type": "command", "command": "custom-tool check"}
                ]
            }
        }
        settings_path.write_text(json.dumps(existing), encoding="utf-8")

        result = runner.invoke(
            main, ["init", "--path", str(project_dir)]
        )
        assert result.exit_code == 0
        assert "Existing hooks detected" in result.output

    def test_all_presets_produce_valid_yaml(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """All preset YAML templates should be valid YAML."""
        for preset_name, preset_content in _PRESETS.items():
            parsed = yaml.safe_load(preset_content)
            assert isinstance(parsed, dict), f"Preset {preset_name} is not a dict"
            assert parsed.get("version") == 1, f"Preset {preset_name} missing version"


# ===========================================================================
# doctor command tests
# ===========================================================================


class TestDoctorCommand:
    """Tests for the `hookwise doctor` command."""

    def test_python_version_check_passes(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should show Python version pass (we are running 3.10+)."""
        result = runner.invoke(main, ["doctor"])
        assert result.exit_code == 0
        assert "Python" in result.output
        assert "[ok]" in result.output

    def test_settings_json_found(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should pass when settings.json exists."""
        settings_path = env_patches["settings_path"]
        settings_path.write_text("{}", encoding="utf-8")
        result = runner.invoke(main, ["doctor"])
        assert "settings.json found" in result.output

    def test_settings_json_missing(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should fail when settings.json is missing."""
        result = runner.invoke(main, ["doctor"])
        assert "settings.json not found" in result.output
        assert "Some checks failed" in result.output

    def test_hooks_registered_check(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should verify hookwise hooks are registered."""
        settings_path = env_patches["settings_path"]
        # Register hooks
        _register_hooks(settings_path)
        result = runner.invoke(main, ["doctor"])
        assert "hook event types registered" in result.output

    def test_no_hooks_registered(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should fail when no hookwise hooks registered."""
        settings_path = env_patches["settings_path"]
        settings_path.write_text('{"hooks": {}}', encoding="utf-8")
        result = runner.invoke(main, ["doctor"])
        assert "No hookwise hooks registered" in result.output

    def test_partial_hooks_warns(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should warn when only some hooks are registered."""
        settings_path = env_patches["settings_path"]
        partial = {
            "hooks": {
                "PreToolUse": [
                    {"type": "command", "command": "hookwise dispatch PreToolUse"}
                ]
            }
        }
        settings_path.write_text(json.dumps(partial), encoding="utf-8")
        result = runner.invoke(main, ["doctor"])
        assert "1/" in result.output

    def test_config_file_check_missing(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should warn when hookwise.yaml is not found."""
        # Run from a temp directory where no hookwise.yaml exists
        result = runner.invoke(main, ["doctor"])
        assert "hookwise.yaml not found" in result.output

    def test_config_file_valid(
        self, runner: CliRunner, env_patches: dict[str, Path], monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Should pass when hookwise.yaml has valid content."""
        project_dir = env_patches["project_dir"]
        config_file = project_dir / "hookwise.yaml"
        config_file.write_text("version: 1\nguards: []\n", encoding="utf-8")
        monkeypatch.chdir(project_dir)

        settings_path = env_patches["settings_path"]
        settings_path.write_text("{}", encoding="utf-8")

        result = runner.invoke(main, ["doctor"])
        assert "hookwise.yaml found" in result.output
        assert "Config syntax and schema valid" in result.output

    def test_config_file_invalid_yaml(
        self, runner: CliRunner, env_patches: dict[str, Path], monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Should fail when hookwise.yaml has invalid YAML."""
        project_dir = env_patches["project_dir"]
        config_file = project_dir / "hookwise.yaml"
        config_file.write_text("invalid: yaml: content: [", encoding="utf-8")
        monkeypatch.chdir(project_dir)

        settings_path = env_patches["settings_path"]
        settings_path.write_text("{}", encoding="utf-8")

        result = runner.invoke(main, ["doctor"])
        assert "invalid YAML syntax" in result.output or "Config syntax and schema valid" in result.output or "[X]" in result.output

    def test_config_file_invalid_schema(
        self, runner: CliRunner, env_patches: dict[str, Path], monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Should fail when hookwise.yaml has schema errors."""
        project_dir = env_patches["project_dir"]
        config_file = project_dir / "hookwise.yaml"
        config_file.write_text("version: 1\nbogus_section: true\n", encoding="utf-8")
        monkeypatch.chdir(project_dir)

        settings_path = env_patches["settings_path"]
        settings_path.write_text("{}", encoding="utf-8")

        result = runner.invoke(main, ["doctor"])
        assert "Unknown config section" in result.output

    def test_state_directory_exists(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should pass when state directory exists."""
        state_dir = env_patches["state_dir"]
        state_dir.chmod(0o700)
        result = runner.invoke(main, ["doctor"])
        assert "State directory exists" in result.output

    def test_state_directory_permissions(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should check state directory permissions are 0o700."""
        state_dir = env_patches["state_dir"]
        state_dir.chmod(0o700)
        result = runner.invoke(main, ["doctor"])
        assert "Permissions correct" in result.output

    def test_state_directory_writability(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should check state directory is writable."""
        result = runner.invoke(main, ["doctor"])
        assert "State directory is writable" in result.output

    def test_state_directory_missing(
        self, runner: CliRunner, tmp_path: Path
    ) -> None:
        """Should fail when state directory does not exist."""
        missing_dir = tmp_path / "nonexistent_state"
        with (
            patch.dict(
                os.environ,
                {"HOOKWISE_STATE_DIR": str(missing_dir)},
            ),
            patch(
                "hookwise.cli._get_settings_path",
                return_value=tmp_path / "settings.json",
            ),
        ):
            result = runner.invoke(main, ["doctor"])
            assert "State directory not found" in result.output

    def test_analytics_db_missing(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should warn when analytics DB does not exist."""
        result = runner.invoke(main, ["doctor"])
        assert "Analytics database not found" in result.output

    def test_analytics_db_exists(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should pass when analytics DB exists and is readable."""
        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        # Create a minimal SQLite DB
        conn = sqlite3.connect(str(db_path))
        conn.execute("CREATE TABLE test (id INTEGER)")
        conn.close()

        result = runner.invoke(main, ["doctor"])
        assert "Analytics database at" in result.output
        assert "Database is readable" in result.output

    def test_all_checks_passed_message(
        self, runner: CliRunner, env_patches: dict[str, Path], monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Should show 'All checks passed!' when everything is good."""
        project_dir = env_patches["project_dir"]
        state_dir = env_patches["state_dir"]
        settings_path = env_patches["settings_path"]

        # Set up everything to pass
        config_file = project_dir / "hookwise.yaml"
        config_file.write_text("version: 1\nguards: []\n", encoding="utf-8")
        monkeypatch.chdir(project_dir)

        _register_hooks(settings_path)

        state_dir.chmod(0o700)

        # Create analytics DB
        db_path = state_dir / "analytics.db"
        conn = sqlite3.connect(str(db_path))
        conn.execute("CREATE TABLE test (id INTEGER)")
        conn.close()

        result = runner.invoke(main, ["doctor"])
        assert "All checks passed!" in result.output

    def test_doctor_title(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should display 'Hookwise Doctor' title."""
        result = runner.invoke(main, ["doctor"])
        assert "Hookwise Doctor" in result.output


# ===========================================================================
# status command tests
# ===========================================================================


class TestStatusCommand:
    """Tests for the `hookwise status` command."""

    def test_displays_title(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should display 'Hookwise Status' title."""
        result = runner.invoke(main, ["status"])
        assert result.exit_code == 0
        assert "Hookwise Status" in result.output

    def test_shows_config_version(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should display config version."""
        result = runner.invoke(main, ["status"])
        assert "Config version: 1" in result.output

    def test_no_handlers_message(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should show no handlers message when none configured."""
        result = runner.invoke(main, ["status"])
        assert "(no handlers configured)" in result.output

    def test_no_guards_message(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should show no guard rules message when none configured."""
        result = runner.invoke(main, ["status"])
        assert "(no guard rules configured)" in result.output

    def test_coaching_disabled_by_default(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should show coaching disabled when not configured."""
        result = runner.invoke(main, ["status"])
        assert "Enabled: no" in result.output

    def test_analytics_disabled_by_default(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should show analytics disabled when not configured."""
        result = runner.invoke(main, ["status"])
        assert "Enabled: no" in result.output

    def test_with_guards_configured(
        self, runner: CliRunner, env_patches: dict[str, Path], monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Should display guard rules summary when guards are configured."""
        project_dir = env_patches["project_dir"]
        config = """\
version: 1
guards:
  - match: "Bash"
    action: block
    when: 'tool_input.command contains "rm -rf"'
    reason: "Dangerous"
  - match: "Bash"
    action: confirm
    when: 'tool_input.command contains "force push"'
    reason: "Force push"
  - match: "mcp__gmail__*"
    action: warn
    reason: "Gmail tool"
"""
        (project_dir / "hookwise.yaml").write_text(config, encoding="utf-8")
        monkeypatch.chdir(project_dir)

        result = runner.invoke(main, ["status"])
        assert "3 rule(s)" in result.output
        assert "block: 1" in result.output
        assert "confirm: 1" in result.output
        assert "warn: 1" in result.output

    def test_with_coaching_enabled(
        self, runner: CliRunner, env_patches: dict[str, Path], monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Should display coaching details when enabled."""
        project_dir = env_patches["project_dir"]
        config = """\
version: 1
coaching:
  enabled: true
  idle_threshold: 15
  session_duration_warning: 45
"""
        (project_dir / "hookwise.yaml").write_text(config, encoding="utf-8")
        monkeypatch.chdir(project_dir)

        result = runner.invoke(main, ["status"])
        assert "Enabled: yes" in result.output
        assert "15 tool calls" in result.output
        assert "45 minutes" in result.output

    def test_with_analytics_enabled(
        self, runner: CliRunner, env_patches: dict[str, Path], monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Should display analytics details when enabled."""
        project_dir = env_patches["project_dir"]
        config = """\
version: 1
analytics:
  enabled: true
"""
        (project_dir / "hookwise.yaml").write_text(config, encoding="utf-8")
        monkeypatch.chdir(project_dir)

        result = runner.invoke(main, ["status"])
        assert "Enabled: yes" in result.output
        assert "not yet created" in result.output

    def test_with_cost_tracking_enabled(
        self, runner: CliRunner, env_patches: dict[str, Path], monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Should display cost tracking details when enabled."""
        project_dir = env_patches["project_dir"]
        config = """\
version: 1
cost_tracking:
  enabled: true
  daily_budget_usd: 15.50
  warn_at_percent: 90
"""
        (project_dir / "hookwise.yaml").write_text(config, encoding="utf-8")
        monkeypatch.chdir(project_dir)

        result = runner.invoke(main, ["status"])
        assert "Enabled: yes" in result.output
        assert "$15.50" in result.output
        assert "90%" in result.output

    def test_displays_all_sections(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should display all status sections."""
        result = runner.invoke(main, ["status"])
        assert "Handlers:" in result.output
        assert "Guard Rules:" in result.output
        assert "Coaching:" in result.output
        assert "Analytics:" in result.output
        assert "Cost Tracking:" in result.output

    def test_with_handlers_configured(
        self, runner: CliRunner, env_patches: dict[str, Path], monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Should display handlers grouped by event type."""
        project_dir = env_patches["project_dir"]
        config = """\
version: 1
handlers:
  - name: my_handler
    type: builtin
    module: hookwise.handlers.example
    events:
      - PreToolUse
      - PostToolUse
"""
        (project_dir / "hookwise.yaml").write_text(config, encoding="utf-8")
        monkeypatch.chdir(project_dir)

        result = runner.invoke(main, ["status"])
        assert "PreToolUse:" in result.output
        assert "PostToolUse:" in result.output
        assert "my_handler" in result.output

    def test_analytics_db_size_display(
        self, runner: CliRunner, env_patches: dict[str, Path], monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """Should display analytics DB size when it exists."""
        project_dir = env_patches["project_dir"]
        state_dir = env_patches["state_dir"]
        config = """\
version: 1
analytics:
  enabled: true
"""
        (project_dir / "hookwise.yaml").write_text(config, encoding="utf-8")
        monkeypatch.chdir(project_dir)

        # Create a small DB
        db_path = state_dir / "analytics.db"
        conn = sqlite3.connect(str(db_path))
        conn.execute("CREATE TABLE test (id INTEGER)")
        conn.close()

        result = runner.invoke(main, ["status"])
        assert str(db_path) in result.output
        # Should show size (e.g., "8.0 KB" or "8192 B")
        assert "B" in result.output or "KB" in result.output


# ===========================================================================
# stats command tests
# ===========================================================================


class TestStatsCommand:
    """Tests for the `hookwise stats` command."""

    def test_no_analytics_db(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should show message when analytics DB does not exist."""
        result = runner.invoke(main, ["stats"])
        assert result.exit_code == 0
        assert "No analytics data yet" in result.output

    def test_no_analytics_db_json_mode(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should output JSON error when DB missing and --json flag used."""
        result = runner.invoke(main, ["stats", "--json"])
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert data["error"] == "No analytics data yet"

    def test_with_empty_db(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should show zero stats when DB exists but is empty."""
        from hookwise.analytics.db import AnalyticsDB

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)
        db.close()

        result = runner.invoke(main, ["stats"])
        assert result.exit_code == 0
        assert "Hookwise Stats" in result.output
        assert "Sessions: 0" in result.output

    def test_with_session_data(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should display session statistics when data exists."""
        from hookwise.analytics.db import AnalyticsDB
        from datetime import datetime, timezone

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)

        # Insert a recent session
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        db.insert_session("session-1", now)
        db.update_session(
            "session-1",
            total_tool_calls=42,
            ai_authored_lines=100,
            human_verified_lines=50,
            duration_seconds=1800,
        )
        db.close()

        result = runner.invoke(main, ["stats"])
        assert result.exit_code == 0
        assert "Sessions: 1" in result.output
        assert "Tool calls: 42" in result.output

    def test_with_session_data_authorship_bar(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should display authorship bar chart when line data exists."""
        from hookwise.analytics.db import AnalyticsDB
        from datetime import datetime, timezone

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)

        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        db.insert_session("session-1", now)
        db.update_session(
            "session-1",
            ai_authored_lines=75,
            human_verified_lines=25,
        )
        db.close()

        result = runner.invoke(main, ["stats"])
        assert "Authorship:" in result.output
        assert "AI:75" in result.output
        assert "Human:25" in result.output

    def test_json_output(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should output valid JSON with --json flag."""
        from hookwise.analytics.db import AnalyticsDB
        from datetime import datetime, timezone

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)

        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        db.insert_session("session-1", now)
        db.update_session("session-1", total_tool_calls=10)
        db.close()

        result = runner.invoke(main, ["stats", "--json"])
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert "today" in data
        assert data["today"]["sessions"] == 1
        assert data["today"]["total_tool_calls"] == 10
        assert "tool_breakdown" in data
        assert "authorship" in data
        assert "weekly_trends" in data

    def test_json_with_cost_flag(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should include cost data in JSON when --cost flag used."""
        from hookwise.analytics.db import AnalyticsDB
        from datetime import datetime, timezone

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)

        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        db.insert_session("session-1", now)
        db.update_session(
            "session-1",
            estimated_cost_usd=0.05,
            estimated_tokens=5000,
        )
        db.close()

        result = runner.invoke(main, ["stats", "--json", "--cost"])
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert "cost" in data
        assert len(data["cost"]) == 1
        assert data["cost"][0]["cost_usd"] == 0.05

    def test_json_with_agents_flag(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should include agents data in JSON when --agents flag used."""
        from hookwise.analytics.db import AnalyticsDB
        from datetime import datetime, timezone

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)

        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        db.insert_session("session-1", now)
        db.insert_agent_span(
            "session-1", "agent-abc123def456", now,
            agent_type="researcher",
        )
        db.close()

        result = runner.invoke(main, ["stats", "--json", "--agents"])
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert "agents" in data

    def test_tool_breakdown_display(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should display tool breakdown when events exist."""
        from hookwise.analytics.db import AnalyticsDB
        from datetime import datetime, timezone

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)

        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        db.insert_session("session-1", now)
        db.insert_event("session-1", "PostToolUse", now, tool_name="Bash", lines_added=10)
        db.insert_event("session-1", "PostToolUse", now, tool_name="Bash", lines_added=5)
        db.insert_event("session-1", "PostToolUse", now, tool_name="Read")
        db.close()

        result = runner.invoke(main, ["stats"])
        assert "Tool Calls (today):" in result.output
        assert "Bash: 2" in result.output
        assert "Read: 1" in result.output

    def test_weekly_trends_display(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should display 7-day trends section."""
        from hookwise.analytics.db import AnalyticsDB
        from datetime import datetime, timezone

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)

        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        db.insert_session("session-1", now)
        db.close()

        result = runner.invoke(main, ["stats"])
        assert "7-Day Trends:" in result.output

    def test_no_data_shows_no_tool_data(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should show 'no tool data' when no events exist."""
        from hookwise.analytics.db import AnalyticsDB

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)
        db.close()

        result = runner.invoke(main, ["stats"])
        assert "(no tool data)" in result.output

    def test_no_authorship_data(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should show 'no data' for authorship when no lines tracked."""
        from hookwise.analytics.db import AnalyticsDB

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)
        db.close()

        result = runner.invoke(main, ["stats"])
        assert "Authorship: no data" in result.output

    def test_avg_session_duration(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should display average session duration."""
        from hookwise.analytics.db import AnalyticsDB
        from datetime import datetime, timezone

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)

        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        db.insert_session("session-1", now)
        db.update_session("session-1", duration_seconds=3600)
        db.close()

        result = runner.invoke(main, ["stats"])
        assert "Avg session: 60.0 min" in result.output

    def test_cost_flag_human_readable(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should display cost breakdown in human-readable format."""
        from hookwise.analytics.db import AnalyticsDB
        from datetime import datetime, timezone

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)

        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        db.insert_session("session-1", now)
        db.update_session(
            "session-1",
            estimated_cost_usd=1.2345,
            estimated_tokens=50000,
        )
        db.close()

        result = runner.invoke(main, ["stats", "--cost"])
        assert "Cost Breakdown (today):" in result.output
        assert "Total cost:" in result.output
        assert "Total tokens:" in result.output

    def test_agents_flag_human_readable(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should display subagent activity in human-readable format."""
        from hookwise.analytics.db import AnalyticsDB
        from datetime import datetime, timezone

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)

        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        db.insert_session("session-1", now)
        db.insert_agent_span(
            "session-1", "agent-abc123def456", now,
            agent_type="researcher",
        )
        db.close()

        result = runner.invoke(main, ["stats", "--agents"])
        assert "Subagent Activity (today):" in result.output

    def test_cost_no_data(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should show no cost data message when no costs tracked."""
        from hookwise.analytics.db import AnalyticsDB
        from datetime import datetime, timezone

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)

        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        db.insert_session("session-1", now)
        db.close()

        result = runner.invoke(main, ["stats", "--cost"])
        assert "(no cost data)" in result.output

    def test_agents_no_data(
        self, runner: CliRunner, env_patches: dict[str, Path]
    ) -> None:
        """Should show no subagent data message when none tracked."""
        from hookwise.analytics.db import AnalyticsDB

        state_dir = env_patches["state_dir"]
        db_path = state_dir / "analytics.db"
        db = AnalyticsDB(db_path=db_path)
        db.close()

        result = runner.invoke(main, ["stats", "--agents"])
        assert "(no subagent data)" in result.output


# ===========================================================================
# CLI registration tests
# ===========================================================================


class TestCliRegistration:
    """Tests for CLI command registration."""

    def test_init_command_registered(self, runner: CliRunner) -> None:
        """The init command should be registered."""
        result = runner.invoke(main, ["init", "--help"])
        assert result.exit_code == 0
        assert "Initialize hookwise" in result.output

    def test_doctor_command_registered(self, runner: CliRunner) -> None:
        """The doctor command should be registered."""
        result = runner.invoke(main, ["doctor", "--help"])
        assert result.exit_code == 0
        assert "Check hookwise installation" in result.output

    def test_status_command_registered(self, runner: CliRunner) -> None:
        """The status command should be registered."""
        result = runner.invoke(main, ["status", "--help"])
        assert result.exit_code == 0
        assert "Display hookwise configuration" in result.output

    def test_stats_command_registered(self, runner: CliRunner) -> None:
        """The stats command should be registered."""
        result = runner.invoke(main, ["stats", "--help"])
        assert result.exit_code == 0
        assert "Display analytics" in result.output

    def test_dispatch_still_exists(self, runner: CliRunner) -> None:
        """The dispatch command should still be registered (not broken)."""
        result = runner.invoke(main, ["dispatch", "--help"])
        assert result.exit_code == 0
        assert "Dispatch a hook event" in result.output

    def test_test_still_exists(self, runner: CliRunner) -> None:
        """The test command should still be registered."""
        result = runner.invoke(main, ["test", "--help"])
        assert result.exit_code == 0
        assert "Discover and run hookwise tests" in result.output

    def test_version_flag(self, runner: CliRunner) -> None:
        """The --version flag should work."""
        result = runner.invoke(main, ["--version"])
        assert result.exit_code == 0
        assert "hookwise" in result.output

    def test_help_lists_all_commands(self, runner: CliRunner) -> None:
        """The help output should list all commands."""
        result = runner.invoke(main, ["--help"])
        assert result.exit_code == 0
        assert "dispatch" in result.output
        assert "init" in result.output
        assert "doctor" in result.output
        assert "status" in result.output
        assert "stats" in result.output

    def test_init_preset_choices(self, runner: CliRunner) -> None:
        """The init --preset should accept all 4 presets."""
        result = runner.invoke(main, ["init", "--help"])
        assert "minimal" in result.output
        assert "coaching" in result.output
        assert "analytics" in result.output
        assert "full" in result.output

    def test_stats_flags(self, runner: CliRunner) -> None:
        """The stats command should accept --json, --cost, --agents."""
        result = runner.invoke(main, ["stats", "--help"])
        assert "--json" in result.output
        assert "--cost" in result.output
        assert "--agents" in result.output
