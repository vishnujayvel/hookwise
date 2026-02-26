"""Coaching tab — Three coaching features with user-friendly explanations."""

from textual.app import ComposeResult
from textual.widget import Widget
from textual.widgets import Static

from hookwise_tui.data import read_config
from hookwise_tui.widgets.feature_card import FeatureCard


COACHING_FEATURES = [
    {
        "key": "metacognition",
        "title": "Metacognition",
        "description": "Periodic nudges to reflect on your approach — prevents autopilot coding",
        "detail_fn": lambda cfg: (
            f"Interval: {cfg.get('interval_seconds', 300)}s"
            if cfg.get("enabled")
            else "Disabled"
        ),
    },
    {
        "key": "builder_trap",
        "title": "Builder's Trap",
        "description": "Alerts when you've been in tooling/config too long without shipping value",
        "detail_fn": lambda cfg: (
            "Thresholds: yellow={yellow}m, orange={orange}m, red={red}m".format(
                **cfg.get("thresholds", {"yellow": "?", "orange": "?", "red": "?"})
            )
            if cfg.get("enabled")
            else "Disabled"
        ),
    },
    {
        "key": "communication",
        "title": "Communication Coach",
        "description": "Grammar and clarity checks on your prompts (pattern-based, no LLM cost)",
        "detail_fn": lambda cfg: (
            f"Tone: {cfg.get('tone', 'gentle')}, "
            f"Min length: {cfg.get('min_length', 50)} chars, "
            f"Frequency: every {cfg.get('frequency', 3)} prompts"
            if cfg.get("enabled")
            else "Disabled"
        ),
    },
]


class CoachingTab(Widget):
    """Coaching — AI-powered coding guidance and habit awareness."""

    DEFAULT_CSS = """
    CoachingTab {
        height: auto;
    }
    CoachingTab .coaching-header {
        text-style: bold;
        color: $accent;
        margin: 0 0 1 0;
    }
    CoachingTab .coaching-intro {
        color: $text-muted;
        margin: 0 0 1 0;
    }
    """

    def compose(self) -> ComposeResult:
        config = read_config()
        coaching = config.get("coaching", {})
        if not isinstance(coaching, dict):
            coaching = {}

        yield Static(
            "Coaching features help you build better coding habits without "
            "slowing you down. Each runs locally with zero LLM cost.",
            classes="coaching-intro",
        )

        for feature in COACHING_FEATURES:
            cfg = coaching.get(feature["key"], {})
            if not isinstance(cfg, dict):
                cfg = {}
            enabled = cfg.get("enabled", False)
            detail = feature["detail_fn"](cfg)

            yield FeatureCard(
                title=feature["title"],
                description=feature["description"],
                enabled=enabled,
                detail=detail,
            )
