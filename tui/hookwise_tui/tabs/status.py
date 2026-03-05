"""Status tab — Status line preview with segment configurator."""

from __future__ import annotations

from textual.app import ComposeResult
from textual.containers import Container
from textual.widget import Widget
from textual.widgets import Static, Switch

from hookwise_tui.data import read_cache, read_config, write_config


FIXED_SEGMENTS = [
    "context_bar", "mode_badge", "cost", "duration", "daemon_health",
]
ROTATING_SEGMENTS = [
    "insights_friction", "insights_pace", "insights_trend",
    "news", "calendar", "mantra", "project", "pulse",
]
OTHER_SEGMENTS = [
    "clock", "builder_trap", "session", "practice",
    "streak", "weather", "memories",
]
ALL_SEGMENTS = FIXED_SEGMENTS + ROTATING_SEGMENTS + OTHER_SEGMENTS

SEGMENT_PLACEHOLDERS = {
    "context_bar": "50% \u2588\u2588\u2588\u2588\u2588\u2591\u2591\u2591\u2591\u2591",
    "mode_badge": "[practice]",
    "cost": "$3.45",
    "duration": "1h23m",
    "daemon_health": "daemon: ok",
    "insights_friction": "\u2705 No friction detected",
    "insights_pace": "\U0001f4ca 12 msgs/day | 2.1k+ lines",
    "insights_trend": "\U0001f527 Top: Bash, Read | Peak: afternoon",
    "news": "\U0001f4f0 Show HN: Hookwise (142pts)",
    "calendar": "\U0001f4c5 Standup in 15min",
    "mantra": "Ship it",
    "project": "\U0001f4e6 hookwise (main) \u2022 3m ago",
    "pulse": "\U0001f49a 2m",
    "clock": "14:32",
    "builder_trap": "\u26a0\ufe0f 25m tooling",
    "session": "45m \u2022 12 calls",
    "practice": "\U0001f3af 3 today",
    "streak": "\U0001f525 5d streak",
    "weather": "\u2600\ufe0f 72\u00b0F",
    "memories": "\U0001f570\ufe0f On this day: 2 sessions",
}


class SegmentRow(Widget):
    """A single segment row with switch toggle and label."""

    DEFAULT_CSS = """
    SegmentRow {
        layout: horizontal;
        height: 3;
        padding: 0 1;
    }
    SegmentRow .seg-switch {
        width: 8;
        margin: 0 1 0 0;
    }
    SegmentRow .seg-name {
        width: auto;
        content-align-vertical: middle;
        padding: 1 0 0 0;
    }
    SegmentRow .seg-data-dot {
        width: 3;
        content-align-vertical: middle;
        padding: 1 0 0 0;
    }
    """

    def __init__(self, segment_name: str, enabled: bool, has_data: bool) -> None:
        super().__init__()
        self._segment_name = segment_name
        self._enabled = enabled
        self._has_data = has_data

    def compose(self) -> ComposeResult:
        dot = "[green]\u25cf[/green]" if self._has_data else "[dim]\u25cb[/dim]"
        yield Static(dot, classes="seg-data-dot")
        yield Switch(value=self._enabled, id=f"seg-{self._segment_name}", classes="seg-switch")
        yield Static(self._segment_name, classes="seg-name")


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
    StatusTab .tier-header {
        text-style: bold;
        color: $accent;
        margin: 1 0 0 0;
        padding: 0 1;
    }
    StatusTab .segment-group {
        margin: 0 0 1 0;
        padding: 0 2;
        border: round $primary;
        background: $surface-darken-1;
        height: auto;
    }
    StatusTab .config-info {
        margin: 1 0;
        padding: 1 2;
        border: round $primary;
        background: $surface-darken-1;
        height: auto;
    }
    StatusTab .tier-summary {
        padding: 1 2;
        margin: 1 0;
        border: round $accent;
        background: $surface-darken-1;
        height: auto;
        color: $text-muted;
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
        active_segments = status_config.get("segments", list(ALL_SEGMENTS))
        if not isinstance(active_segments, list):
            active_segments = list(ALL_SEGMENTS)
        active_set = set(active_segments)

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

        # Tier summary
        yield Container(id="tier-summary", classes="tier-summary")

        # Fixed segments group
        yield Static("Line 1 \u2014 Fixed (always visible)", classes="tier-header")
        with Container(classes="segment-group"):
            for seg in FIXED_SEGMENTS:
                has_data = seg in cache and isinstance(cache.get(seg), dict)
                yield SegmentRow(seg, seg in active_set, has_data)

        # Rotating segments group
        yield Static("Line 2 \u2014 Rotating (cycles through)", classes="tier-header")
        with Container(classes="segment-group"):
            for seg in ROTATING_SEGMENTS:
                has_data = seg in cache and isinstance(cache.get(seg), dict)
                yield SegmentRow(seg, seg in active_set, has_data)

        # Other segments group
        yield Static("Other / Standalone", classes="tier-header")
        with Container(classes="segment-group"):
            for seg in OTHER_SEGMENTS:
                has_data = seg in cache and isinstance(cache.get(seg), dict)
                yield SegmentRow(seg, seg in active_set, has_data)

    def on_mount(self) -> None:
        self._refresh_preview()
        self.set_interval(3.0, self._refresh_preview)

    def _get_active_segments(self) -> list[str]:
        """Read currently toggled-on segments from the Switch widgets."""
        active = []
        for seg in ALL_SEGMENTS:
            try:
                switch = self.query_one(f"#seg-{seg}", Switch)
                if switch.value:
                    active.append(seg)
            except Exception:
                continue
        return active

    def _refresh_preview(self) -> None:
        config = read_config()
        cache = read_cache()
        status_config = config.get("status_line", {})
        if not isinstance(status_config, dict):
            status_config = {}

        delimiter = status_config.get("delimiter", " | ")
        active = self._get_active_segments()
        active_set = set(active)

        # Build preview using cache data or placeholders
        fixed_parts = []
        for seg in FIXED_SEGMENTS:
            if seg not in active_set:
                continue
            text = self._render_segment(seg, cache)
            if text:
                fixed_parts.append(text)

        rotating_parts = []
        for seg in ROTATING_SEGMENTS:
            if seg not in active_set:
                continue
            text = self._render_segment(seg, cache)
            if text:
                rotating_parts.append(text)

        line1 = delimiter.join(fixed_parts) if fixed_parts else ""
        # Show first non-empty rotating segment (simulates rotation)
        line2 = rotating_parts[0] if rotating_parts else ""

        if line1 and line2:
            preview_text = f"{line1}\n{line2}"
        elif line1:
            preview_text = line1
        elif line2:
            preview_text = line2
        else:
            preview_text = "[dim]No segments enabled[/dim]"

        preview = self.query_one("#preview-box", Container)
        preview.remove_children()
        preview.mount(Static(preview_text, classes="preview-line"))

        # Update tier summary
        fixed_active = [s for s in FIXED_SEGMENTS if s in active_set]
        rotating_active = [s for s in ROTATING_SEGMENTS if s in active_set]

        summary_lines = []
        if fixed_active:
            summary_lines.append(
                f"[bold]Line 1 (fixed):[/bold] {delimiter.join(fixed_active)}"
            )
        if rotating_active:
            summary_lines.append(
                f"[bold]Line 2 (rotating):[/bold] {' \u2192 '.join(rotating_active)}"
            )
        if not summary_lines:
            summary_lines.append("[dim]No segments active[/dim]")

        try:
            tier_summary = self.query_one("#tier-summary", Container)
            tier_summary.remove_children()
            tier_summary.mount(Static("\n".join(summary_lines)))
        except Exception:
            pass

    @staticmethod
    def _render_segment(seg: str, cache: dict) -> str:
        """Render a segment from cache data, falling back to a placeholder."""
        entry = cache.get(seg)
        if isinstance(entry, dict):
            if "text" in entry:
                return str(entry["text"])
            if "value" in entry:
                return str(entry["value"])
            if "branch" in entry:
                return f"\u23e1 {entry['branch']}"
            if "idle_minutes" in entry:
                return f"\U0001f49a {entry.get('idle_minutes', 0)}m"
            return SEGMENT_PLACEHOLDERS.get(seg, f"[{seg}]")
        return SEGMENT_PLACEHOLDERS.get(seg, f"[dim]{seg}[/dim]")

    def on_switch_changed(self, event: Switch.Changed) -> None:
        switch_id = event.switch.id or ""
        if not switch_id.startswith("seg-"):
            return

        segment_name = switch_id[4:]
        active = self._get_active_segments()

        config = read_config()
        if "status_line" not in config or not isinstance(config.get("status_line"), dict):
            config["status_line"] = {}
        config["status_line"]["segments"] = active

        if write_config(config):
            state = "enabled" if event.value else "disabled"
            self.notify(f"{segment_name} {state}")
        else:
            self.notify("Failed to save config", severity="error")

        self._refresh_preview()
