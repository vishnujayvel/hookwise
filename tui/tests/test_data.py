"""Tests for hookwise_tui.data — all data readers."""

import json
import os
import sqlite3
from pathlib import Path

import pytest
import yaml

from hookwise_tui.data import (
    AnalyticsData,
    DaemonStatus,
    DailySummary,
    FeedHealth,
    InsightsData,
    InsightsSummary,
    Recipe,
    _effective_config_path,
    is_fresh,
    read_analytics,
    read_cache,
    read_config,
    read_daemon_status,
    read_feed_health,
    read_insights,
    read_insights_summary,
    read_recipes,
    write_config,
)


@pytest.fixture
def tmp_dir(tmp_path):
    """Create a temporary directory for test data."""
    return tmp_path


# --- read_config ---


class TestReadConfig:
    def test_reads_yaml_config(self, tmp_dir):
        config_path = tmp_dir / "hookwise.yaml"
        config_path.write_text(
            yaml.dump({"version": 1, "guards": [{"match": "Bash", "action": "block"}]})
        )
        result = read_config(config_path)
        assert result["version"] == 1
        assert len(result["guards"]) == 1

    def test_missing_file_falls_back_to_global(self, tmp_dir):
        result = read_config(tmp_dir / "nonexistent.yaml")
        # Falls back to ~/.hookwise/config.yaml if it exists, else {}
        assert isinstance(result, dict)

    def test_malformed_yaml_returns_empty(self, tmp_dir):
        config_path = tmp_dir / "bad.yaml"
        config_path.write_text(":::invalid yaml{{{")
        result = read_config(config_path)
        assert result == {}

    def test_non_dict_yaml_returns_empty(self, tmp_dir):
        config_path = tmp_dir / "list.yaml"
        config_path.write_text("- item1\n- item2\n")
        result = read_config(config_path)
        assert result == {}


# --- write_config ---


class TestWriteConfig:
    def test_round_trip_consistency(self, tmp_dir):
        config_path = tmp_dir / "hookwise.yaml"
        original = {"version": 1, "guards": [{"match": "Bash", "action": "block"}]}
        assert write_config(original, config_path) is True
        result = read_config(config_path)
        assert result == original

    def test_creates_parent_directories(self, tmp_dir):
        nested_path = tmp_dir / "a" / "b" / "hookwise.yaml"
        assert write_config({"key": "val"}, nested_path) is True
        assert read_config(nested_path) == {"key": "val"}

    def test_overwrites_existing_config(self, tmp_dir):
        config_path = tmp_dir / "hookwise.yaml"
        write_config({"old": True}, config_path)
        write_config({"new": True}, config_path)
        assert read_config(config_path) == {"new": True}


# --- _effective_config_path ---


class TestEffectiveConfigPath:
    def test_explicit_path_takes_priority(self, tmp_dir):
        explicit = tmp_dir / "explicit.yaml"
        result = _effective_config_path(explicit)
        assert result == explicit

    def test_local_path_when_exists(self, tmp_dir, monkeypatch):
        local = tmp_dir / "hookwise.yaml"
        local.write_text("version: 1\n")
        monkeypatch.setattr("hookwise_tui.data._default_config_path", lambda: local)
        result = _effective_config_path(None)
        assert result == local

    def test_falls_back_to_global_when_local_missing(self, tmp_dir, monkeypatch):
        local = tmp_dir / "nonexistent" / "hookwise.yaml"
        global_path = tmp_dir / "global" / "config.yaml"
        global_path.parent.mkdir(parents=True)
        global_path.write_text("version: 2\n")
        monkeypatch.setattr("hookwise_tui.data._default_config_path", lambda: local)
        monkeypatch.setattr("hookwise_tui.data._config_dir", lambda: tmp_dir / "global")
        result = _effective_config_path(None)
        assert result == global_path

    def test_defaults_to_local_when_neither_exists(self, tmp_dir, monkeypatch):
        local = tmp_dir / "nonexistent" / "hookwise.yaml"
        monkeypatch.setattr("hookwise_tui.data._default_config_path", lambda: local)
        monkeypatch.setattr("hookwise_tui.data._config_dir", lambda: tmp_dir / "also_nonexistent")
        result = _effective_config_path(None)
        assert result == local


# --- read_cache ---


class TestReadCache:
    def test_reads_json_cache(self, tmp_dir):
        cache_path = tmp_dir / "cache.json"
        cache_path.write_text(json.dumps({
            "pulse": {"updated_at": "2026-02-23T12:00:00Z", "ttl_seconds": 30},
            "project": {"branch": "main"},
        }))
        result = read_cache(cache_path)
        assert "pulse" in result
        assert result["pulse"]["ttl_seconds"] == 30

    def test_missing_file_returns_empty(self, tmp_dir):
        result = read_cache(tmp_dir / "nonexistent.json")
        assert result == {}

    def test_corrupt_json_returns_empty(self, tmp_dir):
        cache_path = tmp_dir / "bad.json"
        cache_path.write_text("{broken json")
        result = read_cache(cache_path)
        assert result == {}


# --- is_fresh ---


class TestIsFresh:
    def test_fresh_entry(self):
        from datetime import datetime, timezone
        now = datetime.now(timezone.utc).isoformat()
        assert is_fresh({"updated_at": now, "ttl_seconds": 300}) is True

    def test_stale_entry(self):
        assert is_fresh({"updated_at": "2020-01-01T00:00:00Z", "ttl_seconds": 30}) is False

    def test_missing_updated_at(self):
        assert is_fresh({"ttl_seconds": 30}) is False

    def test_missing_ttl(self):
        assert is_fresh({"updated_at": "2026-01-01T00:00:00Z"}) is False

    def test_invalid_timestamp(self):
        assert is_fresh({"updated_at": "not-a-date", "ttl_seconds": 30}) is False

    def test_zero_ttl(self):
        assert is_fresh({"updated_at": "2026-01-01T00:00:00Z", "ttl_seconds": 0}) is False


# --- read_analytics ---


class TestReadAnalytics:
    def _create_db(self, db_path):
        conn = sqlite3.connect(str(db_path))
        conn.execute("""
            CREATE TABLE events (
                id INTEGER PRIMARY KEY, session_id TEXT, event_type TEXT,
                tool_name TEXT, timestamp TEXT, file_path TEXT,
                lines_added INTEGER, lines_removed INTEGER, ai_confidence_score REAL
            )
        """)
        conn.execute("""
            CREATE TABLE authorship_ledger (
                id INTEGER PRIMARY KEY, session_id TEXT, file_path TEXT,
                tool_name TEXT, lines_changed INTEGER, ai_score REAL,
                classification TEXT, timestamp TEXT
            )
        """)
        return conn

    def test_reads_analytics(self, tmp_dir):
        db_path = tmp_dir / "analytics.db"
        conn = self._create_db(db_path)
        conn.execute(
            "INSERT INTO events (session_id, tool_name, timestamp, lines_added, lines_removed) "
            "VALUES ('s1', 'Bash', datetime('now'), 10, 2)"
        )
        conn.commit()
        conn.close()

        result = read_analytics(db_path)
        assert isinstance(result, AnalyticsData)
        assert len(result.daily) >= 1
        assert result.daily[0].lines_added == 10

    def test_missing_db_returns_empty(self, tmp_dir):
        result = read_analytics(tmp_dir / "nonexistent.db")
        assert result.daily == []
        assert result.tools == []

    def test_tool_breakdown(self, tmp_dir):
        db_path = tmp_dir / "analytics.db"
        conn = self._create_db(db_path)
        for _ in range(5):
            conn.execute(
                "INSERT INTO events (session_id, tool_name, timestamp, lines_added, lines_removed) "
                "VALUES ('s1', 'Read', datetime('now'), 0, 0)"
            )
        conn.execute(
            "INSERT INTO events (session_id, tool_name, timestamp, lines_added, lines_removed) "
            "VALUES ('s1', 'Write', datetime('now'), 100, 0)"
        )
        conn.commit()
        conn.close()

        result = read_analytics(db_path)
        assert len(result.tools) == 2
        assert result.tools[0].tool_name == "Read"
        assert result.tools[0].count == 5


# --- read_daemon_status ---


class TestReadDaemonStatus:
    def test_missing_pid_file(self, tmp_dir):
        result = read_daemon_status(tmp_dir / "daemon.pid")
        assert result.running is False
        assert result.pid is None

    def test_invalid_pid_content(self, tmp_dir):
        pid_path = tmp_dir / "daemon.pid"
        pid_path.write_text("not-a-number")
        result = read_daemon_status(pid_path)
        assert result.running is False

    def test_stale_pid(self, tmp_dir):
        pid_path = tmp_dir / "daemon.pid"
        pid_path.write_text("999999999")  # Very unlikely to be alive
        result = read_daemon_status(pid_path)
        assert result.running is False
        assert result.pid == 999999999


# --- read_feed_health ---


class TestReadFeedHealth:
    def test_builtin_feeds(self):
        config = {
            "feeds": {
                "pulse": {"enabled": True, "interval_seconds": 30},
                "project": {"enabled": False, "interval_seconds": 60},
                "calendar": {"enabled": False, "interval_seconds": 300},
                "news": {"enabled": False, "interval_seconds": 1800},
                "insights": {"enabled": True, "interval_seconds": 120},
            }
        }
        cache = {
            "pulse": {"updated_at": "2020-01-01T00:00:00Z", "ttl_seconds": 30},
        }

        feeds = read_feed_health(config, cache)
        assert len(feeds) == 5  # dynamic discovery: only feeds present in config

        pulse = next(f for f in feeds if f.name == "pulse")
        assert pulse.enabled is True
        assert pulse.healthy is False  # Old timestamp

        project = next(f for f in feeds if f.name == "project")
        assert project.enabled is False
        assert project.healthy is True  # Disabled = healthy

    def test_empty_feeds_config(self):
        # No feeds in config → no feed health entries (dynamic discovery)
        feeds = read_feed_health({}, {})
        assert feeds == []

    def test_discovers_unknown_feed_dynamically(self):
        """Any key under feeds: is discovered — no hardcoded list."""
        config = {
            "feeds": {
                "brand_new_feed": {"enabled": True, "interval_seconds": 42},
            }
        }
        cache = {}
        feeds = read_feed_health(config, cache)
        assert len(feeds) == 1
        assert feeds[0].name == "brand_new_feed"
        assert feeds[0].interval_seconds == 42
        assert feeds[0].healthy is False  # Enabled but no cache entry


# --- read_insights ---


class TestReadInsights:
    def test_reads_session_meta(self, tmp_dir):
        meta_dir = tmp_dir / "session-meta"
        meta_dir.mkdir()
        facets_dir = tmp_dir / "facets"
        facets_dir.mkdir()

        session = {
            "session_id": "test-1",
            "start_time": "2026-02-20T10:00:00Z",
            "user_message_count": 5,
            "lines_added": 100,
            "duration_minutes": 30,
            "tool_counts": {"Bash": 3, "Read": 7},
            "message_hours": [10, 10, 11],
        }
        (meta_dir / "test-1.json").write_text(json.dumps(session))

        facet = {
            "session_id": "test-1",
            "friction_counts": {"tool_error": 2},
        }
        (facets_dir / "test-1.json").write_text(json.dumps(facet))

        result = read_insights(tmp_dir, staleness_days=30)
        assert result.total_sessions == 1
        assert result.total_messages == 5
        assert result.total_lines_added == 100
        assert result.friction_total == 2
        assert ("Bash", 3) in result.top_tools or ("Read", 7) in result.top_tools

    def test_empty_directory(self, tmp_dir):
        result = read_insights(tmp_dir)
        assert result.total_sessions == 0

    def test_filters_old_sessions(self, tmp_dir):
        meta_dir = tmp_dir / "session-meta"
        meta_dir.mkdir()
        facets_dir = tmp_dir / "facets"
        facets_dir.mkdir()

        old_session = {
            "session_id": "old",
            "start_time": "2020-01-01T00:00:00Z",
            "user_message_count": 10,
        }
        (meta_dir / "old.json").write_text(json.dumps(old_session))

        result = read_insights(tmp_dir, staleness_days=30)
        assert result.total_sessions == 0


# --- read_insights_summary ---


class TestReadInsightsSummary:
    def test_reads_cached_summary(self, tmp_dir):
        summary_path = tmp_dir / "summary.json"
        summary_path.write_text(json.dumps({
            "patterns": "Heavy Bash usage",
            "top_insight": "Consider batching",
            "focus_area": "Test coverage",
            "generated_at": "2026-02-23T08:00:00",
        }))
        result = read_insights_summary(summary_path)
        assert result is not None
        assert result.patterns == "Heavy Bash usage"

    def test_missing_file(self, tmp_dir):
        result = read_insights_summary(tmp_dir / "nonexistent.json")
        assert result is None


# --- read_recipes ---


class TestReadRecipes:
    def test_discovers_recipes(self, tmp_dir):
        os.environ["HOOKWISE_CONFIG"] = str(tmp_dir)
        recipes_dir = tmp_dir / "recipes" / "safety" / "test-recipe"
        recipes_dir.mkdir(parents=True)
        hooks_yaml = recipes_dir / "hooks.yaml"
        hooks_yaml.write_text(yaml.dump({
            "name": "test-recipe",
            "description": "A test recipe",
        }))

        config = {"includes": []}
        recipes = read_recipes(config)
        assert len(recipes) == 1
        assert recipes[0].name == "test-recipe"
        assert recipes[0].category == "safety"

        # Cleanup env
        del os.environ["HOOKWISE_CONFIG"]

    def test_no_recipes_dir(self, tmp_dir):
        os.environ["HOOKWISE_CONFIG"] = str(tmp_dir)
        recipes = read_recipes({})
        assert recipes == []
        del os.environ["HOOKWISE_CONFIG"]


# --- read_analytics localtime (RC-3) ---


class TestAnalyticsLocaltime:
    """Verify that read_analytics SQL uses 'localtime' modifier (RC-3)."""

    def test_sql_uses_localtime_modifier(self, tmp_dir):
        """The SQL queries in read_analytics must use 'localtime' for DATE()
        to avoid UTC date boundaries grouping events on wrong local dates."""
        import inspect

        source = inspect.getsource(read_analytics)
        # All DATE(timestamp) calls should include 'localtime'
        assert "DATE(timestamp, 'localtime')" in source
        # The WHERE clause should use localtime for the date cutoff
        assert "DATE('now', ?, 'localtime')" in source
        # GROUP BY should also use localtime
        assert "GROUP BY DATE(timestamp, 'localtime')" in source

    def test_analytics_query_does_not_use_bare_date(self, tmp_dir):
        """Ensure no bare DATE(timestamp) without localtime remains."""
        import inspect
        import re

        source = inspect.getsource(read_analytics)
        # Find DATE(timestamp) without 'localtime' — should not exist
        # Match DATE(timestamp) NOT followed by , 'localtime'
        bare_matches = re.findall(r"DATE\(timestamp\)(?!\s*#)", source)
        assert len(bare_matches) == 0, (
            f"Found {len(bare_matches)} bare DATE(timestamp) without 'localtime'"
        )
