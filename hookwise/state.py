"""Atomic state management utilities for hookwise.

Provides safe file I/O primitives that protect against data corruption
from crashes, concurrent writes, and missing/corrupt files. All state
is stored under ~/.hookwise/ by default.

Key guarantees:
- atomic_write_json: Uses temp-file-plus-rename to ensure writes are atomic
- safe_read_json: Returns a fallback dict on any read failure
- ensure_state_dir: Creates the state directory with restrictive permissions
- file_lock: Advisory file locking for concurrent write scenarios
"""

from __future__ import annotations

import contextlib
import fcntl
import json
import os
import tempfile
import threading
from pathlib import Path
from typing import Any, Generator

# Default state directory
_DEFAULT_STATE_DIR = Path.home() / ".hookwise"


def get_state_dir() -> Path:
    """Return the state directory path (~/.hookwise/).

    Respects the HOOKWISE_STATE_DIR environment variable if set,
    otherwise defaults to ~/.hookwise/.

    Returns:
        Path to the state directory.
    """
    env_dir = os.environ.get("HOOKWISE_STATE_DIR")
    if env_dir:
        return Path(env_dir)
    return _DEFAULT_STATE_DIR


def ensure_state_dir(state_dir: Path | None = None) -> Path:
    """Create the state directory if it does not exist.

    Sets permissions to 0o700 (owner-only access) for security,
    since hook state may contain sensitive information.

    Args:
        state_dir: Path to the state directory. If None, uses get_state_dir().

    Returns:
        Path to the (now existing) state directory.
    """
    if state_dir is None:
        state_dir = get_state_dir()

    state_dir.mkdir(parents=True, exist_ok=True)
    # Ensure permissions are restrictive even if dir already existed
    state_dir.chmod(0o700)
    return state_dir


def atomic_write_json(path: Path, data: dict[str, Any]) -> None:
    """Write JSON data to a file atomically.

    Uses the temp-file-plus-rename pattern to ensure that the file is
    either fully written or not modified at all. This protects against
    data corruption from crashes or power loss mid-write.

    The temp file is created in the same directory as the target to
    ensure the rename is atomic (same filesystem).

    Args:
        path: Target file path.
        data: Dictionary to serialize as JSON.

    Raises:
        OSError: If the write or rename fails.
        TypeError: If data is not JSON-serializable.
    """
    # Ensure parent directory exists
    path.parent.mkdir(parents=True, exist_ok=True)

    # Write to a temp file in the same directory, then rename
    fd = None
    tmp_path = None
    try:
        fd, tmp_path = tempfile.mkstemp(
            dir=path.parent,
            prefix=f".{path.name}.",
            suffix=".tmp",
        )
        # Write JSON content
        content = json.dumps(data, indent=2, sort_keys=True) + "\n"
        os.write(fd, content.encode("utf-8"))
        os.fsync(fd)
        os.close(fd)
        fd = None  # Mark as closed

        # Atomic rename (POSIX guarantees this is atomic on same filesystem)
        os.replace(tmp_path, path)
        tmp_path = None  # Mark as renamed (no cleanup needed)
    finally:
        # Clean up fd if still open
        if fd is not None:
            with contextlib.suppress(OSError):
                os.close(fd)
        # Clean up temp file if rename didn't happen
        if tmp_path is not None:
            with contextlib.suppress(OSError):
                os.unlink(tmp_path)


def safe_read_json(path: Path, default: dict[str, Any] | None = None) -> dict[str, Any]:
    """Read JSON from a file with fallback on any failure.

    Returns the default value (empty dict if not specified) when:
    - The file does not exist
    - The file contains invalid JSON
    - Any I/O error occurs during reading

    This ensures that callers never need to handle file-not-found or
    parse errors -- they always get a usable dict back.

    Args:
        path: Path to the JSON file.
        default: Fallback value if reading fails. Defaults to {}.

    Returns:
        Parsed dictionary from the file, or the default value.
    """
    if default is None:
        default = {}

    try:
        content = path.read_text(encoding="utf-8")
        result = json.loads(content)
        if not isinstance(result, dict):
            return default
        return result
    except (OSError, json.JSONDecodeError, ValueError):
        return default


# In-process thread locks keyed by resolved lock file path.
# fcntl.flock only provides inter-process locking; threads within the
# same process need a threading.Lock to serialize access.
_thread_locks: dict[str, threading.Lock] = {}
_thread_locks_guard = threading.Lock()


def _get_thread_lock(lock_path: Path) -> threading.Lock:
    """Return (or create) the in-process threading lock for a path."""
    key = str(lock_path.resolve())
    with _thread_locks_guard:
        if key not in _thread_locks:
            _thread_locks[key] = threading.Lock()
        return _thread_locks[key]


@contextlib.contextmanager
def file_lock(path: Path, *, timeout: float | None = None) -> Generator[None, None, None]:
    """Advisory file lock for concurrent write scenarios.

    Uses a two-layer locking strategy:
    - threading.Lock for serializing access across threads within the
      same process (fcntl.flock does not help here on macOS).
    - fcntl.flock for serializing access across separate processes.

    The lock file is created alongside the target file with a .lock suffix.

    This is an advisory lock -- it only prevents concurrent access from
    other hookwise processes/threads that also use file_lock(). It does not
    prevent external programs from modifying the file.

    Args:
        path: Path to the file being protected. A .lock file will be
            created at path.with_suffix(path.suffix + '.lock').
        timeout: Not currently used (reserved for future non-blocking
            lock attempts). The lock is always blocking.

    Yields:
        None. The lock is held for the duration of the context.

    Raises:
        OSError: If the lock file cannot be created or locked.
    """
    lock_path = Path(str(path) + ".lock")
    lock_path.parent.mkdir(parents=True, exist_ok=True)

    thread_lock = _get_thread_lock(lock_path)
    thread_lock.acquire()

    lock_fd = None
    try:
        lock_fd = open(lock_path, "w")  # noqa: SIM115
        fcntl.flock(lock_fd.fileno(), fcntl.LOCK_EX)
        yield
    finally:
        if lock_fd is not None:
            with contextlib.suppress(OSError):
                fcntl.flock(lock_fd.fileno(), fcntl.LOCK_UN)
            lock_fd.close()
            # Clean up lock file (best effort)
            with contextlib.suppress(OSError):
                lock_path.unlink()
        thread_lock.release()
