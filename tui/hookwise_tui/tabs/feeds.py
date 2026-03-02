"""Feeds tab — Live feed dashboard with auto-refresh and timer."""

from datetime import datetime, timezone

from textual.app import ComposeResult
from textual.containers import Container, Horizontal, Vertical
from textual.widget import Widget
from textual.widgets import Static

from hookwise_tui.data import (
    read_cache,
    read_config,
    read_daemon_status,
    read_feed_health,
)
from hookwise_tui.widgets.feed_health import FeedHealthWidget


FEED_DESCRIPTIONS = {
    "pulse": "Heartbeat showing time since last interaction",
    "project": "Current git branch and last commit info",
    "calendar": "Upcoming events from Google Calendar",
    "news": "Top stories from Hacker News",
    "insights": "Claude Code usage patterns and productivity metrics",
}


class FeedsTab(Widget):
    """Feeds — live data dashboard with auto-refresh."""

    DEFAULT_CSS = """
    FeedsTab {
        height: auto;
    }
    FeedsTab .feeds-intro {
        color: $text-muted;
        margin: 0 0 1 0;
    }
    FeedsTab .daemon-status {
        padding: 1 2;
        margin: 0 0 1 0;
        border: round $primary;
        background: $surface-darken-1;
        height: auto;
    }
    FeedsTab .daemon-running {
        color: $success;
        text-style: bold;
    }
    FeedsTab .daemon-stopped {
        color: $error;
        text-style: bold;
    }
    FeedsTab .feed-desc {
        color: $text-muted;
        margin: 0 0 0 2;
        text-style: italic;
    }
    FeedsTab .timer-display {
        color: $warning;
        text-style: italic;
        margin: 1 0;
        text-align: right;
    }
    FeedsTab .architecture {
        padding: 1 2;
        margin: 1 0;
        border: round $accent;
        background: $surface-darken-1;
        height: auto;
    }
    FeedsTab .section-title {
        text-style: bold;
        color: $accent;
        margin: 1 0 0 0;
    }
    """

    _refresh_count = 0
    _last_refresh: str = ""

    def compose(self) -> ComposeResult:
        yield Static(
            "Feed producers run in the background daemon and write to the shared cache bus.",
            classes="feeds-intro",
        )

        # Daemon status
        yield Static(id="daemon-panel", classes="daemon-status")

        # Timer
        yield Static(id="timer-display", classes="timer-display")

        # Feed health indicators
        yield Static("Feed Health", classes="section-title")
        yield Container(id="feed-list")

        # Architecture diagram
        yield Static("Architecture", classes="section-title")
        yield Static(
            "[dim]Daemon → Feed Registry → Cache Bus → Status Line\n"
            "  └─ pulse (30s)  └─ project (60s)  └─ calendar (300s)\n"
            "  └─ news (1800s) └─ insights (120s) └─ custom feeds[/dim]",
            classes="architecture",
        )

    def on_mount(self) -> None:
        self._refresh_data()
        self.set_interval(3.0, self._refresh_data)

    def _refresh_data(self) -> None:
        self._refresh_count += 1
        now = datetime.now(timezone.utc)
        self._last_refresh = now.strftime("%H:%M:%S")

        config = read_config()
        cache = read_cache()
        daemon = read_daemon_status()
        feeds = read_feed_health(config, cache)

        # Update daemon panel
        daemon_panel = self.query_one("#daemon-panel", Static)
        if daemon.running:
            uptime_str = ""
            if daemon.uptime_seconds is not None:
                h = daemon.uptime_seconds // 3600
                m = (daemon.uptime_seconds % 3600) // 60
                uptime_str = f" | Uptime: {h}h {m}m"
            daemon_panel.update(
                f"[green bold]● DAEMON RUNNING[/green bold] "
                f"PID: {daemon.pid}{uptime_str}"
            )
        else:
            daemon_panel.update(
                "[red bold]● DAEMON STOPPED[/red bold] "
                "Run: hookwise daemon start"
            )

        # Update timer
        timer = self.query_one("#timer-display", Static)
        timer.update(
            f"Last refresh: {self._last_refresh} UTC | "
            f"Next in 3s | "
            f"Refresh #{self._refresh_count}"
        )

        # Update feed list
        feed_list = self.query_one("#feed-list", Container)
        feed_list.remove_children()
        for feed in feeds:
            feed_list.mount(FeedHealthWidget(feed))
            desc = FEED_DESCRIPTIONS.get(feed.name, "")
            if desc:
                feed_list.mount(Static(desc, classes="feed-desc"))
