"""Tests for hookwise.errors -- error handling and logging infrastructure."""

from __future__ import annotations

import json
import logging
from pathlib import Path

import pytest

from hookwise.errors import (
    DEBUG_LOG_FILENAME,
    ERROR_LOG_FILENAME,
    LOG_DIR_NAME,
    MAX_LOG_BACKUPS,
    MAX_LOG_BYTES,
    FailOpen,
    setup_logging,
)


# ---------------------------------------------------------------------------
# setup_logging
# ---------------------------------------------------------------------------


class TestSetupLogging:
    """Tests for setup_logging()."""

    def test_creates_log_directory(self, tmp_state_dir: Path) -> None:
        """Should create the logs/ directory under state dir."""
        setup_logging(tmp_state_dir)
        log_dir = tmp_state_dir / LOG_DIR_NAME
        assert log_dir.is_dir()

    def test_creates_error_log_file(self, tmp_state_dir: Path) -> None:
        """Should create the error.log file."""
        logger = setup_logging(tmp_state_dir)
        logger.error("test error message")
        error_log = tmp_state_dir / LOG_DIR_NAME / ERROR_LOG_FILENAME
        assert error_log.exists()
        content = error_log.read_text(encoding="utf-8")
        assert "test error message" in content

    def test_returns_hookwise_logger(self, tmp_state_dir: Path) -> None:
        """Should return a logger named 'hookwise'."""
        logger = setup_logging(tmp_state_dir)
        assert logger.name == "hookwise"

    def test_default_log_level_info(self, tmp_state_dir: Path) -> None:
        """Default log level should be INFO."""
        logger = setup_logging(tmp_state_dir)
        # INFO messages should be captured
        logger.info("info message")
        error_log = tmp_state_dir / LOG_DIR_NAME / ERROR_LOG_FILENAME
        content = error_log.read_text(encoding="utf-8")
        assert "info message" in content

    def test_debug_messages_hidden_by_default(self, tmp_state_dir: Path) -> None:
        """DEBUG messages should not appear in error.log by default."""
        logger = setup_logging(tmp_state_dir)
        logger.debug("debug message")
        error_log = tmp_state_dir / LOG_DIR_NAME / ERROR_LOG_FILENAME
        if error_log.exists():
            content = error_log.read_text(encoding="utf-8")
            assert "debug message" not in content

    def test_debug_mode_creates_debug_log(self, tmp_state_dir: Path) -> None:
        """When debug=True, should create debug.log with DEBUG messages."""
        logger = setup_logging(tmp_state_dir, debug=True)
        logger.debug("detailed debug info")
        debug_log = tmp_state_dir / LOG_DIR_NAME / DEBUG_LOG_FILENAME
        assert debug_log.exists()
        content = debug_log.read_text(encoding="utf-8")
        assert "detailed debug info" in content

    def test_custom_log_level(self, tmp_state_dir: Path) -> None:
        """Should respect custom log level setting."""
        logger = setup_logging(tmp_state_dir, log_level="WARNING")
        logger.info("should not appear")
        logger.warning("should appear")
        error_log = tmp_state_dir / LOG_DIR_NAME / ERROR_LOG_FILENAME
        content = error_log.read_text(encoding="utf-8")
        assert "should not appear" not in content
        assert "should appear" in content

    def test_rotating_handler_config(self, tmp_state_dir: Path) -> None:
        """Should configure RotatingFileHandler with correct params."""
        logger = setup_logging(tmp_state_dir)
        handlers = logger.handlers
        assert len(handlers) >= 1
        from logging.handlers import RotatingFileHandler

        rotating = [h for h in handlers if isinstance(h, RotatingFileHandler)]
        assert len(rotating) >= 1
        handler = rotating[0]
        assert handler.maxBytes == MAX_LOG_BYTES
        assert handler.backupCount == MAX_LOG_BACKUPS

    def test_idempotent_setup(self, tmp_state_dir: Path) -> None:
        """Calling setup_logging twice should not duplicate handlers."""
        logger1 = setup_logging(tmp_state_dir)
        n_handlers = len(logger1.handlers)
        logger2 = setup_logging(tmp_state_dir)
        assert len(logger2.handlers) == n_handlers
        assert logger1 is logger2  # Same logger instance

    def test_log_format_includes_timestamp(self, tmp_state_dir: Path) -> None:
        """Log entries should include ISO-style timestamps."""
        logger = setup_logging(tmp_state_dir)
        logger.error("timestamp test")
        error_log = tmp_state_dir / LOG_DIR_NAME / ERROR_LOG_FILENAME
        content = error_log.read_text(encoding="utf-8")
        # Should contain ISO-style timestamp like 2026-02-10T...
        import re

        assert re.search(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}", content)

    def test_log_format_includes_level(self, tmp_state_dir: Path) -> None:
        """Log entries should include the log level."""
        logger = setup_logging(tmp_state_dir)
        logger.error("level test")
        error_log = tmp_state_dir / LOG_DIR_NAME / ERROR_LOG_FILENAME
        content = error_log.read_text(encoding="utf-8")
        assert "[ERROR]" in content


# ---------------------------------------------------------------------------
# FailOpen
# ---------------------------------------------------------------------------


class TestFailOpen:
    """Tests for the FailOpen context manager."""

    @pytest.fixture
    def logger(self, tmp_state_dir: Path) -> logging.Logger:
        """Provide a configured logger for tests."""
        return setup_logging(tmp_state_dir)

    def test_no_exception_passes_through(self, logger: logging.Logger) -> None:
        """Should do nothing when no exception occurs."""
        with FailOpen(logger=logger, exit_on_error=False) as boundary:
            x = 1 + 1
        assert boundary.caught_exception is None
        assert x == 2

    def test_catches_runtime_error(self, logger: logging.Logger) -> None:
        """Should catch RuntimeError and suppress it."""
        with FailOpen(logger=logger, exit_on_error=False) as boundary:
            raise RuntimeError("test error")
        assert isinstance(boundary.caught_exception, RuntimeError)
        assert str(boundary.caught_exception) == "test error"

    def test_catches_value_error(self, logger: logging.Logger) -> None:
        """Should catch ValueError."""
        with FailOpen(logger=logger, exit_on_error=False) as boundary:
            raise ValueError("bad value")
        assert isinstance(boundary.caught_exception, ValueError)

    def test_catches_type_error(self, logger: logging.Logger) -> None:
        """Should catch TypeError."""
        with FailOpen(logger=logger, exit_on_error=False) as boundary:
            raise TypeError("wrong type")
        assert isinstance(boundary.caught_exception, TypeError)

    def test_catches_os_error(self, logger: logging.Logger) -> None:
        """Should catch OSError (file I/O failures)."""
        with FailOpen(logger=logger, exit_on_error=False) as boundary:
            raise OSError("disk full")
        assert isinstance(boundary.caught_exception, OSError)

    def test_catches_generic_exception(self, logger: logging.Logger) -> None:
        """Should catch any Exception subclass."""
        with FailOpen(logger=logger, exit_on_error=False) as boundary:
            raise Exception("generic")  # noqa: TRY002
        assert isinstance(boundary.caught_exception, Exception)

    def test_logs_error_on_exception(
        self, logger: logging.Logger, tmp_state_dir: Path
    ) -> None:
        """Should log the exception details to error.log."""
        with FailOpen(logger=logger, exit_on_error=False):
            raise RuntimeError("logged error")

        error_log = tmp_state_dir / LOG_DIR_NAME / ERROR_LOG_FILENAME
        content = error_log.read_text(encoding="utf-8")
        assert "logged error" in content
        assert "fail-open" in content

    def test_logs_traceback(
        self, logger: logging.Logger, tmp_state_dir: Path
    ) -> None:
        """Should log the full traceback for debugging."""
        with FailOpen(logger=logger, exit_on_error=False):
            raise RuntimeError("traceback test")

        error_log = tmp_state_dir / LOG_DIR_NAME / ERROR_LOG_FILENAME
        content = error_log.read_text(encoding="utf-8")
        assert "Traceback" in content
        assert "RuntimeError" in content

    def test_exit_0_on_error(self, logger: logging.Logger) -> None:
        """Should call sys.exit(0) when exit_on_error=True."""
        with pytest.raises(SystemExit) as exc_info:
            with FailOpen(logger=logger, exit_on_error=True):
                raise RuntimeError("exit test")
        assert exc_info.value.code == 0

    def test_no_exit_when_no_error(self, logger: logging.Logger) -> None:
        """Should not exit when no exception occurs (no guard block)."""
        # This should complete without SystemExit
        with FailOpen(logger=logger, exit_on_error=True):
            pass

    # --- Guard block tests ---

    def test_guard_block_with_dict(
        self, logger: logging.Logger, capsys: pytest.CaptureFixture[str]
    ) -> None:
        """Should emit JSON to stdout for guard blocks (dict input)."""
        with pytest.raises(SystemExit) as exc_info:
            with FailOpen(logger=logger, exit_on_error=True) as boundary:
                boundary.mark_guard_block({"decision": "block", "reason": "forbidden"})
        assert exc_info.value.code == 1
        captured = capsys.readouterr()
        output = json.loads(captured.out.strip())
        assert output["decision"] == "block"
        assert output["reason"] == "forbidden"

    def test_guard_block_with_string(
        self, logger: logging.Logger, capsys: pytest.CaptureFixture[str]
    ) -> None:
        """Should emit pre-serialized JSON string for guard blocks."""
        json_str = '{"decision": "block", "reason": "test"}'
        with pytest.raises(SystemExit) as exc_info:
            with FailOpen(logger=logger, exit_on_error=True) as boundary:
                boundary.mark_guard_block(json_str)
        assert exc_info.value.code == 1
        captured = capsys.readouterr()
        assert captured.out.strip() == json_str

    def test_guard_block_exits_1(self, logger: logging.Logger) -> None:
        """Guard block should exit with code 1."""
        with pytest.raises(SystemExit) as exc_info:
            with FailOpen(logger=logger, exit_on_error=True) as boundary:
                boundary.mark_guard_block({"decision": "block"})
        assert exc_info.value.code == 1

    def test_guard_block_plus_exception(
        self, logger: logging.Logger, capsys: pytest.CaptureFixture[str]
    ) -> None:
        """If guard block is set AND handler errors, should still honor block."""
        with pytest.raises(SystemExit) as exc_info:
            with FailOpen(logger=logger, exit_on_error=True) as boundary:
                boundary.mark_guard_block({"decision": "block"})
                raise RuntimeError("handler crashed after block decision")
        # Should exit 1 (block), not 0 (fail-open)
        assert exc_info.value.code == 1
        captured = capsys.readouterr()
        output = json.loads(captured.out.strip())
        assert output["decision"] == "block"

    def test_guard_block_no_exit_in_test_mode(
        self, logger: logging.Logger, capsys: pytest.CaptureFixture[str]
    ) -> None:
        """With exit_on_error=False, guard block should not sys.exit."""
        with FailOpen(logger=logger, exit_on_error=False) as boundary:
            boundary.mark_guard_block({"decision": "block"})
        captured = capsys.readouterr()
        output = json.loads(captured.out.strip())
        assert output["decision"] == "block"

    def test_no_guard_block_property_initially(self, logger: logging.Logger) -> None:
        """guard_block_output should be None initially."""
        boundary = FailOpen(logger=logger, exit_on_error=False)
        assert boundary.guard_block_output is None

    def test_guard_block_property_after_mark(self, logger: logging.Logger) -> None:
        """guard_block_output should be set after mark_guard_block."""
        boundary = FailOpen(logger=logger, exit_on_error=False)
        boundary.mark_guard_block({"decision": "block"})
        assert boundary.guard_block_output is not None
        parsed = json.loads(boundary.guard_block_output)
        assert parsed["decision"] == "block"
