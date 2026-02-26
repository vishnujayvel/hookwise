"""Data readers for hookwise TUI — all read-only access to hookwise state.

Reads:
  - hookwise.yaml (YAML config)
  - ~/.hookwise/state/status-line-cache.json (feed cache bus)
  - ~/.hookwise/analytics.db (SQLite analytics)
  - ~/.claude/usage-data/ (session-meta + facets for insights)
  - ~/.hookwise/daemon.pid (daemon status)
"""

from __future__ import annotations

import json
import os
import sqlite3
from collections import Counter
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import yaml


# --- Path resolution ---

def _home() -> Path:
    return Path.home()


def _resolve(p: str) -> Path:
    if p.startswith("~/"):
        return _home() / p[2:]
    return Path(p)


def _config_dir() -> Path:
    """Hookwise state directory: ~/.hookwise/"""
    return _home() / ".hookwise"


def _default_config_path() -> Path:
    """Project config: hookwise.yaml in HOOKWISE_CONFIG env or cwd."""
    config_dir = os.environ.get("HOOKWISE_CONFIG", os.getcwd())
    return Path(config_dir) / "hookwise.yaml"


def _default_cache_path() -> Path:
    return _config_dir() / "state" / "status-line-cache.json"


def _default_db_path() -> Path:
    return _config_dir() / "analytics.db"


def _default_pid_path() -> Path:
    return _config_dir() / "daemon.pid"


def _default_usage_data_path() -> Path:
    return _home() / ".claude" / "usage-data"


# --- Config reader ---

def read_config(config_path: Path | None = None) -> dict[str, Any]:
    """Read hookwise.yaml config. Returns empty dict if missing."""
    path = config_path or _default_config_path()
    if not path.exists():
        # Try global config
        global_path = _config_dir() / "config.yaml"
        if global_path.exists():
            path = global_path
        else:
            return {}
    try:
        with open(path) as f:
            data = yaml.safe_load(f)
        return data if isinstance(data, dict) else {}
    except Exception:
        return {}


# --- Cache bus reader ---

def read_cache(cache_path: Path | None = None) -> dict[str, Any]:
    """Read the status-line cache bus JSON. Returns empty dict on error."""
    path = cache_path or _default_cache_path()
    if not path.exists():
        return {}
    try:
        with open(path) as f:
            data = json.load(f)
        return data if isinstance(data, dict) else {}
    except Exception:
        return {}


def is_fresh(entry: dict[str, Any]) -> bool:
    """Check if a cache entry is within its TTL."""
    updated_at = entry.get("updated_at")
    ttl = entry.get("ttl_seconds")
    if not isinstance(updated_at, str) or not isinstance(ttl, (int, float)) or ttl <= 0:
        return False
    try:
        ts = datetime.fromisoformat(updated_at.replace("Z", "+00:00"))
        now = datetime.now(timezone.utc)
        elapsed = (now - ts).total_seconds()
        return elapsed < ttl
    except Exception:
        return False


# --- Analytics DB reader ---

@dataclass
class DailySummary:
    date: str
    total_events: int
    total_tool_calls: int
    lines_added: int
    lines_removed: int
    sessions: int


@dataclass
class ToolBreakdown:
    tool_name: str
    count: int
    lines_added: int
    lines_removed: int


@dataclass
class AuthorshipSummary:
    total_entries: int
    total_lines_changed: int
    weighted_ai_score: float
    breakdown: dict[str, int] = field(default_factory=dict)


@dataclass
class AnalyticsData:
    daily: list[DailySummary] = field(default_factory=list)
    tools: list[ToolBreakdown] = field(default_factory=list)
    authorship: AuthorshipSummary = field(
        default_factory=lambda: AuthorshipSummary(0, 0, 0.0)
    )


def read_analytics(db_path: Path | None = None, days: int = 7) -> AnalyticsData:
    """Read analytics from SQLite. Returns empty data on error."""
    path = db_path or _default_db_path()
    if not path.exists():
        return AnalyticsData()

    try:
        conn = sqlite3.connect(str(path), timeout=5)
        conn.row_factory = sqlite3.Row

        # Daily summary
        daily_rows = conn.execute(
            """
            SELECT
                DATE(timestamp) as date,
                COUNT(*) as total_events,
                COUNT(tool_name) as total_tool_calls,
                COALESCE(SUM(lines_added), 0) as lines_added,
                COALESCE(SUM(lines_removed), 0) as lines_removed,
                COUNT(DISTINCT session_id) as sessions
            FROM events
            WHERE timestamp >= DATE('now', ?)
            GROUP BY DATE(timestamp)
            ORDER BY date DESC
            """,
            (f"-{days} days",),
        ).fetchall()

        daily = [
            DailySummary(
                date=r["date"],
                total_events=r["total_events"],
                total_tool_calls=r["total_tool_calls"],
                lines_added=r["lines_added"],
                lines_removed=r["lines_removed"],
                sessions=r["sessions"],
            )
            for r in daily_rows
        ]

        # Tool breakdown
        tool_rows = conn.execute(
            """
            SELECT
                tool_name,
                COUNT(*) as count,
                COALESCE(SUM(lines_added), 0) as lines_added,
                COALESCE(SUM(lines_removed), 0) as lines_removed
            FROM events
            WHERE tool_name IS NOT NULL
            GROUP BY tool_name
            ORDER BY count DESC
            """
        ).fetchall()

        tools = [
            ToolBreakdown(
                tool_name=r["tool_name"],
                count=r["count"],
                lines_added=r["lines_added"],
                lines_removed=r["lines_removed"],
            )
            for r in tool_rows
        ]

        # Authorship
        auth_row = conn.execute(
            """
            SELECT
                COUNT(*) as total_entries,
                COALESCE(SUM(lines_changed), 0) as total_lines_changed,
                COALESCE(SUM(ai_score * lines_changed), 0) as weighted_sum
            FROM authorship_ledger
            """
        ).fetchone()

        breakdown_rows = conn.execute(
            """
            SELECT classification, COUNT(*) as count
            FROM authorship_ledger
            GROUP BY classification
            """
        ).fetchall()

        breakdown = {r["classification"]: r["count"] for r in breakdown_rows}
        total_lines = auth_row["total_lines_changed"] if auth_row else 0
        weighted_score = (
            auth_row["weighted_sum"] / total_lines
            if auth_row and total_lines > 0
            else 0.0
        )

        authorship = AuthorshipSummary(
            total_entries=auth_row["total_entries"] if auth_row else 0,
            total_lines_changed=total_lines,
            weighted_ai_score=weighted_score,
            breakdown=breakdown,
        )

        conn.close()
        return AnalyticsData(daily=daily, tools=tools, authorship=authorship)

    except Exception:
        return AnalyticsData()


# --- Daemon status ---

@dataclass
class DaemonStatus:
    running: bool
    pid: int | None
    uptime_seconds: int | None


def read_daemon_status(pid_path: Path | None = None) -> DaemonStatus:
    """Check if the hookwise daemon is running."""
    path = pid_path or _default_pid_path()
    if not path.exists():
        return DaemonStatus(running=False, pid=None, uptime_seconds=None)

    try:
        pid_str = path.read_text().strip()
        pid = int(pid_str)
        if pid <= 0:
            return DaemonStatus(running=False, pid=None, uptime_seconds=None)
    except (ValueError, OSError):
        return DaemonStatus(running=False, pid=None, uptime_seconds=None)

    # Check if process is alive
    try:
        os.kill(pid, 0)
    except (ProcessLookupError, PermissionError):
        return DaemonStatus(running=False, pid=pid, uptime_seconds=None)

    # Compute uptime from PID file mtime
    try:
        mtime = path.stat().st_mtime
        uptime = int(datetime.now().timestamp() - mtime)
    except OSError:
        uptime = None

    return DaemonStatus(running=True, pid=pid, uptime_seconds=uptime)


# --- Feed health ---

@dataclass
class FeedHealth:
    name: str
    enabled: bool
    last_update: str | None
    interval_seconds: int
    healthy: bool


def read_feed_health(
    config: dict[str, Any],
    cache: dict[str, Any],
) -> list[FeedHealth]:
    """Compute health for each configured feed."""
    feeds_config = config.get("feeds", {})
    if not isinstance(feeds_config, dict):
        return []

    builtin_feeds = [
        ("pulse", feeds_config.get("pulse", {})),
        ("project", feeds_config.get("project", {})),
        ("calendar", feeds_config.get("calendar", {})),
        ("news", feeds_config.get("news", {})),
        ("insights", feeds_config.get("insights", {})),
    ]

    results = []
    for name, feed_cfg in builtin_feeds:
        if not isinstance(feed_cfg, dict):
            continue
        enabled = feed_cfg.get("enabled", False)
        interval = feed_cfg.get("interval_seconds", 60)

        entry = cache.get(name, {})
        updated_at = entry.get("updated_at") if isinstance(entry, dict) else None
        last_update = updated_at if isinstance(updated_at, str) else None

        # Healthy if disabled (not expected to update) or updated within 2x interval
        healthy = True
        if enabled and last_update:
            try:
                ts = datetime.fromisoformat(last_update.replace("Z", "+00:00"))
                elapsed = (datetime.now(timezone.utc) - ts).total_seconds()
                healthy = elapsed < interval * 2
            except Exception:
                healthy = False
        elif enabled and not last_update:
            healthy = False

        results.append(FeedHealth(
            name=name,
            enabled=enabled,
            last_update=last_update,
            interval_seconds=interval,
            healthy=healthy,
        ))

    # Custom feeds
    custom = feeds_config.get("custom", [])
    if isinstance(custom, list):
        for c in custom:
            if not isinstance(c, dict):
                continue
            name = c.get("name", "unknown")
            enabled = c.get("enabled", True)
            interval = c.get("interval_seconds", 60)
            entry = cache.get(name, {})
            updated_at = entry.get("updated_at") if isinstance(entry, dict) else None
            results.append(FeedHealth(
                name=name,
                enabled=enabled,
                last_update=updated_at if isinstance(updated_at, str) else None,
                interval_seconds=interval,
                healthy=bool(updated_at),
            ))

    return results


# --- Insights aggregation ---

@dataclass
class InsightsData:
    total_sessions: int = 0
    total_messages: int = 0
    total_lines_added: int = 0
    avg_duration_minutes: float = 0.0
    top_tools: list[tuple[str, int]] = field(default_factory=list)
    friction_counts: dict[str, int] = field(default_factory=dict)
    friction_total: int = 0
    peak_hour: int = 0
    days_active: int = 0
    daily_sessions: dict[str, int] = field(default_factory=dict)
    daily_messages: dict[str, int] = field(default_factory=dict)
    daily_lines: dict[str, int] = field(default_factory=dict)


def _read_json_files(dir_path: Path) -> list[dict[str, Any]]:
    """Read all JSON files in a directory, skipping malformed ones."""
    if not dir_path.is_dir():
        return []
    results = []
    for f in dir_path.iterdir():
        if not f.suffix == ".json":
            continue
        try:
            with open(f) as fh:
                data = json.load(fh)
            if isinstance(data, dict):
                results.append(data)
        except Exception:
            continue
    return results


def read_insights(
    usage_data_path: Path | None = None,
    staleness_days: int = 30,
) -> InsightsData:
    """Aggregate Claude Code usage data from session-meta + facets.

    Mirrors the TypeScript aggregateInsights() function.
    """
    base = usage_data_path or _default_usage_data_path()
    session_meta_dir = base / "session-meta"
    facets_dir = base / "facets"

    now = datetime.now(timezone.utc)
    cutoff = now.timestamp() - staleness_days * 86400

    # Read and filter sessions
    all_sessions = _read_json_files(session_meta_dir)
    valid_sessions = []
    for s in all_sessions:
        start_time = s.get("start_time")
        if not isinstance(start_time, str):
            continue
        try:
            ts = datetime.fromisoformat(start_time.replace("Z", "+00:00")).timestamp()
        except Exception:
            continue
        if ts >= cutoff:
            valid_sessions.append(s)

    if not valid_sessions:
        return InsightsData()

    valid_ids = {s.get("session_id") for s in valid_sessions if s.get("session_id")}

    # Accumulators
    total_messages = 0
    total_lines = 0
    total_duration = 0.0
    tool_counts: Counter[str] = Counter()
    hour_counts = [0] * 24
    active_dates: set[str] = set()
    daily_sessions: Counter[str] = Counter()
    daily_messages: Counter[str] = Counter()
    daily_lines: Counter[str] = Counter()

    for session in valid_sessions:
        msgs = session.get("user_message_count", 0)
        lines = session.get("lines_added", 0)
        dur = session.get("duration_minutes", 0)

        total_messages += msgs if isinstance(msgs, (int, float)) else 0
        total_lines += lines if isinstance(lines, (int, float)) else 0
        total_duration += dur if isinstance(dur, (int, float)) else 0

        # Tool counts
        tools = session.get("tool_counts", {})
        if isinstance(tools, dict):
            for name, count in tools.items():
                if isinstance(count, (int, float)):
                    tool_counts[name] += int(count)

        # Message hours
        hours = session.get("message_hours", [])
        if isinstance(hours, list):
            for h in hours:
                if isinstance(h, int) and 0 <= h < 24:
                    hour_counts[h] += 1

        # Days active + daily breakdowns
        start_time = session.get("start_time", "")
        if isinstance(start_time, str) and len(start_time) >= 10:
            date_str = start_time[:10]
            active_dates.add(date_str)
            daily_sessions[date_str] += 1
            daily_messages[date_str] += msgs if isinstance(msgs, (int, float)) else 0
            daily_lines[date_str] += lines if isinstance(lines, (int, float)) else 0

    # Read facets for friction
    friction_counts: Counter[str] = Counter()
    all_facets = _read_json_files(facets_dir)
    for facet in all_facets:
        sid = facet.get("session_id")
        if not sid or sid not in valid_ids:
            continue
        friction = facet.get("friction_counts", {})
        if isinstance(friction, dict):
            for cat, count in friction.items():
                if isinstance(count, (int, float)):
                    friction_counts[cat] += int(count)

    # Derived metrics
    avg_duration = total_duration / len(valid_sessions) if valid_sessions else 0
    top_tools = tool_counts.most_common(10)
    peak_hour = max(range(24), key=lambda h: hour_counts[h]) if any(hour_counts) else 0
    friction_total = sum(friction_counts.values())

    return InsightsData(
        total_sessions=len(valid_sessions),
        total_messages=total_messages,
        total_lines_added=total_lines,
        avg_duration_minutes=round(avg_duration, 1),
        top_tools=top_tools,
        friction_counts=dict(friction_counts),
        friction_total=friction_total,
        peak_hour=peak_hour,
        days_active=len(active_dates),
        daily_sessions=dict(daily_sessions),
        daily_messages=dict(daily_messages),
        daily_lines=dict(daily_lines),
    )


# --- Insights LLM summary ---

@dataclass
class InsightsSummary:
    patterns: str = ""
    top_insight: str = ""
    focus_area: str = ""
    generated_at: str = ""


def read_insights_summary(
    summary_path: Path | None = None,
) -> InsightsSummary | None:
    """Read cached daily LLM-generated insights summary."""
    path = summary_path or (_config_dir() / "state" / "insights-summary.json")
    if not path.exists():
        return None
    try:
        with open(path) as f:
            data = json.load(f)
        if not isinstance(data, dict):
            return None
        return InsightsSummary(
            patterns=data.get("patterns", ""),
            top_insight=data.get("top_insight", ""),
            focus_area=data.get("focus_area", ""),
            generated_at=data.get("generated_at", ""),
        )
    except Exception:
        return None


def generate_insights_summary(
    insights: InsightsData,
    summary_path: Path | None = None,
) -> InsightsSummary:
    """Generate a daily LLM summary of usage insights using Claude API (haiku).

    Caches the result to disk. Returns cached version if already generated today.
    """
    path = summary_path or (_config_dir() / "state" / "insights-summary.json")

    # Check if already generated today
    existing = read_insights_summary(path)
    if existing and existing.generated_at:
        try:
            gen_date = existing.generated_at[:10]
            today = datetime.now().strftime("%Y-%m-%d")
            if gen_date == today:
                return existing
        except Exception:
            pass

    # Generate with Claude API
    try:
        from anthropic import Anthropic

        client = Anthropic()

        prompt = f"""Analyze these Claude Code usage metrics from the last 30 days and provide a brief daily summary:

- Total sessions: {insights.total_sessions}
- Total messages: {insights.total_messages}
- Total lines added: {insights.total_lines_added}
- Avg session duration: {insights.avg_duration_minutes} minutes
- Days active: {insights.days_active}
- Peak coding hour: {insights.peak_hour}:00
- Top tools: {', '.join(f'{t[0]}({t[1]})' for t in insights.top_tools[:5])}
- Friction events: {insights.friction_total} ({', '.join(f'{k}:{v}' for k, v in insights.friction_counts.items())})

Respond in exactly this JSON format:
{{"patterns": "1-2 sentence coding patterns narrative", "top_insight": "1 sentence top productivity insight", "focus_area": "1 sentence suggested focus area"}}"""

        response = client.messages.create(
            model="claude-haiku-4-5-20251001",
            max_tokens=300,
            messages=[{"role": "user", "content": prompt}],
        )

        text = response.content[0].text.strip()
        data = json.loads(text)

        summary = InsightsSummary(
            patterns=data.get("patterns", ""),
            top_insight=data.get("top_insight", ""),
            focus_area=data.get("focus_area", ""),
            generated_at=datetime.now().isoformat(),
        )

        # Cache to disk
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "w") as f:
            json.dump(
                {
                    "patterns": summary.patterns,
                    "top_insight": summary.top_insight,
                    "focus_area": summary.focus_area,
                    "generated_at": summary.generated_at,
                },
                f,
                indent=2,
            )

        return summary

    except Exception:
        return InsightsSummary(
            patterns="Unable to generate summary — check ANTHROPIC_API_KEY",
            generated_at=datetime.now().isoformat(),
        )


# --- Recipe discovery ---

@dataclass
class Recipe:
    name: str
    description: str
    category: str
    path: str
    active: bool


def read_recipes(config: dict[str, Any]) -> list[Recipe]:
    """Discover recipes from the recipes/ directory and check which are active."""
    recipes_dir = Path(os.environ.get("HOOKWISE_CONFIG", os.getcwd())) / "recipes"
    active_includes = set()
    includes = config.get("includes", [])
    if isinstance(includes, list):
        for inc in includes:
            if isinstance(inc, str):
                active_includes.add(inc)

    results = []
    if not recipes_dir.is_dir():
        return results

    for category_dir in sorted(recipes_dir.iterdir()):
        if not category_dir.is_dir():
            continue
        category = category_dir.name
        for recipe_dir in sorted(category_dir.iterdir()):
            if not recipe_dir.is_dir():
                continue
            hooks_yaml = recipe_dir / "hooks.yaml"
            if not hooks_yaml.exists():
                continue
            try:
                with open(hooks_yaml) as f:
                    data = yaml.safe_load(f)
                if not isinstance(data, dict):
                    continue
                name = data.get("name", recipe_dir.name)
                desc = data.get("description", "")
                # Check if active by seeing if includes references this recipe
                rel_path = str(recipe_dir.relative_to(
                    Path(os.environ.get("HOOKWISE_CONFIG", os.getcwd()))
                ))
                active = any(rel_path in inc for inc in active_includes)
                results.append(Recipe(
                    name=name,
                    description=desc,
                    category=category,
                    path=str(recipe_dir),
                    active=active,
                ))
            except Exception:
                continue

    return results
