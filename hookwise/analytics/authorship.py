"""AI Confidence Scoring and Authorship Ledger for hookwise analytics.

Tracks the provenance of code changes by computing an AI Confidence Score
for each file write event. The score estimates the probability that a
change was AI-authored vs. human-authored, based on timing heuristics
and change magnitude.

Scoring heuristic:
- time_since_prompt < 10s AND lines_changed > 50 -> 0.9+ ("high_probability_ai")
- time_since_prompt < 10s AND lines_changed 10-50 -> 0.6-0.8 ("likely_ai")
- time_since_prompt > 30s OR lines_changed < 5 -> 0.2-0.4 ("mixed_verified")
- tool = Edit AND lines_changed < 3 -> 0.1 ("human_authored")

The last UserPromptSubmit timestamp is tracked per-session to compute
the time-since-prompt latency window used in scoring.

Privacy: Only timestamps and character counts are stored for prompts.
Actual prompt content is NEVER recorded.
"""

from __future__ import annotations

import logging
from datetime import datetime, timezone
from typing import Any

from hookwise.analytics.db import AnalyticsDB

logger = logging.getLogger("hookwise")

# Tools that indicate file write operations
WRITE_TOOLS = frozenset({"Write", "Edit", "NotebookEdit"})

# AI Confidence Score classification thresholds
CLASSIFICATION_HIGH_PROBABILITY_AI = "high_probability_ai"
CLASSIFICATION_LIKELY_AI = "likely_ai"
CLASSIFICATION_MIXED_VERIFIED = "mixed_verified"
CLASSIFICATION_HUMAN_AUTHORED = "human_authored"

# Time thresholds in seconds
PROMPT_RECENT_THRESHOLD = 10.0    # Within 10s of prompt = recent
PROMPT_STALE_THRESHOLD = 30.0     # Beyond 30s = likely human-verified

# Line change thresholds
LINES_LARGE_CHANGE = 50           # > 50 lines = large change
LINES_MEDIUM_CHANGE_MIN = 10      # 10-50 lines = medium change
LINES_SMALL_CHANGE = 5            # < 5 lines = small change
LINES_TRIVIAL_CHANGE = 3          # < 3 lines with Edit = human-like


class AuthorshipLedger:
    """Tracks code authorship by computing AI confidence scores.

    Wraps the AnalyticsDB for authorship-specific operations. Maintains
    the last prompt timestamp per session (in memory) and uses it to
    compute scores for each file write event.

    Usage::

        ledger = AuthorshipLedger(db)
        ledger.record_prompt_timestamp("session-1", "2026-02-10T12:00:00Z", 150)

        # Later, when a file write happens:
        result = ledger.compute_ai_score(
            "session-1", "Write", 100, "2026-02-10T12:00:05Z"
        )
        # result = {"score": 0.95, "classification": "high_probability_ai", ...}

        ratio = ledger.get_session_ai_ratio("session-1")
        # ratio = 0.95

    Attributes:
        db: The underlying AnalyticsDB instance.
    """

    def __init__(self, db: AnalyticsDB) -> None:
        """Initialize the authorship ledger.

        Args:
            db: The AnalyticsDB instance to use for storage.
        """
        self.db = db
        # In-memory cache of last prompt timestamp per session.
        # Keys are session IDs, values are ISO 8601 timestamp strings.
        self._last_prompt_timestamps: dict[str, str] = {}
        # Also track prompt char counts (for potential future use)
        self._last_prompt_char_counts: dict[str, int] = {}

    def record_prompt_timestamp(
        self,
        session_id: str,
        timestamp: str,
        char_count: int,
    ) -> None:
        """Record the timestamp of a UserPromptSubmit event.

        Updates the in-memory cache of the last prompt time for the
        given session. This is used later by compute_ai_score() to
        determine the time-since-prompt window.

        Args:
            session_id: The active session ID.
            timestamp: ISO 8601 timestamp of the prompt submission.
            char_count: Character count of the prompt (NEVER the content).
        """
        self._last_prompt_timestamps[session_id] = timestamp
        self._last_prompt_char_counts[session_id] = char_count

    def compute_ai_score(
        self,
        session_id: str,
        tool_name: str,
        lines_changed: int,
        timestamp: str,
        *,
        file_path: str = "",
    ) -> dict[str, Any]:
        """Compute the AI Confidence Score for a file write event.

        Uses the time-since-last-prompt and lines-changed heuristics
        to estimate the probability that the change was AI-authored.

        The result is stored in the authorship_ledger table and also
        returned for immediate use (e.g., status line display).

        Args:
            session_id: The active session ID.
            tool_name: The tool that made the write (Write, Edit, NotebookEdit).
            lines_changed: Total number of lines changed (added + removed).
            timestamp: ISO 8601 timestamp of the write event.
            file_path: Path of the file that was written.

        Returns:
            Dict with keys:
            - score: float (0.0-1.0) AI confidence score
            - classification: str classification label
            - time_since_prompt_seconds: float or None
            - reasoning: str human-readable explanation
        """
        time_since_prompt = self._compute_time_since_prompt(session_id, timestamp)
        score, classification, reasoning = self._score_heuristic(
            tool_name, lines_changed, time_since_prompt,
        )

        # Store in the database
        try:
            self.db.insert_authorship_entry(
                session_id=session_id,
                file_path=file_path,
                timestamp=timestamp,
                lines_changed=lines_changed,
                ai_confidence_score=score,
                classification=classification,
                time_since_prompt_seconds=time_since_prompt,
            )
        except Exception as exc:
            logger.error("Failed to insert authorship entry: %s", exc)

        return {
            "score": score,
            "classification": classification,
            "time_since_prompt_seconds": time_since_prompt,
            "reasoning": reasoning,
        }

    def get_session_ai_ratio(self, session_id: str) -> float:
        """Get the running weighted AI confidence ratio for a session.

        Delegates to the database's weighted average computation.

        Args:
            session_id: The session to compute the ratio for.

        Returns:
            Weighted average AI confidence (0.0-1.0).
        """
        return self.db.get_session_ai_ratio(session_id)

    def _compute_time_since_prompt(
        self, session_id: str, event_timestamp: str
    ) -> float | None:
        """Compute seconds since the last prompt for this session.

        Args:
            session_id: The session to check.
            event_timestamp: ISO 8601 timestamp of the current event.

        Returns:
            Seconds since last prompt, or None if no prompt has been
            recorded for this session.
        """
        last_prompt_ts = self._last_prompt_timestamps.get(session_id)
        if last_prompt_ts is None:
            return None

        try:
            event_dt = self._parse_timestamp(event_timestamp)
            prompt_dt = self._parse_timestamp(last_prompt_ts)
            delta = (event_dt - prompt_dt).total_seconds()
            return max(0.0, delta)  # Clamp to non-negative
        except (ValueError, TypeError):
            logger.debug(
                "Could not parse timestamps for time-since-prompt: %s, %s",
                event_timestamp, last_prompt_ts,
            )
            return None

    @staticmethod
    def _parse_timestamp(ts: str) -> datetime:
        """Parse an ISO 8601 timestamp string to a datetime.

        Handles both Z suffix and +00:00 offset formats.

        Args:
            ts: ISO 8601 timestamp string.

        Returns:
            Timezone-aware datetime in UTC.
        """
        # Handle Z suffix
        if ts.endswith("Z"):
            ts = ts[:-1] + "+00:00"
        return datetime.fromisoformat(ts)

    @staticmethod
    def _score_heuristic(
        tool_name: str,
        lines_changed: int,
        time_since_prompt: float | None,
    ) -> tuple[float, str, str]:
        """Apply the AI confidence scoring heuristic.

        The scoring rules are applied in priority order:

        1. Edit with < 3 lines -> 0.1 (human_authored)
           Small edits via the Edit tool are typically human-directed
           surgical changes.

        2. Recent prompt (< 10s) + large change (> 50 lines) -> 0.9+
           AI typically generates large blocks of code immediately
           after receiving a prompt.

        3. Recent prompt (< 10s) + medium change (10-50 lines) -> 0.6-0.8
           Likely AI-generated but possibly with human guidance.

        4. Stale prompt (> 30s) OR small change (< 5 lines) -> 0.2-0.4
           Either the human has been working independently, or the
           change is small enough to be human-authored.

        5. Everything else -> 0.5 (ambiguous)

        Args:
            tool_name: The writing tool name.
            lines_changed: Total lines changed.
            time_since_prompt: Seconds since last prompt, or None.

        Returns:
            Tuple of (score, classification, reasoning).
        """
        # Rule 1: Edit with trivial change
        if tool_name == "Edit" and lines_changed < LINES_TRIVIAL_CHANGE:
            return (
                0.1,
                CLASSIFICATION_HUMAN_AUTHORED,
                f"Edit tool with {lines_changed} lines changed (<{LINES_TRIVIAL_CHANGE})",
            )

        # If we have no prompt timestamp, use a conservative score
        if time_since_prompt is None:
            if lines_changed > LINES_LARGE_CHANGE:
                return (
                    0.7,
                    CLASSIFICATION_LIKELY_AI,
                    f"No prompt timestamp, but {lines_changed} lines (>{LINES_LARGE_CHANGE}) suggests AI",
                )
            return (
                0.3,
                CLASSIFICATION_MIXED_VERIFIED,
                "No prompt timestamp available, using conservative estimate",
            )

        # Rule 2: Recent prompt + large change
        if time_since_prompt < PROMPT_RECENT_THRESHOLD and lines_changed > LINES_LARGE_CHANGE:
            # Scale between 0.90 and 0.99 based on lines_changed
            score = min(0.99, 0.90 + (lines_changed - LINES_LARGE_CHANGE) * 0.001)
            return (
                score,
                CLASSIFICATION_HIGH_PROBABILITY_AI,
                f"Recent prompt ({time_since_prompt:.1f}s) + large change "
                f"({lines_changed} lines)",
            )

        # Rule 3: Recent prompt + medium change
        if (
            time_since_prompt < PROMPT_RECENT_THRESHOLD
            and lines_changed >= LINES_MEDIUM_CHANGE_MIN
        ):
            # Scale between 0.6 and 0.8 based on lines_changed
            ratio = (lines_changed - LINES_MEDIUM_CHANGE_MIN) / (
                LINES_LARGE_CHANGE - LINES_MEDIUM_CHANGE_MIN
            )
            score = 0.6 + ratio * 0.2
            return (
                round(score, 2),
                CLASSIFICATION_LIKELY_AI,
                f"Recent prompt ({time_since_prompt:.1f}s) + medium change "
                f"({lines_changed} lines)",
            )

        # Rule 4: Stale prompt OR small change
        if time_since_prompt > PROMPT_STALE_THRESHOLD or lines_changed < LINES_SMALL_CHANGE:
            # Scale between 0.2 and 0.4
            if lines_changed < LINES_SMALL_CHANGE:
                score = 0.2 + lines_changed * 0.04
            else:
                # Beyond 30s, score decreases
                score = max(0.2, 0.4 - (time_since_prompt - PROMPT_STALE_THRESHOLD) * 0.005)
            score = round(min(0.4, max(0.2, score)), 2)
            return (
                score,
                CLASSIFICATION_MIXED_VERIFIED,
                f"{'Stale prompt' if time_since_prompt > PROMPT_STALE_THRESHOLD else 'Small change'} "
                f"({time_since_prompt:.1f}s, {lines_changed} lines)",
            )

        # Rule 5: Ambiguous (10-30s window, medium-ish changes)
        return (
            0.5,
            CLASSIFICATION_MIXED_VERIFIED,
            f"Ambiguous timing ({time_since_prompt:.1f}s, {lines_changed} lines)",
        )
