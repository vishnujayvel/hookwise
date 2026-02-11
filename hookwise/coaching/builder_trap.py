"""Builder's trap detector and mode classification for hookwise coaching.

Classifies tool usage into modes (coding, tooling, practice, prep, neutral)
and accumulates tooling time to detect when users spend too long on
meta-work (tooling, config, etc.) instead of actual coding or practice.

Alert levels:
- none: Less than 30 minutes of consecutive tooling
- yellow: 30-60 minutes (gentle nudge)
- orange: 60-90 minutes (stronger reminder)
- red: 90+ minutes (direct intervention)

Mode classification uses configurable file path patterns and tool name
patterns to categorize each tool call. Time deltas between consecutive
PostToolUse events are accumulated, with gaps >10 minutes treated as
session breaks (capped at 10 minutes).
"""

from __future__ import annotations

import fnmatch
import logging
from datetime import datetime, timezone
from typing import Any

logger = logging.getLogger("hookwise")

# Default thresholds in minutes
DEFAULT_THRESHOLDS = {"yellow": 30, "orange": 60, "red": 90}

# Maximum time delta between events before we assume a session break
MAX_DELTA_MINUTES = 10.0

# Default tooling file path patterns
DEFAULT_TOOLING_PATTERNS = [
    "~/.claude/skills/*",
    "~/.claude/hooks/*",
    "*/hookwise.yaml",
    "*/.claude/*",
]

# Default practice tool name patterns
DEFAULT_PRACTICE_TOOLS = [
    "mcp__practice-tracker__*",
]

# Default coding file extensions (used for mode detection)
CODING_EXTENSIONS = frozenset({
    ".py", ".js", ".ts", ".tsx", ".jsx", ".go", ".rs", ".java",
    ".c", ".cpp", ".h", ".hpp", ".rb", ".swift", ".kt",
    ".sh", ".bash", ".zsh", ".sql", ".html", ".css", ".scss",
})

# Default prep file path patterns
DEFAULT_PREP_PATTERNS = [
    "*/interview/*",
    "*/prep/*",
    "*/study/*",
]

# Nudge messages for each alert level
DEFAULT_TRAP_PROMPTS: dict[str, list[dict[str, str]]] = {
    "yellow": [
        {"id": "trap_y1", "text": "30 minutes on tooling. Is this moving the needle?"},
    ],
    "orange": [
        {"id": "trap_o1", "text": "60 minutes of tooling without coding. Time to refocus."},
    ],
    "red": [
        {"id": "trap_r1", "text": "90+ minutes on tooling. Stop and ask: what was my original goal?"},
    ],
}


def classify_mode(
    tool_name: str,
    tool_input: dict[str, Any],
    config: dict[str, Any],
) -> str:
    """Classify a tool call into a mode: coding, tooling, practice, prep, neutral.

    Classification priority:
    1. Practice tools (matched by tool name patterns)
    2. Tooling (matched by file path patterns)
    3. Prep (matched by file path patterns)
    4. Coding (matched by file extension or Write/Edit tool to code file)
    5. Neutral (default)

    Args:
        tool_name: The name of the tool being used.
        tool_input: The tool_input dict from the event payload.
        config: The builder_trap config section.

    Returns:
        One of: "coding", "tooling", "practice", "prep", "neutral".
    """
    # Extract file path from tool_input
    file_path = (
        tool_input.get("file_path", "")
        or tool_input.get("path", "")
        or tool_input.get("command", "")
    )
    if not isinstance(file_path, str):
        file_path = ""

    # 1. Check practice tools
    practice_tools = config.get("practice_tools", DEFAULT_PRACTICE_TOOLS)
    for pattern in practice_tools:
        if fnmatch.fnmatch(tool_name, pattern):
            return "practice"

    # 2. Check tooling patterns (file path based)
    tooling_patterns = config.get("tooling_patterns", DEFAULT_TOOLING_PATTERNS)
    for pattern in tooling_patterns:
        # Expand ~ for matching
        expanded = pattern.replace("~", "/Users/*")
        if fnmatch.fnmatch(file_path, pattern) or fnmatch.fnmatch(file_path, expanded):
            return "tooling"

    # 3. Check prep patterns
    prep_patterns = config.get("prep_patterns", DEFAULT_PREP_PATTERNS)
    for pattern in prep_patterns:
        if fnmatch.fnmatch(file_path, pattern):
            return "prep"

    # 4. Check coding (file extension heuristic)
    if file_path:
        # Extract extension
        dot_idx = file_path.rfind(".")
        if dot_idx >= 0:
            ext = file_path[dot_idx:]
            if ext in CODING_EXTENSIONS:
                return "coding"

    # Write/Edit tools without matching patterns default to coding
    if tool_name in ("Write", "Edit", "NotebookEdit"):
        return "coding"

    return "neutral"


def compute_alert_level(
    tooling_minutes: float,
    thresholds: dict[str, int] | None = None,
) -> str:
    """Compute the alert level based on accumulated tooling minutes.

    Args:
        tooling_minutes: Total accumulated tooling minutes.
        thresholds: Dict with yellow/orange/red minute thresholds.
            Defaults to {"yellow": 30, "orange": 60, "red": 90}.

    Returns:
        One of: "none", "yellow", "orange", "red".
    """
    if thresholds is None:
        thresholds = DEFAULT_THRESHOLDS

    red = thresholds.get("red", 90)
    orange = thresholds.get("orange", 60)
    yellow = thresholds.get("yellow", 30)

    if tooling_minutes >= red:
        return "red"
    elif tooling_minutes >= orange:
        return "orange"
    elif tooling_minutes >= yellow:
        return "yellow"
    return "none"


def accumulate_tooling_time(
    cache: dict[str, Any],
    mode: str,
    now: datetime,
) -> dict[str, Any]:
    """Accumulate tooling time and update cache state.

    For tooling mode: adds the time delta since the last event
    (capped at MAX_DELTA_MINUTES) to tooling_minutes.

    For practice or coding mode: resets the tooling timer.

    Handles date changes by resetting daily counters.

    Args:
        cache: The coaching cache dict (mutated in place).
        mode: The classified mode for the current event.
        now: The current timestamp.

    Returns:
        The updated cache dict.
    """
    today = now.strftime("%Y-%m-%d")

    # Reset daily counters on date change
    if cache.get("today_date") != today:
        cache["today_date"] = today
        cache["tooling_minutes"] = 0.0
        cache["practice_count"] = 0
        cache["alert_level"] = "none"
        cache["prompt_history"] = cache.get("prompt_history", [])

    # Calculate time delta from last event
    last_at = cache.get("mode_started_at")
    delta_minutes = 0.0
    if last_at:
        try:
            last_dt = datetime.fromisoformat(last_at)
            delta = (now - last_dt).total_seconds() / 60.0
            delta_minutes = min(delta, MAX_DELTA_MINUTES)
            if delta_minutes < 0:
                delta_minutes = 0.0
        except (ValueError, TypeError):
            delta_minutes = 0.0

    # Update based on mode
    if mode == "tooling":
        # Only accumulate if previous mode was also tooling
        if cache.get("current_mode") == "tooling" and delta_minutes > 0:
            cache["tooling_minutes"] = cache.get("tooling_minutes", 0.0) + delta_minutes
    elif mode in ("practice", "coding"):
        # Reset tooling timer on productive activity
        cache["tooling_minutes"] = 0.0
        cache["alert_level"] = "none"
        if mode == "practice":
            cache["practice_count"] = cache.get("practice_count", 0) + 1

    # Update current mode and timestamp
    cache["current_mode"] = mode
    cache["mode_started_at"] = now.isoformat()

    return cache


def select_trap_nudge(
    alert_level: str,
    prompt_history: list[str],
    prompts: dict[str, list[dict[str, str]]] | None = None,
) -> dict[str, str] | None:
    """Select a nudge message for the current alert level.

    Avoids repeating prompts that are in the recent history.

    Args:
        alert_level: Current alert level (none/yellow/orange/red).
        prompt_history: List of recently shown prompt IDs.
        prompts: Override prompt pool. Defaults to DEFAULT_TRAP_PROMPTS.

    Returns:
        Dict with "id" and "text" keys, or None if no nudge needed.
    """
    if alert_level == "none":
        return None

    if prompts is None:
        prompts = DEFAULT_TRAP_PROMPTS

    candidates = prompts.get(alert_level, [])
    if not candidates:
        return None

    recent = set(prompt_history[-3:]) if prompt_history else set()

    # Prefer prompts not recently shown
    for prompt in candidates:
        if prompt["id"] not in recent:
            return prompt

    # All were recently shown -- use first one anyway
    return candidates[0]
