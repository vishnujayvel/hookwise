"""Structured result from hook test execution.

Wraps stdout, stderr, and exit_code from a subprocess hook invocation
and provides assertion methods for common hook outcome patterns:
allowed, blocked, warned, silent, and confirm (asks).

All assertion methods raise AssertionError with descriptive messages
when the assertion fails, making them suitable for use directly in
pytest test functions.
"""

from __future__ import annotations

import json
from typing import Any


class HookResult:
    """Result from executing a hook in a subprocess.

    Captures the raw stdout, stderr, and exit code from a hook process,
    and provides convenience properties and assertion methods for
    verifying hook behavior in tests.

    Attributes:
        stdout: Raw stdout string from the subprocess.
        stderr: Raw stderr string from the subprocess.
        exit_code: Process exit code (0 = allow/success).
    """

    def __init__(
        self,
        stdout: str = "",
        stderr: str = "",
        exit_code: int = 0,
    ) -> None:
        self.stdout = stdout
        self.stderr = stderr
        self.exit_code = exit_code

    @property
    def json(self) -> dict[str, Any] | None:
        """Parse stdout as JSON and return the result.

        Returns:
            Parsed dict if stdout is valid JSON, None otherwise.
        """
        if not self.stdout or not self.stdout.strip():
            return None
        try:
            parsed = json.loads(self.stdout)
            if isinstance(parsed, dict):
                return parsed
            return None
        except (json.JSONDecodeError, TypeError):
            return None

    def assert_allowed(self) -> None:
        """Assert the hook allowed the action (exit 0, no block decision).

        Raises:
            AssertionError: If exit code is non-zero or stdout contains
                a block decision.
        """
        if self.exit_code != 0:
            raise AssertionError(
                f"Expected exit code 0 (allow), got {self.exit_code}. "
                f"stdout={self.stdout!r}, stderr={self.stderr!r}"
            )
        parsed = self.json
        if parsed is not None and parsed.get("decision") == "block":
            raise AssertionError(
                f"Expected allow, but stdout contains block decision: {self.stdout!r}"
            )

    def assert_blocked(self, reason_contains: str | None = None) -> None:
        """Assert the hook blocked the action.

        Verifies that stdout contains a JSON object with
        ``"decision": "block"``. Optionally checks that the reason
        field contains a given substring.

        Args:
            reason_contains: If provided, assert the reason field
                contains this substring.

        Raises:
            AssertionError: If no block decision found or reason
                does not match.
        """
        parsed = self.json
        if parsed is None:
            raise AssertionError(
                f"Expected block decision in stdout JSON, but stdout is not valid JSON. "
                f"stdout={self.stdout!r}, stderr={self.stderr!r}, exit_code={self.exit_code}"
            )
        decision = parsed.get("decision")
        if decision != "block":
            raise AssertionError(
                f"Expected decision='block', got decision={decision!r}. "
                f"stdout={self.stdout!r}"
            )
        if reason_contains is not None:
            reason = parsed.get("reason", "")
            if reason_contains not in (reason or ""):
                raise AssertionError(
                    f"Expected reason to contain {reason_contains!r}, "
                    f"got reason={reason!r}. stdout={self.stdout!r}"
                )

    def assert_warns(self, message_contains: str | None = None) -> None:
        """Assert the hook emitted a warning.

        Checks for a warn decision in stdout JSON, or for warning
        content in stderr or stdout text.

        Args:
            message_contains: If provided, assert the warning message
                contains this substring (checked in reason field,
                stderr, and stdout).

        Raises:
            AssertionError: If no warning found or message does not match.
        """
        parsed = self.json
        has_warn_decision = parsed is not None and parsed.get("decision") == "warn"
        has_stderr_content = bool(self.stderr and self.stderr.strip())

        if not has_warn_decision and not has_stderr_content:
            raise AssertionError(
                f"Expected warning (warn decision in stdout or content in stderr), "
                f"but found neither. stdout={self.stdout!r}, stderr={self.stderr!r}, "
                f"exit_code={self.exit_code}"
            )

        if message_contains is not None:
            # Check reason field in JSON
            reason = (parsed or {}).get("reason", "") or ""
            # Check stderr and stdout for the message
            found_in_reason = message_contains in reason
            found_in_stderr = message_contains in (self.stderr or "")
            found_in_stdout = message_contains in (self.stdout or "")

            if not (found_in_reason or found_in_stderr or found_in_stdout):
                raise AssertionError(
                    f"Expected warning message to contain {message_contains!r}, "
                    f"but not found in reason={reason!r}, stderr={self.stderr!r}, "
                    f"stdout={self.stdout!r}"
                )

    def assert_silent(self) -> None:
        """Assert the hook produced no output and exited successfully.

        Verifies exit code 0, empty stdout, and empty stderr.

        Raises:
            AssertionError: If there is any output or non-zero exit code.
        """
        if self.exit_code != 0:
            raise AssertionError(
                f"Expected exit code 0 (silent), got {self.exit_code}"
            )
        if self.stdout and self.stdout.strip():
            raise AssertionError(
                f"Expected no stdout (silent), got: {self.stdout!r}"
            )
        if self.stderr and self.stderr.strip():
            raise AssertionError(
                f"Expected no stderr (silent), got: {self.stderr!r}"
            )

    def assert_asks(self) -> None:
        """Assert the hook requested user confirmation.

        Verifies that stdout contains a JSON object with
        ``"decision": "confirm"``.

        Raises:
            AssertionError: If no confirm decision found in stdout.
        """
        parsed = self.json
        if parsed is None:
            raise AssertionError(
                f"Expected confirm decision in stdout JSON, but stdout is not valid JSON. "
                f"stdout={self.stdout!r}, stderr={self.stderr!r}, exit_code={self.exit_code}"
            )
        decision = parsed.get("decision")
        if decision != "confirm":
            raise AssertionError(
                f"Expected decision='confirm', got decision={decision!r}. "
                f"stdout={self.stdout!r}"
            )

    def __repr__(self) -> str:
        return (
            f"HookResult(exit_code={self.exit_code}, "
            f"stdout={self.stdout!r}, stderr={self.stderr!r})"
        )
