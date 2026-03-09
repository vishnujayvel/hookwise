"""Coaching tab — Three coaching features with interactive toggles."""

from textual.app import ComposeResult
from textual.containers import Horizontal
from textual.widget import Widget
from textual.widgets import Static, Switch

from hookwise_tui.data import read_config, write_config
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
                **{**{"yellow": 30, "orange": 60, "red": 90}, **cfg.get("thresholds", {})}
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
    CoachingTab .feature-row {
        height: auto;
        margin: 0 0 1 0;
    }
    CoachingTab .feature-row FeatureCard {
        width: 1fr;
    }
    CoachingTab .feature-row Switch {
        margin: 1 1 0 1;
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

            with Horizontal(classes="feature-row"):
                yield FeatureCard(
                    title=feature["title"],
                    description=feature["description"],
                    enabled=enabled,
                    detail=detail,
                )
                yield Switch(value=enabled, id=f"switch-{feature['key']}")

    def on_switch_changed(self, event: Switch.Changed) -> None:
        switch_id = event.switch.id or ""
        if not switch_id.startswith("switch-"):
            return
        feature_key = switch_id.removeprefix("switch-")

        config = read_config()
        coaching = config.get("coaching", {})
        if not isinstance(coaching, dict):
            coaching = {}
            config["coaching"] = coaching

        feature_cfg = coaching.get(feature_key, {})
        if not isinstance(feature_cfg, dict):
            feature_cfg = {}
        feature_cfg["enabled"] = event.value
        coaching[feature_key] = feature_cfg
        config["coaching"] = coaching

        if write_config(config):
            state = "enabled" if event.value else "disabled"
            self.notify(f"{feature_key} {state}")
        else:
            self.notify("Failed to save config", severity="error")
