"""Snapshot tests for every Hookwise TUI tab at 80x24 terminal size.

These tests use pytest-textual-snapshot to capture SVG screenshots of each tab
and compare against stored golden files.  Run with ``--snapshot-update`` on the
first invocation (or after intentional visual changes) to regenerate baselines.
"""

from __future__ import annotations

import os
import time
from pathlib import Path
from typing import Any, Callable

import pytest
from textual.css.query import NoMatches
from textual.pilot import Pilot
from textual.widgets import Static

from hookwise_tui.app import HookwiseTUI
from hookwise_tui.data import (
    AnalyticsData,
    DailySummary,
    FeedHealth,
    ToolBreakdown,
)

# Type alias for the snap_compare fixture callable (matches the plugin's inner
# compare() signature: app + keyword args → bool).
SnapCompare = Callable[..., bool]

TERMINAL_SIZE = (80, 24)


# -- Fixed data sources for tabs that compose from live machine state --------
# The Guards tab composes from read_config() (the developer's real
# ~/.hookwise/config.yaml) and the Analytics tab from read_analytics() (the
# live ~/.hookwise/analytics.db).  Post-compose stabiliser callbacks cannot
# make those deterministic: they can rewrite widget *content* but not widget
# *structure* — e.g. the sparklines and tool table only exist at all when the
# DB has rows.  The data readers themselves are therefore monkeypatched so
# compose sees identical fixed data on every machine.

FIXED_GUARDS_CONFIG: dict[str, Any] = {
    "guards": [
        {
            "match": "Bash",
            "action": "block",
            "reason": "Destructive command blocked",
            "when": "command contains 'rm -rf /'",
        },
        {
            "match": "Edit",
            "action": "warn",
            "reason": "Editing a lockfile is usually a mistake",
            "when": "path ends_with '.lock'",
        },
        {
            "match": "Bash",
            "action": "confirm",
            "reason": "Force push needs explicit confirmation",
            "when": "command contains '--force'",
            "unless": "command contains '--force-with-lease'",
        },
    ]
}


def _fixed_feed_health(
    config: dict[str, Any], cache: dict[str, Any]
) -> list[FeedHealth]:
    """Deterministic stand-in for read_feed_health().

    Non-empty on purpose: the ``#feed-list`` container lays out at a height
    of one row, so its children clip to a single border line — a line that
    is present whenever at least one feed exists and absent otherwise.
    ``last_update=None`` renders the age as "never" (no live-clock maths).
    """
    return [
        FeedHealth(
            name="pulse", enabled=True, last_update=None,
            interval_seconds=30, healthy=False,
        ),
        FeedHealth(
            name="project", enabled=True, last_update=None,
            interval_seconds=60, healthy=False,
        ),
    ]


def _fixed_analytics_data(
    db_path: Path | None = None, days: int = 7
) -> AnalyticsData:
    """Deterministic stand-in for read_analytics().

    Non-empty on purpose: exercises the metric boxes, both sparklines, and
    the tool-breakdown DataTable, all of which are omitted from the DOM when
    the data is empty.
    """
    return AnalyticsData(
        daily=[
            DailySummary(
                date="2026-01-01",
                total_events=40,
                total_tool_calls=30,
                lines_added=120,
                lines_removed=15,
                sessions=2,
            ),
            DailySummary(
                date="2026-01-02",
                total_events=80,
                total_tool_calls=60,
                lines_added=310,
                lines_removed=42,
                sessions=3,
            ),
            DailySummary(
                date="2026-01-03",
                total_events=25,
                total_tool_calls=20,
                lines_added=55,
                lines_removed=8,
                sessions=1,
            ),
        ],
        tools=[
            ToolBreakdown(tool_name="Edit", count=42, lines_added=380, lines_removed=51),
            ToolBreakdown(tool_name="Bash", count=35, lines_added=0, lines_removed=0),
            ToolBreakdown(tool_name="Read", count=28, lines_added=0, lines_removed=0),
        ],
    )


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
    from hookwise_tui.tabs.feeds import FeedsTab

    # Stop the tab's 3-second refresh interval first: if it fired between
    # this callback and the snapshot capture it would overwrite the pinned
    # widgets below with live machine state (an intermittent race).
    try:
        tab = pilot.app.query_one(FeedsTab)
        for interval_timer in list(tab._timers):
            interval_timer.stop()
    except NoMatches:
        pass
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


async def _stabilise_status(pilot: Pilot[Any]) -> None:
    """Pin the clock segment and preview to fixed content to avoid snapshot flakiness.

    The status tab includes a live clock segment and an auto-refreshing preview
    that both render time-dependent content, causing spurious mismatches.
    """
    os.environ["TZ"] = "UTC"
    time.tzset()
    from textual.containers import Container
    from hookwise_tui.tabs.status import StatusTab

    # Stop the tab's 3-second refresh interval first: if it fired between
    # this callback and the snapshot capture it would overwrite the pinned
    # preview with live machine state (an intermittent race).
    try:
        tab = pilot.app.query_one(StatusTab)
        for timer in list(tab._timers):
            timer.stop()
    except NoMatches:
        pass
    # Pin the preview box to static content
    try:
        preview = pilot.app.query_one("#preview-box", Container)
        preview.remove_children()
        preview.mount(Static("[dim]No active session — start Claude Code to see live preview[/dim]"))
    except NoMatches:
        pass


@pytest.mark.parametrize("tab_id, keys", TAB_SPECS, ids=[t[0] for t in TAB_SPECS])
def test_tab_snapshot(
    snap_compare: SnapCompare,
    tab_id: str,
    keys: tuple[str, ...],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Each TUI tab renders correctly at 80x24."""
    if tab_id == "guards":
        # Compose-time data source — pin before the app runs (see note above).
        monkeypatch.setattr(
            "hookwise_tui.tabs.guards.read_config", lambda: FIXED_GUARDS_CONFIG
        )
    elif tab_id == "analytics":
        monkeypatch.setattr(
            "hookwise_tui.tabs.analytics.read_analytics", _fixed_analytics_data
        )
    elif tab_id == "feeds":
        monkeypatch.setattr(
            "hookwise_tui.tabs.feeds.read_feed_health", _fixed_feed_health
        )

    if tab_id == "feeds":
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
