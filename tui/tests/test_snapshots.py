"""Snapshot tests for every Hookwise TUI tab at 80x24 terminal size.

These tests use pytest-textual-snapshot to capture SVG screenshots of each tab
and compare against stored golden files.  Run with ``--snapshot-update`` on the
first invocation (or after intentional visual changes) to regenerate baselines.
"""

from __future__ import annotations

import pytest
from textual.widgets import Static

from hookwise_tui.app import HookwiseTUI

TERMINAL_SIZE = (80, 24)


# -- Tab IDs and the key press needed to reach each one ----------------------
# Dashboard is the default active tab (key "1"), so no press is needed.
# For all other tabs we press the corresponding number key.

TAB_SPECS: list[tuple[str, tuple[str, ...]]] = [
    ("dashboard", ()),
    ("guards", ("2",)),
    ("coaching", ("3",)),
    ("analytics", ("4",)),
    ("feeds", ("5",)),
    ("insights", ("6",)),
    ("recipes", ("7",)),
    ("status", ("8",)),
]


async def _stabilise_feeds(pilot) -> None:
    """Replace dynamic timer content in the Feeds tab with deterministic text.

    The Feeds tab refreshes every 3 seconds and includes a UTC timestamp plus
    an incrementing refresh counter, both of which cause spurious snapshot
    mismatches.  This callback pins the timer to a fixed string.
    """
    try:
        timer = pilot.app.query_one("#timer-display", Static)
        timer.update("Last refresh: 00:00:00 UTC | Next in 3s | Refresh #1")
    except Exception:
        pass  # Tab not visible / widget not yet mounted — safe to skip


@pytest.mark.parametrize("tab_id, keys", TAB_SPECS, ids=[t[0] for t in TAB_SPECS])
def test_tab_snapshot(snap_compare, tab_id, keys):
    """Each TUI tab renders correctly at 80x24."""
    run_before = _stabilise_feeds if tab_id == "feeds" else None
    assert snap_compare(
        HookwiseTUI(),
        press=keys,
        terminal_size=TERMINAL_SIZE,
        run_before=run_before,
    )
