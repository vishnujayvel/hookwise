"""Status tab — Status line preview with segment configurator."""

from __future__ import annotations

import time
from datetime import datetime
from pathlib import Path

from textual.app import ComposeResult
from textual.containers import Container
from textual.css.query import NoMatches
from textual.widget import Widget
from textual.widgets import Static, Switch

from hookwise_tui.data import read_cache, read_config, write_config


# Path where the TS status-line command persists its ANSI-stripped output
_LAST_STATUS_OUTPUT_PATH = Path.home() / ".hookwise" / "cache" / "last-status-output.txt"

# Maximum age (seconds) before the live output file is considered stale
_LIVE_OUTPUT_MAX_AGE = 60


# 5-line default layout (matches TS DEFAULT_TWO_TIER_CONFIG)
FIXED_LINE_1 = ["context_bar", "mode_badge", "cost", "duration", "daemon_health"]
FIXED_LINE_2 = ["project", "calendar", "weather"]
FIXED_LINE_3 = ["insights_friction", "insights_pace"]
FIXED_LINE_4 = ["insights_trend"]
MIDDLE_SEGMENTS = ["agents"]
ROTATING_SEGMENTS = [
    "news", "mantra", "memories", "pulse", "streak", "builder_trap", "clock",
]
ALL_FIXED = FIXED_LINE_1 + FIXED_LINE_2 + FIXED_LINE_3 + FIXED_LINE_4
ALL_SEGMENTS = ALL_FIXED + MIDDLE_SEGMENTS + ROTATING_SEGMENTS

# Segments that need live stdin data (not available in cache alone)
STDIN_SEGMENTS = {"context_bar", "cost", "duration", "daemon_health"}


class SegmentRow(Widget):
    """A single segment row with switch toggle and label."""

    DEFAULT_CSS = """
    SegmentRow {
        layout: horizontal;
        height: 3;
        padding: 0 1;
    }
    SegmentRow .seg-switch {
        width: 8;
        margin: 0 1 0 0;
    }
    SegmentRow .seg-name {
        width: auto;
        content-align-vertical: middle;
        padding: 1 0 0 0;
    }
    SegmentRow .seg-data-dot {
        width: 3;
        content-align-vertical: middle;
        padding: 1 0 0 0;
    }
    """

    def __init__(self, segment_name: str, enabled: bool, has_data: bool) -> None:
        super().__init__()
        self._segment_name = segment_name
        self._enabled = enabled
        self._has_data = has_data

    def compose(self) -> ComposeResult:
        dot = "[green]\u25cf[/green]" if self._has_data else "[dim]\u25cb[/dim]"
        yield Static(dot, classes="seg-data-dot")
        yield Switch(value=self._enabled, id=f"seg-{self._segment_name}", classes="seg-switch")
        yield Static(self._segment_name, classes="seg-name")


class StatusTab(Widget):
    """Status — status line preview and segment configuration."""

    DEFAULT_CSS = """
    StatusTab {
        height: auto;
    }
    StatusTab .status-intro {
        color: $text-muted;
        margin: 0 0 1 0;
    }
    StatusTab .preview-box {
        padding: 1 2;
        margin: 1 0;
        border: heavy $accent;
        background: $surface-darken-2;
        height: auto;
    }
    StatusTab .preview-label {
        text-style: bold;
        color: $accent;
        margin: 0 0 1 0;
    }
    StatusTab .preview-line {
        text-style: bold;
        color: $text;
    }
    StatusTab .tier-header {
        text-style: bold;
        color: $accent;
        margin: 1 0 0 0;
        padding: 0 1;
    }
    StatusTab .segment-group {
        margin: 0 0 1 0;
        padding: 0 2;
        border: round $primary;
        background: $surface-darken-1;
        height: auto;
    }
    StatusTab .config-info {
        margin: 1 0;
        padding: 1 2;
        border: round $primary;
        background: $surface-darken-1;
        height: auto;
    }
    StatusTab .tier-summary {
        padding: 1 2;
        margin: 1 0;
        border: round $accent;
        background: $surface-darken-1;
        height: auto;
        color: $text-muted;
    }
    """

    def compose(self) -> ComposeResult:
        config = read_config()
        cache = read_cache()
        status_config = config.get("status_line", {})
        if not isinstance(status_config, dict):
            status_config = {}

        enabled = status_config.get("enabled", False)
        delimiter = status_config.get("delimiter", " | ")
        active_segments = status_config.get("segments", list(ALL_SEGMENTS))
        if not isinstance(active_segments, list):
            active_segments = list(ALL_SEGMENTS)
        active_set = set(active_segments)

        yield Static(
            "The status line renders in your terminal showing live data from feeds and hooks.",
            classes="status-intro",
        )

        # Preview
        yield Static("Live Preview", classes="preview-label")
        yield Container(id="preview-box", classes="preview-box")

        # Config info
        with Container(classes="config-info"):
            yield Static(
                f"[bold]Status Line:[/bold] "
                f"{'[green]ENABLED[/green]' if enabled else '[red]DISABLED[/red]'}"
            )
            yield Static(f"[bold]Delimiter:[/bold] [cyan]{repr(delimiter)}[/cyan]")
            yield Static(
                f"[bold]Cache Path:[/bold] [dim]{status_config.get('cache_path', '~/.hookwise/state/status-line-cache.json')}[/dim]"
            )

        # Tier summary
        yield Container(id="tier-summary", classes="tier-summary")

        # Fixed line groups
        for _, (label, segs) in enumerate([
            ("Line 1 \u2014 Status bar", FIXED_LINE_1),
            ("Line 2 \u2014 Context", FIXED_LINE_2),
            ("Line 3 \u2014 Insights", FIXED_LINE_3),
            ("Line 4 \u2014 Trends", FIXED_LINE_4),
        ], 1):
            yield Static(f"{label} (fixed)", classes="tier-header")
            with Container(classes="segment-group"):
                for seg in segs:
                    has_data = self._segment_has_data(seg, cache)
                    yield SegmentRow(seg, seg in active_set, has_data)

        # Middle segments (agents — shown between fixed and rotating)
        if MIDDLE_SEGMENTS:
            yield Static("Middle \u2014 Agents", classes="tier-header")
            with Container(classes="segment-group"):
                for seg in MIDDLE_SEGMENTS:
                    has_data = self._segment_has_data(seg, cache)
                    yield SegmentRow(seg, seg in active_set, has_data)

        # Rotating segments group
        yield Static("Line 5 \u2014 Rotating (cycles through)", classes="tier-header")
        with Container(classes="segment-group"):
            for seg in ROTATING_SEGMENTS:
                has_data = self._segment_has_data(seg, cache)
                yield SegmentRow(seg, seg in active_set, has_data)

    @staticmethod
    def _segment_has_data(seg: str, cache: dict) -> bool:
        """Check if a segment has real data available in the cache."""
        if seg in STDIN_SEGMENTS:
            # These get data from live stdin; check if live output is fresh
            return StatusTab._read_live_output() is not None
        if seg in ("insights_friction", "insights_pace", "insights_trend"):
            ins = cache.get("insights")
            return isinstance(ins, dict) and bool(ins.get("total_sessions"))
        if seg == "clock":
            return True  # Always has data
        entry = cache.get(seg)
        return isinstance(entry, dict) and len(entry) > 0

    def on_mount(self) -> None:
        self._refresh_preview()
        self.set_interval(3.0, self._refresh_preview)

    def _get_active_segments(self) -> list[str]:
        """Read currently toggled-on segments from the Switch widgets."""
        active = []
        for seg in ALL_SEGMENTS:
            try:
                switch = self.query_one(f"#seg-{seg}", Switch)
                if switch.value:
                    active.append(seg)
            except NoMatches:
                continue
        return active

    @staticmethod
    def _read_live_output() -> str | None:
        """Read the persisted status-line output if it exists and is fresh."""
        try:
            if not _LAST_STATUS_OUTPUT_PATH.exists():
                return None
            age = time.time() - _LAST_STATUS_OUTPUT_PATH.stat().st_mtime
            if age > _LIVE_OUTPUT_MAX_AGE:
                return None
            content = _LAST_STATUS_OUTPUT_PATH.read_text("utf-8").strip()
            return content if content else None
        except Exception:
            return None

    def _refresh_preview(self) -> None:
        # Try live output from the TS status-line command first
        live_output = self._read_live_output()
        if live_output is not None:
            preview = self.query_one("#preview-box", Container)
            preview.remove_children()
            preview.mount(Static(live_output, classes="preview-line"))
        else:
            self._refresh_preview_from_cache()

        self._refresh_tier_summary()

    def _refresh_preview_from_cache(self) -> None:
        config = read_config()
        cache = read_cache()
        status_config = config.get("status_line", {})
        if not isinstance(status_config, dict):
            status_config = {}

        delimiter = status_config.get("delimiter", " | ")
        active = self._get_active_segments()
        active_set = set(active)

        # Build 5-line preview matching the TS multi-tier layout
        lines: list[str] = []
        for line_segs in [FIXED_LINE_1, FIXED_LINE_2, FIXED_LINE_3, FIXED_LINE_4]:
            parts = []
            for seg in line_segs:
                if seg not in active_set:
                    continue
                text = self._render_segment(seg, cache)
                if text:
                    parts.append(text)
            if parts:
                lines.append(delimiter.join(parts))

        # Rotating line: show first non-empty (simulates rotation)
        for seg in ROTATING_SEGMENTS:
            if seg not in active_set:
                continue
            text = self._render_segment(seg, cache)
            if text:
                lines.append(text)
                break
        if lines:
            preview_text = "\n".join(lines)
        else:
            preview_text = "[dim]No active session — start Claude Code to see live preview[/dim]"

        preview = self.query_one("#preview-box", Container)
        preview.remove_children()
        preview.mount(Static(preview_text, classes="preview-line"))

    def _refresh_tier_summary(self) -> None:
        """Update the tier summary widget showing active segments per line."""
        config = read_config()
        status_config = config.get("status_line", {})
        if not isinstance(status_config, dict):
            status_config = {}

        delimiter = status_config.get("delimiter", " | ")
        active = self._get_active_segments()
        active_set = set(active)

        summary_lines = []
        for _, (label, segs) in enumerate([
            ("Line 1 (status)", FIXED_LINE_1),
            ("Line 2 (context)", FIXED_LINE_2),
            ("Line 3 (insights)", FIXED_LINE_3),
            ("Line 4 (trends)", FIXED_LINE_4),
        ], 1):
            active_segs = [s for s in segs if s in active_set]
            if active_segs:
                summary_lines.append(
                    f"[bold]{label}:[/bold] {delimiter.join(active_segs)}"
                )
        middle_active = [s for s in MIDDLE_SEGMENTS if s in active_set]
        if middle_active:
            summary_lines.append(
                f"[bold]Middle (agents):[/bold] {delimiter.join(middle_active)}"
            )
        rotating_active = [s for s in ROTATING_SEGMENTS if s in active_set]
        if rotating_active:
            summary_lines.append(
                f"[bold]Line 5 (rotating):[/bold] {' \u2192 '.join(rotating_active)}"
            )
        if not summary_lines:
            summary_lines.append("[dim]No segments active[/dim]")

        try:
            tier_summary = self.query_one("#tier-summary", Container)
            tier_summary.remove_children()
            tier_summary.mount(Static("\n".join(summary_lines)))
        except NoMatches:
            pass

    # Friction category → actionable tip (mirrors TS FRICTION_TIPS)
    _FRICTION_TIPS = {
        "wrong_approach": "break tasks into steps",
        "misunderstood_request": "be more specific",
        "stale_context": "try a fresh session",
        "tool_error": "check tool setup",
        "scope_creep": "define done upfront",
        "repeated_errors": "read error output first",
    }

    @staticmethod
    def _top_friction_tip(friction_counts: dict) -> str:
        """Find the top friction category and return its tip."""
        if not isinstance(friction_counts, dict):
            return ""
        top_cat = ""
        top_count = 0
        for cat, count in friction_counts.items():
            if isinstance(count, (int, float)) and count > top_count:
                top_cat = cat
                top_count = count
        if not top_cat:
            return ""
        human_name = top_cat.replace("_", " ")
        tip = StatusTab._FRICTION_TIPS.get(top_cat)
        return f"{human_name} \u2192 {tip}" if tip else human_name

    @staticmethod
    def _render_segment(seg: str, cache: dict) -> str:
        """Render a segment from cache data with real rendering logic."""
        # Segments that need live stdin data — skip in cache-only mode
        if seg in STDIN_SEGMENTS:
            return ""

        # -- mode_badge reads from builder_trap cache entry (not its own key) --
        if seg == "mode_badge":
            bt = cache.get("builder_trap")
            if not isinstance(bt, dict):
                return ""
            mode = bt.get("current_mode", "")
            if not mode or mode == "neutral":
                return ""
            return f"[{mode}]"

        # -- insights segments read from the shared "insights" cache entry --
        insights = cache.get("insights")
        if isinstance(insights, dict):
            if seg == "insights_friction":
                recent_friction = 0
                rs = insights.get("recent_session")
                if isinstance(rs, dict):
                    recent_friction = rs.get("friction_count", 0) or 0
                total_friction = insights.get("friction_total", 0) or 0
                friction_counts = insights.get("friction_counts", {})
                if recent_friction > 0:
                    tip = StatusTab._top_friction_tip(friction_counts)
                    if tip:
                        return f"\u26a0\ufe0f {recent_friction} friction this session \u00b7 {tip}"
                    return f"\u26a0\ufe0f {recent_friction} friction this session"
                window = insights.get("staleness_days", 30)
                if total_friction > 0:
                    return f"\u2705 Clean session \u00b7 {total_friction} in {window}d"
                return "\u2705 No friction detected"

            if seg == "insights_pace":
                total_msgs = insights.get("total_messages", 0) or 0
                days_active = insights.get("days_active", 1) or 1
                lines_added = insights.get("total_lines_added", 0) or 0
                sessions = insights.get("total_sessions", 0) or 0
                recent_mpd = insights.get("recent_msgs_per_day")
                msgs_per_day = round(total_msgs / days_active)
                if recent_mpd is None:
                    recent_mpd = msgs_per_day
                # Trend arrow
                if recent_mpd > msgs_per_day * 1.2:
                    arrow = "\u2191"
                elif recent_mpd < msgs_per_day * 0.8:
                    arrow = "\u2193"
                else:
                    arrow = "\u2192"
                # Format large numbers
                if lines_added >= 1000:
                    k = lines_added / 1000
                    fmt_lines = f"{int(k)}k" if k == int(k) else f"{k:.1f}k"
                else:
                    fmt_lines = str(lines_added)
                return f"\U0001f4ca {msgs_per_day} msgs/day {arrow} | {fmt_lines}+ lines | {sessions} sessions"

            if seg == "insights_trend":
                top_tools = insights.get("top_tools", [])
                peak_hour = insights.get("peak_hour", 0) or 0
                tool_names = ", ".join(t.get("name", "") for t in top_tools[:2] if isinstance(t, dict))
                if not tool_names:
                    return ""
                if 6 <= peak_hour < 12:
                    peak_label = "morning"
                elif 12 <= peak_hour < 18:
                    peak_label = "afternoon"
                elif 18 <= peak_hour < 24:
                    peak_label = "evening"
                else:
                    peak_label = "night"
                return f"\U0001f527 Top: {tool_names} | Peak: {peak_label}"

        # -- other segments with direct cache entries --
        entry = cache.get(seg)
        if not isinstance(entry, dict):
            return ""

        if seg == "mantra":
            return str(entry.get("text", ""))

        if seg == "project":
            repo = entry.get("repo", "")
            if not repo:
                return ""
            branch = entry.get("branch", "unknown")
            if entry.get("detached"):
                branch = "detached"
            parts = [f"\U0001f4e6 {repo} ({branch})"]
            ts = entry.get("last_commit_ts")
            if ts is not None:
                diff_s = int(time.time()) - int(ts)
                if diff_s < 3600:
                    parts.append(f"{diff_s // 60}m ago")
                elif diff_s < 86400:
                    parts.append(f"{diff_s // 3600}h ago")
                else:
                    parts.append(f"{diff_s // 86400}d ago")
            return " \u2022 ".join(parts)

        if seg == "calendar":
            events = entry.get("events", [])
            if not isinstance(events, list):
                events = []
            next_event = entry.get("next_event")
            current = next((e for e in events if isinstance(e, dict) and e.get("is_current")), None)
            if current:
                ends_in = ""
                try:
                    end_ms = int(datetime.fromisoformat(current["end"].replace("Z", "+00:00")).timestamp() * 1000)
                    now_ms = int(time.time() * 1000)
                    ends_in_min = round((end_ms - now_ms) / 60000)
                    ends_in = f" (ends in {ends_in_min}min)"
                except (ValueError, KeyError):
                    pass
                suffix = f" (+{len(events) - 1} more)" if len(events) > 1 else ""
                return f"\U0001f4c5 {current.get('title', '?')}{ends_in}{suffix}"
            if not isinstance(next_event, dict) or not next_event.get("title"):
                return "\U0001f4c5 Free"
            try:
                start_ms = int(datetime.fromisoformat(next_event["start"].replace("Z", "+00:00")).timestamp() * 1000)
            except (ValueError, KeyError):
                return f"\U0001f4c5 {next_event['title']}"
            now_ms = int(time.time() * 1000)
            diff_min = round((start_ms - now_ms) / 60000)
            other_count = len(events) - 1 if len(events) > 1 else 0
            more_suffix = f" (+{other_count} more)" if other_count > 0 else ""
            if diff_min < 5:
                text = f"\U0001f4c5 {next_event['title']} NOW"
            elif diff_min < 15:
                text = f"\U0001f4c5 {next_event['title']} in {diff_min}min \u26a1"
            elif diff_min <= 60:
                text = f"\U0001f4c5 {next_event['title']} in {diff_min}min"
            else:
                hours = round(diff_min / 60)
                text = f"\U0001f4c5 Free for {hours}h"
            return f"{text}{more_suffix}"

        if seg == "news":
            story = entry.get("current_story")
            if not isinstance(story, dict) or not story.get("title"):
                return ""
            title = story["title"]
            if len(title) > 45:
                title = title[:45] + "\u2026"
            score = story.get("score", 0)
            if isinstance(score, (int, float)) and score != 0:
                return f"\U0001f4f0 {title} ({score}pts)"
            return f"\U0001f4f0 {title}"

        if seg == "weather":
            temp = entry.get("temperature")
            emoji = entry.get("emoji", "\U0001f324\ufe0f")
            if temp is None:
                return f"{emoji} --"
            unit = "C" if entry.get("temperatureUnit") == "celsius" else "F"
            text = f"{emoji} {round(temp)}\u00b0{unit}"
            if (entry.get("windSpeed") or 0) > 20:
                text += " \U0001f4a8"
            return text

        if seg == "memories":
            if not entry.get("hasMemories"):
                return ""
            mems = entry.get("memories", [])
            if not mems:
                return ""
            count = len(mems)
            best = max(mems, key=lambda m: m.get("toolCalls", 0) if isinstance(m, dict) else 0)
            label = best.get("label", "") if isinstance(best, dict) else ""
            return f"\U0001f570\ufe0f On this day: {count} session{'s' if count != 1 else ''} ({label})"

        if seg == "pulse":
            val = entry.get("value", "")
            return str(val) if val else ""

        if seg == "streak":
            coding = entry.get("coding", 0)
            return f"\U0001f525 {coding}d streak" if coding else ""

        if seg == "builder_trap":
            level = entry.get("alertLevel", "none")
            if level == "none":
                return ""
            mins = round(entry.get("toolingMinutes", 0))
            return f"\u26a0\ufe0f {mins}m tooling"

        if seg == "agents":
            agents_list = entry.get("agents", [])
            if not isinstance(agents_list, list) or not agents_list:
                return ""
            active = [a for a in agents_list if isinstance(a, dict) and a.get("status") == "active"]
            count = len(active)
            if count == 0:
                return ""
            names = ", ".join(a.get("name", "?")[:15] for a in active[:3])
            suffix = f" +{count - 3}" if count > 3 else ""
            return f"\U0001f916 {count} agent{'s' if count != 1 else ''}: {names}{suffix}"

        if seg == "clock":
            return datetime.now().strftime("%I:%M %p").lstrip("0")

        # Generic fallback for unknown segments
        if "text" in entry:
            return str(entry["text"])
        if "value" in entry:
            return str(entry["value"])
        return ""

    def on_switch_changed(self, event: Switch.Changed) -> None:
        switch_id = event.switch.id or ""
        if not switch_id.startswith("seg-"):
            return

        segment_name = switch_id[4:]
        active = self._get_active_segments()

        config = read_config()
        if "status_line" not in config or not isinstance(config.get("status_line"), dict):
            config["status_line"] = {}
        config["status_line"]["segments"] = active

        if write_config(config):
            state = "enabled" if event.value else "disabled"
            self.notify(f"{segment_name} {state}")
        else:
            self.notify("Failed to save config", severity="error")

        self._refresh_preview()
