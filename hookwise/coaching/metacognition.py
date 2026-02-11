"""Metacognition reminder engine for hookwise coaching.

Provides time-gated metacognitive prompts that encourage the user
to pause and reflect during Claude Code sessions. Prompts are shown
at a configurable interval (default: 5 minutes) and avoid repeating
recent prompts.

Prompt selection weights toward mode-relevant prompts when available
and draws from a configurable prompt pool (built-in defaults or
external JSON file).
"""

from __future__ import annotations

import json
import logging
import random
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

logger = logging.getLogger("hookwise")

# Default interval between metacognition prompts (in seconds)
DEFAULT_INTERVAL_SECONDS = 300  # 5 minutes

# Default metacognition prompts (built into code)
DEFAULT_METACOGNITION_PROMPTS: list[dict[str, str]] = [
    {"id": "meta_01", "text": "Pause: Are you solving the right problem?"},
    {"id": "meta_02", "text": "One idea. One pause. Let them ask."},
    {"id": "meta_03", "text": "What would you explain to a junior dev about this change?"},
]

# Rapid acceptance prompts
DEFAULT_RAPID_ACCEPTANCE_PROMPTS: list[dict[str, str]] = [
    {"id": "rapid_01", "text": "You just accepted a large AI change very quickly. Can you explain what it does?"},
]


def load_prompts_file(prompts_file: str | None) -> dict[str, Any] | None:
    """Load prompts from an external JSON file.

    The file should contain a JSON object with keys like
    "metacognition", "builder_trap", "rapid_acceptance", each
    containing a list of prompt dicts with "id" and "text".

    Args:
        prompts_file: Path to the JSON prompts file (may contain ~).
            If None, returns None.

    Returns:
        Parsed dict from the file, or None if the file doesn't exist
        or is malformed.
    """
    if not prompts_file:
        return None

    try:
        path = Path(prompts_file).expanduser()
        if not path.is_file():
            return None
        content = path.read_text(encoding="utf-8")
        data = json.loads(content)
        if isinstance(data, dict):
            return data
        return None
    except (OSError, json.JSONDecodeError, ValueError) as exc:
        logger.debug("Could not load prompts file %s: %s", prompts_file, exc)
        return None


def check_metacognition_interval(
    cache: dict[str, Any],
    now: datetime,
    config: dict[str, Any],
) -> bool:
    """Check whether enough time has passed for a metacognition prompt.

    Args:
        cache: The coaching cache dict.
        now: Current timestamp.
        config: The metacognition config section.

    Returns:
        True if the interval has elapsed, False otherwise.
    """
    if not config.get("enabled", True):
        return False

    interval = config.get("interval_seconds", DEFAULT_INTERVAL_SECONDS)

    last_prompt_at = cache.get("last_prompt_at")
    if not last_prompt_at:
        # No prompt has been shown yet -- show one
        return True

    try:
        last_dt = datetime.fromisoformat(last_prompt_at)
        elapsed = (now - last_dt).total_seconds()
        return elapsed >= interval
    except (ValueError, TypeError):
        # Malformed timestamp -- show prompt
        return True


def select_metacognition_prompt(
    cache: dict[str, Any],
    current_mode: str,
    config: dict[str, Any],
) -> dict[str, str] | None:
    """Select a metacognition prompt, avoiding recent repeats.

    Weights toward mode-relevant prompts when available in the pool.
    Falls back to any available prompt if no mode-specific ones exist.

    Args:
        cache: The coaching cache dict.
        current_mode: The current classified mode.
        config: The metacognition config section.

    Returns:
        Dict with "id" and "text", or None if no prompts available.
    """
    # Load prompts from file or use defaults
    prompts_file = config.get("prompts_file")
    external_prompts = load_prompts_file(prompts_file)

    if external_prompts and "metacognition" in external_prompts:
        pool = external_prompts["metacognition"]
    else:
        pool = DEFAULT_METACOGNITION_PROMPTS

    if not pool:
        return None

    # Get recent prompt IDs to avoid
    prompt_history = cache.get("prompt_history", [])
    recent = set(prompt_history[-3:]) if prompt_history else set()

    # Filter out recently shown prompts
    candidates = [p for p in pool if p.get("id") not in recent]

    # If all filtered out, use full pool
    if not candidates:
        candidates = list(pool)

    # Weight toward mode-relevant prompts if they have a "modes" field
    mode_relevant = [
        p for p in candidates
        if current_mode in p.get("modes", [])
    ]

    if mode_relevant:
        return random.choice(mode_relevant)

    return random.choice(candidates)
