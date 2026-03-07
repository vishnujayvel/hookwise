"""Tests for StatusTab._read_live_output() — TUI live output file reading.

Verifies that the TUI correctly reads the persisted status-line output
written by the TS status-line command, with proper freshness checks.
"""

from __future__ import annotations

import time
from pathlib import Path

import pytest

from hookwise_tui.tabs.status import StatusTab, _LAST_STATUS_OUTPUT_PATH, _LIVE_OUTPUT_MAX_AGE


class TestReadLiveOutput:
    """Tests for StatusTab._read_live_output() static method."""

    def test_returns_content_when_file_is_fresh(self, tmp_path, monkeypatch):
        output_file = tmp_path / "cache" / "last-status-output.txt"
        output_file.parent.mkdir(parents=True)
        output_file.write_text("55% | $3.14 | 30m\nFocus deeply")

        monkeypatch.setattr(
            "hookwise_tui.tabs.status._LAST_STATUS_OUTPUT_PATH", output_file
        )

        result = StatusTab._read_live_output()
        assert result is not None
        assert "55%" in result
        assert "Focus deeply" in result

    def test_returns_none_when_file_missing(self, tmp_path, monkeypatch):
        missing = tmp_path / "cache" / "last-status-output.txt"
        monkeypatch.setattr(
            "hookwise_tui.tabs.status._LAST_STATUS_OUTPUT_PATH", missing
        )

        result = StatusTab._read_live_output()
        assert result is None

    def test_returns_none_when_file_is_stale(self, tmp_path, monkeypatch):
        output_file = tmp_path / "cache" / "last-status-output.txt"
        output_file.parent.mkdir(parents=True)
        output_file.write_text("stale content")

        # Set mtime to well past the max age
        import os
        stale_time = time.time() - _LIVE_OUTPUT_MAX_AGE - 10
        os.utime(output_file, (stale_time, stale_time))

        monkeypatch.setattr(
            "hookwise_tui.tabs.status._LAST_STATUS_OUTPUT_PATH", output_file
        )

        result = StatusTab._read_live_output()
        assert result is None

    def test_returns_none_when_file_is_empty(self, tmp_path, monkeypatch):
        output_file = tmp_path / "cache" / "last-status-output.txt"
        output_file.parent.mkdir(parents=True)
        output_file.write_text("")

        monkeypatch.setattr(
            "hookwise_tui.tabs.status._LAST_STATUS_OUTPUT_PATH", output_file
        )

        result = StatusTab._read_live_output()
        assert result is None

    def test_returns_none_when_file_is_whitespace_only(self, tmp_path, monkeypatch):
        output_file = tmp_path / "cache" / "last-status-output.txt"
        output_file.parent.mkdir(parents=True)
        output_file.write_text("   \n  \n  ")

        monkeypatch.setattr(
            "hookwise_tui.tabs.status._LAST_STATUS_OUTPUT_PATH", output_file
        )

        result = StatusTab._read_live_output()
        assert result is None

    def test_freshness_boundary(self, tmp_path, monkeypatch):
        """File at exactly the max age boundary should still be considered fresh."""
        output_file = tmp_path / "cache" / "last-status-output.txt"
        output_file.parent.mkdir(parents=True)
        output_file.write_text("boundary content")

        # Set mtime to just under the max age (with 1s buffer for test timing)
        import os
        almost_stale = time.time() - _LIVE_OUTPUT_MAX_AGE + 2
        os.utime(output_file, (almost_stale, almost_stale))

        monkeypatch.setattr(
            "hookwise_tui.tabs.status._LAST_STATUS_OUTPUT_PATH", output_file
        )

        result = StatusTab._read_live_output()
        assert result is not None
        assert result == "boundary content"

    def test_default_path_points_to_hookwise_cache(self):
        """The default path should be under ~/.hookwise/cache/."""
        expected = Path.home() / ".hookwise" / "cache" / "last-status-output.txt"
        assert _LAST_STATUS_OUTPUT_PATH == expected


class TestSegmentHasDataWithLiveOutput:
    """Tests for StatusTab._segment_has_data() with stdin segments."""

    def test_stdin_segment_has_data_when_live_output_fresh(self, tmp_path, monkeypatch):
        output_file = tmp_path / "cache" / "last-status-output.txt"
        output_file.parent.mkdir(parents=True)
        output_file.write_text("live output")

        monkeypatch.setattr(
            "hookwise_tui.tabs.status._LAST_STATUS_OUTPUT_PATH", output_file
        )

        assert StatusTab._segment_has_data("context_bar", {}) is True
        assert StatusTab._segment_has_data("cost", {}) is True
        assert StatusTab._segment_has_data("duration", {}) is True
        assert StatusTab._segment_has_data("daemon_health", {}) is True

    def test_stdin_segment_no_data_when_live_output_missing(self, tmp_path, monkeypatch):
        missing = tmp_path / "cache" / "last-status-output.txt"
        monkeypatch.setattr(
            "hookwise_tui.tabs.status._LAST_STATUS_OUTPUT_PATH", missing
        )

        assert StatusTab._segment_has_data("context_bar", {}) is False
        assert StatusTab._segment_has_data("cost", {}) is False


class TestRenderWeatherSegment:
    """Tests for weather segment rendering via StatusTab._render_segment.

    These tests validate that the Python TUI correctly renders weather data
    produced by the Go WeatherProducer after FlattenForTUI processing.
    This is a cross-boundary contract test: the cache dict keys must match
    what the Go producer writes (camelCase field names like temperatureUnit,
    windSpeed, emoji) after the bridge flattens the Go envelope.
    """

    def test_renders_temperature_fahrenheit(self):
        """Go producer writes temperatureUnit='fahrenheit'; TUI should show °F."""
        cache = {
            "weather": {
                "temperature": 72,
                "temperatureUnit": "fahrenheit",
                "emoji": "\u2600\ufe0f",
                "windSpeed": 5,
                "updated_at": "2026-03-07T10:00:00Z",
                "ttl_seconds": 300,
            }
        }
        result = StatusTab._render_segment("weather", cache)
        assert "72" in result
        assert "F" in result
        assert "\u2600\ufe0f" in result

    def test_renders_temperature_celsius(self):
        """Go producer writes temperatureUnit='celsius'; TUI should show °C."""
        cache = {
            "weather": {
                "temperature": 22,
                "temperatureUnit": "celsius",
                "emoji": "\U0001f324\ufe0f",
                "windSpeed": 3,
                "updated_at": "2026-03-07T10:00:00Z",
                "ttl_seconds": 300,
            }
        }
        result = StatusTab._render_segment("weather", cache)
        assert "22" in result
        assert "C" in result

    def test_renders_wind_indicator_when_windy(self):
        """windSpeed > 20 should show the wind emoji."""
        cache = {
            "weather": {
                "temperature": 55,
                "temperatureUnit": "fahrenheit",
                "emoji": "\U0001f32c\ufe0f",
                "windSpeed": 25,
                "updated_at": "2026-03-07T10:00:00Z",
                "ttl_seconds": 300,
            }
        }
        result = StatusTab._render_segment("weather", cache)
        assert "55" in result
        assert "\U0001f4a8" in result  # wind emoji

    def test_no_wind_indicator_when_calm(self):
        """windSpeed <= 20 should NOT show the wind emoji."""
        cache = {
            "weather": {
                "temperature": 70,
                "temperatureUnit": "fahrenheit",
                "emoji": "\u2600\ufe0f",
                "windSpeed": 10,
                "updated_at": "2026-03-07T10:00:00Z",
                "ttl_seconds": 300,
            }
        }
        result = StatusTab._render_segment("weather", cache)
        assert "70" in result
        assert "\U0001f4a8" not in result

    def test_renders_dashes_when_no_temperature(self):
        """Missing temperature should render '--' placeholder."""
        cache = {
            "weather": {
                "emoji": "\u2600\ufe0f",
                "updated_at": "2026-03-07T10:00:00Z",
                "ttl_seconds": 300,
            }
        }
        result = StatusTab._render_segment("weather", cache)
        assert "--" in result

    def test_go_producer_snake_case_fields_not_consumed(self):
        """If Go producer writes old snake_case fields, temperature won't render.

        This is the regression test for the field-name mismatch bug:
        Go wrote 'unit'/'wind_speed' but Python expects 'temperatureUnit'/'windSpeed'.
        """
        cache_with_old_schema = {
            "weather": {
                "temperature": 72,
                "unit": "F",           # WRONG: Python expects "temperatureUnit"
                "wind_speed": 25,      # WRONG: Python expects "windSpeed"
                "updated_at": "2026-03-07T10:00:00Z",
                "ttl_seconds": 300,
            }
        }
        result = StatusTab._render_segment("weather", cache_with_old_schema)
        # With old schema, temperature renders but unit defaults to F and wind is missed
        assert "72" in result
        # The wind emoji should NOT appear because "windSpeed" is missing
        assert "\U0001f4a8" not in result

    def test_full_go_producer_flattened_output(self):
        """Simulates exact output of Go WeatherProducer after FlattenForTUI.

        This is the definitive cross-boundary integration test. The dict must
        match what bridge.FlattenForTUI produces from the Go WeatherProducer.
        """
        # This is exactly what the Go producer + FlattenForTUI should produce
        flattened_cache = {
            "weather": {
                "temperature": 68,
                "temperatureUnit": "fahrenheit",
                "emoji": "\U0001f324\ufe0f",
                "condition": "unknown (placeholder)",
                "humidity": 0,
                "windSpeed": 0,
                "source": "placeholder",
                "updated_at": "2026-03-07T10:00:00Z",
                "ttl_seconds": 300,
            }
        }
        result = StatusTab._render_segment("weather", flattened_cache)
        assert "68" in result, f"Temperature should appear in rendered output: {result}"
        assert "\u00b0F" in result, f"Fahrenheit unit should appear in rendered output: {result}"
        assert "\U0001f324\ufe0f" in result, f"Emoji should appear in rendered output: {result}"
