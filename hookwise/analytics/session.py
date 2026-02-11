"""Session tracking and classification for hookwise analytics.

Manages session lifecycle: start, event accumulation, end with summary
computation. Classifies sessions based on dominant tool usage patterns.

Session classification heuristic:
- "coding": Majority of tool calls are Write/Edit/NotebookEdit
- "reviewing": Majority are Read/Grep/Glob
- "tooling": Majority are Bash/script commands
- "writing": Majority are file creation (Write with new files)
- "mixed": No clear majority pattern
"""

from __future__ import annotations

import logging
from datetime import datetime, timezone
from typing import Any

from hookwise.analytics.db import AnalyticsDB

logger = logging.getLogger("hookwise")

# Tool groups for session classification
_CODING_TOOLS = frozenset({"Write", "Edit", "NotebookEdit"})
_REVIEWING_TOOLS = frozenset({"Read", "Grep", "Glob"})
_TOOLING_TOOLS = frozenset({"Bash"})

# Minimum ratio for a classification to be "dominant"
_CLASSIFICATION_THRESHOLD = 0.4

# Cost estimation constants (rough approximations)
# Claude Opus 4: ~$15/1M input tokens, ~$75/1M output tokens
# Average blended rate: ~$30/1M tokens
_COST_PER_TOKEN = 30.0 / 1_000_000
# Rough token estimates per event type
_TOKENS_PER_TOOL_CALL = 500       # Average tokens per tool use cycle
_TOKENS_PER_PROMPT = 200          # Average tokens per user prompt
_TOKENS_PER_LINE_WRITTEN = 10     # Average tokens per line of code written


class SessionTracker:
    """Tracks session lifecycle and computes end-of-session summaries.

    Accumulates event counts during a session and computes the final
    summary when the session ends.

    Usage::

        tracker = SessionTracker(db)
        tracker.start_session("session-1")
        tracker.record_tool_call("session-1", "Write", lines_added=50)
        tracker.record_tool_call("session-1", "Edit", lines_added=5, lines_removed=3)
        summary = tracker.end_session("session-1")

    Attributes:
        db: The underlying AnalyticsDB instance.
    """

    def __init__(self, db: AnalyticsDB) -> None:
        """Initialize the session tracker.

        Args:
            db: The AnalyticsDB instance to use for storage.
        """
        self.db = db
        # In-memory counters per session
        self._tool_counts: dict[str, dict[str, int]] = {}
        self._total_lines_added: dict[str, int] = {}
        self._total_lines_removed: dict[str, int] = {}
        self._prompt_count: dict[str, int] = {}

    def start_session(self, session_id: str, timestamp: str | None = None) -> None:
        """Start a new session.

        Creates the session record in the database and initializes
        in-memory counters.

        Args:
            session_id: Unique session identifier.
            timestamp: ISO 8601 start timestamp. Uses current UTC if None.
        """
        if timestamp is None:
            timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

        self.db.insert_session(session_id, timestamp)
        self._tool_counts[session_id] = {}
        self._total_lines_added[session_id] = 0
        self._total_lines_removed[session_id] = 0
        self._prompt_count[session_id] = 0

    def record_tool_call(
        self,
        session_id: str,
        tool_name: str,
        *,
        lines_added: int = 0,
        lines_removed: int = 0,
    ) -> None:
        """Record a tool call for session tracking.

        Updates in-memory counters for classification and summary.

        Args:
            session_id: The session to update.
            tool_name: Name of the tool that was called.
            lines_added: Lines added by this call.
            lines_removed: Lines removed by this call.
        """
        if session_id not in self._tool_counts:
            self._tool_counts[session_id] = {}
        counts = self._tool_counts[session_id]
        counts[tool_name] = counts.get(tool_name, 0) + 1

        if session_id not in self._total_lines_added:
            self._total_lines_added[session_id] = 0
        self._total_lines_added[session_id] += lines_added

        if session_id not in self._total_lines_removed:
            self._total_lines_removed[session_id] = 0
        self._total_lines_removed[session_id] += lines_removed

    def record_prompt(self, session_id: str) -> None:
        """Record a user prompt for session tracking.

        Args:
            session_id: The session to update.
        """
        if session_id not in self._prompt_count:
            self._prompt_count[session_id] = 0
        self._prompt_count[session_id] += 1

    def end_session(
        self,
        session_id: str,
        timestamp: str | None = None,
        *,
        ai_authored_lines: int | None = None,
        human_verified_lines: int | None = None,
    ) -> dict[str, Any]:
        """End a session and compute the summary.

        Computes duration, classification, tool counts, and cost
        estimates. Updates the session record in the database.

        Args:
            session_id: The session to end.
            timestamp: ISO 8601 end timestamp. Uses current UTC if None.
            ai_authored_lines: Override for AI-authored line count.
                If None, computed from authorship ledger.
            human_verified_lines: Override for human-verified line count.
                If None, computed from authorship ledger.

        Returns:
            Summary dict with all computed fields.
        """
        if timestamp is None:
            timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

        session = self.db.get_session(session_id)
        if session is None:
            logger.warning("Session %s not found, creating minimal record", session_id)
            return {"session_id": session_id, "error": "session_not_found"}

        # Compute duration
        duration_seconds = self._compute_duration(session["started_at"], timestamp)

        # Compute tool call totals
        tool_counts = self._tool_counts.get(session_id, {})
        total_tool_calls = sum(tool_counts.values())

        # Compute file edit count (Write + Edit + NotebookEdit calls)
        file_edits_count = sum(
            tool_counts.get(t, 0) for t in _CODING_TOOLS
        )

        # Compute classification
        classification = self._classify_session(tool_counts)

        # Compute authorship lines from ledger if not provided
        if ai_authored_lines is None or human_verified_lines is None:
            authorship = self.db.query_authorship_summary(session_id=session_id)
            breakdown = authorship.get("classification_breakdown", {})
            total_lines = authorship.get("total_lines_changed", 0)
            weighted_score = authorship.get("weighted_ai_score", 0.0)

            if ai_authored_lines is None:
                ai_authored_lines = int(total_lines * weighted_score)
            if human_verified_lines is None:
                human_verified_lines = total_lines - ai_authored_lines

        # Estimate tokens and cost
        estimated_tokens = self._estimate_tokens(
            total_tool_calls,
            self._prompt_count.get(session_id, 0),
            self._total_lines_added.get(session_id, 0),
        )
        estimated_cost = estimated_tokens * _COST_PER_TOKEN

        summary = {
            "session_id": session_id,
            "ended_at": timestamp,
            "duration_seconds": duration_seconds,
            "total_tool_calls": total_tool_calls,
            "file_edits_count": file_edits_count,
            "ai_authored_lines": ai_authored_lines,
            "human_verified_lines": human_verified_lines,
            "classification": classification,
            "estimated_tokens": estimated_tokens,
            "estimated_cost_usd": round(estimated_cost, 6),
        }

        # Update the database
        self.db.update_session(
            session_id,
            ended_at=timestamp,
            duration_seconds=duration_seconds,
            total_tool_calls=total_tool_calls,
            file_edits_count=file_edits_count,
            ai_authored_lines=ai_authored_lines,
            human_verified_lines=human_verified_lines,
            classification=classification,
            estimated_tokens=estimated_tokens,
            estimated_cost_usd=round(estimated_cost, 6),
        )

        # Clean up in-memory state
        self._tool_counts.pop(session_id, None)
        self._total_lines_added.pop(session_id, None)
        self._total_lines_removed.pop(session_id, None)
        self._prompt_count.pop(session_id, None)

        return summary

    def _compute_duration(self, started_at: str, ended_at: str) -> int:
        """Compute session duration in seconds.

        Args:
            started_at: ISO 8601 start timestamp.
            ended_at: ISO 8601 end timestamp.

        Returns:
            Duration in whole seconds, or 0 on parse failure.
        """
        try:
            start_dt = self._parse_timestamp(started_at)
            end_dt = self._parse_timestamp(ended_at)
            delta = (end_dt - start_dt).total_seconds()
            return max(0, int(delta))
        except (ValueError, TypeError):
            logger.debug(
                "Could not compute duration: %s to %s", started_at, ended_at,
            )
            return 0

    @staticmethod
    def _parse_timestamp(ts: str) -> datetime:
        """Parse an ISO 8601 timestamp string to a datetime."""
        if ts.endswith("Z"):
            ts = ts[:-1] + "+00:00"
        return datetime.fromisoformat(ts)

    @staticmethod
    def _classify_session(tool_counts: dict[str, int]) -> str:
        """Classify a session based on dominant tool usage.

        Args:
            tool_counts: Dict mapping tool names to call counts.

        Returns:
            Classification string: "coding", "reviewing", "tooling", or "mixed".
        """
        total = sum(tool_counts.values())
        if total == 0:
            return "mixed"

        coding_count = sum(tool_counts.get(t, 0) for t in _CODING_TOOLS)
        reviewing_count = sum(tool_counts.get(t, 0) for t in _REVIEWING_TOOLS)
        tooling_count = sum(tool_counts.get(t, 0) for t in _TOOLING_TOOLS)

        coding_ratio = coding_count / total
        reviewing_ratio = reviewing_count / total
        tooling_ratio = tooling_count / total

        # Find the dominant category
        ratios = {
            "coding": coding_ratio,
            "reviewing": reviewing_ratio,
            "tooling": tooling_ratio,
        }
        dominant = max(ratios, key=ratios.get)  # type: ignore[arg-type]
        if ratios[dominant] >= _CLASSIFICATION_THRESHOLD:
            return dominant

        return "mixed"

    @staticmethod
    def _estimate_tokens(
        tool_calls: int,
        prompts: int,
        lines_written: int,
    ) -> int:
        """Estimate total token usage for the session.

        Uses rough heuristics based on typical Claude Code usage patterns.

        Args:
            tool_calls: Number of tool calls.
            prompts: Number of user prompts.
            lines_written: Total lines of code written.

        Returns:
            Estimated token count.
        """
        return (
            tool_calls * _TOKENS_PER_TOOL_CALL
            + prompts * _TOKENS_PER_PROMPT
            + lines_written * _TOKENS_PER_LINE_WRITTEN
        )
