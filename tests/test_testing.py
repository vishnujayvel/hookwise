"""Tests for hookwise.testing -- HookResult, HookRunner, GuardTester, and CLI test command."""

from __future__ import annotations

import json
import os
import sys
import textwrap
from pathlib import Path

import pytest

from hookwise.testing import GuardTester, HookResult, HookRunner


# ===========================================================================
# HookResult tests
# ===========================================================================


class TestHookResultJsonProperty:
    """Tests for HookResult.json property."""

    def test_valid_json_dict(self) -> None:
        """Should parse valid JSON dict from stdout."""
        r = HookResult(stdout='{"decision": "block", "reason": "test"}')
        assert r.json == {"decision": "block", "reason": "test"}

    def test_empty_stdout_returns_none(self) -> None:
        """Should return None for empty stdout."""
        r = HookResult(stdout="")
        assert r.json is None

    def test_whitespace_only_returns_none(self) -> None:
        """Should return None for whitespace-only stdout."""
        r = HookResult(stdout="   \n  ")
        assert r.json is None

    def test_invalid_json_returns_none(self) -> None:
        """Should return None for non-JSON stdout."""
        r = HookResult(stdout="not json at all")
        assert r.json is None

    def test_json_array_returns_none(self) -> None:
        """Should return None for JSON arrays (not dicts)."""
        r = HookResult(stdout="[1, 2, 3]")
        assert r.json is None

    def test_json_string_returns_none(self) -> None:
        """Should return None for JSON strings (not dicts)."""
        r = HookResult(stdout='"just a string"')
        assert r.json is None

    def test_json_with_whitespace(self) -> None:
        """Should parse JSON with surrounding whitespace."""
        r = HookResult(stdout='  \n  {"key": "value"}  \n  ')
        assert r.json == {"key": "value"}


class TestHookResultAssertAllowed:
    """Tests for HookResult.assert_allowed()."""

    def test_passes_on_clean_exit(self) -> None:
        """Should pass for exit code 0 with no block decision."""
        r = HookResult(exit_code=0)
        r.assert_allowed()

    def test_passes_with_non_block_json(self) -> None:
        """Should pass for exit code 0 with non-block JSON."""
        r = HookResult(stdout='{"additionalContext": "info"}', exit_code=0)
        r.assert_allowed()

    def test_passes_with_warn_decision(self) -> None:
        """Should pass even with a warn decision (warn is not block)."""
        r = HookResult(stdout='{"decision": "warn"}', exit_code=0)
        r.assert_allowed()

    def test_fails_on_nonzero_exit(self) -> None:
        """Should raise AssertionError for non-zero exit code."""
        r = HookResult(exit_code=2)
        with pytest.raises(AssertionError, match="Expected exit code 0"):
            r.assert_allowed()

    def test_fails_on_block_decision(self) -> None:
        """Should raise AssertionError when stdout has block decision."""
        r = HookResult(stdout='{"decision": "block"}', exit_code=0)
        with pytest.raises(AssertionError, match="block decision"):
            r.assert_allowed()

    def test_error_message_includes_details(self) -> None:
        """Error message should include stdout, stderr, exit_code."""
        r = HookResult(stdout="output", stderr="err", exit_code=1)
        with pytest.raises(AssertionError) as exc_info:
            r.assert_allowed()
        msg = str(exc_info.value)
        assert "output" in msg
        assert "err" in msg


class TestHookResultAssertBlocked:
    """Tests for HookResult.assert_blocked()."""

    def test_passes_on_block_decision(self) -> None:
        """Should pass when stdout has block decision."""
        r = HookResult(stdout='{"decision": "block", "reason": "forbidden"}')
        r.assert_blocked()

    def test_passes_with_reason_match(self) -> None:
        """Should pass when reason contains the expected substring."""
        r = HookResult(stdout='{"decision": "block", "reason": "Force push blocked"}')
        r.assert_blocked(reason_contains="Force push")

    def test_fails_on_no_json(self) -> None:
        """Should fail when stdout is not valid JSON."""
        r = HookResult(stdout="not json")
        with pytest.raises(AssertionError, match="not valid JSON"):
            r.assert_blocked()

    def test_fails_on_allow_decision(self) -> None:
        """Should fail when decision is allow, not block."""
        r = HookResult(stdout='{"decision": "allow"}')
        with pytest.raises(AssertionError, match="Expected decision='block'"):
            r.assert_blocked()

    def test_fails_on_wrong_reason(self) -> None:
        """Should fail when reason doesn't contain expected substring."""
        r = HookResult(stdout='{"decision": "block", "reason": "other reason"}')
        with pytest.raises(AssertionError, match="Expected reason to contain"):
            r.assert_blocked(reason_contains="missing text")

    def test_fails_on_empty_stdout(self) -> None:
        """Should fail when stdout is empty."""
        r = HookResult(stdout="")
        with pytest.raises(AssertionError, match="not valid JSON"):
            r.assert_blocked()

    def test_passes_with_none_reason_when_no_check(self) -> None:
        """Should pass even with no reason field if no reason_contains."""
        r = HookResult(stdout='{"decision": "block"}')
        r.assert_blocked()

    def test_fails_when_reason_is_none_and_check_required(self) -> None:
        """Should fail when reason is None but reason_contains is provided."""
        r = HookResult(stdout='{"decision": "block"}')
        with pytest.raises(AssertionError, match="Expected reason to contain"):
            r.assert_blocked(reason_contains="something")


class TestHookResultAssertWarns:
    """Tests for HookResult.assert_warns()."""

    def test_passes_on_warn_decision(self) -> None:
        """Should pass with warn decision in JSON."""
        r = HookResult(stdout='{"decision": "warn", "reason": "caution"}')
        r.assert_warns()

    def test_passes_on_stderr_content(self) -> None:
        """Should pass with content in stderr (common warning pattern)."""
        r = HookResult(stderr="WARNING: something happened")
        r.assert_warns()

    def test_passes_with_message_match_in_reason(self) -> None:
        """Should pass when reason contains expected text."""
        r = HookResult(stdout='{"decision": "warn", "reason": "Gmail detected"}')
        r.assert_warns(message_contains="Gmail")

    def test_passes_with_message_match_in_stderr(self) -> None:
        """Should pass when stderr contains expected text."""
        r = HookResult(stderr="WARNING: force push detected")
        r.assert_warns(message_contains="force push")

    def test_passes_with_message_match_in_stdout(self) -> None:
        """Should pass when stdout text contains expected text."""
        r = HookResult(stdout="Warning: something risky", stderr="some output")
        r.assert_warns(message_contains="risky")

    def test_fails_on_no_warning_signal(self) -> None:
        """Should fail when no warn decision and no stderr."""
        r = HookResult(stdout='{"decision": "allow"}')
        with pytest.raises(AssertionError, match="Expected warning"):
            r.assert_warns()

    def test_fails_on_message_not_found(self) -> None:
        """Should fail when message_contains is not found anywhere."""
        r = HookResult(
            stdout='{"decision": "warn", "reason": "unrelated"}',
            stderr="also unrelated",
        )
        with pytest.raises(AssertionError, match="Expected warning message"):
            r.assert_warns(message_contains="specific text")


class TestHookResultAssertSilent:
    """Tests for HookResult.assert_silent()."""

    def test_passes_on_silent_result(self) -> None:
        """Should pass with no output and exit code 0."""
        r = HookResult()
        r.assert_silent()

    def test_passes_with_whitespace_only(self) -> None:
        """Should pass with whitespace-only stdout/stderr."""
        r = HookResult(stdout="  \n  ", stderr="  ")
        r.assert_silent()

    def test_fails_on_stdout(self) -> None:
        """Should fail when stdout has content."""
        r = HookResult(stdout="some output")
        with pytest.raises(AssertionError, match="no stdout"):
            r.assert_silent()

    def test_fails_on_stderr(self) -> None:
        """Should fail when stderr has content."""
        r = HookResult(stderr="some error")
        with pytest.raises(AssertionError, match="no stderr"):
            r.assert_silent()

    def test_fails_on_nonzero_exit(self) -> None:
        """Should fail with non-zero exit code."""
        r = HookResult(exit_code=1)
        with pytest.raises(AssertionError, match="exit code 0"):
            r.assert_silent()


class TestHookResultAssertAsks:
    """Tests for HookResult.assert_asks()."""

    def test_passes_on_confirm_decision(self) -> None:
        """Should pass with confirm decision in JSON."""
        r = HookResult(stdout='{"decision": "confirm", "reason": "Are you sure?"}')
        r.assert_asks()

    def test_fails_on_no_json(self) -> None:
        """Should fail when stdout is not valid JSON."""
        r = HookResult(stdout="not json")
        with pytest.raises(AssertionError, match="not valid JSON"):
            r.assert_asks()

    def test_fails_on_block_decision(self) -> None:
        """Should fail when decision is block, not confirm."""
        r = HookResult(stdout='{"decision": "block"}')
        with pytest.raises(AssertionError, match="Expected decision='confirm'"):
            r.assert_asks()

    def test_fails_on_allow_decision(self) -> None:
        """Should fail when decision is allow (no confirm)."""
        r = HookResult(stdout='{"decision": "allow"}')
        with pytest.raises(AssertionError, match="Expected decision='confirm'"):
            r.assert_asks()


class TestHookResultRepr:
    """Tests for HookResult.__repr__()."""

    def test_repr_contains_all_fields(self) -> None:
        """Repr should include exit_code, stdout, and stderr."""
        r = HookResult(stdout="out", stderr="err", exit_code=1)
        repr_str = repr(r)
        assert "exit_code=1" in repr_str
        assert "out" in repr_str
        assert "err" in repr_str


# ===========================================================================
# HookRunner tests
# ===========================================================================


class TestHookRunner:
    """Tests for HookRunner subprocess execution."""

    def test_run_captures_stdout(self) -> None:
        """Should capture stdout from a simple echo command."""
        runner = HookRunner(f"{sys.executable} -c \"import sys; print('hello')\"")
        result = runner.run("PreToolUse", {})
        assert "hello" in result.stdout

    def test_run_captures_stderr(self) -> None:
        """Should capture stderr from a command."""
        runner = HookRunner(
            f"{sys.executable} -c \"import sys; print('warning', file=sys.stderr)\""
        )
        result = runner.run("PreToolUse", {})
        assert "warning" in result.stderr

    def test_run_captures_exit_code(self) -> None:
        """Should capture non-zero exit code."""
        runner = HookRunner(f"{sys.executable} -c \"import sys; sys.exit(2)\"")
        result = runner.run("PreToolUse", {})
        assert result.exit_code == 2

    def test_run_pipes_json_to_stdin(self) -> None:
        """Should pipe JSON payload to stdin."""
        script = textwrap.dedent("""\
            import sys, json
            data = json.load(sys.stdin)
            print(json.dumps({"received_type": data["event_type"]}))
        """)
        runner = HookRunner(f"{sys.executable} -c '{script}'")
        result = runner.run("PreToolUse", {"tool_name": "Bash"})
        assert result.json is not None
        assert result.json["received_type"] == "PreToolUse"

    def test_run_pipes_payload_to_stdin(self) -> None:
        """Should include the payload in the piped JSON."""
        script = textwrap.dedent("""\
            import sys, json
            data = json.load(sys.stdin)
            tool_name = data["payload"]["tool_name"]
            print(json.dumps({"tool": tool_name}))
        """)
        runner = HookRunner(f"{sys.executable} -c '{script}'")
        result = runner.run("PreToolUse", {"tool_name": "Read"})
        assert result.json is not None
        assert result.json["tool"] == "Read"

    def test_run_default_payload_is_empty_dict(self) -> None:
        """Should default payload to empty dict when not provided."""
        script = textwrap.dedent("""\
            import sys, json
            data = json.load(sys.stdin)
            print(json.dumps({"payload_keys": list(data["payload"].keys())}))
        """)
        runner = HookRunner(f"{sys.executable} -c '{script}'")
        result = runner.run("PreToolUse")
        assert result.json is not None
        assert result.json["payload_keys"] == []

    def test_run_timeout_returns_error_result(self) -> None:
        """Should return a result with error info on timeout."""
        runner = HookRunner(
            f"{sys.executable} -c \"import time; time.sleep(10)\"",
            timeout=1,
        )
        result = runner.run("PreToolUse", {})
        assert result.exit_code == -1
        assert "TIMEOUT" in result.stderr

    def test_run_timeout_override(self) -> None:
        """Should allow per-run timeout override."""
        runner = HookRunner(
            f"{sys.executable} -c \"import time; time.sleep(10)\"",
            timeout=30,
        )
        result = runner.run("PreToolUse", {}, timeout=1)
        assert result.exit_code == -1
        assert "TIMEOUT" in result.stderr

    def test_execute_static_method(self) -> None:
        """Static execute method should work as a one-shot runner."""
        result = HookRunner.execute(
            f"{sys.executable} -c \"print('static')\"",
            event_type="PreToolUse",
            payload={},
        )
        assert "static" in result.stdout

    def test_run_result_is_hook_result(self) -> None:
        """Should return a HookResult instance."""
        runner = HookRunner(f"{sys.executable} -c \"pass\"")
        result = runner.run("PreToolUse", {})
        assert isinstance(result, HookResult)

    def test_run_block_pattern(self) -> None:
        """Should correctly detect a block response pattern."""
        script = textwrap.dedent("""\
            import json, sys
            print(json.dumps({"decision": "block", "reason": "Blocked by guard"}))
            sys.exit(2)
        """)
        runner = HookRunner(f"{sys.executable} -c '{script}'")
        result = runner.run("PreToolUse", {"tool_name": "Bash"})
        result.assert_blocked(reason_contains="Blocked by guard")

    def test_run_allow_pattern(self) -> None:
        """Should correctly detect an allow (silent exit 0) pattern."""
        runner = HookRunner(f"{sys.executable} -c \"pass\"")
        result = runner.run("PreToolUse", {"tool_name": "Read"})
        result.assert_silent()

    def test_run_confirm_pattern(self) -> None:
        """Should correctly detect a confirm response pattern."""
        script = textwrap.dedent("""\
            import json
            print(json.dumps({"decision": "confirm", "reason": "Are you sure?"}))
        """)
        runner = HookRunner(f"{sys.executable} -c '{script}'")
        result = runner.run("PreToolUse", {})
        result.assert_asks()


# ===========================================================================
# GuardTester tests
# ===========================================================================


class TestGuardTesterInit:
    """Tests for GuardTester initialization."""

    def test_init_with_guards_list(self) -> None:
        """Should accept a list of guard dicts."""
        tester = GuardTester(guards=[
            {"match": "Bash", "action": "block", "reason": "No bash"},
        ])
        assert len(tester.rules) == 1

    def test_init_with_config_dict(self) -> None:
        """Should accept a config dict with guards key."""
        tester = GuardTester(config_dict={
            "guards": [
                {"match": "Bash", "action": "block", "reason": "No bash"},
            ]
        })
        assert len(tester.rules) == 1

    def test_init_with_config_file(self, tmp_path: Path) -> None:
        """Should load guards from a YAML config file."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "guards:\n"
            "  - match: Bash\n"
            "    action: block\n"
            "    reason: No bash\n"
        )
        tester = GuardTester(config_path=config_file)
        assert len(tester.rules) == 1

    def test_init_raises_on_no_source(self) -> None:
        """Should raise ValueError when no guard source is provided."""
        with pytest.raises(ValueError, match="requires at least one"):
            GuardTester()

    def test_init_missing_config_file(self) -> None:
        """Should raise FileNotFoundError for missing config file."""
        with pytest.raises(FileNotFoundError):
            GuardTester(config_path="/nonexistent/hookwise.yaml")

    def test_init_config_file_no_guards_key(self, tmp_path: Path) -> None:
        """Should raise ValueError when config file has no guards key."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text("version: 1\n")
        with pytest.raises(ValueError, match="no 'guards' key"):
            GuardTester(config_path=config_file)

    def test_init_config_file_guards_not_list(self, tmp_path: Path) -> None:
        """Should raise ValueError when guards is not a list."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text("guards: not_a_list\n")
        with pytest.raises(ValueError, match="not a list"):
            GuardTester(config_path=config_file)

    def test_init_config_file_not_mapping(self, tmp_path: Path) -> None:
        """Should raise ValueError when YAML is not a mapping."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text("- item1\n- item2\n")
        with pytest.raises(ValueError, match="not contain a YAML mapping"):
            GuardTester(config_path=config_file)

    def test_init_empty_guards_list(self) -> None:
        """Should accept an empty guards list."""
        tester = GuardTester(guards=[])
        assert len(tester.rules) == 0

    def test_init_config_dict_no_guards_key(self) -> None:
        """Should produce empty rules when config_dict has no guards key."""
        tester = GuardTester(config_dict={"version": 1})
        assert len(tester.rules) == 0

    def test_guards_priority_over_config_dict(self) -> None:
        """guards param should take priority over config_dict."""
        tester = GuardTester(
            guards=[{"match": "Bash", "action": "block", "reason": "from guards"}],
            config_dict={
                "guards": [{"match": "Read", "action": "warn", "reason": "from config"}]
            },
        )
        assert len(tester.rules) == 1
        assert tester.rules[0].match == "Bash"


class TestGuardTesterTestToolCall:
    """Tests for GuardTester.test_tool_call()."""

    def test_blocked_tool(self) -> None:
        """Should return block for a matching blocked tool."""
        tester = GuardTester(guards=[
            {"match": "Bash", "action": "block", "reason": "No bash"},
        ])
        result = tester.test_tool_call("Bash")
        assert result.action == "block"
        assert result.reason == "No bash"

    def test_allowed_tool(self) -> None:
        """Should return allow for a tool that doesn't match any rule."""
        tester = GuardTester(guards=[
            {"match": "Bash", "action": "block", "reason": "No bash"},
        ])
        result = tester.test_tool_call("Read")
        assert result.action == "allow"

    def test_warn_tool(self) -> None:
        """Should return warn for a matching warn rule."""
        tester = GuardTester(guards=[
            {"match": "mcp__gmail__*", "action": "warn", "reason": "Gmail detected"},
        ])
        result = tester.test_tool_call("mcp__gmail__send_email")
        assert result.action == "warn"

    def test_confirm_tool(self) -> None:
        """Should return confirm for a matching confirm rule."""
        tester = GuardTester(guards=[
            {"match": "mcp__gmail__send_email", "action": "confirm", "reason": "Sending email"},
        ])
        result = tester.test_tool_call("mcp__gmail__send_email")
        assert result.action == "confirm"

    def test_with_tool_input(self) -> None:
        """Should evaluate rules with tool_input conditions."""
        tester = GuardTester(guards=[
            {
                "match": "Bash",
                "action": "block",
                "reason": "Force push blocked",
                "when": 'tool_input.command contains "force push"',
            },
        ])
        # Should block when condition is met
        result = tester.test_tool_call("Bash", {"command": "git push --force push"})
        assert result.action == "block"

        # Should allow when condition is not met
        result = tester.test_tool_call("Bash", {"command": "git push"})
        assert result.action == "allow"

    def test_default_tool_input_is_empty(self) -> None:
        """Should default tool_input to empty dict."""
        tester = GuardTester(guards=[
            {"match": "Bash", "action": "block", "reason": "No bash"},
        ])
        # Should work without explicit tool_input
        result = tester.test_tool_call("Bash")
        assert result.action == "block"

    def test_glob_pattern_matching(self) -> None:
        """Should support glob patterns in match field."""
        tester = GuardTester(guards=[
            {"match": "mcp__slack__*", "action": "warn", "reason": "Slack tool"},
        ])
        result = tester.test_tool_call("mcp__slack__send_message")
        assert result.action == "warn"

        result = tester.test_tool_call("mcp__gmail__send_email")
        assert result.action == "allow"

    def test_first_match_wins(self) -> None:
        """Should use first-match-wins semantics."""
        tester = GuardTester(guards=[
            {"match": "Bash", "action": "block", "reason": "blocked"},
            {"match": "Bash", "action": "warn", "reason": "warned"},
        ])
        result = tester.test_tool_call("Bash")
        assert result.action == "block"

    def test_unless_condition(self) -> None:
        """Should skip rule when unless condition is met."""
        tester = GuardTester(guards=[
            {
                "match": "mcp__gmail__*",
                "action": "warn",
                "reason": "Gmail tool",
                "unless": 'tool_input.to starts_with "test@"',
            },
        ])
        # Should warn for non-test recipients
        result = tester.test_tool_call("mcp__gmail__send_email", {"to": "user@example.com"})
        assert result.action == "warn"

        # Should allow for test recipients
        result = tester.test_tool_call("mcp__gmail__send_email", {"to": "test@example.com"})
        assert result.action == "allow"


class TestGuardTesterAssertions:
    """Tests for GuardTester assertion methods."""

    @pytest.fixture
    def tester(self) -> GuardTester:
        """Provide a GuardTester with a variety of rules."""
        return GuardTester(guards=[
            {"match": "Bash", "action": "block", "reason": "No bash allowed"},
            {"match": "mcp__gmail__*", "action": "warn", "reason": "Gmail detected"},
            {"match": "mcp__gmail__send_email", "action": "confirm", "reason": "Sending email"},
        ])

    def test_assert_blocked_passes(self, tester: GuardTester) -> None:
        """Should pass for a blocked tool."""
        tester.assert_blocked("Bash")

    def test_assert_blocked_with_reason(self, tester: GuardTester) -> None:
        """Should pass with matching reason substring."""
        tester.assert_blocked("Bash", reason_contains="No bash")

    def test_assert_blocked_fails_on_allow(self, tester: GuardTester) -> None:
        """Should fail for an allowed tool."""
        with pytest.raises(AssertionError, match="Expected tool.*to be blocked"):
            tester.assert_blocked("Read")

    def test_assert_blocked_fails_on_wrong_reason(self, tester: GuardTester) -> None:
        """Should fail when reason doesn't match."""
        with pytest.raises(AssertionError, match="Expected block reason"):
            tester.assert_blocked("Bash", reason_contains="wrong reason")

    def test_assert_allowed_passes(self, tester: GuardTester) -> None:
        """Should pass for an allowed tool."""
        tester.assert_allowed("Read")

    def test_assert_allowed_fails_on_block(self, tester: GuardTester) -> None:
        """Should fail for a blocked tool."""
        with pytest.raises(AssertionError, match="Expected tool.*to be allowed"):
            tester.assert_allowed("Bash")

    def test_assert_warns_passes(self) -> None:
        """Should pass for a tool with warn rule."""
        tester = GuardTester(guards=[
            {"match": "mcp__gmail__*", "action": "warn", "reason": "Gmail detected"},
        ])
        tester.assert_warns("mcp__gmail__read_email")

    def test_assert_warns_fails_on_allow(self) -> None:
        """Should fail for an allowed tool."""
        tester = GuardTester(guards=[
            {"match": "Bash", "action": "block", "reason": "blocked"},
        ])
        with pytest.raises(AssertionError, match="Expected tool.*to trigger a warning"):
            tester.assert_warns("Read")

    def test_assert_blocked_with_tool_input(self) -> None:
        """Should support tool_input in assertion methods."""
        tester = GuardTester(guards=[
            {
                "match": "Bash",
                "action": "block",
                "reason": "Force push blocked",
                "when": 'tool_input.command contains "force push"',
            },
        ])
        tester.assert_blocked("Bash", tool_input={"command": "git force push"})

    def test_assert_allowed_with_tool_input(self) -> None:
        """Should support tool_input in assert_allowed."""
        tester = GuardTester(guards=[
            {
                "match": "Bash",
                "action": "block",
                "reason": "Force push blocked",
                "when": 'tool_input.command contains "force push"',
            },
        ])
        tester.assert_allowed("Bash", tool_input={"command": "git push"})


class TestGuardTesterRunScenarios:
    """Tests for GuardTester.run_scenarios()."""

    def test_basic_scenarios(self) -> None:
        """Should run scenarios and return results."""
        tester = GuardTester(guards=[
            {"match": "Bash", "action": "block", "reason": "No bash"},
            {"match": "mcp__gmail__*", "action": "warn", "reason": "Gmail"},
        ])
        scenarios = [
            {"tool_name": "Bash", "expected": "block"},
            {"tool_name": "Read", "expected": "allow"},
            {"tool_name": "mcp__gmail__send_email", "expected": "warn"},
        ]
        results = tester.run_scenarios(scenarios)
        assert len(results) == 3
        assert all(passed for _, _, passed in results)

    def test_scenario_with_failure(self) -> None:
        """Should return passed=False for incorrect expectations."""
        tester = GuardTester(guards=[
            {"match": "Bash", "action": "block", "reason": "No bash"},
        ])
        scenarios = [
            {"tool_name": "Bash", "expected": "allow"},  # Wrong expectation
        ]
        results = tester.run_scenarios(scenarios)
        assert len(results) == 1
        scenario, guard_result, passed = results[0]
        assert not passed
        assert guard_result.action == "block"

    def test_scenario_with_tool_input(self) -> None:
        """Should pass tool_input from scenario to evaluation."""
        tester = GuardTester(guards=[
            {
                "match": "Bash",
                "action": "block",
                "reason": "blocked",
                "when": 'tool_input.command contains "rm"',
            },
        ])
        scenarios = [
            {"tool_name": "Bash", "tool_input": {"command": "rm -rf /"}, "expected": "block"},
            {"tool_name": "Bash", "tool_input": {"command": "ls"}, "expected": "allow"},
        ]
        results = tester.run_scenarios(scenarios)
        assert all(passed for _, _, passed in results)

    def test_empty_scenarios(self) -> None:
        """Should return empty list for empty scenarios."""
        tester = GuardTester(guards=[])
        results = tester.run_scenarios([])
        assert results == []

    def test_scenario_result_tuple_structure(self) -> None:
        """Each result should be a tuple of (scenario, GuardResult, bool)."""
        tester = GuardTester(guards=[
            {"match": "Bash", "action": "block", "reason": "No bash"},
        ])
        scenarios = [{"tool_name": "Bash", "expected": "block"}]
        results = tester.run_scenarios(scenarios)
        scenario, guard_result, passed = results[0]
        assert scenario == scenarios[0]
        assert guard_result.action == "block"
        assert passed is True

    def test_confirm_scenario(self) -> None:
        """Should handle confirm expected action."""
        tester = GuardTester(guards=[
            {"match": "mcp__gmail__send_email", "action": "confirm", "reason": "Confirm send"},
        ])
        scenarios = [
            {"tool_name": "mcp__gmail__send_email", "expected": "confirm"},
        ]
        results = tester.run_scenarios(scenarios)
        assert all(passed for _, _, passed in results)


# ===========================================================================
# CLI test command tests
# ===========================================================================


class TestCLITestCommand:
    """Tests for the `hookwise test` CLI command."""

    def test_test_command_exists(self) -> None:
        """The test command should be importable and registered."""
        from hookwise.cli import main
        # Click groups expose commands via .commands dict
        assert "test" in main.commands

    def test_test_command_no_test_files(self, tmp_path: Path) -> None:
        """Should report no test files and exit 0 for empty directory."""
        from click.testing import CliRunner

        from hookwise.cli import main

        runner = CliRunner()
        result = runner.invoke(main, ["test", str(tmp_path)])
        assert result.exit_code == 0
        assert "No test files found" in result.output

    def test_test_command_discovers_test_files(self, tmp_path: Path) -> None:
        """Should discover test_*.py and *_test.py files."""
        # Create test files
        (tmp_path / "test_one.py").write_text("def test_pass(): pass\n")
        (tmp_path / "two_test.py").write_text("def test_pass(): pass\n")
        (tmp_path / "helper.py").write_text("x = 1\n")

        from click.testing import CliRunner

        from hookwise.cli import main

        runner = CliRunner()
        result = runner.invoke(main, ["test", str(tmp_path)])
        assert "Discovered 2 test file(s)" in result.output

    def test_test_command_runs_pytest(self, tmp_path: Path) -> None:
        """Should run discovered tests via pytest and report results."""
        (tmp_path / "test_sample.py").write_text(
            "def test_pass():\n    assert True\n\n"
            "def test_also_pass():\n    assert 1 + 1 == 2\n"
        )

        from click.testing import CliRunner

        from hookwise.cli import main

        runner = CliRunner()
        result = runner.invoke(main, ["test", str(tmp_path)])
        assert "2 passed" in result.output

    def test_test_command_reports_failures(self, tmp_path: Path) -> None:
        """Should report test failures and exit non-zero."""
        (tmp_path / "test_fail.py").write_text(
            "def test_fail():\n    assert False\n"
        )

        from click.testing import CliRunner

        from hookwise.cli import main

        runner = CliRunner()
        result = runner.invoke(main, ["test", str(tmp_path)])
        assert result.exit_code != 0
        assert "1 failed" in result.output

    def test_test_command_invalid_directory(self) -> None:
        """Should error on non-existent directory."""
        from click.testing import CliRunner

        from hookwise.cli import main

        runner = CliRunner()
        result = runner.invoke(main, ["test", "/nonexistent/dir"])
        assert result.exit_code != 0
        assert "not a directory" in result.output

    def test_test_command_default_directory(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
        """Should default to current directory."""
        (tmp_path / "test_here.py").write_text("def test_ok(): pass\n")
        monkeypatch.chdir(tmp_path)

        from click.testing import CliRunner

        from hookwise.cli import main

        runner = CliRunner()
        result = runner.invoke(main, ["test"])
        assert "Discovered 1 test file(s)" in result.output

    def test_test_command_verbose_flag(self, tmp_path: Path) -> None:
        """Should pass -v to pytest when verbose flag is set."""
        (tmp_path / "test_v.py").write_text("def test_ok(): pass\n")

        from click.testing import CliRunner

        from hookwise.cli import main

        runner = CliRunner()
        result = runner.invoke(main, ["test", str(tmp_path), "-v"])
        # Verbose output includes PASSED/FAILED per test
        assert "PASSED" in result.output or "passed" in result.output


# ===========================================================================
# Integration tests -- import smoke tests
# ===========================================================================


class TestTestingModuleImports:
    """Verify that the testing subpackage is correctly wired."""

    def test_import_from_package(self) -> None:
        """All public classes should be importable from hookwise.testing."""
        from hookwise.testing import GuardTester, HookResult, HookRunner

        assert callable(HookResult)
        assert callable(HookRunner)
        assert callable(GuardTester)

    def test_import_from_submodules(self) -> None:
        """Should be importable from individual submodules."""
        from hookwise.testing.guard_tester import GuardTester
        from hookwise.testing.result import HookResult
        from hookwise.testing.runner import HookRunner

        assert callable(HookResult)
        assert callable(HookRunner)
        assert callable(GuardTester)

    def test_all_exports(self) -> None:
        """__all__ should contain all public classes."""
        import hookwise.testing

        assert "HookResult" in hookwise.testing.__all__
        assert "HookRunner" in hookwise.testing.__all__
        assert "GuardTester" in hookwise.testing.__all__
