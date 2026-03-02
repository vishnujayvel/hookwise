"""Main Hookwise TUI application — Textual app with 8 tabbed views."""

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.widgets import Footer, Header, TabbedContent, TabPane

from hookwise_tui.tabs.dashboard import DashboardTab
from hookwise_tui.tabs.guards import GuardsTab
from hookwise_tui.tabs.coaching import CoachingTab
from hookwise_tui.tabs.analytics import AnalyticsTab
from hookwise_tui.tabs.feeds import FeedsTab
from hookwise_tui.tabs.insights import InsightsTab
from hookwise_tui.tabs.recipes import RecipesTab
from hookwise_tui.tabs.status import StatusTab


class HookwiseTUI(App):
    """Hookwise — Claude Code hooks dashboard."""

    TITLE = "Hookwise"
    SUB_TITLE = "Claude Code Hooks Dashboard"
    CSS_PATH = "app.tcss"

    BINDINGS = [
        Binding("1", "switch_tab('dashboard')", "Dashboard", show=True),
        Binding("2", "switch_tab('guards')", "Guards", show=True),
        Binding("3", "switch_tab('coaching')", "Coaching", show=True),
        Binding("4", "switch_tab('analytics')", "Analytics", show=True),
        Binding("5", "switch_tab('feeds')", "Feeds", show=True),
        Binding("6", "switch_tab('insights')", "Insights", show=True),
        Binding("7", "switch_tab('recipes')", "Recipes", show=True),
        Binding("8", "switch_tab('status')", "Status", show=True),
        Binding("q", "quit", "Quit", show=True),
    ]

    def compose(self) -> ComposeResult:
        yield Header()
        with TabbedContent(
            "Dashboard",
            "Guards",
            "Coaching",
            "Analytics",
            "Feeds",
            "Insights",
            "Recipes",
            "Status",
            id="tabs",
        ):
            with TabPane("Dashboard", id="dashboard"):
                yield DashboardTab()
            with TabPane("Guards", id="guards"):
                yield GuardsTab()
            with TabPane("Coaching", id="coaching"):
                yield CoachingTab()
            with TabPane("Analytics", id="analytics"):
                yield AnalyticsTab()
            with TabPane("Feeds", id="feeds"):
                yield FeedsTab()
            with TabPane("Insights", id="insights"):
                yield InsightsTab()
            with TabPane("Recipes", id="recipes"):
                yield RecipesTab()
            with TabPane("Status", id="status"):
                yield StatusTab()
        yield Footer()

    def action_switch_tab(self, tab_id: str) -> None:
        tabs = self.query_one(TabbedContent)
        tabs.active = tab_id
