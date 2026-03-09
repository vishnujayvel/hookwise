"""Reusable feature card widget — title, description, enabled badge."""

from textual.app import ComposeResult
from textual.containers import Horizontal
from textual.widget import Widget
from textual.widgets import Static


class FeatureCard(Widget):
    """A bordered card showing a feature's name, description, and enabled status."""

    DEFAULT_CSS = """
    FeatureCard {
        height: auto;
        margin: 0 0 1 0;
        padding: 1 2;
        border: round $accent;
        background: $surface-darken-1;
    }
    FeatureCard.enabled {
        border: round $success;
    }
    FeatureCard.disabled {
        border: round $error-darken-1;
        opacity: 0.7;
    }
    FeatureCard .card-header {
        layout: horizontal;
        height: auto;
    }
    FeatureCard .card-title {
        text-style: bold;
        color: $text;
        width: 1fr;
        min-width: 20;
    }
    FeatureCard .card-badge-on {
        color: $success;
        text-style: bold;
    }
    FeatureCard .card-badge-off {
        color: $error;
    }
    FeatureCard .card-description {
        color: $text-muted;
        margin-top: 1;
    }
    FeatureCard .card-detail {
        color: $text-disabled;
        margin-top: 0;
    }
    """

    def __init__(
        self,
        title: str,
        description: str,
        enabled: bool = False,
        detail: str = "",
        **kwargs,
    ) -> None:
        super().__init__(**kwargs)
        self._title = title
        self._description = description
        self._enabled = enabled
        self._detail = detail
        self.add_class("enabled" if enabled else "disabled")

    def compose(self) -> ComposeResult:
        with Horizontal(classes="card-header"):
            yield Static(self._title, classes="card-title")
            badge = "[ON]" if self._enabled else "[OFF]"
            cls = "card-badge-on" if self._enabled else "card-badge-off"
            yield Static(badge, classes=cls)
        yield Static(self._description, classes="card-description")
        if self._detail:
            yield Static(self._detail, classes="card-detail")
