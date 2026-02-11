"""Transcript backup handler for hookwise.

On PreCompact events, saves the transcript content to a timestamped
backup file. Backups are written atomically (temp file + rename) to
prevent corruption. The backup directory is size-managed: oldest
backups are deleted when the total size exceeds the configured limit.

Config example::

    transcript_backup:
      enabled: true
      backup_dir: "~/.hookwise/backups/"
      max_size_mb: 100

All errors are handled fail-open -- a backup failure never blocks
the compaction process.
"""

from __future__ import annotations

import json
import logging
import os
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

logger = logging.getLogger("hookwise")


# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------

DEFAULT_BACKUP_DIR = "~/.hookwise/backups"
DEFAULT_MAX_SIZE_MB = 100


# ---------------------------------------------------------------------------
# Backup directory management
# ---------------------------------------------------------------------------


def get_backup_dir(config_path: str | None = None) -> Path:
    """Resolve the backup directory path.

    Args:
        config_path: Configured backup directory path. Supports ``~``.
            If None, uses the default.

    Returns:
        Resolved Path to the backup directory.
    """
    if config_path:
        return Path(config_path).expanduser()
    return Path(DEFAULT_BACKUP_DIR).expanduser()


def get_dir_size(directory: Path) -> int:
    """Calculate total size of all files in a directory (non-recursive).

    Args:
        directory: Path to the directory.

    Returns:
        Total size in bytes, or 0 if directory doesn't exist.
    """
    if not directory.is_dir():
        return 0
    total = 0
    try:
        for entry in directory.iterdir():
            if entry.is_file():
                try:
                    total += entry.stat().st_size
                except OSError:
                    pass
    except OSError:
        pass
    return total


def get_sorted_backups(directory: Path) -> list[Path]:
    """Get backup files sorted by modification time (oldest first).

    Only considers ``.json`` files matching the backup naming pattern.

    Args:
        directory: Path to the backup directory.

    Returns:
        List of Path objects sorted oldest-first.
    """
    if not directory.is_dir():
        return []
    try:
        backups = [
            f for f in directory.iterdir()
            if f.is_file() and f.suffix == ".json"
        ]
        backups.sort(key=lambda f: f.stat().st_mtime)
        return backups
    except OSError:
        return []


def enforce_size_limit(directory: Path, max_bytes: int) -> int:
    """Delete oldest backups until directory size is under the limit.

    Checks the CURRENT directory size and removes oldest backup files
    until the total is under ``max_bytes``. This is called BEFORE
    writing a new backup to ensure space is available.

    Args:
        directory: Path to the backup directory.
        max_bytes: Maximum allowed total size in bytes.

    Returns:
        Number of files deleted.
    """
    deleted = 0
    current_size = get_dir_size(directory)

    if current_size <= max_bytes:
        return 0

    backups = get_sorted_backups(directory)
    for backup in backups:
        if current_size <= max_bytes:
            break
        try:
            file_size = backup.stat().st_size
            backup.unlink()
            current_size -= file_size
            deleted += 1
            logger.debug("Deleted old backup: %s", backup.name)
        except OSError as exc:
            logger.debug("Failed to delete backup %s: %s", backup.name, exc)

    return deleted


# ---------------------------------------------------------------------------
# Atomic backup writing
# ---------------------------------------------------------------------------


def write_backup(
    backup_dir: Path,
    content: str,
    session_id: str = "",
    timestamp: str | None = None,
) -> Path | None:
    """Write transcript content to a timestamped backup file atomically.

    Uses temp-file-plus-rename for atomic writes. The backup filename
    includes the timestamp and session_id for identification.

    Args:
        backup_dir: Path to the backup directory.
        content: The transcript content to back up.
        session_id: Session identifier for the filename.
        timestamp: ISO 8601 timestamp. Uses current UTC if None.

    Returns:
        Path to the written backup file, or None on failure.
    """
    if timestamp is None:
        timestamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    else:
        # Sanitize timestamp for use in filename
        timestamp = timestamp.replace(":", "").replace("-", "")

    # Sanitize session_id for filename
    safe_session = "".join(c for c in session_id if c.isalnum() or c in "-_")[:32]
    if not safe_session:
        safe_session = "unknown"

    filename = f"transcript_{timestamp}_{safe_session}.json"
    target_path = backup_dir / filename

    try:
        backup_dir.mkdir(parents=True, exist_ok=True)

        # Prepare backup data
        backup_data = {
            "session_id": session_id,
            "timestamp": timestamp,
            "content": content,
        }
        json_content = json.dumps(backup_data, indent=2, ensure_ascii=False) + "\n"

        # Write atomically: temp file + rename
        fd = None
        tmp_path = None
        try:
            fd, tmp_path = tempfile.mkstemp(
                dir=backup_dir,
                prefix=f".{filename}.",
                suffix=".tmp",
            )
            os.write(fd, json_content.encode("utf-8"))
            os.fsync(fd)
            os.close(fd)
            fd = None

            os.replace(tmp_path, target_path)
            tmp_path = None
            logger.debug("Wrote transcript backup: %s", target_path)
            return target_path
        finally:
            if fd is not None:
                try:
                    os.close(fd)
                except OSError:
                    pass
            if tmp_path is not None:
                try:
                    os.unlink(tmp_path)
                except OSError:
                    pass

    except Exception as exc:
        logger.error("Failed to write transcript backup: %s", exc)
        return None


# ---------------------------------------------------------------------------
# Builtin handler entry point
# ---------------------------------------------------------------------------


def handle(
    event_type: str,
    payload: dict[str, Any],
    config: Any,
) -> dict[str, Any] | None:
    """Builtin handler entry point for the transcript backup handler.

    On PreCompact events, saves transcript content to a timestamped
    backup file. Checks directory size limits before writing and
    deletes oldest backups when the limit is exceeded.

    Only runs for PreCompact events.

    Args:
        event_type: The hook event type.
        payload: The event payload dict from stdin.
        config: The HooksConfig instance.

    Returns:
        None (backup is a pure side effect, no output to Claude Code).
    """
    if event_type != "PreCompact":
        return None

    transcript_cfg = getattr(config, "transcript_backup", {})
    if not isinstance(transcript_cfg, dict):
        transcript_cfg = {}

    if not transcript_cfg.get("enabled", True):
        return None

    try:
        # Extract transcript content from payload
        transcript = payload.get("transcript", "")
        if not transcript:
            # Try alternate payload shapes
            transcript = payload.get("content", "")
        if not transcript:
            logger.debug("No transcript content in PreCompact payload")
            return None

        # If transcript is a list (conversation turns), serialize it
        if isinstance(transcript, (list, dict)):
            transcript = json.dumps(transcript, indent=2, ensure_ascii=False)

        # Resolve backup directory and size limit
        backup_dir = get_backup_dir(transcript_cfg.get("backup_dir"))
        max_size_mb = transcript_cfg.get("max_size_mb", DEFAULT_MAX_SIZE_MB)
        if not isinstance(max_size_mb, (int, float)):
            max_size_mb = DEFAULT_MAX_SIZE_MB
        max_bytes = int(max_size_mb * 1024 * 1024)

        # Enforce size limit BEFORE writing
        enforce_size_limit(backup_dir, max_bytes)

        # Write the backup
        session_id = payload.get("session_id", "")
        write_backup(
            backup_dir=backup_dir,
            content=transcript,
            session_id=session_id,
        )

    except Exception as exc:
        # Fail-open: never let backup crash the hook
        logger.error("Transcript handle() failed: %s", exc)

    return None
