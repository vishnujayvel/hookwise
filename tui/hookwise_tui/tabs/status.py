"""Status tab — Status line preview with segment configurator."""

from datetime import datetime, timezone

from textual.app import ComposeResult
from textual.containers import Container
from textual.widget import Widget
from textual.widgets import Static

from hookwise_tui.data import read_cache, read_config


# Default status line segments
DEFAULT_SEGMENTS = [
    "pulse", "project", "calendar", "news",
    "insights_friction", "insights_pace", "insights_trend",
    "cost", "context",
]


class StatusTab(Widget):
    """Status — status line preview and segment configuration."""

    DEFAULT_CSS = """
    StatusTab {
        height: auto;
    }
    StatusTab .status-intro {
        color: $text-muted;
        margin: 0 0 1 0;
    }
    StatusTab .preview-box {
        padding: 1 2;
        margin: 1 0;
        border: heavy $accent;
        background: $surface-darken-2;
        height: auto;
    }
    StatusTab .preview-label {
        text-style: bold;
        color: $accent;
        margin: 0 0 1 0;
    }
    StatusTab .preview-line {
        text-style: bold;
        color: $text;
    }
    StatusTab .section-title {
        text-style: bold;
        color: $accent;
        margin: 1 0 0 0;
    }
    StatusTab .segment-list {
        margin: 1 0;
        padding: 1 2;
        border: round $primary;
        background: $surface-darken-1;
        height: auto;
    }
    StatusTab .segment-on {
        color: $success;
    }
    StatusTab .segment-off {
        color: $text-disabled;
    }
    StatusTab .config-info {
        margin: 1 0;
        padding: 1 2;
        border: round $primary;
        background: $surface-darken-1;
        height: auto;
    }
    """

    def compose(self) -> ComposeResult:
        config = read_config()
        cache = read_cache()
        status_config = config.get("status_line", {})
        if not isinstance(status_config, dict):
            status_config = {}

        enabled = status_config.get("enabled", False)
        delimiter = status_config.get("delimiter", " | ")
        segments = status_config.get("segments", DEFAULT_SEGMENTS)
        if not isinstance(segments, list):
            segments = DEFAULT_SEGMENTS

        yield Static(
            "The status line renders in your terminal showing live data from feeds and hooks.",
            classes="status-intro",
        )

        # Preview
        yield Static("Live Preview", classes="preview-label")
        yield Container(id="preview-box", classes="preview-box")

        # Config info
        with Container(classes="config-info"):
            yield Static(
                f"[bold]Status Line:[/bold] "
                f"{'[green]ENABLED[/green]' if enabled else '[red]DISABLED[/red]'}"
            )
            yield Static(f"[bold]Delimiter:[/bold] [cyan]{repr(delimiter)}[/cyan]")
            yield Static(
                f"[bold]Cache Path:[/bold] [dim]{status_config.get('cache_path', '~/.hookwise/state/status-line-cache.json')}[/dim]"
            )

        # Segment configurator
        yield Static("Segments", classes="section-title")
        yield Container(id="segment-list", classes="segment-list")

    def on_mount(self) -> None:
        self._refresh()
        self.set_interval(3.0, self._refresh)

    def _refresh(self) -> None:
        config = read_config()
        cache = read_cache()
        status_config = config.get("status_line", {})
        if not isinstance(status_config, dict):
            status_config = {}

        delimiter = status_config.get("delimiter", " | ")
        segments = status_config.get("segments", DEFAULT_SEGMENTS)
        if not isinstance(segments, list):
            segments = DEFAULT_SEGMENTS

        # Build preview
        parts = []
        for seg in segments:
            if isinstance(seg, str):
                entry = cache.get(seg)
                if isinstance(entry, dict):
                    # Try to extract a display value
                    if "text" in entry:
                        parts.append(str(entry["text"]))
                    elif "value" in entry:
                        parts.append(str(entry["value"]))
                    elif "branch" in entry:
                        parts.append(f"⎇ {entry['branch']}")
                    elif "idle_minutes" in entry:
                        mins = entry.get("idle_minutes", 0)
                        parts.append(f"💓 {mins}m")
                    else:
                        parts.append(f"[{seg}]")
                else:
                    parts.append(f"[dim]{seg}[/dim]")

        preview_text = delimiter.join(parts) if parts else "[dim]No segments rendering[/dim]"

        preview = self.query_one("#preview-box", Container)
        preview.remove_children()
        preview.mount(Static(preview_text, classes="preview-line"))

        # Update segment list
        seg_list = self.query_one("#segment-list", Container)
        seg_list.remove_children()
        for seg in segments:
            if isinstance(seg, str):
                has_data = seg in cache and isinstance(cache.get(seg), dict)
                if has_data:
                    seg_list.mount(
                        Static(f"  [green]●[/green] {seg}", classes="segment-on")
                    )
                else:
                    seg_list.mount(
                        Static(f"  [dim]○[/dim] {seg} [dim](no data)[/dim]", classes="segment-off")
                    )
