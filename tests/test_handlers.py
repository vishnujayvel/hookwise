"""Tests for hookwise event handlers: greeting, sounds, transcript, agents, cost.

Covers Tasks 8.1-8.6:
- 8.1: Session greeting handler
- 8.2: Notification sounds handler
- 8.3: Transcript backup handler
- 8.4: Multi-agent observability handler
- 8.5: Cost estimation and logging
- 8.6: Budget enforcement guard
"""

from __future__ import annotations

import json
import os
import random
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from unittest.mock import MagicMock, patch

import pytest

from hookwise.config import HooksConfig


# =========================================================================
# Task 8.1: Greeting handler tests
# =========================================================================


class TestGreetingQuoteSelection:
    """Tests for quote loading and weighted selection logic."""

    def test_default_quotes_exist(self):
        """DEFAULT_QUOTES has at least 3 entries with required keys."""
        from hookwise.greeting import DEFAULT_QUOTES

        assert len(DEFAULT_QUOTES) >= 3
        for q in DEFAULT_QUOTES:
            assert "text" in q
            assert "attribution" in q
            assert "category" in q

    def test_select_quote_from_defaults(self):
        """select_quote returns a quote from the provided list."""
        from hookwise.greeting import DEFAULT_QUOTES, select_quote

        rng = random.Random(42)
        quote = select_quote(DEFAULT_QUOTES, {"grit": 1.0}, rng=rng)
        assert quote is not None
        assert "text" in quote

    def test_select_quote_empty_list_returns_none(self):
        """select_quote returns None for empty quote list."""
        from hookwise.greeting import select_quote

        result = select_quote([], {"mindset": 1.0})
        assert result is None

    def test_select_quote_weighted_category(self):
        """Weighted selection favors the higher-weighted category."""
        from hookwise.greeting import select_quote

        quotes = [
            {"text": "A", "attribution": "X", "category": "alpha"},
            {"text": "B", "attribution": "Y", "category": "beta"},
        ]
        # Give alpha 99% weight
        weights = {"alpha": 0.99, "beta": 0.01}

        alpha_count = 0
        rng = random.Random(12345)
        for _ in range(100):
            q = select_quote(quotes, weights, rng=rng)
            if q and q["category"] == "alpha":
                alpha_count += 1

        # Alpha should dominate
        assert alpha_count > 70

    def test_select_quote_unknown_category_gets_default_weight(self):
        """Quotes with categories not in weights still have a chance."""
        from hookwise.greeting import select_quote

        quotes = [
            {"text": "A", "attribution": "X", "category": "unknown_cat"},
        ]
        weights = {"mindset": 1.0}
        rng = random.Random(42)
        quote = select_quote(quotes, weights, rng=rng)
        assert quote is not None
        assert quote["text"] == "A"

    def test_load_quotes_file_valid(self, tmp_path: Path):
        """Loads quotes from a valid JSON file."""
        from hookwise.greeting import load_quotes_file

        quotes_data = [
            {"text": "Custom quote", "attribution": "Author", "category": "custom"},
            {"text": "Another quote"},
        ]
        qfile = tmp_path / "quotes.json"
        qfile.write_text(json.dumps(quotes_data))

        loaded = load_quotes_file(str(qfile))
        assert len(loaded) == 2
        assert loaded[0]["text"] == "Custom quote"
        assert loaded[1]["attribution"] == "Unknown"
        assert loaded[1]["category"] == "custom"

    def test_load_quotes_file_missing(self):
        """Returns empty list for missing file."""
        from hookwise.greeting import load_quotes_file

        result = load_quotes_file("/nonexistent/path/quotes.json")
        assert result == []

    def test_load_quotes_file_invalid_json(self, tmp_path: Path):
        """Returns empty list for malformed JSON."""
        from hookwise.greeting import load_quotes_file

        bad_file = tmp_path / "bad.json"
        bad_file.write_text("not valid json{{{")
        result = load_quotes_file(str(bad_file))
        assert result == []

    def test_load_quotes_file_not_a_list(self, tmp_path: Path):
        """Returns empty list when JSON is a dict instead of list."""
        from hookwise.greeting import load_quotes_file

        bad_file = tmp_path / "dict.json"
        bad_file.write_text('{"not": "a list"}')
        result = load_quotes_file(str(bad_file))
        assert result == []

    def test_load_quotes_file_filters_invalid_entries(self, tmp_path: Path):
        """Skips entries without a 'text' field."""
        from hookwise.greeting import load_quotes_file

        data = [
            {"text": "Valid"},
            {"attribution": "No text field"},
            "not a dict",
            {"text": ""},  # empty text
        ]
        qfile = tmp_path / "mixed.json"
        qfile.write_text(json.dumps(data))

        loaded = load_quotes_file(str(qfile))
        assert len(loaded) == 1
        assert loaded[0]["text"] == "Valid"


class TestGreetingFormat:
    """Tests for quote formatting."""

    def test_format_quote_with_attribution(self):
        """Formats quote with attribution."""
        from hookwise.greeting import format_quote

        result = format_quote({"text": "Hello world", "attribution": "Author"})
        assert result == '"Hello world" -- Author'

    def test_format_quote_without_attribution(self):
        """Formats quote without attribution."""
        from hookwise.greeting import format_quote

        result = format_quote({"text": "Just text", "attribution": ""})
        assert result == '"Just text"'


class TestGreetingHandle:
    """Tests for the greeting handle() entry point."""

    def test_only_runs_on_session_start(self):
        """Returns None for non-SessionStart events."""
        from hookwise.greeting import handle

        config = HooksConfig(greeting={"enabled": True})
        assert handle("PreToolUse", {}, config) is None
        assert handle("PostToolUse", {}, config) is None
        assert handle("Stop", {}, config) is None

    def test_returns_additional_context_on_session_start(self):
        """Returns additionalContext with quote on SessionStart."""
        from hookwise.greeting import handle

        config = HooksConfig(greeting={"enabled": True})
        result = handle("SessionStart", {}, config)
        assert result is not None
        assert "additionalContext" in result
        assert len(result["additionalContext"]) > 0

    def test_disabled_returns_none(self):
        """Returns None when greeting is disabled."""
        from hookwise.greeting import handle

        config = HooksConfig(greeting={"enabled": False})
        result = handle("SessionStart", {}, config)
        assert result is None

    def test_emits_to_stderr(self, capsys):
        """Prints quote to stderr for terminal display."""
        from hookwise.greeting import handle

        config = HooksConfig(greeting={"enabled": True})
        handle("SessionStart", {}, config)
        captured = capsys.readouterr()
        assert len(captured.err) > 0
        assert '"' in captured.err  # Quote marks

    def test_custom_additional_context(self):
        """Uses custom additional_context from config."""
        from hookwise.greeting import handle

        config = HooksConfig(greeting={
            "enabled": True,
            "additional_context": "Custom instruction for LLM.",
        })
        result = handle("SessionStart", {}, config)
        assert result is not None
        assert "Custom instruction for LLM." in result["additionalContext"]

    def test_loads_custom_quotes_file(self, tmp_path: Path):
        """Loads and uses quotes from custom file."""
        from hookwise.greeting import handle

        custom_quotes = [
            {"text": "Custom wisdom", "attribution": "Me", "category": "custom"},
        ]
        qfile = tmp_path / "my_quotes.json"
        qfile.write_text(json.dumps(custom_quotes))

        config = HooksConfig(greeting={
            "enabled": True,
            "quotes_file": str(qfile),
            "category_weights": {"custom": 1.0},
        })

        # Run many times to ensure custom quotes are used at least once
        found_custom = False
        for _ in range(50):
            result = handle("SessionStart", {}, config)
            if result and "Custom wisdom" in result["additionalContext"]:
                found_custom = True
                break
        assert found_custom

    def test_empty_config_uses_defaults(self):
        """Works with empty greeting config."""
        from hookwise.greeting import handle

        config = HooksConfig(greeting={})
        result = handle("SessionStart", {}, config)
        assert result is not None
        assert "additionalContext" in result

    def test_invalid_config_type_handled(self):
        """Handles non-dict greeting config gracefully."""
        from hookwise.greeting import handle

        config = HooksConfig()
        config.greeting = "not a dict"
        result = handle("SessionStart", {}, config)
        # Should still work with defaults
        assert result is not None


# =========================================================================
# Task 8.2: Sounds handler tests
# =========================================================================


class TestSoundsPlayback:
    """Tests for sound playback logic."""

    @patch("hookwise.sounds.subprocess.Popen")
    def test_play_sound_calls_subprocess(self, mock_popen, tmp_path: Path):
        """play_sound launches a subprocess with the right command."""
        from hookwise.sounds import play_sound

        sound_file = tmp_path / "test.aiff"
        sound_file.touch()

        result = play_sound(str(sound_file), volume=2, command_template="afplay -v {volume} {file}")
        assert result is True
        mock_popen.assert_called_once()
        call_args = mock_popen.call_args
        cmd = call_args[0][0]
        assert "afplay" in cmd
        assert str(sound_file) in cmd
        assert "2" in cmd

    @patch("hookwise.sounds.subprocess.Popen")
    def test_play_sound_missing_file_returns_false(self, mock_popen):
        """play_sound returns False for missing sound file."""
        from hookwise.sounds import play_sound

        result = play_sound("/nonexistent/sound.aiff")
        assert result is False
        mock_popen.assert_not_called()

    @patch("hookwise.sounds.subprocess.Popen")
    def test_play_sound_empty_path_returns_false(self, mock_popen):
        """play_sound returns False for empty file path."""
        from hookwise.sounds import play_sound

        result = play_sound("")
        assert result is False
        mock_popen.assert_not_called()

    @patch("hookwise.sounds.subprocess.Popen", side_effect=OSError("audio fail"))
    def test_play_sound_exception_returns_false(self, mock_popen, tmp_path: Path):
        """play_sound returns False on subprocess error."""
        from hookwise.sounds import play_sound

        sound_file = tmp_path / "test.aiff"
        sound_file.touch()

        result = play_sound(str(sound_file))
        assert result is False

    def test_get_default_command_returns_string(self):
        """get_default_command returns a non-empty string."""
        from hookwise.sounds import get_default_command

        cmd = get_default_command()
        assert isinstance(cmd, str)
        assert len(cmd) > 0
        assert "{file}" in cmd


class TestSoundsHandle:
    """Tests for the sounds handle() entry point."""

    def test_only_runs_on_notification_and_stop(self):
        """Returns None for unsupported events."""
        from hookwise.sounds import handle

        config = HooksConfig(sounds={"enabled": True})
        assert handle("SessionStart", {}, config) is None
        assert handle("PreToolUse", {}, config) is None
        assert handle("PostToolUse", {}, config) is None

    @patch("hookwise.sounds.play_sound")
    def test_notification_plays_notification_sound(self, mock_play):
        """Plays notification sound on Notification event."""
        from hookwise.sounds import handle

        config = HooksConfig(sounds={
            "enabled": True,
            "notification": {"file": "/System/Library/Sounds/Glass.aiff", "volume": 2},
        })
        handle("Notification", {}, config)
        mock_play.assert_called_once_with(
            "/System/Library/Sounds/Glass.aiff",
            volume=2,
            command_template=None,
        )

    @patch("hookwise.sounds.play_sound")
    def test_stop_plays_completion_sound(self, mock_play):
        """Plays completion sound on Stop event."""
        from hookwise.sounds import handle

        config = HooksConfig(sounds={
            "enabled": True,
            "completion": {"file": "/System/Library/Sounds/Hero.aiff", "volume": 3},
        })
        handle("Stop", {}, config)
        mock_play.assert_called_once_with(
            "/System/Library/Sounds/Hero.aiff",
            volume=3,
            command_template=None,
        )

    @patch("hookwise.sounds.play_sound")
    def test_custom_command_template(self, mock_play):
        """Passes custom command template from config."""
        from hookwise.sounds import handle

        config = HooksConfig(sounds={
            "enabled": True,
            "command": "custom_player {file} --vol {volume}",
            "notification": {"file": "/path/to/sound.wav", "volume": 5},
        })
        handle("Notification", {}, config)
        mock_play.assert_called_once_with(
            "/path/to/sound.wav",
            volume=5,
            command_template="custom_player {file} --vol {volume}",
        )

    @patch("hookwise.sounds.play_sound")
    def test_disabled_does_not_play(self, mock_play):
        """Does not play when disabled."""
        from hookwise.sounds import handle

        config = HooksConfig(sounds={
            "enabled": False,
            "notification": {"file": "/sound.aiff"},
        })
        handle("Notification", {}, config)
        mock_play.assert_not_called()

    @patch("hookwise.sounds.play_sound")
    def test_missing_sound_config_is_safe(self, mock_play):
        """Handles missing notification/completion config gracefully."""
        from hookwise.sounds import handle

        config = HooksConfig(sounds={"enabled": True})
        handle("Notification", {}, config)
        mock_play.assert_not_called()

    @patch("hookwise.sounds.play_sound")
    def test_empty_file_does_not_play(self, mock_play):
        """Does not play when file path is empty."""
        from hookwise.sounds import handle

        config = HooksConfig(sounds={
            "enabled": True,
            "notification": {"file": "", "volume": 1},
        })
        handle("Notification", {}, config)
        mock_play.assert_not_called()


# =========================================================================
# Task 8.3: Transcript backup handler tests
# =========================================================================


class TestTranscriptBackupDir:
    """Tests for backup directory management."""

    def test_get_backup_dir_default(self):
        """Returns default path when no config."""
        from hookwise.transcript import get_backup_dir

        result = get_backup_dir()
        assert "backups" in str(result)

    def test_get_backup_dir_custom(self):
        """Returns custom path from config."""
        from hookwise.transcript import get_backup_dir

        result = get_backup_dir("/custom/backups")
        assert str(result) == "/custom/backups"

    def test_get_dir_size(self, tmp_path: Path):
        """Calculates directory size correctly."""
        from hookwise.transcript import get_dir_size

        (tmp_path / "a.json").write_text("x" * 100)
        (tmp_path / "b.json").write_text("y" * 200)

        size = get_dir_size(tmp_path)
        assert size == 300

    def test_get_dir_size_empty(self, tmp_path: Path):
        """Returns 0 for empty directory."""
        from hookwise.transcript import get_dir_size

        assert get_dir_size(tmp_path) == 0

    def test_get_dir_size_nonexistent(self):
        """Returns 0 for nonexistent directory."""
        from hookwise.transcript import get_dir_size

        assert get_dir_size(Path("/nonexistent/dir")) == 0


class TestTranscriptSizeEnforcement:
    """Tests for backup size limit enforcement."""

    def test_enforce_size_limit_under_limit(self, tmp_path: Path):
        """No deletions when under limit."""
        from hookwise.transcript import enforce_size_limit

        (tmp_path / "a.json").write_text("x" * 50)
        deleted = enforce_size_limit(tmp_path, 1000)
        assert deleted == 0
        assert (tmp_path / "a.json").exists()

    def test_enforce_size_limit_deletes_oldest(self, tmp_path: Path):
        """Deletes oldest files first when over limit."""
        from hookwise.transcript import enforce_size_limit

        # Create files with different modification times
        old = tmp_path / "old.json"
        old.write_text("x" * 100)
        # Ensure different mtime
        os.utime(old, (time.time() - 100, time.time() - 100))

        new = tmp_path / "new.json"
        new.write_text("y" * 100)

        # Limit to 150 bytes -- must delete one file
        deleted = enforce_size_limit(tmp_path, 150)
        assert deleted == 1
        assert not old.exists()
        assert new.exists()

    def test_enforce_size_limit_deletes_multiple(self, tmp_path: Path):
        """Deletes multiple files to get under limit."""
        from hookwise.transcript import enforce_size_limit

        for i in range(5):
            f = tmp_path / f"backup_{i}.json"
            f.write_text("x" * 100)
            os.utime(f, (time.time() - (50 - i * 10), time.time() - (50 - i * 10)))

        # 500 bytes total, limit to 200 -- must delete 3 files
        deleted = enforce_size_limit(tmp_path, 200)
        assert deleted >= 3


class TestTranscriptWriteBackup:
    """Tests for atomic backup writing."""

    def test_write_backup_creates_file(self, tmp_path: Path):
        """Creates a backup file with correct content."""
        from hookwise.transcript import write_backup

        backup_dir = tmp_path / "backups"
        result = write_backup(
            backup_dir=backup_dir,
            content="Hello transcript",
            session_id="sess-123",
            timestamp="20260210T120000Z",
        )
        assert result is not None
        assert result.exists()
        data = json.loads(result.read_text())
        assert data["content"] == "Hello transcript"
        assert data["session_id"] == "sess-123"

    def test_write_backup_filename_format(self, tmp_path: Path):
        """Backup filename includes timestamp and session_id."""
        from hookwise.transcript import write_backup

        result = write_backup(
            backup_dir=tmp_path,
            content="test",
            session_id="abc-123",
            timestamp="20260210T120000Z",
        )
        assert result is not None
        assert "transcript_" in result.name
        assert "abc-123" in result.name
        assert ".json" in result.name

    def test_write_backup_creates_directory(self, tmp_path: Path):
        """Creates backup directory if it doesn't exist."""
        from hookwise.transcript import write_backup

        deep_dir = tmp_path / "a" / "b" / "c"
        result = write_backup(deep_dir, "content", "sess")
        assert result is not None
        assert deep_dir.is_dir()

    def test_write_backup_sanitizes_session_id(self, tmp_path: Path):
        """Sanitizes special characters from session_id in filename."""
        from hookwise.transcript import write_backup

        result = write_backup(
            tmp_path, "content", "sess/../../malicious", "20260210T000000Z",
        )
        assert result is not None
        assert "/" not in result.name
        assert ".." not in result.name

    def test_write_backup_atomic(self, tmp_path: Path):
        """No temp files remain after successful write."""
        from hookwise.transcript import write_backup

        write_backup(tmp_path, "content", "sess")
        files = list(tmp_path.iterdir())
        assert all(not f.name.startswith(".") for f in files)
        assert all(not f.name.endswith(".tmp") for f in files)


class TestTranscriptHandle:
    """Tests for the transcript handle() entry point."""

    def test_only_runs_on_precompact(self, tmp_state_dir):
        """Returns None for non-PreCompact events."""
        from hookwise.transcript import handle

        config = HooksConfig(transcript_backup={"enabled": True})
        assert handle("SessionStart", {}, config) is None
        assert handle("Stop", {}, config) is None

    def test_backs_up_transcript_content(self, tmp_path: Path):
        """Saves transcript content on PreCompact."""
        from hookwise.transcript import handle

        backup_dir = tmp_path / "backups"
        config = HooksConfig(transcript_backup={
            "enabled": True,
            "backup_dir": str(backup_dir),
            "max_size_mb": 100,
        })
        payload = {
            "transcript": "This is the conversation transcript.",
            "session_id": "test-sess",
        }
        handle("PreCompact", payload, config)

        files = list(backup_dir.iterdir())
        assert len(files) == 1
        data = json.loads(files[0].read_text())
        assert data["content"] == "This is the conversation transcript."

    def test_handles_list_transcript(self, tmp_path: Path):
        """Serializes list transcript to JSON string."""
        from hookwise.transcript import handle

        backup_dir = tmp_path / "backups"
        config = HooksConfig(transcript_backup={
            "enabled": True,
            "backup_dir": str(backup_dir),
        })
        payload = {
            "transcript": [{"role": "user", "content": "Hello"}],
            "session_id": "sess",
        }
        handle("PreCompact", payload, config)

        files = list(backup_dir.iterdir())
        assert len(files) == 1

    def test_disabled_returns_none(self, tmp_path: Path):
        """Does not create backup when disabled."""
        from hookwise.transcript import handle

        backup_dir = tmp_path / "backups"
        config = HooksConfig(transcript_backup={
            "enabled": False,
            "backup_dir": str(backup_dir),
        })
        handle("PreCompact", {"transcript": "data"}, config)
        assert not backup_dir.exists()

    def test_empty_transcript_skipped(self, tmp_path: Path):
        """Does not create backup for empty transcript."""
        from hookwise.transcript import handle

        backup_dir = tmp_path / "backups"
        config = HooksConfig(transcript_backup={
            "enabled": True,
            "backup_dir": str(backup_dir),
        })
        handle("PreCompact", {}, config)
        if backup_dir.exists():
            assert len(list(backup_dir.iterdir())) == 0

    def test_enforces_size_limit(self, tmp_path: Path):
        """Deletes old backups when size limit exceeded."""
        from hookwise.transcript import handle

        backup_dir = tmp_path / "backups"
        backup_dir.mkdir()

        # Create existing large backups
        for i in range(5):
            f = backup_dir / f"transcript_2026010{i}T000000Z_old.json"
            f.write_text("x" * 500_000)  # 500KB each = 2.5MB total
            os.utime(f, (time.time() - (50 - i * 10), time.time() - (50 - i * 10)))

        config = HooksConfig(transcript_backup={
            "enabled": True,
            "backup_dir": str(backup_dir),
            "max_size_mb": 1,  # 1MB limit
        })
        payload = {"transcript": "new data", "session_id": "new"}
        handle("PreCompact", payload, config)

        # Some old files should have been deleted
        remaining = list(backup_dir.iterdir())
        total_size = sum(f.stat().st_size for f in remaining)
        # The new backup should exist and total should be manageable
        assert any("new" in f.name for f in remaining)

    def test_uses_content_fallback(self, tmp_path: Path):
        """Falls back to 'content' key if 'transcript' not in payload."""
        from hookwise.transcript import handle

        backup_dir = tmp_path / "backups"
        config = HooksConfig(transcript_backup={
            "enabled": True,
            "backup_dir": str(backup_dir),
        })
        payload = {"content": "Fallback content", "session_id": "sess"}
        handle("PreCompact", payload, config)

        files = list(backup_dir.iterdir())
        assert len(files) == 1
        data = json.loads(files[0].read_text())
        assert data["content"] == "Fallback content"


# =========================================================================
# Task 8.4: Multi-agent observability handler tests
# =========================================================================


class TestAgentTreeManagement:
    """Tests for agent tree data operations."""

    def test_record_agent_start(self):
        """Records a new agent in the tree."""
        from hookwise.agents import record_agent_start

        tree = {"agents": {}, "edges": [], "file_owners": {}}
        record = record_agent_start(
            tree, "agent-1", "main", "Explore", "Search codebase",
            timestamp="2026-02-10T12:00:00Z",
        )
        assert record["agent_id"] == "agent-1"
        assert record["parent_id"] == "main"
        assert record["agent_type"] == "Explore"
        assert record["task"] == "Search codebase"
        assert record["status"] == "running"
        assert tree["agents"]["agent-1"] == record
        assert ["main", "agent-1"] in tree["edges"]

    def test_record_agent_start_duplicate_edge(self):
        """Does not duplicate edges on repeated start calls."""
        from hookwise.agents import record_agent_start

        tree = {"agents": {}, "edges": [], "file_owners": {}}
        record_agent_start(tree, "agent-1", "main", "Explore", "task1")
        record_agent_start(tree, "agent-1", "main", "Explore", "task2")
        edge_count = sum(1 for e in tree["edges"] if e == ["main", "agent-1"])
        assert edge_count == 1

    def test_record_agent_stop_updates_status(self):
        """Stop updates status, timestamp, and files."""
        from hookwise.agents import record_agent_start, record_agent_stop

        tree = {"agents": {}, "edges": [], "file_owners": {}}
        record_agent_start(tree, "agent-1", "main", "Explore", "task")

        agent, conflicts = record_agent_stop(
            tree, "agent-1", status="completed",
            files_modified=["/path/a.py"],
            timestamp="2026-02-10T12:05:00Z",
        )
        assert agent is not None
        assert agent["status"] == "completed"
        assert agent["stopped_at"] == "2026-02-10T12:05:00Z"
        assert agent["files_modified"] == ["/path/a.py"]
        assert conflicts == []

    def test_record_agent_stop_unknown_agent(self):
        """Returns None for unknown agent ID."""
        from hookwise.agents import record_agent_stop

        tree = {"agents": {}, "edges": [], "file_owners": {}}
        agent, conflicts = record_agent_stop(tree, "unknown-agent")
        assert agent is None
        assert conflicts == []


class TestAgentFileConflicts:
    """Tests for file conflict detection."""

    def test_no_conflict_single_agent(self):
        """No conflict when only one agent modifies a file."""
        from hookwise.agents import detect_file_conflicts

        tree = {"file_owners": {}}
        conflicts = detect_file_conflicts(tree, "agent-1", ["/path/a.py"])
        assert conflicts == []

    def test_conflict_detected(self):
        """Detects conflict when two agents modify the same file."""
        from hookwise.agents import detect_file_conflicts

        tree = {"file_owners": {"/path/a.py": ["agent-1"]}}
        conflicts = detect_file_conflicts(tree, "agent-2", ["/path/a.py"])
        assert len(conflicts) == 1
        assert conflicts[0]["file"] == "/path/a.py"
        assert "agent-1" in conflicts[0]["agents"]
        assert "agent-2" in conflicts[0]["agents"]

    def test_multiple_conflicts(self):
        """Detects multiple file conflicts."""
        from hookwise.agents import detect_file_conflicts

        tree = {"file_owners": {
            "/path/a.py": ["agent-1"],
            "/path/b.py": ["agent-1"],
        }}
        conflicts = detect_file_conflicts(
            tree, "agent-2", ["/path/a.py", "/path/b.py", "/path/c.py"],
        )
        assert len(conflicts) == 2

    def test_record_stop_detects_conflict(self):
        """record_agent_stop returns conflicts list."""
        from hookwise.agents import record_agent_start, record_agent_stop

        tree = {"agents": {}, "edges": [], "file_owners": {}}
        record_agent_start(tree, "agent-1", "main", "Explore", "task1")
        record_agent_stop(tree, "agent-1", files_modified=["/path/a.py"])

        record_agent_start(tree, "agent-2", "main", "Explore", "task2")
        _, conflicts = record_agent_stop(
            tree, "agent-2", files_modified=["/path/a.py"],
        )
        assert len(conflicts) == 1


class TestAgentMermaidDiagram:
    """Tests for Mermaid diagram generation."""

    def test_empty_tree(self):
        """Generates placeholder for empty tree."""
        from hookwise.agents import generate_mermaid_diagram

        diagram = generate_mermaid_diagram({"agents": {}, "edges": []})
        assert "graph TD" in diagram
        assert "No agents observed" in diagram

    def test_single_agent(self):
        """Generates diagram with one agent node."""
        from hookwise.agents import generate_mermaid_diagram

        tree = {
            "agents": {
                "agent1": {
                    "agent_id": "agent1",
                    "agent_type": "Explore",
                    "task": "Search codebase",
                    "parent_id": "main",
                },
            },
            "edges": [["main", "agent1"]],
        }
        diagram = generate_mermaid_diagram(tree)
        assert "graph TD" in diagram
        assert "Explore" in diagram
        assert "-->" in diagram

    def test_multi_level_tree(self):
        """Generates diagram with parent and child agents."""
        from hookwise.agents import generate_mermaid_diagram

        tree = {
            "agents": {
                "agent1": {
                    "agent_id": "agent1",
                    "agent_type": "general-purpose",
                    "task": "Implement feature",
                    "parent_id": "main",
                },
                "agent2": {
                    "agent_id": "agent2",
                    "agent_type": "Explore",
                    "task": "Read files",
                    "parent_id": "agent1",
                },
            },
            "edges": [["main", "agent1"], ["agent1", "agent2"]],
        }
        diagram = generate_mermaid_diagram(tree)
        assert "graph TD" in diagram
        assert "general-purpose" in diagram
        assert "Explore" in diagram
        lines = diagram.split("\n")
        arrow_lines = [l for l in lines if "-->" in l]
        assert len(arrow_lines) == 2


class TestAgentHandle:
    """Tests for the agents handle() entry point."""

    def test_only_runs_on_subagent_events(self, tmp_state_dir):
        """Returns None for non-subagent events."""
        from hookwise.agents import handle

        config = HooksConfig()
        assert handle("SessionStart", {}, config) is None
        assert handle("PreToolUse", {}, config) is None

    def test_subagent_start_records(self, tmp_state_dir):
        """Records agent on SubagentStart."""
        from hookwise.agents import handle, load_agent_tree

        config = HooksConfig()
        payload = {
            "agent_id": "test-agent",
            "parent_id": "main",
            "agent_type": "Explore",
            "task": "Find files",
        }
        handle("SubagentStart", payload, config)

        tree = load_agent_tree()
        assert "test-agent" in tree["agents"]
        assert tree["agents"]["test-agent"]["agent_type"] == "Explore"

    def test_subagent_stop_records(self, tmp_state_dir):
        """Records completion on SubagentStop."""
        from hookwise.agents import handle, load_agent_tree

        config = HooksConfig()
        # Start first
        handle("SubagentStart", {
            "agent_id": "test-agent",
            "parent_id": "main",
            "agent_type": "Explore",
            "task": "task",
        }, config)

        # Then stop
        handle("SubagentStop", {
            "agent_id": "test-agent",
            "status": "completed",
            "files_modified": ["/path/a.py"],
        }, config)

        tree = load_agent_tree()
        assert tree["agents"]["test-agent"]["status"] == "completed"
        assert tree["agents"]["test-agent"]["files_modified"] == ["/path/a.py"]

    def test_missing_agent_id_skipped(self, tmp_state_dir):
        """Skips events with no agent_id."""
        from hookwise.agents import handle

        config = HooksConfig()
        result = handle("SubagentStart", {}, config)
        assert result is None

    def test_file_conflict_warning(self, tmp_state_dir, capsys):
        """Emits warning to stderr on file conflicts."""
        from hookwise.agents import handle

        config = HooksConfig()

        # Agent 1 modifies a file
        handle("SubagentStart", {
            "agent_id": "agent1", "parent_id": "main",
            "agent_type": "Explore", "task": "t1",
        }, config)
        handle("SubagentStop", {
            "agent_id": "agent1", "status": "completed",
            "files_modified": ["/path/shared.py"],
        }, config)

        # Agent 2 modifies the same file
        handle("SubagentStart", {
            "agent_id": "agent2", "parent_id": "main",
            "agent_type": "Explore", "task": "t2",
        }, config)
        handle("SubagentStop", {
            "agent_id": "agent2", "status": "completed",
            "files_modified": ["/path/shared.py"],
        }, config)

        captured = capsys.readouterr()
        assert "FILE CONFLICT" in captured.err
        assert "shared.py" in captured.err


# =========================================================================
# Task 8.5: Cost estimation and logging tests
# =========================================================================


class TestCostEstimation:
    """Tests for cost estimation logic."""

    def test_estimate_tokens_string(self):
        """Estimates tokens from string content."""
        from hookwise.cost import estimate_tokens

        # 40 chars / 4 chars per token = 10 tokens
        tokens = estimate_tokens("x" * 40)
        assert tokens == 10

    def test_estimate_tokens_dict(self):
        """Estimates tokens from dict (serialized to JSON)."""
        from hookwise.cost import estimate_tokens

        data = {"key": "value", "number": 42}
        tokens = estimate_tokens(data)
        assert tokens > 0

    def test_estimate_tokens_none(self):
        """Returns 0 for None input."""
        from hookwise.cost import estimate_tokens

        assert estimate_tokens(None) == 0

    def test_estimate_tokens_minimum_one(self):
        """Returns at least 1 token for non-empty input."""
        from hookwise.cost import estimate_tokens

        assert estimate_tokens("a") >= 1

    def test_estimate_cost_basic(self):
        """Computes cost from token counts and rate."""
        from hookwise.cost import estimate_cost

        # 1000 input + 500 output = 1500 tokens
        # sonnet rate: 0.003 per 1K tokens
        cost = estimate_cost(1000, 500, "sonnet", {"sonnet": 0.003})
        assert abs(cost - 0.0045) < 0.0001

    def test_estimate_cost_different_models(self):
        """Different models have different rates."""
        from hookwise.cost import estimate_cost

        rates = {"haiku": 0.001, "sonnet": 0.003, "opus": 0.015}
        haiku_cost = estimate_cost(1000, 0, "haiku", rates)
        opus_cost = estimate_cost(1000, 0, "opus", rates)
        assert opus_cost > haiku_cost

    def test_estimate_cost_unknown_model_uses_sonnet(self):
        """Unknown model falls back to sonnet rate."""
        from hookwise.cost import estimate_cost

        rates = {"sonnet": 0.003}
        cost = estimate_cost(1000, 0, "unknown_model", rates)
        assert abs(cost - 0.003) < 0.0001

    def test_detect_model_opus(self):
        """Detects opus from payload model field."""
        from hookwise.cost import detect_model

        assert detect_model({"model": "claude-opus-4"}) == "opus"

    def test_detect_model_sonnet(self):
        """Detects sonnet from payload model field."""
        from hookwise.cost import detect_model

        assert detect_model({"model": "claude-sonnet-4"}) == "sonnet"

    def test_detect_model_haiku(self):
        """Detects haiku from payload model field."""
        from hookwise.cost import detect_model

        assert detect_model({"model": "claude-haiku-3.5"}) == "haiku"

    def test_detect_model_default(self):
        """Defaults to sonnet when model not recognized."""
        from hookwise.cost import detect_model

        assert detect_model({}) == "sonnet"
        assert detect_model({"model": ""}) == "sonnet"


class TestCostAccumulation:
    """Tests for cost accumulation and daily tracking."""

    def test_accumulate_cost_adds_to_daily(self):
        """accumulate_cost increases daily total."""
        from hookwise.cost import accumulate_cost

        today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
        state = {"today": today, "total_today": 1.0, "session_costs": {}}
        new_total = accumulate_cost(state, "sess-1", 0.5)
        assert new_total == 1.5

    def test_accumulate_cost_tracks_per_session(self):
        """accumulate_cost tracks per-session costs."""
        from hookwise.cost import accumulate_cost

        today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
        state = {"today": today, "total_today": 0.0, "session_costs": {}}
        accumulate_cost(state, "sess-1", 0.5)
        accumulate_cost(state, "sess-1", 0.3)
        accumulate_cost(state, "sess-2", 0.1)
        assert abs(state["session_costs"]["sess-1"] - 0.8) < 0.001
        assert abs(state["session_costs"]["sess-2"] - 0.1) < 0.001

    def test_reset_daily_on_new_day(self):
        """Resets daily total when date changes."""
        from hookwise.cost import accumulate_cost

        state = {"today": "2020-01-01", "total_today": 99.0, "session_costs": {}}
        new_total = accumulate_cost(state, "sess-1", 0.5)
        assert new_total == 0.5  # reset + 0.5


# =========================================================================
# Task 8.6: Budget enforcement tests
# =========================================================================


class TestBudgetEnforcement:
    """Tests for daily budget checking."""

    def test_within_budget_returns_none(self):
        """Returns None when within budget."""
        from hookwise.cost import check_budget

        today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
        state = {"today": today, "total_today": 5.0}
        result = check_budget(state, daily_budget=10.0, enforcement="enforce")
        assert result is None

    def test_over_budget_warn_mode(self, capsys):
        """Warn mode: emits stderr warning, returns None."""
        from hookwise.cost import check_budget

        today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
        state = {"today": today, "total_today": 15.0}
        result = check_budget(state, daily_budget=10.0, enforcement="warn")
        assert result is None  # Does not block
        captured = capsys.readouterr()
        assert "exceeded" in captured.err
        assert "WARNING" in captured.err

    def test_over_budget_enforce_mode(self):
        """Enforce mode: returns block decision."""
        from hookwise.cost import check_budget

        today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
        state = {"today": today, "total_today": 15.0}
        result = check_budget(state, daily_budget=10.0, enforcement="enforce")
        assert result is not None
        assert result["decision"] == "block"
        assert "exceeded" in result["reason"]
        assert "approximate" in result["reason"]

    def test_exactly_at_budget_is_ok(self):
        """Returns None when exactly at budget (not over)."""
        from hookwise.cost import check_budget

        today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
        state = {"today": today, "total_today": 10.0}
        result = check_budget(state, daily_budget=10.0, enforcement="enforce")
        assert result is None


class TestCostHandle:
    """Tests for the cost handle() entry point."""

    def test_disabled_returns_none(self, tmp_state_dir):
        """Returns None when cost tracking is disabled."""
        from hookwise.cost import handle

        config = HooksConfig(cost_tracking={"enabled": False})
        assert handle("PostToolUse", {}, config) is None

    def test_unsupported_event_returns_none(self, tmp_state_dir):
        """Returns None for unsupported event types."""
        from hookwise.cost import handle

        config = HooksConfig(cost_tracking={"enabled": True})
        assert handle("SessionStart", {}, config) is None
        assert handle("Notification", {}, config) is None

    def test_post_tool_use_accumulates_cost(self, tmp_state_dir):
        """Accumulates cost on PostToolUse events."""
        from hookwise.cost import handle, load_cost_state

        config = HooksConfig(cost_tracking={
            "enabled": True,
            "rates": {"sonnet": 0.003},
        })
        payload = {
            "session_id": "sess-1",
            "tool_name": "Write",
            "tool_input": {"file_path": "/a.py", "content": "x" * 400},
            "tool_output": {"result": "ok"},
            "model": "claude-sonnet-4",
        }
        handle("PostToolUse", payload, config)

        state = load_cost_state()
        assert state["total_today"] > 0
        assert state["session_costs"]["sess-1"] > 0

    def test_pretooluse_checks_budget(self, tmp_state_dir):
        """PreToolUse checks budget and may block."""
        from hookwise.cost import handle, save_cost_state

        today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
        save_cost_state({
            "today": today,
            "total_today": 20.0,
            "session_costs": {},
            "daily_costs": {},
        })

        config = HooksConfig(cost_tracking={
            "enabled": True,
            "daily_budget": 10.0,
            "enforcement": "enforce",
        })
        result = handle("PreToolUse", {}, config)
        assert result is not None
        assert result["decision"] == "block"

    def test_pretooluse_warn_mode(self, tmp_state_dir, capsys):
        """PreToolUse in warn mode emits warning but doesn't block."""
        from hookwise.cost import handle, save_cost_state

        today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
        save_cost_state({
            "today": today,
            "total_today": 20.0,
            "session_costs": {},
            "daily_costs": {},
        })

        config = HooksConfig(cost_tracking={
            "enabled": True,
            "daily_budget": 10.0,
            "enforcement": "warn",
        })
        result = handle("PreToolUse", {}, config)
        assert result is None  # Does not block
        captured = capsys.readouterr()
        assert "exceeded" in captured.err

    def test_stop_logs_summary(self, tmp_state_dir, capsys):
        """Stop event logs cost summary to stderr."""
        from hookwise.cost import handle, save_cost_state

        today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
        save_cost_state({
            "today": today,
            "total_today": 5.0,
            "session_costs": {"sess-1": 3.0},
            "daily_costs": {},
        })

        config = HooksConfig(cost_tracking={
            "enabled": True,
            "daily_budget": 10.0,
        })
        handle("Stop", {"session_id": "sess-1"}, config)
        captured = capsys.readouterr()
        assert "Session cost" in captured.err
        assert "$3.00" in captured.err
        assert "approximate" in captured.err.lower()

    def test_session_end_logs_summary(self, tmp_state_dir, capsys):
        """SessionEnd event logs cost summary to stderr."""
        from hookwise.cost import handle, save_cost_state

        today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
        save_cost_state({
            "today": today,
            "total_today": 2.0,
            "session_costs": {"sess-2": 2.0},
            "daily_costs": {},
        })

        config = HooksConfig(cost_tracking={
            "enabled": True,
            "daily_budget": 10.0,
        })
        handle("SessionEnd", {"session_id": "sess-2"}, config)
        captured = capsys.readouterr()
        assert "Session cost" in captured.err

    def test_default_enabled_is_false(self, tmp_state_dir):
        """Cost tracking is disabled by default (enabled: false)."""
        from hookwise.cost import handle

        config = HooksConfig(cost_tracking={})
        assert handle("PostToolUse", {"session_id": "s"}, config) is None

    def test_invalid_rates_uses_defaults(self, tmp_state_dir):
        """Falls back to default rates when config rates are invalid."""
        from hookwise.cost import handle, load_cost_state

        config = HooksConfig(cost_tracking={
            "enabled": True,
            "rates": "not a dict",
        })
        payload = {
            "session_id": "sess",
            "tool_input": {"data": "test"},
            "tool_output": {"result": "ok"},
        }
        handle("PostToolUse", payload, config)
        state = load_cost_state()
        assert state["total_today"] > 0

    def test_disclaimer_in_budget_block(self, tmp_state_dir):
        """Budget block includes the disclaimer."""
        from hookwise.cost import handle, save_cost_state

        today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
        save_cost_state({
            "today": today,
            "total_today": 20.0,
            "session_costs": {},
            "daily_costs": {},
        })

        config = HooksConfig(cost_tracking={
            "enabled": True,
            "daily_budget": 10.0,
            "enforcement": "enforce",
        })
        result = handle("PreToolUse", {}, config)
        assert "approximate" in result["reason"].lower()


# =========================================================================
# Cross-handler tests
# =========================================================================


class TestHandlerProtocol:
    """Verify all handlers follow the builtin handler protocol."""

    @pytest.mark.parametrize("module_name", [
        "hookwise.greeting",
        "hookwise.sounds",
        "hookwise.transcript",
        "hookwise.agents",
        "hookwise.cost",
    ])
    def test_handle_function_exists(self, module_name):
        """Each handler module exports a handle() function."""
        import importlib
        mod = importlib.import_module(module_name)
        assert hasattr(mod, "handle")
        assert callable(mod.handle)

    @pytest.mark.parametrize("module_name", [
        "hookwise.greeting",
        "hookwise.sounds",
        "hookwise.transcript",
        "hookwise.agents",
        "hookwise.cost",
    ])
    def test_handle_accepts_three_args(self, module_name):
        """Each handle() accepts (event_type, payload, config)."""
        import importlib
        import inspect
        mod = importlib.import_module(module_name)
        sig = inspect.signature(mod.handle)
        params = list(sig.parameters.keys())
        assert len(params) == 3
        assert params[0] == "event_type"
        assert params[1] == "payload"
        assert params[2] == "config"

    @pytest.mark.parametrize("module_name", [
        "hookwise.greeting",
        "hookwise.sounds",
        "hookwise.transcript",
        "hookwise.agents",
        "hookwise.cost",
    ])
    def test_handle_returns_dict_or_none(self, module_name, tmp_state_dir):
        """Each handle() returns dict or None for irrelevant events."""
        import importlib
        mod = importlib.import_module(module_name)
        config = HooksConfig(cost_tracking={"enabled": True})
        # Use an event type that should be irrelevant for most handlers
        result = mod.handle("Setup", {}, config)
        assert result is None or isinstance(result, dict)
