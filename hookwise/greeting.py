"""Session greeting handler for hookwise.

Selects a motivational quote on SessionStart and delivers it via
stdout (additionalContext for the LLM) and stderr (terminal display).

Supports multiple quote categories with configurable weights and
loading quotes from a custom JSON file.

Config example::

    greeting:
      enabled: true
      quotes_file: "~/.hookwise/quotes.json"
      category_weights: {mindset: 0.3, grit: 0.2, practice: 0.3, custom: 0.2}
      additional_context: "Display this quote warmly in 1-2 lines."

The quotes JSON file should contain a list of dicts with keys:
``text``, ``attribution``, ``category``.
"""

from __future__ import annotations

import json
import logging
import random
import sys
from pathlib import Path
from typing import Any

logger = logging.getLogger("hookwise")


# ---------------------------------------------------------------------------
# Default built-in quotes
# ---------------------------------------------------------------------------

DEFAULT_QUOTES: list[dict[str, str]] = [
    {
        "text": "Our potential is one thing. What we do with it is quite another.",
        "attribution": "Angela Duckworth",
        "category": "grit",
    },
    {
        "text": "The obstacle is the way.",
        "attribution": "Marcus Aurelius",
        "category": "mindset",
    },
    {
        "text": "Practice isn't the thing you do once you're good. "
        "It's the thing you do that makes you good.",
        "attribution": "Malcolm Gladwell",
        "category": "practice",
    },
]

# Default category weights (uniform when not configured)
DEFAULT_CATEGORY_WEIGHTS: dict[str, float] = {
    "mindset": 0.33,
    "grit": 0.33,
    "practice": 0.34,
}

# Default instruction for the LLM
DEFAULT_ADDITIONAL_CONTEXT = "Display this quote warmly in 1-2 lines."


# ---------------------------------------------------------------------------
# Quote selection logic
# ---------------------------------------------------------------------------


def load_quotes_file(path_str: str) -> list[dict[str, str]]:
    """Load quotes from a custom JSON file.

    The file should contain a JSON array of objects with keys:
    ``text``, ``attribution``, ``category``.

    Returns an empty list if the file is missing, unreadable, or
    malformed (fail-open).

    Args:
        path_str: Path to the quotes JSON file. Supports ``~`` expansion.

    Returns:
        List of quote dicts, or empty list on failure.
    """
    try:
        path = Path(path_str).expanduser()
        if not path.is_file():
            logger.debug("Quotes file not found: %s", path)
            return []
        content = path.read_text(encoding="utf-8")
        data = json.loads(content)
        if not isinstance(data, list):
            logger.warning("Quotes file does not contain a list: %s", path)
            return []
        # Validate each quote has at least text
        valid = []
        for item in data:
            if isinstance(item, dict) and item.get("text"):
                valid.append({
                    "text": str(item["text"]),
                    "attribution": str(item.get("attribution", "Unknown")),
                    "category": str(item.get("category", "custom")),
                })
        return valid
    except (OSError, json.JSONDecodeError, ValueError) as exc:
        logger.warning("Failed to load quotes file %s: %s", path_str, exc)
        return []


def select_quote(
    quotes: list[dict[str, str]],
    category_weights: dict[str, float],
    rng: random.Random | None = None,
) -> dict[str, str] | None:
    """Select a quote using weighted category sampling.

    First selects a category based on weights, then uniformly selects
    a quote from that category. Falls back to uniform selection from
    all quotes if the chosen category has no quotes.

    Args:
        quotes: List of quote dicts with ``text``, ``attribution``, ``category``.
        category_weights: Dict mapping category names to weights (need not sum to 1).
        rng: Optional Random instance for reproducibility.

    Returns:
        A quote dict, or None if no quotes are available.
    """
    if not quotes:
        return None

    if rng is None:
        rng = random.Random()

    # Group quotes by category
    by_category: dict[str, list[dict[str, str]]] = {}
    for q in quotes:
        cat = q.get("category", "custom")
        by_category.setdefault(cat, []).append(q)

    # Filter weights to categories that have quotes
    available_weights = {
        cat: w for cat, w in category_weights.items()
        if cat in by_category and w > 0
    }

    # Add any categories from quotes that aren't in weights
    for cat in by_category:
        if cat not in available_weights:
            available_weights[cat] = 0.1  # small default weight

    if not available_weights:
        # Fallback: uniform over all quotes
        return rng.choice(quotes)

    # Weighted category selection
    categories = list(available_weights.keys())
    weights = [available_weights[c] for c in categories]
    chosen_category = rng.choices(categories, weights=weights, k=1)[0]

    # Uniform selection within category
    return rng.choice(by_category[chosen_category])


def format_quote(quote: dict[str, str]) -> str:
    """Format a quote for display.

    Args:
        quote: Dict with ``text`` and ``attribution`` keys.

    Returns:
        Formatted string like: '"Quote text" -- Attribution'
    """
    text = quote.get("text", "")
    attribution = quote.get("attribution", "")
    if attribution:
        return f'"{text}" -- {attribution}'
    return f'"{text}"'


# ---------------------------------------------------------------------------
# Builtin handler entry point
# ---------------------------------------------------------------------------


def handle(
    event_type: str,
    payload: dict[str, Any],
    config: Any,
) -> dict[str, Any] | None:
    """Builtin handler entry point for the greeting handler.

    On SessionStart, selects a quote from the configured pool and
    returns it as additionalContext for the LLM. Also emits the
    quote to stderr for terminal display.

    Only runs for SessionStart events. For other event types, returns None.

    Args:
        event_type: The hook event type.
        payload: The event payload dict from stdin.
        config: The HooksConfig instance.

    Returns:
        Dict with ``additionalContext`` key containing the quote and
        display instruction, or None for non-SessionStart events.
    """
    if event_type != "SessionStart":
        return None

    greeting_cfg = getattr(config, "greeting", {})
    if not isinstance(greeting_cfg, dict):
        greeting_cfg = {}

    if not greeting_cfg.get("enabled", True):
        return None

    try:
        # Load quotes: custom file + defaults
        quotes = list(DEFAULT_QUOTES)
        quotes_file = greeting_cfg.get("quotes_file")
        if quotes_file:
            custom_quotes = load_quotes_file(str(quotes_file))
            if custom_quotes:
                quotes.extend(custom_quotes)

        # Get category weights
        category_weights = greeting_cfg.get(
            "category_weights", DEFAULT_CATEGORY_WEIGHTS
        )
        if not isinstance(category_weights, dict):
            category_weights = DEFAULT_CATEGORY_WEIGHTS

        # Select a quote
        quote = select_quote(quotes, category_weights)
        if quote is None:
            return None

        formatted = format_quote(quote)

        # Emit to stderr for terminal display
        print(f"\n  {formatted}\n", file=sys.stderr)

        # Build additionalContext for LLM
        context_instruction = greeting_cfg.get(
            "additional_context", DEFAULT_ADDITIONAL_CONTEXT
        )
        context = f"{context_instruction}\n\nQuote: {formatted}"

        return {"additionalContext": context}

    except Exception as exc:
        # Fail-open: never let greeting crash the hook
        logger.error("Greeting handle() failed: %s", exc)
        return None
