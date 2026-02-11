"""Tests for hookwise.dispatcher -- event dispatch and handler execution."""

from __future__ import annotations

import io
import json
from pathlib import Path
from typing import Any
from unittest.mock import patch

import pytest

from hookwise.config import (
    ConfigEngine,
    HooksConfig,
    ResolvedHandler,
    DEFAULT_HANDLER_TIMEOUT,
)
from hookwise.dispatcher import (
    Dispatcher,
    DispatchResult,
    HandlerResult,
    execute_handler,
    read_stdin_payload,
    _execute_inline_handler,
    _execute_script_handler,
)


# ---------------------------------------------------------------------------
# HandlerResult dataclass
# ---------------------------------------------------------------------------


class TestHandlerResult:
    """Tests for the HandlerResult dataclass."""

    def test_default_values(self) -> None:
        """Default HandlerResult should have all None fields."""
        result = HandlerResult()
        assert result.decision is None
        assert result.reason is None
        assert result.additional_context is None
        assert result.output is None

    def test_custom_values(self) -> None:
        """Should accept custom values."""
        result = HandlerResult(
            decision="block",
            reason="forbidden",
            additional_context="extra info",
            output={"key": "value"},
        )
        assert result.decision == "block"
        assert result.reason == "forbidden"
        assert result.additional_context == "extra info"
        assert result.output == {"key": "value"}


# ---------------------------------------------------------------------------
# DispatchResult dataclass
# ---------------------------------------------------------------------------


class TestDispatchResult:
    """Tests for the DispatchResult dataclass."""

    def test_default_values(self) -> None:
        """Default DispatchResult should have exit_code=0 and no output."""
        result = DispatchResult()
        assert result.stdout is None
        assert result.stderr is None
        assert result.exit_code == 0

    def test_block_result(self) -> None:
        """Block result should have exit_code=2."""
        result = DispatchResult(
            stdout='{"decision": "block"}',
            exit_code=2,
        )
        assert result.exit_code == 2


# ---------------------------------------------------------------------------
# read_stdin_payload
# ---------------------------------------------------------------------------


class TestReadStdinPayload:
    """Tests for reading event payload from stdin."""

    def test_valid_json_object(self) -> None:
        """Should parse valid JSON object from stdin."""
        with patch("sys.stdin", io.StringIO('{"event": "test", "data": 42}')):
            result = read_stdin_payload()
        assert result == {"event": "test", "data": 42}

    def test_empty_stdin(self) -> None:
        """Empty stdin should return empty dict."""
        with patch("sys.stdin", io.StringIO("")):
            result = read_stdin_payload()
        assert result == {}

    def test_whitespace_only_stdin(self) -> None:
        """Whitespace-only stdin should return empty dict."""
        with patch("sys.stdin", io.StringIO("   \n  ")):
            result = read_stdin_payload()
        assert result == {}

    def test_malformed_json(self) -> None:
        """Malformed JSON should return empty dict."""
        with patch("sys.stdin", io.StringIO("{not valid json")):
            result = read_stdin_payload()
        assert result == {}

    def test_non_dict_json(self) -> None:
        """JSON that is not an object should return empty dict."""
        with patch("sys.stdin", io.StringIO("[1, 2, 3]")):
            result = read_stdin_payload()
        assert result == {}

    def test_json_string_returns_empty(self) -> None:
        """JSON string (not object) should return empty dict."""
        with patch("sys.stdin", io.StringIO('"just a string"')):
            result = read_stdin_payload()
        assert result == {}


# ---------------------------------------------------------------------------
# _execute_inline_handler
# ---------------------------------------------------------------------------


class TestExecuteInlineHandler:
    """Tests for inline handler execution."""

    def _make_inline_handler(
        self, action: dict[str, Any], phase: str = "context"
    ) -> ResolvedHandler:
        return ResolvedHandler(
            name="test_inline",
            handler_type="inline",
            events={"PreToolUse"},
            action=action,
            phase=phase,
        )

    def test_context_action(self) -> None:
        """action.type=context should return additionalContext."""
        handler = self._make_inline_handler(
            {"type": "context", "message": "Be careful with this file"}
        )
        result = _execute_inline_handler(handler, "PreToolUse", {})
        assert result.additional_context == "Be careful with this file"
        assert result.decision is None

    def test_block_action(self) -> None:
        """action.type=block should return block decision."""
        handler = self._make_inline_handler(
            {"type": "block", "reason": "Not allowed"}, phase="guard"
        )
        result = _execute_inline_handler(handler, "PreToolUse", {})
        assert result.decision == "block"
        assert result.reason == "Not allowed"

    def test_warn_action(self) -> None:
        """action.type=warn should return warn decision."""
        handler = self._make_inline_handler(
            {"type": "warn", "reason": "Think twice"}
        )
        result = _execute_inline_handler(handler, "PreToolUse", {})
        assert result.decision == "warn"
        assert result.reason == "Think twice"

    def test_confirm_action(self) -> None:
        """action.type=confirm should return confirm decision."""
        handler = self._make_inline_handler(
            {"type": "confirm", "reason": "Are you sure?"}
        )
        result = _execute_inline_handler(handler, "PreToolUse", {})
        assert result.decision == "confirm"

    def test_no_action_returns_empty(self) -> None:
        """Handler with no action should return context with None message."""
        handler = ResolvedHandler(
            name="test",
            handler_type="inline",
            events={"PreToolUse"},
            action=None,
        )
        result = _execute_inline_handler(handler, "PreToolUse", {})
        assert result.additional_context is None

    def test_block_action_default_reason(self) -> None:
        """Block action without reason should have default reason."""
        handler = self._make_inline_handler({"type": "block"})
        result = _execute_inline_handler(handler, "PreToolUse", {})
        assert result.reason == "Blocked by inline handler"


# ---------------------------------------------------------------------------
# _execute_script_handler
# ---------------------------------------------------------------------------


class TestExecuteScriptHandler:
    """Tests for script handler execution."""

    def _make_script_handler(
        self, command: str, timeout: int = 10
    ) -> ResolvedHandler:
        return ResolvedHandler(
            name="test_script",
            handler_type="script",
            events={"PreToolUse"},
            command=command,
            timeout=timeout,
        )

    def test_script_success_with_json_output(self) -> None:
        """Script that outputs valid JSON should be parsed correctly."""
        handler = self._make_script_handler(
            'echo \'{"additionalContext": "from script"}\''
        )
        result = _execute_script_handler(handler, "PreToolUse", {"tool": "Bash"})
        assert result.additional_context == "from script"

    def test_script_success_with_block(self) -> None:
        """Script that outputs block JSON and exits 2 should produce block result."""
        handler = self._make_script_handler(
            'echo \'{"decision": "block", "reason": "dangerous"}\' && exit 2'
        )
        result = _execute_script_handler(handler, "PreToolUse", {})
        assert result.decision == "block"
        assert result.reason == "dangerous"

    def test_script_exit_2_without_block_json(self) -> None:
        """Script exiting 2 without block JSON should clear the decision."""
        handler = self._make_script_handler(
            'echo "not json" && exit 2'
        )
        result = _execute_script_handler(handler, "PreToolUse", {})
        assert result.decision is None

    def test_script_nonzero_exit_clears_decision(self) -> None:
        """Script with non-zero, non-2 exit should clear decision."""
        handler = self._make_script_handler(
            'echo \'{"decision": "block"}\' && exit 1'
        )
        result = _execute_script_handler(handler, "PreToolUse", {})
        assert result.decision is None

    def test_script_non_json_output(self) -> None:
        """Script with non-JSON output should capture raw output."""
        handler = self._make_script_handler('echo "plain text"')
        result = _execute_script_handler(handler, "PreToolUse", {})
        assert result.output == {"raw": "plain text"}

    def test_script_no_output(self) -> None:
        """Script with no output should return empty result."""
        handler = self._make_script_handler("true")
        result = _execute_script_handler(handler, "PreToolUse", {})
        assert result.decision is None
        assert result.additional_context is None

    def test_script_timeout(self) -> None:
        """Script exceeding timeout should raise _HandlerTimeoutError."""
        from hookwise.dispatcher import _HandlerTimeoutError

        handler = self._make_script_handler("sleep 10", timeout=1)
        with pytest.raises(_HandlerTimeoutError):
            _execute_script_handler(handler, "PreToolUse", {})

    def test_script_no_command_raises(self) -> None:
        """Script handler without command should raise RuntimeError."""
        handler = ResolvedHandler(
            name="no_cmd",
            handler_type="script",
            events={"PreToolUse"},
            command=None,
        )
        with pytest.raises(RuntimeError, match="has no command"):
            _execute_script_handler(handler, "PreToolUse", {})

    def test_script_receives_payload_on_stdin(self) -> None:
        """Script should receive event_type and payload as JSON on stdin."""
        handler = self._make_script_handler("cat")
        result = _execute_script_handler(
            handler, "PreToolUse", {"tool": "Write", "path": "/tmp/x"}
        )
        # cat echoes stdin, so output should contain the input
        assert result.output is not None
        parsed = result.output
        if "raw" in parsed:
            parsed = json.loads(parsed["raw"])
        assert parsed.get("event_type") == "PreToolUse"
        assert parsed.get("payload", {}).get("tool") == "Write"


# ---------------------------------------------------------------------------
# execute_handler (error boundary)
# ---------------------------------------------------------------------------


class TestExecuteHandler:
    """Tests for the execute_handler error boundary."""

    def test_catches_handler_exceptions(self) -> None:
        """Exceptions from handlers should be caught, return empty result."""
        handler = ResolvedHandler(
            name="broken",
            handler_type="builtin",
            events={"PreToolUse"},
            module="nonexistent.module.that.does.not.exist",
        )
        config = HooksConfig()
        result = execute_handler(handler, "PreToolUse", {}, config)
        assert result.decision is None

    def test_unknown_handler_type_returns_empty(self) -> None:
        """Unknown handler type should return empty result."""
        handler = ResolvedHandler(
            name="mystery",
            handler_type="unknown",
            events={"PreToolUse"},
        )
        config = HooksConfig()
        result = execute_handler(handler, "PreToolUse", {}, config)
        assert result.decision is None

    def test_timeout_returns_empty(self) -> None:
        """Timed-out script handler should return empty result."""
        handler = ResolvedHandler(
            name="slow",
            handler_type="script",
            events={"PreToolUse"},
            command="sleep 10",
            timeout=1,
        )
        config = HooksConfig()
        result = execute_handler(handler, "PreToolUse", {}, config)
        assert result.decision is None

    def test_inline_handler_success(self) -> None:
        """Inline handler through execute_handler should work."""
        handler = ResolvedHandler(
            name="inline_test",
            handler_type="inline",
            events={"PreToolUse"},
            action={"type": "context", "message": "test context"},
        )
        config = HooksConfig()
        result = execute_handler(handler, "PreToolUse", {}, config)
        assert result.additional_context == "test context"


# ---------------------------------------------------------------------------
# Dispatcher -- three-phase dispatch
# ---------------------------------------------------------------------------


class TestDispatcher:
    """Tests for the Dispatcher three-phase dispatch engine."""

    @pytest.fixture
    def engine(self) -> ConfigEngine:
        return ConfigEngine()

    @pytest.fixture
    def dispatcher(self, engine: ConfigEngine) -> Dispatcher:
        return Dispatcher(config_engine=engine)

    def test_no_handlers_returns_empty(self, dispatcher: Dispatcher) -> None:
        """No handlers for event should return empty DispatchResult (exit 0)."""
        config = HooksConfig()
        result = dispatcher.dispatch("PreToolUse", {}, config=config)
        assert result.exit_code == 0
        assert result.stdout is None

    def test_guard_block_short_circuits(self, dispatcher: Dispatcher) -> None:
        """First guard block should short-circuit, skip context/side-effects."""
        config = HooksConfig(
            guards=[{
                "name": "blocker",
                "type": "inline",
                "events": ["PreToolUse"],
                "action": {"type": "block", "reason": "denied"},
            }],
            handlers=[{
                "name": "should_not_run",
                "type": "inline",
                "events": ["PreToolUse"],
                "phase": "context",
                "action": {"type": "context", "message": "unreachable"},
            }],
        )
        result = dispatcher.dispatch("PreToolUse", {}, config=config)
        assert result.exit_code == 2
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert parsed["decision"] == "block"
        assert parsed["reason"] == "denied"

    def test_context_injection_merges(self, dispatcher: Dispatcher) -> None:
        """Multiple context handlers should merge their additionalContext."""
        config = HooksConfig(handlers=[
            {
                "name": "ctx1",
                "type": "inline",
                "events": ["PreToolUse"],
                "phase": "context",
                "action": {"type": "context", "message": "first context"},
            },
            {
                "name": "ctx2",
                "type": "inline",
                "events": ["PreToolUse"],
                "phase": "context",
                "action": {"type": "context", "message": "second context"},
            },
        ])
        result = dispatcher.dispatch("PreToolUse", {}, config=config)
        assert result.exit_code == 0
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert "first context" in parsed["additionalContext"]
        assert "second context" in parsed["additionalContext"]

    def test_side_effects_run_after_context(self, dispatcher: Dispatcher) -> None:
        """Side effect handlers should run without affecting stdout."""
        config = HooksConfig(handlers=[
            {
                "name": "ctx",
                "type": "inline",
                "events": ["PreToolUse"],
                "phase": "context",
                "action": {"type": "context", "message": "context output"},
            },
            {
                "name": "se",
                "type": "inline",
                "events": ["PreToolUse"],
                "phase": "side_effect",
                "action": {"type": "context", "message": "side effect (ignored)"},
            },
        ])
        result = dispatcher.dispatch("PreToolUse", {}, config=config)
        assert result.exit_code == 0
        # Side effect's context should NOT appear in stdout
        parsed = json.loads(result.stdout)
        assert "side effect" not in parsed["additionalContext"]
        assert "context output" in parsed["additionalContext"]

    def test_guard_pass_allows_context(self, dispatcher: Dispatcher) -> None:
        """Guard that does not block should allow context phase to run."""
        config = HooksConfig(
            guards=[{
                "name": "allowing_guard",
                "type": "inline",
                "events": ["PreToolUse"],
                "action": {"type": "context", "message": "guard says ok"},
            }],
            handlers=[{
                "name": "ctx",
                "type": "inline",
                "events": ["PreToolUse"],
                "phase": "context",
                "action": {"type": "context", "message": "context runs"},
            }],
        )
        result = dispatcher.dispatch("PreToolUse", {}, config=config)
        assert result.exit_code == 0
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert "context runs" in parsed["additionalContext"]

    def test_multiple_guards_first_block_wins(self, dispatcher: Dispatcher) -> None:
        """With multiple guards, the first block should short-circuit."""
        config = HooksConfig(
            guards=[
                {
                    "name": "pass_guard",
                    "type": "inline",
                    "events": ["PreToolUse"],
                    "action": {"type": "context", "message": "ok"},
                },
                {
                    "name": "block_guard",
                    "type": "inline",
                    "events": ["PreToolUse"],
                    "action": {"type": "block", "reason": "second guard blocks"},
                },
            ],
        )
        result = dispatcher.dispatch("PreToolUse", {}, config=config)
        assert result.exit_code == 2
        parsed = json.loads(result.stdout)
        assert parsed["reason"] == "second guard blocks"

    def test_only_matching_event_handlers_run(self, dispatcher: Dispatcher) -> None:
        """Handlers for other events should not run."""
        config = HooksConfig(handlers=[
            {
                "name": "pre_tool_handler",
                "type": "inline",
                "events": ["PreToolUse"],
                "phase": "context",
                "action": {"type": "context", "message": "pre tool"},
            },
            {
                "name": "post_tool_handler",
                "type": "inline",
                "events": ["PostToolUse"],
                "phase": "context",
                "action": {"type": "context", "message": "post tool"},
            },
        ])
        result = dispatcher.dispatch("PostToolUse", {}, config=config)
        assert result.exit_code == 0
        parsed = json.loads(result.stdout)
        assert "post tool" in parsed["additionalContext"]
        assert "pre tool" not in parsed.get("additionalContext", "")

    def test_dispatch_loads_config_from_disk(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Dispatcher should load config from project_dir when config not provided."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "handlers:\n"
            "  - name: disk_handler\n"
            "    type: inline\n"
            "    events: [PreToolUse]\n"
            "    phase: context\n"
            "    action:\n"
            "      type: context\n"
            "      message: loaded from disk\n",
            encoding="utf-8",
        )
        dispatcher = Dispatcher()
        result = dispatcher.dispatch("PreToolUse", {}, project_dir=tmp_path)
        assert result.exit_code == 0
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert "loaded from disk" in parsed["additionalContext"]

    def test_no_context_returns_no_stdout(self, dispatcher: Dispatcher) -> None:
        """When context handlers produce nothing, stdout should be None."""
        config = HooksConfig(handlers=[
            {
                "name": "side_only",
                "type": "inline",
                "events": ["PreToolUse"],
                "phase": "side_effect",
                "action": {"type": "context", "message": "side effect only"},
            },
        ])
        result = dispatcher.dispatch("PreToolUse", {}, config=config)
        assert result.exit_code == 0
        assert result.stdout is None

    def test_script_handler_in_dispatch(
        self, dispatcher: Dispatcher,
    ) -> None:
        """Script handler should work within full dispatch pipeline."""
        config = HooksConfig(handlers=[
            {
                "name": "echo_script",
                "type": "script",
                "events": ["PreToolUse"],
                "phase": "context",
                "command": 'echo \'{"additionalContext": "from echo"}\'',
            },
        ])
        result = dispatcher.dispatch("PreToolUse", {}, config=config)
        assert result.exit_code == 0
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert "from echo" in parsed["additionalContext"]


# ---------------------------------------------------------------------------
# Graceful handling (Task 3.3)
# ---------------------------------------------------------------------------


class TestGracefulHandling:
    """Tests for graceful handling of missing/malformed configuration."""

    def test_missing_config_exits_silently(self, tmp_path: Path, tmp_state_dir: Path) -> None:
        """When config file is missing, should exit with code 0."""
        dispatcher = Dispatcher()
        result = dispatcher.dispatch("PreToolUse", {}, project_dir=tmp_path)
        assert result.exit_code == 0
        assert result.stdout is None

    def test_malformed_config_exits_silently(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """When config is malformed YAML, should exit with code 0."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text("{{broken yaml: [", encoding="utf-8")
        dispatcher = Dispatcher()
        result = dispatcher.dispatch("PreToolUse", {}, project_dir=tmp_path)
        assert result.exit_code == 0
        assert result.stdout is None

    def test_no_handlers_for_event_exits_silently(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """When no handlers match the event type, should exit with code 0."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "handlers:\n"
            "  - type: inline\n"
            "    events: [SessionStart]\n"
            "    action:\n"
            "      type: context\n"
            "      message: hello\n",
            encoding="utf-8",
        )
        dispatcher = Dispatcher()
        result = dispatcher.dispatch("PreToolUse", {}, project_dir=tmp_path)
        assert result.exit_code == 0
        assert result.stdout is None

    def test_handler_exception_does_not_break_dispatch(
        self,
    ) -> None:
        """Handler that raises should not break the dispatch pipeline."""
        config = HooksConfig(handlers=[
            {
                "name": "broken_builtin",
                "type": "builtin",
                "events": ["PreToolUse"],
                "phase": "context",
                "module": "nonexistent.module.xyz",
            },
            {
                "name": "good_handler",
                "type": "inline",
                "events": ["PreToolUse"],
                "phase": "context",
                "action": {"type": "context", "message": "still works"},
            },
        ])
        dispatcher = Dispatcher()
        result = dispatcher.dispatch("PreToolUse", {}, config=config)
        assert result.exit_code == 0
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert "still works" in parsed["additionalContext"]

    def test_timed_out_handler_does_not_break_dispatch(self) -> None:
        """Timed-out handler should not prevent other handlers from running."""
        config = HooksConfig(handlers=[
            {
                "name": "slow_script",
                "type": "script",
                "events": ["PreToolUse"],
                "phase": "context",
                "command": "sleep 10",
                "timeout": 1,
            },
            {
                "name": "fast_handler",
                "type": "inline",
                "events": ["PreToolUse"],
                "phase": "context",
                "action": {"type": "context", "message": "fast one"},
            },
        ])
        dispatcher = Dispatcher()
        result = dispatcher.dispatch("PreToolUse", {}, config=config)
        assert result.exit_code == 0
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert "fast one" in parsed["additionalContext"]


# ---------------------------------------------------------------------------
# CLI integration (basic smoke test)
# ---------------------------------------------------------------------------


class TestCLIIntegration:
    """Basic smoke tests for the CLI dispatch command."""

    def test_dispatch_no_event_exits_0(self, tmp_state_dir: Path) -> None:
        """dispatch with no event_name should exit 0."""
        from click.testing import CliRunner
        from hookwise.cli import main

        runner = CliRunner()
        result = runner.invoke(main, ["dispatch"])
        # Should exit 0 (no event name -> early return)
        assert result.exit_code == 0

    def test_dispatch_with_event_no_config(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """dispatch with event but no config should exit 0."""
        from click.testing import CliRunner
        from hookwise.cli import main

        runner = CliRunner()
        # Use empty stdin and no config in cwd
        with runner.isolated_filesystem(temp_dir=tmp_path):
            result = runner.invoke(main, ["dispatch", "PreToolUse"], input="{}")
        assert result.exit_code == 0

    def test_dispatch_with_inline_handler(
        self, tmp_path: Path, tmp_state_dir: Path, monkeypatch: pytest.MonkeyPatch,
    ) -> None:
        """dispatch with a working inline handler should produce output."""
        from click.testing import CliRunner
        from hookwise.cli import main

        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "handlers:\n"
            "  - name: test\n"
            "    type: inline\n"
            "    events: [PreToolUse]\n"
            "    phase: context\n"
            "    action:\n"
            "      type: context\n"
            "      message: cli test works\n",
            encoding="utf-8",
        )
        monkeypatch.chdir(tmp_path)
        runner = CliRunner()
        result = runner.invoke(
            main,
            ["dispatch", "PreToolUse"],
            input='{"tool": "Bash"}',
        )
        assert result.exit_code == 0
        # Output should contain the context message
        assert "cli test works" in result.output

    def test_dispatch_guard_block_exits_nonzero(
        self, tmp_path: Path, tmp_state_dir: Path, monkeypatch: pytest.MonkeyPatch,
    ) -> None:
        """dispatch with a blocking guard should exit with non-zero code."""
        from click.testing import CliRunner
        from hookwise.cli import main

        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "guards:\n"
            "  - name: blocker\n"
            "    type: inline\n"
            "    events: [PreToolUse]\n"
            "    action:\n"
            "      type: block\n"
            "      reason: cli block test\n",
            encoding="utf-8",
        )
        monkeypatch.chdir(tmp_path)
        runner = CliRunner()
        result = runner.invoke(
            main,
            ["dispatch", "PreToolUse"],
            input="{}",
        )
        # Should exit non-zero for guard block
        assert result.exit_code == 2
        parsed = json.loads(result.output.strip())
        assert parsed["decision"] == "block"
        assert parsed["reason"] == "cli block test"
