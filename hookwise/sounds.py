"""Notification sounds handler for hookwise.

Plays configured notification sounds on Notification events and
completion sounds on Stop events. Uses platform-native audio commands
(afplay on macOS, paplay/aplay on Linux).

Config example::

    sounds:
      enabled: true
      notification: {file: "/System/Library/Sounds/Glass.aiff", volume: 2}
      completion: {file: "/System/Library/Sounds/Hero.aiff", volume: 2}
      command: "afplay -v {volume} {file}"

All playback errors are silently ignored (fail-open). Sound playback
runs as a fire-and-forget subprocess to avoid blocking the hook process.
"""

from __future__ import annotations

import logging
import platform
import subprocess
from pathlib import Path
from typing import Any

logger = logging.getLogger("hookwise")


# ---------------------------------------------------------------------------
# Platform detection and default commands
# ---------------------------------------------------------------------------

# Default sound commands per platform
_DEFAULT_COMMANDS: dict[str, str] = {
    "Darwin": "afplay -v {volume} {file}",
    "Linux": "paplay {file}",
}


def get_default_command() -> str:
    """Return the default audio playback command for the current platform.

    Returns:
        Command template string with ``{file}`` and ``{volume}`` placeholders.
    """
    system = platform.system()
    return _DEFAULT_COMMANDS.get(system, "aplay {file}")


# ---------------------------------------------------------------------------
# Sound playback
# ---------------------------------------------------------------------------


def play_sound(
    file_path: str,
    volume: int | float = 1,
    command_template: str | None = None,
) -> bool:
    """Play a sound file using the platform audio command.

    Substitutes ``{file}`` and ``{volume}`` in the command template
    and executes it as a fire-and-forget subprocess. Returns False
    silently on any failure.

    Args:
        file_path: Path to the sound file.
        volume: Volume level (interpretation depends on platform command).
        command_template: Command template with ``{file}`` and ``{volume}``
            placeholders. If None, uses the platform default.

    Returns:
        True if the subprocess was launched successfully, False otherwise.
    """
    if not file_path:
        return False

    # Check if file exists
    path = Path(file_path).expanduser()
    if not path.is_file():
        logger.debug("Sound file not found: %s", path)
        return False

    if command_template is None:
        command_template = get_default_command()

    try:
        cmd = command_template.format(
            file=str(path),
            volume=volume,
        )
        # Fire-and-forget: don't wait for playback to complete
        subprocess.Popen(
            cmd,
            shell=True,  # noqa: S602
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        return True
    except Exception as exc:
        logger.debug("Sound playback failed: %s", exc)
        return False


# ---------------------------------------------------------------------------
# Builtin handler entry point
# ---------------------------------------------------------------------------


def handle(
    event_type: str,
    payload: dict[str, Any],
    config: Any,
) -> dict[str, Any] | None:
    """Builtin handler entry point for the sounds handler.

    Plays notification sounds on Notification events and completion
    sounds on Stop events. All errors are silently caught (fail-open).

    Only runs for Notification and Stop events.

    Args:
        event_type: The hook event type.
        payload: The event payload dict from stdin.
        config: The HooksConfig instance.

    Returns:
        None (sounds are a pure side effect, no output to Claude Code).
    """
    if event_type not in ("Notification", "Stop"):
        return None

    sounds_cfg = getattr(config, "sounds", {})
    if not isinstance(sounds_cfg, dict):
        sounds_cfg = {}

    if not sounds_cfg.get("enabled", True):
        return None

    try:
        # Get the command template (shared across sound types)
        command_template = sounds_cfg.get("command")

        if event_type == "Notification":
            sound_cfg = sounds_cfg.get("notification", {})
        elif event_type == "Stop":
            sound_cfg = sounds_cfg.get("completion", {})
        else:
            return None

        if not isinstance(sound_cfg, dict):
            return None

        file_path = sound_cfg.get("file", "")
        volume = sound_cfg.get("volume", 1)

        if file_path:
            play_sound(file_path, volume=volume, command_template=command_template)

    except Exception as exc:
        # Fail-open: never let sounds crash the hook
        logger.debug("Sounds handle() failed: %s", exc)

    return None
