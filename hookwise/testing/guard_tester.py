"""Config-based guard rule tester.

Evaluates guard rules against synthetic tool call events without
subprocess execution. Loads rules from a YAML config file, a raw
dict, or a pre-parsed list of guard dicts, then uses the hookwise
GuardEngine to evaluate tool calls against those rules.

This enables fast, in-process testing of guard configurations:

.. code-block:: python

    tester = GuardTester(config_dict={
        "guards": [
            {"match": "Bash", "action": "block", "reason": "No bash"},
        ]
    })
    tester.assert_blocked("Bash")
    tester.assert_allowed("Read")

Or with batch scenarios:

.. code-block:: python

    results = tester.run_scenarios([
        {"tool_name": "Bash", "expected": "block"},
        {"tool_name": "Read", "expected": "allow"},
        {"tool_name": "mcp__gmail__send_email", "expected": "confirm"},
    ])
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml

from hookwise.guards import GuardEngine, GuardResult, GuardRule, parse_guard_rules


class GuardTester:
    """Test guard rules against synthetic tool calls without subprocess execution.

    Loads guard rules from one of three sources (in priority order):
    1. ``guards``: A pre-built list of guard rule dicts
    2. ``config_dict``: A full hookwise config dict with a ``guards`` key
    3. ``config_path``: Path to a YAML config file with a ``guards`` key

    Args:
        config_path: Path to a YAML config file containing guard rules.
        config_dict: A dict with a ``guards`` key containing rule dicts.
        guards: A list of guard rule dicts (match/action/reason format).

    Raises:
        ValueError: If no guard source is provided.
    """

    def __init__(
        self,
        config_path: str | Path | None = None,
        config_dict: dict[str, Any] | None = None,
        guards: list[dict[str, Any]] | None = None,
    ) -> None:
        self._engine = GuardEngine()

        if guards is not None:
            self._raw_guards = guards
        elif config_dict is not None:
            self._raw_guards = config_dict.get("guards", [])
        elif config_path is not None:
            self._raw_guards = self._load_guards_from_file(config_path)
        else:
            raise ValueError(
                "GuardTester requires at least one of: "
                "config_path, config_dict, or guards"
            )

        self._rules: list[GuardRule] = parse_guard_rules(self._raw_guards)

    @property
    def rules(self) -> list[GuardRule]:
        """The parsed guard rules."""
        return self._rules

    def test_tool_call(
        self,
        tool_name: str,
        tool_input: dict[str, Any] | None = None,
    ) -> GuardResult:
        """Evaluate guard rules against a synthetic tool call.

        Args:
            tool_name: The tool name to test (e.g., ``"Bash"``).
            tool_input: The tool_input dict. Defaults to empty dict.

        Returns:
            GuardResult with the matching rule's action, or allow.
        """
        if tool_input is None:
            tool_input = {}
        return self._engine.evaluate(tool_name, tool_input, self._rules)

    def assert_blocked(
        self,
        tool_name: str,
        tool_input: dict[str, Any] | None = None,
        reason_contains: str | None = None,
    ) -> None:
        """Assert that a tool call is blocked by the guard rules.

        Args:
            tool_name: The tool name to test.
            tool_input: The tool_input dict.
            reason_contains: If provided, assert the reason contains
                this substring.

        Raises:
            AssertionError: If the tool call is not blocked or reason
                does not match.
        """
        result = self.test_tool_call(tool_name, tool_input)
        if result.action != "block":
            raise AssertionError(
                f"Expected tool {tool_name!r} to be blocked, "
                f"got action={result.action!r}. "
                f"tool_input={tool_input!r}"
            )
        if reason_contains is not None:
            reason = result.reason or ""
            if reason_contains not in reason:
                raise AssertionError(
                    f"Expected block reason to contain {reason_contains!r}, "
                    f"got reason={reason!r}"
                )

    def assert_allowed(
        self,
        tool_name: str,
        tool_input: dict[str, Any] | None = None,
    ) -> None:
        """Assert that a tool call is allowed by the guard rules.

        Args:
            tool_name: The tool name to test.
            tool_input: The tool_input dict.

        Raises:
            AssertionError: If the tool call is not allowed.
        """
        result = self.test_tool_call(tool_name, tool_input)
        if result.action != "allow":
            raise AssertionError(
                f"Expected tool {tool_name!r} to be allowed, "
                f"got action={result.action!r}, reason={result.reason!r}. "
                f"tool_input={tool_input!r}"
            )

    def assert_warns(
        self,
        tool_name: str,
        tool_input: dict[str, Any] | None = None,
    ) -> None:
        """Assert that a tool call triggers a warning.

        Args:
            tool_name: The tool name to test.
            tool_input: The tool_input dict.

        Raises:
            AssertionError: If the tool call does not trigger a warning.
        """
        result = self.test_tool_call(tool_name, tool_input)
        if result.action != "warn":
            raise AssertionError(
                f"Expected tool {tool_name!r} to trigger a warning, "
                f"got action={result.action!r}, reason={result.reason!r}. "
                f"tool_input={tool_input!r}"
            )

    def run_scenarios(
        self,
        scenarios: list[dict[str, Any]],
    ) -> list[tuple[dict[str, Any], GuardResult, bool]]:
        """Run multiple test scenarios and return results.

        Each scenario is a dict with:
        - ``tool_name`` (str): Required. The tool name to test.
        - ``tool_input`` (dict): Optional. Defaults to ``{}``.
        - ``expected`` (str): Required. One of ``"block"``, ``"allow"``,
          ``"warn"``, ``"confirm"``.

        Args:
            scenarios: List of scenario dicts.

        Returns:
            List of tuples: ``(scenario, guard_result, passed)``.
            ``passed`` is True if the guard result action matches
            the expected action.
        """
        results: list[tuple[dict[str, Any], GuardResult, bool]] = []
        for scenario in scenarios:
            tool_name = scenario["tool_name"]
            tool_input = scenario.get("tool_input", {})
            expected = scenario["expected"]

            guard_result = self.test_tool_call(tool_name, tool_input)
            passed = guard_result.action == expected
            results.append((scenario, guard_result, passed))

        return results

    @staticmethod
    def _load_guards_from_file(config_path: str | Path) -> list[dict[str, Any]]:
        """Load guard rules from a YAML config file.

        Args:
            config_path: Path to the YAML file.

        Returns:
            List of raw guard rule dicts from the ``guards`` key.

        Raises:
            FileNotFoundError: If the config file does not exist.
            ValueError: If the file does not contain valid YAML or
                has no ``guards`` key.
        """
        path = Path(config_path)
        if not path.is_file():
            raise FileNotFoundError(f"Config file not found: {path}")

        content = path.read_text(encoding="utf-8")
        parsed = yaml.safe_load(content)

        if not isinstance(parsed, dict):
            raise ValueError(f"Config file {path} does not contain a YAML mapping")

        guards = parsed.get("guards")
        if guards is None:
            raise ValueError(f"Config file {path} has no 'guards' key")

        if not isinstance(guards, list):
            raise ValueError(f"Config file {path} 'guards' is not a list")

        return guards
