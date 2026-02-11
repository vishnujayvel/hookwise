"""Analytics engine for hookwise.

Collects and stores usage analytics from Claude Code hook events.
Provides the AnalyticsEngine facade class and the handle() builtin
entry point for the dispatcher.

Event handling:
- PostToolUse: Record tool usage event + compute authorship score for file writes
- UserPromptSubmit: Record prompt timestamp (NEVER stores content)
- SessionStart: Start a new session
- Stop/SessionEnd: End session with computed summary

All writes are designed to be non-blocking side effects (Phase 3).
Database errors are logged but never crash the hook process (fail-open).
"""

from __future__ import annotations

import json
import logging
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from hookwise.analytics.authorship import AuthorshipLedger, WRITE_TOOLS
from hookwise.analytics.db import AnalyticsDB
from hookwise.analytics.session import SessionTracker

logger = logging.getLogger("hookwise")


class AnalyticsEngine:
    """Facade for the hookwise analytics subsystem.

    Coordinates the database layer, authorship ledger, and session
    tracker into a single interface. Used by the handle() builtin
    entry point and the stats CLI command.

    Usage::

        engine = AnalyticsEngine()
        engine.start_session("session-1")
        engine.record_event({
            "event_type": "PostToolUse",
            "tool_name": "Write",
            "timestamp": "2026-02-10T12:00:05Z",
            "file_path": "/path/to/file.py",
            "lines_added": 50,
            "lines_removed": 0,
        })
        engine.end_session("session-1", {"ended_at": "2026-02-10T12:30:00Z"})
        stats = engine.query_stats(days=7)

    Attributes:
        db: The AnalyticsDB instance.
        ledger: The AuthorshipLedger instance.
        tracker: The SessionTracker instance.
    """

    def __init__(self, db_path: Path | None = None) -> None:
        """Initialize with DB path (default ~/.hookwise/analytics.db).

        Args:
            db_path: Path to the SQLite database file. If None, uses
                the default path from AnalyticsDB.
        """
        self.db = AnalyticsDB(db_path)
        self.ledger = AuthorshipLedger(self.db)
        self.tracker = SessionTracker(self.db)

    def record_event(self, event: dict[str, Any]) -> None:
        """Record a hook event.

        Dispatches to the appropriate handler based on event_type.
        For file write events (PostToolUse with Write/Edit/NotebookEdit),
        also computes and stores the AI confidence score.

        Args:
            event: Event dict with keys:
                - session_id: str (required)
                - event_type: str (required)
                - timestamp: str ISO 8601 (required)
                - tool_name: str (optional, for tool events)
                - file_path: str (optional, for file operations)
                - lines_added: int (optional)
                - lines_removed: int (optional)
                - char_count: int (optional, for prompt events)
        """
        session_id = event.get("session_id", "")
        event_type = event.get("event_type", "")
        timestamp = event.get("timestamp", "")

        if not session_id or not event_type or not timestamp:
            logger.debug("Ignoring event with missing required fields: %s", event)
            return

        try:
            if event_type == "UserPromptSubmit":
                self._handle_prompt(session_id, timestamp, event)
            elif event_type == "PostToolUse":
                self._handle_tool_use(session_id, timestamp, event)
            elif event_type == "SessionStart":
                self._handle_session_start(session_id, timestamp, event)
            elif event_type in ("Stop", "SessionEnd"):
                self._handle_session_end(session_id, timestamp, event)
            else:
                # Record other events as generic entries
                self._record_generic_event(session_id, event_type, timestamp, event)
        except Exception as exc:
            logger.error("Failed to record event %s: %s", event_type, exc)

    def start_session(self, session_id: str, timestamp: str | None = None) -> None:
        """Start a new analytics session.

        Args:
            session_id: Unique session identifier.
            timestamp: ISO 8601 start timestamp. Uses current UTC if None.
        """
        if timestamp is None:
            timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        try:
            self.tracker.start_session(session_id, timestamp)
        except Exception as exc:
            logger.error("Failed to start session %s: %s", session_id, exc)

    def end_session(self, session_id: str, summary: dict[str, Any] | None = None) -> dict[str, Any]:
        """End a session and compute the summary.

        Args:
            session_id: The session to end.
            summary: Optional summary overrides (e.g., ended_at).

        Returns:
            Computed summary dict.
        """
        if summary is None:
            summary = {}
        try:
            timestamp = summary.get("ended_at")
            return self.tracker.end_session(
                session_id,
                timestamp=timestamp,
                ai_authored_lines=summary.get("ai_authored_lines"),
                human_verified_lines=summary.get("human_verified_lines"),
            )
        except Exception as exc:
            logger.error("Failed to end session %s: %s", session_id, exc)
            return {"session_id": session_id, "error": str(exc)}

    def query_stats(self, days: int = 7, format: str = "dict") -> dict[str, Any]:
        """Query session stats for the stats CLI command.

        Computes a comprehensive analytics summary including daily
        breakdowns, tool usage, and authorship statistics.

        Args:
            days: Number of days to look back.
            format: Output format. Currently only "dict" is supported.
                    "json" will serialize the dict to a JSON string.

        Returns:
            Dict with keys: sessions, daily_summary, tool_breakdown,
            authorship_summary.
        """
        try:
            sessions = self.db.query_session_stats(days=days)
            daily = self.db.query_daily_summary(days=days)
            tools = self.db.query_tool_breakdown(days=days)
            authorship = self.db.query_authorship_summary(days=days)

            result = {
                "days": days,
                "sessions": sessions,
                "daily_summary": daily,
                "tool_breakdown": tools,
                "authorship_summary": authorship,
            }

            if format == "json":
                result = {"json": json.dumps(result, indent=2, default=str)}

            return result
        except Exception as exc:
            logger.error("Failed to query stats: %s", exc)
            return {"error": str(exc)}

    def close(self) -> None:
        """Close the database connection."""
        self.db.close()

    # ------------------------------------------------------------------
    # Private event handlers
    # ------------------------------------------------------------------

    def _handle_prompt(
        self, session_id: str, timestamp: str, event: dict[str, Any]
    ) -> None:
        """Handle a UserPromptSubmit event.

        Records the prompt timestamp for authorship scoring.
        NEVER stores the actual prompt content.

        Args:
            session_id: Active session ID.
            timestamp: Event timestamp.
            event: Full event dict.
        """
        char_count = event.get("char_count", 0)
        if not isinstance(char_count, int):
            char_count = 0

        # Record prompt timestamp for authorship scoring
        self.ledger.record_prompt_timestamp(session_id, timestamp, char_count)

        # Record prompt in session tracker
        self.tracker.record_prompt(session_id)

        # Record the event in the events table
        self.db.insert_event(
            session_id=session_id,
            event_type="UserPromptSubmit",
            timestamp=timestamp,
        )

    def _handle_tool_use(
        self, session_id: str, timestamp: str, event: dict[str, Any]
    ) -> None:
        """Handle a PostToolUse event.

        Records the tool usage event in the events table.
        For file write tools (Write/Edit/NotebookEdit), also computes
        and stores the AI confidence score.

        Args:
            session_id: Active session ID.
            timestamp: Event timestamp.
            event: Full event dict.
        """
        tool_name = event.get("tool_name", "")
        file_path = event.get("file_path", "")
        lines_added = event.get("lines_added", 0) or 0
        lines_removed = event.get("lines_removed", 0) or 0

        # Compute AI confidence score for file writes
        ai_confidence_score = None
        if tool_name in WRITE_TOOLS and (lines_added > 0 or lines_removed > 0):
            lines_changed = lines_added + lines_removed
            score_result = self.ledger.compute_ai_score(
                session_id=session_id,
                tool_name=tool_name,
                lines_changed=lines_changed,
                timestamp=timestamp,
                file_path=file_path,
            )
            ai_confidence_score = score_result.get("score")

        # Record event in the events table
        self.db.insert_event(
            session_id=session_id,
            event_type="PostToolUse",
            timestamp=timestamp,
            tool_name=tool_name,
            file_path=file_path,
            lines_added=lines_added,
            lines_removed=lines_removed,
            ai_confidence_score=ai_confidence_score,
        )

        # Update session tracker
        self.tracker.record_tool_call(
            session_id, tool_name,
            lines_added=lines_added,
            lines_removed=lines_removed,
        )

    def _handle_session_start(
        self, session_id: str, timestamp: str, event: dict[str, Any]
    ) -> None:
        """Handle a SessionStart event.

        Args:
            session_id: Session ID to start.
            timestamp: Event timestamp.
            event: Full event dict.
        """
        self.start_session(session_id, timestamp)

    def _handle_session_end(
        self, session_id: str, timestamp: str, event: dict[str, Any]
    ) -> None:
        """Handle a Stop or SessionEnd event.

        Args:
            session_id: Session to end.
            timestamp: Event timestamp.
            event: Full event dict.
        """
        self.end_session(session_id, {"ended_at": timestamp})

    def _record_generic_event(
        self,
        session_id: str,
        event_type: str,
        timestamp: str,
        event: dict[str, Any],
    ) -> None:
        """Record a generic event (not a special type).

        Args:
            session_id: Active session ID.
            event_type: The event type string.
            timestamp: Event timestamp.
            event: Full event dict.
        """
        self.db.insert_event(
            session_id=session_id,
            event_type=event_type,
            timestamp=timestamp,
            tool_name=event.get("tool_name"),
            file_path=event.get("file_path"),
        )


# ---------------------------------------------------------------------------
# Singleton management
# ---------------------------------------------------------------------------

_engine_instance: AnalyticsEngine | None = None


def _get_engine() -> AnalyticsEngine:
    """Get or create the singleton AnalyticsEngine instance.

    Returns:
        The global AnalyticsEngine instance.
    """
    global _engine_instance
    if _engine_instance is None:
        _engine_instance = AnalyticsEngine()
    return _engine_instance


def _reset_engine() -> None:
    """Reset the singleton engine (for testing)."""
    global _engine_instance
    if _engine_instance is not None:
        _engine_instance.close()
    _engine_instance = None


# ---------------------------------------------------------------------------
# Builtin handler entry point
# ---------------------------------------------------------------------------


def handle(
    event_type: str,
    payload: dict[str, Any],
    config: Any,
) -> dict[str, Any] | None:
    """Builtin handler entry point for the analytics engine.

    Called by the dispatcher when a builtin handler references the
    ``hookwise.analytics`` module. Routes events to the appropriate
    AnalyticsEngine methods.

    This handler is designed for Phase 3 (side effects). It never
    returns a guard decision or additionalContext.

    Args:
        event_type: The hook event type.
        payload: The event payload dict from stdin.
        config: The HooksConfig instance.

    Returns:
        None (analytics is a pure side effect, no output to Claude Code).
    """
    try:
        engine = _get_engine()

        # Extract session_id from payload (Claude Code provides this)
        session_id = payload.get("session_id", "")
        if not session_id:
            # Generate a fallback session ID if not provided
            session_id = f"auto-{uuid.uuid4().hex[:12]}"

        timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

        # Build the event dict
        event: dict[str, Any] = {
            "session_id": session_id,
            "event_type": event_type,
            "timestamp": timestamp,
        }

        # Add tool-specific fields
        if event_type == "PostToolUse":
            tool_name = payload.get("tool_name", "")
            tool_input = payload.get("tool_input", {})
            tool_output = payload.get("tool_output", {})
            if not isinstance(tool_input, dict):
                tool_input = {}
            if not isinstance(tool_output, dict):
                tool_output = {}

            event["tool_name"] = tool_name
            event["file_path"] = tool_input.get("file_path", "") or tool_input.get("path", "")
            event["lines_added"] = _extract_lines(tool_output, "lines_added")
            event["lines_removed"] = _extract_lines(tool_output, "lines_removed")

        elif event_type == "UserPromptSubmit":
            # Record character count but NEVER the content
            prompt_content = payload.get("content", "")
            event["char_count"] = len(prompt_content) if isinstance(prompt_content, str) else 0

        engine.record_event(event)

    except Exception as exc:
        # Fail-open: never let analytics crash the hook
        logger.error("Analytics handle() failed: %s", exc)

    return None


def _extract_lines(output: dict[str, Any], key: str) -> int:
    """Safely extract a line count from tool output.

    Args:
        output: The tool_output dict.
        key: The key to extract (e.g., "lines_added").

    Returns:
        Integer line count, or 0 if not available.
    """
    value = output.get(key, 0)
    if isinstance(value, int):
        return value
    if isinstance(value, str):
        try:
            return int(value)
        except ValueError:
            return 0
    return 0
