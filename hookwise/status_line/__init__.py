"""Status line renderer for Claude Code.

Generates a composable status line string from multiple independent
segments. Segments can be built-in (clock, mantra, builder_trap,
session, practice, ai_ratio, cost) or custom (shell commands).

All segment data is read from a shared cache file written by
coaching/analytics handlers. The renderer never crashes -- missing
or corrupt cache data produces graceful fallbacks (empty segments).

Usage::

    from hookwise.status_line import StatusLineRenderer

    renderer = StatusLineRenderer()
    line = renderer.render({
        "enabled": True,
        "segments": [
            {"builtin": "clock", "format": "%H:%M"},
            {"builtin": "mantra"},
            {"builtin": "cost"},
        ],
        "delimiter": " | ",
        "cache_path": "~/.hookwise/state/status-line-cache.json",
    })
    print(line)  # "14:30 | One idea. One pause. | $0.67"
"""

from __future__ import annotations

import logging
from typing import Any

from hookwise.state import safe_read_json
from hookwise.status_line.renderer import (
    DEFAULT_DELIMITER,
    compose_segments,
    resolve_cache_path,
)

logger = logging.getLogger("hookwise")

__all__ = ["StatusLineRenderer", "render"]


class StatusLineRenderer:
    """Reads cache, composes segments, returns final status line string.

    This is the main entry point for status line rendering. It reads
    the shared cache file, composes configured segments in order, and
    returns a single string suitable for display in the Claude Code
    interface.

    The renderer is stateless -- all data comes from the cache file
    and config dict passed to render().
    """

    def render(self, config: dict[str, Any]) -> str:
        """Render the complete status line from config.

        Reads segment data from the shared cache file, composes
        all configured segments in order, and joins them with the
        configured delimiter.

        If the status line is disabled (enabled: false) or there
        are no segments configured, returns an empty string.

        Args:
            config: Status line config dict with keys:
                enabled: bool (default True)
                segments: list of segment defs
                delimiter: str (default " | ")
                cache_path: str (default "~/.hookwise/state/status-line-cache.json")

        Returns:
            Composed status line string, or empty string if disabled.
        """
        # Check if enabled
        enabled = config.get("enabled", True)
        if not enabled:
            return ""

        # Get segments list
        segments = config.get("segments", [])
        if not isinstance(segments, list) or not segments:
            return ""

        # Get delimiter
        delimiter = config.get("delimiter", DEFAULT_DELIMITER)
        if not isinstance(delimiter, str):
            delimiter = DEFAULT_DELIMITER

        # Read cache
        cache_path = resolve_cache_path(config)
        cache = safe_read_json(cache_path)

        # Compose and return
        return compose_segments(segments, cache, delimiter)


def render(config: dict[str, Any]) -> str:
    """Module-level render function for convenience.

    Creates a StatusLineRenderer and calls render() with the
    provided config. This is the simplest way to get a status line.

    Args:
        config: Status line config dict (same as StatusLineRenderer.render).

    Returns:
        Composed status line string.
    """
    renderer = StatusLineRenderer()
    return renderer.render(config)
