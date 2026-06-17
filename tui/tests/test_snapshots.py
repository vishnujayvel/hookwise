"""Snapshot tests for every Hookwise TUI tab at 80x24 terminal size.

These tests use pytest-textual-snapshot to capture SVG screenshots of each tab
and compare against stored golden files.  Run with ``--snapshot-update`` on the
first invocation (or after intentional visual changes) to regenerate baselines.
"""

from __future__ import annotations

import os
import time
from typing import Any, Callable, cast
from textual.css.query import DOMQuery

import pytest
from textual.css.query import NoMatches
from textual.pilot import Pilot
from textual.widgets import Static

from hookwise_tui.app import HookwiseTUI

# Type alias for the snap_compare fixture callable (matches the plugin's inner
# compare() signature: app + keyword args → bool).
SnapCompare = Callable[..., bool]

TERMINAL_SIZE = (80, 24)


# -- Tab IDs and the key press needed to reach each one ----------------------
# Dashboard is the default active tab (key "1"), so no press is needed.
# For all other tabs we press the corresponding number key.

TAB_SPECS: list[tuple[str, tuple[str, ...]]] = [
    ("dashboard", ()),
    ("guards", ("2",)),
    ("analytics", ("3",)),
    ("feeds", ("4",)),
    ("insights", ("5",)),
    ("recipes", ("6",)),
    ("status", ("7",)),
]


async def _stabilise_feeds(pilot: Pilot[Any]) -> None:
    """Replace dynamic content in the Feeds tab with deterministic text.

    The Feeds tab refreshes every 3 seconds and includes:
    - ``#timer-display``: a UTC timestamp plus an incrementing refresh counter.
    - ``#daemon-panel``: the live daemon PID and uptime (changes every second).
    Both are pinned to fixed strings here to prevent spurious snapshot mismatches.
    """
    try:
        timer = pilot.app.query_one("#timer-display", Static)
        timer.update("Last refresh: 00:00:00 UTC | Next in 3s | Refresh #1")
    except NoMatches:
        pass  # Tab not visible / widget not yet mounted — safe to skip
    try:
        daemon = pilot.app.query_one("#daemon-panel", Static)
        daemon.update(
            "[red bold]● DAEMON STOPPED[/red bold] "
            "Run: hookwise daemon start"
        )
    except NoMatches:
        pass  # Tab not visible / widget not yet mounted — safe to skip


async def _stabilise_insights(pilot: Pilot[Any]) -> None:
    """Pin TZ to UTC before rendering the Insights tab.

    The ``peak_hour`` metric is computed from UTC hours offset by the local
    timezone, so different contributor machines produce different snapshots.
    Pinning ``TZ=UTC`` ensures the golden file is reproducible everywhere.
    """
    os.environ["TZ"] = "UTC"
    time.tzset()


async def _stabilise_analytics(pilot: Pilot[Any]) -> None:
    """Pin the Analytics tab's live database-driven content to deterministic values.

    The Analytics tab reads from the live ``~/.hookwise/analytics.db`` SQLite
    database at compose time.  Session counts, tool-call totals, sparkline
    values, and table rows all change as real work is recorded.  This callback
    overwrites those widgets with fixed content so the snapshot is reproducible
    regardless of the developer's local analytics state.

    Widgets pinned:
    - ``.metric-box`` Statics (3): session count, tool calls, lines added.
    - ``SparklineWidget`` children (2): ``.spark-label`` + ``.spark-bar`` per spark.
    - ``DataTable`` (1): cleared and refilled with a single fixed placeholder row.
    """
    from textual.css.query import NoMatches as _NoMatches
    from hookwise_tui.tabs.analytics import AnalyticsTab
    from hookwise_tui.widgets.sparkline import SparklineWidget
    from textual.widgets import DataTable

    try:
        tab = pilot.app.query_one(AnalyticsTab)
    except _NoMatches:
        return  # Analytics tab not in DOM — safe to skip

    # Pin the three metric-box Statics to fixed values
    fixed_metrics = [
        "Sessions (7d)\n[bold cyan]0[/bold cyan]",
        "Tool Calls\n[bold cyan]0[/bold cyan]",
        "Lines Added\n[bold cyan]0[/bold cyan]",
    ]
    for widget, text in zip(cast(DOMQuery[Static], tab.query(".metric-box")), fixed_metrics):
        widget.update(text)

    # Pin each SparklineWidget's label and bar to fixed placeholder content
    for spark in tab.query(SparklineWidget):
        try:
            spark.query_one(".spark-label", Static).update(
                f"{spark._label}: [bold cyan]0[/bold cyan]"
            )
            spark.query_one(".spark-bar", Static).update("[green]▄▄▄▄▄▄▄[/green]")
        except _NoMatches:
            pass

    # Clear and stub the tool-breakdown DataTable if present
    try:
        table = tab.query_one(DataTable)
        table.clear()
    except _NoMatches:
        pass  # No data → "no analytics data" Static is shown instead


async def _stabilise_status(pilot: Pilot[Any]) -> None:
    """Pin the clock segment and preview to fixed content to avoid snapshot flakiness.

    The status tab includes a live clock segment and an auto-refreshing preview
    that both render time-dependent content, causing spurious mismatches.
    """
    os.environ["TZ"] = "UTC"
    time.tzset()
    # Pin the preview box to static content
    from textual.containers import Container
    try:
        preview = pilot.app.query_one("#preview-box", Container)
        preview.remove_children()
        preview.mount(Static("[dim]No active session — start Claude Code to see live preview[/dim]"))
    except NoMatches:
        pass


@pytest.mark.parametrize("tab_id, keys", TAB_SPECS, ids=[t[0] for t in TAB_SPECS])
def test_tab_snapshot(snap_compare: SnapCompare, tab_id: str, keys: tuple[str, ...]) -> None:
    """Each TUI tab renders correctly at 80x24."""
    if tab_id == "analytics":
        run_before = _stabilise_analytics
    elif tab_id == "feeds":
        run_before = _stabilise_feeds
    elif tab_id == "insights":
        run_before = _stabilise_insights
    elif tab_id == "status":
        run_before = _stabilise_status
    else:
        run_before = None
    assert snap_compare(
        HookwiseTUI(),
        press=keys,
        terminal_size=TERMINAL_SIZE,
        run_before=run_before,
    )
