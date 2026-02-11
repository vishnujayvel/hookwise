"""Tests for hookwise.analytics.authorship -- AI Confidence Scoring and Authorship Ledger."""

from __future__ import annotations

from pathlib import Path
from typing import Any

import pytest

from hookwise.analytics.authorship import (
    AuthorshipLedger,
    CLASSIFICATION_HIGH_PROBABILITY_AI,
    CLASSIFICATION_HUMAN_AUTHORED,
    CLASSIFICATION_LIKELY_AI,
    CLASSIFICATION_MIXED_VERIFIED,
    LINES_LARGE_CHANGE,
    LINES_MEDIUM_CHANGE_MIN,
    LINES_SMALL_CHANGE,
    LINES_TRIVIAL_CHANGE,
    PROMPT_RECENT_THRESHOLD,
    PROMPT_STALE_THRESHOLD,
    WRITE_TOOLS,
)
from hookwise.analytics.db import AnalyticsDB


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def db(tmp_path: Path) -> AnalyticsDB:
    """Provide a fresh AnalyticsDB instance."""
    db_path = tmp_path / "test_authorship.db"
    database = AnalyticsDB(db_path)
    database.insert_session("s1", "2026-02-10T12:00:00Z")
    yield database
    database.close()


@pytest.fixture
def ledger(db: AnalyticsDB) -> AuthorshipLedger:
    """Provide a fresh AuthorshipLedger instance."""
    return AuthorshipLedger(db)


# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------


class TestConstants:
    """Tests for module-level constants."""

    def test_write_tools_set(self) -> None:
        """WRITE_TOOLS should contain the file-writing tools."""
        assert "Write" in WRITE_TOOLS
        assert "Edit" in WRITE_TOOLS
        assert "NotebookEdit" in WRITE_TOOLS
        assert "Bash" not in WRITE_TOOLS
        assert "Read" not in WRITE_TOOLS

    def test_thresholds_reasonable(self) -> None:
        """Time and line thresholds should be in expected ranges."""
        assert PROMPT_RECENT_THRESHOLD == 10.0
        assert PROMPT_STALE_THRESHOLD == 30.0
        assert LINES_LARGE_CHANGE == 50
        assert LINES_MEDIUM_CHANGE_MIN == 10
        assert LINES_SMALL_CHANGE == 5
        assert LINES_TRIVIAL_CHANGE == 3


# ---------------------------------------------------------------------------
# AuthorshipLedger -- record_prompt_timestamp
# ---------------------------------------------------------------------------


class TestRecordPromptTimestamp:
    """Tests for recording prompt timestamps."""

    def test_records_timestamp(self, ledger: AuthorshipLedger) -> None:
        """Should store the prompt timestamp in memory."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        assert "s1" in ledger._last_prompt_timestamps
        assert ledger._last_prompt_timestamps["s1"] == "2026-02-10T12:00:00Z"

    def test_records_char_count(self, ledger: AuthorshipLedger) -> None:
        """Should store the char count in memory."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 150)
        assert ledger._last_prompt_char_counts["s1"] == 150

    def test_updates_on_new_prompt(self, ledger: AuthorshipLedger) -> None:
        """Subsequent prompts should update the timestamp."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:05:00Z", 200)
        assert ledger._last_prompt_timestamps["s1"] == "2026-02-10T12:05:00Z"
        assert ledger._last_prompt_char_counts["s1"] == 200

    def test_independent_sessions(self, ledger: AuthorshipLedger) -> None:
        """Different sessions should have independent prompt timestamps."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        ledger.record_prompt_timestamp("s2", "2026-02-10T13:00:00Z", 200)
        assert ledger._last_prompt_timestamps["s1"] == "2026-02-10T12:00:00Z"
        assert ledger._last_prompt_timestamps["s2"] == "2026-02-10T13:00:00Z"


# ---------------------------------------------------------------------------
# AuthorshipLedger -- compute_ai_score -- Rule 1: Edit + trivial change
# ---------------------------------------------------------------------------


class TestAIScoreRule1HumanAuthored:
    """Tests for Rule 1: Edit with < 3 lines -> human_authored (0.1)."""

    def test_edit_1_line(self, ledger: AuthorshipLedger) -> None:
        """Edit with 1 line changed should score 0.1."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Edit", 1, "2026-02-10T12:00:03Z")
        assert result["score"] == 0.1
        assert result["classification"] == CLASSIFICATION_HUMAN_AUTHORED

    def test_edit_2_lines(self, ledger: AuthorshipLedger) -> None:
        """Edit with 2 lines changed should score 0.1."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Edit", 2, "2026-02-10T12:00:03Z")
        assert result["score"] == 0.1
        assert result["classification"] == CLASSIFICATION_HUMAN_AUTHORED

    def test_edit_0_lines(self, ledger: AuthorshipLedger) -> None:
        """Edit with 0 lines should score 0.1."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Edit", 0, "2026-02-10T12:00:03Z")
        assert result["score"] == 0.1
        assert result["classification"] == CLASSIFICATION_HUMAN_AUTHORED

    def test_edit_3_lines_not_trivial(self, ledger: AuthorshipLedger) -> None:
        """Edit with exactly 3 lines should NOT be classified as human_authored."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Edit", 3, "2026-02-10T12:00:03Z")
        # 3 lines is not < 3, so should not be Rule 1
        assert result["classification"] != CLASSIFICATION_HUMAN_AUTHORED

    def test_write_1_line_not_rule1(self, ledger: AuthorshipLedger) -> None:
        """Write with 1 line should NOT trigger Rule 1 (only Edit does)."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 1, "2026-02-10T12:00:03Z")
        # Write + 1 line + recent prompt -> Rule 4 (small change)
        assert result["classification"] != CLASSIFICATION_HUMAN_AUTHORED


# ---------------------------------------------------------------------------
# AuthorshipLedger -- compute_ai_score -- Rule 2: Recent + large change
# ---------------------------------------------------------------------------


class TestAIScoreRule2HighProbabilityAI:
    """Tests for Rule 2: time < 10s AND lines > 50 -> high_probability_ai."""

    def test_recent_prompt_large_write(self, ledger: AuthorshipLedger) -> None:
        """Write with 100 lines within 5s of prompt should score 0.9+."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 100, "2026-02-10T12:00:05Z")
        assert result["score"] >= 0.9
        assert result["classification"] == CLASSIFICATION_HIGH_PROBABILITY_AI
        assert result["time_since_prompt_seconds"] == 5.0

    def test_recent_prompt_barely_large(self, ledger: AuthorshipLedger) -> None:
        """Write with 51 lines (just above threshold) should score 0.9+."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 51, "2026-02-10T12:00:03Z")
        assert result["score"] >= 0.9
        assert result["classification"] == CLASSIFICATION_HIGH_PROBABILITY_AI

    def test_recent_prompt_exactly_50_not_rule2(self, ledger: AuthorshipLedger) -> None:
        """Write with exactly 50 lines should NOT be Rule 2 (> 50 required)."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 50, "2026-02-10T12:00:03Z")
        # 50 is not > 50, so should be Rule 3 (medium change)
        assert result["classification"] == CLASSIFICATION_LIKELY_AI

    def test_score_scales_with_lines(self, ledger: AuthorshipLedger) -> None:
        """Larger writes should get slightly higher scores."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result_51 = ledger.compute_ai_score("s1", "Write", 51, "2026-02-10T12:00:03Z")
        result_200 = ledger.compute_ai_score("s1", "Write", 200, "2026-02-10T12:00:03Z")
        assert result_200["score"] > result_51["score"]

    def test_score_capped_at_099(self, ledger: AuthorshipLedger) -> None:
        """Score should never exceed 0.99."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 10000, "2026-02-10T12:00:01Z")
        assert result["score"] <= 0.99

    def test_9_seconds_still_recent(self, ledger: AuthorshipLedger) -> None:
        """9.9 seconds should still be considered recent."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 100, "2026-02-10T12:00:09Z")
        assert result["score"] >= 0.9
        assert result["classification"] == CLASSIFICATION_HIGH_PROBABILITY_AI

    def test_10_seconds_not_recent(self, ledger: AuthorshipLedger) -> None:
        """Exactly 10 seconds should NOT be considered recent (< 10 required)."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 100, "2026-02-10T12:00:10Z")
        # 10s is not < 10s, so should not be Rule 2
        assert result["classification"] != CLASSIFICATION_HIGH_PROBABILITY_AI


# ---------------------------------------------------------------------------
# AuthorshipLedger -- compute_ai_score -- Rule 3: Recent + medium change
# ---------------------------------------------------------------------------


class TestAIScoreRule3LikelyAI:
    """Tests for Rule 3: time < 10s AND lines 10-50 -> likely_ai (0.6-0.8)."""

    def test_recent_prompt_medium_write(self, ledger: AuthorshipLedger) -> None:
        """Write with 30 lines within 5s should be likely_ai."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 30, "2026-02-10T12:00:05Z")
        assert 0.6 <= result["score"] <= 0.8
        assert result["classification"] == CLASSIFICATION_LIKELY_AI

    def test_10_lines_minimum(self, ledger: AuthorshipLedger) -> None:
        """10 lines (minimum medium) should score at the low end."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 10, "2026-02-10T12:00:03Z")
        assert result["score"] == 0.6
        assert result["classification"] == CLASSIFICATION_LIKELY_AI

    def test_50_lines_maximum(self, ledger: AuthorshipLedger) -> None:
        """50 lines (maximum medium) should score at the high end."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 50, "2026-02-10T12:00:03Z")
        assert result["score"] == 0.8
        assert result["classification"] == CLASSIFICATION_LIKELY_AI

    def test_score_scales_linearly(self, ledger: AuthorshipLedger) -> None:
        """Score should increase linearly from 0.6 to 0.8 across the range."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result_20 = ledger.compute_ai_score("s1", "Write", 20, "2026-02-10T12:00:03Z")
        result_40 = ledger.compute_ai_score("s1", "Write", 40, "2026-02-10T12:00:03Z")
        assert result_20["score"] < result_40["score"]

    def test_9_lines_not_medium(self, ledger: AuthorshipLedger) -> None:
        """9 lines should NOT be classified as likely_ai (below minimum)."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 9, "2026-02-10T12:00:03Z")
        assert result["classification"] != CLASSIFICATION_LIKELY_AI


# ---------------------------------------------------------------------------
# AuthorshipLedger -- compute_ai_score -- Rule 4: Stale or small change
# ---------------------------------------------------------------------------


class TestAIScoreRule4MixedVerified:
    """Tests for Rule 4: time > 30s OR lines < 5 -> mixed_verified (0.2-0.4)."""

    def test_stale_prompt_large_change(self, ledger: AuthorshipLedger) -> None:
        """Large change 31s after prompt should be mixed_verified."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 100, "2026-02-10T12:00:31Z")
        assert 0.2 <= result["score"] <= 0.4
        assert result["classification"] == CLASSIFICATION_MIXED_VERIFIED

    def test_small_change_recent_prompt(self, ledger: AuthorshipLedger) -> None:
        """4 lines within 5s of prompt should be mixed_verified."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 4, "2026-02-10T12:00:05Z")
        assert 0.2 <= result["score"] <= 0.4
        assert result["classification"] == CLASSIFICATION_MIXED_VERIFIED

    def test_1_line_change(self, ledger: AuthorshipLedger) -> None:
        """1 line change with Write (not Edit) should be mixed_verified."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 1, "2026-02-10T12:00:05Z")
        assert result["classification"] == CLASSIFICATION_MIXED_VERIFIED

    def test_very_stale_prompt(self, ledger: AuthorshipLedger) -> None:
        """60s since prompt should score lower within the range."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 100, "2026-02-10T12:01:00Z")
        assert 0.2 <= result["score"] <= 0.4

    def test_30s_not_stale(self, ledger: AuthorshipLedger) -> None:
        """Exactly 30s is NOT > 30s, so should not trigger stale rule alone."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 30, "2026-02-10T12:00:30Z")
        # 30s is not > 30s, and 30 lines is medium, so this is the ambiguous case
        # Should be ambiguous (Rule 5) or mixed_verified
        assert result["score"] == 0.5 or result["classification"] == CLASSIFICATION_MIXED_VERIFIED


# ---------------------------------------------------------------------------
# AuthorshipLedger -- compute_ai_score -- No prompt timestamp
# ---------------------------------------------------------------------------


class TestAIScoreNoPrompt:
    """Tests for scoring when no prompt timestamp is available."""

    def test_no_prompt_large_change(self, ledger: AuthorshipLedger) -> None:
        """Large change with no prompt should be likely_ai."""
        result = ledger.compute_ai_score("s1", "Write", 100, "2026-02-10T12:00:05Z")
        assert result["score"] == 0.7
        assert result["classification"] == CLASSIFICATION_LIKELY_AI
        assert result["time_since_prompt_seconds"] is None

    def test_no_prompt_small_change(self, ledger: AuthorshipLedger) -> None:
        """Small change with no prompt should be mixed_verified."""
        result = ledger.compute_ai_score("s1", "Write", 10, "2026-02-10T12:00:05Z")
        assert result["score"] == 0.3
        assert result["classification"] == CLASSIFICATION_MIXED_VERIFIED

    def test_edit_no_prompt_trivial(self, ledger: AuthorshipLedger) -> None:
        """Edit with trivial change and no prompt should still be human_authored."""
        result = ledger.compute_ai_score("s1", "Edit", 1, "2026-02-10T12:00:05Z")
        # Rule 1 fires before prompt check
        assert result["score"] == 0.1
        assert result["classification"] == CLASSIFICATION_HUMAN_AUTHORED


# ---------------------------------------------------------------------------
# AuthorshipLedger -- compute_ai_score -- Rule 5: Ambiguous
# ---------------------------------------------------------------------------


class TestAIScoreRule5Ambiguous:
    """Tests for Rule 5: ambiguous timing (10-30s, medium changes)."""

    def test_ambiguous_timing(self, ledger: AuthorshipLedger) -> None:
        """20s since prompt with 20 lines should be ambiguous."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 20, "2026-02-10T12:00:20Z")
        assert result["score"] == 0.5
        assert result["classification"] == CLASSIFICATION_MIXED_VERIFIED

    def test_15s_medium_change(self, ledger: AuthorshipLedger) -> None:
        """15s since prompt with 15 lines should be ambiguous."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        result = ledger.compute_ai_score("s1", "Write", 15, "2026-02-10T12:00:15Z")
        assert result["score"] == 0.5


# ---------------------------------------------------------------------------
# AuthorshipLedger -- Database persistence
# ---------------------------------------------------------------------------


class TestAuthorshipPersistence:
    """Tests for authorship data persistence in the database."""

    def test_compute_stores_in_db(self, ledger: AuthorshipLedger, db: AnalyticsDB) -> None:
        """compute_ai_score should persist the entry in the database."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        ledger.compute_ai_score(
            "s1", "Write", 100, "2026-02-10T12:00:05Z",
            file_path="/tmp/test.py",
        )
        entries = db.get_authorship_for_session("s1")
        assert len(entries) == 1
        assert entries[0]["file_path"] == "/tmp/test.py"
        assert entries[0]["lines_changed"] == 100
        assert entries[0]["ai_confidence_score"] >= 0.9

    def test_multiple_entries_accumulated(self, ledger: AuthorshipLedger, db: AnalyticsDB) -> None:
        """Multiple compute calls should accumulate entries."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        ledger.compute_ai_score("s1", "Write", 100, "2026-02-10T12:00:05Z", file_path="/a.py")
        ledger.compute_ai_score("s1", "Edit", 2, "2026-02-10T12:00:10Z", file_path="/b.py")
        ledger.compute_ai_score("s1", "Write", 30, "2026-02-10T12:00:15Z", file_path="/c.py")
        entries = db.get_authorship_for_session("s1")
        assert len(entries) == 3


# ---------------------------------------------------------------------------
# AuthorshipLedger -- get_session_ai_ratio
# ---------------------------------------------------------------------------


class TestSessionAIRatio:
    """Tests for the session AI ratio computation."""

    def test_single_entry_ratio(self, ledger: AuthorshipLedger) -> None:
        """Ratio with one entry should equal that entry's score."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        ledger.compute_ai_score("s1", "Write", 100, "2026-02-10T12:00:05Z")
        ratio = ledger.get_session_ai_ratio("s1")
        # Single entry: ratio should equal the score
        assert ratio >= 0.9

    def test_weighted_ratio(self, ledger: AuthorshipLedger) -> None:
        """Ratio should be weighted by lines_changed."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        # Large AI write: 100 lines, high score
        ledger.compute_ai_score("s1", "Write", 100, "2026-02-10T12:00:05Z")
        # Small human edit: 2 lines, low score
        ledger.compute_ai_score("s1", "Edit", 2, "2026-02-10T12:00:10Z")
        ratio = ledger.get_session_ai_ratio("s1")
        # Should be dominated by the large write
        assert ratio > 0.5

    def test_empty_session_ratio(self, ledger: AuthorshipLedger) -> None:
        """Empty session should have 0.0 ratio."""
        ratio = ledger.get_session_ai_ratio("s1")
        assert ratio == 0.0


# ---------------------------------------------------------------------------
# AuthorshipLedger -- _compute_time_since_prompt
# ---------------------------------------------------------------------------


class TestTimeSincePrompt:
    """Tests for time-since-prompt computation."""

    def test_normal_time_delta(self, ledger: AuthorshipLedger) -> None:
        """Should correctly compute seconds between timestamps."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        delta = ledger._compute_time_since_prompt("s1", "2026-02-10T12:00:05Z")
        assert delta == 5.0

    def test_no_prompt_recorded(self, ledger: AuthorshipLedger) -> None:
        """Should return None when no prompt has been recorded."""
        delta = ledger._compute_time_since_prompt("s1", "2026-02-10T12:00:05Z")
        assert delta is None

    def test_zero_delta(self, ledger: AuthorshipLedger) -> None:
        """Same timestamp should give 0 delta."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        delta = ledger._compute_time_since_prompt("s1", "2026-02-10T12:00:00Z")
        assert delta == 0.0

    def test_large_delta(self, ledger: AuthorshipLedger) -> None:
        """Should handle large time deltas (hours)."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:00Z", 100)
        delta = ledger._compute_time_since_prompt("s1", "2026-02-10T14:00:00Z")
        assert delta == 7200.0

    def test_negative_delta_clamped(self, ledger: AuthorshipLedger) -> None:
        """Event timestamp before prompt should be clamped to 0."""
        ledger.record_prompt_timestamp("s1", "2026-02-10T12:00:10Z", 100)
        delta = ledger._compute_time_since_prompt("s1", "2026-02-10T12:00:05Z")
        assert delta == 0.0

    def test_invalid_timestamp_returns_none(self, ledger: AuthorshipLedger) -> None:
        """Invalid timestamps should return None."""
        ledger._last_prompt_timestamps["s1"] = "not-a-timestamp"
        delta = ledger._compute_time_since_prompt("s1", "2026-02-10T12:00:05Z")
        assert delta is None


# ---------------------------------------------------------------------------
# AuthorshipLedger -- _parse_timestamp
# ---------------------------------------------------------------------------


class TestParseTimestamp:
    """Tests for timestamp parsing."""

    def test_z_suffix(self) -> None:
        """Should parse Z-suffix timestamps."""
        dt = AuthorshipLedger._parse_timestamp("2026-02-10T12:00:00Z")
        assert dt.year == 2026
        assert dt.month == 2
        assert dt.hour == 12

    def test_offset_format(self) -> None:
        """Should parse +00:00 offset timestamps."""
        dt = AuthorshipLedger._parse_timestamp("2026-02-10T12:00:00+00:00")
        assert dt.year == 2026

    def test_invalid_raises(self) -> None:
        """Invalid timestamp should raise ValueError."""
        with pytest.raises(ValueError):
            AuthorshipLedger._parse_timestamp("not-a-timestamp")


# ---------------------------------------------------------------------------
# AuthorshipLedger -- _score_heuristic (static method)
# ---------------------------------------------------------------------------


class TestScoreHeuristicDirect:
    """Direct tests for the scoring heuristic logic."""

    def test_edit_trivial_always_wins(self) -> None:
        """Rule 1 should fire regardless of timing."""
        # Even with very recent prompt (0s), Edit + 1 line = human
        score, cls, reason = AuthorshipLedger._score_heuristic("Edit", 1, 0.0)
        assert score == 0.1
        assert cls == CLASSIFICATION_HUMAN_AUTHORED

    def test_large_change_no_prompt(self) -> None:
        """Large change with no prompt should be likely_ai."""
        score, cls, reason = AuthorshipLedger._score_heuristic("Write", 100, None)
        assert score == 0.7
        assert cls == CLASSIFICATION_LIKELY_AI

    def test_small_change_no_prompt(self) -> None:
        """Small change with no prompt should be mixed_verified."""
        score, cls, reason = AuthorshipLedger._score_heuristic("Write", 10, None)
        assert score == 0.3
        assert cls == CLASSIFICATION_MIXED_VERIFIED

    def test_reasoning_included(self) -> None:
        """All results should include a reasoning string."""
        _, _, reason = AuthorshipLedger._score_heuristic("Edit", 1, 0.0)
        assert len(reason) > 0
        _, _, reason = AuthorshipLedger._score_heuristic("Write", 100, 3.0)
        assert len(reason) > 0

    def test_notebook_edit_treated_like_write(self) -> None:
        """NotebookEdit should NOT trigger Rule 1 (Edit-only rule)."""
        score, cls, _ = AuthorshipLedger._score_heuristic("NotebookEdit", 1, 3.0)
        # NotebookEdit with 1 line -> Rule 4 (small change), not Rule 1
        assert cls != CLASSIFICATION_HUMAN_AUTHORED

    def test_boundary_10s_50_lines(self) -> None:
        """At exactly 10s and 50 lines, should not be high_probability_ai."""
        score, cls, _ = AuthorshipLedger._score_heuristic("Write", 50, 10.0)
        assert cls != CLASSIFICATION_HIGH_PROBABILITY_AI
