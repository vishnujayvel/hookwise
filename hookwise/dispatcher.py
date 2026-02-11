"""Hook event dispatcher for hookwise.

Routes incoming hook events to the appropriate handlers based on
configuration. Implements a three-phase execution model:

Phase 1 (Blocking Guards):
    Evaluated for guard-phase handlers. First block decision
    short-circuits -- no further handlers are executed.

Phase 2 (Context Injection):
    Handlers that return additionalContext. All context strings
    are collected and merged into the stdout JSON response.

Phase 3 (Non-Blocking Side Effects):
    Analytics, coaching state, sounds. Run serially after stdout
    is flushed. No threading in v1.

Error boundary:
    All phases are wrapped in fail-open error handling. Any unhandled
    exception results in exit code 0 so Claude Code continues normally.
    The only exception is an explicit guard block (exit code 2).
"""

from __future__ import annotations

import json
import logging
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from hookwise.config import (
    ConfigEngine,
    HooksConfig,
    ResolvedHandler,
    VALID_EVENT_TYPES,
)
from hookwise.guards import GuardEngine, parse_guard_rules

logger = logging.getLogger("hookwise")


@dataclass
class HandlerResult:
    """Result from executing a single handler.

    Attributes:
        decision: Guard decision: "block", "warn", "confirm", or None.
        reason: Human-readable reason for the decision.
        additional_context: Context string to inject into the response.
        output: Arbitrary output dict from the handler.
    """

    decision: str | None = None
    reason: str | None = None
    additional_context: str | None = None
    output: dict[str, Any] | None = None


@dataclass
class DispatchResult:
    """Result from dispatching an event through all handler phases.

    Attributes:
        stdout: JSON string to emit to stdout (or None for no output).
        stderr: String to emit to stderr (or None).
        exit_code: Process exit code. 0 = success/allow, 2 = block.
    """

    stdout: str | None = None
    stderr: str | None = None
    exit_code: int = 0


class _HandlerTimeoutError(Exception):
    """Raised when a handler exceeds its configured timeout."""


def _execute_script_handler(
    handler: ResolvedHandler,
    event_type: str,
    payload: dict[str, Any],
) -> HandlerResult:
    """Execute a script-type handler as a subprocess.

    The handler's command is executed with the event payload piped
    to its stdin as JSON. The handler's stdout is parsed as JSON;
    if parsing fails, the raw output is captured in HandlerResult.output.

    Handler exit codes:
    - 0: Success, parse stdout as result
    - 2: Hard block -- only honored if stdout contains valid JSON
          with decision="block"; otherwise treated as error (exit 0)
    - Other: Treated as error, logged, continue

    Args:
        handler: Resolved script handler with a command attribute.
        event_type: The event type string.
        payload: The event payload dict.

    Returns:
        HandlerResult parsed from the handler's stdout.

    Raises:
        _HandlerTimeoutError: If the handler exceeds its timeout.
        RuntimeError: If the handler command is missing.
    """
    if not handler.command:
        raise RuntimeError(f"Script handler {handler.name!r} has no command")

    input_data = json.dumps({"event_type": event_type, "payload": payload})

    try:
        proc = subprocess.run(
            handler.command,
            shell=True,  # noqa: S602
            input=input_data,
            capture_output=True,
            text=True,
            timeout=handler.timeout,
        )
    except subprocess.TimeoutExpired:
        raise _HandlerTimeoutError(
            f"Handler {handler.name!r} timed out after {handler.timeout}s"
        )

    stdout_text = proc.stdout.strip() if proc.stdout else ""
    stderr_text = proc.stderr.strip() if proc.stderr else ""

    if stderr_text:
        logger.debug("Handler %r stderr: %s", handler.name, stderr_text)

    # Parse stdout as JSON
    result = HandlerResult()
    if stdout_text:
        try:
            parsed = json.loads(stdout_text)
            if isinstance(parsed, dict):
                result.decision = parsed.get("decision")
                result.reason = parsed.get("reason")
                result.additional_context = parsed.get("additionalContext")
                result.output = parsed
            else:
                result.output = {"raw": stdout_text}
        except json.JSONDecodeError:
            result.output = {"raw": stdout_text}

    # Handle exit codes
    if proc.returncode == 2:
        # Hard block -- only if stdout has valid block decision
        if result.decision == "block":
            return result
        else:
            logger.warning(
                "Handler %r exited with code 2 but stdout does not contain "
                "a valid block decision, treating as error",
                handler.name,
            )
            result.decision = None
    elif proc.returncode != 0:
        logger.warning(
            "Handler %r exited with code %d, treating as error",
            handler.name, proc.returncode,
        )
        result.decision = None

    return result


def _execute_inline_handler(
    handler: ResolvedHandler,
    event_type: str,
    payload: dict[str, Any],
) -> HandlerResult:
    """Execute an inline-type handler.

    Inline handlers have their action defined directly in the config.
    Currently supports:
    - action.type == "context": Returns additionalContext from the action
    - action.type == "block": Returns a block decision

    Args:
        handler: Resolved inline handler with an action attribute.
        event_type: The event type string.
        payload: The event payload dict.

    Returns:
        HandlerResult derived from the inline action definition.
    """
    action = handler.action or {}
    result = HandlerResult()

    action_type = action.get("type", "context")
    if action_type == "context":
        result.additional_context = action.get("message")
    elif action_type == "block":
        result.decision = "block"
        result.reason = action.get("reason", "Blocked by inline handler")
    elif action_type == "warn":
        result.decision = "warn"
        result.reason = action.get("reason")
    elif action_type == "confirm":
        result.decision = "confirm"
        result.reason = action.get("reason")

    result.output = action
    return result


def _execute_builtin_handler(
    handler: ResolvedHandler,
    event_type: str,
    payload: dict[str, Any],
    config: HooksConfig,
) -> HandlerResult:
    """Execute a builtin-type handler by importing and calling its module.

    Builtin handlers reference a Python module path that must expose
    a ``handle(event_type, payload, config)`` function returning a
    HandlerResult (or None).

    Args:
        handler: Resolved builtin handler with a module attribute.
        event_type: The event type string.
        payload: The event payload dict.
        config: The full HooksConfig for handler access.

    Returns:
        HandlerResult from the builtin module, or empty result on import failure.
    """
    if not handler.module:
        logger.warning("Builtin handler %r has no module path", handler.name)
        return HandlerResult()

    try:
        import importlib

        mod = importlib.import_module(handler.module)
        handle_fn = getattr(mod, "handle", None)
        if handle_fn is None:
            logger.warning(
                "Builtin module %r has no handle() function", handler.module,
            )
            return HandlerResult()
        result = handle_fn(event_type, payload, config)
        if result is None:
            return HandlerResult()
        if isinstance(result, HandlerResult):
            return result
        # If the handler returned a dict, wrap it
        if isinstance(result, dict):
            return HandlerResult(
                decision=result.get("decision"),
                reason=result.get("reason"),
                additional_context=result.get("additionalContext"),
                output=result,
            )
        return HandlerResult()
    except Exception as exc:
        logger.error("Builtin handler %r failed: %s", handler.name, exc)
        return HandlerResult()


def execute_handler(
    handler: ResolvedHandler,
    event_type: str,
    payload: dict[str, Any],
    config: HooksConfig,
) -> HandlerResult:
    """Execute a single handler with error boundary and timeout.

    Dispatches to the appropriate execution function based on handler type.
    All exceptions are caught, logged, and result in an empty HandlerResult
    (fail-open). Timeouts are enforced for script handlers.

    Args:
        handler: The resolved handler to execute.
        event_type: The event type string.
        payload: The event payload dict.
        config: The full HooksConfig.

    Returns:
        HandlerResult from the handler, or empty result on any failure.
    """
    try:
        if handler.handler_type == "script":
            return _execute_script_handler(handler, event_type, payload)
        elif handler.handler_type == "inline":
            return _execute_inline_handler(handler, event_type, payload)
        elif handler.handler_type == "builtin":
            return _execute_builtin_handler(handler, event_type, payload, config)
        else:
            logger.warning("Unknown handler type %r", handler.handler_type)
            return HandlerResult()
    except _HandlerTimeoutError as exc:
        logger.error("Handler timeout: %s", exc)
        return HandlerResult()
    except Exception as exc:
        logger.error(
            "Handler %r raised %s: %s", handler.name, type(exc).__name__, exc,
        )
        return HandlerResult()


class Dispatcher:
    """Three-phase event dispatch engine for hookwise.

    Receives an event type and payload, loads the configuration,
    resolves matching handlers, and executes them in three phases:

    1. Guards (blocking) -- first block short-circuits
    2. Context injection -- collects additionalContext
    3. Side effects -- runs serially after stdout is flushed

    Usage::

        dispatcher = Dispatcher(config_engine=ConfigEngine())
        result = dispatcher.dispatch("PreToolUse", payload, config=config)
        # result.stdout contains JSON for Claude Code
    """

    def __init__(self, config_engine: ConfigEngine | None = None) -> None:
        """Initialize the dispatcher.

        Args:
            config_engine: ConfigEngine to use for loading config and
                resolving handlers. If None, creates a new instance.
        """
        self._config_engine = config_engine or ConfigEngine()

    def dispatch(
        self,
        event_type: str,
        payload: dict[str, Any],
        *,
        config: HooksConfig | None = None,
        project_dir: str | Path | None = None,
    ) -> DispatchResult:
        """Main entry point for hook event dispatch.

        Executes the three-phase handler pipeline and aggregates all
        outputs into a single DispatchResult.

        Args:
            event_type: One of the 13 Claude Code hook event types.
            payload: The event payload dict (parsed from stdin JSON).
            config: Pre-loaded config. If None, loads from disk.
            project_dir: Project directory for config loading.

        Returns:
            DispatchResult with stdout JSON, stderr, and exit code.
        """
        # Load config if not provided
        if config is None:
            config = self._config_engine.load_config(project_dir)

        # Get handlers for this event type
        handlers = self._config_engine.get_handlers_for_event(config, event_type)

        # Check for match/action style guard rules (evaluated by guard engine)
        has_guard_rules = any(
            isinstance(g, dict) and "match" in g
            for g in config.guards
        )

        if not handlers and not has_guard_rules:
            # No handlers and no guard rules for this event -- exit silently
            return DispatchResult()

        # Separate handlers by phase
        guard_handlers = [h for h in handlers if h.phase == "guard"]
        context_handlers = [h for h in handlers if h.phase == "context"]
        side_effect_handlers = [h for h in handlers if h.phase == "side_effect"]

        # --- Phase 1: Blocking Guards ---
        block_result = self._phase_guards(guard_handlers, event_type, payload, config)
        if block_result is not None:
            return block_result

        # --- Phase 2: Context Injection ---
        context_parts = self._phase_context(
            context_handlers, event_type, payload, config,
        )

        # Build stdout JSON from context results
        stdout_data: dict[str, Any] = {}
        if context_parts:
            # Merge all additionalContext strings
            stdout_data["additionalContext"] = "\n".join(context_parts)

        # Flush stdout before side effects
        stdout_json = json.dumps(stdout_data) if stdout_data else None

        # --- Phase 3: Non-Blocking Side Effects ---
        self._phase_side_effects(side_effect_handlers, event_type, payload, config)

        return DispatchResult(stdout=stdout_json, exit_code=0)

    def _phase_guards(
        self,
        handlers: list[ResolvedHandler],
        event_type: str,
        payload: dict[str, Any],
        config: HooksConfig,
    ) -> DispatchResult | None:
        """Phase 1: Execute guard handlers.

        First evaluates the builtin guard_rails engine against
        ``config.guards`` rules (match/action/when/unless format).
        Then evaluates guard-phase handlers in order. The first
        block/warn/confirm decision short-circuits where appropriate.

        Args:
            handlers: Guard-phase handlers for this event.
            event_type: The event type.
            payload: The event payload.
            config: The full config.

        Returns:
            DispatchResult if a guard blocks (exit_code=2), None otherwise.
        """
        # --- Builtin guard_rails engine ---
        guard_result = self._evaluate_guard_rules(event_type, payload, config)
        if guard_result is not None:
            return guard_result

        # --- User-defined guard-phase handlers ---
        for handler in handlers:
            result = execute_handler(handler, event_type, payload, config)
            if result.decision == "block":
                block_output = {
                    "decision": "block",
                    "reason": result.reason or "Blocked by guard",
                }
                return DispatchResult(
                    stdout=json.dumps(block_output),
                    exit_code=2,
                )
        return None

    def _evaluate_guard_rules(
        self,
        event_type: str,
        payload: dict[str, Any],
        config: HooksConfig,
    ) -> DispatchResult | None:
        """Evaluate the builtin guard_rails engine against config.guards rules.

        Parses the match/action/when/unless style guard rules from
        ``config.guards`` and evaluates them using the GuardEngine.
        Only fires for PreToolUse events (guards gate tool calls).

        Args:
            event_type: The event type.
            payload: The event payload.
            config: The full config.

        Returns:
            DispatchResult if a guard fires (block/warn/confirm), None if allow.
        """
        if event_type != "PreToolUse":
            return None

        # Only evaluate match/action style rules (not handler-style guards)
        raw_guards = config.guards
        if not raw_guards:
            return None

        # Filter to only match/action style guard dicts (have "match" key)
        match_style_guards = [
            g for g in raw_guards
            if isinstance(g, dict) and "match" in g
        ]
        if not match_style_guards:
            return None

        rules = parse_guard_rules(match_style_guards)
        if not rules:
            return None

        tool_name = payload.get("tool_name", "")
        tool_input = payload.get("tool_input", {})
        if not isinstance(tool_input, dict):
            tool_input = {}

        try:
            engine = GuardEngine()
            result = engine.evaluate(tool_name, tool_input, rules)
        except Exception as exc:
            logger.error("Guard engine error: %s (fail-open)", exc)
            return None

        if result.action == "allow":
            return None

        output = {
            "decision": result.action,
            "reason": result.reason or f"Guard rule matched: {result.action}",
        }
        # block returns exit_code=2, warn/confirm return exit_code=0 with decision
        if result.action == "block":
            return DispatchResult(stdout=json.dumps(output), exit_code=2)
        else:
            # warn and confirm are communicated via stdout JSON
            return DispatchResult(stdout=json.dumps(output), exit_code=0)

    def _phase_context(
        self,
        handlers: list[ResolvedHandler],
        event_type: str,
        payload: dict[str, Any],
        config: HooksConfig,
    ) -> list[str]:
        """Phase 2: Execute context-injection handlers.

        Collects all additionalContext strings from handlers that
        return them.

        Args:
            handlers: Context-phase handlers for this event.
            event_type: The event type.
            payload: The event payload.
            config: The full config.

        Returns:
            List of context strings (may be empty).
        """
        context_parts: list[str] = []
        for handler in handlers:
            result = execute_handler(handler, event_type, payload, config)
            if result.additional_context:
                context_parts.append(result.additional_context)
        return context_parts

    def _phase_side_effects(
        self,
        handlers: list[ResolvedHandler],
        event_type: str,
        payload: dict[str, Any],
        config: HooksConfig,
    ) -> None:
        """Phase 3: Execute side-effect handlers serially.

        Side effects run after stdout has been flushed. Errors are
        caught and logged but do not affect the dispatch result.

        Args:
            handlers: Side-effect-phase handlers for this event.
            event_type: The event type.
            payload: The event payload.
            config: The full config.
        """
        for handler in handlers:
            execute_handler(handler, event_type, payload, config)


def read_stdin_payload() -> dict[str, Any]:
    """Read and parse event payload from stdin as JSON.

    Handles malformed input gracefully:
    - Empty stdin: Returns empty dict
    - Non-JSON input: Returns empty dict with logged warning
    - Non-dict JSON: Returns empty dict with logged warning

    Returns:
        Parsed payload dict, or empty dict on any failure.
    """
    try:
        raw = sys.stdin.read()
        if not raw or not raw.strip():
            return {}
        parsed = json.loads(raw)
        if not isinstance(parsed, dict):
            logger.warning("Stdin payload is not a JSON object, using empty payload")
            return {}
        return parsed
    except json.JSONDecodeError as exc:
        logger.warning("Malformed JSON on stdin: %s", exc)
        return {}
    except (OSError, ValueError) as exc:
        logger.warning("Error reading stdin: %s", exc)
        return {}
