"""Sparkline widget for trend visualization using Rich Sparkline."""

from rich.text import Text
from textual.app import ComposeResult
from textual.widget import Widget
from textual.widgets import Static

# Unicode sparkline characters (blocks from lowest to highest)
SPARK_CHARS = "▁▂▃▄▅▆▇█"


def _sparkline_text(values: list[int | float], width: int = 30) -> str:
    """Render a list of values as a sparkline string."""
    if not values:
        return "[dim]no data[/dim]"

    # Pad or truncate to width
    if len(values) > width:
        values = values[-width:]

    min_val = min(values)
    max_val = max(values)
    value_range = max_val - min_val

    if value_range == 0:
        return SPARK_CHARS[3] * len(values)

    chars = []
    for v in values:
        idx = int((v - min_val) / value_range * (len(SPARK_CHARS) - 1))
        chars.append(SPARK_CHARS[idx])
    return "".join(chars)


class SparklineWidget(Widget):
    """A compact sparkline with label, value, and trend bar."""

    DEFAULT_CSS = """
    SparklineWidget {
        height: 3;
        padding: 0 1;
        margin: 0 0 1 0;
        border: round $primary;
        background: $surface-darken-1;
    }
    SparklineWidget .spark-label {
        text-style: bold;
        color: $text;
    }
    SparklineWidget .spark-value {
        color: $accent;
        text-style: bold;
    }
    SparklineWidget .spark-bar {
        color: $success;
    }
    """

    def __init__(
        self,
        label: str,
        values: list[int | float],
        current_value: str = "",
        **kwargs,
    ) -> None:
        super().__init__(**kwargs)
        self._label = label
        self._values = values
        self._current_value = current_value

    def compose(self) -> ComposeResult:
        spark = _sparkline_text(self._values)
        yield Static(
            f"{self._label}: [bold cyan]{self._current_value}[/bold cyan]",
            classes="spark-label",
        )
        yield Static(f"[green]{spark}[/green]", classes="spark-bar")
