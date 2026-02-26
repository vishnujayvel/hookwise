"""Dashboard tab — Overview grid with feature cards."""

from textual.app import ComposeResult
from textual.containers import Container
from textual.widget import Widget

from hookwise_tui.data import read_config
from hookwise_tui.widgets.feature_card import FeatureCard


# Feature descriptions for every hookwise feature
FEATURES = [
    {
        "key": "analytics",
        "title": "Analytics",
        "description": "Track tool usage, AI authorship ratio, and coding patterns over time",
        "config_path": ("analytics", "enabled"),
    },
    {
        "key": "guards",
        "title": "Guards",
        "description": "Safety rules that block, warn, or confirm tool actions before they execute",
        "config_path": None,  # Always on if guards list non-empty
    },
    {
        "key": "coaching",
        "title": "Coaching",
        "description": "AI-powered coding guidance — metacognition, builder's trap, and communication coaching",
        "config_path": None,  # Check sub-features
    },
    {
        "key": "feeds",
        "title": "Feeds",
        "description": "Live data from calendar, news, git, and usage insights — powered by the background daemon",
        "config_path": None,  # Check if any feed enabled
    },
    {
        "key": "status_line",
        "title": "Status Line",
        "description": "Two-tier display showing feeds, costs, and context in your terminal",
        "config_path": ("status_line", "enabled"),
    },
    {
        "key": "cost_tracking",
        "title": "Cost Tracking",
        "description": "Monitor API spending with daily budget enforcement and per-model rates",
        "config_path": ("cost_tracking", "enabled"),
    },
    {
        "key": "greeting",
        "title": "Greeting",
        "description": "Random motivational quote at session start to set the right mindset",
        "config_path": ("greeting", "enabled"),
    },
    {
        "key": "transcript_backup",
        "title": "Transcript Backup",
        "description": "Auto-save session transcripts for later reference and auditing",
        "config_path": ("transcript_backup", "enabled"),
    },
]


def _is_enabled(config: dict, feature: dict) -> bool:
    """Check if a feature is enabled in the config."""
    if feature["config_path"] is None:
        key = feature["key"]
        if key == "guards":
            guards = config.get("guards", [])
            return isinstance(guards, list) and len(guards) > 0
        if key == "coaching":
            coaching = config.get("coaching", {})
            if not isinstance(coaching, dict):
                return False
            for sub in coaching.values():
                if isinstance(sub, dict) and sub.get("enabled"):
                    return True
            return False
        if key == "feeds":
            feeds = config.get("feeds", {})
            if not isinstance(feeds, dict):
                return False
            for name, feed in feeds.items():
                if isinstance(feed, dict) and feed.get("enabled"):
                    return True
            return False
        return False

    section, field = feature["config_path"]
    section_data = config.get(section, {})
    if isinstance(section_data, dict):
        return bool(section_data.get(field, False))
    return False


def _get_detail(config: dict, feature: dict) -> str:
    """Get extra detail text for a feature."""
    key = feature["key"]
    if key == "guards":
        guards = config.get("guards", [])
        count = len(guards) if isinstance(guards, list) else 0
        return f"{count} guard rule{'s' if count != 1 else ''} configured"
    if key == "coaching":
        coaching = config.get("coaching", {})
        if not isinstance(coaching, dict):
            return ""
        active = [
            name for name, sub in coaching.items()
            if isinstance(sub, dict) and sub.get("enabled")
        ]
        if active:
            return f"Active: {', '.join(active)}"
        return "All coaching features disabled"
    if key == "feeds":
        feeds = config.get("feeds", {})
        if not isinstance(feeds, dict):
            return ""
        active = [
            name for name, feed in feeds.items()
            if isinstance(feed, dict) and feed.get("enabled")
        ]
        if active:
            return f"Active feeds: {', '.join(active)}"
        return "No feeds enabled"
    if key == "cost_tracking":
        ct = config.get("cost_tracking", {})
        if isinstance(ct, dict):
            budget = ct.get("daily_budget", "?")
            return f"Daily budget: ${budget}"
    return ""


class DashboardTab(Widget):
    """Dashboard — overview of all hookwise features."""

    DEFAULT_CSS = """
    DashboardTab {
        height: auto;
    }
    DashboardTab .dash-grid {
        layout: grid;
        grid-size: 2;
        grid-gutter: 1;
        height: auto;
        padding: 0;
    }
    DashboardTab .dash-header {
        text-style: bold;
        color: $accent;
        margin: 0 0 1 0;
    }
    DashboardTab .dash-summary {
        color: $text-muted;
        margin: 0 0 1 0;
    }
    """

    def compose(self) -> ComposeResult:
        config = read_config()

        enabled_count = sum(1 for f in FEATURES if _is_enabled(config, f))
        total = len(FEATURES)

        yield Container(
            *[
                FeatureCard(
                    title=f["title"],
                    description=f["description"],
                    enabled=_is_enabled(config, f),
                    detail=_get_detail(config, f),
                )
                for f in FEATURES
            ],
            classes="dash-grid",
        )
