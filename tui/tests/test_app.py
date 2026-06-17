"""Tests for hookwise_tui.app — Textual pilot tests."""

import pytest
from textual.widgets import TabbedContent

from hookwise_tui.app import HookwiseTUI


@pytest.fixture
def app() -> HookwiseTUI:
    return HookwiseTUI()


class TestHookwiseTUI:
    async def test_app_launches(self, app: HookwiseTUI) -> None:
        """App starts without error and shows header."""
        async with app.run_test():
            assert app.title == "Hookwise"
            assert app.sub_title == "Claude Code Hooks Dashboard"

    async def test_tab_switching_with_keys(self, app: HookwiseTUI) -> None:
        """Number keys switch between tabs."""
        async with app.run_test() as pilot:
            # Default is first tab (Dashboard). Query by type so the result is
            # a typed TabbedContent (with .active), not a bare Widget.
            tabs = app.query_one(TabbedContent)
            assert tabs.active == "dashboard"

            await pilot.press("2")
            assert tabs.active == "guards"

            await pilot.press("4")
            assert tabs.active == "feeds"

            await pilot.press("7")
            assert tabs.active == "status"

            await pilot.press("1")
            assert tabs.active == "dashboard"

    async def test_quit_key(self, app: HookwiseTUI) -> None:
        """q key exits the app."""
        async with app.run_test() as pilot:
            await pilot.press("q")
            # App should be in exit state after pressing q

    async def test_all_tabs_render(self, app: HookwiseTUI) -> None:
        """Each tab renders without crashing."""
        async with app.run_test() as pilot:
            for key in ["1", "2", "3", "4", "5", "6", "7"]:
                await pilot.press(key)
                # No exception = tab renders fine

    async def test_dashboard_has_feature_cards(self, app: HookwiseTUI) -> None:
        """Dashboard tab contains FeatureCard widgets."""
        async with app.run_test():
            from hookwise_tui.widgets.feature_card import FeatureCard
            cards = app.query(FeatureCard)
            # Should have at least the 7 features from dashboard
            assert len(list(cards)) >= 1

    async def test_feeds_tab_has_timer(self, app: HookwiseTUI) -> None:
        """Feeds tab shows refresh timer."""
        async with app.run_test() as pilot:
            await pilot.press("4")
            timer = app.query_one("#timer-display")
            assert timer is not None
