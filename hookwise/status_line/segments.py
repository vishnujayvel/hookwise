"""Built-in status line segments for hookwise.

Each segment function takes the shared cache dict and a per-segment
config dict, and returns a display string. If the cache is missing
data for a segment, it returns an empty string (composited out by
the renderer).

Segment functions:
    clock     -- Current time with configurable strftime format.
    mantra    -- Rotating motivational/metacognition prompt from cache.
    builder_trap -- Alert indicator with emoji and tooling duration.
    session   -- Duration and tool count for the current session.
    practice  -- Daily practice rep counter.
    ai_ratio  -- Running session average of AI-generated code percentage.
    cost      -- Estimated session cost in dollars.
"""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any


def clock(cache: dict[str, Any], config: dict[str, Any]) -> str:
    """Render the current time.

    Config options:
        format: strftime format string (default "%H:%M").

    Args:
        cache: Shared status line cache (unused by clock).
        config: Segment config dict.

    Returns:
        Formatted current time string.
    """
    fmt = config.get("format", "%H:%M")
    if not isinstance(fmt, str):
        fmt = "%H:%M"
    try:
        return datetime.now(timezone.utc).astimezone().strftime(fmt)
    except (ValueError, TypeError):
        return datetime.now(timezone.utc).astimezone().strftime("%H:%M")


def mantra(cache: dict[str, Any], config: dict[str, Any]) -> str:
    """Render the current mantra/motivational prompt from cache.

    Cache shape:
        mantra: {text: str, id: str}

    Args:
        cache: Shared status line cache.
        config: Segment config dict (unused).

    Returns:
        Mantra text, or empty string if not available.
    """
    mantra_data = cache.get("mantra")
    if not isinstance(mantra_data, dict):
        return ""
    text = mantra_data.get("text", "")
    if not isinstance(text, str):
        return ""
    return text


# Alert level to emoji mapping
_ALERT_EMOJIS: dict[str, str] = {
    "yellow": "\u26a0\ufe0f",   # warning sign
    "orange": "\U0001f7e0",     # orange circle
    "red": "\U0001f534",        # red circle
}


def builder_trap(cache: dict[str, Any], config: dict[str, Any]) -> str:
    """Render the builder trap alert indicator.

    Shows an emoji based on alert level plus the tooling duration
    and optional nudge message.

    Cache shape:
        builder_trap: {
            alert_level: "none"|"yellow"|"orange"|"red",
            tooling_minutes: float,
            nudge_message: str
        }

    Args:
        cache: Shared status line cache.
        config: Segment config dict (unused).

    Returns:
        Alert string like "warning 30m: Is this moving the needle?",
        or empty string if alert_level is "none" or data is missing.
    """
    trap_data = cache.get("builder_trap")
    if not isinstance(trap_data, dict):
        return ""

    alert_level = trap_data.get("alert_level", "none")
    if not isinstance(alert_level, str) or alert_level == "none":
        return ""

    emoji = _ALERT_EMOJIS.get(alert_level, "")
    if not emoji:
        return ""

    tooling_minutes = trap_data.get("tooling_minutes", 0)
    if not isinstance(tooling_minutes, (int, float)):
        tooling_minutes = 0

    minutes_display = f"{int(tooling_minutes)}m"
    nudge = trap_data.get("nudge_message", "")
    if not isinstance(nudge, str):
        nudge = ""

    if nudge:
        return f"{emoji} {minutes_display}: {nudge}"
    return f"{emoji} {minutes_display}"


def session(cache: dict[str, Any], config: dict[str, Any]) -> str:
    """Render session duration and tool count.

    Cache shape:
        session: {
            started_at: str (ISO 8601),
            tool_calls: int,
            ai_ratio: float
        }

    Args:
        cache: Shared status line cache.
        config: Segment config dict (unused).

    Returns:
        String like "45m / 47 tools", or empty if data missing.
    """
    session_data = cache.get("session")
    if not isinstance(session_data, dict):
        return ""

    started_at = session_data.get("started_at", "")
    tool_calls = session_data.get("tool_calls", 0)

    if not isinstance(started_at, str) or not started_at:
        return ""
    if not isinstance(tool_calls, int):
        try:
            tool_calls = int(tool_calls)
        except (ValueError, TypeError):
            tool_calls = 0

    # Parse started_at and compute duration
    try:
        start_dt = datetime.fromisoformat(started_at)
        now = datetime.now(timezone.utc)
        # Ensure both are offset-aware for comparison
        if start_dt.tzinfo is None:
            start_dt = start_dt.replace(tzinfo=timezone.utc)
        delta = now - start_dt
        total_minutes = int(delta.total_seconds() / 60)
        if total_minutes < 0:
            total_minutes = 0
    except (ValueError, TypeError):
        return ""

    if total_minutes >= 60:
        hours = total_minutes // 60
        mins = total_minutes % 60
        duration = f"{hours}h{mins}m"
    else:
        duration = f"{total_minutes}m"

    return f"{duration} / {tool_calls} tools"


def practice(cache: dict[str, Any], config: dict[str, Any]) -> str:
    """Render the daily practice rep counter.

    Cache shape:
        practice: {today_total: int, today_date: str}

    Args:
        cache: Shared status line cache.
        config: Segment config dict (unused).

    Returns:
        String like "reps: 2", or empty if data missing.
    """
    practice_data = cache.get("practice")
    if not isinstance(practice_data, dict):
        return ""

    today_total = practice_data.get("today_total", 0)
    if not isinstance(today_total, int):
        try:
            today_total = int(today_total)
        except (ValueError, TypeError):
            today_total = 0

    today_date = practice_data.get("today_date", "")
    if not isinstance(today_date, str) or not today_date:
        return ""

    # Only show if date matches today
    try:
        now_date = datetime.now(timezone.utc).astimezone().strftime("%Y-%m-%d")
        if today_date != now_date:
            return ""
    except (ValueError, TypeError):
        return ""

    return f"reps: {today_total}"


def ai_ratio(cache: dict[str, Any], config: dict[str, Any]) -> str:
    """Render the running AI-generated code percentage.

    Cache shape:
        session: {ai_ratio: float}

    Args:
        cache: Shared status line cache.
        config: Segment config dict (unused).

    Returns:
        String like "AI: 72%", or empty if data missing.
    """
    session_data = cache.get("session")
    if not isinstance(session_data, dict):
        return ""

    ratio = session_data.get("ai_ratio")
    if ratio is None:
        return ""
    if not isinstance(ratio, (int, float)):
        try:
            ratio = float(ratio)
        except (ValueError, TypeError):
            return ""

    percentage = int(ratio * 100)
    return f"AI: {percentage}%"


def cost(cache: dict[str, Any], config: dict[str, Any]) -> str:
    """Render the estimated session cost.

    Cache shape:
        cost: {session_tokens: int, session_cost_usd: float}

    Args:
        cache: Shared status line cache.
        config: Segment config dict (unused).

    Returns:
        String like "$0.67", or empty if data missing.
    """
    cost_data = cache.get("cost")
    if not isinstance(cost_data, dict):
        return ""

    cost_usd = cost_data.get("session_cost_usd")
    if cost_usd is None:
        return ""
    if not isinstance(cost_usd, (int, float)):
        try:
            cost_usd = float(cost_usd)
        except (ValueError, TypeError):
            return ""

    return f"${cost_usd:.2f}"


# Registry of built-in segment names to their render functions.
BUILTIN_SEGMENTS: dict[str, Any] = {
    "clock": clock,
    "mantra": mantra,
    "builder_trap": builder_trap,
    "session": session,
    "practice": practice,
    "ai_ratio": ai_ratio,
    "cost": cost,
}
