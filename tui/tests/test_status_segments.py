"""Cross-boundary tests for StatusTab._render_segment().

These bind the Python status-line renderer to the EXACT field names the Go
feed producers emit (after FlattenForTUI spreads the envelope's data fields to
the top level of each cache entry). The renderer reads producer field names
directly -- there is no rename layer in data.py -- so a mismatch silently
degrades a segment in production (the Go->JSON->Python class, cf. weather bug
#29). See GitHub issue #155: calendar/project read names the producers never
emit. Canonical side: the Go producer (consistent + fixtured + tested); the
Python renderer adapts to it here.
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone

from hookwise_tui.tabs.status import StatusTab


class TestCalendarSegment:
    """The calendar producer emits event 'name'; the renderer must read it."""

    def test_current_event_renders_producer_name(self):
        # Mirrors feeds.CalendarTestFixture(): events carry 'name', not 'title'.
        cache = {
            "calendar": {
                "events": [
                    {
                        "name": "Standup",
                        "start": "2026-03-07T10:30:00Z",
                        "end": "2999-01-01T00:00:00Z",  # far future => "is current"
                        "all_day": False,
                        "is_current": True,
                    }
                ],
                "next_event": {"name": "Standup", "start": "2026-03-07T10:30:00Z"},
            }
        }
        out = StatusTab._render_segment("calendar", cache)
        assert "Standup" in out, (
            "calendar must render the producer's event 'name'; reading 'title' "
            "yields '?' (issue #155)"
        )
        assert "?" not in out

    def test_next_event_renders_producer_name_not_free(self):
        # No current event, one upcoming ~30min out (inside the <=60min naming
        # window) -> must show its name. Reading 'title' here returned "Free"
        # because next_event.get("title") was falsy (issue #155).
        soon = (datetime.now(timezone.utc) + timedelta(minutes=30)).strftime(
            "%Y-%m-%dT%H:%M:%SZ"
        )
        cache = {
            "calendar": {
                "events": [{"name": "Standup", "start": soon, "is_current": False}],
                "next_event": {"name": "Standup", "start": soon},
            }
        }
        out = StatusTab._render_segment("calendar", cache)
        assert "Standup" in out, (
            "next_event must render the producer's 'name'; reading 'title' falls "
            "through to 'Free' (issue #155)"
        )
