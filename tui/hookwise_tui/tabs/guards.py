"""Guards tab — DataTable of guard rules with descriptions."""

from textual.app import ComposeResult
from textual.widget import Widget
from textual.widgets import DataTable, Static

from hookwise_tui.data import read_config


ACTION_DESCRIPTIONS = {
    "block": "Prevents the tool from executing entirely",
    "warn": "Shows a warning message but allows execution",
    "confirm": "Asks for user confirmation before executing",
}


class GuardsTab(Widget):
    """Guards — safety rules that control tool execution."""

    DEFAULT_CSS = """
    GuardsTab {
        height: auto;
    }
    GuardsTab .guards-header {
        text-style: bold;
        color: $accent;
        margin: 0 0 1 0;
    }
    GuardsTab .guards-info {
        color: $text-muted;
        margin: 0 0 1 0;
    }
    GuardsTab .action-legend {
        margin: 1 0;
        padding: 1 2;
        border: round $primary;
        background: $surface-darken-1;
        height: auto;
    }
    GuardsTab .action-legend-title {
        text-style: bold;
        color: $text;
        margin: 0 0 1 0;
    }
    """

    def compose(self) -> ComposeResult:
        config = read_config()
        guards = config.get("guards", [])
        if not isinstance(guards, list):
            guards = []

        yield Static(
            "Guard rules are evaluated top-to-bottom — first match wins.",
            classes="guards-info",
        )

        # Action legend
        yield Static("Action Types", classes="action-legend-title")
        for action, desc in ACTION_DESCRIPTIONS.items():
            color = {"block": "red", "warn": "yellow", "confirm": "cyan"}[action]
            yield Static(
                f"  [{color}]{action.upper()}[/{color}] — {desc}"
            )

        # Guard rules table
        table = DataTable()
        table.add_columns("#", "Match", "Action", "Reason", "Condition")

        for i, guard in enumerate(guards):
            if not isinstance(guard, dict):
                continue
            match = str(guard.get("match", ""))
            action = str(guard.get("action", ""))
            reason = str(guard.get("reason", ""))
            when = str(guard.get("when", "—"))
            unless = guard.get("unless")
            condition = when
            if unless:
                condition = f"{when} (unless: {unless})"

            table.add_row(
                str(i + 1),
                match,
                action.upper(),
                reason[:60] + ("..." if len(reason) > 60 else ""),
                condition[:50] + ("..." if len(str(condition)) > 50 else ""),
            )

        if guards:
            yield table
        else:
            yield Static(
                "[dim]No guard rules configured. Add guards to hookwise.yaml.[/dim]"
            )
