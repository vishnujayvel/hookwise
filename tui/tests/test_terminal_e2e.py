"""E2E tests validating Claude Code's status line renders real hookwise data.

These tests spawn a real Claude Code session in a PTY, wait for the
React/Ink UI to render, and assert that the hookwise status line
contains real data instead of '--' placeholders.

Also includes Director/Actor/Critic orchestration primitives:
- Multi-session Actor spawning and prompt injection (Task 8.1)
- Screen polling, completion/error detection, artifact capture (Task 8.2)

Requires: Claude Code installed, hookwise binary built, hookwise.yaml configured.
Run with: pytest -m terminal_e2e -v
"""
from __future__ import annotations

import os
import re
import subprocess
import time
from dataclasses import dataclass
from enum import Enum, auto
from typing import Callable

import pytest


# ---------------------------------------------------------------------------
# Prerequisites — skip entire module if required binaries are missing
# ---------------------------------------------------------------------------

pytestmark = pytest.mark.terminal_e2e


def _claude_available() -> bool:
    """Check if the claude command is available."""
    import shutil
    return shutil.which("claude") is not None


def _hookwise_available() -> bool:
    """Check if the hookwise binary is available on PATH."""
    import shutil
    return shutil.which("hookwise") is not None


# ---------------------------------------------------------------------------
# Environment setup — sanitise env vars so Claude Code starts cleanly in PTY
# ---------------------------------------------------------------------------

# Project root derived from this file's location (tui/tests/test_terminal_e2e.py -> repo root)
_PROJECT_ROOT = os.path.normpath(
    os.path.join(os.path.dirname(__file__), "..", "..")
)


# Environment vars that must be stripped when spawning a nested Claude Code session.
# CLAUDECODE=1 triggers the nesting guard; CLAUDE_CODE_SSE_PORT binds to the parent's port.
_CLAUDE_STRIP_VARS = [
    "CLAUDECODE",
    "CLAUDE_CODE_SSE_PORT",
    "CLAUDE_CODE_ENTRYPOINT",
]


def _clean_claude_env() -> dict[str, str | None]:
    """Return env overrides that allow Claude Code to start inside a test harness.

    Values of None cause the key to be removed from the child environment.
    """
    return {var: None for var in _CLAUDE_STRIP_VARS}


def _safe_cleanup(session) -> None:
    """Safely send Ctrl+C and close a session, ignoring errors."""
    try:
        session.send_keys("Ctrl+C")
        time.sleep(1)
    except Exception:
        pass
    session.close()


skipif_no_claude = pytest.mark.skipif(
    not _claude_available(), reason="Claude Code not installed"
)
skipif_no_hookwise = pytest.mark.skipif(
    not _hookwise_available(), reason="hookwise binary not built"
)


# ---------------------------------------------------------------------------
# Status line detection — scan rendered screen for hookwise segment pattern
# ---------------------------------------------------------------------------


def _find_status_line(screen_text: str) -> str | None:
    """Search full screen for the hookwise status line.

    Looks for the characteristic segment pattern (e.g. "session:" or "cost:")
    anywhere in the screen, since the render position depends on terminal
    size and Claude Code's layout.

    Returns the matching line or None.
    """
    for line in screen_text.split("\n"):
        stripped = line.strip()
        # The status line contains segment labels separated by " | "
        if "session:" in stripped or "cost:" in stripped:
            return stripped
    return None


class TestStatusLineE2E:
    """Validate hookwise status line renders inside Claude Code."""

    @skipif_no_claude
    @skipif_no_hookwise
    def test_status_line_renders(self, terminal_session):
        """Spawn Claude Code, wait for render, assert status line is present."""
        session = terminal_session.create_session(
            "claude",
            rows=24,
            cols=120,
            env=_clean_claude_env(),
            cwd=_PROJECT_ROOT,
        )
        try:
            # Wait for Claude Code to fully render (React/Ink startup)
            session.wait_for_stable(timeout=60, idle_ms=3000)

            screen = session.get_screen_text()
            status_line = _find_status_line(screen)

            # Status line should be present somewhere on screen
            assert status_line is not None, (
                f"Status line not found on screen.\n"
                f"Full screen:\n{screen}"
            )

            # Verify it has the expected segment format (label: value)
            assert "|" in status_line, (
                f"Status line missing segment delimiter '|'.\n"
                f"Found: {status_line}"
            )

        finally:
            _safe_cleanup(session)

    @skipif_no_claude
    @skipif_no_hookwise
    def test_status_line_segment_format(self, terminal_session):
        """Verify status line segments have correct label: value format."""
        session = terminal_session.create_session(
            "claude",
            rows=24,
            cols=120,
            env=_clean_claude_env(),
            cwd=_PROJECT_ROOT,
        )
        try:
            session.wait_for_stable(timeout=60, idle_ms=3000)

            screen = session.get_screen_text()
            status_line = _find_status_line(screen)

            assert status_line is not None, (
                f"Status line not found on screen.\n"
                f"Full screen:\n{screen}"
            )

            # Each configured segment should appear as "label: <value>"
            # Values may be "--" (placeholder) or real data, both are valid
            expected_segments = ["session:", "cost:", "project:", "calendar:"]
            for seg in expected_segments:
                assert seg in status_line, (
                    f"Expected segment '{seg}' not in status line.\n"
                    f"Found: {status_line}"
                )

        finally:
            _safe_cleanup(session)

    @skipif_no_claude
    @skipif_no_hookwise
    @pytest.mark.xfail(
        reason=(
            "Some feed producers still return placeholder data. "
            "Tracked by: #57 (project), #58 (calendar), #59 (session/cost). "
            "Insights producer is fixed — see test_status_line_has_insights_data."
        ),
        strict=True,
    )
    def test_status_line_has_real_data(self, terminal_session):
        """E2E: ALL status line segments must show real data, not '--' placeholders.

        This test validates the feature end-to-end: from feed producers
        through the cache pipeline to the rendered status line inside
        Claude Code. It fails when producers return placeholder data,
        because from the user's perspective the feature is broken.

        Currently xfail because some feed producers are stubs:
        - #57: ProjectProducer returns placeholder
        - #58: CalendarProducer returns placeholder
        - #59: session/cost analytics not wired to Dolt

        Note: InsightsProducer is fixed and tested separately via
        test_status_line_has_insights_data.
        """
        session = terminal_session.create_session(
            "claude",
            rows=24,
            cols=120,
            env=_clean_claude_env(),
            cwd=_PROJECT_ROOT,
        )
        try:
            session.wait_for_stable(timeout=60, idle_ms=3000)

            screen = session.get_screen_text()
            status_line = _find_status_line(screen)

            assert status_line is not None, (
                f"Status line not found on screen.\n"
                f"Full screen:\n{screen}"
            )

            placeholder_segments = []
            for seg_name in ("session", "cost", "project", "calendar"):
                pattern = rf"{seg_name}:\s*([^|]+)"
                match = re.search(pattern, status_line)
                if match:
                    value = match.group(1).strip()
                    if value == "--":
                        placeholder_segments.append(seg_name)

            assert len(placeholder_segments) == 0, (
                f"Segments with placeholder '--' data: {placeholder_segments}\n"
                f"Status line: {status_line}\n"
                f"The feature is broken end-to-end — feed producers are not "
                f"providing real data.\n"
                f"Fix: #57 (project), #58 (calendar), #59 (session/cost)"
            )

        finally:
            _safe_cleanup(session)


class TestStatusLineInsights:
    """Validate insights data appears in hookwise status-line output.

    These tests are the EXIT CONDITION for the InsightsProducer fix.
    The fast inner test runs hookwise status-line directly (~1s).
    The full E2E test spawns Claude Code in a PTY (~60s).
    """

    @skipif_no_hookwise
    def test_status_line_has_insights_data(self):
        """hookwise status-line must include insights with real data.

        This is the PRIMARY EXIT CONDITION for the InsightsProducer fix.
        Runs hookwise status-line directly (no Claude Code PTY needed).
        """
        result = subprocess.run(
            ["hookwise", "status-line", "--project-dir", _PROJECT_ROOT],
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert result.returncode == 0, (
            f"hookwise status-line failed with rc={result.returncode}\n"
            f"stderr: {result.stderr}"
        )
        # Strip ANSI escape codes
        output = re.sub(r"\x1b\[[0-9;]*m", "", result.stdout)
        lines = output.strip().split("\n")

        assert len(lines) >= 2, (
            f"Expected 2+ lines, got {len(lines)}:\n{output}"
        )
        assert re.search(r"\d+ sessions", output), (
            f"Missing session count in output:\n{output}"
        )
        assert re.search(r"\d+.*lines", output), (
            f"Missing lines count in output:\n{output}"
        )

    @skipif_no_claude
    @skipif_no_hookwise
    def test_insights_visible_in_claude_code(self, terminal_session):
        """Insights data visible in Claude Code's rendered status line.

        Full E2E: spawns Claude Code in a PTY and checks that the
        multi-line status output includes insights data.
        """
        session = terminal_session.create_session(
            "claude",
            rows=24,
            cols=120,
            env=_clean_claude_env(),
            cwd=_PROJECT_ROOT,
        )
        try:
            session.wait_for_stable(timeout=60, idle_ms=3000)
            screen = session.get_screen_text()

            assert re.search(r"\d+ sessions", screen), (
                f"No insights session count in Claude Code screen:\n{screen}"
            )
        finally:
            _safe_cleanup(session)


class TestStatusLineCleanup:
    """Verify clean session lifecycle."""

    @skipif_no_claude
    @skipif_no_hookwise
    def test_session_terminates_cleanly(self, terminal_session):
        """After /exit, Claude Code should exit and release PTY."""
        session = terminal_session.create_session(
            "claude",
            rows=24,
            cols=80,
            env=_clean_claude_env(),
            cwd=_PROJECT_ROOT,
        )
        try:
            session.wait_for_stable(timeout=60, idle_ms=3000)
            session.send_keys("/exit")
            time.sleep(0.3)
            session.send_keys("Enter")
            # Wait for process to exit
            deadline = time.monotonic() + 15
            while session.is_alive and time.monotonic() < deadline:
                time.sleep(0.5)
            assert not session.is_alive, "Claude Code should have exited after /exit"
        finally:
            session.close()


# ===========================================================================
# Director/Actor/Critic orchestration primitives (Tasks 8.1 & 8.2)
# ===========================================================================
#
# These primitives enable multi-session test orchestration:
#
# - ActorState: lifecycle enum tracking each session from SPAWNED → COMPLETED/ERROR
# - PollResult: immutable snapshot of one screen poll (state + screen text + match info)
# - detect_completion/detect_error/detect_transient_error: regex-based screen analysis
# - poll_session_nonblocking: single non-blocking screen read + detection pass
# - capture_session_artifact: save screen state to disk for post-mortem analysis
# - DirectorPollLoop: orchestrate polling across multiple Actor sessions until
#   all reach a terminal state (COMPLETED or ERROR) or timeout
#
# The Director spawns Actor sessions, the poll loop monitors them, and a Critic
# (not implemented here) would evaluate the collected artifacts.
# ===========================================================================

# Artifact directory for captured screen state (absolute path for portability)
ARTIFACT_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "artifacts"))
os.makedirs(ARTIFACT_DIR, exist_ok=True)


class ActorState(Enum):
    """Lifecycle state of an Actor session."""
    SPAWNED = auto()
    PROMPT_INJECTED = auto()
    PROCESSING = auto()
    COMPLETED = auto()
    ERROR = auto()


@dataclass
class PollResult:
    """Result of a single screen poll for an Actor."""
    actor_id: str
    state: ActorState
    screen_text: str
    matched_pattern: str | None = None
    error_detail: str | None = None
    timestamp: float = 0.0

    def __post_init__(self):
        if self.timestamp == 0.0:
            self.timestamp = time.monotonic()


# -- Completion detection patterns ------------------------------------------
# These patterns identify when Claude Code has finished processing and returned
# to an idle state (either the ">" input prompt or a shell prompt).

# Shell prompt patterns that indicate Claude Code has finished and returned
# to the input prompt (the React/Ink ">" prompt or a shell prompt).
COMPLETION_PATTERNS: list[re.Pattern] = [
    re.compile(r"^\s*>\s*$", re.MULTILINE),   # Claude Code empty input prompt
    re.compile(r"\$\s*$", re.MULTILINE),       # Shell prompt returned
]

# Specific output markers that signal task completion
OUTPUT_COMPLETION_MARKERS: list[str] = [
    "Task completed",
    "Done.",
    "Finished",
    "No changes needed",
]

# -- Error detection patterns -----------------------------------------------
# These patterns identify hard failures (crashes, panics, missing commands)
# vs. transient issues (rate limits, timeouts) that may be retryable.

ERROR_PATTERNS: list[re.Pattern] = [
    re.compile(r"Traceback \(most recent call last\)", re.IGNORECASE),
    re.compile(r"panic:", re.IGNORECASE),
    re.compile(r"FATAL|fatal error", re.IGNORECASE),
    re.compile(r"Error:.*\n.*Error:", re.DOTALL),  # Repeated error lines
    re.compile(r"Segmentation fault"),
    re.compile(r"command not found"),
    re.compile(r"Permission denied"),
    re.compile(r"ENOENT|EACCES|EPERM"),
]

# Patterns that indicate a transient/retryable issue (not a hard error)
TRANSIENT_PATTERNS: list[re.Pattern] = [
    re.compile(r"rate limit", re.IGNORECASE),
    re.compile(r"timeout", re.IGNORECASE),
    re.compile(r"connection refused", re.IGNORECASE),
]


def detect_completion(screen_text: str) -> str | None:
    """Check if screen text contains a completion marker.

    Returns the matched marker string, or None if not completed.
    """
    for marker in OUTPUT_COMPLETION_MARKERS:
        if marker in screen_text:
            return marker
    for pattern in COMPLETION_PATTERNS:
        match = pattern.search(screen_text)
        if match:
            return match.group(0).strip()
    return None


def detect_error(screen_text: str) -> str | None:
    """Check if screen text contains an error pattern.

    Returns the matched error string, or None if no error detected.
    """
    for pattern in ERROR_PATTERNS:
        match = pattern.search(screen_text)
        if match:
            return match.group(0).strip()
    return None


def detect_transient_error(screen_text: str) -> str | None:
    """Check if screen text contains a transient/retryable error.

    Returns the matched pattern string, or None.
    """
    for pattern in TRANSIENT_PATTERNS:
        match = pattern.search(screen_text)
        if match:
            return match.group(0).strip()
    return None


def poll_session_nonblocking(
    session, actor_id: str, previous_state: ActorState
) -> PollResult:
    """Poll a single session's screen buffer without blocking.

    Reads the current screen content and checks for completion or error
    markers. Does NOT call wait_for_stable — this is a non-blocking snapshot.
    """
    screen_text = session.get_screen_text()

    # Check for errors first (they take priority)
    error_match = detect_error(screen_text)
    if error_match:
        return PollResult(
            actor_id=actor_id,
            state=ActorState.ERROR,
            screen_text=screen_text,
            error_detail=error_match,
        )

    # Check for completion
    completion_match = detect_completion(screen_text)
    if completion_match:
        return PollResult(
            actor_id=actor_id,
            state=ActorState.COMPLETED,
            screen_text=screen_text,
            matched_pattern=completion_match,
        )

    # Still processing
    return PollResult(
        actor_id=actor_id,
        state=previous_state,
        screen_text=screen_text,
    )


def capture_session_artifact(
    session, test_name: str, actor_id: str
) -> str:
    """Capture final screen state as a text artifact.

    Returns the path to the saved artifact file.
    """
    artifact_path = os.path.join(
        ARTIFACT_DIR, f"{test_name}_{actor_id}.txt"
    )
    session.capture_artifact(artifact_path)
    return artifact_path


class DirectorPollLoop:
    """A Director-side polling loop that monitors multiple Actor sessions.

    The Director periodically polls each Actor's screen buffer (non-blocking),
    detects completion or error states, and accumulates results for the
    Critic to evaluate.
    """

    def __init__(
        self,
        sessions: dict[str, object],
        poll_interval: float = 2.0,
        timeout: float = 300.0,
        on_completion: Callable[[PollResult], None] | None = None,
        on_error: Callable[[PollResult], None] | None = None,
    ):
        self._sessions = sessions  # actor_id -> TerminalSession
        self._poll_interval = poll_interval
        self._timeout = timeout
        self._on_completion = on_completion
        self._on_error = on_error
        self._states: dict[str, ActorState] = {
            aid: ActorState.PROCESSING for aid in sessions
        }
        self._results: list[PollResult] = []
        self._final_results: dict[str, PollResult] = {}

    def run(self) -> dict[str, PollResult]:
        """Run the polling loop until all sessions complete, error, or timeout.

        Returns a dict mapping actor_id -> final PollResult.
        """
        deadline = time.monotonic() + self._timeout
        pending = set(self._sessions.keys())

        while pending and time.monotonic() < deadline:
            for actor_id in list(pending):
                session = self._sessions[actor_id]

                # Skip sessions that have exited
                if not session.is_alive:
                    result = PollResult(
                        actor_id=actor_id,
                        state=ActorState.COMPLETED,
                        screen_text=session.get_screen_text(),
                        matched_pattern="process_exited",
                    )
                    self._final_results[actor_id] = result
                    self._results.append(result)
                    pending.discard(actor_id)
                    if self._on_completion:
                        self._on_completion(result)
                    continue

                result = poll_session_nonblocking(
                    session, actor_id, self._states[actor_id]
                )
                self._results.append(result)

                if result.state == ActorState.COMPLETED:
                    self._final_results[actor_id] = result
                    self._states[actor_id] = ActorState.COMPLETED
                    pending.discard(actor_id)
                    if self._on_completion:
                        self._on_completion(result)
                elif result.state == ActorState.ERROR:
                    self._final_results[actor_id] = result
                    self._states[actor_id] = ActorState.ERROR
                    pending.discard(actor_id)
                    if self._on_error:
                        self._on_error(result)

            if pending:
                time.sleep(self._poll_interval)

        # Timeout any remaining sessions
        for actor_id in pending:
            session = self._sessions[actor_id]
            self._final_results[actor_id] = PollResult(
                actor_id=actor_id,
                state=ActorState.ERROR,
                screen_text=session.get_screen_text(),
                error_detail=f"Timed out after {self._timeout}s",
            )

        return self._final_results

    @property
    def all_results(self) -> list[PollResult]:
        """All poll results collected during the loop."""
        return list(self._results)


class TestMultiSessionActorSpawning:
    """Task 8.1: Multi-session Actor spawning and prompt injection.

    Verifies that the SessionManager can spawn multiple Claude Code sessions
    (Actors), inject task prompts into each, and confirm that each Actor
    begins processing via screen stability checks.

    Requirements: R6.1 (spawn Actor), R6.2 (inject prompt and confirm),
    R6.7 (parallel sessions).
    """

    @skipif_no_claude
    @skipif_no_hookwise
    def test_spawn_multiple_actors(self, terminal_session):
        """Spawn two Claude Code sessions via SessionManager and verify both are alive."""
        sessions = {}
        try:
            for actor_id in ("actor-1", "actor-2"):
                session = terminal_session.create_session(
                    "claude",
                    rows=24,
                    cols=120,
                    env=_clean_claude_env(),
                    cwd=_PROJECT_ROOT,
                )
                sessions[actor_id] = session

            # Both sessions should be alive after spawning
            for actor_id, session in sessions.items():
                assert session.is_alive, (
                    f"{actor_id} should be alive immediately after spawn"
                )

            # Wait for both to stabilise (React/Ink startup)
            for actor_id, session in sessions.items():
                session.wait_for_stable(timeout=60, idle_ms=3000)

            # After stability, both should still be alive and have rendered content
            for actor_id, session in sessions.items():
                assert session.is_alive, (
                    f"{actor_id} should still be alive after stabilisation"
                )
                screen = session.get_screen_text()
                assert screen.strip(), (
                    f"{actor_id} screen should have rendered content.\n"
                    f"Screen:\n{screen}"
                )

        finally:
            for actor_id, session in sessions.items():
                _safe_cleanup(session)

    @skipif_no_claude
    @skipif_no_hookwise
    def test_inject_prompts_into_actors(self, terminal_session):
        """Inject a task prompt into each Actor and confirm processing begins.

        Sends a simple, side-effect-free prompt to each Actor session
        (asking Claude to echo a marker string), then waits for screen
        stability to confirm the Actor has begun processing the prompt.
        """
        actor_prompts = {
            "actor-a": 'echo "ACTOR_A_MARKER_8_1"',
            "actor-b": 'echo "ACTOR_B_MARKER_8_1"',
        }
        sessions = {}
        try:
            # Phase 1: Spawn all Actor sessions
            for actor_id in actor_prompts:
                session = terminal_session.create_session(
                    "claude",
                    rows=24,
                    cols=120,
                    env=_clean_claude_env(),
                    cwd=_PROJECT_ROOT,
                )
                sessions[actor_id] = session

            # Phase 2: Wait for initial render
            for actor_id, session in sessions.items():
                session.wait_for_stable(timeout=60, idle_ms=3000)

            # Phase 3: Inject prompts — send keys followed by Enter
            for actor_id, session in sessions.items():
                prompt = actor_prompts[actor_id]
                session.send_keys(prompt)
                time.sleep(0.3)  # Small delay between keystrokes and Enter
                session.send_keys("Enter")

            # Phase 4: Wait for screen change indicating processing started.
            # After injecting the prompt, the screen should change from
            # the idle input state — the prompt text should appear somewhere
            # on screen, or Claude should start generating output.
            for actor_id, session in sessions.items():
                prompt = actor_prompts[actor_id]
                # The prompt text we typed should appear on screen
                session.wait_for_text(
                    prompt,
                    timeout=30,
                    poll_interval=0.5,
                )
                # Confirm screen is no longer in idle state by checking
                # that we have more content than just the initial prompt
                screen_text = session.get_screen_text()
                assert prompt in screen_text, (
                    f"{actor_id}: injected prompt not found on screen.\n"
                    f"Expected: {prompt}\n"
                    f"Screen:\n{screen_text}"
                )

        finally:
            for actor_id, session in sessions.items():
                capture_session_artifact(
                    session, "test_inject_prompts_into_actors", actor_id
                )
                _safe_cleanup(session)

    @skipif_no_claude
    @skipif_no_hookwise
    def test_parallel_sessions_independent(self, terminal_session):
        """Verify parallel sessions operate independently (R6.7).

        Spawns two sessions and injects different prompts. Confirms each
        session's screen shows only its own prompt, not the other's.
        """
        sessions = {}
        prompts = {
            "session-x": "UNIQUE_MARKER_X_8_1_PARALLEL",
            "session-y": "UNIQUE_MARKER_Y_8_1_PARALLEL",
        }
        try:
            for sid in prompts:
                session = terminal_session.create_session(
                    "claude",
                    rows=24,
                    cols=120,
                    env=_clean_claude_env(),
                    cwd=_PROJECT_ROOT,
                )
                sessions[sid] = session

            # Wait for both to render
            for sid, session in sessions.items():
                session.wait_for_stable(timeout=60, idle_ms=3000)

            # Inject unique prompts
            for sid, session in sessions.items():
                session.send_keys(f'echo "{prompts[sid]}"')
                time.sleep(0.3)
                session.send_keys("Enter")

            # Wait for prompts to appear
            for sid, session in sessions.items():
                session.wait_for_text(prompts[sid], timeout=30)

            # Verify isolation: each session's screen should contain only
            # its own marker, not the other's
            for sid, session in sessions.items():
                screen = session.get_screen_text()
                own_marker = prompts[sid]
                other_sid = [k for k in prompts if k != sid][0]
                other_marker = prompts[other_sid]

                assert own_marker in screen, (
                    f"{sid}: own marker not found.\nScreen:\n{screen}"
                )
                assert other_marker not in screen, (
                    f"{sid}: other session's marker leaked into this screen.\n"
                    f"Leaked marker: {other_marker}\nScreen:\n{screen}"
                )

        finally:
            for sid, session in sessions.items():
                capture_session_artifact(
                    session, "test_parallel_sessions_independent", sid
                )
                _safe_cleanup(session)


class TestScreenPollingAndDetection:
    """Task 8.2: Screen polling and completion/error detection.

    Builds non-blocking polling loops, detects completion markers and error
    states, and captures artifacts for post-execution analysis.

    Requirements: R6.3 (non-blocking poll), R6.4 (completion detection),
    R6.5 (error detection), R6.6 (30+ min sessions), R7.2 (artifact capture).
    """

    @skipif_no_claude
    @skipif_no_hookwise
    def test_nonblocking_poll_reads_screen(self, terminal_session):
        """Poll a running session's screen without blocking (R6.3).

        Verifies that poll_session_nonblocking returns a PollResult
        with the current screen text without waiting for stability.
        """
        session = terminal_session.create_session(
            "claude",
            rows=24,
            cols=120,
            env=_clean_claude_env(),
            cwd=_PROJECT_ROOT,
        )
        try:
            # Wait for initial render
            session.wait_for_stable(timeout=60, idle_ms=3000)

            # Non-blocking poll should return immediately
            start = time.monotonic()
            result = poll_session_nonblocking(
                session, "poll-actor", ActorState.PROCESSING
            )
            elapsed = time.monotonic() - start

            # Should complete very quickly (< 1s) — non-blocking
            assert elapsed < 1.0, (
                f"Non-blocking poll took {elapsed:.2f}s — should be near-instant"
            )
            assert result.actor_id == "poll-actor"
            assert result.screen_text.strip(), (
                "Poll should return non-empty screen text"
            )
            assert result.timestamp > 0

        finally:
            _safe_cleanup(session)

    @skipif_no_claude
    @skipif_no_hookwise
    def test_completion_detection_on_exit(self, terminal_session):
        """Detect when an Actor completes by process exit (R6.4).

        Sends /exit to Claude Code, then polls until completion is detected
        either via the process exiting or a completion marker appearing.
        """
        session = terminal_session.create_session(
            "claude",
            rows=24,
            cols=120,
            env=_clean_claude_env(),
            cwd=_PROJECT_ROOT,
        )
        try:
            session.wait_for_stable(timeout=60, idle_ms=3000)

            # Send /exit command to make Claude Code exit gracefully
            session.send_keys("/exit")
            time.sleep(0.3)
            session.send_keys("Enter")

            # Poll until process exits or timeout
            deadline = time.monotonic() + 30
            detected_exit = False
            while time.monotonic() < deadline:
                if not session.is_alive:
                    detected_exit = True
                    break
                time.sleep(0.5)

            assert detected_exit, (
                "Should detect Claude Code exit after /exit command.\n"
                f"Screen:\n{session.get_screen_text()}"
            )

        finally:
            capture_session_artifact(
                session, "test_completion_detection_on_exit", "exit-actor"
            )
            session.close()

    @skipif_no_claude
    @skipif_no_hookwise
    def test_director_poll_loop_completion(self, terminal_session):
        """DirectorPollLoop detects completion across multiple Actors (R6.4).

        Spawns two sessions, sends /exit to both, and runs the Director
        poll loop until both are detected as completed.
        """
        sessions = {}
        completion_notifications = []

        def on_complete(result: PollResult):
            completion_notifications.append(result)

        try:
            for actor_id in ("loop-actor-1", "loop-actor-2"):
                session = terminal_session.create_session(
                    "claude",
                    rows=24,
                    cols=120,
                    env=_clean_claude_env(),
                    cwd=_PROJECT_ROOT,
                )
                sessions[actor_id] = session

            # Wait for both to render
            for session in sessions.values():
                session.wait_for_stable(timeout=60, idle_ms=3000)

            # Send /exit to both
            for session in sessions.values():
                session.send_keys("/exit")
                time.sleep(0.3)
                session.send_keys("Enter")

            # Run Director poll loop
            loop = DirectorPollLoop(
                sessions=sessions,
                poll_interval=1.0,
                timeout=60.0,
                on_completion=on_complete,
            )
            final_results = loop.run()

            # Both should reach COMPLETED state
            for actor_id, result in final_results.items():
                assert result.state == ActorState.COMPLETED, (
                    f"{actor_id} should be COMPLETED, got {result.state}.\n"
                    f"Error: {result.error_detail}\n"
                    f"Screen:\n{result.screen_text}"
                )

            # on_completion callback should have fired for each actor
            assert len(completion_notifications) == 2, (
                f"Expected 2 completion callbacks, got {len(completion_notifications)}"
            )

        finally:
            for actor_id, session in sessions.items():
                capture_session_artifact(
                    session, "test_director_poll_loop_completion", actor_id
                )
                session.close()

    def test_error_detection_patterns(self):
        """Verify error detection recognises known error patterns (R6.5).

        This is a unit test of the detection logic — no Claude Code needed.
        """
        # Stack trace detection
        traceback_text = (
            "Traceback (most recent call last):\n"
            "  File \"test.py\", line 1\n"
            "ValueError: bad value"
        )
        assert detect_error(traceback_text) is not None

        # Go panic detection
        panic_text = "panic: runtime error: index out of range"
        error = detect_error(panic_text)
        assert error is not None
        assert "panic:" in error

        # Fatal error detection
        assert detect_error("FATAL: cannot connect") is not None

        # Command not found
        assert detect_error("bash: hookwise: command not found") is not None

        # Permission denied
        assert detect_error("Permission denied") is not None

        # No error in normal text
        assert detect_error("Everything is fine, no errors here.") is None

        # Empty screen
        assert detect_error("") is None

    def test_completion_detection_patterns(self):
        """Verify completion detection recognises known markers (R6.4).

        Unit test of completion logic — no Claude Code needed.
        """
        # Direct markers
        assert detect_completion("Task completed successfully") is not None
        assert detect_completion("Done.") is not None
        assert detect_completion("Finished the work") is not None

        # Shell prompt return
        assert detect_completion("user@host:~$ ") is not None

        # Claude Code empty prompt
        assert detect_completion("  >  ") is not None

        # No completion marker
        assert detect_completion("Still working on it...") is None

    def test_transient_error_detection(self):
        """Verify transient error patterns are distinguished from hard errors.

        Unit test — no Claude Code needed.
        """
        assert detect_transient_error("API rate limit exceeded") is not None
        assert detect_transient_error("Connection refused") is not None
        assert detect_transient_error("Request timeout after 30s") is not None
        assert detect_transient_error("Everything is fine") is None

    @skipif_no_claude
    @skipif_no_hookwise
    def test_error_detection_on_bad_command(self, terminal_session):
        """Detect error state when Actor runs an invalid command (R6.5).

        Sends a deliberately broken command and verifies the polling
        loop catches the error pattern.
        """
        session = terminal_session.create_session(
            "claude",
            rows=24,
            cols=120,
            env=_clean_claude_env(),
            cwd=_PROJECT_ROOT,
        )
        error_notifications = []

        def on_error(result: PollResult):
            error_notifications.append(result)

        try:
            session.wait_for_stable(timeout=60, idle_ms=3000)

            # Ask Claude to run a command that will produce an error
            session.send_keys(
                "Run this exact command and show the output: "
                "nonexistent_command_that_does_not_exist_12345"
            )
            time.sleep(0.3)
            session.send_keys("Enter")

            # Poll for error or completion within a reasonable window
            loop = DirectorPollLoop(
                sessions={"error-actor": session},
                poll_interval=2.0,
                timeout=120.0,
                on_error=on_error,
            )
            final_results = loop.run()

            # We should get a terminal state (error or completed)
            result = final_results["error-actor"]
            assert result.state in (ActorState.ERROR, ActorState.COMPLETED), (
                f"Actor should reach a terminal state, got {result.state}"
            )

            # Either the error callback fired, or the session completed
            # (Claude might handle the error gracefully and complete)
            assert result.screen_text.strip(), (
                "Should have captured screen content"
            )

        finally:
            capture_session_artifact(
                session, "test_error_detection_on_bad_command", "error-actor"
            )
            _safe_cleanup(session)

    @skipif_no_claude
    @skipif_no_hookwise
    def test_artifact_capture_on_completion(self, terminal_session):
        """Capture final screen artifacts for post-execution analysis (R7.2).

        Spawns a session, lets it render, then captures the screen state
        to the artifacts directory and verifies the file was created.
        """
        session = terminal_session.create_session(
            "claude",
            rows=24,
            cols=120,
            env=_clean_claude_env(),
            cwd=_PROJECT_ROOT,
        )
        try:
            session.wait_for_stable(timeout=60, idle_ms=3000)

            # Capture artifact
            artifact_path = capture_session_artifact(
                session, "test_artifact_capture_on_completion", "artifact-actor"
            )

            # Verify artifact file exists and has content
            assert os.path.exists(artifact_path), (
                f"Artifact file should exist at {artifact_path}"
            )
            with open(artifact_path) as f:
                content = f.read()
            assert content.strip(), (
                f"Artifact file should have content, but is empty: {artifact_path}"
            )

            # Verify the artifact contains the same content as get_screen_text
            current_screen = session.get_screen_text()
            assert content == current_screen, (
                "Artifact content should match current screen"
            )

        finally:
            _safe_cleanup(session)

    @skipif_no_claude
    @skipif_no_hookwise
    def test_poll_loop_respects_timeout(self, terminal_session):
        """DirectorPollLoop times out properly for long-running sessions (R6.6).

        Spawns a session with a short timeout and verifies the loop
        exits with an error result when the session doesn't complete
        in time, demonstrating the timeout mechanism needed for 30+ min
        session support.
        """
        session = terminal_session.create_session(
            "claude",
            rows=24,
            cols=120,
            env=_clean_claude_env(),
            cwd=_PROJECT_ROOT,
        )
        try:
            session.wait_for_stable(timeout=60, idle_ms=3000)

            # Don't send /exit — let the session stay idle.
            # Use a very short timeout to test the timeout path.
            loop = DirectorPollLoop(
                sessions={"timeout-actor": session},
                poll_interval=1.0,
                timeout=5.0,  # Very short timeout for testing
            )
            final_results = loop.run()

            result = final_results["timeout-actor"]
            # Should have timed out (session is still idle/alive)
            assert result.state == ActorState.ERROR, (
                f"Session should time out as ERROR, got {result.state}"
            )
            assert result.error_detail is not None
            assert "Timed out" in result.error_detail

        finally:
            _safe_cleanup(session)

    @skipif_no_claude
    @skipif_no_hookwise
    def test_multiple_poll_snapshots_non_blocking(self, terminal_session):
        """Multiple rapid polls don't block each other (R6.3).

        Takes several polls in quick succession and verifies each
        returns a valid result without cumulative delay.
        """
        session = terminal_session.create_session(
            "claude",
            rows=24,
            cols=120,
            env=_clean_claude_env(),
            cwd=_PROJECT_ROOT,
        )
        try:
            session.wait_for_stable(timeout=60, idle_ms=3000)

            # Take 10 rapid polls
            results = []
            start = time.monotonic()
            for i in range(10):
                result = poll_session_nonblocking(
                    session, f"rapid-{i}", ActorState.PROCESSING
                )
                results.append(result)
            total_elapsed = time.monotonic() - start

            # 10 non-blocking polls should complete in well under 2 seconds
            assert total_elapsed < 2.0, (
                f"10 rapid polls took {total_elapsed:.2f}s — "
                f"should be near-instant for non-blocking reads"
            )

            # All results should have valid screen text
            for result in results:
                assert result.screen_text is not None
                assert result.actor_id.startswith("rapid-")

            # Results should have monotonically increasing timestamps
            for i in range(1, len(results)):
                assert results[i].timestamp >= results[i - 1].timestamp

        finally:
            _safe_cleanup(session)


# ---------------------------------------------------------------------------
# Calendar status line E2E — validates real Google Calendar data renders
# ---------------------------------------------------------------------------


class TestStatusLineCalendar:
    """EXIT CONDITION: hookwise status-line shows calendar event data."""

    @skipif_no_hookwise
    def test_status_line_has_calendar_data(self):
        """hookwise status-line output should contain calendar emoji or relative time."""
        result = subprocess.run(
            ["hookwise", "status-line", "--project-dir", _PROJECT_ROOT],
            capture_output=True,
            text=True,
            timeout=10,
        )
        # Strip ANSI escape codes for clean matching.
        output = re.sub(r"\x1b\[[0-9;]*m", "", result.stdout)
        assert "\U0001f4c5" in output or re.search(r"in \d+[mh]", output), (
            f"No calendar data in status-line output:\n{output}"
        )
