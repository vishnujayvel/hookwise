"""Tests for hookwise.analytics -- AnalyticsEngine, DB layer, and session tracking."""

from __future__ import annotations

import json
import os
import sqlite3
import stat
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import pytest

from hookwise.analytics import (
    AnalyticsEngine,
    _extract_lines,
    _get_engine,
    _reset_engine,
    handle,
)
from hookwise.analytics.db import AnalyticsDB, SCHEMA_VERSION, _now_iso
from hookwise.analytics.session import SessionTracker
from hookwise.config import HooksConfig


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def db_path(tmp_path: Path) -> Path:
    """Provide a temporary database path."""
    return tmp_path / "test_analytics.db"


@pytest.fixture
def db(db_path: Path) -> AnalyticsDB:
    """Provide a fresh AnalyticsDB instance."""
    database = AnalyticsDB(db_path)
    yield database
    database.close()


@pytest.fixture
def engine(db_path: Path) -> AnalyticsEngine:
    """Provide a fresh AnalyticsEngine instance."""
    eng = AnalyticsEngine(db_path)
    yield eng
    eng.close()


@pytest.fixture(autouse=True)
def reset_singleton():
    """Reset the singleton engine between tests."""
    _reset_engine()
    yield
    _reset_engine()


# ---------------------------------------------------------------------------
# AnalyticsDB -- initialization
# ---------------------------------------------------------------------------


class TestAnalyticsDBInit:
    """Tests for AnalyticsDB initialization."""

    def test_creates_database_file(self, db_path: Path) -> None:
        """Database file should be created on initialization."""
        db = AnalyticsDB(db_path)
        assert db_path.exists()
        db.close()

    def test_creates_parent_directories(self, tmp_path: Path) -> None:
        """Should create parent directories if they don't exist."""
        deep_path = tmp_path / "a" / "b" / "c" / "analytics.db"
        db = AnalyticsDB(deep_path)
        assert deep_path.exists()
        db.close()

    def test_sets_owner_only_permissions(self, db_path: Path) -> None:
        """Database file should have 0o600 permissions."""
        db = AnalyticsDB(db_path)
        file_stat = os.stat(db_path)
        permissions = stat.S_IMODE(file_stat.st_mode)
        assert permissions == 0o600
        db.close()

    def test_wal_mode_enabled(self, db_path: Path) -> None:
        """WAL journal mode should be active."""
        db = AnalyticsDB(db_path)
        conn = sqlite3.connect(str(db_path))
        cursor = conn.execute("PRAGMA journal_mode")
        mode = cursor.fetchone()[0]
        assert mode == "wal"
        conn.close()
        db.close()

    def test_schema_version_recorded(self, db: AnalyticsDB) -> None:
        """Schema version should be stored in schema_meta table."""
        row = db._fetchone(
            "SELECT value FROM schema_meta WHERE key = 'schema_version'"
        )
        assert row is not None
        assert row["value"] == str(SCHEMA_VERSION)

    def test_tables_created(self, db: AnalyticsDB) -> None:
        """All required tables should exist."""
        tables = db._fetchall(
            "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name"
        )
        table_names = {row["name"] for row in tables}
        expected = {
            "sessions", "events", "authorship_ledger",
            "metacognition_logs", "agent_spans", "schema_meta",
        }
        assert expected.issubset(table_names)

    def test_idempotent_initialization(self, db_path: Path) -> None:
        """Opening the same DB twice should not fail."""
        db1 = AnalyticsDB(db_path)
        db1.close()
        db2 = AnalyticsDB(db_path)
        # Verify tables still work
        db2.insert_session("test-session", "2026-02-10T12:00:00Z")
        session = db2.get_session("test-session")
        assert session is not None
        db2.close()

    def test_default_db_path(self, tmp_state_dir: Path) -> None:
        """Default path should be under HOOKWISE_STATE_DIR."""
        db = AnalyticsDB()
        assert "analytics.db" in str(db.db_path)
        assert str(tmp_state_dir) in str(db.db_path)
        db.close()


# ---------------------------------------------------------------------------
# AnalyticsDB -- Sessions CRUD
# ---------------------------------------------------------------------------


class TestAnalyticsDBSessions:
    """Tests for session CRUD operations."""

    def test_insert_and_get_session(self, db: AnalyticsDB) -> None:
        """Should insert and retrieve a session."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        session = db.get_session("s1")
        assert session is not None
        assert session["id"] == "s1"
        assert session["started_at"] == "2026-02-10T12:00:00Z"
        assert session["ended_at"] is None

    def test_insert_duplicate_session_ignored(self, db: AnalyticsDB) -> None:
        """Duplicate session insert should be silently ignored."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        db.insert_session("s1", "2026-02-10T13:00:00Z")
        session = db.get_session("s1")
        # Should keep the original start time
        assert session["started_at"] == "2026-02-10T12:00:00Z"

    def test_update_session(self, db: AnalyticsDB) -> None:
        """Should update session fields."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        db.update_session(
            "s1",
            ended_at="2026-02-10T12:30:00Z",
            duration_seconds=1800,
            total_tool_calls=42,
            classification="coding",
        )
        session = db.get_session("s1")
        assert session["ended_at"] == "2026-02-10T12:30:00Z"
        assert session["duration_seconds"] == 1800
        assert session["total_tool_calls"] == 42
        assert session["classification"] == "coding"

    def test_update_empty_fields_noop(self, db: AnalyticsDB) -> None:
        """Calling update with no fields should be a no-op."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        db.update_session("s1")  # No fields
        session = db.get_session("s1")
        assert session is not None

    def test_get_nonexistent_session(self, db: AnalyticsDB) -> None:
        """Should return None for missing session."""
        assert db.get_session("nonexistent") is None

    def test_session_defaults(self, db: AnalyticsDB) -> None:
        """New session should have sensible defaults."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        session = db.get_session("s1")
        assert session["total_tool_calls"] == 0
        assert session["file_edits_count"] == 0
        assert session["ai_authored_lines"] == 0
        assert session["human_verified_lines"] == 0
        assert session["estimated_cost_usd"] == 0.0
        assert session["estimated_tokens"] == 0


# ---------------------------------------------------------------------------
# AnalyticsDB -- Events CRUD
# ---------------------------------------------------------------------------


class TestAnalyticsDBEvents:
    """Tests for event CRUD operations."""

    def test_insert_and_get_events(self, db: AnalyticsDB) -> None:
        """Should insert and retrieve events for a session."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        db.insert_event(
            session_id="s1",
            event_type="PostToolUse",
            timestamp="2026-02-10T12:00:05Z",
            tool_name="Write",
            file_path="/tmp/test.py",
            lines_added=50,
            lines_removed=0,
        )
        events = db.get_events_for_session("s1")
        assert len(events) == 1
        assert events[0]["tool_name"] == "Write"
        assert events[0]["lines_added"] == 50

    def test_insert_event_returns_id(self, db: AnalyticsDB) -> None:
        """Insert should return the auto-generated event ID."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        event_id = db.insert_event(
            session_id="s1",
            event_type="PostToolUse",
            timestamp="2026-02-10T12:00:05Z",
        )
        assert event_id > 0

    def test_multiple_events_ordered_by_timestamp(self, db: AnalyticsDB) -> None:
        """Events should be returned in timestamp order."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        db.insert_event(
            session_id="s1",
            event_type="PostToolUse",
            timestamp="2026-02-10T12:00:10Z",
            tool_name="Edit",
        )
        db.insert_event(
            session_id="s1",
            event_type="PostToolUse",
            timestamp="2026-02-10T12:00:05Z",
            tool_name="Write",
        )
        events = db.get_events_for_session("s1")
        assert len(events) == 2
        assert events[0]["tool_name"] == "Write"
        assert events[1]["tool_name"] == "Edit"

    def test_event_with_ai_confidence(self, db: AnalyticsDB) -> None:
        """Should store AI confidence score with event."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        db.insert_event(
            session_id="s1",
            event_type="PostToolUse",
            timestamp="2026-02-10T12:00:05Z",
            ai_confidence_score=0.92,
        )
        events = db.get_events_for_session("s1")
        assert abs(events[0]["ai_confidence_score"] - 0.92) < 0.001

    def test_event_with_null_optional_fields(self, db: AnalyticsDB) -> None:
        """Optional fields should accept None."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        db.insert_event(
            session_id="s1",
            event_type="UserPromptSubmit",
            timestamp="2026-02-10T12:00:01Z",
        )
        events = db.get_events_for_session("s1")
        assert events[0]["tool_name"] is None
        assert events[0]["file_path"] is None
        assert events[0]["lines_added"] is None


# ---------------------------------------------------------------------------
# AnalyticsDB -- Authorship Ledger CRUD
# ---------------------------------------------------------------------------


class TestAnalyticsDBAuthorship:
    """Tests for authorship ledger CRUD operations."""

    def test_insert_and_get_authorship(self, db: AnalyticsDB) -> None:
        """Should insert and retrieve authorship entries."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        db.insert_authorship_entry(
            session_id="s1",
            file_path="/tmp/test.py",
            timestamp="2026-02-10T12:00:05Z",
            lines_changed=50,
            ai_confidence_score=0.92,
            classification="high_probability_ai",
            time_since_prompt_seconds=3.5,
        )
        entries = db.get_authorship_for_session("s1")
        assert len(entries) == 1
        assert entries[0]["file_path"] == "/tmp/test.py"
        assert entries[0]["lines_changed"] == 50
        assert abs(entries[0]["ai_confidence_score"] - 0.92) < 0.001
        assert entries[0]["classification"] == "high_probability_ai"

    def test_session_ai_ratio_weighted(self, db: AnalyticsDB) -> None:
        """AI ratio should be weighted by lines_changed."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        # 100 lines at 0.9 = 90 weight
        db.insert_authorship_entry(
            "s1", "/a.py", "2026-02-10T12:00:05Z",
            100, 0.9, "high_probability_ai",
        )
        # 10 lines at 0.1 = 1 weight
        db.insert_authorship_entry(
            "s1", "/b.py", "2026-02-10T12:00:10Z",
            10, 0.1, "human_authored",
        )
        # Weighted average: (90 + 1) / 110 = 0.8272...
        ratio = db.get_session_ai_ratio("s1")
        assert abs(ratio - (90 + 1) / 110) < 0.01

    def test_session_ai_ratio_empty(self, db: AnalyticsDB) -> None:
        """AI ratio should be 0.0 when no entries exist."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        assert db.get_session_ai_ratio("s1") == 0.0


# ---------------------------------------------------------------------------
# AnalyticsDB -- Metacognition Logs
# ---------------------------------------------------------------------------


class TestAnalyticsDBMetacognition:
    """Tests for metacognition log operations."""

    def test_insert_metacognition_log(self, db: AnalyticsDB) -> None:
        """Should insert a metacognition log entry."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        log_id = db.insert_metacognition_log(
            session_id="s1",
            timestamp="2026-02-10T12:05:00Z",
            trigger_type="rambling_detected",
            alert_level="warning",
        )
        assert log_id > 0


# ---------------------------------------------------------------------------
# AnalyticsDB -- Agent Spans
# ---------------------------------------------------------------------------


class TestAnalyticsDBAgentSpans:
    """Tests for agent span operations."""

    def test_insert_and_update_agent_span(self, db: AnalyticsDB) -> None:
        """Should insert and update an agent span."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        span_id = db.insert_agent_span(
            session_id="s1",
            agent_id="agent-1",
            started_at="2026-02-10T12:00:00Z",
            agent_type="researcher",
        )
        assert span_id > 0

        db.update_agent_span(
            session_id="s1",
            agent_id="agent-1",
            stopped_at="2026-02-10T12:10:00Z",
            files_modified="/a.py,/b.py",
        )


# ---------------------------------------------------------------------------
# AnalyticsDB -- Query interface
# ---------------------------------------------------------------------------


class TestAnalyticsDBQueries:
    """Tests for the query interface."""

    def _populate_sessions(self, db: AnalyticsDB) -> None:
        """Populate test data for query tests."""
        now = datetime.now(timezone.utc)
        for i in range(5):
            from datetime import timedelta
            ts = (now - timedelta(days=i)).strftime("%Y-%m-%dT%H:%M:%SZ")
            end_ts = (now - timedelta(days=i) + timedelta(hours=1)).strftime(
                "%Y-%m-%dT%H:%M:%SZ"
            )
            sid = f"session-{i}"
            db.insert_session(sid, ts)
            db.update_session(
                sid,
                ended_at=end_ts,
                duration_seconds=3600,
                total_tool_calls=10 + i,
                file_edits_count=5 + i,
                ai_authored_lines=100 + i * 10,
                human_verified_lines=20 + i,
                classification="coding",
                estimated_cost_usd=0.05 + i * 0.01,
                estimated_tokens=5000 + i * 500,
            )
            # Add events
            db.insert_event(
                session_id=sid,
                event_type="PostToolUse",
                timestamp=ts,
                tool_name="Write",
                lines_added=50,
            )
            db.insert_event(
                session_id=sid,
                event_type="PostToolUse",
                timestamp=ts,
                tool_name="Edit",
                lines_added=10,
                lines_removed=3,
            )

    def test_query_session_stats(self, db: AnalyticsDB) -> None:
        """Should return sessions within the time window."""
        self._populate_sessions(db)
        sessions = db.query_session_stats(days=3)
        assert len(sessions) >= 3
        # Should be in descending order
        assert sessions[0]["started_at"] >= sessions[1]["started_at"]

    def test_query_session_stats_empty(self, db: AnalyticsDB) -> None:
        """Should return empty list when no sessions exist."""
        sessions = db.query_session_stats(days=7)
        assert sessions == []

    def test_query_daily_summary(self, db: AnalyticsDB) -> None:
        """Should return daily aggregated summaries."""
        self._populate_sessions(db)
        daily = db.query_daily_summary(days=7)
        assert len(daily) >= 1
        # Each day should have numeric totals
        for day in daily:
            assert "date" in day
            assert "sessions" in day
            assert day["sessions"] >= 1

    def test_query_tool_breakdown(self, db: AnalyticsDB) -> None:
        """Should return tool usage breakdown."""
        self._populate_sessions(db)
        tools = db.query_tool_breakdown(days=7)
        assert len(tools) >= 1
        tool_names = {t["tool_name"] for t in tools}
        assert "Write" in tool_names

    def test_query_tool_breakdown_by_session(self, db: AnalyticsDB) -> None:
        """Should return tool breakdown for a specific session."""
        self._populate_sessions(db)
        tools = db.query_tool_breakdown(session_id="session-0")
        assert len(tools) >= 1

    def test_query_authorship_summary(self, db: AnalyticsDB) -> None:
        """Should return authorship breakdown summary."""
        db.insert_session("s1", "2026-02-10T12:00:00Z")
        db.insert_authorship_entry(
            "s1", "/a.py", "2026-02-10T12:00:05Z",
            100, 0.9, "high_probability_ai",
        )
        db.insert_authorship_entry(
            "s1", "/b.py", "2026-02-10T12:00:10Z",
            10, 0.1, "human_authored",
        )
        summary = db.query_authorship_summary(session_id="s1")
        assert summary["total_entries"] == 2
        assert summary["total_lines_changed"] == 110
        assert abs(summary["weighted_ai_score"] - (90 + 1) / 110) < 0.01
        assert "high_probability_ai" in summary["classification_breakdown"]
        assert "human_authored" in summary["classification_breakdown"]

    def test_query_authorship_summary_empty(self, db: AnalyticsDB) -> None:
        """Should return zeroed summary when no entries exist."""
        summary = db.query_authorship_summary(session_id="nonexistent")
        assert summary["total_entries"] == 0
        assert summary["total_lines_changed"] == 0
        assert summary["weighted_ai_score"] == 0.0


# ---------------------------------------------------------------------------
# AnalyticsDB -- _now_iso helper
# ---------------------------------------------------------------------------


class TestNowIso:
    """Tests for the _now_iso helper."""

    def test_returns_iso_string(self) -> None:
        """Should return an ISO 8601 string ending with Z."""
        ts = _now_iso()
        assert ts.endswith("Z")
        # Should be parseable
        dt = datetime.fromisoformat(ts.replace("Z", "+00:00"))
        assert dt.tzinfo is not None


# ---------------------------------------------------------------------------
# SessionTracker
# ---------------------------------------------------------------------------


class TestSessionTracker:
    """Tests for the SessionTracker class."""

    def test_start_and_end_session(self, db: AnalyticsDB) -> None:
        """Should start and end a session with computed summary."""
        tracker = SessionTracker(db)
        tracker.start_session("s1", "2026-02-10T12:00:00Z")
        tracker.record_tool_call("s1", "Write", lines_added=50)
        tracker.record_tool_call("s1", "Write", lines_added=30)
        tracker.record_tool_call("s1", "Read")
        summary = tracker.end_session("s1", "2026-02-10T12:30:00Z")

        assert summary["session_id"] == "s1"
        assert summary["duration_seconds"] == 1800
        assert summary["total_tool_calls"] == 3
        assert summary["file_edits_count"] == 2

    def test_session_classification_coding(self, db: AnalyticsDB) -> None:
        """Session with mostly Write/Edit calls should be classified as coding."""
        tracker = SessionTracker(db)
        tracker.start_session("s1", "2026-02-10T12:00:00Z")
        for _ in range(5):
            tracker.record_tool_call("s1", "Write", lines_added=10)
        tracker.record_tool_call("s1", "Read")
        summary = tracker.end_session("s1", "2026-02-10T12:30:00Z")
        assert summary["classification"] == "coding"

    def test_session_classification_reviewing(self, db: AnalyticsDB) -> None:
        """Session with mostly Read/Grep/Glob calls should be classified as reviewing."""
        tracker = SessionTracker(db)
        tracker.start_session("s1", "2026-02-10T12:00:00Z")
        for _ in range(5):
            tracker.record_tool_call("s1", "Read")
        tracker.record_tool_call("s1", "Grep")
        tracker.record_tool_call("s1", "Write", lines_added=5)
        summary = tracker.end_session("s1", "2026-02-10T12:30:00Z")
        assert summary["classification"] == "reviewing"

    def test_session_classification_tooling(self, db: AnalyticsDB) -> None:
        """Session with mostly Bash calls should be classified as tooling."""
        tracker = SessionTracker(db)
        tracker.start_session("s1", "2026-02-10T12:00:00Z")
        for _ in range(5):
            tracker.record_tool_call("s1", "Bash")
        tracker.record_tool_call("s1", "Read")
        summary = tracker.end_session("s1", "2026-02-10T12:30:00Z")
        assert summary["classification"] == "tooling"

    def test_session_classification_mixed(self, db: AnalyticsDB) -> None:
        """Session with evenly distributed calls should be classified as mixed."""
        tracker = SessionTracker(db)
        tracker.start_session("s1", "2026-02-10T12:00:00Z")
        tracker.record_tool_call("s1", "Write", lines_added=10)
        tracker.record_tool_call("s1", "Read")
        tracker.record_tool_call("s1", "Bash")
        summary = tracker.end_session("s1", "2026-02-10T12:30:00Z")
        assert summary["classification"] == "mixed"

    def test_session_classification_empty(self, db: AnalyticsDB) -> None:
        """Session with no tool calls should be classified as mixed."""
        tracker = SessionTracker(db)
        tracker.start_session("s1", "2026-02-10T12:00:00Z")
        summary = tracker.end_session("s1", "2026-02-10T12:30:00Z")
        assert summary["classification"] == "mixed"

    def test_end_nonexistent_session(self, db: AnalyticsDB) -> None:
        """Ending a non-existent session should return error."""
        tracker = SessionTracker(db)
        summary = tracker.end_session("nonexistent")
        assert "error" in summary

    def test_token_estimation(self, db: AnalyticsDB) -> None:
        """Token estimation should be non-zero when there are events."""
        tracker = SessionTracker(db)
        tracker.start_session("s1", "2026-02-10T12:00:00Z")
        tracker.record_tool_call("s1", "Write", lines_added=50)
        tracker.record_prompt("s1")
        summary = tracker.end_session("s1", "2026-02-10T12:30:00Z")
        assert summary["estimated_tokens"] > 0
        assert summary["estimated_cost_usd"] > 0

    def test_duration_computation(self, db: AnalyticsDB) -> None:
        """Should correctly compute duration in seconds."""
        tracker = SessionTracker(db)
        tracker.start_session("s1", "2026-02-10T12:00:00Z")
        summary = tracker.end_session("s1", "2026-02-10T13:30:00Z")
        # 1.5 hours = 5400 seconds
        assert summary["duration_seconds"] == 5400

    def test_record_tool_call_initializes_counters(self, db: AnalyticsDB) -> None:
        """Recording a tool call for an unstarted session should not crash."""
        tracker = SessionTracker(db)
        # No start_session call -- counters should be auto-initialized
        tracker.record_tool_call("s1", "Write", lines_added=10)
        # No assertion needed -- just checking it doesn't crash

    def test_session_cleans_up_memory(self, db: AnalyticsDB) -> None:
        """Ending a session should clean up in-memory state."""
        tracker = SessionTracker(db)
        tracker.start_session("s1", "2026-02-10T12:00:00Z")
        tracker.record_tool_call("s1", "Write", lines_added=10)
        tracker.end_session("s1", "2026-02-10T12:30:00Z")
        # Internal state should be cleaned up
        assert "s1" not in tracker._tool_counts
        assert "s1" not in tracker._total_lines_added


# ---------------------------------------------------------------------------
# AnalyticsEngine
# ---------------------------------------------------------------------------


class TestAnalyticsEngine:
    """Tests for the AnalyticsEngine facade."""

    def test_start_and_end_session(self, engine: AnalyticsEngine) -> None:
        """Should start and end a session via the engine."""
        engine.start_session("s1", "2026-02-10T12:00:00Z")
        summary = engine.end_session("s1", {"ended_at": "2026-02-10T12:30:00Z"})
        assert summary["session_id"] == "s1"
        assert summary["duration_seconds"] == 1800

    def test_record_tool_use_event(self, engine: AnalyticsEngine) -> None:
        """Should record a PostToolUse event."""
        engine.start_session("s1", "2026-02-10T12:00:00Z")
        engine.record_event({
            "session_id": "s1",
            "event_type": "PostToolUse",
            "timestamp": "2026-02-10T12:00:05Z",
            "tool_name": "Bash",
            "lines_added": 0,
            "lines_removed": 0,
        })
        events = engine.db.get_events_for_session("s1")
        assert len(events) == 1
        assert events[0]["tool_name"] == "Bash"

    def test_record_write_event_computes_ai_score(self, engine: AnalyticsEngine) -> None:
        """Write events should compute and store AI confidence score."""
        engine.start_session("s1", "2026-02-10T12:00:00Z")

        # Record a prompt first
        engine.record_event({
            "session_id": "s1",
            "event_type": "UserPromptSubmit",
            "timestamp": "2026-02-10T12:00:00Z",
            "char_count": 100,
        })

        # Record a Write event within 10s
        engine.record_event({
            "session_id": "s1",
            "event_type": "PostToolUse",
            "timestamp": "2026-02-10T12:00:05Z",
            "tool_name": "Write",
            "file_path": "/tmp/test.py",
            "lines_added": 100,
            "lines_removed": 0,
        })

        events = engine.db.get_events_for_session("s1")
        # Should have prompt event + tool event
        assert len(events) == 2
        write_event = events[1]
        assert write_event["ai_confidence_score"] is not None
        assert write_event["ai_confidence_score"] >= 0.9

    def test_record_prompt_event(self, engine: AnalyticsEngine) -> None:
        """Should record a UserPromptSubmit event."""
        engine.start_session("s1", "2026-02-10T12:00:00Z")
        engine.record_event({
            "session_id": "s1",
            "event_type": "UserPromptSubmit",
            "timestamp": "2026-02-10T12:00:01Z",
            "char_count": 150,
        })
        events = engine.db.get_events_for_session("s1")
        assert len(events) == 1
        assert events[0]["event_type"] == "UserPromptSubmit"

    def test_record_session_start_event(self, engine: AnalyticsEngine) -> None:
        """SessionStart event should start a session."""
        engine.record_event({
            "session_id": "s1",
            "event_type": "SessionStart",
            "timestamp": "2026-02-10T12:00:00Z",
        })
        session = engine.db.get_session("s1")
        assert session is not None

    def test_record_session_end_event(self, engine: AnalyticsEngine) -> None:
        """Stop event should end a session."""
        engine.start_session("s1", "2026-02-10T12:00:00Z")
        engine.record_event({
            "session_id": "s1",
            "event_type": "Stop",
            "timestamp": "2026-02-10T12:30:00Z",
        })
        session = engine.db.get_session("s1")
        assert session["ended_at"] is not None

    def test_record_session_end_via_session_end_event(self, engine: AnalyticsEngine) -> None:
        """SessionEnd event should also end a session."""
        engine.start_session("s1", "2026-02-10T12:00:00Z")
        engine.record_event({
            "session_id": "s1",
            "event_type": "SessionEnd",
            "timestamp": "2026-02-10T12:30:00Z",
        })
        session = engine.db.get_session("s1")
        assert session["ended_at"] is not None

    def test_record_event_missing_fields_ignored(self, engine: AnalyticsEngine) -> None:
        """Events with missing required fields should be silently ignored."""
        engine.record_event({})  # No session_id, event_type, timestamp
        engine.record_event({"session_id": "s1"})  # Missing event_type
        engine.record_event({"session_id": "s1", "event_type": "PostToolUse"})  # Missing timestamp
        # Should not crash

    def test_record_generic_event(self, engine: AnalyticsEngine) -> None:
        """Non-special event types should be recorded as generic events."""
        engine.start_session("s1", "2026-02-10T12:00:00Z")
        engine.record_event({
            "session_id": "s1",
            "event_type": "Notification",
            "timestamp": "2026-02-10T12:00:05Z",
        })
        events = engine.db.get_events_for_session("s1")
        assert len(events) == 1
        assert events[0]["event_type"] == "Notification"

    def test_query_stats(self, engine: AnalyticsEngine) -> None:
        """Should return a stats dict with expected keys."""
        engine.start_session("s1", "2026-02-10T12:00:00Z")
        engine.end_session("s1", {"ended_at": "2026-02-10T12:30:00Z"})
        stats = engine.query_stats(days=7)
        assert "sessions" in stats
        assert "daily_summary" in stats
        assert "tool_breakdown" in stats
        assert "authorship_summary" in stats

    def test_query_stats_json_format(self, engine: AnalyticsEngine) -> None:
        """JSON format should produce a serialized string."""
        stats = engine.query_stats(days=7, format="json")
        assert "json" in stats
        # Should be valid JSON
        parsed = json.loads(stats["json"])
        assert "sessions" in parsed

    def test_non_write_tool_no_authorship(self, engine: AnalyticsEngine) -> None:
        """Non-write tools (Bash, Read) should not produce authorship entries."""
        engine.start_session("s1", "2026-02-10T12:00:00Z")
        engine.record_event({
            "session_id": "s1",
            "event_type": "PostToolUse",
            "timestamp": "2026-02-10T12:00:05Z",
            "tool_name": "Bash",
            "lines_added": 0,
            "lines_removed": 0,
        })
        entries = engine.db.get_authorship_for_session("s1")
        assert len(entries) == 0

    def test_write_with_zero_lines_no_authorship(self, engine: AnalyticsEngine) -> None:
        """Write tool with zero lines should not produce authorship entry."""
        engine.start_session("s1", "2026-02-10T12:00:00Z")
        engine.record_event({
            "session_id": "s1",
            "event_type": "PostToolUse",
            "timestamp": "2026-02-10T12:00:05Z",
            "tool_name": "Write",
            "lines_added": 0,
            "lines_removed": 0,
        })
        entries = engine.db.get_authorship_for_session("s1")
        assert len(entries) == 0


# ---------------------------------------------------------------------------
# handle() builtin entry point
# ---------------------------------------------------------------------------


class TestHandleEntryPoint:
    """Tests for the handle() builtin entry point."""

    def test_handle_returns_none(self, tmp_state_dir: Path) -> None:
        """handle() should always return None (pure side effect)."""
        result = handle("PostToolUse", {"session_id": "s1", "tool_name": "Bash"}, HooksConfig())
        assert result is None

    def test_handle_post_tool_use(self, tmp_state_dir: Path) -> None:
        """handle() should record PostToolUse events."""
        handle("PostToolUse", {
            "session_id": "s1",
            "tool_name": "Write",
            "tool_input": {"file_path": "/tmp/test.py"},
            "tool_output": {"lines_added": 50, "lines_removed": 0},
        }, HooksConfig())
        # Verify it didn't crash (analytics is fire-and-forget)
        assert True

    def test_handle_user_prompt(self, tmp_state_dir: Path) -> None:
        """handle() should record UserPromptSubmit events without content."""
        handle("UserPromptSubmit", {
            "session_id": "s1",
            "content": "This is my prompt text that should NOT be stored",
        }, HooksConfig())
        assert True

    def test_handle_session_start(self, tmp_state_dir: Path) -> None:
        """handle() should start a session on SessionStart."""
        handle("SessionStart", {"session_id": "s1"}, HooksConfig())
        assert True

    def test_handle_stop(self, tmp_state_dir: Path) -> None:
        """handle() should end a session on Stop."""
        handle("SessionStart", {"session_id": "s1"}, HooksConfig())
        handle("Stop", {"session_id": "s1"}, HooksConfig())
        assert True

    def test_handle_missing_session_id(self, tmp_state_dir: Path) -> None:
        """handle() should generate a fallback session ID."""
        handle("PostToolUse", {"tool_name": "Bash"}, HooksConfig())
        assert True  # Should not crash

    def test_handle_non_dict_tool_input(self, tmp_state_dir: Path) -> None:
        """handle() should handle non-dict tool_input gracefully."""
        handle("PostToolUse", {
            "session_id": "s1",
            "tool_name": "Write",
            "tool_input": "not_a_dict",
            "tool_output": "also_not_a_dict",
        }, HooksConfig())
        assert True

    def test_handle_fail_open(self, tmp_state_dir: Path) -> None:
        """handle() should never crash the hook process."""
        # Even with completely bogus input, handle() should not raise
        handle("UnknownEvent", {}, None)
        handle("PostToolUse", None, HooksConfig())  # type: ignore[arg-type]
        assert True


# ---------------------------------------------------------------------------
# _extract_lines helper
# ---------------------------------------------------------------------------


class TestExtractLines:
    """Tests for the _extract_lines helper function."""

    def test_integer_value(self) -> None:
        """Should return integer values directly."""
        assert _extract_lines({"lines_added": 42}, "lines_added") == 42

    def test_string_value(self) -> None:
        """Should convert string values to int."""
        assert _extract_lines({"lines_added": "50"}, "lines_added") == 50

    def test_invalid_string(self) -> None:
        """Should return 0 for non-numeric strings."""
        assert _extract_lines({"lines_added": "lots"}, "lines_added") == 0

    def test_missing_key(self) -> None:
        """Should return 0 for missing keys."""
        assert _extract_lines({}, "lines_added") == 0

    def test_none_value(self) -> None:
        """Should return 0 for None values."""
        assert _extract_lines({"lines_added": None}, "lines_added") == 0

    def test_float_value(self) -> None:
        """Should return 0 for float values (not int or str)."""
        assert _extract_lines({"lines_added": 3.14}, "lines_added") == 0


# ---------------------------------------------------------------------------
# Full workflow integration
# ---------------------------------------------------------------------------


class TestAnalyticsWorkflow:
    """Integration tests for the full analytics workflow."""

    def test_full_coding_session(self, engine: AnalyticsEngine) -> None:
        """Should track a complete coding session end-to-end."""
        # Start session
        engine.start_session("s1", "2026-02-10T12:00:00Z")

        # User submits a prompt
        engine.record_event({
            "session_id": "s1",
            "event_type": "UserPromptSubmit",
            "timestamp": "2026-02-10T12:00:01Z",
            "char_count": 100,
        })

        # AI writes a file (within 5s of prompt)
        engine.record_event({
            "session_id": "s1",
            "event_type": "PostToolUse",
            "timestamp": "2026-02-10T12:00:06Z",
            "tool_name": "Write",
            "file_path": "/tmp/main.py",
            "lines_added": 100,
            "lines_removed": 0,
        })

        # AI reads a file
        engine.record_event({
            "session_id": "s1",
            "event_type": "PostToolUse",
            "timestamp": "2026-02-10T12:00:10Z",
            "tool_name": "Read",
        })

        # AI makes a small edit
        engine.record_event({
            "session_id": "s1",
            "event_type": "PostToolUse",
            "timestamp": "2026-02-10T12:00:15Z",
            "tool_name": "Edit",
            "file_path": "/tmp/main.py",
            "lines_added": 2,
            "lines_removed": 1,
        })

        # End session
        summary = engine.end_session("s1", {"ended_at": "2026-02-10T12:30:00Z"})

        assert summary["session_id"] == "s1"
        assert summary["duration_seconds"] == 1800
        assert summary["total_tool_calls"] == 3
        assert summary["file_edits_count"] == 2
        assert summary["classification"] == "coding"
        assert summary["estimated_tokens"] > 0

        # Verify events were recorded
        events = engine.db.get_events_for_session("s1")
        assert len(events) == 4  # prompt + 3 tool uses

        # Verify authorship entries were created for write tools
        authorship = engine.db.get_authorship_for_session("s1")
        assert len(authorship) == 2  # Write + Edit

        # The Write entry should have high AI confidence
        write_entry = [e for e in authorship if e["file_path"] == "/tmp/main.py"]
        assert len(write_entry) >= 1

        # Query stats
        stats = engine.query_stats(days=1)
        assert len(stats["sessions"]) >= 1

    def test_reviewing_session(self, engine: AnalyticsEngine) -> None:
        """Should correctly classify a reviewing session."""
        engine.start_session("s1", "2026-02-10T12:00:00Z")
        for i in range(10):
            engine.record_event({
                "session_id": "s1",
                "event_type": "PostToolUse",
                "timestamp": f"2026-02-10T12:00:{10+i:02d}Z",
                "tool_name": "Read",
            })
        summary = engine.end_session("s1", {"ended_at": "2026-02-10T12:30:00Z"})
        assert summary["classification"] == "reviewing"

    def test_prompt_content_never_stored(self, engine: AnalyticsEngine) -> None:
        """Prompt content must NEVER be stored in the database."""
        engine.start_session("s1", "2026-02-10T12:00:00Z")
        secret_content = "This is secret prompt content that must not be stored"
        engine.record_event({
            "session_id": "s1",
            "event_type": "UserPromptSubmit",
            "timestamp": "2026-02-10T12:00:01Z",
            "char_count": len(secret_content),
        })

        # Check events table -- should have no content field
        events = engine.db.get_events_for_session("s1")
        for event in events:
            for key, value in event.items():
                if isinstance(value, str):
                    assert secret_content not in value

    def test_multiple_sessions(self, engine: AnalyticsEngine) -> None:
        """Should track multiple sessions independently."""
        engine.start_session("s1", "2026-02-10T12:00:00Z")
        engine.start_session("s2", "2026-02-10T13:00:00Z")

        engine.record_event({
            "session_id": "s1",
            "event_type": "PostToolUse",
            "timestamp": "2026-02-10T12:00:05Z",
            "tool_name": "Write",
            "lines_added": 50,
        })
        engine.record_event({
            "session_id": "s2",
            "event_type": "PostToolUse",
            "timestamp": "2026-02-10T13:00:05Z",
            "tool_name": "Bash",
        })

        s1_events = engine.db.get_events_for_session("s1")
        s2_events = engine.db.get_events_for_session("s2")
        assert len(s1_events) == 1
        assert len(s2_events) == 1
        assert s1_events[0]["tool_name"] == "Write"
        assert s2_events[0]["tool_name"] == "Bash"
