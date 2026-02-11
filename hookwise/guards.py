"""Guard rails engine for hookwise.

Evaluates guard rules against tool call events to determine whether
actions should be blocked, warned about, confirmed, or allowed.
Implements a first-match-wins evaluation strategy with support for
exact and glob pattern matching on tool names, plus conditional
expressions on tool_input fields.

Guard rules are defined in hookwise.yaml under the ``guards`` key:

.. code-block:: yaml

    guards:
      - match: "mcp__gmail__send_email"
        action: confirm
        reason: "About to send a real email."

      - match: "Bash"
        action: block
        when: 'tool_input.command contains "force push"'
        reason: "Force push blocked."

      - match: "mcp__gmail__*"
        action: warn
        reason: "Gmail tool detected."
        unless: 'tool_input.to starts_with "test@"'

Condition expression syntax::

    <field_path> <operator> <value>

    field_path: tool_input.command, tool_input.file_path, etc.
    operator:   contains, starts_with, ends_with, matches, ==
    value:      quoted string "force push" or unquoted single word

Integration:
    The dispatcher calls ``guard_rails_handle()`` as a builtin handler
    during Phase 1 (blocking guards). The function receives the event
    payload and config, evaluates guard rules, and returns a
    HandlerResult with the appropriate decision.
"""

from __future__ import annotations

import fnmatch
import logging
import re
from dataclasses import dataclass
from typing import Any

logger = logging.getLogger("hookwise")


# ---------------------------------------------------------------------------
# Data classes
# ---------------------------------------------------------------------------


@dataclass
class GuardRule:
    """A single guard rule from the ``guards`` config list.

    Attributes:
        match: Tool name pattern -- exact string or glob (e.g., ``mcp__gmail__*``).
        action: Decision to apply: ``"block"``, ``"warn"``, or ``"confirm"``.
        reason: Human-readable message for the decision.
        when: Optional condition expression that must be true for the rule
            to fire. If present and not met, the rule is skipped.
        unless: Optional exception condition. If present and met, the rule
            is skipped (overrides the match).
    """

    match: str
    action: str
    reason: str
    when: str | None = None
    unless: str | None = None


@dataclass
class GuardResult:
    """Result of evaluating guard rules against a tool call.

    Attributes:
        action: The decision: ``"allow"``, ``"block"``, ``"warn"``, or ``"confirm"``.
        reason: Human-readable reason (None when action is ``"allow"``).
        matched_rule: The GuardRule that matched, or None if no rule matched.
    """

    action: str
    reason: str | None = None
    matched_rule: GuardRule | None = None


# ---------------------------------------------------------------------------
# Condition expression parser
# ---------------------------------------------------------------------------

# Supported operators in condition expressions
_OPERATORS = frozenset({"contains", "starts_with", "ends_with", "matches", "=="})

# Regex to parse a condition expression:
#   <field_path> <operator> <value>
# where value is either a quoted string or an unquoted single word.
_CONDITION_PATTERN = re.compile(
    r"^"
    r"(?P<field_path>[a-zA-Z_][a-zA-Z0-9_.]*)"  # e.g. tool_input.command
    r"\s+"
    r"(?P<operator>contains|starts_with|ends_with|matches|==)"
    r"\s+"
    r"(?P<value>\"[^\"]*\"|'[^']*'|\S+)"  # quoted or unquoted
    r"$"
)


def _parse_condition(expression: str) -> tuple[str, str, str] | None:
    """Parse a condition expression into (field_path, operator, value).

    Returns None if the expression is malformed. This is intentional:
    malformed conditions cause the rule to be skipped (fail-open),
    not crash the process.

    Args:
        expression: The condition string, e.g.
            ``'tool_input.command contains "force push"'``

    Returns:
        Tuple of (field_path, operator, value) with quotes stripped
        from value, or None if parsing fails.
    """
    match = _CONDITION_PATTERN.match(expression.strip())
    if match is None:
        return None

    field_path = match.group("field_path")
    operator = match.group("operator")
    value = match.group("value")

    # Strip surrounding quotes from value
    if (value.startswith('"') and value.endswith('"')) or (
        value.startswith("'") and value.endswith("'")
    ):
        value = value[1:-1]

    return field_path, operator, value


def _resolve_field_path(data: dict[str, Any], field_path: str) -> str | None:
    """Resolve a dot-notation field path against a data dict.

    Navigates nested dicts following the dot-separated path components.
    The first path segment (typically ``tool_input``) is treated as the
    root key in the data dict.

    Args:
        data: The data dict to navigate (e.g., tool_input contents or
            a dict containing ``tool_input`` as a key).
        field_path: Dot-separated path, e.g. ``tool_input.command``.

    Returns:
        The resolved value as a string, or None if the path does not
        exist or the value is not a string/number.
    """
    parts = field_path.split(".")
    current: Any = data

    for part in parts:
        if isinstance(current, dict):
            if part not in current:
                return None
            current = current[part]
        else:
            return None

    # Convert to string for comparison operators
    if current is None:
        return None
    return str(current)


def _evaluate_condition(
    expression: str, tool_input: dict[str, Any]
) -> bool | None:
    """Evaluate a condition expression against tool_input data.

    Returns True if the condition is met, False if not met, or None
    if the expression is malformed (which causes the rule to be skipped
    in fail-open fashion).

    Args:
        expression: The condition string.
        tool_input: The tool_input dict from the event payload.

    Returns:
        True if condition is met, False if not met, None if malformed.
    """
    parsed = _parse_condition(expression)
    if parsed is None:
        logger.warning("Malformed guard condition: %r (skipping rule)", expression)
        return None

    field_path, operator, value = parsed

    # Build a context dict that includes tool_input at the expected path
    context = {"tool_input": tool_input}
    field_value = _resolve_field_path(context, field_path)

    if field_value is None:
        # Field not found -- condition is not met
        return False

    if operator == "contains":
        return value in field_value
    elif operator == "starts_with":
        return field_value.startswith(value)
    elif operator == "ends_with":
        return field_value.endswith(value)
    elif operator == "==":
        return field_value == value
    elif operator == "matches":
        try:
            return bool(re.search(value, field_value))
        except re.error:
            logger.warning(
                "Invalid regex in guard condition: %r (skipping rule)", value
            )
            return None

    # Should not reach here due to regex validation, but fail-open
    return None


# ---------------------------------------------------------------------------
# Guard engine
# ---------------------------------------------------------------------------


class GuardEngine:
    """Evaluates guard rules against tool call events.

    Implements first-match-wins semantics: rules are evaluated in order,
    and the first rule whose match pattern and conditions are satisfied
    determines the result. If no rule matches, the default is to allow.

    Usage::

        engine = GuardEngine()
        rules = [
            GuardRule(match="Bash", action="block", reason="No bash"),
            GuardRule(match="mcp__gmail__*", action="warn", reason="Gmail"),
        ]
        result = engine.evaluate("Bash", {"command": "ls"}, rules)
        # result.action == "block"
    """

    def evaluate(
        self,
        tool_name: str,
        tool_input: dict[str, Any],
        rules: list[GuardRule],
    ) -> GuardResult:
        """Evaluate guard rules against a tool call. First-match-wins.

        For each rule in order:
        1. Check if tool_name matches the rule's match pattern (exact or glob)
        2. If ``when`` condition exists, evaluate it -- skip rule if not met
        3. If ``unless`` condition exists, evaluate it -- skip rule if met
        4. Return the rule's action as the result

        If no rule matches, returns ``GuardResult(action="allow")``.

        Malformed conditions cause the rule to be skipped (fail-open),
        not crash the engine. This matches hookwise's overall fail-open
        error boundary philosophy.

        Args:
            tool_name: The tool name from the event (e.g., ``"Bash"``).
            tool_input: The tool_input dict from the event payload.
            rules: Ordered list of GuardRule objects to evaluate.

        Returns:
            GuardResult with the matching rule's action, or allow.
        """
        for rule in rules:
            try:
                # Step 1: Match tool name (exact or glob)
                if not self._matches_tool(tool_name, rule.match):
                    continue

                # Step 2: Evaluate 'when' condition (if present)
                if rule.when is not None:
                    when_result = _evaluate_condition(rule.when, tool_input)
                    if when_result is None:
                        # Malformed condition -- skip rule (fail-open)
                        logger.warning(
                            "Guard rule match=%r has malformed 'when' condition, "
                            "skipping rule",
                            rule.match,
                        )
                        continue
                    if not when_result:
                        # Condition not met -- skip rule
                        continue

                # Step 3: Evaluate 'unless' condition (if present)
                if rule.unless is not None:
                    unless_result = _evaluate_condition(rule.unless, tool_input)
                    if unless_result is None:
                        # Malformed condition -- skip rule (fail-open)
                        logger.warning(
                            "Guard rule match=%r has malformed 'unless' condition, "
                            "skipping rule",
                            rule.match,
                        )
                        continue
                    if unless_result:
                        # Exception condition met -- skip rule
                        continue

                # Step 4: Rule matches -- return its action
                return GuardResult(
                    action=rule.action,
                    reason=rule.reason,
                    matched_rule=rule,
                )

            except Exception as exc:
                # Fail-open: any unexpected error skips the rule
                logger.error(
                    "Error evaluating guard rule match=%r: %s (skipping rule)",
                    rule.match, exc,
                )
                continue

        # No rule matched -- default allow
        return GuardResult(action="allow")

    def _matches_tool(self, tool_name: str, pattern: str) -> bool:
        """Check if a tool name matches a guard rule pattern.

        Supports exact match and glob patterns (using fnmatch).
        Glob characters are ``*``, ``?``, ``[seq]``, ``[!seq]``.

        Args:
            tool_name: The actual tool name.
            pattern: The match pattern from the guard rule.

        Returns:
            True if the tool name matches the pattern.
        """
        # Exact match first (fast path)
        if tool_name == pattern:
            return True
        # Glob match
        return fnmatch.fnmatch(tool_name, pattern)


# ---------------------------------------------------------------------------
# Public helpers
# ---------------------------------------------------------------------------


def parse_guard_rules(raw_guards: list[dict[str, Any]]) -> list[GuardRule]:
    """Parse raw guard config dicts into GuardRule objects.

    Malformed entries (missing ``match`` or ``action``) are skipped
    with a warning, matching hookwise's fail-open pattern.

    Args:
        raw_guards: List of guard dicts from ``config.guards``.

    Returns:
        List of valid GuardRule objects, preserving config order.
    """
    rules: list[GuardRule] = []
    for i, raw in enumerate(raw_guards):
        if not isinstance(raw, dict):
            logger.warning("Guard at index %d is not a dict, skipping", i)
            continue

        match = raw.get("match")
        action = raw.get("action")
        reason = raw.get("reason", "")

        if not match:
            logger.warning("Guard at index %d has no 'match' field, skipping", i)
            continue
        if not action:
            logger.warning("Guard at index %d has no 'action' field, skipping", i)
            continue
        if action not in ("block", "warn", "confirm"):
            logger.warning(
                "Guard at index %d has invalid action %r, skipping", i, action
            )
            continue

        rules.append(GuardRule(
            match=match,
            action=action,
            reason=reason,
            when=raw.get("when"),
            unless=raw.get("unless"),
        ))

    return rules


# ---------------------------------------------------------------------------
# Dispatcher integration: builtin handler entry point
# ---------------------------------------------------------------------------


def handle(
    event_type: str,
    payload: dict[str, Any],
    config: Any,
) -> dict[str, Any] | None:
    """Builtin handler entry point for the guard rails engine.

    Called by the dispatcher when a builtin handler references the
    ``hookwise.guards`` module. Evaluates guard rules from
    ``config.guards`` against the tool call in the payload.

    Only runs for ``PreToolUse`` events (guards are tool-call gates).
    For other event types, returns None (no-op).

    Args:
        event_type: The hook event type.
        payload: The event payload dict from stdin.
        config: The HooksConfig instance.

    Returns:
        Dict with ``decision`` and ``reason`` keys if a guard fires,
        or None if no guard matches (allow).
    """
    if event_type != "PreToolUse":
        return None

    # Extract tool name and tool_input from the payload
    tool_name = payload.get("tool_name", "")
    tool_input = payload.get("tool_input", {})
    if not isinstance(tool_input, dict):
        tool_input = {}

    # Parse guard rules from config
    raw_guards = getattr(config, "guards", [])
    if not raw_guards:
        return None

    rules = parse_guard_rules(raw_guards)
    if not rules:
        return None

    # Evaluate
    engine = GuardEngine()
    result = engine.evaluate(tool_name, tool_input, rules)

    if result.action == "allow":
        return None

    return {
        "decision": result.action,
        "reason": result.reason,
    }
