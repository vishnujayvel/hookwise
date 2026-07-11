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

import json
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, cast

from hookwise_tui.tabs.status import StatusTab

# Shared with internal/bridge's TestCalendarFeedContractFixture_FlattenForTUI:
# the Go side proves envelope -> expected_flattened, this file proves
# expected_flattened -> rendered segment.
_CALENDAR_FIXTURE = (
    Path(__file__).resolve().parents[2]
    / "testdata"
    / "contracts"
    / "feeds"
    / "calendar.json"
)


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


def _iso_in(minutes: float) -> str:
    """An ISO-8601 UTC timestamp `minutes` from now (negative = past)."""
    return (datetime.now(timezone.utc) + timedelta(minutes=minutes)).strftime(
        "%Y-%m-%dT%H:%M:%SZ"
    )


class TestCalendarContractFixture:
    """Python half of the shared calendar feed contract fixture
    (testdata/contracts/feeds/calendar.json). The Go side (internal/bridge)
    pins producer envelope -> flattened entry; this pins flattened entry ->
    rendered segment, so a field rename cannot pass one side silently."""

    @staticmethod
    def _fixture_entry() -> dict[str, Any]:
        with _CALENDAR_FIXTURE.open() as f:
            return cast("dict[str, Any]", json.load(f)["expected_flattened"])

    def test_fixture_fresh_stamped_renders_producer_event_name(self) -> None:
        # The fixture's updated_at is frozen in the past; re-stamp it fresh
        # (keeping ttl_seconds and every data field as-written) the way a live
        # daemon write would, since post-#249 the renderer gates on freshness.
        entry = {**self._fixture_entry(), "updated_at": _fresh()["updated_at"]}
        out = StatusTab._render_segment("calendar", {"calendar": entry})
        assert "Standup" in out, (
            "the shared contract fixture must render its event 'name'; a "
            "producer field rename that updates the fixture must fail here"
        )

    def test_fixture_as_written_is_stale_and_renders_absent(self) -> None:
        # As-written, the fixture's frozen updated_at is far past ttl_seconds:
        # the freshness gate must treat the entire entry as absent.
        entry = self._fixture_entry()
        assert entry["ttl_seconds"] == 300, "fixture must carry the default TTL"
        out = StatusTab._render_segment("calendar", {"calendar": entry})
        assert out == "", "a past-TTL fixture entry must render as absent"


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


class TestCalendarFormattingBranches:
    """Pins every formatting branch of the calendar renderer (status.py
    _render_segment 'calendar'): the current-event path with its ends-in and
    (+N more) suffixes and ValueError/KeyError parse fallbacks, and the
    next-event NOW / in-Xmin / Free-for-Xh bucketing (scout hw-xthx #9).
    Offsets sit mid-bucket so second-level test jitter can't cross an edge."""

    @staticmethod
    def _cache(
        events: list[dict[str, Any]], next_event: dict[str, Any] | None = None
    ) -> dict[str, Any]:
        entry: dict[str, Any] = {"events": events, **_fresh()}
        if next_event is not None:
            entry["next_event"] = next_event
        return {"calendar": entry}

    # -- current-event path --

    def test_current_event_shows_ends_in_suffix(self) -> None:
        events = [
            {"name": "Standup", "start": _iso_in(-30), "end": _iso_in(30), "is_current": True}
        ]
        out = StatusTab._render_segment("calendar", self._cache(events))
        assert "Standup" in out
        assert "ends in" in out, "a parseable 'end' must render the ends-in suffix"

    def test_current_event_missing_end_renders_without_suffix(self) -> None:
        # KeyError fallback: no 'end' key must drop the suffix, not crash.
        events = [{"name": "Standup", "start": _iso_in(-30), "is_current": True}]
        out = StatusTab._render_segment("calendar", self._cache(events))
        assert "Standup" in out
        assert "ends in" not in out

    def test_current_event_malformed_end_renders_without_suffix(self) -> None:
        # ValueError fallback: an unparseable 'end' must drop the suffix.
        events = [{"name": "Standup", "end": "not-a-timestamp", "is_current": True}]
        out = StatusTab._render_segment("calendar", self._cache(events))
        assert "Standup" in out
        assert "ends in" not in out

    def test_current_event_counts_additional_events(self) -> None:
        events = [
            {"name": "Standup", "end": _iso_in(30), "is_current": True},
            {"name": "Review", "start": _iso_in(60), "is_current": False},
            {"name": "1:1", "start": _iso_in(120), "is_current": False},
        ]
        out = StatusTab._render_segment("calendar", self._cache(events))
        assert "(+2 more)" in out

    # -- next-event path: fallbacks --

    def test_no_events_renders_free(self) -> None:
        out = StatusTab._render_segment("calendar", self._cache([]))
        assert out == "\U0001f4c5 Free"

    def test_next_event_without_name_renders_free(self) -> None:
        out = StatusTab._render_segment(
            "calendar", self._cache([], next_event={"start": _iso_in(30)})
        )
        assert out == "\U0001f4c5 Free"

    def test_next_event_malformed_start_falls_back_to_bare_name(self) -> None:
        # ValueError fallback: name still renders, without any time bucket.
        out = StatusTab._render_segment(
            "calendar",
            self._cache([], next_event={"name": "Standup", "start": "not-a-timestamp"}),
        )
        assert out == "\U0001f4c5 Standup"

    def test_next_event_missing_start_falls_back_to_bare_name(self) -> None:
        # KeyError fallback: no 'start' key at all.
        out = StatusTab._render_segment(
            "calendar", self._cache([], next_event={"name": "Standup"})
        )
        assert out == "\U0001f4c5 Standup"

    # -- next-event path: time bucketing --

    def test_next_event_within_5min_renders_now(self) -> None:
        ev = {"name": "Standup", "start": _iso_in(2), "is_current": False}
        out = StatusTab._render_segment("calendar", self._cache([ev], next_event=ev))
        assert "Standup NOW" in out

    def test_next_event_within_15min_renders_min_with_bolt(self) -> None:
        ev = {"name": "Standup", "start": _iso_in(10), "is_current": False}
        out = StatusTab._render_segment("calendar", self._cache([ev], next_event=ev))
        assert "in 10min" in out
        assert "⚡" in out, "5-15min out must carry the imminent bolt"

    def test_next_event_within_hour_renders_min_without_bolt(self) -> None:
        ev = {"name": "Standup", "start": _iso_in(30), "is_current": False}
        out = StatusTab._render_segment("calendar", self._cache([ev], next_event=ev))
        assert "in 30min" in out
        assert "⚡" not in out, "15-60min out must not carry the bolt"

    def test_next_event_beyond_hour_renders_free_for_hours(self) -> None:
        ev = {"name": "Standup", "start": _iso_in(180), "is_current": False}
        out = StatusTab._render_segment("calendar", self._cache([ev], next_event=ev))
        assert "Free for 3h" in out
        assert "Standup" not in out, ">60min out shows free time, not the event"

    def test_next_event_path_counts_additional_events(self) -> None:
        events = [
            {"name": "Standup", "start": _iso_in(30), "is_current": False},
            {"name": "Review", "start": _iso_in(90), "is_current": False},
        ]
        next_event = {"name": "Standup", "start": _iso_in(30)}
        out = StatusTab._render_segment("calendar", self._cache(events, next_event))
        assert "(+1 more)" in out
