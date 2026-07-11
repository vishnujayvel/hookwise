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
from typing import Any

from hookwise_tui.tabs.status import StatusTab


def _fresh() -> dict[str, Any]:
    """Freshness fields FlattenForTUI stamps on every cache entry.

    The renderer treats entries missing these (or past TTL) as absent,
    mirroring the Go status line's IsEnvelopeFresh gate.
    """
    now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    return {"updated_at": now, "ttl_seconds": 300}


def _stale() -> dict[str, Any]:
    """Freshness fields for an entry whose TTL has expired."""
    old = (datetime.now(timezone.utc) - timedelta(hours=2)).strftime(
        "%Y-%m-%dT%H:%M:%SZ"
    )
    return {"updated_at": old, "ttl_seconds": 300}


class TestCalendarSegment:
    """The calendar producer emits event 'name'; the renderer must read it."""

    def test_current_event_renders_producer_name(self) -> None:
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
                **_fresh(),
            }
        }
        out = StatusTab._render_segment("calendar", cache)
        assert "Standup" in out, (
            "calendar must render the producer's event 'name'; reading 'title' "
            "yields '?' (issue #155)"
        )
        assert "?" not in out

    def test_next_event_renders_producer_name_not_free(self) -> None:
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
                **_fresh(),
            }
        }
        out = StatusTab._render_segment("calendar", cache)
        assert "Standup" in out, (
            "next_event must render the producer's 'name'; reading 'title' falls "
            "through to 'Free' (issue #155)"
        )


class TestProjectSegment:
    """The project producer emits name/branch/last_commit/dirty/last_commit_ts;
    the renderer must read those, not repo/detached (issue #155)."""

    def test_renders_repo_name_from_producer_name_field(self) -> None:
        # Mirrors feeds.ProjectTestFixture(): the repo identity key is 'name'.
        cache = {
            "project": {
                "name": "hookwise",
                "branch": "main",
                "last_commit": "abc1234",
                "dirty": False,
                **_fresh(),
            }
        }
        out = StatusTab._render_segment("project", cache)
        assert "hookwise" in out, (
            "project must render the producer's 'name'; reading 'repo' gates the "
            "segment to empty (issue #155)"
        )
        assert "main" in out

    def test_marks_dirty_working_tree(self) -> None:
        # The producer emits 'dirty' (working-tree state), not 'detached'.
        cache = {
            "project": {"name": "hookwise", "branch": "main", "dirty": True, **_fresh()}
        }
        out = StatusTab._render_segment("project", cache)
        assert "hookwise" in out
        assert "*" in out, "a dirty working tree must show a '*' marker (issue #155)"

    def test_clean_tree_has_no_dirty_marker(self) -> None:
        cache = {
            "project": {"name": "hookwise", "branch": "main", "dirty": False, **_fresh()}
        }
        out = StatusTab._render_segment("project", cache)
        assert "*" not in out, "a clean tree must not show the dirty marker"

    def test_renders_commit_age_from_last_commit_ts(self) -> None:
        # The producer now emits 'last_commit_ts' (committer epoch); the renderer
        # turns it into an "Xm ago" suffix. Binds the new producer field name to
        # its consumer so it can't silently drift (issue #155).
        import time

        cache = {
            "project": {
                "name": "hookwise",
                "branch": "main",
                "last_commit_ts": int(time.time()) - 120,  # 2 minutes ago
                **_fresh(),
            }
        }
        out = StatusTab._render_segment("project", cache)
        assert "ago" in out, "last_commit_ts must render an 'ago' suffix"


class TestFreshnessGating:
    """Stale cache entries must render as absent, matching the Go status line
    (cmd_status_line.go feedData -> bridge.IsEnvelopeFresh). Without this gate
    a days-old calendar event renders as current forever (scout hw-xthx #1)."""

    @staticmethod
    def _calendar_events() -> dict[str, Any]:
        return {
            "events": [
                {
                    "name": "Standup",
                    "start": "2026-03-07T10:30:00Z",
                    "end": "2999-01-01T00:00:00Z",
                    "all_day": False,
                    "is_current": True,
                }
            ],
            "next_event": {"name": "Standup", "start": "2026-03-07T10:30:00Z"},
        }

    def test_stale_calendar_does_not_render(self) -> None:
        cache = {"calendar": {**self._calendar_events(), **_stale()}}
        out = StatusTab._render_segment("calendar", cache)
        assert out == "", (
            "an expired calendar entry must render as absent, not as a "
            "current event (and not as 'Free')"
        )

    def test_fresh_calendar_renders_unchanged(self) -> None:
        cache = {"calendar": {**self._calendar_events(), **_fresh()}}
        out = StatusTab._render_segment("calendar", cache)
        assert "Standup" in out

    def test_entry_missing_freshness_fields_does_not_render(self) -> None:
        # Go parity: IsEnvelopeFresh fails closed on a missing timestamp.
        # FlattenForTUI stamps updated_at/ttl_seconds on every entry, so a
        # bare entry is malformed and must be treated as absent.
        cache = {"calendar": self._calendar_events()}
        out = StatusTab._render_segment("calendar", cache)
        assert out == ""

    def test_stale_weather_does_not_render(self) -> None:
        cache = {
            "weather": {
                "temperature": 72,
                "temperatureUnit": "fahrenheit",
                "emoji": "☀️",
                **_stale(),
            }
        }
        assert StatusTab._render_segment("weather", cache) == ""

    def test_stale_insights_does_not_render(self) -> None:
        cache = {"insights": {"total_sessions": 5, "friction_total": 0, **_stale()}}
        assert StatusTab._render_segment("insights_friction", cache) == ""

    def test_segment_has_data_false_for_stale_entry(self) -> None:
        cache = {"calendar": {**self._calendar_events(), **_stale()}}
        assert StatusTab._segment_has_data("calendar", cache) is False

    def test_segment_has_data_true_for_fresh_entry(self) -> None:
        cache = {"calendar": {**self._calendar_events(), **_fresh()}}
        assert StatusTab._segment_has_data("calendar", cache) is True

    def test_segment_has_data_false_for_stale_insights(self) -> None:
        cache = {"insights": {"total_sessions": 5, **_stale()}}
        assert StatusTab._segment_has_data("insights_pace", cache) is False
