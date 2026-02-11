"""Error handling and logging infrastructure for hookwise.

Implements the fail-open error boundary pattern: hook handlers must never
cause Claude Code to fail. If a handler raises an exception, hookwise
catches it, logs it, and exits with code 0 (success) so Claude Code
continues normally.

The only exception to fail-open is an explicit guard block: when a guard
hook has already written valid JSON with {"decision": "block", ...} to
stdout, a non-zero exit IS appropriate. But even then, hookwise handles
this explicitly rather than letting exceptions propagate.

Logging uses stdlib's RotatingFileHandler to maintain bounded log files
under ~/.hookwise/logs/.
"""

from __future__ import annotations

import json
import logging
import sys
import traceback
from logging.handlers import RotatingFileHandler
from pathlib import Path
from typing import Any

from hookwise.state import ensure_state_dir, get_state_dir

# Constants
DEFAULT_LOG_LEVEL = "INFO"
ERROR_LOG_FILENAME = "error.log"
DEBUG_LOG_FILENAME = "debug.log"
LOG_DIR_NAME = "logs"

# 10 MB max per log file, keep 5 rotations
MAX_LOG_BYTES = 10 * 1024 * 1024
MAX_LOG_BACKUPS = 5

# Log format
LOG_FORMAT = "%(asctime)s [%(levelname)s] %(name)s: %(message)s"
LOG_DATE_FORMAT = "%Y-%m-%dT%H:%M:%S"


def _get_log_dir(state_dir: Path | None = None) -> Path:
    """Return the log directory path, creating it if needed.

    Args:
        state_dir: Base state directory. Uses get_state_dir() if None.

    Returns:
        Path to the logs directory.
    """
    if state_dir is None:
        state_dir = get_state_dir()
    log_dir = state_dir / LOG_DIR_NAME
    log_dir.mkdir(parents=True, exist_ok=True)
    return log_dir


def setup_logging(
    state_dir: Path | None = None,
    log_level: str = DEFAULT_LOG_LEVEL,
    *,
    debug: bool = False,
) -> logging.Logger:
    """Set up rotating file loggers for hookwise.

    Creates two log files under {state_dir}/logs/:
    - error.log: Always active, captures WARNING and above
    - debug.log: Only when debug=True, captures DEBUG and above

    Uses RotatingFileHandler with 10MB max size and 5 backup rotations.

    Args:
        state_dir: Base state directory. Uses get_state_dir() if None.
        log_level: Minimum log level for the error log. Default "INFO".
        debug: If True, also create a debug log capturing all levels.

    Returns:
        The configured 'hookwise' logger.
    """
    if state_dir is None:
        state_dir = ensure_state_dir()

    log_dir = _get_log_dir(state_dir)
    logger = logging.getLogger("hookwise")

    # Clear any existing handlers (important for tests / re-initialization)
    logger.handlers.clear()

    # Set the logger to the lowest level we might need
    effective_level = logging.DEBUG if debug else getattr(logging, log_level.upper(), logging.INFO)
    logger.setLevel(min(effective_level, logging.DEBUG) if debug else effective_level)

    formatter = logging.Formatter(LOG_FORMAT, datefmt=LOG_DATE_FORMAT)

    # Error/info log handler (always active)
    error_log_path = log_dir / ERROR_LOG_FILENAME
    error_handler = RotatingFileHandler(
        error_log_path,
        maxBytes=MAX_LOG_BYTES,
        backupCount=MAX_LOG_BACKUPS,
        encoding="utf-8",
    )
    error_handler.setLevel(getattr(logging, log_level.upper(), logging.INFO))
    error_handler.setFormatter(formatter)
    logger.addHandler(error_handler)

    # Debug log handler (only when debug mode is enabled)
    if debug:
        debug_log_path = log_dir / DEBUG_LOG_FILENAME
        debug_handler = RotatingFileHandler(
            debug_log_path,
            maxBytes=MAX_LOG_BYTES,
            backupCount=MAX_LOG_BACKUPS,
            encoding="utf-8",
        )
        debug_handler.setLevel(logging.DEBUG)
        debug_handler.setFormatter(formatter)
        logger.addHandler(debug_handler)

    return logger


class FailOpen:
    """Context manager that implements the fail-open error boundary.

    Catches ALL exceptions from hook handler code and ensures the process
    exits with code 0 (allowing Claude Code to continue). Exceptions are
    logged to the error log for later debugging.

    The only case where a non-zero exit is permitted is when:
    1. The handler explicitly wrote a guard block response to stdout
       (valid JSON with "decision": "block")
    2. AND the handler explicitly set a non-zero exit code

    Usage::

        with FailOpen(logger=logger):
            # Hook handler code here
            run_my_handler()
        # If handler raises, we get here with exit code 0

    Usage with guard hooks::

        with FailOpen(logger=logger) as boundary:
            result = evaluate_guard(event)
            if result.should_block:
                boundary.mark_guard_block(result.to_json())
                # FailOpen will exit with code 1 after printing JSON

    Attributes:
        logger: Logger instance for recording errors.
        guard_block_output: If set, contains the JSON to emit on stdout
            before exiting with code 1.
    """

    def __init__(
        self,
        *,
        logger: logging.Logger | None = None,
        exit_on_error: bool = True,
    ) -> None:
        """Initialize the FailOpen boundary.

        Args:
            logger: Logger for recording caught exceptions.
                If None, uses logging.getLogger('hookwise').
            exit_on_error: If True (default), calls sys.exit(0) on error.
                Set to False for testing to allow exception inspection.
        """
        self._logger = logger or logging.getLogger("hookwise")
        self._exit_on_error = exit_on_error
        self._guard_block_output: str | None = None
        self._exception: BaseException | None = None

    @property
    def caught_exception(self) -> BaseException | None:
        """The exception that was caught, if any (useful for testing)."""
        return self._exception

    @property
    def guard_block_output(self) -> str | None:
        """The guard block JSON output, if set."""
        return self._guard_block_output

    def mark_guard_block(self, output: dict[str, Any] | str) -> None:
        """Mark this boundary as containing a guard block decision.

        When a guard hook determines that an action should be blocked,
        call this method with the block response. The FailOpen boundary
        will then exit with code 1 (block) instead of code 0 (allow),
        and will emit the JSON to stdout.

        Args:
            output: The guard block response, either as a dict (will be
                serialized to JSON) or as a pre-serialized JSON string.
        """
        if isinstance(output, dict):
            self._guard_block_output = json.dumps(output)
        else:
            self._guard_block_output = output

    def __enter__(self) -> FailOpen:
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: Any,
    ) -> bool:
        """Handle any exception from the wrapped code.

        - If no exception: check for guard block and exit accordingly
        - If exception + guard block marked: log warning, emit block JSON,
          exit 1 (the guard decision takes precedence)
        - If exception + no guard block: log error, exit 0 (fail open)
        """
        if exc_val is not None:
            self._exception = exc_val
            # Log the full traceback for debugging
            tb_str = "".join(traceback.format_exception(exc_type, exc_val, exc_tb))
            self._logger.error("Hook handler failed (fail-open): %s\n%s", exc_val, tb_str)

            if self._guard_block_output is not None:
                # Guard block was set before the error -- honor the block
                self._logger.warning(
                    "Guard block was set before handler error; honoring block decision"
                )
                print(self._guard_block_output, flush=True)  # noqa: T201
                if self._exit_on_error:
                    sys.exit(1)
                return True  # Suppress exception for testing

            # Fail open: suppress the exception and exit cleanly
            if self._exit_on_error:
                sys.exit(0)
            return True  # Suppress exception for testing

        # No exception -- check if this is a guard block
        if self._guard_block_output is not None:
            print(self._guard_block_output, flush=True)  # noqa: T201
            if self._exit_on_error:
                sys.exit(1)

        return False
