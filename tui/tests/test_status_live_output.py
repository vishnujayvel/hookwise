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
