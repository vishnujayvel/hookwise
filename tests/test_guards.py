"""Tests for hookwise.guards -- guard rails engine and condition parser."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest

from hookwise.guards import (
    GuardEngine,
    GuardResult,
    GuardRule,
    _evaluate_condition,
    _parse_condition,
    _resolve_field_path,
    parse_guard_rules,
    handle,
)
from hookwise.config import ConfigEngine, HooksConfig
from hookwise.dispatcher import Dispatcher, DispatchResult


# ---------------------------------------------------------------------------
# GuardRule dataclass
# ---------------------------------------------------------------------------


class TestGuardRule:
    """Tests for the GuardRule dataclass."""

    def test_default_values(self) -> None:
        """Required fields should be set; optional fields default to None."""
        rule = GuardRule(match="Bash", action="block", reason="No bash")
        assert rule.match == "Bash"
        assert rule.action == "block"
        assert rule.reason == "No bash"
        assert rule.when is None
        assert rule.unless is None

    def test_with_conditions(self) -> None:
        """Should accept when and unless conditions."""
        rule = GuardRule(
            match="Bash",
            action="block",
            reason="No force push",
            when='tool_input.command contains "force push"',
            unless='tool_input.command starts_with "echo"',
        )
        assert rule.when == 'tool_input.command contains "force push"'
        assert rule.unless == 'tool_input.command starts_with "echo"'


# ---------------------------------------------------------------------------
# GuardResult dataclass
# ---------------------------------------------------------------------------


class TestGuardResult:
    """Tests for the GuardResult dataclass."""

    def test_allow_result(self) -> None:
        """Allow result should have no reason or matched rule."""
        result = GuardResult(action="allow")
        assert result.action == "allow"
        assert result.reason is None
        assert result.matched_rule is None

    def test_block_result(self) -> None:
        """Block result should carry reason and matched rule."""
        rule = GuardRule(match="Bash", action="block", reason="denied")
        result = GuardResult(action="block", reason="denied", matched_rule=rule)
        assert result.action == "block"
        assert result.reason == "denied"
        assert result.matched_rule is rule


# ---------------------------------------------------------------------------
# Condition expression parser: _parse_condition
# ---------------------------------------------------------------------------


class TestParseCondition:
    """Tests for the condition expression parser."""

    def test_contains_with_quoted_value(self) -> None:
        """Should parse 'contains' with double-quoted value."""
        result = _parse_condition('tool_input.command contains "force push"')
        assert result == ("tool_input.command", "contains", "force push")

    def test_contains_with_single_quoted_value(self) -> None:
        """Should parse 'contains' with single-quoted value."""
        result = _parse_condition("tool_input.command contains 'force push'")
        assert result == ("tool_input.command", "contains", "force push")

    def test_contains_with_unquoted_value(self) -> None:
        """Should parse 'contains' with unquoted single-word value."""
        result = _parse_condition("tool_input.command contains rm")
        assert result == ("tool_input.command", "contains", "rm")

    def test_starts_with_operator(self) -> None:
        """Should parse starts_with operator."""
        result = _parse_condition('tool_input.filepath starts_with "5-notes/"')
        assert result == ("tool_input.filepath", "starts_with", "5-notes/")

    def test_ends_with_operator(self) -> None:
        """Should parse ends_with operator."""
        result = _parse_condition('tool_input.file_path ends_with ".env"')
        assert result == ("tool_input.file_path", "ends_with", ".env")

    def test_matches_operator(self) -> None:
        """Should parse matches (regex) operator."""
        result = _parse_condition(r'tool_input.command matches "^git\s+push"')
        assert result is not None
        assert result[0] == "tool_input.command"
        assert result[1] == "matches"
        assert result[2] == r"^git\s+push"

    def test_equals_operator(self) -> None:
        """Should parse == (equals) operator."""
        result = _parse_condition('tool_input.action == "delete"')
        assert result == ("tool_input.action", "==", "delete")

    def test_malformed_no_operator(self) -> None:
        """Missing operator should return None."""
        result = _parse_condition("tool_input.command")
        assert result is None

    def test_malformed_empty_string(self) -> None:
        """Empty string should return None."""
        result = _parse_condition("")
        assert result is None

    def test_malformed_invalid_operator(self) -> None:
        """Invalid operator should return None."""
        result = _parse_condition('tool_input.command includes "test"')
        assert result is None

    def test_whitespace_around_expression(self) -> None:
        """Surrounding whitespace should be stripped."""
        result = _parse_condition('  tool_input.cmd contains "x"  ')
        assert result == ("tool_input.cmd", "contains", "x")

    def test_nested_field_path(self) -> None:
        """Deeply nested field path should parse."""
        result = _parse_condition('tool_input.config.nested.deep contains "val"')
        assert result is not None
        assert result[0] == "tool_input.config.nested.deep"


# ---------------------------------------------------------------------------
# Field path resolution: _resolve_field_path
# ---------------------------------------------------------------------------


class TestResolveFieldPath:
    """Tests for resolving dot-notation field paths."""

    def test_simple_path(self) -> None:
        """Should resolve a top-level key."""
        data = {"tool_input": {"command": "ls -la"}}
        assert _resolve_field_path(data, "tool_input.command") == "ls -la"

    def test_nested_path(self) -> None:
        """Should resolve nested dicts."""
        data = {"tool_input": {"config": {"setting": "value"}}}
        assert _resolve_field_path(data, "tool_input.config.setting") == "value"

    def test_missing_key_returns_none(self) -> None:
        """Missing key in path should return None."""
        data = {"tool_input": {"command": "ls"}}
        assert _resolve_field_path(data, "tool_input.missing") is None

    def test_empty_dict_returns_none(self) -> None:
        """Empty dict should return None for any path."""
        assert _resolve_field_path({}, "tool_input.command") is None

    def test_non_dict_intermediate_returns_none(self) -> None:
        """Non-dict intermediate value should return None."""
        data = {"tool_input": "not_a_dict"}
        assert _resolve_field_path(data, "tool_input.command") is None

    def test_numeric_value_converted_to_string(self) -> None:
        """Numeric values should be converted to string."""
        data = {"tool_input": {"port": 8080}}
        assert _resolve_field_path(data, "tool_input.port") == "8080"

    def test_none_value_returns_none(self) -> None:
        """None values should return None."""
        data = {"tool_input": {"key": None}}
        assert _resolve_field_path(data, "tool_input.key") is None

    def test_boolean_value_converted_to_string(self) -> None:
        """Boolean values should be converted to string."""
        data = {"tool_input": {"flag": True}}
        assert _resolve_field_path(data, "tool_input.flag") == "True"


# ---------------------------------------------------------------------------
# Condition evaluation: _evaluate_condition
# ---------------------------------------------------------------------------


class TestEvaluateCondition:
    """Tests for evaluating condition expressions against tool_input."""

    def test_contains_true(self) -> None:
        """contains should return True when value is in field."""
        result = _evaluate_condition(
            'tool_input.command contains "force push"',
            {"command": "git push --force push to main"},
        )
        assert result is True

    def test_contains_false(self) -> None:
        """contains should return False when value is not in field."""
        result = _evaluate_condition(
            'tool_input.command contains "force push"',
            {"command": "git push origin main"},
        )
        assert result is False

    def test_starts_with_true(self) -> None:
        """starts_with should return True when field starts with value."""
        result = _evaluate_condition(
            'tool_input.filepath starts_with "5-notes/"',
            {"filepath": "5-notes/todo.md"},
        )
        assert result is True

    def test_starts_with_false(self) -> None:
        """starts_with should return False when field does not start with value."""
        result = _evaluate_condition(
            'tool_input.filepath starts_with "5-notes/"',
            {"filepath": "3-resources/doc.md"},
        )
        assert result is False

    def test_ends_with_true(self) -> None:
        """ends_with should return True when field ends with value."""
        result = _evaluate_condition(
            'tool_input.file_path ends_with ".env"',
            {"file_path": "/home/user/.env"},
        )
        assert result is True

    def test_ends_with_false(self) -> None:
        """ends_with should return False when field does not end with value."""
        result = _evaluate_condition(
            'tool_input.file_path ends_with ".env"',
            {"file_path": "/home/user/config.yaml"},
        )
        assert result is False

    def test_matches_true(self) -> None:
        """matches should return True for matching regex."""
        result = _evaluate_condition(
            r'tool_input.command matches "^git\s+push"',
            {"command": "git push --force"},
        )
        assert result is True

    def test_matches_false(self) -> None:
        """matches should return False for non-matching regex."""
        result = _evaluate_condition(
            r'tool_input.command matches "^git\s+push"',
            {"command": "echo hello"},
        )
        assert result is False

    def test_matches_invalid_regex_returns_none(self) -> None:
        """Invalid regex should return None (malformed condition)."""
        result = _evaluate_condition(
            'tool_input.command matches "[invalid"',
            {"command": "anything"},
        )
        assert result is None

    def test_equals_true(self) -> None:
        """== should return True when field equals value."""
        result = _evaluate_condition(
            'tool_input.action == "delete"',
            {"action": "delete"},
        )
        assert result is True

    def test_equals_false(self) -> None:
        """== should return False when field does not equal value."""
        result = _evaluate_condition(
            'tool_input.action == "delete"',
            {"action": "create"},
        )
        assert result is False

    def test_missing_field_returns_false(self) -> None:
        """Missing field should return False (condition not met)."""
        result = _evaluate_condition(
            'tool_input.missing_field contains "test"',
            {"other_field": "data"},
        )
        assert result is False

    def test_empty_tool_input_returns_false(self) -> None:
        """Empty tool_input should return False for any condition."""
        result = _evaluate_condition(
            'tool_input.command contains "rm"',
            {},
        )
        assert result is False

    def test_malformed_expression_returns_none(self) -> None:
        """Malformed expression should return None."""
        result = _evaluate_condition("not a valid expression", {"command": "ls"})
        assert result is None

    def test_nested_field_path(self) -> None:
        """Should evaluate conditions on nested tool_input fields."""
        result = _evaluate_condition(
            'tool_input.options.recursive == "true"',
            {"options": {"recursive": "true"}},
        )
        assert result is True


# ---------------------------------------------------------------------------
# GuardEngine.evaluate -- tool name matching
# ---------------------------------------------------------------------------


class TestGuardEngineToolMatching:
    """Tests for GuardEngine tool name matching."""

    @pytest.fixture
    def engine(self) -> GuardEngine:
        return GuardEngine()

    def test_exact_match(self, engine: GuardEngine) -> None:
        """Exact tool name match should fire the rule."""
        rules = [GuardRule(match="mcp__gmail__send_email", action="confirm", reason="Email")]
        result = engine.evaluate("mcp__gmail__send_email", {}, rules)
        assert result.action == "confirm"
        assert result.reason == "Email"

    def test_exact_match_no_match(self, engine: GuardEngine) -> None:
        """Non-matching exact name should allow."""
        rules = [GuardRule(match="mcp__gmail__send_email", action="block", reason="No")]
        result = engine.evaluate("Bash", {}, rules)
        assert result.action == "allow"

    def test_glob_star_match(self, engine: GuardEngine) -> None:
        """Glob * pattern should match."""
        rules = [GuardRule(match="mcp__gmail__*", action="warn", reason="Gmail")]
        result = engine.evaluate("mcp__gmail__send_email", {}, rules)
        assert result.action == "warn"

    def test_glob_star_no_match(self, engine: GuardEngine) -> None:
        """Glob * should not match other tool families."""
        rules = [GuardRule(match="mcp__gmail__*", action="warn", reason="Gmail")]
        result = engine.evaluate("mcp__slack__post_message", {}, rules)
        assert result.action == "allow"

    def test_glob_question_mark(self, engine: GuardEngine) -> None:
        """Glob ? should match a single character."""
        rules = [GuardRule(match="Tool?", action="block", reason="T")]
        result = engine.evaluate("ToolX", {}, rules)
        assert result.action == "block"

    def test_glob_question_mark_no_match(self, engine: GuardEngine) -> None:
        """Glob ? should not match multiple characters."""
        rules = [GuardRule(match="Tool?", action="block", reason="T")]
        result = engine.evaluate("ToolXY", {}, rules)
        assert result.action == "allow"

    def test_glob_bracket_pattern(self, engine: GuardEngine) -> None:
        """Glob [abc] should match character set."""
        rules = [GuardRule(match="Tool[AB]", action="warn", reason="T")]
        result = engine.evaluate("ToolA", {}, rules)
        assert result.action == "warn"

    def test_glob_broad_star(self, engine: GuardEngine) -> None:
        """Single * should match any tool name."""
        rules = [GuardRule(match="*", action="warn", reason="everything")]
        result = engine.evaluate("anything", {}, rules)
        assert result.action == "warn"


# ---------------------------------------------------------------------------
# GuardEngine.evaluate -- actions
# ---------------------------------------------------------------------------


class TestGuardEngineActions:
    """Tests for the three guard actions: block, warn, confirm."""

    @pytest.fixture
    def engine(self) -> GuardEngine:
        return GuardEngine()

    def test_block_action(self, engine: GuardEngine) -> None:
        """Block action should return block result."""
        rules = [GuardRule(match="Bash", action="block", reason="No bash")]
        result = engine.evaluate("Bash", {}, rules)
        assert result.action == "block"
        assert result.reason == "No bash"
        assert result.matched_rule is not None

    def test_warn_action(self, engine: GuardEngine) -> None:
        """Warn action should return warn result."""
        rules = [GuardRule(match="Bash", action="warn", reason="Be careful")]
        result = engine.evaluate("Bash", {}, rules)
        assert result.action == "warn"

    def test_confirm_action(self, engine: GuardEngine) -> None:
        """Confirm action should return confirm result."""
        rules = [GuardRule(match="Bash", action="confirm", reason="Sure?")]
        result = engine.evaluate("Bash", {}, rules)
        assert result.action == "confirm"

    def test_no_match_returns_allow(self, engine: GuardEngine) -> None:
        """No matching rule should return allow."""
        rules = [GuardRule(match="Bash", action="block", reason="No")]
        result = engine.evaluate("Write", {}, rules)
        assert result.action == "allow"
        assert result.reason is None
        assert result.matched_rule is None


# ---------------------------------------------------------------------------
# GuardEngine.evaluate -- first-match-wins ordering
# ---------------------------------------------------------------------------


class TestGuardEngineFirstMatchWins:
    """Tests for first-match-wins evaluation order."""

    @pytest.fixture
    def engine(self) -> GuardEngine:
        return GuardEngine()

    def test_first_matching_rule_wins(self, engine: GuardEngine) -> None:
        """First matching rule should determine the result."""
        rules = [
            GuardRule(match="Bash", action="warn", reason="Warning"),
            GuardRule(match="Bash", action="block", reason="Block"),
        ]
        result = engine.evaluate("Bash", {}, rules)
        assert result.action == "warn"
        assert result.reason == "Warning"

    def test_non_matching_rules_skipped(self, engine: GuardEngine) -> None:
        """Non-matching rules should be skipped, later match should fire."""
        rules = [
            GuardRule(match="Write", action="block", reason="No write"),
            GuardRule(match="Bash", action="warn", reason="Bash warning"),
        ]
        result = engine.evaluate("Bash", {}, rules)
        assert result.action == "warn"
        assert result.reason == "Bash warning"

    def test_specific_before_glob(self, engine: GuardEngine) -> None:
        """Specific match before glob should win when listed first."""
        rules = [
            GuardRule(match="mcp__gmail__send_email", action="confirm", reason="Send"),
            GuardRule(match="mcp__gmail__*", action="warn", reason="Gmail"),
        ]
        result = engine.evaluate("mcp__gmail__send_email", {}, rules)
        assert result.action == "confirm"

    def test_glob_before_specific(self, engine: GuardEngine) -> None:
        """Glob listed before specific should win (order matters, not specificity)."""
        rules = [
            GuardRule(match="mcp__gmail__*", action="warn", reason="Gmail"),
            GuardRule(match="mcp__gmail__send_email", action="confirm", reason="Send"),
        ]
        result = engine.evaluate("mcp__gmail__send_email", {}, rules)
        assert result.action == "warn"


# ---------------------------------------------------------------------------
# GuardEngine.evaluate -- when conditions
# ---------------------------------------------------------------------------


class TestGuardEngineWhenCondition:
    """Tests for 'when' condition evaluation in guard rules."""

    @pytest.fixture
    def engine(self) -> GuardEngine:
        return GuardEngine()

    def test_when_met_fires_rule(self, engine: GuardEngine) -> None:
        """Rule should fire when 'when' condition is met."""
        rules = [GuardRule(
            match="Bash",
            action="block",
            reason="No force push",
            when='tool_input.command contains "git push --force"',
        )]
        result = engine.evaluate(
            "Bash", {"command": "git push --force origin main"}, rules
        )
        assert result.action == "block"

    def test_when_not_met_skips_rule(self, engine: GuardEngine) -> None:
        """Rule should be skipped when 'when' condition is not met."""
        rules = [GuardRule(
            match="Bash",
            action="block",
            reason="No force push",
            when='tool_input.command contains "git push --force"',
        )]
        result = engine.evaluate(
            "Bash", {"command": "git push origin main"}, rules
        )
        assert result.action == "allow"

    def test_when_not_met_allows_later_rule(self, engine: GuardEngine) -> None:
        """When condition not met should skip to next rule."""
        rules = [
            GuardRule(
                match="Bash",
                action="block",
                reason="No force push",
                when='tool_input.command contains "git push --force"',
            ),
            GuardRule(
                match="Bash",
                action="warn",
                reason="Bash used",
            ),
        ]
        result = engine.evaluate(
            "Bash", {"command": "ls -la"}, rules
        )
        assert result.action == "warn"
        assert result.reason == "Bash used"

    def test_malformed_when_skips_rule(self, engine: GuardEngine) -> None:
        """Malformed 'when' condition should skip the rule (fail-open)."""
        rules = [
            GuardRule(
                match="Bash",
                action="block",
                reason="Bad condition",
                when="this is not valid",
            ),
            GuardRule(match="Bash", action="warn", reason="Fallback"),
        ]
        result = engine.evaluate("Bash", {"command": "ls"}, rules)
        assert result.action == "warn"
        assert result.reason == "Fallback"


# ---------------------------------------------------------------------------
# GuardEngine.evaluate -- unless conditions
# ---------------------------------------------------------------------------


class TestGuardEngineUnlessCondition:
    """Tests for 'unless' condition evaluation in guard rules."""

    @pytest.fixture
    def engine(self) -> GuardEngine:
        return GuardEngine()

    def test_unless_met_skips_rule(self, engine: GuardEngine) -> None:
        """Rule should be skipped when 'unless' condition is met."""
        rules = [GuardRule(
            match="mcp__obsidian-rest__obsidian_append_content",
            action="warn",
            reason="Writing to Obsidian vault.",
            unless='tool_input.filepath starts_with "5-notes/"',
        )]
        result = engine.evaluate(
            "mcp__obsidian-rest__obsidian_append_content",
            {"filepath": "5-notes/today.md"},
            rules,
        )
        assert result.action == "allow"

    def test_unless_not_met_fires_rule(self, engine: GuardEngine) -> None:
        """Rule should fire when 'unless' condition is not met."""
        rules = [GuardRule(
            match="mcp__obsidian-rest__obsidian_append_content",
            action="warn",
            reason="Writing to Obsidian vault.",
            unless='tool_input.filepath starts_with "5-notes/"',
        )]
        result = engine.evaluate(
            "mcp__obsidian-rest__obsidian_append_content",
            {"filepath": "0-plan/important.md"},
            rules,
        )
        assert result.action == "warn"

    def test_unless_with_when_both_apply(self, engine: GuardEngine) -> None:
        """Both when and unless should be evaluated: when must be true, unless must be false."""
        rules = [GuardRule(
            match="Bash",
            action="block",
            reason="Dangerous",
            when='tool_input.command contains "rm"',
            unless='tool_input.command contains "rm -i"',
        )]
        # when met, unless met -> skip rule
        result = engine.evaluate("Bash", {"command": "rm -i file.txt"}, rules)
        assert result.action == "allow"

        # when met, unless not met -> fire rule
        result = engine.evaluate("Bash", {"command": "rm -rf /"}, rules)
        assert result.action == "block"

        # when not met -> skip rule
        result = engine.evaluate("Bash", {"command": "ls"}, rules)
        assert result.action == "allow"

    def test_malformed_unless_skips_rule(self, engine: GuardEngine) -> None:
        """Malformed 'unless' condition should skip the rule (fail-open)."""
        rules = [
            GuardRule(
                match="Bash",
                action="block",
                reason="Bad unless",
                unless="broken expression here",
            ),
            GuardRule(match="Bash", action="warn", reason="Fallback"),
        ]
        result = engine.evaluate("Bash", {"command": "ls"}, rules)
        assert result.action == "warn"


# ---------------------------------------------------------------------------
# GuardEngine.evaluate -- edge cases
# ---------------------------------------------------------------------------


class TestGuardEngineEdgeCases:
    """Edge case tests for GuardEngine."""

    @pytest.fixture
    def engine(self) -> GuardEngine:
        return GuardEngine()

    def test_empty_rules_returns_allow(self, engine: GuardEngine) -> None:
        """Empty rules list should return allow."""
        result = engine.evaluate("Bash", {"command": "ls"}, [])
        assert result.action == "allow"

    def test_empty_tool_input(self, engine: GuardEngine) -> None:
        """Rule without conditions should still match on empty tool_input."""
        rules = [GuardRule(match="Bash", action="warn", reason="Bash detected")]
        result = engine.evaluate("Bash", {}, rules)
        assert result.action == "warn"

    def test_empty_tool_name(self, engine: GuardEngine) -> None:
        """Empty tool name should not match non-wildcard rules."""
        rules = [GuardRule(match="Bash", action="block", reason="No")]
        result = engine.evaluate("", {}, rules)
        assert result.action == "allow"

    def test_empty_tool_name_matches_wildcard(self, engine: GuardEngine) -> None:
        """Empty tool name should match wildcard."""
        rules = [GuardRule(match="*", action="warn", reason="All")]
        result = engine.evaluate("", {}, rules)
        assert result.action == "warn"

    def test_matched_rule_is_set(self, engine: GuardEngine) -> None:
        """matched_rule should reference the exact rule that fired."""
        rule = GuardRule(match="Bash", action="block", reason="No bash")
        result = engine.evaluate("Bash", {}, [rule])
        assert result.matched_rule is rule

    def test_matched_rule_is_none_on_allow(self, engine: GuardEngine) -> None:
        """matched_rule should be None when no rule matches."""
        result = engine.evaluate("Write", {}, [
            GuardRule(match="Bash", action="block", reason="No"),
        ])
        assert result.matched_rule is None

    def test_deeply_nested_tool_input_condition(self, engine: GuardEngine) -> None:
        """Should handle deeply nested field paths in conditions."""
        rules = [GuardRule(
            match="CustomTool",
            action="warn",
            reason="Deep nesting",
            when='tool_input.config.database.host contains "prod"',
        )]
        result = engine.evaluate(
            "CustomTool",
            {"config": {"database": {"host": "prod-db.example.com"}}},
            rules,
        )
        assert result.action == "warn"


# ---------------------------------------------------------------------------
# parse_guard_rules
# ---------------------------------------------------------------------------


class TestParseGuardRules:
    """Tests for parsing raw guard config dicts into GuardRule objects."""

    def test_valid_guard(self) -> None:
        """Valid guard dict should be parsed into a GuardRule."""
        raw = [{"match": "Bash", "action": "block", "reason": "No bash"}]
        rules = parse_guard_rules(raw)
        assert len(rules) == 1
        assert rules[0].match == "Bash"
        assert rules[0].action == "block"
        assert rules[0].reason == "No bash"

    def test_missing_match_skipped(self) -> None:
        """Guard without match should be skipped."""
        raw = [{"action": "block", "reason": "No match"}]
        rules = parse_guard_rules(raw)
        assert rules == []

    def test_missing_action_skipped(self) -> None:
        """Guard without action should be skipped."""
        raw = [{"match": "Bash", "reason": "No action"}]
        rules = parse_guard_rules(raw)
        assert rules == []

    def test_invalid_action_skipped(self) -> None:
        """Guard with invalid action should be skipped."""
        raw = [{"match": "Bash", "action": "allow", "reason": "bad"}]
        rules = parse_guard_rules(raw)
        assert rules == []

    def test_non_dict_skipped(self) -> None:
        """Non-dict entries should be skipped."""
        raw = ["not_a_dict", {"match": "Bash", "action": "block", "reason": "ok"}]
        rules = parse_guard_rules(raw)
        assert len(rules) == 1
        assert rules[0].match == "Bash"

    def test_with_when_and_unless(self) -> None:
        """when and unless should be preserved."""
        raw = [{
            "match": "Bash",
            "action": "block",
            "reason": "guarded",
            "when": 'tool_input.command contains "rm"',
            "unless": 'tool_input.command contains "-i"',
        }]
        rules = parse_guard_rules(raw)
        assert len(rules) == 1
        assert rules[0].when == 'tool_input.command contains "rm"'
        assert rules[0].unless == 'tool_input.command contains "-i"'

    def test_default_reason(self) -> None:
        """Missing reason should default to empty string."""
        raw = [{"match": "Bash", "action": "warn"}]
        rules = parse_guard_rules(raw)
        assert rules[0].reason == ""

    def test_empty_list(self) -> None:
        """Empty raw list should return empty rules list."""
        assert parse_guard_rules([]) == []

    def test_preserves_order(self) -> None:
        """Rules should preserve config order."""
        raw = [
            {"match": "Bash", "action": "block", "reason": "first"},
            {"match": "Write", "action": "warn", "reason": "second"},
            {"match": "Read", "action": "confirm", "reason": "third"},
        ]
        rules = parse_guard_rules(raw)
        assert [r.match for r in rules] == ["Bash", "Write", "Read"]

    def test_all_three_actions_valid(self) -> None:
        """block, warn, confirm should all be accepted."""
        raw = [
            {"match": "A", "action": "block", "reason": "b"},
            {"match": "B", "action": "warn", "reason": "w"},
            {"match": "C", "action": "confirm", "reason": "c"},
        ]
        rules = parse_guard_rules(raw)
        assert len(rules) == 3
        assert [r.action for r in rules] == ["block", "warn", "confirm"]


# ---------------------------------------------------------------------------
# handle() -- builtin handler entry point
# ---------------------------------------------------------------------------


class TestHandleEntryPoint:
    """Tests for the handle() builtin handler entry point."""

    def test_non_pretooluse_returns_none(self) -> None:
        """Non-PreToolUse events should return None (no-op)."""
        config = HooksConfig(guards=[{
            "match": "Bash", "action": "block", "reason": "No",
        }])
        result = handle("PostToolUse", {"tool_name": "Bash"}, config)
        assert result is None

    def test_no_guards_returns_none(self) -> None:
        """Empty guards list should return None."""
        config = HooksConfig()
        result = handle("PreToolUse", {"tool_name": "Bash"}, config)
        assert result is None

    def test_block_returns_decision_dict(self) -> None:
        """Matching block rule should return decision dict."""
        config = HooksConfig(guards=[{
            "match": "Bash", "action": "block", "reason": "Blocked",
        }])
        result = handle(
            "PreToolUse",
            {"tool_name": "Bash", "tool_input": {"command": "ls"}},
            config,
        )
        assert result is not None
        assert result["decision"] == "block"
        assert result["reason"] == "Blocked"

    def test_warn_returns_decision_dict(self) -> None:
        """Matching warn rule should return decision dict."""
        config = HooksConfig(guards=[{
            "match": "mcp__gmail__*", "action": "warn", "reason": "Gmail detected",
        }])
        result = handle(
            "PreToolUse",
            {"tool_name": "mcp__gmail__send_email", "tool_input": {}},
            config,
        )
        assert result is not None
        assert result["decision"] == "warn"

    def test_allow_returns_none(self) -> None:
        """No matching rule should return None (allow)."""
        config = HooksConfig(guards=[{
            "match": "Bash", "action": "block", "reason": "No bash",
        }])
        result = handle(
            "PreToolUse",
            {"tool_name": "Write", "tool_input": {}},
            config,
        )
        assert result is None

    def test_non_dict_tool_input_handled(self) -> None:
        """Non-dict tool_input in payload should be handled gracefully."""
        config = HooksConfig(guards=[{
            "match": "Bash", "action": "warn", "reason": "Bash",
        }])
        result = handle(
            "PreToolUse",
            {"tool_name": "Bash", "tool_input": "not_a_dict"},
            config,
        )
        assert result is not None
        assert result["decision"] == "warn"

    def test_missing_tool_name_handled(self) -> None:
        """Missing tool_name in payload should be handled gracefully."""
        config = HooksConfig(guards=[{
            "match": "Bash", "action": "block", "reason": "No",
        }])
        result = handle("PreToolUse", {}, config)
        assert result is None  # Empty string doesn't match "Bash"


# ---------------------------------------------------------------------------
# Dispatcher integration -- guard_rails wiring
# ---------------------------------------------------------------------------


class TestDispatcherGuardRailsIntegration:
    """Tests for guard rails engine integration with the dispatcher."""

    @pytest.fixture
    def dispatcher(self) -> Dispatcher:
        return Dispatcher(config_engine=ConfigEngine())

    def test_guard_rule_block_via_dispatcher(self, dispatcher: Dispatcher) -> None:
        """Guard rule block should produce exit_code=2 through the dispatcher."""
        config = HooksConfig(guards=[{
            "match": "Bash",
            "action": "block",
            "reason": "Bash blocked by guard",
        }])
        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "Bash", "tool_input": {"command": "rm -rf /"}},
            config=config,
        )
        assert result.exit_code == 2
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert parsed["decision"] == "block"
        assert parsed["reason"] == "Bash blocked by guard"

    def test_guard_rule_warn_via_dispatcher(self, dispatcher: Dispatcher) -> None:
        """Guard rule warn should produce exit_code=0 with warn decision."""
        config = HooksConfig(guards=[{
            "match": "mcp__gmail__*",
            "action": "warn",
            "reason": "Gmail detected",
        }])
        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "mcp__gmail__send_email", "tool_input": {}},
            config=config,
        )
        assert result.exit_code == 0
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert parsed["decision"] == "warn"

    def test_guard_rule_confirm_via_dispatcher(self, dispatcher: Dispatcher) -> None:
        """Guard rule confirm should produce exit_code=0 with confirm decision."""
        config = HooksConfig(guards=[{
            "match": "mcp__gmail__send_email",
            "action": "confirm",
            "reason": "Confirm email",
        }])
        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "mcp__gmail__send_email", "tool_input": {}},
            config=config,
        )
        assert result.exit_code == 0
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert parsed["decision"] == "confirm"

    def test_guard_rule_allow_does_not_block(self, dispatcher: Dispatcher) -> None:
        """No matching guard rule should allow the dispatch to continue."""
        config = HooksConfig(guards=[{
            "match": "mcp__gmail__*",
            "action": "block",
            "reason": "Gmail blocked",
        }])
        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "Bash", "tool_input": {"command": "ls"}},
            config=config,
        )
        assert result.exit_code == 0
        # No stdout since no context handlers are configured
        assert result.stdout is None

    def test_guard_rule_with_when_condition_via_dispatcher(
        self, dispatcher: Dispatcher,
    ) -> None:
        """Guard rule with when condition should only fire when condition is met."""
        config = HooksConfig(guards=[{
            "match": "Bash",
            "action": "block",
            "reason": "No force push",
            "when": 'tool_input.command contains "git push --force"',
        }])

        # Condition met -> block
        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "Bash", "tool_input": {"command": "git push --force origin main"}},
            config=config,
        )
        assert result.exit_code == 2

        # Condition not met -> allow
        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "Bash", "tool_input": {"command": "git push origin main"}},
            config=config,
        )
        assert result.exit_code == 0

    def test_guard_rule_with_unless_condition_via_dispatcher(
        self, dispatcher: Dispatcher,
    ) -> None:
        """Guard rule with unless condition should skip when exception is met."""
        config = HooksConfig(guards=[{
            "match": "mcp__obsidian-rest__obsidian_append_content",
            "action": "warn",
            "reason": "Writing to Obsidian vault.",
            "unless": 'tool_input.filepath starts_with "5-notes/"',
        }])

        # Unless met -> allow (rule skipped)
        result = dispatcher.dispatch(
            "PreToolUse",
            {
                "tool_name": "mcp__obsidian-rest__obsidian_append_content",
                "tool_input": {"filepath": "5-notes/fleeting.md"},
            },
            config=config,
        )
        assert result.exit_code == 0
        assert result.stdout is None

        # Unless not met -> warn
        result = dispatcher.dispatch(
            "PreToolUse",
            {
                "tool_name": "mcp__obsidian-rest__obsidian_append_content",
                "tool_input": {"filepath": "0-plan/goals.md"},
            },
            config=config,
        )
        assert result.exit_code == 0
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert parsed["decision"] == "warn"

    def test_guard_rules_not_evaluated_for_non_pretooluse(
        self, dispatcher: Dispatcher,
    ) -> None:
        """Guard rules should only be evaluated for PreToolUse events."""
        config = HooksConfig(guards=[{
            "match": "Bash",
            "action": "block",
            "reason": "Blocked",
        }])
        result = dispatcher.dispatch(
            "PostToolUse",
            {"tool_name": "Bash", "tool_input": {}},
            config=config,
        )
        assert result.exit_code == 0

    def test_handler_style_guards_still_work(self, dispatcher: Dispatcher) -> None:
        """Old-style handler guards (with type/events) should still work alongside."""
        config = HooksConfig(guards=[
            # match/action style -- evaluated by guard engine
            {
                "match": "Write",
                "action": "block",
                "reason": "No writing",
            },
            # handler-style -- evaluated by dispatcher as before
            {
                "name": "handler_guard",
                "type": "inline",
                "events": ["PreToolUse"],
                "action": {"type": "block", "reason": "handler-style block"},
            },
        ])
        # Guard engine should block Write
        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "Write", "tool_input": {}},
            config=config,
        )
        assert result.exit_code == 2
        parsed = json.loads(result.stdout)
        assert parsed["reason"] == "No writing"

    def test_guard_rules_before_handler_guards(self, dispatcher: Dispatcher) -> None:
        """Guard engine rules should be evaluated before handler-style guards."""
        config = HooksConfig(guards=[
            {
                "match": "Bash",
                "action": "warn",
                "reason": "Engine says warn",
            },
            {
                "name": "handler_guard",
                "type": "inline",
                "events": ["PreToolUse"],
                "action": {"type": "block", "reason": "Handler says block"},
            },
        ])
        # Guard engine should fire first (warn, not block)
        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "Bash", "tool_input": {}},
            config=config,
        )
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert parsed["decision"] == "warn"
        assert parsed["reason"] == "Engine says warn"


# ---------------------------------------------------------------------------
# Full YAML config integration
# ---------------------------------------------------------------------------


class TestGuardRailsYAMLIntegration:
    """Tests for guard rails loaded from YAML config files."""

    def test_full_yaml_config(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Should load and evaluate guards from a YAML config file."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "guards:\n"
            '  - match: "mcp__gmail__send_email"\n'
            "    action: confirm\n"
            '    reason: "Confirm email send"\n'
            "\n"
            '  - match: "Bash"\n'
            "    action: block\n"
            "    when: 'tool_input.command contains \"git push --force\"'\n"
            '    reason: "Force push blocked"\n'
            "\n"
            '  - match: "mcp__gmail__*"\n'
            "    action: warn\n"
            '    reason: "Gmail tool detected"\n',
            encoding="utf-8",
        )
        dispatcher = Dispatcher()
        # Test confirm for specific Gmail tool
        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "mcp__gmail__send_email", "tool_input": {}},
            project_dir=tmp_path,
        )
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert parsed["decision"] == "confirm"

    def test_yaml_bash_guard_with_condition(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Bash guard with when condition should block force push."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "guards:\n"
            '  - match: "Bash"\n'
            "    action: block\n"
            "    when: 'tool_input.command contains \"git push --force\"'\n"
            '    reason: "Force push blocked"\n',
            encoding="utf-8",
        )
        dispatcher = Dispatcher()

        # Force push -- blocked
        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "Bash", "tool_input": {"command": "git push --force origin main"}},
            project_dir=tmp_path,
        )
        assert result.exit_code == 2
        parsed = json.loads(result.stdout)
        assert parsed["reason"] == "Force push blocked"

        # Normal push -- allowed
        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "Bash", "tool_input": {"command": "git push origin main"}},
            project_dir=tmp_path,
        )
        assert result.exit_code == 0

    def test_yaml_glob_fallthrough(
        self, tmp_path: Path, tmp_state_dir: Path,
    ) -> None:
        """Glob pattern should catch tools that don't match earlier specific rules."""
        config_file = tmp_path / "hookwise.yaml"
        config_file.write_text(
            "guards:\n"
            '  - match: "mcp__gmail__send_email"\n'
            "    action: confirm\n"
            '    reason: "Confirm email"\n'
            "\n"
            '  - match: "mcp__gmail__*"\n'
            "    action: warn\n"
            '    reason: "Gmail tool"\n',
            encoding="utf-8",
        )
        dispatcher = Dispatcher()

        # read_email should hit the glob rule
        result = dispatcher.dispatch(
            "PreToolUse",
            {"tool_name": "mcp__gmail__read_email", "tool_input": {}},
            project_dir=tmp_path,
        )
        assert result.stdout is not None
        parsed = json.loads(result.stdout)
        assert parsed["decision"] == "warn"
        assert parsed["reason"] == "Gmail tool"


# ---------------------------------------------------------------------------
# Condition operators -- comprehensive
# ---------------------------------------------------------------------------


class TestConditionOperatorsComprehensive:
    """Comprehensive tests for all 5 condition operators."""

    @pytest.fixture
    def engine(self) -> GuardEngine:
        return GuardEngine()

    def test_contains_substring_match(self, engine: GuardEngine) -> None:
        """contains should match substring anywhere in the field value."""
        rules = [GuardRule(
            match="Bash", action="block", reason="rm found",
            when='tool_input.command contains "rm"',
        )]
        # rm in middle
        assert engine.evaluate("Bash", {"command": "sudo rm -rf"}, rules).action == "block"
        # rm at start
        assert engine.evaluate("Bash", {"command": "rm file.txt"}, rules).action == "block"
        # rm at end
        assert engine.evaluate("Bash", {"command": "echo rm"}, rules).action == "block"
        # no rm
        assert engine.evaluate("Bash", {"command": "ls -la"}, rules).action == "allow"

    def test_starts_with_prefix_match(self, engine: GuardEngine) -> None:
        """starts_with should match only at the beginning of the field value."""
        rules = [GuardRule(
            match="Bash", action="block", reason="sudo detected",
            when='tool_input.command starts_with "sudo"',
        )]
        assert engine.evaluate("Bash", {"command": "sudo rm -rf"}, rules).action == "block"
        assert engine.evaluate("Bash", {"command": "echo sudo"}, rules).action == "allow"

    def test_ends_with_suffix_match(self, engine: GuardEngine) -> None:
        """ends_with should match only at the end of the field value."""
        rules = [GuardRule(
            match="Write", action="warn", reason="env file",
            when='tool_input.file_path ends_with ".env"',
        )]
        assert engine.evaluate("Write", {"file_path": "/app/.env"}, rules).action == "warn"
        assert engine.evaluate("Write", {"file_path": "/app/.envrc"}, rules).action == "allow"

    def test_matches_regex_patterns(self, engine: GuardEngine) -> None:
        """matches should support full regex patterns."""
        rules = [GuardRule(
            match="Bash", action="block", reason="destructive git",
            when=r'tool_input.command matches "git\s+(push\s+--force|reset\s+--hard)"',
        )]
        assert engine.evaluate(
            "Bash", {"command": "git push --force origin main"}, rules
        ).action == "block"
        assert engine.evaluate(
            "Bash", {"command": "git reset --hard HEAD~1"}, rules
        ).action == "block"
        assert engine.evaluate(
            "Bash", {"command": "git push origin main"}, rules
        ).action == "allow"

    def test_equals_exact_match(self, engine: GuardEngine) -> None:
        """== should require exact equality."""
        rules = [GuardRule(
            match="CustomTool", action="block", reason="exact",
            when='tool_input.mode == "delete"',
        )]
        assert engine.evaluate(
            "CustomTool", {"mode": "delete"}, rules
        ).action == "block"
        assert engine.evaluate(
            "CustomTool", {"mode": "delete_all"}, rules
        ).action == "allow"
        assert engine.evaluate(
            "CustomTool", {"mode": "safe_delete"}, rules
        ).action == "allow"
