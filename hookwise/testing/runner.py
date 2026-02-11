"""Subprocess-based hook test runner.

Executes a hook script in a subprocess with a simulated stdin JSON
payload and captures the result as a structured HookResult. This
allows testing hook scripts in isolation without requiring a full
hookwise dispatch pipeline.

Usage::

    runner = HookRunner("python my_hook.py")
    result = runner.run("PreToolUse", {"tool_name": "Bash", "tool_input": {"command": "ls"}})
    result.assert_blocked(reason_contains="Bash blocked")

Or as a one-shot convenience::

    result = HookRunner.execute(
        "hookwise dispatch PreToolUse",
        event_type="PreToolUse",
        payload={"tool_name": "Bash"},
    )
    result.assert_allowed()
"""

from __future__ import annotations

import json
import subprocess
from typing import Any

from hookwise.testing.result import HookResult


class HookRunner:
    """Execute hook scripts in a subprocess for testing.

    Pipes a JSON payload to the hook command's stdin and captures
    stdout, stderr, and exit code into a HookResult.

    Args:
        hook_command: The command to run (e.g., ``"python my_hook.py"``
            or ``"hookwise dispatch PreToolUse"``).
        timeout: Default timeout in seconds for subprocess execution.
            Defaults to 30.
    """

    def __init__(self, hook_command: str, *, timeout: int = 30) -> None:
        self.hook_command = hook_command
        self.timeout = timeout

    def run(
        self,
        event_type: str,
        payload: dict[str, Any] | None = None,
        *,
        timeout: int | None = None,
    ) -> HookResult:
        """Execute the hook command with a simulated event payload.

        Pipes ``{"event_type": event_type, "payload": payload}`` as
        JSON to the subprocess's stdin. Captures stdout, stderr, and
        exit code.

        Args:
            event_type: The hook event type (e.g., ``"PreToolUse"``).
            payload: The event payload dict. Defaults to empty dict.
            timeout: Override the default timeout for this execution.

        Returns:
            A HookResult with captured output and exit code.

        Raises:
            subprocess.TimeoutExpired: If the command exceeds the timeout.
        """
        if payload is None:
            payload = {}

        effective_timeout = timeout if timeout is not None else self.timeout
        input_data = json.dumps({"event_type": event_type, "payload": payload})

        try:
            proc = subprocess.run(
                self.hook_command,
                shell=True,  # noqa: S602
                input=input_data,
                capture_output=True,
                text=True,
                timeout=effective_timeout,
            )
        except subprocess.TimeoutExpired as exc:
            # Return a result that captures the timeout as stderr
            return HookResult(
                stdout=exc.stdout or "" if isinstance(exc.stdout, str) else "",
                stderr=f"TIMEOUT: Command timed out after {effective_timeout}s",
                exit_code=-1,
            )

        return HookResult(
            stdout=proc.stdout or "",
            stderr=proc.stderr or "",
            exit_code=proc.returncode,
        )

    @staticmethod
    def execute(
        hook_command: str,
        event_type: str,
        payload: dict[str, Any] | None = None,
        *,
        timeout: int = 30,
    ) -> HookResult:
        """One-shot convenience method for running a hook command.

        Creates a temporary HookRunner and executes the command.
        Useful when you only need a single invocation.

        Args:
            hook_command: The command to run.
            event_type: The hook event type.
            payload: The event payload dict.
            timeout: Timeout in seconds.

        Returns:
            A HookResult with captured output and exit code.
        """
        runner = HookRunner(hook_command, timeout=timeout)
        return runner.run(event_type, payload, timeout=timeout)
