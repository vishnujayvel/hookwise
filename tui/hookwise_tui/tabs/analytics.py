"""Analytics tab — Sparkline trends, AI authorship ratio, tool breakdown."""

from textual.app import ComposeResult
from textual.containers import Container, Horizontal
from textual.widget import Widget
from textual.widgets import DataTable, Static

from hookwise_tui.data import read_analytics
from hookwise_tui.widgets.sparkline import SparklineWidget


def _ai_ratio_bar(score: float, width: int = 30) -> str:
    """Render AI authorship ratio as a visual progress bar."""
    pct = max(0, min(100, score * 100))
    filled = int(pct / 100 * width)
    empty = width - filled
    bar = f"[magenta]{'█' * filled}[/magenta][dim]{'░' * empty}[/dim]"
    return f"{bar} {pct:.0f}% AI"


class AnalyticsTab(Widget):
    """Analytics — coding patterns, tool usage, and AI authorship."""

    DEFAULT_CSS = """
    AnalyticsTab {
        height: auto;
    }
    AnalyticsTab .analytics-intro {
        color: $text-muted;
        margin: 0 0 1 0;
    }
    AnalyticsTab .section-title {
        text-style: bold;
        color: $accent;
        margin: 1 0 0 0;
    }
    AnalyticsTab .ai-ratio {
        margin: 1 0;
        padding: 1 2;
        border: round $primary;
        background: $surface-darken-1;
        height: auto;
    }
    AnalyticsTab .metric-row {
        layout: horizontal;
        height: auto;
        margin: 0 0 1 0;
    }
    AnalyticsTab .metric-box {
        padding: 1 2;
        margin: 0 1 0 0;
        border: round $primary;
        background: $surface-darken-1;
        width: 1fr;
        height: auto;
    }
    """

    def compose(self) -> ComposeResult:
        data = read_analytics(days=7)

        yield Static(
            "All data stored locally in SQLite — never sent anywhere.",
            classes="analytics-intro",
        )

        # Session summary metrics
        total_sessions = sum(d.sessions for d in data.daily)
        total_events = sum(d.total_events for d in data.daily)
        total_lines = sum(d.lines_added for d in data.daily)

        with Horizontal(classes="metric-row"):
            yield Static(
                f"Sessions (7d)\n[bold cyan]{total_sessions}[/bold cyan]",
                classes="metric-box",
            )
            yield Static(
                f"Tool Calls\n[bold cyan]{total_events}[/bold cyan]",
                classes="metric-box",
            )
            yield Static(
                f"Lines Added\n[bold cyan]{total_lines}[/bold cyan]",
                classes="metric-box",
            )

        # Daily sparklines
        if data.daily:
            dates = sorted([d.date for d in data.daily])
            sessions_by_date = {d.date: d.sessions for d in data.daily}
            lines_by_date = {d.date: d.lines_added for d in data.daily}

            session_values = [sessions_by_date.get(d, 0) for d in dates]
            line_values = [lines_by_date.get(d, 0) for d in dates]

            yield SparklineWidget(
                label="Sessions/day",
                values=session_values,
                current_value=str(session_values[-1]) if session_values else "0",
            )
            yield SparklineWidget(
                label="Lines added/day",
                values=line_values,
                current_value=str(line_values[-1]) if line_values else "0",
            )

        # AI Authorship
        yield Static("AI Authorship", classes="section-title")
        with Container(classes="ai-ratio"):
            yield Static(_ai_ratio_bar(data.authorship.weighted_ai_score))
            breakdown = data.authorship.breakdown
            if breakdown:
                parts = [f"{k}: {v}" for k, v in breakdown.items()]
                yield Static(f"[dim]{' | '.join(parts)}[/dim]")

        # Tool breakdown
        if data.tools:
            yield Static("Top Tools", classes="section-title")
            table = DataTable()
            table.add_columns("Tool", "Calls", "Lines +", "Lines -")
            for t in data.tools[:10]:
                table.add_row(t.tool_name, str(t.count), str(t.lines_added), str(t.lines_removed))
            yield table
        else:
            yield Static("[dim]No analytics data yet. Enable analytics in hookwise.yaml.[/dim]")
