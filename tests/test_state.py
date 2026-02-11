"""Tests for hookwise.state -- atomic state management utilities."""

from __future__ import annotations

import json
import os
import stat
import threading
from pathlib import Path

import pytest

from hookwise.state import (
    atomic_write_json,
    ensure_state_dir,
    file_lock,
    get_state_dir,
    safe_read_json,
)


# ---------------------------------------------------------------------------
# get_state_dir
# ---------------------------------------------------------------------------


class TestGetStateDir:
    """Tests for get_state_dir()."""

    def test_returns_default_when_no_env(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """Should return ~/.hookwise/ when HOOKWISE_STATE_DIR is not set."""
        monkeypatch.delenv("HOOKWISE_STATE_DIR", raising=False)
        result = get_state_dir()
        assert result == Path.home() / ".hookwise"

    def test_respects_env_var(self, monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
        """Should return the path from HOOKWISE_STATE_DIR when set."""
        custom_dir = tmp_path / "custom_state"
        monkeypatch.setenv("HOOKWISE_STATE_DIR", str(custom_dir))
        result = get_state_dir()
        assert result == custom_dir

    def test_env_var_empty_string(self, monkeypatch: pytest.MonkeyPatch) -> None:
        """Empty string env var should fall back to default."""
        monkeypatch.setenv("HOOKWISE_STATE_DIR", "")
        result = get_state_dir()
        # Empty string is falsy, so should use default
        assert result == Path.home() / ".hookwise"


# ---------------------------------------------------------------------------
# ensure_state_dir
# ---------------------------------------------------------------------------


class TestEnsureStateDir:
    """Tests for ensure_state_dir()."""

    def test_creates_directory(self, tmp_path: Path) -> None:
        """Should create the directory if it doesn't exist."""
        new_dir = tmp_path / "new_state"
        assert not new_dir.exists()
        result = ensure_state_dir(new_dir)
        assert result == new_dir
        assert new_dir.is_dir()

    def test_creates_nested_directories(self, tmp_path: Path) -> None:
        """Should create parent directories as needed."""
        nested = tmp_path / "a" / "b" / "c" / "state"
        result = ensure_state_dir(nested)
        assert result == nested
        assert nested.is_dir()

    def test_sets_permissions_0o700(self, tmp_path: Path) -> None:
        """Should set directory permissions to owner-only (0o700)."""
        new_dir = tmp_path / "secure_state"
        ensure_state_dir(new_dir)
        mode = new_dir.stat().st_mode & 0o777
        assert mode == 0o700

    def test_fixes_permissions_on_existing_dir(self, tmp_path: Path) -> None:
        """Should correct permissions even if directory already exists."""
        existing = tmp_path / "existing_state"
        existing.mkdir(mode=0o755)
        ensure_state_dir(existing)
        mode = existing.stat().st_mode & 0o777
        assert mode == 0o700

    def test_uses_get_state_dir_when_none(self, tmp_state_dir: Path) -> None:
        """Should use get_state_dir() when no argument is provided."""
        result = ensure_state_dir()
        assert result == tmp_state_dir

    def test_idempotent(self, tmp_path: Path) -> None:
        """Calling twice should not error or change behavior."""
        new_dir = tmp_path / "idempotent"
        ensure_state_dir(new_dir)
        ensure_state_dir(new_dir)
        assert new_dir.is_dir()
        mode = new_dir.stat().st_mode & 0o777
        assert mode == 0o700


# ---------------------------------------------------------------------------
# atomic_write_json
# ---------------------------------------------------------------------------


class TestAtomicWriteJson:
    """Tests for atomic_write_json()."""

    def test_writes_valid_json(self, tmp_json_path: Path) -> None:
        """Should write a valid JSON file."""
        data = {"key": "value", "count": 42}
        atomic_write_json(tmp_json_path, data)
        content = tmp_json_path.read_text(encoding="utf-8")
        parsed = json.loads(content)
        assert parsed == data

    def test_json_is_pretty_printed(self, tmp_json_path: Path) -> None:
        """Should write indented JSON with sorted keys."""
        data = {"z_key": 1, "a_key": 2}
        atomic_write_json(tmp_json_path, data)
        content = tmp_json_path.read_text(encoding="utf-8")
        assert "  " in content  # indented
        # a_key should come before z_key (sorted)
        a_pos = content.index("a_key")
        z_pos = content.index("z_key")
        assert a_pos < z_pos

    def test_ends_with_newline(self, tmp_json_path: Path) -> None:
        """JSON file should end with a trailing newline."""
        atomic_write_json(tmp_json_path, {"x": 1})
        content = tmp_json_path.read_text(encoding="utf-8")
        assert content.endswith("\n")

    def test_overwrites_existing_file(self, tmp_json_path: Path) -> None:
        """Should replace existing file contents atomically."""
        atomic_write_json(tmp_json_path, {"version": 1})
        atomic_write_json(tmp_json_path, {"version": 2})
        data = json.loads(tmp_json_path.read_text(encoding="utf-8"))
        assert data == {"version": 2}

    def test_creates_parent_directories(self, tmp_path: Path) -> None:
        """Should create parent directories if they don't exist."""
        nested_path = tmp_path / "a" / "b" / "data.json"
        atomic_write_json(nested_path, {"nested": True})
        assert nested_path.exists()
        data = json.loads(nested_path.read_text(encoding="utf-8"))
        assert data == {"nested": True}

    def test_no_temp_files_left_on_success(self, tmp_path: Path) -> None:
        """Should not leave temporary files after successful write."""
        target = tmp_path / "clean.json"
        atomic_write_json(target, {"clean": True})
        files = list(tmp_path.iterdir())
        assert len(files) == 1
        assert files[0].name == "clean.json"

    def test_no_temp_files_on_serialization_error(self, tmp_path: Path) -> None:
        """Should clean up temp files even if JSON serialization fails."""
        target = tmp_path / "fail.json"

        class NotSerializable:
            pass

        with pytest.raises(TypeError):
            atomic_write_json(target, {"bad": NotSerializable()})  # type: ignore[dict-item]

        # No temp files should remain
        files = list(tmp_path.iterdir())
        assert len(files) == 0

    def test_empty_dict(self, tmp_json_path: Path) -> None:
        """Should handle empty dictionaries."""
        atomic_write_json(tmp_json_path, {})
        data = json.loads(tmp_json_path.read_text(encoding="utf-8"))
        assert data == {}

    def test_nested_data_structures(self, tmp_json_path: Path) -> None:
        """Should handle nested dicts, lists, and various types."""
        data = {
            "string": "hello",
            "number": 3.14,
            "boolean": True,
            "null_val": None,
            "list": [1, 2, 3],
            "nested": {"a": {"b": "c"}},
        }
        atomic_write_json(tmp_json_path, data)
        parsed = json.loads(tmp_json_path.read_text(encoding="utf-8"))
        assert parsed == data


# ---------------------------------------------------------------------------
# safe_read_json
# ---------------------------------------------------------------------------


class TestSafeReadJson:
    """Tests for safe_read_json()."""

    def test_reads_valid_json(self, tmp_json_path: Path) -> None:
        """Should parse and return a valid JSON file."""
        data = {"key": "value"}
        tmp_json_path.write_text(json.dumps(data), encoding="utf-8")
        result = safe_read_json(tmp_json_path)
        assert result == data

    def test_returns_default_on_missing_file(self, tmp_path: Path) -> None:
        """Should return default dict when file doesn't exist."""
        missing = tmp_path / "nonexistent.json"
        result = safe_read_json(missing)
        assert result == {}

    def test_returns_custom_default_on_missing(self, tmp_path: Path) -> None:
        """Should return the specified default when file is missing."""
        missing = tmp_path / "nonexistent.json"
        default = {"status": "unknown"}
        result = safe_read_json(missing, default=default)
        assert result == default

    def test_returns_default_on_invalid_json(self, tmp_json_path: Path) -> None:
        """Should return default when file contains invalid JSON."""
        tmp_json_path.write_text("not valid json {{{", encoding="utf-8")
        result = safe_read_json(tmp_json_path)
        assert result == {}

    def test_returns_default_on_empty_file(self, tmp_json_path: Path) -> None:
        """Should return default when file is empty."""
        tmp_json_path.write_text("", encoding="utf-8")
        result = safe_read_json(tmp_json_path)
        assert result == {}

    def test_returns_default_on_non_dict_json(self, tmp_json_path: Path) -> None:
        """Should return default when JSON is valid but not a dict."""
        tmp_json_path.write_text("[1, 2, 3]", encoding="utf-8")
        result = safe_read_json(tmp_json_path)
        assert result == {}

    def test_returns_default_on_json_string(self, tmp_json_path: Path) -> None:
        """Should return default when JSON is a bare string."""
        tmp_json_path.write_text('"just a string"', encoding="utf-8")
        result = safe_read_json(tmp_json_path)
        assert result == {}

    def test_returns_default_on_permission_error(self, tmp_json_path: Path) -> None:
        """Should return default when file is not readable."""
        tmp_json_path.write_text('{"key": "value"}', encoding="utf-8")
        tmp_json_path.chmod(0o000)
        try:
            result = safe_read_json(tmp_json_path)
            assert result == {}
        finally:
            # Restore permissions for cleanup
            tmp_json_path.chmod(0o644)

    def test_roundtrip_with_atomic_write(self, tmp_json_path: Path) -> None:
        """Data written with atomic_write_json should be readable."""
        data = {"roundtrip": True, "count": 99}
        atomic_write_json(tmp_json_path, data)
        result = safe_read_json(tmp_json_path)
        assert result == data


# ---------------------------------------------------------------------------
# file_lock
# ---------------------------------------------------------------------------


class TestFileLock:
    """Tests for file_lock() context manager."""

    def test_basic_lock_unlock(self, tmp_json_path: Path) -> None:
        """Should acquire and release lock without error."""
        with file_lock(tmp_json_path):
            # Lock is held here
            assert True
        # Lock is released here

    def test_lock_creates_lock_file(self, tmp_json_path: Path) -> None:
        """Should create a .lock file alongside the target."""
        lock_path = Path(str(tmp_json_path) + ".lock")
        assert not lock_path.exists()
        with file_lock(tmp_json_path):
            # Lock file exists while held
            assert lock_path.exists()

    def test_lock_file_cleaned_up(self, tmp_json_path: Path) -> None:
        """Lock file should be removed after context exits."""
        lock_path = Path(str(tmp_json_path) + ".lock")
        with file_lock(tmp_json_path):
            pass
        assert not lock_path.exists()

    def test_sequential_lock_acquire(self, tmp_json_path: Path) -> None:
        """Should be able to acquire the same lock sequentially."""
        with file_lock(tmp_json_path):
            pass
        # Re-acquire after release
        with file_lock(tmp_json_path):
            pass

    def test_protects_concurrent_writes(self, tmp_path: Path) -> None:
        """Lock should serialize concurrent write access."""
        target = tmp_path / "counter.json"
        atomic_write_json(target, {"count": 0})

        errors: list[Exception] = []
        iterations = 20

        def increment() -> None:
            try:
                for _ in range(iterations):
                    with file_lock(target):
                        data = safe_read_json(target)
                        data["count"] = data.get("count", 0) + 1
                        atomic_write_json(target, data)
            except Exception as e:
                errors.append(e)

        threads = [threading.Thread(target=increment) for _ in range(4)]
        for t in threads:
            t.start()
        for t in threads:
            t.join(timeout=10)

        assert not errors, f"Errors during concurrent writes: {errors}"
        final = safe_read_json(target)
        assert final["count"] == iterations * 4

    def test_lock_on_nonexistent_parent(self, tmp_path: Path) -> None:
        """Should create parent directories for the lock file."""
        deep_path = tmp_path / "a" / "b" / "data.json"
        with file_lock(deep_path):
            assert True
