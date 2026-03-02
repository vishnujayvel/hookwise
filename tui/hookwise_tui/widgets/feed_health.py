"""Feed health indicator widget."""

from datetime import datetime, timezone

from textual.app import ComposeResult
from textual.containers import Horizontal
from textual.widget import Widget
from textual.widgets import Static

from hookwise_tui.data import FeedHealth


def _format_age(last_update: str | None) -> str:
    """Format time since last update."""
    if not last_update:
        return "never"
    try:
        ts = datetime.fromisoformat(last_update.replace("Z", "+00:00"))
        elapsed = (datetime.now(timezone.utc) - ts).total_seconds()
        if elapsed < 60:
            return f"{int(elapsed)}s ago"
        if elapsed < 3600:
            return f"{int(elapsed / 60)}m ago"
        return f"{int(elapsed / 3600)}h ago"
    except Exception:
        return "unknown"


class FeedHealthWidget(Widget):
    """Single feed health indicator with name, status, and age."""

    DEFAULT_CSS = """
    FeedHealthWidget {
        height: 3;
        padding: 0 2;
        margin: 0 0 0 0;
        border: round $primary;
        background: $surface-darken-1;
        layout: horizontal;
    }
    FeedHealthWidget .feed-name {
        width: 12;
        text-style: bold;
    }
    FeedHealthWidget .feed-status {
        width: 10;
    }
    FeedHealthWidget .feed-age {
        width: 12;
        color: $text-muted;
    }
    FeedHealthWidget .feed-interval {
        color: $text-disabled;
    }
    """

    def __init__(self, feed: FeedHealth, **kwargs) -> None:
        super().__init__(**kwargs)
        self._feed = feed

    def compose(self) -> ComposeResult:
        f = self._feed
        yield Static(f.name.upper(), classes="feed-name")

        if not f.enabled:
            yield Static("[dim]DISABLED[/dim]", classes="feed-status")
        elif f.healthy:
            yield Static("[green]HEALTHY[/green]", classes="feed-status")
        else:
            yield Static("[red]STALE[/red]", classes="feed-status")

        yield Static(_format_age(f.last_update), classes="feed-age")
        yield Static(f"every {f.interval_seconds}s", classes="feed-interval")
