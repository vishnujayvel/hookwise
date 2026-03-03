"""Main Hookwise TUI application — Textual app with 8 tabbed views + weather background."""

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Container
from textual.widgets import Footer, Header, TabbedContent, TabPane

from hookwise_tui.tabs.dashboard import DashboardTab
from hookwise_tui.tabs.guards import GuardsTab
from hookwise_tui.tabs.coaching import CoachingTab
from hookwise_tui.tabs.analytics import AnalyticsTab
from hookwise_tui.tabs.feeds import FeedsTab
from hookwise_tui.tabs.insights import InsightsTab
from hookwise_tui.tabs.recipes import RecipesTab
from hookwise_tui.tabs.status import StatusTab
from hookwise_tui.widgets.weather_background import WeatherBackground
from hookwise_tui.widgets.weather_data import WeatherInfo, get_weather


# Map weather conditions to background engine values
_CONDITION_TO_WEATHER = {
    "clear": "sun", "sun": "sun", "cloudy": "cloudy", "fog": "fog",
    "drizzle": "drizzle", "rain": "rain", "heavy_rain": "heavy_rain",
    "snow": "snow", "heavy_snow": "heavy_snow",
    "thunderstorm": "thunderstorm",
}


class HookwiseTUI(App):
    """Hookwise — Claude Code hooks dashboard with animated weather background."""

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
        Binding("w", "cycle_weather", "Weather", show=False),
    ]

    # Weather cycle order for the 'w' key
    _WEATHER_CYCLE = [
        "rain", "drizzle", "heavy_rain", "thunderstorm",
        "snow", "heavy_snow", "sun", "fog", "cloudy",
    ]

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        self._weather_index = 0

    def compose(self) -> ComposeResult:
        yield Header()

        # Weather background layer — renders behind all content
        # weather_info=None uses default; actual weather loaded in on_mount()
        yield WeatherBackground(
            weather_info=None,
            id="weather-bg",
        )

        # Content layer — tabs sit on top of the weather background
        with Container(id="content-layer"):
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

    def on_mount(self) -> None:
        # Load weather (may do a network fetch if cache is empty)
        try:
            weather_info = get_weather()
        except Exception:
            weather_info = WeatherInfo(
                city="Local", condition="rain", code=61,
                temp_c=9, temp_f=48, wind_speed=12,
            )
        bg = self.query_one("#weather-bg", WeatherBackground)
        initial = _CONDITION_TO_WEATHER.get(
            weather_info.condition, "rain"
        )
        bg.weather = initial

        # Find the initial index in the cycle
        if initial in self._WEATHER_CYCLE:
            self._weather_index = self._WEATHER_CYCLE.index(initial)

    def action_switch_tab(self, tab_id: str) -> None:
        tabs = self.query_one(TabbedContent)
        tabs.active = tab_id

    def action_cycle_weather(self) -> None:
        """Cycle through weather conditions with the 'w' key."""
        self._weather_index = (self._weather_index + 1) % len(self._WEATHER_CYCLE)
        new_weather = self._WEATHER_CYCLE[self._weather_index]
        self.query_one("#weather-bg", WeatherBackground).weather = new_weather
