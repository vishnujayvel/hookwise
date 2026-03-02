"""Insights tab — Claude Code usage analytics with daily LLM summary."""

from textual.app import ComposeResult
from textual.containers import Container, Horizontal
from textual.widget import Widget
from textual.widgets import Button, Static

from hookwise_tui.data import (
    read_insights,
    read_insights_summary,
    generate_insights_summary,
)
from hookwise_tui.widgets.sparkline import SparklineWidget


class InsightsTab(Widget):
    """Insights — Claude Code usage patterns and productivity metrics."""

    DEFAULT_CSS = """
    InsightsTab {
        height: auto;
    }
    InsightsTab .insights-intro {
        color: $text-muted;
        margin: 0 0 1 0;
    }
    InsightsTab .metric-row {
        layout: horizontal;
        height: auto;
        margin: 0 0 1 0;
    }
    InsightsTab .metric-box {
        padding: 1 2;
        margin: 0 1 0 0;
        border: round $primary;
        background: $surface-darken-1;
        width: 1fr;
        height: auto;
    }
    InsightsTab .section-title {
        text-style: bold;
        color: $accent;
        margin: 1 0 0 0;
    }
    InsightsTab .llm-summary {
        border: round $warning;
        padding: 1 2;
        margin: 1 0;
        background: $surface-darken-1;
        height: auto;
    }
    InsightsTab .summary-header {
        layout: horizontal;
        height: auto;
    }
    InsightsTab .summary-title {
        text-style: bold;
        color: $warning;
        width: 1fr;
    }
    InsightsTab .summary-time {
        color: $text-disabled;
        text-style: italic;
    }
    InsightsTab .summary-section {
        margin: 1 0 0 0;
    }
    InsightsTab .summary-label {
        text-style: bold;
        color: $text;
    }
    InsightsTab .summary-text {
        color: $text-muted;
        margin: 0 0 0 2;
    }
    InsightsTab .friction-table {
        margin: 1 0;
        padding: 1 2;
        border: round $error;
        background: $surface-darken-1;
        height: auto;
    }
    InsightsTab .top-tools {
        margin: 1 0;
        padding: 1 2;
        border: round $primary;
        background: $surface-darken-1;
        height: auto;
    }
    """

    def compose(self) -> ComposeResult:
        insights = read_insights()

        yield Static(
            "Aggregated from ~/.claude/usage-data/ — last 30 days.",
            classes="insights-intro",
        )

        # Key metrics
        with Horizontal(classes="metric-row"):
            yield Static(
                f"Sessions\n[bold cyan]{insights.total_sessions}[/bold cyan]",
                classes="metric-box",
            )
            yield Static(
                f"Messages\n[bold cyan]{insights.total_messages}[/bold cyan]",
                classes="metric-box",
            )
            yield Static(
                f"Lines Added\n[bold cyan]{insights.total_lines_added}[/bold cyan]",
                classes="metric-box",
            )
            yield Static(
                f"Avg Duration\n[bold cyan]{insights.avg_duration_minutes}m[/bold cyan]",
                classes="metric-box",
            )

        with Horizontal(classes="metric-row"):
            yield Static(
                f"Peak Hour\n[bold cyan]{insights.peak_hour}:00[/bold cyan]",
                classes="metric-box",
            )
            yield Static(
                f"Days Active\n[bold cyan]{insights.days_active}[/bold cyan]",
                classes="metric-box",
            )
            yield Static(
                f"Friction Events\n[bold cyan]{insights.friction_total}[/bold cyan]",
                classes="metric-box",
            )
            yield Static(
                f"Top Tool\n[bold cyan]{insights.top_tools[0][0] if insights.top_tools else 'N/A'}[/bold cyan]",
                classes="metric-box",
            )

        # Sparkline trends
        if insights.daily_sessions:
            yield Static("Trends (30d)", classes="section-title")
            dates = sorted(insights.daily_sessions.keys())

            session_vals = [insights.daily_sessions.get(d, 0) for d in dates]
            message_vals = [insights.daily_messages.get(d, 0) for d in dates]
            line_vals = [insights.daily_lines.get(d, 0) for d in dates]

            yield SparklineWidget(
                label="Sessions/day",
                values=session_vals,
                current_value=str(session_vals[-1]) if session_vals else "0",
            )
            yield SparklineWidget(
                label="Messages/day",
                values=message_vals,
                current_value=str(message_vals[-1]) if message_vals else "0",
            )
            yield SparklineWidget(
                label="Lines/day",
                values=line_vals,
                current_value=str(line_vals[-1]) if line_vals else "0",
            )

        # Top tools
        if insights.top_tools:
            with Container(classes="top-tools"):
                yield Static("[bold]Top Tools (30d)[/bold]")
                for name, count in insights.top_tools[:8]:
                    bar_len = min(30, int(count / max(t[1] for t in insights.top_tools) * 30))
                    bar = "█" * bar_len
                    yield Static(f"  {name:20s} [cyan]{bar}[/cyan] {count}")

        # Friction breakdown
        if insights.friction_counts:
            with Container(classes="friction-table"):
                yield Static("[bold red]Friction Events[/bold red]")
                for cat, count in sorted(
                    insights.friction_counts.items(),
                    key=lambda x: -x[1],
                ):
                    yield Static(f"  [red]{cat}[/red]: {count}")

        # LLM Summary
        yield Static("Daily AI Summary", classes="section-title")
        yield Container(id="llm-summary", classes="llm-summary")
        yield Button("Refresh Summary", id="refresh-summary", variant="warning")

    def on_mount(self) -> None:
        self._load_summary()

    def _load_summary(self) -> None:
        summary_container = self.query_one("#llm-summary", Container)
        summary_container.remove_children()

        summary = read_insights_summary()
        if summary:
            summary_container.mount(
                Static(f"[dim italic]Generated: {summary.generated_at}[/dim italic]")
            )
            if summary.patterns:
                summary_container.mount(
                    Static(f"\n[bold]Patterns:[/bold] {summary.patterns}")
                )
            if summary.top_insight:
                summary_container.mount(
                    Static(f"[bold]Top Insight:[/bold] {summary.top_insight}")
                )
            if summary.focus_area:
                summary_container.mount(
                    Static(f"[bold]Focus Area:[/bold] {summary.focus_area}")
                )
        else:
            summary_container.mount(
                Static(
                    "[dim]No summary generated yet. "
                    "Click Refresh to generate (~$0.01, uses Haiku).[/dim]"
                )
            )

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "refresh-summary":
            insights = read_insights()
            generate_insights_summary(insights)
            self._load_summary()
