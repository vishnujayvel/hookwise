"""CLI entry point for hookwise.

Provides the `hookwise` command and subcommands. The primary command
is `dispatch`, which reads a hook event from stdin and routes it
through the configured handler pipeline. Additional commands provide
project initialization, diagnostics, status display, and analytics.
"""

from __future__ import annotations

import json
import sys
from collections import Counter
from pathlib import Path
from typing import Any

import click

from hookwise import __version__
from hookwise.errors import FailOpen, setup_logging


# ---------------------------------------------------------------------------
# Preset YAML templates for `hookwise init --preset`
# ---------------------------------------------------------------------------

_PRESET_MINIMAL = """\
# hookwise.yaml -- minimal configuration
# Docs: https://github.com/hookwise/hookwise
version: 1

guards:
  - match: "Bash"
    action: block
    when: 'tool_input.command contains "rm -rf"'
    reason: "Dangerous command blocked"
  - match: "Bash"
    action: confirm
    when: 'tool_input.command contains "force push"'
    reason: "Force push requires confirmation"
"""

_PRESET_COACHING = """\
# hookwise.yaml -- coaching configuration
# Docs: https://github.com/hookwise/hookwise
version: 1

guards:
  - match: "Bash"
    action: block
    when: 'tool_input.command contains "rm -rf"'
    reason: "Dangerous command blocked"
  - match: "Bash"
    action: confirm
    when: 'tool_input.command contains "force push"'
    reason: "Force push requires confirmation"

coaching:
  enabled: true
  # Nudge after N tool calls without a user prompt
  idle_threshold: 10
  # Maximum minutes before suggesting a break
  session_duration_warning: 60

status_line:
  enabled: true
  format: "{session_duration} | {tool_calls} calls | {ai_ratio}% AI"
"""

_PRESET_ANALYTICS = """\
# hookwise.yaml -- analytics configuration
# Docs: https://github.com/hookwise/hookwise
version: 1

guards:
  - match: "Bash"
    action: block
    when: 'tool_input.command contains "rm -rf"'
    reason: "Dangerous command blocked"
  - match: "Bash"
    action: confirm
    when: 'tool_input.command contains "force push"'
    reason: "Force push requires confirmation"

analytics:
  enabled: true
  # Track tool usage, session length, authorship
  track_sessions: true
  track_tool_calls: true
  track_authorship: true
"""

_PRESET_FULL = """\
# hookwise.yaml -- full configuration
# Docs: https://github.com/hookwise/hookwise
version: 1

guards:
  - match: "Bash"
    action: block
    when: 'tool_input.command contains "rm -rf"'
    reason: "Dangerous command blocked"
  - match: "Bash"
    action: confirm
    when: 'tool_input.command contains "force push"'
    reason: "Force push requires confirmation"

coaching:
  enabled: true
  idle_threshold: 10
  session_duration_warning: 60

analytics:
  enabled: true
  track_sessions: true
  track_tool_calls: true
  track_authorship: true

status_line:
  enabled: true
  format: "{session_duration} | {tool_calls} calls | {ai_ratio}% AI"

cost_tracking:
  enabled: true
  daily_budget_usd: 10.00
  warn_at_percent: 80

# greeting:
#   enabled: true
#   message: "Welcome to hookwise!"

# sounds:
#   enabled: false
#   on_block: "alert"

# transcript_backup:
#   enabled: false
#   path: "~/.hookwise/transcripts/"
"""

_PRESETS: dict[str, str] = {
    "minimal": _PRESET_MINIMAL,
    "coaching": _PRESET_COACHING,
    "analytics": _PRESET_ANALYTICS,
    "full": _PRESET_FULL,
}

# Claude Code hook event types that hookwise dispatches
_HOOK_EVENT_TYPES = [
    "PreToolUse",
    "PostToolUse",
    "Notification",
    "Stop",
    "SubagentStop",
    "SessionStart",
    "SessionEnd",
]


# ---------------------------------------------------------------------------
# Helper: read/write Claude Code settings.json
# ---------------------------------------------------------------------------

def _get_settings_path() -> Path:
    """Return the path to Claude Code's settings.json."""
    return Path.home() / ".claude" / "settings.json"


def _read_settings(settings_path: Path) -> dict[str, Any]:
    """Read Claude Code settings.json, returning empty dict if missing."""
    if not settings_path.is_file():
        return {}
    try:
        return json.loads(settings_path.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError):
        return {}


def _write_settings(settings_path: Path, data: dict[str, Any]) -> None:
    """Write Claude Code settings.json atomically."""
    settings_path.parent.mkdir(parents=True, exist_ok=True)
    settings_path.write_text(
        json.dumps(data, indent=2, sort_keys=False) + "\n",
        encoding="utf-8",
    )


def _register_hooks(settings_path: Path) -> list[str]:
    """Register hookwise dispatch hooks in Claude Code settings.json.

    Returns a list of warning messages for event types that already
    have existing (non-hookwise) hook entries.
    """
    settings = _read_settings(settings_path)
    hooks = settings.setdefault("hooks", {})
    warnings: list[str] = []

    for event_type in _HOOK_EVENT_TYPES:
        hookwise_entry = {
            "type": "command",
            "command": f"hookwise dispatch {event_type}",
        }
        existing = hooks.get(event_type, [])

        # Check if hookwise dispatch is already registered
        already_registered = any(
            isinstance(entry, dict)
            and entry.get("command", "").startswith("hookwise dispatch")
            for entry in existing
        )
        if already_registered:
            continue

        # Warn about existing non-hookwise entries
        if existing:
            warnings.append(
                f"  {event_type}: has {len(existing)} existing hook(s), "
                f"adding hookwise dispatch alongside them"
            )

        existing.append(hookwise_entry)
        hooks[event_type] = existing

    settings["hooks"] = hooks
    _write_settings(settings_path, settings)
    return warnings


# ---------------------------------------------------------------------------
# Click group
# ---------------------------------------------------------------------------

@click.group()
@click.version_option(version=__version__, prog_name="hookwise")
def main() -> None:
    """Hookwise: A config-driven framework for Claude Code hooks."""


# ---------------------------------------------------------------------------
# dispatch command (existing -- DO NOT MODIFY)
# ---------------------------------------------------------------------------

@main.command()
@click.argument("event_name", required=False, default=None)
def dispatch(event_name: str | None) -> None:
    """Dispatch a hook event to registered handlers.

    Reads hook event data from stdin and dispatches to matching handlers
    defined in the hookwise configuration.

    EVENT_NAME is the Claude Code hook event type (e.g., PreToolUse,
    UserPromptSubmit). If not provided, exits silently with code 0.
    """
    logger = setup_logging()

    # Run the dispatch inside FailOpen, capture result outside
    # so that sys.exit() is not swallowed by the error boundary.
    dispatch_result = None

    with FailOpen(logger=logger):
        # Import here to avoid circular imports and keep CLI fast when
        # no dispatch is needed
        from hookwise.config import ConfigEngine
        from hookwise.dispatcher import Dispatcher, read_stdin_payload

        if event_name is None:
            # No event type specified -- exit silently
            return

        # Read event payload from stdin
        payload = read_stdin_payload()

        # Load config and dispatch
        engine = ConfigEngine()
        config = engine.load_config()

        dispatcher = Dispatcher(config_engine=engine)
        dispatch_result = dispatcher.dispatch(event_name, payload, config=config)

    # Emit results OUTSIDE FailOpen so sys.exit is not caught
    if dispatch_result is not None:
        if dispatch_result.stdout:
            click.echo(dispatch_result.stdout)
        if dispatch_result.stderr:
            click.echo(dispatch_result.stderr, err=True)
        if dispatch_result.exit_code != 0:
            sys.exit(dispatch_result.exit_code)


# ---------------------------------------------------------------------------
# test command (existing -- DO NOT MODIFY)
# ---------------------------------------------------------------------------

@main.command()
@click.argument("directory", required=False, default=".")
@click.option("-v", "--verbose", is_flag=True, help="Show verbose test output.")
def test(directory: str, verbose: bool) -> None:
    """Discover and run hookwise tests via pytest.

    Discovers test files matching test_*.py or *_test.py in DIRECTORY
    (defaults to current directory) and runs them via pytest.

    Reports pass/fail counts and exits with the pytest exit code.
    """
    import subprocess

    test_dir = Path(directory).resolve()
    if not test_dir.is_dir():
        click.echo(f"Error: {directory!r} is not a directory.", err=True)
        sys.exit(1)

    # Discover test files
    test_files = sorted(
        set(test_dir.glob("test_*.py")) | set(test_dir.glob("*_test.py"))
    )

    if not test_files:
        click.echo(f"No test files found in {test_dir}")
        sys.exit(0)

    click.echo(f"Discovered {len(test_files)} test file(s) in {test_dir}")

    # Build pytest command
    pytest_args = [sys.executable, "-m", "pytest"]
    if verbose:
        pytest_args.append("-v")
    else:
        pytest_args.extend(["--tb=short", "-q"])
    pytest_args.extend(str(f) for f in test_files)

    result = subprocess.run(  # noqa: S603
        pytest_args,
        capture_output=True,
        text=True,
    )
    if result.stdout:
        click.echo(result.stdout, nl=False)
    if result.stderr:
        click.echo(result.stderr, err=True, nl=False)
    sys.exit(result.returncode)


# ---------------------------------------------------------------------------
# init command
# ---------------------------------------------------------------------------

@main.command()
@click.option(
    "--preset",
    type=click.Choice(["minimal", "coaching", "analytics", "full"]),
    default="minimal",
    help="Named configuration preset.",
)
@click.option(
    "--force",
    is_flag=True,
    default=False,
    help="Overwrite existing hookwise.yaml.",
)
@click.option(
    "--path",
    "target_dir",
    type=click.Path(exists=True, file_okay=False, resolve_path=True),
    default=None,
    help="Directory to create hookwise.yaml in (defaults to current directory).",
)
def init(preset: str, force: bool, target_dir: str | None) -> None:
    """Initialize hookwise in a project.

    Generates a hookwise.yaml config file, creates the state directory,
    and registers dispatcher hooks in Claude Code's settings.json.
    """
    from hookwise.state import ensure_state_dir

    # Determine target directory
    if target_dir is None:
        target_path = Path.cwd()
    else:
        target_path = Path(target_dir)

    config_file = target_path / "hookwise.yaml"

    # Check if config already exists
    if config_file.exists() and not force:
        click.echo(
            click.style("Error: ", fg="red")
            + f"hookwise.yaml already exists at {config_file}"
        )
        click.echo("Use --force to overwrite.")
        sys.exit(1)

    # 1. Write hookwise.yaml
    yaml_content = _PRESETS[preset]
    config_file.write_text(yaml_content, encoding="utf-8")
    click.echo(
        click.style("[ok] ", fg="green")
        + f"Created hookwise.yaml ({preset} preset)"
    )

    # 2. Create state directory
    state_dir = ensure_state_dir()
    click.echo(
        click.style("[ok] ", fg="green")
        + f"State directory ready at {state_dir}"
    )

    # 3. Register hooks in Claude Code settings.json
    settings_path = _get_settings_path()
    hook_warnings = _register_hooks(settings_path)
    if hook_warnings:
        click.echo(
            click.style("[!] ", fg="yellow")
            + "Existing hooks detected:"
        )
        for w in hook_warnings:
            click.echo(w)
    click.echo(
        click.style("[ok] ", fg="green")
        + f"Registered hooks in {settings_path}"
    )

    # 4. Success message
    click.echo("")
    click.echo(click.style("Hookwise initialized!", fg="green", bold=True))
    click.echo("")
    click.echo("Next steps:")
    click.echo(f"  1. Edit {config_file} to customize your hooks")
    click.echo("  2. Run 'hookwise doctor' to verify setup")
    click.echo("  3. Start a Claude Code session to see hooks in action")


# ---------------------------------------------------------------------------
# doctor command
# ---------------------------------------------------------------------------

@main.command()
def doctor() -> None:
    """Check hookwise installation and configuration health.

    Runs diagnostic checks and reports issues with actionable fix suggestions.
    """
    from hookwise.config import ConfigEngine, PROJECT_CONFIG_FILENAME
    from hookwise.state import get_state_dir

    all_ok = True

    def _pass(msg: str) -> None:
        click.echo(click.style("  [ok] ", fg="green") + msg)

    def _fail(msg: str, fix: str | None = None) -> None:
        nonlocal all_ok
        all_ok = False
        click.echo(click.style("  [X]  ", fg="red") + msg)
        if fix:
            click.echo(click.style("       Fix: ", fg="yellow") + fix)

    def _warn(msg: str, suggestion: str | None = None) -> None:
        click.echo(click.style("  [!]  ", fg="yellow") + msg)
        if suggestion:
            click.echo("       " + suggestion)

    click.echo(click.style("Hookwise Doctor", bold=True))
    click.echo("")

    # 1. Python version
    click.echo("Python:")
    py_version = sys.version_info
    if py_version >= (3, 10):
        _pass(f"Python {py_version.major}.{py_version.minor}.{py_version.micro}")
    else:
        _fail(
            f"Python {py_version.major}.{py_version.minor}.{py_version.micro} "
            f"(requires 3.10+)",
            fix="Install Python 3.10 or newer.",
        )

    # 2. Claude Code settings.json
    click.echo("")
    click.echo("Claude Code:")
    settings_path = _get_settings_path()
    if settings_path.is_file():
        _pass(f"settings.json found at {settings_path}")

        # 3. Hook registration
        settings = _read_settings(settings_path)
        hooks = settings.get("hooks", {})
        registered = 0
        for event_type in _HOOK_EVENT_TYPES:
            entries = hooks.get(event_type, [])
            has_hookwise = any(
                isinstance(e, dict)
                and e.get("command", "").startswith("hookwise dispatch")
                for e in entries
            )
            if has_hookwise:
                registered += 1

        if registered == len(_HOOK_EVENT_TYPES):
            _pass(f"All {registered} hook event types registered")
        elif registered > 0:
            _warn(
                f"{registered}/{len(_HOOK_EVENT_TYPES)} hook event types registered",
                suggestion="Run 'hookwise init' to register missing hooks.",
            )
        else:
            _fail(
                "No hookwise hooks registered in settings.json",
                fix="Run 'hookwise init' to register hooks.",
            )
    else:
        _fail(
            f"settings.json not found at {settings_path}",
            fix="Install Claude Code or create ~/.claude/settings.json.",
        )

    # 4. Config file
    click.echo("")
    click.echo("Configuration:")
    config_path = Path.cwd() / PROJECT_CONFIG_FILENAME
    if config_path.is_file():
        _pass(f"hookwise.yaml found at {config_path}")

        # Validate config
        try:
            import yaml

            raw = yaml.safe_load(config_path.read_text(encoding="utf-8"))
            if raw is None:
                raw = {}
            if isinstance(raw, dict):
                engine = ConfigEngine()
                result = engine.validate_config(raw)
                if result.valid:
                    _pass("Config syntax and schema valid")
                else:
                    for err in result.errors:
                        _fail(
                            f"[{err.path}] {err.message}",
                            fix=err.suggestion,
                        )
            else:
                _fail(
                    "hookwise.yaml does not contain a YAML mapping",
                    fix="Ensure the file starts with valid YAML key-value pairs.",
                )
        except yaml.YAMLError as exc:
            _fail(
                f"hookwise.yaml has invalid YAML syntax: {exc}",
                fix="Fix the YAML syntax errors in hookwise.yaml.",
            )
    else:
        _warn(
            f"hookwise.yaml not found at {config_path}",
            suggestion="Run 'hookwise init' to create a config file.",
        )

    # 5. State directory
    click.echo("")
    click.echo("State Directory:")
    state_dir = get_state_dir()
    if state_dir.is_dir():
        _pass(f"State directory exists at {state_dir}")

        # Check permissions
        try:
            stat = state_dir.stat()
            mode = stat.st_mode & 0o777
            if mode == 0o700:
                _pass("Permissions correct (0o700)")
            else:
                _warn(
                    f"Permissions are {oct(mode)} (expected 0o700)",
                    suggestion=f"Run: chmod 700 {state_dir}",
                )
        except OSError:
            _warn("Could not check permissions")

        # Check writability
        try:
            test_file = state_dir / ".doctor_test"
            test_file.write_text("test", encoding="utf-8")
            test_file.unlink()
            _pass("State directory is writable")
        except OSError:
            _fail(
                "State directory is not writable",
                fix=f"Check permissions on {state_dir}.",
            )
    else:
        _fail(
            f"State directory not found at {state_dir}",
            fix="Run 'hookwise init' to create the state directory.",
        )

    # 6. Analytics DB
    click.echo("")
    click.echo("Analytics:")
    db_path = state_dir / "analytics.db"
    if db_path.is_file():
        _pass(f"Analytics database at {db_path}")
        # Check readability
        try:
            import sqlite3

            conn = sqlite3.connect(str(db_path), timeout=2.0)
            conn.execute("SELECT 1")
            conn.close()
            _pass("Database is readable")
        except (sqlite3.Error, OSError) as exc:
            _fail(
                f"Database is not readable: {exc}",
                fix=f"Check file permissions on {db_path}.",
            )
    else:
        _warn(
            "Analytics database not found (will be created on first session)",
        )

    # Summary
    click.echo("")
    if all_ok:
        click.echo(click.style("All checks passed!", fg="green", bold=True))
    else:
        click.echo(
            click.style("Some checks failed.", fg="red", bold=True)
            + " See suggestions above."
        )


# ---------------------------------------------------------------------------
# status command
# ---------------------------------------------------------------------------

@main.command()
def status() -> None:
    """Display hookwise configuration status.

    Shows enabled handlers, guard rules, coaching config, analytics path,
    and cost tracking configuration loaded from hookwise.yaml.
    """
    from hookwise.config import ConfigEngine
    from hookwise.state import get_state_dir

    engine = ConfigEngine()
    try:
        config = engine.load_config()
    except Exception:
        click.echo(click.style("Error: ", fg="red") + "Could not load config.")
        sys.exit(1)

    click.echo(click.style("Hookwise Status", bold=True))
    click.echo(f"Config version: {config.version}")
    click.echo("")

    # Handlers grouped by event type
    click.echo(click.style("Handlers:", bold=True))
    resolved = engine.resolve_handlers(config)
    if resolved:
        event_handlers: dict[str, list[str]] = {}
        for h in resolved:
            for evt in sorted(h.events):
                event_handlers.setdefault(evt, []).append(
                    f"{h.name} ({h.handler_type}, phase={h.phase})"
                )
        for evt_name in sorted(event_handlers):
            click.echo(f"  {evt_name}:")
            for handler_desc in event_handlers[evt_name]:
                click.echo(f"    - {handler_desc}")
    else:
        click.echo("  (no handlers configured)")
    click.echo("")

    # Guard rules
    click.echo(click.style("Guard Rules:", bold=True))
    guards = config.guards
    if guards:
        action_counts: Counter[str] = Counter()
        for g in guards:
            action = g.get("action", "unknown")
            action_counts[action] += 1
        click.echo(f"  Total: {len(guards)} rule(s)")
        for action, count in sorted(action_counts.items()):
            click.echo(f"    {action}: {count}")
    else:
        click.echo("  (no guard rules configured)")
    click.echo("")

    # Coaching
    click.echo(click.style("Coaching:", bold=True))
    coaching = config.coaching
    if coaching and coaching.get("enabled"):
        click.echo("  Enabled: yes")
        if "idle_threshold" in coaching:
            click.echo(f"  Idle threshold: {coaching['idle_threshold']} tool calls")
        if "session_duration_warning" in coaching:
            click.echo(
                f"  Session duration warning: {coaching['session_duration_warning']} minutes"
            )
    else:
        click.echo("  Enabled: no")
    click.echo("")

    # Analytics
    click.echo(click.style("Analytics:", bold=True))
    analytics_cfg = config.analytics
    state_dir = get_state_dir()
    db_path = state_dir / "analytics.db"
    if analytics_cfg and analytics_cfg.get("enabled"):
        click.echo("  Enabled: yes")
        if db_path.is_file():
            size_bytes = db_path.stat().st_size
            if size_bytes < 1024:
                size_str = f"{size_bytes} B"
            elif size_bytes < 1024 * 1024:
                size_str = f"{size_bytes / 1024:.1f} KB"
            else:
                size_str = f"{size_bytes / (1024 * 1024):.1f} MB"
            click.echo(f"  Database: {db_path} ({size_str})")
        else:
            click.echo(f"  Database: {db_path} (not yet created)")
    else:
        click.echo("  Enabled: no")
        if db_path.is_file():
            size_bytes = db_path.stat().st_size
            if size_bytes < 1024:
                size_str = f"{size_bytes} B"
            elif size_bytes < 1024 * 1024:
                size_str = f"{size_bytes / 1024:.1f} KB"
            else:
                size_str = f"{size_bytes / (1024 * 1024):.1f} MB"
            click.echo(f"  Database: {db_path} ({size_str})")
    click.echo("")

    # Cost tracking
    click.echo(click.style("Cost Tracking:", bold=True))
    cost_cfg = config.cost_tracking
    if cost_cfg and cost_cfg.get("enabled"):
        click.echo("  Enabled: yes")
        if "daily_budget_usd" in cost_cfg:
            click.echo(f"  Daily budget: ${cost_cfg['daily_budget_usd']:.2f}")
        if "warn_at_percent" in cost_cfg:
            click.echo(f"  Warning threshold: {cost_cfg['warn_at_percent']}%")
    else:
        click.echo("  Enabled: no")


# ---------------------------------------------------------------------------
# stats command
# ---------------------------------------------------------------------------

@main.command()
@click.option("--json", "as_json", is_flag=True, default=False, help="Output as JSON.")
@click.option("--cost", "show_cost", is_flag=True, default=False, help="Show cost breakdown by model.")
@click.option("--agents", "show_agents", is_flag=True, default=False, help="Show subagent activity summary.")
def stats(as_json: bool, show_cost: bool, show_agents: bool) -> None:
    """Display analytics and usage statistics.

    Shows today's sessions, authorship breakdown, tool call breakdown,
    and 7-day trend data. Requires the analytics database to exist.
    """
    from hookwise.state import get_state_dir

    state_dir = get_state_dir()
    db_path = state_dir / "analytics.db"

    if not db_path.is_file():
        if as_json:
            click.echo(json.dumps({"error": "No analytics data yet"}, indent=2))
        else:
            click.echo("No analytics data yet.")
            click.echo(
                f"The analytics database will be created at {db_path} "
                "when analytics is enabled and a session runs."
            )
        return

    from hookwise.analytics.db import AnalyticsDB

    db = AnalyticsDB(db_path=db_path)

    try:
        # Gather data
        today_sessions = db.query_session_stats(days=1)
        tool_breakdown = db.query_tool_breakdown(days=1)
        authorship = db.query_authorship_summary(days=1)
        weekly_trends = db.query_daily_summary(days=7)

        # Cost data (from sessions)
        cost_data: list[dict[str, Any]] = []
        if show_cost:
            for s in today_sessions:
                if s.get("estimated_cost_usd", 0) > 0:
                    cost_data.append({
                        "session_id": s["id"],
                        "cost_usd": s.get("estimated_cost_usd", 0),
                        "tokens": s.get("estimated_tokens", 0),
                    })

        # Agent data
        agent_data: list[dict[str, Any]] = []
        if show_agents:
            try:
                rows = db._fetchall(
                    """SELECT agent_id, agent_type,
                        COUNT(*) as span_count,
                        GROUP_CONCAT(DISTINCT files_modified) as files
                       FROM agent_spans
                       WHERE started_at >= datetime('now', '-1 day')
                       GROUP BY agent_id, agent_type
                       ORDER BY span_count DESC""",
                    (),
                )
                agent_data = [dict(r) for r in rows]
            except Exception:
                agent_data = []

        if as_json:
            output: dict[str, Any] = {
                "today": {
                    "sessions": len(today_sessions),
                    "total_tool_calls": sum(
                        s.get("total_tool_calls", 0) or 0 for s in today_sessions
                    ),
                    "ai_authored_lines": sum(
                        s.get("ai_authored_lines", 0) or 0 for s in today_sessions
                    ),
                    "human_verified_lines": sum(
                        s.get("human_verified_lines", 0) or 0 for s in today_sessions
                    ),
                    "total_duration_seconds": sum(
                        s.get("duration_seconds", 0) or 0 for s in today_sessions
                    ),
                },
                "tool_breakdown": tool_breakdown,
                "authorship": authorship,
                "weekly_trends": weekly_trends,
            }
            if show_cost:
                output["cost"] = cost_data
            if show_agents:
                output["agents"] = agent_data
            click.echo(json.dumps(output, indent=2, default=str))
            return

        # Human-readable output
        click.echo(click.style("Hookwise Stats", bold=True))
        click.echo("")

        # Today's summary
        click.echo(click.style("Today:", bold=True))
        num_sessions = len(today_sessions)
        total_tools = sum(s.get("total_tool_calls", 0) or 0 for s in today_sessions)
        ai_lines = sum(s.get("ai_authored_lines", 0) or 0 for s in today_sessions)
        human_lines = sum(
            s.get("human_verified_lines", 0) or 0 for s in today_sessions
        )
        total_duration = sum(
            s.get("duration_seconds", 0) or 0 for s in today_sessions
        )

        click.echo(f"  Sessions: {num_sessions}")
        click.echo(f"  Tool calls: {total_tools}")

        # Authorship bar chart
        total_lines = ai_lines + human_lines
        if total_lines > 0:
            ai_pct = ai_lines / total_lines * 100
            human_pct = human_lines / total_lines * 100
            bar_width = 30
            ai_bar = int(ai_pct / 100 * bar_width)
            human_bar = bar_width - ai_bar
            bar = (
                click.style("A" * ai_bar, fg="cyan")
                + click.style("H" * human_bar, fg="green")
            )
            click.echo(
                f"  Authorship: [{bar}] "
                f"AI:{ai_lines} ({ai_pct:.0f}%) "
                f"Human:{human_lines} ({human_pct:.0f}%)"
            )
        else:
            click.echo("  Authorship: no data")

        # Average session duration
        if num_sessions > 0:
            avg_sec = total_duration / num_sessions
            avg_min = avg_sec / 60
            click.echo(f"  Avg session: {avg_min:.1f} min")
        click.echo("")

        # Tool breakdown
        click.echo(click.style("Tool Calls (today):", bold=True))
        if tool_breakdown:
            for tb in tool_breakdown[:10]:  # Top 10
                click.echo(
                    f"  {tb['tool_name']}: {tb['count']} "
                    f"(+{tb.get('total_lines_added', 0)} "
                    f"-{tb.get('total_lines_removed', 0)} lines)"
                )
        else:
            click.echo("  (no tool data)")
        click.echo("")

        # 7-day trends
        click.echo(click.style("7-Day Trends:", bold=True))
        if weekly_trends:
            for day in weekly_trends:
                day_sessions = day.get("sessions", 0)
                day_tools = day.get("total_tool_calls", 0)
                day_cost = day.get("estimated_cost_usd", 0) or 0
                bar_len = min(day_sessions * 3, 30)
                bar = click.style("|" * bar_len, fg="cyan")
                cost_str = f" ${day_cost:.2f}" if day_cost > 0 else ""
                click.echo(
                    f"  {day['date']}: {bar} "
                    f"{day_sessions} sessions, {day_tools} tools{cost_str}"
                )
        else:
            click.echo("  (no trend data)")

        # Cost breakdown
        if show_cost:
            click.echo("")
            click.echo(click.style("Cost Breakdown (today):", bold=True))
            if cost_data:
                total_cost = sum(c["cost_usd"] for c in cost_data)
                total_tokens = sum(c["tokens"] for c in cost_data)
                click.echo(f"  Total cost: ${total_cost:.4f}")
                click.echo(f"  Total tokens: {total_tokens:,}")
                for c in cost_data:
                    click.echo(
                        f"  Session {c['session_id'][:8]}...: "
                        f"${c['cost_usd']:.4f} ({c['tokens']:,} tokens)"
                    )
            else:
                click.echo("  (no cost data)")

        # Agent summary
        if show_agents:
            click.echo("")
            click.echo(click.style("Subagent Activity (today):", bold=True))
            if agent_data:
                for a in agent_data:
                    agent_type = a.get("agent_type") or "unknown"
                    click.echo(
                        f"  {a['agent_id'][:12]}... ({agent_type}): "
                        f"{a['span_count']} span(s)"
                    )
            else:
                click.echo("  (no subagent data)")

    finally:
        db.close()


if __name__ == "__main__":
    main()
