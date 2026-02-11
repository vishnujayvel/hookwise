"""Cost estimation, logging, and budget enforcement for hookwise.

Phase 3 (side effect): Estimates token usage per tool call by tracking
input/output sizes, accumulates running session cost using configurable
per-model rates, and logs session cost summaries on SessionEnd/Stop events.

Phase 1 (blocking guard): Checks daily budget limits on PreToolUse events,
supporting warn-only mode (stderr warning) and enforce mode (block tool call).

Config example::

    cost_tracking:
      enabled: true
      rates: {haiku: 0.001, sonnet: 0.003, opus: 0.015}
      daily_budget: 10.00
      enforcement: warn  # warn | enforce

All cost estimates are approximate. The handler displays a disclaimer
in all cost outputs.
"""

from __future__ import annotations

import json
import logging
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from hookwise.state import atomic_write_json, get_state_dir, safe_read_json

logger = logging.getLogger("hookwise")


# ---------------------------------------------------------------------------
# Constants and defaults
# ---------------------------------------------------------------------------

COST_STATE_FILENAME = "cost-state.json"
COST_DISCLAIMER = "Cost estimates are approximate."

# Default per-model rates (USD per 1K tokens, combined input+output estimate)
DEFAULT_RATES: dict[str, float] = {
    "haiku": 0.001,
    "sonnet": 0.003,
    "opus": 0.015,
}

DEFAULT_DAILY_BUDGET = 10.00
DEFAULT_ENFORCEMENT = "warn"

# Rough chars-per-token estimate for sizing
CHARS_PER_TOKEN = 4


# ---------------------------------------------------------------------------
# State management
# ---------------------------------------------------------------------------


def _get_cost_state_path() -> Path:
    """Return the path to the cost state file."""
    state_dir = get_state_dir()
    return state_dir / "state" / COST_STATE_FILENAME


def load_cost_state() -> dict[str, Any]:
    """Load the cost tracking state from disk.

    Returns:
        Cost state dict with ``daily_costs``, ``session_costs``, ``today`` keys.
    """
    state = safe_read_json(_get_cost_state_path(), default={
        "daily_costs": {},
        "session_costs": {},
        "today": "",
        "total_today": 0.0,
    })
    state.setdefault("daily_costs", {})
    state.setdefault("session_costs", {})
    state.setdefault("today", "")
    state.setdefault("total_today", 0.0)
    return state


def save_cost_state(state: dict[str, Any]) -> None:
    """Save cost state to disk atomically.

    Args:
        state: The cost state dict.
    """
    path = _get_cost_state_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    atomic_write_json(path, state)


def reset_daily_if_needed(state: dict[str, Any]) -> None:
    """Reset daily cost totals if the date has changed.

    Args:
        state: The cost state dict (mutated in place).
    """
    today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
    if state.get("today") != today:
        state["today"] = today
        state["total_today"] = 0.0


# ---------------------------------------------------------------------------
# Cost estimation
# ---------------------------------------------------------------------------


def estimate_tokens(text: str | dict | list | Any) -> int:
    """Estimate token count from text or structured data.

    Uses a rough chars-per-token heuristic. For dicts/lists, serializes
    to JSON first.

    Args:
        text: The content to estimate tokens for.

    Returns:
        Estimated token count.
    """
    if text is None:
        return 0
    if isinstance(text, (dict, list)):
        try:
            text = json.dumps(text)
        except (TypeError, ValueError):
            text = str(text)
    elif not isinstance(text, str):
        text = str(text)
    return max(1, len(text) // CHARS_PER_TOKEN)


def detect_model(payload: dict[str, Any]) -> str:
    """Detect the model from the event payload.

    Looks for model hints in the payload and maps to a rate tier.

    Args:
        payload: The event payload.

    Returns:
        Model tier string (e.g., "sonnet", "opus", "haiku").
    """
    # Check explicit model field
    model = payload.get("model", "")
    if isinstance(model, str):
        model_lower = model.lower()
        if "opus" in model_lower:
            return "opus"
        if "haiku" in model_lower:
            return "haiku"
        if "sonnet" in model_lower:
            return "sonnet"

    # Default to sonnet as most common
    return "sonnet"


def estimate_cost(
    input_tokens: int,
    output_tokens: int,
    model: str,
    rates: dict[str, float] | None = None,
) -> float:
    """Estimate cost for a tool call based on token counts.

    Args:
        input_tokens: Estimated input token count.
        output_tokens: Estimated output token count.
        model: Model tier name.
        rates: Per-model rates (USD per 1K tokens). Uses defaults if None.

    Returns:
        Estimated cost in USD.
    """
    if rates is None:
        rates = DEFAULT_RATES

    rate = rates.get(model, rates.get("sonnet", 0.003))
    total_tokens = input_tokens + output_tokens
    return (total_tokens / 1000) * rate


def accumulate_cost(
    state: dict[str, Any],
    session_id: str,
    cost: float,
) -> float:
    """Add cost to the running session and daily totals.

    Args:
        state: The cost state dict (mutated in place).
        session_id: Current session ID.
        cost: Cost amount to add.

    Returns:
        New daily total.
    """
    reset_daily_if_needed(state)

    state["total_today"] = state.get("total_today", 0.0) + cost

    session_costs = state.setdefault("session_costs", {})
    session_costs[session_id] = session_costs.get(session_id, 0.0) + cost

    return state["total_today"]


# ---------------------------------------------------------------------------
# Budget enforcement
# ---------------------------------------------------------------------------


def check_budget(
    state: dict[str, Any],
    daily_budget: float,
    enforcement: str,
) -> dict[str, Any] | None:
    """Check if the daily budget has been exceeded.

    Args:
        state: The cost state dict.
        daily_budget: Daily budget limit in USD.
        enforcement: Either "warn" or "enforce".

    Returns:
        Dict with ``decision``/``reason`` if budget exceeded in enforce mode,
        or None if within budget or warn-only mode.
    """
    reset_daily_if_needed(state)
    total_today = state.get("total_today", 0.0)

    if total_today <= daily_budget:
        return None

    overage = total_today - daily_budget
    reason = (
        f"Daily budget of ${daily_budget:.2f} exceeded "
        f"(current: ${total_today:.2f}, over by ${overage:.2f}). "
        f"{COST_DISCLAIMER}"
    )

    if enforcement == "enforce":
        return {
            "decision": "block",
            "reason": reason,
        }

    # Warn mode: emit to stderr, don't block
    print(f"[hookwise] WARNING: {reason}", file=sys.stderr)
    return None


# ---------------------------------------------------------------------------
# Builtin handler entry point
# ---------------------------------------------------------------------------


def handle(
    event_type: str,
    payload: dict[str, Any],
    config: Any,
) -> dict[str, Any] | None:
    """Builtin handler entry point for cost estimation and budget enforcement.

    Phase 1 (PreToolUse): Check daily budget limits.
    Phase 3 (PostToolUse): Estimate and accumulate tool call cost.
    Phase 3 (Stop/SessionEnd): Log session cost summary.

    Args:
        event_type: The hook event type.
        payload: The event payload dict from stdin.
        config: The HooksConfig instance.

    Returns:
        Dict with ``decision``/``reason`` if budget enforcement blocks,
        or None for side effects.
    """
    cost_cfg = getattr(config, "cost_tracking", {})
    if not isinstance(cost_cfg, dict):
        cost_cfg = {}

    if not cost_cfg.get("enabled", False):
        return None

    supported_events = {"PreToolUse", "PostToolUse", "Stop", "SessionEnd"}
    if event_type not in supported_events:
        return None

    try:
        rates = cost_cfg.get("rates", DEFAULT_RATES)
        if not isinstance(rates, dict):
            rates = DEFAULT_RATES

        daily_budget = cost_cfg.get("daily_budget", DEFAULT_DAILY_BUDGET)
        if not isinstance(daily_budget, (int, float)):
            daily_budget = DEFAULT_DAILY_BUDGET

        enforcement = cost_cfg.get("enforcement", DEFAULT_ENFORCEMENT)
        if enforcement not in ("warn", "enforce"):
            enforcement = DEFAULT_ENFORCEMENT

        state = load_cost_state()

        # --- Phase 1: Budget enforcement on PreToolUse ---
        if event_type == "PreToolUse":
            result = check_budget(state, daily_budget, enforcement)
            return result

        # --- Phase 3: Cost estimation on PostToolUse ---
        if event_type == "PostToolUse":
            session_id = payload.get("session_id", "unknown")
            model = detect_model(payload)

            # Estimate tokens from tool input and output
            tool_input = payload.get("tool_input", {})
            tool_output = payload.get("tool_output", {})
            input_tokens = estimate_tokens(tool_input)
            output_tokens = estimate_tokens(tool_output)

            cost = estimate_cost(input_tokens, output_tokens, model, rates)
            daily_total = accumulate_cost(state, session_id, cost)
            save_cost_state(state)

            logger.debug(
                "Cost: tool=%s model=%s tokens=%d+%d cost=$%.4f daily=$%.2f",
                payload.get("tool_name", "?"),
                model,
                input_tokens,
                output_tokens,
                cost,
                daily_total,
            )
            return None

        # --- Phase 3: Summary on Stop/SessionEnd ---
        if event_type in ("Stop", "SessionEnd"):
            session_id = payload.get("session_id", "unknown")
            session_cost = state.get("session_costs", {}).get(session_id, 0.0)
            daily_total = state.get("total_today", 0.0)

            summary = (
                f"[hookwise] Session cost: ${session_cost:.2f} | "
                f"Daily total: ${daily_total:.2f} / ${daily_budget:.2f} | "
                f"{COST_DISCLAIMER}"
            )
            print(summary, file=sys.stderr)
            logger.info(summary)
            return None

    except Exception as exc:
        # Fail-open: never let cost tracking crash the hook
        logger.error("Cost handle() failed: %s", exc)

    return None
