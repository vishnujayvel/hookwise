"""Status line segment composition logic.

Reads the shared cache, iterates over configured segments, renders
each one via built-in functions or custom shell commands, and joins
the non-empty results with a configurable delimiter.

Custom segments execute a shell command with a timeout and capture
stdout as the segment content. Failures produce empty segments
(no crash, no error in the status line).
"""

from __future__ import annotations

import logging
import subprocess
from pathlib import Path
from typing import Any

from hookwise.state import safe_read_json
from hookwise.status_line.segments import BUILTIN_SEGMENTS

logger = logging.getLogger("hookwise")

# Default cache path
DEFAULT_CACHE_PATH = "~/.hookwise/state/status-line-cache.json"

# Default segment delimiter
DEFAULT_DELIMITER = " | "

# Default custom command timeout in seconds
DEFAULT_CUSTOM_TIMEOUT = 5


def resolve_cache_path(config: dict[str, Any]) -> Path:
    """Resolve the cache file path from config.

    Expands ~ to the user's home directory.

    Args:
        config: Status line config dict.

    Returns:
        Resolved Path to the cache file.
    """
    raw_path = config.get("cache_path", DEFAULT_CACHE_PATH)
    if not isinstance(raw_path, str) or not raw_path:
        raw_path = DEFAULT_CACHE_PATH
    return Path(raw_path).expanduser()


def render_builtin_segment(
    name: str,
    cache: dict[str, Any],
    segment_config: dict[str, Any],
) -> str:
    """Render a single built-in segment by name.

    Looks up the segment function in the BUILTIN_SEGMENTS registry
    and calls it with the cache and config. Returns empty string
    for unknown segment names or on any error.

    Args:
        name: Built-in segment name (e.g., "clock", "mantra").
        cache: Shared status line cache dict.
        segment_config: Per-segment config dict.

    Returns:
        Rendered segment string, or empty string on failure.
    """
    render_fn = BUILTIN_SEGMENTS.get(name)
    if render_fn is None:
        logger.warning("Unknown builtin segment: %r", name)
        return ""

    try:
        result = render_fn(cache, segment_config)
        if not isinstance(result, str):
            return ""
        return result
    except Exception as exc:
        logger.debug("Builtin segment %r failed: %s", name, exc)
        return ""


def render_custom_segment(custom_config: dict[str, Any]) -> str:
    """Render a custom segment by executing a shell command.

    Runs the configured command in a subprocess with a timeout,
    captures stdout as the segment content. On any failure (timeout,
    non-zero exit, missing command), returns empty string.

    Args:
        custom_config: Custom segment config dict with keys:
            command: Shell command to execute (required).
            label: Display label prefix (optional).
            timeout: Command timeout in seconds (default 5).

    Returns:
        Captured stdout (stripped), or empty string on failure.
    """
    command = custom_config.get("command")
    if not isinstance(command, str) or not command.strip():
        return ""

    timeout = custom_config.get("timeout", DEFAULT_CUSTOM_TIMEOUT)
    if not isinstance(timeout, (int, float)):
        timeout = DEFAULT_CUSTOM_TIMEOUT
    timeout = max(1, int(timeout))

    try:
        result = subprocess.run(
            command,
            shell=True,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
        if result.returncode != 0:
            logger.debug(
                "Custom segment command %r exited with code %d",
                command, result.returncode,
            )
            return ""
        output = result.stdout.strip()
        return output
    except subprocess.TimeoutExpired:
        logger.debug("Custom segment command %r timed out after %ds", command, timeout)
        return ""
    except (OSError, subprocess.SubprocessError) as exc:
        logger.debug("Custom segment command %r failed: %s", command, exc)
        return ""


def compose_segments(
    segments_config: list[dict[str, Any]],
    cache: dict[str, Any],
    delimiter: str = DEFAULT_DELIMITER,
) -> str:
    """Compose all configured segments into a single status line string.

    Iterates over segment definitions in order. For each segment:
    - If it has a "builtin" key, renders the named built-in segment.
    - If it has a "custom" key, executes the custom command.
    - Empty results are filtered out.
    - Non-empty results are joined with the delimiter.

    Args:
        segments_config: List of segment config dicts from YAML.
        cache: Shared status line cache dict.
        delimiter: String to join segments with.

    Returns:
        Composed status line string.
    """
    parts: list[str] = []

    for seg in segments_config:
        if not isinstance(seg, dict):
            continue

        rendered = ""

        if "builtin" in seg:
            builtin_name = seg["builtin"]
            if isinstance(builtin_name, str):
                rendered = render_builtin_segment(builtin_name, cache, seg)

        elif "custom" in seg:
            custom_config = seg["custom"]
            if isinstance(custom_config, dict):
                rendered = render_custom_segment(custom_config)

        if rendered:
            parts.append(rendered)

    return delimiter.join(parts)
