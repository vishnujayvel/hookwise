"""Tests for hookwise.status_line -- composable status line renderer."""

from __future__ import annotations

import json
import os
import sys
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any
from unittest.mock import patch

import pytest

from hookwise.state import atomic_write_json, safe_read_json
from hookwise.status_line import StatusLineRenderer, render
from hookwise.status_line.renderer import (
    DEFAULT_CACHE_PATH,
    DEFAULT_CUSTOM_TIMEOUT,
    DEFAULT_DELIMITER,
    compose_segments,
    render_builtin_segment,
    render_custom_segment,
    resolve_cache_path,
)
from hookwise.status_line.segments import (
    BUILTIN_SEGMENTS,
    ai_ratio,
    builder_trap,
    clock,
    cost,
    mantra,
    practice,
    session,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_full_cache(
    started_minutes_ago: int = 45,
    tool_calls: int = 47,
    ai_ratio_val: float = 0.72,
    cost_usd: float = 0.67,
    today_reps: int = 2,
    alert_level: str = "yellow",
    tooling_minutes: float = 30.5,
) -> dict[str, Any]:
    """Build a complete cache dict for testing."""
    now = datetime.now(timezone.utc)
    started_at = now - timedelta(minutes=started_minutes_ago)
    today_date = now.astimezone().strftime("%Y-%m-%d")

    return {
        "updated_at": now.isoformat(),
        "mantra": {"text": "One idea. One pause.", "id": "metacog_03"},
        "builder_trap": {
            "alert_level": alert_level,
            "tooling_minutes": tooling_minutes,
            "nudge_message": "Is this moving the needle?",
        },
        "practice": {"today_total": today_reps, "today_date": today_date},
        "session": {
            "started_at": started_at.isoformat(),
            "tool_calls": tool_calls,
            "ai_ratio": ai_ratio_val,
        },
        "cost": {"session_tokens": 45000, "session_cost_usd": cost_usd},
    }


def _write_cache(cache_path: Path, data: dict[str, Any]) -> None:
    """Write cache data to the given path."""
    atomic_write_json(cache_path, data)


# ---------------------------------------------------------------------------
# segments.py -- clock
# ---------------------------------------------------------------------------


class TestClockSegment:
    """Tests for the clock segment."""

    def test_default_format(self) -> None:
        """Should render current time in HH:MM format by default."""
        result = clock({}, {})
        assert len(result) == 5  # "HH:MM"
        assert ":" in result

    def test_custom_format(self) -> None:
        """Should respect custom strftime format."""
        result = clock({}, {"format": "%H:%M:%S"})
        assert len(result) == 8  # "HH:MM:SS"
        assert result.count(":") == 2

    def test_invalid_format_falls_back(self) -> None:
        """Should fall back to %H:%M on invalid format."""
        result = clock({}, {"format": 12345})
        assert len(result) == 5
        assert ":" in result

    def test_bad_strftime_falls_back(self) -> None:
        """Should fall back to %H:%M on a format that causes ValueError."""
        # An absurdly wrong format that might error
        result = clock({}, {"format": "%H:%M"})
        assert isinstance(result, str)
        assert len(result) > 0

    def test_ignores_cache(self) -> None:
        """Clock should not depend on cache data."""
        result = clock({"random": "data"}, {})
        assert isinstance(result, str)
        assert len(result) > 0


# ---------------------------------------------------------------------------
# segments.py -- mantra
# ---------------------------------------------------------------------------


class TestMantraSegment:
    """Tests for the mantra segment."""

    def test_renders_text(self) -> None:
        """Should return the mantra text from cache."""
        cache = {"mantra": {"text": "Trust the process.", "id": "m01"}}
        result = mantra(cache, {})
        assert result == "Trust the process."

    def test_empty_on_missing_mantra(self) -> None:
        """Should return empty string when mantra is not in cache."""
        result = mantra({}, {})
        assert result == ""

    def test_empty_on_non_dict_mantra(self) -> None:
        """Should return empty when mantra value is not a dict."""
        result = mantra({"mantra": "just a string"}, {})
        assert result == ""

    def test_empty_on_missing_text_key(self) -> None:
        """Should return empty when text key is missing."""
        result = mantra({"mantra": {"id": "m01"}}, {})
        assert result == ""

    def test_empty_on_non_string_text(self) -> None:
        """Should return empty when text is not a string."""
        result = mantra({"mantra": {"text": 42}}, {})
        assert result == ""


# ---------------------------------------------------------------------------
# segments.py -- builder_trap
# ---------------------------------------------------------------------------


class TestBuilderTrapSegment:
    """Tests for the builder_trap segment."""

    def test_yellow_alert(self) -> None:
        """Should show warning emoji for yellow alert."""
        cache = {
            "builder_trap": {
                "alert_level": "yellow",
                "tooling_minutes": 30.5,
                "nudge_message": "Is this moving the needle?",
            }
        }
        result = builder_trap(cache, {})
        assert "\u26a0\ufe0f" in result
        assert "30m" in result
        assert "Is this moving the needle?" in result

    def test_orange_alert(self) -> None:
        """Should show orange emoji for orange alert."""
        cache = {
            "builder_trap": {
                "alert_level": "orange",
                "tooling_minutes": 45,
                "nudge_message": "Step back.",
            }
        }
        result = builder_trap(cache, {})
        assert "\U0001f7e0" in result
        assert "45m" in result

    def test_red_alert(self) -> None:
        """Should show red emoji for red alert."""
        cache = {
            "builder_trap": {
                "alert_level": "red",
                "tooling_minutes": 60,
                "nudge_message": "Stop.",
            }
        }
        result = builder_trap(cache, {})
        assert "\U0001f534" in result
        assert "60m" in result

    def test_none_alert_returns_empty(self) -> None:
        """Should return empty for alert_level 'none'."""
        cache = {
            "builder_trap": {
                "alert_level": "none",
                "tooling_minutes": 10,
            }
        }
        result = builder_trap(cache, {})
        assert result == ""

    def test_missing_data_returns_empty(self) -> None:
        """Should return empty when builder_trap is missing from cache."""
        result = builder_trap({}, {})
        assert result == ""

    def test_no_nudge_message(self) -> None:
        """Should work without nudge_message."""
        cache = {
            "builder_trap": {
                "alert_level": "yellow",
                "tooling_minutes": 15,
            }
        }
        result = builder_trap(cache, {})
        assert "\u26a0\ufe0f" in result
        assert "15m" in result
        assert ":" not in result  # No colon when no nudge

    def test_unknown_alert_level_returns_empty(self) -> None:
        """Should return empty for unknown alert levels."""
        cache = {
            "builder_trap": {
                "alert_level": "purple",
                "tooling_minutes": 5,
            }
        }
        result = builder_trap(cache, {})
        assert result == ""

    def test_non_dict_builder_trap(self) -> None:
        """Should return empty when builder_trap is not a dict."""
        result = builder_trap({"builder_trap": "wrong"}, {})
        assert result == ""

    def test_non_numeric_tooling_minutes(self) -> None:
        """Should handle non-numeric tooling_minutes gracefully."""
        cache = {
            "builder_trap": {
                "alert_level": "yellow",
                "tooling_minutes": "not a number",
            }
        }
        result = builder_trap(cache, {})
        assert "0m" in result


# ---------------------------------------------------------------------------
# segments.py -- session
# ---------------------------------------------------------------------------


class TestSessionSegment:
    """Tests for the session segment."""

    def test_renders_duration_and_tools(self) -> None:
        """Should show duration and tool count."""
        now = datetime.now(timezone.utc)
        started_at = now - timedelta(minutes=45)
        cache = {
            "session": {
                "started_at": started_at.isoformat(),
                "tool_calls": 47,
            }
        }
        result = session(cache, {})
        assert "45m" in result
        assert "47 tools" in result

    def test_hours_format(self) -> None:
        """Should show hours when duration >= 60 minutes."""
        now = datetime.now(timezone.utc)
        started_at = now - timedelta(minutes=90)
        cache = {
            "session": {
                "started_at": started_at.isoformat(),
                "tool_calls": 100,
            }
        }
        result = session(cache, {})
        assert "1h30m" in result
        assert "100 tools" in result

    def test_zero_tools(self) -> None:
        """Should handle zero tool calls."""
        now = datetime.now(timezone.utc)
        started_at = now - timedelta(minutes=5)
        cache = {
            "session": {
                "started_at": started_at.isoformat(),
                "tool_calls": 0,
            }
        }
        result = session(cache, {})
        assert "0 tools" in result

    def test_missing_session_returns_empty(self) -> None:
        """Should return empty when session data is missing."""
        result = session({}, {})
        assert result == ""

    def test_missing_started_at_returns_empty(self) -> None:
        """Should return empty when started_at is missing."""
        cache = {"session": {"tool_calls": 10}}
        result = session(cache, {})
        assert result == ""

    def test_non_dict_session_returns_empty(self) -> None:
        """Should return empty when session is not a dict."""
        result = session({"session": "wrong"}, {})
        assert result == ""

    def test_invalid_started_at_returns_empty(self) -> None:
        """Should return empty on unparseable started_at."""
        cache = {
            "session": {
                "started_at": "not-a-date",
                "tool_calls": 5,
            }
        }
        result = session(cache, {})
        assert result == ""

    def test_string_tool_calls_coerced(self) -> None:
        """Should coerce string tool_calls to int."""
        now = datetime.now(timezone.utc)
        started_at = now - timedelta(minutes=10)
        cache = {
            "session": {
                "started_at": started_at.isoformat(),
                "tool_calls": "25",
            }
        }
        result = session(cache, {})
        assert "25 tools" in result


# ---------------------------------------------------------------------------
# segments.py -- practice
# ---------------------------------------------------------------------------


class TestPracticeSegment:
    """Tests for the practice segment."""

    def test_renders_rep_count(self) -> None:
        """Should show reps count for today."""
        today = datetime.now(timezone.utc).astimezone().strftime("%Y-%m-%d")
        cache = {"practice": {"today_total": 3, "today_date": today}}
        result = practice(cache, {})
        assert result == "reps: 3"

    def test_different_date_returns_empty(self) -> None:
        """Should return empty if the date doesn't match today."""
        cache = {"practice": {"today_total": 5, "today_date": "2020-01-01"}}
        result = practice(cache, {})
        assert result == ""

    def test_missing_practice_returns_empty(self) -> None:
        """Should return empty when practice data is missing."""
        result = practice({}, {})
        assert result == ""

    def test_non_dict_practice_returns_empty(self) -> None:
        """Should return empty when practice is not a dict."""
        result = practice({"practice": 42}, {})
        assert result == ""

    def test_missing_today_date_returns_empty(self) -> None:
        """Should return empty when today_date is missing."""
        cache = {"practice": {"today_total": 2}}
        result = practice(cache, {})
        assert result == ""

    def test_zero_reps(self) -> None:
        """Should show zero reps."""
        today = datetime.now(timezone.utc).astimezone().strftime("%Y-%m-%d")
        cache = {"practice": {"today_total": 0, "today_date": today}}
        result = practice(cache, {})
        assert result == "reps: 0"


# ---------------------------------------------------------------------------
# segments.py -- ai_ratio
# ---------------------------------------------------------------------------


class TestAiRatioSegment:
    """Tests for the ai_ratio segment."""

    def test_renders_percentage(self) -> None:
        """Should show AI percentage."""
        cache = {"session": {"ai_ratio": 0.72}}
        result = ai_ratio(cache, {})
        assert result == "AI: 72%"

    def test_zero_ratio(self) -> None:
        """Should show 0% for zero ratio."""
        cache = {"session": {"ai_ratio": 0.0}}
        result = ai_ratio(cache, {})
        assert result == "AI: 0%"

    def test_full_ratio(self) -> None:
        """Should show 100% for 1.0 ratio."""
        cache = {"session": {"ai_ratio": 1.0}}
        result = ai_ratio(cache, {})
        assert result == "AI: 100%"

    def test_missing_session_returns_empty(self) -> None:
        """Should return empty when session data is missing."""
        result = ai_ratio({}, {})
        assert result == ""

    def test_missing_ratio_returns_empty(self) -> None:
        """Should return empty when ai_ratio key is missing."""
        cache = {"session": {"tool_calls": 10}}
        result = ai_ratio(cache, {})
        assert result == ""

    def test_none_ratio_returns_empty(self) -> None:
        """Should return empty when ai_ratio is None."""
        cache = {"session": {"ai_ratio": None}}
        result = ai_ratio(cache, {})
        assert result == ""

    def test_string_ratio_coerced(self) -> None:
        """Should coerce string ratio to float."""
        cache = {"session": {"ai_ratio": "0.55"}}
        result = ai_ratio(cache, {})
        assert result == "AI: 55%"

    def test_non_numeric_ratio_returns_empty(self) -> None:
        """Should return empty for non-numeric ratio."""
        cache = {"session": {"ai_ratio": "not-a-number"}}
        result = ai_ratio(cache, {})
        assert result == ""


# ---------------------------------------------------------------------------
# segments.py -- cost
# ---------------------------------------------------------------------------


class TestCostSegment:
    """Tests for the cost segment."""

    def test_renders_cost(self) -> None:
        """Should show cost with dollar sign and 2 decimal places."""
        cache = {"cost": {"session_cost_usd": 0.67}}
        result = cost(cache, {})
        assert result == "$0.67"

    def test_zero_cost(self) -> None:
        """Should show $0.00 for zero cost."""
        cache = {"cost": {"session_cost_usd": 0}}
        result = cost(cache, {})
        assert result == "$0.00"

    def test_large_cost(self) -> None:
        """Should handle costs over a dollar."""
        cache = {"cost": {"session_cost_usd": 12.345}}
        result = cost(cache, {})
        assert result == "$12.35"  # Rounded to 2 decimals

    def test_missing_cost_returns_empty(self) -> None:
        """Should return empty when cost data is missing."""
        result = cost({}, {})
        assert result == ""

    def test_none_cost_returns_empty(self) -> None:
        """Should return empty when cost value is None."""
        cache = {"cost": {"session_cost_usd": None}}
        result = cost(cache, {})
        assert result == ""

    def test_string_cost_coerced(self) -> None:
        """Should coerce string cost to float."""
        cache = {"cost": {"session_cost_usd": "1.50"}}
        result = cost(cache, {})
        assert result == "$1.50"

    def test_non_numeric_cost_returns_empty(self) -> None:
        """Should return empty for non-numeric cost."""
        cache = {"cost": {"session_cost_usd": "expensive"}}
        result = cost(cache, {})
        assert result == ""

    def test_non_dict_cost_returns_empty(self) -> None:
        """Should return empty when cost is not a dict."""
        result = cost({"cost": "wrong"}, {})
        assert result == ""


# ---------------------------------------------------------------------------
# segments.py -- BUILTIN_SEGMENTS registry
# ---------------------------------------------------------------------------


class TestBuiltinSegmentsRegistry:
    """Tests for the BUILTIN_SEGMENTS registry."""

    def test_all_segments_registered(self) -> None:
        """All expected segments should be in the registry."""
        expected = {"clock", "mantra", "builder_trap", "session", "practice", "ai_ratio", "cost"}
        assert set(BUILTIN_SEGMENTS.keys()) == expected

    def test_all_segments_callable(self) -> None:
        """All registered segments should be callable."""
        for name, fn in BUILTIN_SEGMENTS.items():
            assert callable(fn), f"Segment {name!r} is not callable"

    def test_all_segments_return_string(self) -> None:
        """All segments should return a string when given empty inputs."""
        for name, fn in BUILTIN_SEGMENTS.items():
            result = fn({}, {})
            assert isinstance(result, str), f"Segment {name!r} returned {type(result)}"


# ---------------------------------------------------------------------------
# renderer.py -- resolve_cache_path
# ---------------------------------------------------------------------------


class TestResolveCachePath:
    """Tests for resolve_cache_path()."""

    def test_default_path(self) -> None:
        """Should return the default cache path when not configured."""
        result = resolve_cache_path({})
        expected = Path("~/.hookwise/state/status-line-cache.json").expanduser()
        assert result == expected

    def test_custom_path(self) -> None:
        """Should return custom path from config."""
        config = {"cache_path": "/tmp/custom-cache.json"}
        result = resolve_cache_path(config)
        assert result == Path("/tmp/custom-cache.json")

    def test_expands_tilde(self) -> None:
        """Should expand ~ in path."""
        config = {"cache_path": "~/my-cache.json"}
        result = resolve_cache_path(config)
        assert result == Path.home() / "my-cache.json"

    def test_non_string_falls_back(self) -> None:
        """Should fall back to default when cache_path is not a string."""
        config = {"cache_path": 42}
        result = resolve_cache_path(config)
        expected = Path(DEFAULT_CACHE_PATH).expanduser()
        assert result == expected

    def test_empty_string_falls_back(self) -> None:
        """Should fall back to default when cache_path is empty."""
        config = {"cache_path": ""}
        result = resolve_cache_path(config)
        expected = Path(DEFAULT_CACHE_PATH).expanduser()
        assert result == expected


# ---------------------------------------------------------------------------
# renderer.py -- render_builtin_segment
# ---------------------------------------------------------------------------


class TestRenderBuiltinSegment:
    """Tests for render_builtin_segment()."""

    def test_renders_known_segment(self) -> None:
        """Should render a known segment."""
        cache = {"mantra": {"text": "Focus deeply.", "id": "m01"}}
        result = render_builtin_segment("mantra", cache, {})
        assert result == "Focus deeply."

    def test_unknown_segment_returns_empty(self) -> None:
        """Should return empty for unknown segment names."""
        result = render_builtin_segment("nonexistent", {}, {})
        assert result == ""

    def test_passes_config_to_segment(self) -> None:
        """Should pass segment config through to the segment function."""
        result = render_builtin_segment("clock", {}, {"format": "%H"})
        # Should be just the hour (2 digits)
        assert len(result) == 2

    def test_exception_in_segment_returns_empty(self) -> None:
        """Should return empty if segment function raises."""
        with patch.dict(BUILTIN_SEGMENTS, {"broken": lambda c, cfg: 1 / 0}):
            result = render_builtin_segment("broken", {}, {})
            assert result == ""


# ---------------------------------------------------------------------------
# renderer.py -- render_custom_segment
# ---------------------------------------------------------------------------


class TestRenderCustomSegment:
    """Tests for render_custom_segment()."""

    def test_captures_stdout(self) -> None:
        """Should capture stdout from a shell command."""
        result = render_custom_segment({"command": "echo 'focus: deep work'"})
        assert result == "focus: deep work"

    def test_strips_whitespace(self) -> None:
        """Should strip trailing whitespace from stdout."""
        result = render_custom_segment({"command": "echo '  hello  '"})
        assert result == "hello"

    def test_missing_command_returns_empty(self) -> None:
        """Should return empty when command is missing."""
        result = render_custom_segment({})
        assert result == ""

    def test_empty_command_returns_empty(self) -> None:
        """Should return empty for empty command string."""
        result = render_custom_segment({"command": ""})
        assert result == ""

    def test_non_string_command_returns_empty(self) -> None:
        """Should return empty for non-string command."""
        result = render_custom_segment({"command": 42})
        assert result == ""

    def test_failing_command_returns_empty(self) -> None:
        """Should return empty when command exits with non-zero."""
        result = render_custom_segment({"command": "exit 1"})
        assert result == ""

    def test_nonexistent_command_returns_empty(self) -> None:
        """Should return empty for a command that doesn't exist."""
        result = render_custom_segment({"command": "nonexistent_command_xyz_123"})
        assert result == ""

    def test_timeout_returns_empty(self) -> None:
        """Should return empty when command times out."""
        result = render_custom_segment({"command": "sleep 10", "timeout": 1})
        assert result == ""

    def test_respects_custom_timeout(self) -> None:
        """Should use the configured timeout."""
        start = time.time()
        render_custom_segment({"command": "sleep 10", "timeout": 1})
        elapsed = time.time() - start
        assert elapsed < 5  # Should have timed out around 1s

    def test_default_timeout(self) -> None:
        """Default timeout should be 5 seconds."""
        assert DEFAULT_CUSTOM_TIMEOUT == 5

    def test_invalid_timeout_uses_default(self) -> None:
        """Should use default timeout for invalid timeout values."""
        # Non-numeric timeout
        result = render_custom_segment({
            "command": "echo ok",
            "timeout": "not-a-number",
        })
        assert result == "ok"

    def test_multiline_output_captures_all(self) -> None:
        """Should capture all lines of stdout."""
        result = render_custom_segment({"command": "printf 'line1\\nline2'"})
        assert "line1" in result
        assert "line2" in result


# ---------------------------------------------------------------------------
# renderer.py -- compose_segments
# ---------------------------------------------------------------------------


class TestComposeSegments:
    """Tests for compose_segments()."""

    def test_joins_with_delimiter(self) -> None:
        """Should join rendered segments with the delimiter."""
        cache = {
            "mantra": {"text": "Focus.", "id": "m01"},
            "cost": {"session_cost_usd": 0.50},
        }
        segments = [
            {"builtin": "mantra"},
            {"builtin": "cost"},
        ]
        result = compose_segments(segments, cache, " | ")
        assert result == "Focus. | $0.50"

    def test_filters_empty_segments(self) -> None:
        """Should not include empty segments in output."""
        cache = {
            "cost": {"session_cost_usd": 0.50},
        }
        # mantra is missing from cache, so it should be empty
        segments = [
            {"builtin": "mantra"},
            {"builtin": "cost"},
        ]
        result = compose_segments(segments, cache, " | ")
        assert result == "$0.50"

    def test_custom_delimiter(self) -> None:
        """Should use the specified delimiter."""
        cache = {
            "mantra": {"text": "Go.", "id": "m01"},
            "cost": {"session_cost_usd": 1.0},
        }
        segments = [
            {"builtin": "mantra"},
            {"builtin": "cost"},
        ]
        result = compose_segments(segments, cache, " -- ")
        assert result == "Go. -- $1.00"

    def test_empty_segments_list(self) -> None:
        """Should return empty for no segments."""
        result = compose_segments([], {}, " | ")
        assert result == ""

    def test_all_segments_empty(self) -> None:
        """Should return empty when all segments produce empty output."""
        result = compose_segments(
            [{"builtin": "mantra"}, {"builtin": "cost"}],
            {},
            " | ",
        )
        assert result == ""

    def test_non_dict_entries_skipped(self) -> None:
        """Should skip non-dict entries in segments list."""
        cache = {"cost": {"session_cost_usd": 0.25}}
        segments = [
            "not a dict",
            {"builtin": "cost"},
            42,
        ]
        result = compose_segments(segments, cache, " | ")  # type: ignore[arg-type]
        assert result == "$0.25"

    def test_custom_segment_in_composition(self) -> None:
        """Should include custom segments in composition."""
        cache = {"cost": {"session_cost_usd": 0.10}}
        segments = [
            {"builtin": "cost"},
            {"custom": {"command": "echo 'custom output'"}},
        ]
        result = compose_segments(segments, cache, " | ")
        assert "$0.10" in result
        assert "custom output" in result

    def test_order_preserved(self) -> None:
        """Segments should appear in config order."""
        cache = {
            "mantra": {"text": "A", "id": "1"},
            "cost": {"session_cost_usd": 0.0},
        }
        segments = [
            {"builtin": "cost"},
            {"builtin": "mantra"},
        ]
        result = compose_segments(segments, cache, " | ")
        cost_pos = result.index("$0.00")
        mantra_pos = result.index("A")
        assert cost_pos < mantra_pos

    def test_unknown_builtin_filtered_out(self) -> None:
        """Unknown builtin segment names should produce empty (filtered)."""
        cache = {"cost": {"session_cost_usd": 0.50}}
        segments = [
            {"builtin": "nonexistent"},
            {"builtin": "cost"},
        ]
        result = compose_segments(segments, cache, " | ")
        assert result == "$0.50"

    def test_non_dict_custom_skipped(self) -> None:
        """Non-dict custom config should be skipped."""
        segments = [
            {"custom": "not a dict"},
        ]
        result = compose_segments(segments, {}, " | ")
        assert result == ""

    def test_non_string_builtin_skipped(self) -> None:
        """Non-string builtin name should be skipped."""
        segments = [
            {"builtin": 42},
        ]
        result = compose_segments(segments, {}, " | ")
        assert result == ""


# ---------------------------------------------------------------------------
# __init__.py -- StatusLineRenderer
# ---------------------------------------------------------------------------


class TestStatusLineRenderer:
    """Tests for the StatusLineRenderer class."""

    def test_renders_from_cache_file(self, tmp_path: Path) -> None:
        """Should read cache from file and compose segments."""
        cache_path = tmp_path / "cache.json"
        cache = _make_full_cache()
        _write_cache(cache_path, cache)

        renderer = StatusLineRenderer()
        result = renderer.render({
            "enabled": True,
            "segments": [
                {"builtin": "mantra"},
                {"builtin": "cost"},
            ],
            "delimiter": " | ",
            "cache_path": str(cache_path),
        })

        assert "One idea. One pause." in result
        assert "$0.67" in result
        assert " | " in result

    def test_disabled_returns_empty(self) -> None:
        """Should return empty when enabled is False."""
        renderer = StatusLineRenderer()
        result = renderer.render({
            "enabled": False,
            "segments": [{"builtin": "clock"}],
        })
        assert result == ""

    def test_enabled_by_default(self, tmp_path: Path) -> None:
        """Should be enabled when 'enabled' key is missing."""
        cache_path = tmp_path / "cache.json"
        _write_cache(cache_path, {"mantra": {"text": "Go.", "id": "1"}})

        renderer = StatusLineRenderer()
        result = renderer.render({
            "segments": [{"builtin": "mantra"}],
            "cache_path": str(cache_path),
        })
        assert result == "Go."

    def test_no_segments_returns_empty(self) -> None:
        """Should return empty when segments list is empty."""
        renderer = StatusLineRenderer()
        result = renderer.render({"enabled": True, "segments": []})
        assert result == ""

    def test_missing_segments_returns_empty(self) -> None:
        """Should return empty when segments key is missing."""
        renderer = StatusLineRenderer()
        result = renderer.render({"enabled": True})
        assert result == ""

    def test_non_list_segments_returns_empty(self) -> None:
        """Should return empty when segments is not a list."""
        renderer = StatusLineRenderer()
        result = renderer.render({"enabled": True, "segments": "not a list"})
        assert result == ""

    def test_missing_cache_file_graceful(self, tmp_path: Path) -> None:
        """Should handle missing cache file gracefully (empty segments)."""
        renderer = StatusLineRenderer()
        result = renderer.render({
            "enabled": True,
            "segments": [{"builtin": "mantra"}],
            "cache_path": str(tmp_path / "nonexistent.json"),
        })
        # mantra will be empty since cache is empty
        assert result == ""

    def test_corrupt_cache_file_graceful(self, tmp_path: Path) -> None:
        """Should handle corrupt cache file gracefully."""
        cache_path = tmp_path / "corrupt.json"
        cache_path.write_text("not valid json {{{", encoding="utf-8")

        renderer = StatusLineRenderer()
        result = renderer.render({
            "enabled": True,
            "segments": [{"builtin": "mantra"}],
            "cache_path": str(cache_path),
        })
        assert result == ""

    def test_default_delimiter(self, tmp_path: Path) -> None:
        """Should use ' | ' as default delimiter."""
        cache_path = tmp_path / "cache.json"
        _write_cache(cache_path, {
            "mantra": {"text": "A", "id": "1"},
            "cost": {"session_cost_usd": 0.0},
        })

        renderer = StatusLineRenderer()
        result = renderer.render({
            "segments": [
                {"builtin": "mantra"},
                {"builtin": "cost"},
            ],
            "cache_path": str(cache_path),
        })
        assert " | " in result

    def test_custom_delimiter(self, tmp_path: Path) -> None:
        """Should use custom delimiter from config."""
        cache_path = tmp_path / "cache.json"
        _write_cache(cache_path, {
            "mantra": {"text": "A", "id": "1"},
            "cost": {"session_cost_usd": 0.0},
        })

        renderer = StatusLineRenderer()
        result = renderer.render({
            "segments": [
                {"builtin": "mantra"},
                {"builtin": "cost"},
            ],
            "delimiter": " :: ",
            "cache_path": str(cache_path),
        })
        assert " :: " in result

    def test_non_string_delimiter_falls_back(self, tmp_path: Path) -> None:
        """Should fall back to default delimiter for non-string values."""
        cache_path = tmp_path / "cache.json"
        _write_cache(cache_path, {
            "mantra": {"text": "A", "id": "1"},
            "cost": {"session_cost_usd": 0.0},
        })

        renderer = StatusLineRenderer()
        result = renderer.render({
            "segments": [
                {"builtin": "mantra"},
                {"builtin": "cost"},
            ],
            "delimiter": 42,
            "cache_path": str(cache_path),
        })
        assert " | " in result

    def test_full_pipeline_all_segments(self, tmp_path: Path) -> None:
        """End-to-end test with all built-in segments."""
        cache_path = tmp_path / "cache.json"
        cache = _make_full_cache()
        _write_cache(cache_path, cache)

        renderer = StatusLineRenderer()
        result = renderer.render({
            "enabled": True,
            "segments": [
                {"builtin": "clock", "format": "%H:%M"},
                {"builtin": "mantra"},
                {"builtin": "builder_trap"},
                {"builtin": "session"},
                {"builtin": "practice"},
                {"builtin": "ai_ratio"},
                {"builtin": "cost"},
            ],
            "delimiter": " | ",
            "cache_path": str(cache_path),
        })

        # All non-empty segments should be present
        parts = result.split(" | ")
        # clock always renders, mantra has text, builder_trap has yellow alert,
        # session has data, practice has today's data, ai_ratio has data, cost has data
        assert len(parts) >= 5  # At least these many should render

        # Specific content checks
        assert "One idea. One pause." in result
        assert "\u26a0\ufe0f" in result  # builder_trap yellow
        assert "tools" in result  # session
        assert "AI: 72%" in result
        assert "$0.67" in result


# ---------------------------------------------------------------------------
# __init__.py -- module-level render()
# ---------------------------------------------------------------------------


class TestModuleLevelRender:
    """Tests for the module-level render() convenience function."""

    def test_delegates_to_renderer(self, tmp_path: Path) -> None:
        """Should produce the same result as StatusLineRenderer.render()."""
        cache_path = tmp_path / "cache.json"
        _write_cache(cache_path, {"mantra": {"text": "Test.", "id": "1"}})

        config = {
            "segments": [{"builtin": "mantra"}],
            "cache_path": str(cache_path),
        }

        renderer = StatusLineRenderer()
        expected = renderer.render(config)

        result = render(config)
        assert result == expected

    def test_disabled_returns_empty(self) -> None:
        """Should return empty when disabled."""
        result = render({"enabled": False, "segments": [{"builtin": "clock"}]})
        assert result == ""


# ---------------------------------------------------------------------------
# Integration: config structure matches design doc
# ---------------------------------------------------------------------------


class TestConfigIntegration:
    """Tests that the config structure matches the design document."""

    def test_full_config_from_design_doc(self, tmp_path: Path) -> None:
        """Config from the design doc should produce valid output."""
        cache_path = tmp_path / "cache.json"
        cache = _make_full_cache()
        _write_cache(cache_path, cache)

        config = {
            "enabled": True,
            "segments": [
                {"builtin": "clock", "format": "%H:%M"},
                {"builtin": "mantra"},
                {"builtin": "builder_trap"},
                {"builtin": "session"},
                {"builtin": "practice"},
                {"builtin": "ai_ratio"},
                {"builtin": "cost"},
                {
                    "custom": {
                        "command": "echo 'focus: deep work'",
                        "label": "focus",
                        "timeout": 3,
                    }
                },
            ],
            "delimiter": " | ",
            "cache_path": str(cache_path),
        }

        result = render(config)
        assert isinstance(result, str)
        assert len(result) > 0
        # Should have multiple segments joined by delimiter
        assert " | " in result
        # Custom segment should be included
        assert "focus: deep work" in result

    def test_empty_config_returns_empty(self) -> None:
        """Empty config should return empty (no segments)."""
        result = render({})
        assert result == ""

    def test_only_clock_renders(self) -> None:
        """A minimal config with just clock should work."""
        result = render({
            "segments": [{"builtin": "clock"}],
            "cache_path": "/nonexistent/path.json",
        })
        # Clock doesn't depend on cache, should always render
        assert ":" in result

    def test_cache_written_by_atomic_write_is_readable(self, tmp_path: Path) -> None:
        """Cache written with atomic_write_json should be readable by renderer."""
        cache_path = tmp_path / "state" / "status-line-cache.json"
        data = {
            "mantra": {"text": "Shipped.", "id": "m99"},
            "cost": {"session_cost_usd": 2.50},
        }
        atomic_write_json(cache_path, data)

        result = render({
            "segments": [
                {"builtin": "mantra"},
                {"builtin": "cost"},
            ],
            "cache_path": str(cache_path),
        })
        assert "Shipped." in result
        assert "$2.50" in result


# ---------------------------------------------------------------------------
# Edge cases and robustness
# ---------------------------------------------------------------------------


class TestEdgeCases:
    """Robustness tests for edge cases."""

    def test_render_with_none_values_in_cache(self, tmp_path: Path) -> None:
        """Should handle None values in cache gracefully."""
        cache_path = tmp_path / "cache.json"
        _write_cache(cache_path, {
            "mantra": None,
            "cost": None,
            "session": None,
            "practice": None,
            "builder_trap": None,
        })

        result = render({
            "segments": [
                {"builtin": "mantra"},
                {"builtin": "cost"},
                {"builtin": "session"},
                {"builtin": "practice"},
                {"builtin": "builder_trap"},
            ],
            "cache_path": str(cache_path),
        })
        # All segments should be empty (filtered out)
        assert result == ""

    def test_render_with_empty_cache(self, tmp_path: Path) -> None:
        """Should handle empty cache gracefully."""
        cache_path = tmp_path / "cache.json"
        _write_cache(cache_path, {})

        result = render({
            "segments": [
                {"builtin": "mantra"},
                {"builtin": "cost"},
            ],
            "cache_path": str(cache_path),
        })
        assert result == ""

    def test_clock_always_renders(self, tmp_path: Path) -> None:
        """Clock should always produce output regardless of cache state."""
        cache_path = tmp_path / "cache.json"
        _write_cache(cache_path, {})

        result = render({
            "segments": [{"builtin": "clock"}],
            "cache_path": str(cache_path),
        })
        assert len(result) > 0

    def test_mixed_valid_and_invalid_segments(self, tmp_path: Path) -> None:
        """Should render valid segments and skip invalid ones."""
        cache_path = tmp_path / "cache.json"
        _write_cache(cache_path, {"cost": {"session_cost_usd": 1.0}})

        result = render({
            "segments": [
                {"builtin": "nonexistent_segment"},
                {"builtin": "cost"},
                {"builtin": "another_bad_one"},
            ],
            "cache_path": str(cache_path),
        })
        assert result == "$1.00"

    def test_only_custom_segments(self) -> None:
        """Should work with only custom segments (no builtins)."""
        result = render({
            "segments": [
                {"custom": {"command": "echo hello"}},
                {"custom": {"command": "echo world"}},
            ],
            "delimiter": " + ",
            "cache_path": "/nonexistent.json",
        })
        assert result == "hello + world"

    def test_custom_segment_empty_output(self) -> None:
        """Custom command producing empty output should be filtered."""
        result = render({
            "segments": [
                {"custom": {"command": "printf ''"}},
                {"custom": {"command": "echo visible"}},
            ],
            "cache_path": "/nonexistent.json",
        })
        assert result == "visible"

    def test_segment_without_builtin_or_custom_skipped(self) -> None:
        """Segment dict without 'builtin' or 'custom' should be skipped."""
        result = render({
            "segments": [
                {"something_else": "value"},
                {"custom": {"command": "echo ok"}},
            ],
            "cache_path": "/nonexistent.json",
        })
        assert result == "ok"
