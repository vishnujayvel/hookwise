"""Coaching hooks for Claude Code.

Provides coaching-type hooks that offer guidance and suggestions
during Claude Code sessions. The coaching system includes:

- **Builder's trap detector**: Monitors time spent on tooling/meta-work
  vs. actual coding or practice, nudging users when they fall into the
  "builder's trap" of endlessly refining tools instead of using them.

- **Metacognition reminders**: Time-gated prompts encouraging the user
  to pause and reflect on their approach, problem framing, and goals.

- **Rapid acceptance detection**: Flags when users accept large
  AI-generated changes very quickly without apparent review.

- **Communication coach**: Analyzes user prompts for grammar issues
  (designed for voice-to-text), providing gentle corrections.

Event handling:
- PostToolUse: Classify mode, accumulate tooling time, check alerts,
  check metacognition interval, track large changes
- UserPromptSubmit: Check rapid acceptance, run communication coach

All coaching output is delivered via additionalContext in the handler
result. The coaching state is persisted to a JSON cache file at
``~/.hookwise/state/coaching-cache.json``.
"""

from __future__ import annotations

import logging
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from hookwise.state import atomic_write_json, get_state_dir, safe_read_json

from hookwise.coaching.builder_trap import (
    accumulate_tooling_time,
    classify_mode,
    compute_alert_level,
    select_trap_nudge,
    DEFAULT_TRAP_PROMPTS,
)
from hookwise.coaching.metacognition import (
    check_metacognition_interval,
    select_metacognition_prompt,
    DEFAULT_RAPID_ACCEPTANCE_PROMPTS,
)
from hookwise.coaching.communication import analyze_prompt

logger = logging.getLogger("hookwise")

# Default cache file location
CACHE_FILENAME = "coaching-cache.json"

# Large change threshold in lines
LARGE_CHANGE_LINES = 50

# Rapid acceptance window in seconds
RAPID_ACCEPTANCE_SECONDS = 5.0

# Write/Edit tools that produce file changes
WRITE_TOOLS = frozenset({"Write", "Edit", "NotebookEdit"})


def _get_cache_path() -> Path:
    """Return the coaching cache file path."""
    return get_state_dir() / "state" / CACHE_FILENAME


def _load_cache() -> dict[str, Any]:
    """Load the coaching cache from disk.

    Returns a default cache structure if the file doesn't exist
    or is malformed.

    Returns:
        The coaching cache dict.
    """
    default: dict[str, Any] = {
        "last_prompt_at": None,
        "prompt_history": [],
        "current_mode": "neutral",
        "mode_started_at": None,
        "tooling_minutes": 0.0,
        "alert_level": "none",
        "today_date": None,
        "practice_count": 0,
        "last_large_change": None,
        "prompt_check_counter": 0,
        "grammar_scores": [],
    }
    cache = safe_read_json(_get_cache_path(), default=default)
    # Ensure all expected keys exist
    for key, val in default.items():
        if key not in cache:
            cache[key] = val
    return cache


def _save_cache(cache: dict[str, Any]) -> None:
    """Save the coaching cache to disk atomically.

    Args:
        cache: The coaching cache dict to persist.
    """
    try:
        atomic_write_json(_get_cache_path(), cache)
    except Exception as exc:
        logger.debug("Failed to save coaching cache: %s", exc)


class CoachingEngine:
    """Facade for the hookwise coaching subsystem.

    Coordinates builder's trap detection, metacognition reminders,
    rapid acceptance detection, and communication coaching.

    Usage::

        engine = CoachingEngine(config.coaching)
        result = engine.process_event("PostToolUse", payload)
        if result:
            print(result)  # additionalContext string
    """

    def __init__(self, coaching_config: dict[str, Any]) -> None:
        """Initialize with the coaching config section.

        Args:
            coaching_config: The ``coaching`` dict from HooksConfig.
        """
        self._config = coaching_config
        self._cache = _load_cache()

    def process_post_tool_use(
        self,
        payload: dict[str, Any],
        now: datetime | None = None,
    ) -> str | None:
        """Process a PostToolUse event through coaching checks.

        Steps:
        1. Classify the tool call mode
        2. Accumulate tooling time
        3. Compute alert level and select nudge
        4. Check metacognition interval
        5. Track large changes for rapid acceptance detection
        6. Save cache

        Args:
            payload: The event payload dict.
            now: Current timestamp (for testing). Uses UTC now if None.

        Returns:
            additionalContext string if a coaching prompt should be
            emitted, or None if no action needed.
        """
        if now is None:
            now = datetime.now(timezone.utc)

        tool_name = payload.get("tool_name", "")
        tool_input = payload.get("tool_input", {})
        tool_output = payload.get("tool_output", {})
        if not isinstance(tool_input, dict):
            tool_input = {}
        if not isinstance(tool_output, dict):
            tool_output = {}

        bt_config = self._config.get("builder_trap", {})
        meta_config = self._config.get("metacognition", {})

        context_parts: list[str] = []

        # 1. Classify mode
        mode = classify_mode(tool_name, tool_input, bt_config)

        # 2. Accumulate tooling time
        accumulate_tooling_time(self._cache, mode, now)

        # 3. Check builder's trap alert
        if bt_config.get("enabled", True):
            tooling_minutes = self._cache.get("tooling_minutes", 0.0)
            thresholds = bt_config.get("thresholds")
            new_level = compute_alert_level(tooling_minutes, thresholds)
            old_level = self._cache.get("alert_level", "none")

            # Only emit nudge when level escalates
            if new_level != "none" and new_level != old_level:
                prompt_history = self._cache.get("prompt_history", [])
                nudge = select_trap_nudge(new_level, prompt_history)
                if nudge:
                    context_parts.append(f"[Coaching] {nudge['text']}")
                    prompt_history.append(nudge["id"])
                    self._cache["prompt_history"] = prompt_history[-10:]

            self._cache["alert_level"] = new_level

        # 4. Check metacognition interval
        if meta_config.get("enabled", True) and not context_parts:
            if check_metacognition_interval(self._cache, now, meta_config):
                current_mode = self._cache.get("current_mode", "neutral")
                prompt = select_metacognition_prompt(
                    self._cache, current_mode, meta_config,
                )
                if prompt:
                    context_parts.append(f"[Coaching] {prompt['text']}")
                    prompt_history = self._cache.get("prompt_history", [])
                    prompt_history.append(prompt["id"])
                    self._cache["prompt_history"] = prompt_history[-10:]
                    self._cache["last_prompt_at"] = now.isoformat()

        # 5. Track large changes for rapid acceptance detection
        if tool_name in WRITE_TOOLS:
            lines_added = _extract_lines(tool_output, "lines_added")
            lines_removed = _extract_lines(tool_output, "lines_removed")
            total_lines = lines_added + lines_removed
            if total_lines > LARGE_CHANGE_LINES:
                self._cache["last_large_change"] = {
                    "timestamp": now.isoformat(),
                    "lines": total_lines,
                    "tool_name": tool_name,
                }
            else:
                # Small change clears the large change tracker
                self._cache["last_large_change"] = None

        # Save cache
        _save_cache(self._cache)

        if context_parts:
            return "\n".join(context_parts)
        return None

    def process_user_prompt(
        self,
        payload: dict[str, Any],
        now: datetime | None = None,
    ) -> str | None:
        """Process a UserPromptSubmit event through coaching checks.

        Steps:
        1. Check rapid acceptance (large change + quick accept)
        2. Run communication coach
        3. Save cache

        Args:
            payload: The event payload dict.
            now: Current timestamp (for testing). Uses UTC now if None.

        Returns:
            additionalContext string if a coaching prompt should be
            emitted, or None if no action needed.
        """
        if now is None:
            now = datetime.now(timezone.utc)

        context_parts: list[str] = []

        # 1. Check rapid acceptance
        last_large = self._cache.get("last_large_change")
        if last_large and isinstance(last_large, dict):
            try:
                change_ts = datetime.fromisoformat(last_large["timestamp"])
                elapsed = (now - change_ts).total_seconds()
                if 0 <= elapsed <= RAPID_ACCEPTANCE_SECONDS:
                    rapid_prompts = DEFAULT_RAPID_ACCEPTANCE_PROMPTS
                    if rapid_prompts:
                        context_parts.append(
                            f"[Coaching] {rapid_prompts[0]['text']}"
                        )
            except (ValueError, TypeError, KeyError):
                pass
            # Clear the large change tracker after checking
            self._cache["last_large_change"] = None

        # 2. Communication coach
        comm_config = self._config.get("communication", {})
        prompt_content = payload.get("content", "")
        if isinstance(prompt_content, str) and comm_config.get("enabled", False):
            result = analyze_prompt(prompt_content, self._cache, comm_config)
            if result and result.get("corrections"):
                corrections_text = "\n".join(result["corrections"])
                context_parts.append(
                    f"[Communication Coach]\n{corrections_text}"
                )

        # Save cache
        _save_cache(self._cache)

        if context_parts:
            return "\n".join(context_parts)
        return None


def _extract_lines(output: dict[str, Any], key: str) -> int:
    """Safely extract a line count from tool output.

    Args:
        output: The tool_output dict.
        key: The key to extract (e.g., "lines_added").

    Returns:
        Integer line count, or 0 if not available.
    """
    value = output.get(key, 0)
    if isinstance(value, int):
        return value
    if isinstance(value, str):
        try:
            return int(value)
        except ValueError:
            return 0
    return 0


# ---------------------------------------------------------------------------
# Builtin handler entry point
# ---------------------------------------------------------------------------


def handle(
    event_type: str,
    payload: dict[str, Any],
    config: Any,
) -> dict[str, Any] | None:
    """Builtin handler entry point for the coaching engine.

    Called by the dispatcher when a builtin handler references the
    ``hookwise.coaching`` module. Routes events to the CoachingEngine
    for processing.

    This handler returns additionalContext when a coaching prompt
    should be shown to the user. Returns None for no-op events.

    Args:
        event_type: The hook event type.
        payload: The event payload dict from stdin.
        config: The HooksConfig instance.

    Returns:
        Dict with ``additionalContext`` if coaching is active,
        or None if no coaching action is needed.
    """
    try:
        coaching_config = getattr(config, "coaching", {})
        if not coaching_config:
            return None

        engine = CoachingEngine(coaching_config)

        result: str | None = None

        if event_type == "PostToolUse":
            result = engine.process_post_tool_use(payload)
        elif event_type == "UserPromptSubmit":
            result = engine.process_user_prompt(payload)

        if result:
            return {"additionalContext": result}

        return None

    except Exception as exc:
        # Fail-open: never let coaching crash the hook
        logger.error("Coaching handle() failed: %s", exc)
        return None
