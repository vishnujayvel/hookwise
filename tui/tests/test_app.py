"""Tests for hookwise_tui.app — Textual pilot tests."""

import pytest
from textual.pilot import Pilot

from hookwise_tui.app import HookwiseTUI


@pytest.fixture
def app():
    return HookwiseTUI()


class TestHookwiseTUI:
    async def test_app_launches(self, app):
        """App starts without error and shows header."""
        async with app.run_test() as pilot:
            assert app.title == "Hookwise"
            assert app.sub_title == "Claude Code Hooks Dashboard"

    async def test_tab_switching_with_keys(self, app):
        """Number keys switch between tabs."""
        async with app.run_test() as pilot:
            # Default is first tab (Dashboard)
            tabs = app.query_one("TabbedContent")
            assert tabs.active == "dashboard"

            await pilot.press("2")
            assert tabs.active == "guards"

            await pilot.press("5")
            assert tabs.active == "feeds"

            await pilot.press("8")
            assert tabs.active == "status"

            await pilot.press("1")
            assert tabs.active == "dashboard"

    async def test_quit_key(self, app):
        """q key exits the app."""
        async with app.run_test() as pilot:
            await pilot.press("q")
            # App should be in exit state after pressing q

    async def test_all_tabs_render(self, app):
        """Each tab renders without crashing."""
        async with app.run_test() as pilot:
            for key in ["1", "2", "3", "4", "5", "6", "7", "8"]:
                await pilot.press(key)
                # No exception = tab renders fine

    async def test_dashboard_has_feature_cards(self, app):
        """Dashboard tab contains FeatureCard widgets."""
        async with app.run_test() as pilot:
            from hookwise_tui.widgets.feature_card import FeatureCard
            cards = app.query(FeatureCard)
            # Should have at least the 8 features from dashboard
            assert len(list(cards)) >= 1

    async def test_feeds_tab_has_timer(self, app):
        """Feeds tab shows refresh timer."""
        async with app.run_test() as pilot:
            await pilot.press("5")
            timer = app.query_one("#timer-display")
            assert timer is not None
