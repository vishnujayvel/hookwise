"""Shared test fixtures for hookwise tests."""

from __future__ import annotations

import os
from pathlib import Path

import pytest


@pytest.fixture
def tmp_state_dir(tmp_path: Path) -> Path:
    """Provide a temporary state directory for tests.

    Sets HOOKWISE_STATE_DIR to a temp directory so tests don't
    interfere with the real ~/.hookwise/ directory.
    """
    state_dir = tmp_path / "hookwise_state"
    state_dir.mkdir()
    old_val = os.environ.get("HOOKWISE_STATE_DIR")
    os.environ["HOOKWISE_STATE_DIR"] = str(state_dir)
    yield state_dir
    if old_val is None:
        os.environ.pop("HOOKWISE_STATE_DIR", None)
    else:
        os.environ["HOOKWISE_STATE_DIR"] = old_val


@pytest.fixture
def tmp_json_path(tmp_path: Path) -> Path:
    """Provide a path for a temporary JSON file."""
    return tmp_path / "test_data.json"
