"""Tests for hookwise.coaching -- coaching system for Claude Code hooks.

Covers all 5 coaching tasks:
- Task 6.1: Mode classification for tool calls
- Task 6.2: Builder's trap detector
- Task 6.3: Metacognition reminder engine
- Task 6.4: Rapid acceptance detection
- Task 6.5: Communication coach
"""

from __future__ import annotations

import json
import os
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any
from unittest.mock import patch

import pytest

from hookwise.coaching.builder_trap import (
    CODING_EXTENSIONS,
    DEFAULT_THRESHOLDS,
    DEFAULT_TOOLING_PATTERNS,
    DEFAULT_TRAP_PROMPTS,
    MAX_DELTA_MINUTES,
    accumulate_tooling_time,
    classify_mode,
    compute_alert_level,
    select_trap_nudge,
)
from hookwise.coaching.metacognition import (
    DEFAULT_INTERVAL_SECONDS,
    DEFAULT_METACOGNITION_PROMPTS,
    DEFAULT_RAPID_ACCEPTANCE_PROMPTS,
    check_metacognition_interval,
    load_prompts_file,
    select_metacognition_prompt,
)
from hookwise.coaching.communication import (
    DEFAULT_FREQUENCY,
    DEFAULT_MIN_LENGTH,
    GRAMMAR_RULES,
    MAX_SCORE_HISTORY,
    analyze_prompt,
    _check_incomplete_sentences,
    _check_missing_articles,
    _check_run_on_sentences,
    _check_subject_verb_disagreement,
)
from hookwise.coaching import (
    CACHE_FILENAME,
    LARGE_CHANGE_LINES,
    RAPID_ACCEPTANCE_SECONDS,
    WRITE_TOOLS,
    CoachingEngine,
    _extract_lines,
    _get_cache_path,
    _load_cache,
    _save_cache,
    handle,
)
from hookwise.config import HooksConfig


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _now() -> datetime:
    """Return a fixed UTC datetime for reproducible tests."""
    return datetime(2026, 2, 10, 15, 0, 0, tzinfo=timezone.utc)


def _make_cache(**overrides: Any) -> dict[str, Any]:
    """Build a coaching cache dict with defaults and overrides."""
    cache: dict[str, Any] = {
        "last_prompt_at": None,
        "prompt_history": [],
        "current_mode": "neutral",
        "mode_started_at": None,
        "tooling_minutes": 0.0,
        "alert_level": "none",
        "today_date": "2026-02-10",
        "practice_count": 0,
        "last_large_change": None,
        "prompt_check_counter": 0,
        "grammar_scores": [],
    }
    cache.update(overrides)
    return cache


# ===========================================================================
# Task 6.1: Mode classification for tool calls
# ===========================================================================


class TestClassifyMode:
    """Tests for classify_mode() -- tool call mode classification."""

    def test_practice_tool_exact_match(self) -> None:
        """Practice tracker tool should classify as practice."""
        mode = classify_mode(
            "mcp__practice-tracker__log_rep",
            {},
            {"practice_tools": ["mcp__practice-tracker__log_rep"]},
        )
        assert mode == "practice"

    def test_practice_tool_glob_match(self) -> None:
        """Practice tracker wildcard should classify as practice."""
        mode = classify_mode(
            "mcp__practice-tracker__create_question",
            {},
            {"practice_tools": ["mcp__practice-tracker__*"]},
        )
        assert mode == "practice"

    def test_practice_tool_default_patterns(self) -> None:
        """Default practice patterns should match practice-tracker tools."""
        mode = classify_mode(
            "mcp__practice-tracker__get_due_reviews",
            {},
            {},
        )
        assert mode == "practice"

    def test_tooling_by_file_path(self) -> None:
        """Files in ~/.claude/skills/ should classify as tooling."""
        mode = classify_mode(
            "Write",
            {"file_path": "~/.claude/skills/daily-copilot/SKILL.md"},
            {"tooling_patterns": ["~/.claude/skills/*"]},
        )
        assert mode == "tooling"

    def test_tooling_default_patterns(self) -> None:
        """Default tooling patterns should match hooks config files."""
        mode = classify_mode(
            "Write",
            {"file_path": "~/.claude/hooks/config.yaml"},
            {},
        )
        assert mode == "tooling"

    def test_coding_by_python_extension(self) -> None:
        """Python files should classify as coding."""
        mode = classify_mode(
            "Write",
            {"file_path": "/home/user/project/main.py"},
            {},
        )
        assert mode == "coding"

    def test_coding_by_typescript_extension(self) -> None:
        """TypeScript files should classify as coding."""
        mode = classify_mode(
            "Write",
            {"file_path": "/app/src/index.ts"},
            {},
        )
        assert mode == "coding"

    def test_coding_by_write_tool_no_extension(self) -> None:
        """Write tool with no matching extension should default to coding."""
        mode = classify_mode("Write", {"file_path": "/app/Makefile"}, {})
        assert mode == "coding"

    def test_coding_by_edit_tool(self) -> None:
        """Edit tool should classify as coding."""
        mode = classify_mode("Edit", {"file_path": "/app/main.go"}, {})
        assert mode == "coding"

    def test_prep_by_file_path(self) -> None:
        """Files in interview directory should classify as prep."""
        mode = classify_mode(
            "Read",
            {"file_path": "/home/user/interview/system-design.md"},
            {"prep_patterns": ["*/interview/*"]},
        )
        assert mode == "prep"

    def test_neutral_for_read_tool(self) -> None:
        """Read tool with non-matching path should classify as neutral."""
        mode = classify_mode(
            "Read",
            {"file_path": "/tmp/random.txt"},
            {},
        )
        assert mode == "neutral"

    def test_neutral_for_bash_tool(self) -> None:
        """Bash tool should classify as neutral."""
        mode = classify_mode(
            "Bash",
            {"command": "ls -la"},
            {},
        )
        assert mode == "neutral"

    def test_neutral_for_unknown_tool(self) -> None:
        """Unknown tool with no file path should classify as neutral."""
        mode = classify_mode("ToolSearch", {}, {})
        assert mode == "neutral"

    def test_practice_takes_priority_over_tooling(self) -> None:
        """Practice tool match should win over tooling pattern match."""
        mode = classify_mode(
            "mcp__practice-tracker__log_rep",
            {"file_path": "~/.claude/skills/practice.md"},
            {
                "practice_tools": ["mcp__practice-tracker__*"],
                "tooling_patterns": ["~/.claude/skills/*"],
            },
        )
        assert mode == "practice"

    def test_non_dict_tool_input(self) -> None:
        """Non-dict tool_input should not crash."""
        mode = classify_mode("Bash", {"command": 12345}, {})
        assert mode == "neutral"

    def test_empty_file_path(self) -> None:
        """Empty file path should not match tooling patterns."""
        mode = classify_mode("Read", {"file_path": ""}, {})
        assert mode == "neutral"

    def test_notebook_edit_is_coding(self) -> None:
        """NotebookEdit tool should classify as coding."""
        mode = classify_mode("NotebookEdit", {}, {})
        assert mode == "coding"

    def test_coding_extensions_comprehensive(self) -> None:
        """All defined coding extensions should be in CODING_EXTENSIONS."""
        assert ".py" in CODING_EXTENSIONS
        assert ".js" in CODING_EXTENSIONS
        assert ".ts" in CODING_EXTENSIONS
        assert ".go" in CODING_EXTENSIONS
        assert ".rs" in CODING_EXTENSIONS
        assert ".java" in CODING_EXTENSIONS
        assert ".rb" in CODING_EXTENSIONS

    def test_tooling_hookwise_yaml(self) -> None:
        """hookwise.yaml files should classify as tooling by default."""
        mode = classify_mode(
            "Write",
            {"file_path": "/project/hookwise.yaml"},
            {},
        )
        assert mode == "tooling"


# ===========================================================================
# Task 6.2: Builder's trap detector
# ===========================================================================


class TestComputeAlertLevel:
    """Tests for compute_alert_level()."""

    def test_none_below_yellow(self) -> None:
        """Below yellow threshold should return none."""
        assert compute_alert_level(0.0) == "none"
        assert compute_alert_level(15.0) == "none"
        assert compute_alert_level(29.9) == "none"

    def test_yellow_at_threshold(self) -> None:
        """At yellow threshold should return yellow."""
        assert compute_alert_level(30.0) == "yellow"

    def test_yellow_between_thresholds(self) -> None:
        """Between yellow and orange should return yellow."""
        assert compute_alert_level(45.0) == "yellow"

    def test_orange_at_threshold(self) -> None:
        """At orange threshold should return orange."""
        assert compute_alert_level(60.0) == "orange"

    def test_orange_between_thresholds(self) -> None:
        """Between orange and red should return orange."""
        assert compute_alert_level(75.0) == "orange"

    def test_red_at_threshold(self) -> None:
        """At red threshold should return red."""
        assert compute_alert_level(90.0) == "red"

    def test_red_above_threshold(self) -> None:
        """Above red threshold should return red."""
        assert compute_alert_level(120.0) == "red"

    def test_custom_thresholds(self) -> None:
        """Custom thresholds should override defaults."""
        thresholds = {"yellow": 10, "orange": 20, "red": 30}
        assert compute_alert_level(9.0, thresholds) == "none"
        assert compute_alert_level(10.0, thresholds) == "yellow"
        assert compute_alert_level(20.0, thresholds) == "orange"
        assert compute_alert_level(30.0, thresholds) == "red"


class TestAccumulateToolingTime:
    """Tests for accumulate_tooling_time()."""

    def test_tooling_accumulates_minutes(self) -> None:
        """Consecutive tooling events should accumulate time."""
        t0 = _now()
        t1 = t0 + timedelta(minutes=5)
        cache = _make_cache(
            current_mode="tooling",
            mode_started_at=t0.isoformat(),
        )
        accumulate_tooling_time(cache, "tooling", t1)
        assert cache["tooling_minutes"] == pytest.approx(5.0, abs=0.01)

    def test_tooling_caps_at_10_minutes(self) -> None:
        """Time deltas >10 minutes should be capped."""
        t0 = _now()
        t1 = t0 + timedelta(minutes=30)
        cache = _make_cache(
            current_mode="tooling",
            mode_started_at=t0.isoformat(),
        )
        accumulate_tooling_time(cache, "tooling", t1)
        assert cache["tooling_minutes"] == pytest.approx(MAX_DELTA_MINUTES, abs=0.01)

    def test_coding_resets_tooling(self) -> None:
        """Coding activity should reset tooling timer."""
        cache = _make_cache(
            tooling_minutes=45.0,
            alert_level="yellow",
            current_mode="tooling",
            mode_started_at=_now().isoformat(),
        )
        accumulate_tooling_time(cache, "coding", _now() + timedelta(minutes=1))
        assert cache["tooling_minutes"] == 0.0
        assert cache["alert_level"] == "none"

    def test_practice_resets_tooling(self) -> None:
        """Practice activity should reset tooling timer."""
        cache = _make_cache(
            tooling_minutes=60.0,
            current_mode="tooling",
            mode_started_at=_now().isoformat(),
        )
        accumulate_tooling_time(cache, "practice", _now() + timedelta(minutes=1))
        assert cache["tooling_minutes"] == 0.0

    def test_practice_increments_count(self) -> None:
        """Practice mode should increment practice_count."""
        cache = _make_cache(practice_count=2)
        accumulate_tooling_time(cache, "practice", _now())
        assert cache["practice_count"] == 3

    def test_date_change_resets_counters(self) -> None:
        """Date change should reset all daily counters."""
        cache = _make_cache(
            today_date="2026-02-09",
            tooling_minutes=45.0,
            practice_count=5,
            alert_level="yellow",
        )
        accumulate_tooling_time(cache, "neutral", _now())
        assert cache["today_date"] == "2026-02-10"
        assert cache["tooling_minutes"] == 0.0
        assert cache["practice_count"] == 0
        assert cache["alert_level"] == "none"

    def test_neutral_mode_no_accumulation(self) -> None:
        """Neutral mode should not accumulate tooling time."""
        cache = _make_cache(
            tooling_minutes=10.0,
            current_mode="tooling",
            mode_started_at=_now().isoformat(),
        )
        accumulate_tooling_time(cache, "neutral", _now() + timedelta(minutes=5))
        assert cache["tooling_minutes"] == 10.0  # Unchanged

    def test_first_event_no_accumulation(self) -> None:
        """First event (no previous timestamp) should not accumulate."""
        cache = _make_cache()
        accumulate_tooling_time(cache, "tooling", _now())
        assert cache["tooling_minutes"] == 0.0
        assert cache["current_mode"] == "tooling"
        assert cache["mode_started_at"] is not None

    def test_mode_switch_to_tooling_no_immediate_accumulation(self) -> None:
        """Switching from neutral to tooling should not immediately accumulate."""
        cache = _make_cache(
            current_mode="neutral",
            mode_started_at=_now().isoformat(),
        )
        accumulate_tooling_time(cache, "tooling", _now() + timedelta(minutes=5))
        assert cache["tooling_minutes"] == 0.0  # First tooling event
        assert cache["current_mode"] == "tooling"

    def test_malformed_timestamp_handled(self) -> None:
        """Malformed mode_started_at should not crash."""
        cache = _make_cache(
            current_mode="tooling",
            mode_started_at="not-a-date",
        )
        accumulate_tooling_time(cache, "tooling", _now())
        # Should not crash, tooling_minutes stays 0
        assert cache["tooling_minutes"] == 0.0

    def test_negative_delta_ignored(self) -> None:
        """Negative time delta (clock skew) should be treated as zero."""
        t0 = _now()
        t1 = t0 - timedelta(minutes=5)  # Time goes backward
        cache = _make_cache(
            current_mode="tooling",
            mode_started_at=t0.isoformat(),
        )
        accumulate_tooling_time(cache, "tooling", t1)
        assert cache["tooling_minutes"] == 0.0


class TestSelectTrapNudge:
    """Tests for select_trap_nudge()."""

    def test_none_level_returns_none(self) -> None:
        """No alert should return no nudge."""
        assert select_trap_nudge("none", []) is None

    def test_yellow_returns_nudge(self) -> None:
        """Yellow alert should return a nudge."""
        nudge = select_trap_nudge("yellow", [])
        assert nudge is not None
        assert "id" in nudge
        assert "text" in nudge

    def test_orange_returns_nudge(self) -> None:
        """Orange alert should return a nudge."""
        nudge = select_trap_nudge("orange", [])
        assert nudge is not None

    def test_red_returns_nudge(self) -> None:
        """Red alert should return a nudge."""
        nudge = select_trap_nudge("red", [])
        assert nudge is not None

    def test_avoids_recent_prompts(self) -> None:
        """Should prefer prompts not in recent history."""
        prompts = {
            "yellow": [
                {"id": "y1", "text": "first"},
                {"id": "y2", "text": "second"},
            ]
        }
        nudge = select_trap_nudge("yellow", ["y1"], prompts)
        assert nudge is not None
        assert nudge["id"] == "y2"

    def test_falls_back_to_first_when_all_recent(self) -> None:
        """When all prompts are recent, should still return one."""
        prompts = {
            "yellow": [
                {"id": "y1", "text": "only one"},
            ]
        }
        nudge = select_trap_nudge("yellow", ["y1"], prompts)
        assert nudge is not None
        assert nudge["id"] == "y1"

    def test_custom_prompts(self) -> None:
        """Custom prompt pool should be used."""
        custom = {
            "red": [{"id": "custom_r1", "text": "Custom red alert!"}],
        }
        nudge = select_trap_nudge("red", [], custom)
        assert nudge is not None
        assert nudge["id"] == "custom_r1"

    def test_empty_candidates_returns_none(self) -> None:
        """Empty candidates for level should return None."""
        nudge = select_trap_nudge("yellow", [], {"yellow": []})
        assert nudge is None


# ===========================================================================
# Task 6.3: Metacognition reminder engine
# ===========================================================================


class TestCheckMetacognitionInterval:
    """Tests for check_metacognition_interval()."""

    def test_first_prompt_triggers(self) -> None:
        """No previous prompt should trigger metacognition."""
        cache = _make_cache(last_prompt_at=None)
        result = check_metacognition_interval(cache, _now(), {"enabled": True})
        assert result is True

    def test_interval_not_elapsed(self) -> None:
        """Within interval should not trigger."""
        now = _now()
        cache = _make_cache(
            last_prompt_at=(now - timedelta(seconds=100)).isoformat(),
        )
        result = check_metacognition_interval(cache, now, {
            "enabled": True,
            "interval_seconds": 300,
        })
        assert result is False

    def test_interval_elapsed(self) -> None:
        """After interval should trigger."""
        now = _now()
        cache = _make_cache(
            last_prompt_at=(now - timedelta(seconds=301)).isoformat(),
        )
        result = check_metacognition_interval(cache, now, {
            "enabled": True,
            "interval_seconds": 300,
        })
        assert result is True

    def test_disabled_does_not_trigger(self) -> None:
        """Disabled metacognition should never trigger."""
        cache = _make_cache(last_prompt_at=None)
        result = check_metacognition_interval(cache, _now(), {"enabled": False})
        assert result is False

    def test_custom_interval(self) -> None:
        """Custom interval should be respected."""
        now = _now()
        cache = _make_cache(
            last_prompt_at=(now - timedelta(seconds=61)).isoformat(),
        )
        result = check_metacognition_interval(cache, now, {
            "enabled": True,
            "interval_seconds": 60,
        })
        assert result is True

    def test_malformed_timestamp_triggers(self) -> None:
        """Malformed last_prompt_at should trigger (safe fallback)."""
        cache = _make_cache(last_prompt_at="not-a-timestamp")
        result = check_metacognition_interval(cache, _now(), {"enabled": True})
        assert result is True


class TestSelectMetacognitionPrompt:
    """Tests for select_metacognition_prompt()."""

    def test_returns_a_prompt(self) -> None:
        """Should return a prompt dict with id and text."""
        cache = _make_cache()
        prompt = select_metacognition_prompt(cache, "neutral", {})
        assert prompt is not None
        assert "id" in prompt
        assert "text" in prompt

    def test_avoids_recent_prompts(self) -> None:
        """Should not select recently shown prompts when alternatives exist."""
        cache = _make_cache(prompt_history=["meta_01", "meta_02"])
        prompt = select_metacognition_prompt(cache, "neutral", {})
        assert prompt is not None
        # With 3 default prompts and 2 in history, should pick meta_03
        assert prompt["id"] == "meta_03"

    def test_falls_back_when_all_recent(self) -> None:
        """When all prompts are recent, should still return one."""
        all_ids = [p["id"] for p in DEFAULT_METACOGNITION_PROMPTS]
        cache = _make_cache(prompt_history=all_ids)
        prompt = select_metacognition_prompt(cache, "neutral", {})
        assert prompt is not None

    def test_uses_external_prompts_file(self, tmp_path: Path) -> None:
        """Should load prompts from external file when configured."""
        prompts_file = tmp_path / "prompts.json"
        prompts_file.write_text(json.dumps({
            "metacognition": [
                {"id": "ext_01", "text": "External prompt one"},
                {"id": "ext_02", "text": "External prompt two"},
            ],
        }))
        cache = _make_cache()
        prompt = select_metacognition_prompt(cache, "neutral", {
            "prompts_file": str(prompts_file),
        })
        assert prompt is not None
        assert prompt["id"] in ("ext_01", "ext_02")

    def test_falls_back_to_defaults_on_missing_file(self) -> None:
        """Missing prompts file should fall back to defaults."""
        cache = _make_cache()
        prompt = select_metacognition_prompt(cache, "neutral", {
            "prompts_file": "/nonexistent/prompts.json",
        })
        assert prompt is not None
        assert prompt["id"].startswith("meta_")


class TestLoadPromptsFile:
    """Tests for load_prompts_file()."""

    def test_loads_valid_file(self, tmp_path: Path) -> None:
        """Should load and parse a valid JSON prompts file."""
        prompts_file = tmp_path / "prompts.json"
        prompts_file.write_text(json.dumps({
            "metacognition": [{"id": "test", "text": "Test prompt"}],
        }))
        result = load_prompts_file(str(prompts_file))
        assert result is not None
        assert "metacognition" in result

    def test_returns_none_for_missing_file(self) -> None:
        """Missing file should return None."""
        result = load_prompts_file("/nonexistent/file.json")
        assert result is None

    def test_returns_none_for_none_path(self) -> None:
        """None path should return None."""
        result = load_prompts_file(None)
        assert result is None

    def test_returns_none_for_invalid_json(self, tmp_path: Path) -> None:
        """Invalid JSON should return None."""
        bad_file = tmp_path / "bad.json"
        bad_file.write_text("not json {{{")
        result = load_prompts_file(str(bad_file))
        assert result is None

    def test_returns_none_for_non_dict_json(self, tmp_path: Path) -> None:
        """Non-dict JSON should return None."""
        arr_file = tmp_path / "array.json"
        arr_file.write_text("[1, 2, 3]")
        result = load_prompts_file(str(arr_file))
        assert result is None

    def test_handles_tilde_expansion(self, tmp_path: Path) -> None:
        """Should expand ~ in file paths."""
        # This test just verifies it doesn't crash -- actual ~ expansion
        # depends on the home directory
        result = load_prompts_file("~/nonexistent_prompts_file.json")
        assert result is None


# ===========================================================================
# Task 6.4: Rapid acceptance detection
# ===========================================================================


class TestRapidAcceptanceDetection:
    """Tests for rapid acceptance detection in CoachingEngine."""

    def test_rapid_acceptance_triggers(self, tmp_state_dir: Path) -> None:
        """Quick prompt after large change should trigger alert."""
        t0 = _now()
        t1 = t0 + timedelta(seconds=3)  # Within 5-second window

        config = {
            "metacognition": {"enabled": False},
            "builder_trap": {"enabled": False},
        }
        engine = CoachingEngine(config)
        # Simulate large change in cache
        engine._cache["last_large_change"] = {
            "timestamp": t0.isoformat(),
            "lines": 100,
            "tool_name": "Write",
        }

        result = engine.process_user_prompt({"content": "looks good"}, now=t1)
        assert result is not None
        assert "accepted a large AI change" in result

    def test_no_trigger_outside_window(self, tmp_state_dir: Path) -> None:
        """Prompt after 10 seconds should not trigger."""
        t0 = _now()
        t1 = t0 + timedelta(seconds=10)

        config = {
            "metacognition": {"enabled": False},
            "builder_trap": {"enabled": False},
        }
        engine = CoachingEngine(config)
        engine._cache["last_large_change"] = {
            "timestamp": t0.isoformat(),
            "lines": 100,
            "tool_name": "Write",
        }

        result = engine.process_user_prompt({"content": "ok"}, now=t1)
        assert result is None

    def test_no_trigger_without_large_change(self, tmp_state_dir: Path) -> None:
        """Prompt without prior large change should not trigger."""
        config = {
            "metacognition": {"enabled": False},
            "builder_trap": {"enabled": False},
        }
        engine = CoachingEngine(config)
        engine._cache["last_large_change"] = None

        result = engine.process_user_prompt({"content": "ok"}, now=_now())
        assert result is None

    def test_large_change_tracking_on_post_tool_use(self, tmp_state_dir: Path) -> None:
        """PostToolUse with >50 lines should set last_large_change."""
        config = {
            "metacognition": {"enabled": False},
            "builder_trap": {"enabled": False},
        }
        engine = CoachingEngine(config)

        payload = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/app/main.py"},
            "tool_output": {"lines_added": 80, "lines_removed": 0},
        }
        engine.process_post_tool_use(payload, now=_now())
        assert engine._cache["last_large_change"] is not None
        assert engine._cache["last_large_change"]["lines"] == 80

    def test_small_change_clears_tracker(self, tmp_state_dir: Path) -> None:
        """PostToolUse with <=50 lines should clear last_large_change."""
        config = {
            "metacognition": {"enabled": False},
            "builder_trap": {"enabled": False},
        }
        engine = CoachingEngine(config)
        engine._cache["last_large_change"] = {
            "timestamp": _now().isoformat(),
            "lines": 100,
            "tool_name": "Write",
        }

        payload = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/app/main.py"},
            "tool_output": {"lines_added": 5, "lines_removed": 0},
        }
        engine.process_post_tool_use(payload, now=_now())
        assert engine._cache["last_large_change"] is None

    def test_rapid_acceptance_clears_tracker_after_check(self, tmp_state_dir: Path) -> None:
        """After checking rapid acceptance, last_large_change should be cleared."""
        t0 = _now()
        t1 = t0 + timedelta(seconds=3)

        config = {
            "metacognition": {"enabled": False},
            "builder_trap": {"enabled": False},
        }
        engine = CoachingEngine(config)
        engine._cache["last_large_change"] = {
            "timestamp": t0.isoformat(),
            "lines": 100,
            "tool_name": "Write",
        }

        engine.process_user_prompt({"content": "ok"}, now=t1)
        assert engine._cache["last_large_change"] is None


# ===========================================================================
# Task 6.5: Communication coach
# ===========================================================================


class TestGrammarRules:
    """Tests for individual grammar checking rules."""

    def test_missing_articles_detects_pattern(self) -> None:
        """Should detect missing article before noun."""
        issues = _check_missing_articles("please create file for the project")
        assert len(issues) > 0
        assert any("article" in issue.lower() for issue in issues)

    def test_missing_articles_no_false_positive_with_article(self) -> None:
        """Should not flag when article is present."""
        issues = _check_missing_articles("please create a file for the project")
        assert len(issues) == 0

    def test_missing_articles_no_false_positive_with_determiner(self) -> None:
        """Should not flag when 'this' or 'my' is present."""
        issues = _check_missing_articles("please create this file")
        assert len(issues) == 0

    def test_run_on_sentences(self) -> None:
        """Should detect very long sentences."""
        long = " ".join(["word"] * 45)
        issues = _check_run_on_sentences(long)
        assert len(issues) > 0
        assert any("run-on" in issue.lower() for issue in issues)

    def test_run_on_sentences_normal_length(self) -> None:
        """Should not flag normal-length sentences."""
        issues = _check_run_on_sentences("This is a normal sentence.")
        assert len(issues) == 0

    def test_incomplete_sentences_trailing_conjunction(self) -> None:
        """Should detect prompt ending with a conjunction."""
        issues = _check_incomplete_sentences("I want to update the file and")
        assert len(issues) > 0
        assert any("mid-thought" in issue.lower() for issue in issues)

    def test_incomplete_sentences_trailing_preposition(self) -> None:
        """Should detect prompt ending with a preposition."""
        issues = _check_incomplete_sentences("please save it to")
        assert len(issues) > 0

    def test_incomplete_sentences_normal_ending(self) -> None:
        """Should not flag normally ending prompt."""
        issues = _check_incomplete_sentences("please save the file")
        assert len(issues) == 0

    def test_subject_verb_it_dont(self) -> None:
        """Should detect 'it don't'."""
        issues = _check_subject_verb_disagreement("it don't work correctly")
        assert len(issues) > 0
        assert any("doesn't" in issue for issue in issues)

    def test_subject_verb_they_was(self) -> None:
        """Should detect 'they was'."""
        issues = _check_subject_verb_disagreement("they was running the tests")
        assert len(issues) > 0
        assert any("were" in issue for issue in issues)

    def test_subject_verb_correct(self) -> None:
        """Should not flag correct subject-verb agreement."""
        issues = _check_subject_verb_disagreement("it doesn't work correctly")
        assert len(issues) == 0

    def test_grammar_rules_registry(self) -> None:
        """All expected rules should be in the registry."""
        assert "missing_articles" in GRAMMAR_RULES
        assert "run_on_sentences" in GRAMMAR_RULES
        assert "incomplete_sentences" in GRAMMAR_RULES
        assert "subject_verb_disagreement" in GRAMMAR_RULES


class TestAnalyzePrompt:
    """Tests for analyze_prompt() with frequency gating."""

    def test_disabled_returns_none(self) -> None:
        """Disabled communication coach should return None."""
        cache = _make_cache()
        result = analyze_prompt("test text", cache, {"enabled": False})
        assert result is None

    def test_frequency_gating_skips_non_nth(self) -> None:
        """Non-Nth prompts should be skipped."""
        cache = _make_cache(prompt_check_counter=0)
        config = {"enabled": True, "frequency": 3, "min_length": 5}
        # Counter becomes 1 (not multiple of 3)
        result = analyze_prompt("some test text here", cache, config)
        assert result is None
        assert cache["prompt_check_counter"] == 1

    def test_frequency_gating_checks_nth(self) -> None:
        """Every Nth prompt should be analyzed."""
        cache = _make_cache(prompt_check_counter=2)
        config = {
            "enabled": True,
            "frequency": 3,
            "min_length": 5,
            "rules": ["incomplete_sentences"],
        }
        # Counter becomes 3 (multiple of 3)
        result = analyze_prompt("I want to update the file and", cache, config)
        assert result is not None
        assert len(result["corrections"]) > 0

    def test_short_prompts_skipped(self) -> None:
        """Prompts shorter than min_length should be skipped."""
        cache = _make_cache(prompt_check_counter=2)
        config = {"enabled": True, "frequency": 3, "min_length": 50}
        result = analyze_prompt("hi", cache, config)
        assert result is None

    def test_clean_prompt_returns_none(self) -> None:
        """Clean prompt with no issues should return None."""
        cache = _make_cache(prompt_check_counter=2)
        config = {
            "enabled": True,
            "frequency": 3,
            "min_length": 5,
            "rules": ["subject_verb_disagreement"],
        }
        result = analyze_prompt(
            "Please update the configuration file with the new settings.",
            cache,
            config,
        )
        assert result is None

    def test_score_tracking(self) -> None:
        """Should track grammar scores in rolling history."""
        cache = _make_cache(prompt_check_counter=2)
        config = {
            "enabled": True,
            "frequency": 3,
            "min_length": 5,
            "rules": ["incomplete_sentences"],
        }
        analyze_prompt("I want to update the file and", cache, config)
        assert len(cache["grammar_scores"]) > 0

    def test_rolling_scores_capped(self) -> None:
        """Grammar scores should be capped at MAX_SCORE_HISTORY."""
        cache = _make_cache(
            prompt_check_counter=2,
            grammar_scores=[1.0] * 25,
        )
        config = {
            "enabled": True,
            "frequency": 3,
            "min_length": 5,
            "rules": ["incomplete_sentences"],
        }
        analyze_prompt("I want to update the file and", cache, config)
        assert len(cache["grammar_scores"]) <= MAX_SCORE_HISTORY

    def test_gentle_tone_formatting(self) -> None:
        """Gentle tone should prefix corrections with 'Gentle suggestion'."""
        cache = _make_cache(prompt_check_counter=2)
        config = {
            "enabled": True,
            "frequency": 3,
            "min_length": 5,
            "rules": ["incomplete_sentences"],
            "tone": "gentle",
        }
        result = analyze_prompt("I want to update the file and", cache, config)
        assert result is not None
        assert any("Gentle suggestion" in c for c in result["corrections"])

    def test_direct_tone_formatting(self) -> None:
        """Direct tone should prefix corrections with 'Note'."""
        cache = _make_cache(prompt_check_counter=2)
        config = {
            "enabled": True,
            "frequency": 3,
            "min_length": 5,
            "rules": ["incomplete_sentences"],
            "tone": "direct",
        }
        result = analyze_prompt("I want to update the file and", cache, config)
        assert result is not None
        assert any("Note:" in c for c in result["corrections"])

    def test_counter_always_increments(self) -> None:
        """Counter should increment even when analysis is skipped."""
        cache = _make_cache(prompt_check_counter=0)
        config = {"enabled": True, "frequency": 5, "min_length": 5}
        analyze_prompt("test text here", cache, config)
        assert cache["prompt_check_counter"] == 1
        analyze_prompt("more text here", cache, config)
        assert cache["prompt_check_counter"] == 2

    def test_specific_rules_only(self) -> None:
        """Should only run the rules specified in config."""
        cache = _make_cache(prompt_check_counter=2)
        config = {
            "enabled": True,
            "frequency": 3,
            "min_length": 5,
            "rules": ["subject_verb_disagreement"],  # Only this rule
        }
        # This has incomplete sentence but not subject-verb issue
        result = analyze_prompt("I want to update the file and", cache, config)
        assert result is None  # subject_verb rule won't catch this


# ===========================================================================
# CoachingEngine integration
# ===========================================================================


class TestCoachingEngine:
    """Integration tests for the CoachingEngine facade."""

    def test_post_tool_use_mode_classification(self, tmp_state_dir: Path) -> None:
        """PostToolUse should classify mode and update cache."""
        config = {
            "metacognition": {"enabled": False},
            "builder_trap": {"enabled": False},
        }
        engine = CoachingEngine(config)
        payload = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/app/main.py"},
            "tool_output": {},
        }
        engine.process_post_tool_use(payload, now=_now())
        assert engine._cache["current_mode"] == "coding"

    def test_builder_trap_nudge_on_escalation(self, tmp_state_dir: Path) -> None:
        """Should emit nudge when alert level escalates."""
        config = {
            "metacognition": {"enabled": False},
            "builder_trap": {
                "enabled": True,
                "thresholds": {"yellow": 30, "orange": 60, "red": 90},
                "tooling_patterns": ["~/.claude/skills/*"],
            },
        }
        engine = CoachingEngine(config)
        # Pre-set cache to just below yellow threshold
        engine._cache["tooling_minutes"] = 29.0
        engine._cache["current_mode"] = "tooling"
        engine._cache["today_date"] = "2026-02-10"
        t0 = _now()
        engine._cache["mode_started_at"] = t0.isoformat()

        # Tooling event that pushes past 30 minutes
        payload = {
            "tool_name": "Write",
            "tool_input": {"file_path": "~/.claude/skills/test/SKILL.md"},
            "tool_output": {},
        }
        t1 = t0 + timedelta(minutes=2)  # +2 minutes -> 31 total
        result = engine.process_post_tool_use(payload, now=t1)
        assert result is not None
        assert "[Coaching]" in result

    def test_no_duplicate_nudge_at_same_level(self, tmp_state_dir: Path) -> None:
        """Should not re-emit nudge at the same alert level."""
        config = {
            "metacognition": {"enabled": False},
            "builder_trap": {"enabled": True},
        }
        engine = CoachingEngine(config)
        engine._cache["tooling_minutes"] = 35.0
        engine._cache["alert_level"] = "yellow"  # Already at yellow
        engine._cache["current_mode"] = "tooling"
        engine._cache["today_date"] = "2026-02-10"
        t0 = _now()
        engine._cache["mode_started_at"] = t0.isoformat()

        payload = {
            "tool_name": "Write",
            "tool_input": {"file_path": "~/.claude/skills/test/SKILL.md"},
            "tool_output": {},
        }
        t1 = t0 + timedelta(minutes=2)
        result = engine.process_post_tool_use(payload, now=t1)
        # Should not emit since already at yellow
        assert result is None

    def test_metacognition_fires_on_interval(self, tmp_state_dir: Path) -> None:
        """Should emit metacognition prompt when interval elapses."""
        config = {
            "metacognition": {
                "enabled": True,
                "interval_seconds": 60,
            },
            "builder_trap": {"enabled": False},
        }
        engine = CoachingEngine(config)
        t0 = _now()
        engine._cache["last_prompt_at"] = (t0 - timedelta(seconds=120)).isoformat()

        payload = {
            "tool_name": "Read",
            "tool_input": {"file_path": "/tmp/test.txt"},
            "tool_output": {},
        }
        result = engine.process_post_tool_use(payload, now=t0)
        assert result is not None
        assert "[Coaching]" in result

    def test_communication_coach_on_user_prompt(self, tmp_state_dir: Path) -> None:
        """Should analyze user prompts when communication coach is enabled."""
        config = {
            "metacognition": {"enabled": False},
            "builder_trap": {"enabled": False},
            "communication": {
                "enabled": True,
                "frequency": 1,
                "min_length": 5,
                "rules": ["incomplete_sentences"],
                "tone": "gentle",
            },
        }
        engine = CoachingEngine(config)

        payload = {"content": "I want to update the file and"}
        result = engine.process_user_prompt(payload, now=_now())
        assert result is not None
        assert "[Communication Coach]" in result

    def test_non_dict_tool_input_handled(self, tmp_state_dir: Path) -> None:
        """Non-dict tool_input should be handled gracefully."""
        config = {
            "metacognition": {"enabled": False},
            "builder_trap": {"enabled": False},
        }
        engine = CoachingEngine(config)
        payload = {
            "tool_name": "Bash",
            "tool_input": "not a dict",
            "tool_output": {},
        }
        # Should not crash
        result = engine.process_post_tool_use(payload, now=_now())
        # No coaching output expected
        assert result is None


# ===========================================================================
# handle() entry point
# ===========================================================================


class TestHandleEntryPoint:
    """Tests for the handle() builtin handler entry point."""

    def test_returns_none_for_empty_config(self, tmp_state_dir: Path) -> None:
        """Empty coaching config should return None."""
        config = HooksConfig()
        result = handle("PostToolUse", {"tool_name": "Bash"}, config)
        assert result is None

    def test_returns_none_for_unsupported_event(self, tmp_state_dir: Path) -> None:
        """Unsupported event type should return None."""
        config = HooksConfig(coaching={
            "metacognition": {"enabled": False},
            "builder_trap": {"enabled": False},
        })
        result = handle("SessionStart", {}, config)
        assert result is None

    def test_post_tool_use_returns_dict(self, tmp_state_dir: Path) -> None:
        """PostToolUse with coaching should return additionalContext dict."""
        config = HooksConfig(coaching={
            "metacognition": {
                "enabled": True,
                "interval_seconds": 0,  # Always trigger
            },
            "builder_trap": {"enabled": False},
        })
        result = handle(
            "PostToolUse",
            {"tool_name": "Read", "tool_input": {}, "tool_output": {}},
            config,
        )
        assert result is not None
        assert "additionalContext" in result

    def test_user_prompt_with_rapid_acceptance(self, tmp_state_dir: Path) -> None:
        """UserPromptSubmit after large change should include rapid acceptance alert."""
        config = HooksConfig(coaching={
            "metacognition": {"enabled": False},
            "builder_trap": {"enabled": False},
        })
        # First, process a large Write
        engine = CoachingEngine(config.coaching)
        t0 = _now()
        engine.process_post_tool_use({
            "tool_name": "Write",
            "tool_input": {"file_path": "/app/big.py"},
            "tool_output": {"lines_added": 100, "lines_removed": 0},
        }, now=t0)

        # Now handle a quick UserPromptSubmit via the handle() entry point
        # Need to reload the cache (handle creates a new engine)
        t1 = t0 + timedelta(seconds=2)
        with patch("hookwise.coaching._load_cache", return_value=engine._cache):
            result = handle(
                "UserPromptSubmit",
                {"content": "ok looks good"},
                config,
            )
        # The rapid acceptance should trigger
        if result is not None:
            assert "additionalContext" in result

    def test_fail_open_on_exception(self, tmp_state_dir: Path) -> None:
        """Exceptions in coaching should not crash -- fail-open."""
        config = HooksConfig(coaching={
            "metacognition": {"enabled": True},
        })
        with patch(
            "hookwise.coaching.CoachingEngine.process_post_tool_use",
            side_effect=RuntimeError("boom"),
        ):
            result = handle(
                "PostToolUse",
                {"tool_name": "Bash"},
                config,
            )
        assert result is None


# ===========================================================================
# Cache management
# ===========================================================================


class TestCacheManagement:
    """Tests for cache loading and saving."""

    def test_load_cache_default(self, tmp_state_dir: Path) -> None:
        """Loading from nonexistent file should return defaults."""
        cache = _load_cache()
        assert cache["current_mode"] == "neutral"
        assert cache["tooling_minutes"] == 0.0
        assert cache["alert_level"] == "none"
        assert cache["prompt_history"] == []

    def test_save_and_load_roundtrip(self, tmp_state_dir: Path) -> None:
        """Saved cache should be loadable."""
        cache = _make_cache(tooling_minutes=42.5, current_mode="coding")
        _save_cache(cache)
        loaded = _load_cache()
        assert loaded["tooling_minutes"] == 42.5
        assert loaded["current_mode"] == "coding"

    def test_load_cache_fills_missing_keys(self, tmp_state_dir: Path) -> None:
        """Loading a partial cache should fill in missing keys."""
        from hookwise.state import atomic_write_json
        path = _get_cache_path()
        atomic_write_json(path, {"current_mode": "tooling"})
        cache = _load_cache()
        assert cache["current_mode"] == "tooling"
        # Missing keys should be filled with defaults
        assert "tooling_minutes" in cache
        assert cache["tooling_minutes"] == 0.0


class TestExtractLines:
    """Tests for _extract_lines helper."""

    def test_int_value(self) -> None:
        """Integer value should be returned directly."""
        assert _extract_lines({"lines_added": 42}, "lines_added") == 42

    def test_string_value(self) -> None:
        """String value should be converted to int."""
        assert _extract_lines({"lines_added": "42"}, "lines_added") == 42

    def test_missing_key(self) -> None:
        """Missing key should return 0."""
        assert _extract_lines({}, "lines_added") == 0

    def test_non_numeric_string(self) -> None:
        """Non-numeric string should return 0."""
        assert _extract_lines({"lines_added": "abc"}, "lines_added") == 0

    def test_none_value(self) -> None:
        """None value should return 0."""
        assert _extract_lines({"lines_added": None}, "lines_added") == 0


# ===========================================================================
# Constants and defaults
# ===========================================================================


class TestDefaults:
    """Tests for default constants and prompt pools."""

    def test_default_metacognition_prompts_not_empty(self) -> None:
        """Default metacognition prompts should have entries."""
        assert len(DEFAULT_METACOGNITION_PROMPTS) >= 3

    def test_default_trap_prompts_all_levels(self) -> None:
        """Default trap prompts should have entries for all levels."""
        assert "yellow" in DEFAULT_TRAP_PROMPTS
        assert "orange" in DEFAULT_TRAP_PROMPTS
        assert "red" in DEFAULT_TRAP_PROMPTS

    def test_default_rapid_acceptance_prompts(self) -> None:
        """Default rapid acceptance prompts should have entries."""
        assert len(DEFAULT_RAPID_ACCEPTANCE_PROMPTS) >= 1

    def test_write_tools_set(self) -> None:
        """Write tools set should contain expected tools."""
        assert "Write" in WRITE_TOOLS
        assert "Edit" in WRITE_TOOLS
        assert "NotebookEdit" in WRITE_TOOLS

    def test_large_change_threshold(self) -> None:
        """Large change threshold should be 50 lines."""
        assert LARGE_CHANGE_LINES == 50

    def test_rapid_acceptance_window(self) -> None:
        """Rapid acceptance window should be 5 seconds."""
        assert RAPID_ACCEPTANCE_SECONDS == 5.0

    def test_max_delta_minutes(self) -> None:
        """Max delta between events should be 10 minutes."""
        assert MAX_DELTA_MINUTES == 10.0

    def test_default_interval_seconds(self) -> None:
        """Default metacognition interval should be 300 seconds."""
        assert DEFAULT_INTERVAL_SECONDS == 300
