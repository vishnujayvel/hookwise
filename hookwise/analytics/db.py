"""SQLite database layer for hookwise analytics.

Provides the AnalyticsDB class that manages all persistent storage for
analytics data: sessions, events, authorship ledger, metacognition logs,
and agent spans.

Key design decisions:
- WAL mode for concurrent read access (hooks may fire while queries run)
- Owner-only file permissions (0o600) for privacy
- Automatic schema creation on first use (no migration step)
- All writes are synchronous but fast (local SQLite)
- Fail-open: database errors are logged but never crash the hook process
"""

from __future__ import annotations

import logging
import os
import sqlite3
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from hookwise.state import get_state_dir

logger = logging.getLogger("hookwise")

# Default database filename within the state directory
DB_FILENAME = "analytics.db"

# Schema version -- bump this when the schema changes
SCHEMA_VERSION = 1

# SQL statements for table creation
_SCHEMA_SQL = """
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    started_at TEXT NOT NULL,
    ended_at TEXT,
    duration_seconds INTEGER,
    total_tool_calls INTEGER DEFAULT 0,
    file_edits_count INTEGER DEFAULT 0,
    ai_authored_lines INTEGER DEFAULT 0,
    human_verified_lines INTEGER DEFAULT 0,
    classification TEXT,
    estimated_cost_usd REAL DEFAULT 0.0,
    estimated_tokens INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    tool_name TEXT,
    timestamp TEXT NOT NULL,
    file_path TEXT,
    lines_added INTEGER,
    lines_removed INTEGER,
    ai_confidence_score REAL,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);
CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);

CREATE TABLE IF NOT EXISTS authorship_ledger (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    file_path TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    lines_changed INTEGER NOT NULL,
    ai_confidence_score REAL NOT NULL,
    classification TEXT NOT NULL,
    time_since_prompt_seconds REAL,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);
CREATE INDEX IF NOT EXISTS idx_authorship_session ON authorship_ledger(session_id);

CREATE TABLE IF NOT EXISTS metacognition_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    trigger_type TEXT NOT NULL,
    prompt_id TEXT,
    prompt_text TEXT,
    alert_level TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE TABLE IF NOT EXISTS agent_spans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    parent_agent_id TEXT,
    agent_type TEXT,
    started_at TEXT NOT NULL,
    stopped_at TEXT,
    files_modified TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);
CREATE INDEX IF NOT EXISTS idx_agents_session ON agent_spans(session_id);

CREATE TABLE IF NOT EXISTS schema_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
"""


def _now_iso() -> str:
    """Return the current UTC time as an ISO 8601 string with Z suffix."""
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


class AnalyticsDB:
    """SQLite database access layer for hookwise analytics.

    Manages the database lifecycle: creation, schema initialization,
    WAL mode configuration, and file permissions. Provides CRUD
    methods for all analytics tables.

    Usage::

        db = AnalyticsDB()  # Uses default path ~/.hookwise/analytics.db
        db.insert_session("session-123", "2026-02-10T12:00:00Z")
        db.insert_event(session_id="session-123", event_type="PostToolUse", ...)
        stats = db.query_session_stats(days=7)
        db.close()

    All methods are designed for synchronous, single-threaded use
    within a hook handler. WAL mode allows concurrent reads from
    other processes (e.g., the stats CLI command).
    """

    def __init__(self, db_path: Path | None = None) -> None:
        """Initialize the database connection.

        Creates the database file and schema if they don't exist.
        Configures WAL mode and sets file permissions to 0o600.

        Args:
            db_path: Path to the SQLite database file. If None, uses
                ~/.hookwise/analytics.db (respects HOOKWISE_STATE_DIR).
        """
        if db_path is None:
            state_dir = get_state_dir()
            state_dir.mkdir(parents=True, exist_ok=True)
            db_path = state_dir / DB_FILENAME

        self._db_path = db_path
        self._conn: sqlite3.Connection | None = None
        self._initialize()

    @property
    def db_path(self) -> Path:
        """Return the database file path."""
        return self._db_path

    def _initialize(self) -> None:
        """Create the database, apply schema, configure WAL and permissions."""
        # Ensure parent directory exists
        self._db_path.parent.mkdir(parents=True, exist_ok=True)

        self._conn = sqlite3.connect(
            str(self._db_path),
            timeout=5.0,
            isolation_level=None,  # Autocommit mode for WAL
        )
        self._conn.row_factory = sqlite3.Row

        # Enable WAL mode for concurrent read access
        self._conn.execute("PRAGMA journal_mode=WAL")

        # Foreign key enforcement
        self._conn.execute("PRAGMA foreign_keys=ON")

        # Apply schema
        self._conn.executescript(_SCHEMA_SQL)

        # Record schema version
        self._conn.execute(
            "INSERT OR REPLACE INTO schema_meta (key, value) VALUES (?, ?)",
            ("schema_version", str(SCHEMA_VERSION)),
        )

        # Set file permissions to owner-only (0o600)
        try:
            os.chmod(self._db_path, 0o600)
        except OSError:
            logger.debug("Could not set permissions on %s", self._db_path)

    def close(self) -> None:
        """Close the database connection."""
        if self._conn is not None:
            self._conn.close()
            self._conn = None

    def _execute(
        self,
        sql: str,
        params: tuple[Any, ...] = (),
    ) -> sqlite3.Cursor:
        """Execute a SQL statement with error logging.

        Args:
            sql: SQL statement to execute.
            params: Bind parameters.

        Returns:
            The cursor from the execution.

        Raises:
            sqlite3.Error: Re-raised after logging.
        """
        if self._conn is None:
            raise RuntimeError("Database connection is closed")
        return self._conn.execute(sql, params)

    def _fetchall(
        self,
        sql: str,
        params: tuple[Any, ...] = (),
    ) -> list[sqlite3.Row]:
        """Execute a query and return all rows.

        Args:
            sql: SQL SELECT statement.
            params: Bind parameters.

        Returns:
            List of Row objects.
        """
        cursor = self._execute(sql, params)
        return cursor.fetchall()

    def _fetchone(
        self,
        sql: str,
        params: tuple[Any, ...] = (),
    ) -> sqlite3.Row | None:
        """Execute a query and return the first row.

        Args:
            sql: SQL SELECT statement.
            params: Bind parameters.

        Returns:
            Single Row object, or None if no results.
        """
        cursor = self._execute(sql, params)
        return cursor.fetchone()

    # ------------------------------------------------------------------
    # Sessions CRUD
    # ------------------------------------------------------------------

    def insert_session(self, session_id: str, started_at: str) -> None:
        """Insert a new session record.

        Args:
            session_id: Unique session identifier.
            started_at: ISO 8601 timestamp for session start.
        """
        self._execute(
            "INSERT OR IGNORE INTO sessions (id, started_at) VALUES (?, ?)",
            (session_id, started_at),
        )

    def update_session(self, session_id: str, **fields: Any) -> None:
        """Update session fields.

        Args:
            session_id: The session to update.
            **fields: Column name/value pairs to update.
        """
        if not fields:
            return
        set_clauses = ", ".join(f"{k} = ?" for k in fields)
        values = tuple(fields.values()) + (session_id,)
        self._execute(
            f"UPDATE sessions SET {set_clauses} WHERE id = ?",  # noqa: S608
            values,
        )

    def get_session(self, session_id: str) -> dict[str, Any] | None:
        """Retrieve a session by ID.

        Args:
            session_id: The session to retrieve.

        Returns:
            Dict of session fields, or None if not found.
        """
        row = self._fetchone("SELECT * FROM sessions WHERE id = ?", (session_id,))
        if row is None:
            return None
        return dict(row)

    # ------------------------------------------------------------------
    # Events CRUD
    # ------------------------------------------------------------------

    def insert_event(
        self,
        session_id: str,
        event_type: str,
        timestamp: str,
        *,
        tool_name: str | None = None,
        file_path: str | None = None,
        lines_added: int | None = None,
        lines_removed: int | None = None,
        ai_confidence_score: float | None = None,
    ) -> int:
        """Insert an event record.

        Args:
            session_id: The session this event belongs to.
            event_type: The hook event type (e.g., "PostToolUse").
            timestamp: ISO 8601 timestamp.
            tool_name: Name of the tool (for tool events).
            file_path: File path affected (for file operations).
            lines_added: Number of lines added.
            lines_removed: Number of lines removed.
            ai_confidence_score: Computed AI confidence score.

        Returns:
            The auto-generated event ID.
        """
        cursor = self._execute(
            """INSERT INTO events
               (session_id, event_type, timestamp, tool_name, file_path,
                lines_added, lines_removed, ai_confidence_score)
               VALUES (?, ?, ?, ?, ?, ?, ?, ?)""",
            (
                session_id, event_type, timestamp, tool_name, file_path,
                lines_added, lines_removed, ai_confidence_score,
            ),
        )
        return cursor.lastrowid or 0

    def get_events_for_session(self, session_id: str) -> list[dict[str, Any]]:
        """Retrieve all events for a session, ordered by timestamp.

        Args:
            session_id: The session to query.

        Returns:
            List of event dicts.
        """
        rows = self._fetchall(
            "SELECT * FROM events WHERE session_id = ? ORDER BY timestamp",
            (session_id,),
        )
        return [dict(r) for r in rows]

    # ------------------------------------------------------------------
    # Authorship Ledger CRUD
    # ------------------------------------------------------------------

    def insert_authorship_entry(
        self,
        session_id: str,
        file_path: str,
        timestamp: str,
        lines_changed: int,
        ai_confidence_score: float,
        classification: str,
        time_since_prompt_seconds: float | None = None,
    ) -> int:
        """Insert an authorship ledger entry.

        Args:
            session_id: The session this entry belongs to.
            file_path: Path of the file that was written.
            timestamp: ISO 8601 timestamp.
            lines_changed: Total lines modified.
            ai_confidence_score: Computed AI confidence score (0.0-1.0).
            classification: Score classification string.
            time_since_prompt_seconds: Seconds since last user prompt.

        Returns:
            The auto-generated entry ID.
        """
        cursor = self._execute(
            """INSERT INTO authorship_ledger
               (session_id, file_path, timestamp, lines_changed,
                ai_confidence_score, classification, time_since_prompt_seconds)
               VALUES (?, ?, ?, ?, ?, ?, ?)""",
            (
                session_id, file_path, timestamp, lines_changed,
                ai_confidence_score, classification, time_since_prompt_seconds,
            ),
        )
        return cursor.lastrowid or 0

    def get_authorship_for_session(
        self, session_id: str
    ) -> list[dict[str, Any]]:
        """Retrieve all authorship entries for a session.

        Args:
            session_id: The session to query.

        Returns:
            List of authorship entry dicts.
        """
        rows = self._fetchall(
            "SELECT * FROM authorship_ledger WHERE session_id = ? ORDER BY timestamp",
            (session_id,),
        )
        return [dict(r) for r in rows]

    def get_session_ai_ratio(self, session_id: str) -> float:
        """Compute the weighted AI confidence ratio for a session.

        Returns the weighted average of AI confidence scores, where
        each entry is weighted by lines_changed. This gives a more
        accurate picture than a simple average: a 100-line AI write
        counts more than a 2-line human edit.

        Args:
            session_id: The session to compute the ratio for.

        Returns:
            Weighted average AI confidence (0.0-1.0), or 0.0 if
            no authorship entries exist.
        """
        row = self._fetchone(
            """SELECT
                SUM(ai_confidence_score * lines_changed) as weighted_sum,
                SUM(lines_changed) as total_lines
               FROM authorship_ledger
               WHERE session_id = ?""",
            (session_id,),
        )
        if row is None or row["total_lines"] is None or row["total_lines"] == 0:
            return 0.0
        return row["weighted_sum"] / row["total_lines"]

    # ------------------------------------------------------------------
    # Metacognition Logs CRUD
    # ------------------------------------------------------------------

    def insert_metacognition_log(
        self,
        session_id: str,
        timestamp: str,
        trigger_type: str,
        *,
        prompt_id: str | None = None,
        prompt_text: str | None = None,
        alert_level: str | None = None,
    ) -> int:
        """Insert a metacognition log entry.

        Args:
            session_id: The session this log belongs to.
            timestamp: ISO 8601 timestamp.
            trigger_type: What triggered the metacognition event.
            prompt_id: Optional prompt identifier.
            prompt_text: Optional prompt text.
            alert_level: Optional alert level.

        Returns:
            The auto-generated log ID.
        """
        cursor = self._execute(
            """INSERT INTO metacognition_logs
               (session_id, timestamp, trigger_type, prompt_id, prompt_text, alert_level)
               VALUES (?, ?, ?, ?, ?, ?)""",
            (session_id, timestamp, trigger_type, prompt_id, prompt_text, alert_level),
        )
        return cursor.lastrowid or 0

    # ------------------------------------------------------------------
    # Agent Spans CRUD
    # ------------------------------------------------------------------

    def insert_agent_span(
        self,
        session_id: str,
        agent_id: str,
        started_at: str,
        *,
        parent_agent_id: str | None = None,
        agent_type: str | None = None,
    ) -> int:
        """Insert an agent span record.

        Args:
            session_id: The session this span belongs to.
            agent_id: Unique agent identifier.
            started_at: ISO 8601 timestamp for span start.
            parent_agent_id: Parent agent ID (for sub-agents).
            agent_type: Type of agent.

        Returns:
            The auto-generated span ID.
        """
        cursor = self._execute(
            """INSERT INTO agent_spans
               (session_id, agent_id, started_at, parent_agent_id, agent_type)
               VALUES (?, ?, ?, ?, ?)""",
            (session_id, agent_id, started_at, parent_agent_id, agent_type),
        )
        return cursor.lastrowid or 0

    def update_agent_span(
        self,
        session_id: str,
        agent_id: str,
        stopped_at: str,
        files_modified: str | None = None,
    ) -> None:
        """Update an agent span with stop time and files modified.

        Args:
            session_id: The session the span belongs to.
            agent_id: The agent to update.
            stopped_at: ISO 8601 timestamp for span end.
            files_modified: Comma-separated list of modified file paths.
        """
        self._execute(
            """UPDATE agent_spans
               SET stopped_at = ?, files_modified = ?
               WHERE session_id = ? AND agent_id = ? AND stopped_at IS NULL""",
            (stopped_at, files_modified, session_id, agent_id),
        )

    # ------------------------------------------------------------------
    # Query interface
    # ------------------------------------------------------------------

    def query_session_stats(self, days: int = 7) -> list[dict[str, Any]]:
        """Query session statistics for the given number of days.

        Returns sessions ordered by start time, most recent first.

        Args:
            days: Number of days to look back. Default 7.

        Returns:
            List of session dicts with summary fields.
        """
        cutoff = datetime.now(timezone.utc)
        # Compute cutoff as days ago
        from datetime import timedelta
        cutoff = cutoff - timedelta(days=days)
        cutoff_str = cutoff.strftime("%Y-%m-%dT%H:%M:%SZ")

        rows = self._fetchall(
            """SELECT * FROM sessions
               WHERE started_at >= ?
               ORDER BY started_at DESC""",
            (cutoff_str,),
        )
        return [dict(r) for r in rows]

    def query_daily_summary(self, days: int = 7) -> list[dict[str, Any]]:
        """Query daily aggregated summaries.

        Groups sessions by date and computes totals for each day.

        Args:
            days: Number of days to look back.

        Returns:
            List of daily summary dicts with keys: date, sessions,
            total_tool_calls, ai_authored_lines, human_verified_lines,
            total_duration_seconds, estimated_cost_usd.
        """
        from datetime import timedelta
        cutoff = datetime.now(timezone.utc) - timedelta(days=days)
        cutoff_str = cutoff.strftime("%Y-%m-%dT%H:%M:%SZ")

        rows = self._fetchall(
            """SELECT
                substr(started_at, 1, 10) as date,
                COUNT(*) as sessions,
                SUM(COALESCE(total_tool_calls, 0)) as total_tool_calls,
                SUM(COALESCE(ai_authored_lines, 0)) as ai_authored_lines,
                SUM(COALESCE(human_verified_lines, 0)) as human_verified_lines,
                SUM(COALESCE(duration_seconds, 0)) as total_duration_seconds,
                SUM(COALESCE(estimated_cost_usd, 0.0)) as estimated_cost_usd
               FROM sessions
               WHERE started_at >= ?
               GROUP BY substr(started_at, 1, 10)
               ORDER BY date DESC""",
            (cutoff_str,),
        )
        return [dict(r) for r in rows]

    def query_tool_breakdown(
        self, session_id: str | None = None, days: int = 7
    ) -> list[dict[str, Any]]:
        """Query tool usage breakdown.

        Groups events by tool_name and counts occurrences.

        Args:
            session_id: If provided, restrict to this session.
                If None, aggregate across the time window.
            days: Number of days to look back (ignored if session_id set).

        Returns:
            List of dicts with keys: tool_name, count, total_lines_added,
            total_lines_removed.
        """
        if session_id is not None:
            rows = self._fetchall(
                """SELECT
                    tool_name,
                    COUNT(*) as count,
                    SUM(COALESCE(lines_added, 0)) as total_lines_added,
                    SUM(COALESCE(lines_removed, 0)) as total_lines_removed
                   FROM events
                   WHERE session_id = ? AND tool_name IS NOT NULL
                   GROUP BY tool_name
                   ORDER BY count DESC""",
                (session_id,),
            )
        else:
            from datetime import timedelta
            cutoff = datetime.now(timezone.utc) - timedelta(days=days)
            cutoff_str = cutoff.strftime("%Y-%m-%dT%H:%M:%SZ")
            rows = self._fetchall(
                """SELECT
                    tool_name,
                    COUNT(*) as count,
                    SUM(COALESCE(lines_added, 0)) as total_lines_added,
                    SUM(COALESCE(lines_removed, 0)) as total_lines_removed
                   FROM events
                   WHERE timestamp >= ? AND tool_name IS NOT NULL
                   GROUP BY tool_name
                   ORDER BY count DESC""",
                (cutoff_str,),
            )
        return [dict(r) for r in rows]

    def query_authorship_summary(
        self, session_id: str | None = None, days: int = 7
    ) -> dict[str, Any]:
        """Query authorship breakdown summary.

        Args:
            session_id: If provided, restrict to this session.
            days: Number of days to look back (ignored if session_id set).

        Returns:
            Dict with keys: total_entries, total_lines_changed,
            weighted_ai_score, classification_breakdown.
        """
        if session_id is not None:
            rows = self._fetchall(
                """SELECT classification, COUNT(*) as count,
                    SUM(lines_changed) as lines
                   FROM authorship_ledger
                   WHERE session_id = ?
                   GROUP BY classification""",
                (session_id,),
            )
            ratio_row = self._fetchone(
                """SELECT
                    SUM(ai_confidence_score * lines_changed) as weighted_sum,
                    SUM(lines_changed) as total_lines,
                    COUNT(*) as total_entries
                   FROM authorship_ledger
                   WHERE session_id = ?""",
                (session_id,),
            )
        else:
            from datetime import timedelta
            cutoff = datetime.now(timezone.utc) - timedelta(days=days)
            cutoff_str = cutoff.strftime("%Y-%m-%dT%H:%M:%SZ")
            rows = self._fetchall(
                """SELECT classification, COUNT(*) as count,
                    SUM(lines_changed) as lines
                   FROM authorship_ledger
                   WHERE timestamp >= ?
                   GROUP BY classification""",
                (cutoff_str,),
            )
            ratio_row = self._fetchone(
                """SELECT
                    SUM(ai_confidence_score * lines_changed) as weighted_sum,
                    SUM(lines_changed) as total_lines,
                    COUNT(*) as total_entries
                   FROM authorship_ledger
                   WHERE timestamp >= ?""",
                (cutoff_str,),
            )

        classification_breakdown = {
            dict(r)["classification"]: {
                "count": dict(r)["count"],
                "lines": dict(r)["lines"],
            }
            for r in rows
        }

        total_lines = 0
        weighted_score = 0.0
        total_entries = 0
        if ratio_row is not None and ratio_row["total_lines"] is not None:
            total_lines = ratio_row["total_lines"]
            total_entries = ratio_row["total_entries"]
            if total_lines > 0:
                weighted_score = ratio_row["weighted_sum"] / total_lines

        return {
            "total_entries": total_entries,
            "total_lines_changed": total_lines,
            "weighted_ai_score": weighted_score,
            "classification_breakdown": classification_breakdown,
        }
