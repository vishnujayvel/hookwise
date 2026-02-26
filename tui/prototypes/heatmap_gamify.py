"""Prototype 2: Contribution Heatmap + Gamification Dashboard.

Run: python3 prototypes/heatmap_gamify.py

Features:
- GitHub-style contribution heatmap (52 weeks x 7 days) using block characters
- Streak counter with escalating fire emojis
- Achievement badges shelf with locked/unlocked states
- "Beat Last Week" boss battle comparison card
- Animated sparklines that grow on mount
"""

import random
from datetime import datetime, timedelta

from textual.app import App, ComposeResult
from textual.containers import Container, Horizontal, Vertical
from textual.widget import Widget
from textual.widgets import Static, Header, Footer, TabbedContent, TabPane
from textual.reactive import reactive
from rich.text import Text as RichText


# ─── Contribution Heatmap ────────────────────────────────────────────

HEAT_BLOCKS = ["░", "▒", "▓", "█"]
HEAT_COLORS = ["#1a1a2e", "#0e4429", "#006d32", "#26a641", "#39d353"]


class ContributionHeatmap(Widget):
    """GitHub-style contribution heatmap for Claude Code sessions."""

    DEFAULT_CSS = """
    ContributionHeatmap {
        height: auto;
        padding: 1 2;
        margin: 1;
        border: heavy #39d353;
        background: #0d1117;
    }
    """

    pulse: reactive[bool] = reactive(True)

    def on_mount(self) -> None:
        self.set_interval(1.0, self._pulse)

    def _pulse(self) -> None:
        self.pulse = not self.pulse

    def _generate_data(self) -> list[list[int]]:
        """Generate 52 weeks x 7 days of simulated activity data."""
        data = []
        today = datetime.now()
        start = today - timedelta(days=364)

        for week in range(52):
            week_data = []
            for day in range(7):
                date = start + timedelta(days=week * 7 + day)
                if date > today:
                    week_data.append(-1)  # future
                elif date.weekday() >= 5:  # weekends
                    week_data.append(random.choice([0, 0, 0, 1, 1, 2]))
                else:  # weekdays
                    week_data.append(random.choice([0, 1, 1, 2, 2, 3, 3, 4]))
            data.append(week_data)
        return data

    def render(self) -> RichText:
        data = self._generate_data()
        today = datetime.now()

        lines = []
        lines.append("  Claude Code Activity — Last 52 Weeks")
        lines.append("")

        day_labels = ["Mon", "   ", "Wed", "   ", "Fri", "   ", "Sun"]

        for day_idx in range(7):
            row = f"  {day_labels[day_idx]} "
            for week_idx in range(52):
                val = data[week_idx][day_idx]
                if val == -1:
                    row += " "
                elif val == 0:
                    row += "[#1a1a2e]░[/]"
                elif val == 1:
                    row += "[#0e4429]▒[/]"
                elif val == 2:
                    row += "[#006d32]▓[/]"
                elif val == 3:
                    row += "[#26a641]█[/]"
                else:
                    # Today's cell pulses
                    is_today = (week_idx == 51 and day_idx == today.weekday())
                    if is_today and self.pulse:
                        row += "[bold #39d353]█[/]"
                    else:
                        row += "[#39d353]█[/]"
            lines.append(row)

        lines.append("")
        lines.append("  [dim]Less[/] [#1a1a2e]░[/][#0e4429]▒[/][#006d32]▓[/][#26a641]█[/][#39d353]█[/] [dim]More[/]"
                      "                              [dim]Today pulses →[/]")

        return RichText.from_markup("\n".join(lines))


# ─── Streak Counter ──────────────────────────────────────────────────

class StreakCounter(Widget):
    """Streak counter with escalating fire visuals."""

    DEFAULT_CSS = """
    StreakCounter {
        height: auto;
        padding: 1 2;
        margin: 1;
        border: round #f77f00;
        background: #1a1018;
    }
    """

    streak: reactive[int] = reactive(14)
    flame_frame: reactive[int] = reactive(0)

    def on_mount(self) -> None:
        self.set_interval(0.3, self._animate_flame)

    def _animate_flame(self) -> None:
        self.flame_frame = (self.flame_frame + 1) % 4

    def render(self) -> RichText:
        streak = self.streak

        # Escalating fire based on streak length
        if streak >= 30:
            flames = "🔥🔥🔥🔥🔥"
            border_text = "LEGENDARY"
            color = "bold #ff4500"
            # Shimmer effect
            frames = [
                f"[{color}]╔══ {flames} ══╗[/]",
                f"[{color}]╠══ {flames} ══╣[/]",
                f"[{color}]╔══ {flames} ══╗[/]",
                f"[{color}]╠══ {flames} ══╣[/]",
            ]
            header = frames[self.flame_frame]
        elif streak >= 14:
            flames = "🔥🔥🔥"
            border_text = "ON FIRE"
            color = "bold #ff6b35"
            header = f"[{color}]── {flames} {border_text} {flames} ──[/]"
        elif streak >= 7:
            flames = "🔥🔥"
            border_text = "HOT STREAK"
            color = "bold #f4845f"
            header = f"[{color}]── {flames} {border_text} {flames} ──[/]"
        elif streak >= 3:
            flames = "🔥"
            border_text = "warming up"
            color = "#f9c74f"
            header = f"[{color}]── {flames} {border_text} ──[/]"
        else:
            flames = ""
            border_text = "building streak"
            color = "dim"
            header = f"[{color}]── {border_text} ──[/]"

        lines = [
            header,
            "",
            f"  Current Streak: [{color}]{streak} days[/]",
            f"  Best Streak:    [dim]32 days (Jan 2026)[/dim]",
            "",
            f"  [dim]Next milestone: {((streak // 7) + 1) * 7} days ({((streak // 7) + 1) * 7 - streak} to go)[/dim]",
        ]

        if streak >= 7:
            lines.append(f"  [dim italic]Streak freeze available: 1 skip/week[/dim italic]")

        return RichText.from_markup("\n".join(lines))


# ─── Achievement Badges ──────────────────────────────────────────────

BADGES = [
    ("🪝", "First Hook", "Configured your first hook", True),
    ("💯", "Century Club", "100 Claude sessions", True),
    ("🦉", "Night Owl", "Coded past midnight 10x", True),
    ("⚡", "Speed Demon", "Sub-30s average session", False),
    ("🧑‍🍳", "Recipe Master", "Used all 12 recipes", False),
    ("🛡️", "Guardian", "50 guard blocks saved", True),
    ("📊", "Data Nerd", "Checked analytics 30x", True),
    ("🌊", "Streaker", "14-day streak", True),
    ("🏔️", "Summit", "30-day streak", False),
    ("🎯", "Bullseye", "Zero guard blocks in a week", False),
    ("🧹", "Clean Coder", "Used communication coach", True),
    ("🌙", "Moonlighter", "Weekend coding warrior", True),
]


class AchievementBadges(Widget):
    """Achievement badges shelf with locked/unlocked states."""

    DEFAULT_CSS = """
    AchievementBadges {
        height: auto;
        padding: 1 2;
        margin: 1;
        border: round #7b68ee;
        background: #12101e;
    }
    """

    def render(self) -> RichText:
        lines = ["  [bold #7b68ee]Achievement Badges[/]    "
                 f"[dim]{sum(1 for _, _, _, u in BADGES if u)}/{len(BADGES)} unlocked[/dim]",
                 ""]

        row = "  "
        for i, (emoji, name, desc, unlocked) in enumerate(BADGES):
            if unlocked:
                row += f" {emoji} "
            else:
                row += " [dim]🔒[/dim] "

            if (i + 1) % 6 == 0:
                lines.append(row)
                row = "  "

        if row.strip():
            lines.append(row)

        lines.append("")

        # Show details for a few badges
        lines.append("  [dim]─── Recent Unlocks ───[/dim]")
        for emoji, name, desc, unlocked in BADGES:
            if unlocked:
                lines.append(f"  {emoji} [bold]{name}[/bold] — [dim]{desc}[/dim]")
            if len([l for l in lines if "──" not in l and l.strip()]) > 10:
                break

        # Show locked ones teaser
        lines.append("")
        lines.append("  [dim]─── Next to Unlock ───[/dim]")
        for emoji, name, desc, unlocked in BADGES:
            if not unlocked:
                lines.append(f"  🔒 [dim]{name} — {desc}[/dim]")
                break

        return RichText.from_markup("\n".join(lines))


# ─── Weekly Boss Battle ──────────────────────────────────────────────

class WeeklyBattle(Widget):
    """Beat Last Week comparison card."""

    DEFAULT_CSS = """
    WeeklyBattle {
        height: auto;
        padding: 1 2;
        margin: 1;
        border: round #f9c74f;
        background: #1a1810;
    }
    """

    celebration_frame: reactive[int] = reactive(0)

    def on_mount(self) -> None:
        self.set_interval(0.5, self._celebrate)

    def _celebrate(self) -> None:
        self.celebration_frame = (self.celebration_frame + 1) % 4

    def render(self) -> RichText:
        metrics = [
            ("Sessions",      47, 52, True),   # more = better
            ("Lines Added",   2100, 2841, True),
            ("Guard Blocks",  15, 8, False),    # less = better
            ("Recipes Used",  6, 8, True),
            ("Friction Events", 12, 5, False),
        ]

        wins = sum(1 for _, last, this, higher_better in metrics
                   if (this > last) == higher_better)
        total = len(metrics)

        if wins == total:
            celebration = ["🎉 VICTORY! 🎉", "🏆 VICTORY! 🏆", "⭐ VICTORY! ⭐", "🎊 VICTORY! 🎊"]
            header = f"  [bold #39d353]{celebration[self.celebration_frame]}[/]"
        elif wins > total // 2:
            header = "  [bold #f9c74f]⚔️  Weekly Boss Battle — Winning![/]"
        else:
            header = "  [bold #f77f00]⚔️  Weekly Boss Battle — Keep Going![/]"

        lines = [header, ""]
        lines.append(f"  {'Metric':<18} {'Last Wk':>10} {'This Wk':>10}  {'':>4}")
        lines.append(f"  {'─' * 48}")

        for name, last, this, higher_better in metrics:
            if (this > last) == higher_better:
                status = "[green]✓[/green]"
                value_style = "bold green"
            elif this == last:
                status = "[dim]=[/dim]"
                value_style = "dim"
            else:
                status = "[red]✗[/red]"
                value_style = "red"

            pct = ((this - last) / max(last, 1)) * 100
            trend = f"{'↑' if pct > 0 else '↓'} {abs(pct):.0f}%"

            lines.append(
                f"  {name:<18} [dim]{last:>10,}[/dim] [{value_style}]{this:>10,}[/]  {status} [dim]{trend}[/dim]"
            )

        lines.append("")
        lines.append(f"  [dim]Score: {wins}/{total} metrics beaten[/dim]")

        return RichText.from_markup("\n".join(lines))


# ─── Animated Sparkline ──────────────────────────────────────────────

class AnimatedSparkline(Widget):
    """Sparkline that animates in from left to right."""

    DEFAULT_CSS = """
    AnimatedSparkline {
        height: 2;
        padding: 0 2;
        margin: 0 0 0 0;
    }
    """

    BARS = "▁▂▃▄▅▆▇█"

    visible: reactive[int] = reactive(0)

    def __init__(self, label: str, values: list[int], color: str = "cyan", **kwargs):
        super().__init__(**kwargs)
        self.label = label
        self.all_values = values
        self.color = color

    def on_mount(self) -> None:
        self.set_interval(0.04, self._grow)

    def _grow(self) -> None:
        if self.visible < len(self.all_values):
            self.visible = self.visible + 1

    def render(self) -> RichText:
        values = self.all_values[:self.visible]
        if not values:
            return RichText(f"  {self.label}: ")

        max_val = max(max(self.all_values), 1)
        bars = ""
        for v in values:
            idx = min(int(v / max_val * (len(self.BARS) - 1)), len(self.BARS) - 1)
            bars += self.BARS[idx]

        current = values[-1] if values else 0
        return RichText.from_markup(
            f"  {self.label}: [{self.color}]{bars}[/] {current}"
        )


# ─── Main App ─────────────────────────────────────────────────────────

class HeatmapGamifyApp(App):
    """Prototype 2: Contribution Heatmap + Gamification."""

    CSS = """
    Screen {
        background: #0d1117;
    }

    TabPane {
        padding: 1;
    }

    .section-title {
        text-style: bold;
        color: #58a6ff;
        margin: 1 0 0 2;
    }

    .trends-box {
        margin: 1 2;
        padding: 1 2;
        border: round #30363d;
        background: #161b22;
        height: auto;
    }

    .gamification-row {
        layout: horizontal;
        height: auto;
    }

    .gamification-col {
        width: 1fr;
    }
    """

    TITLE = "hookwise — Gamification Mode"
    BINDINGS = [
        ("1", "switch_tab('overview')", "Overview"),
        ("2", "switch_tab('achievements')", "Achievements"),
        ("3", "switch_tab('battle')", "Battle"),
        ("up", "bump_streak(1)", "+Streak"),
        ("down", "bump_streak(-1)", "-Streak"),
        ("q", "quit", "Quit"),
    ]

    def compose(self) -> ComposeResult:
        yield Header()

        with TabbedContent():
            with TabPane("Overview", id="overview"):
                yield ContributionHeatmap()
                yield StreakCounter(id="streak")

                yield Static("Session Trends", classes="section-title")
                with Container(classes="trends-box"):
                    sessions = [3, 5, 2, 7, 4, 6, 8, 3, 5, 9, 4, 6, 7, 3, 5,
                                8, 6, 4, 7, 9, 5, 3, 6, 8, 4, 7, 5, 6, 8, 7]
                    lines_data = [120, 340, 80, 450, 200, 380, 560, 150, 290, 620,
                                  180, 350, 470, 130, 280, 510, 320, 200, 400, 580,
                                  250, 140, 370, 520, 190, 430, 260, 350, 510, 440]
                    friction = [2, 0, 1, 3, 0, 1, 0, 2, 1, 0, 0, 1, 2, 0, 1,
                                0, 1, 0, 2, 0, 1, 0, 0, 1, 0, 2, 0, 1, 0, 0]

                    yield AnimatedSparkline("Sessions/day", sessions, "#39d353")
                    yield AnimatedSparkline("Lines/day   ", lines_data, "#58a6ff")
                    yield AnimatedSparkline("Friction    ", friction, "#f85149")

            with TabPane("Achievements", id="achievements"):
                yield AchievementBadges()
                yield Static(
                    "\n  [dim]Badges are earned automatically based on your Claude Code usage.\n"
                    "  Keep coding to unlock them all![/dim]\n\n"
                    "  [bold #7b68ee]Rarest badge:[/] 🏔️ Summit (30-day streak) — "
                    "[dim]Only 2% of users achieve this[/dim]",
                )

            with TabPane("Weekly Battle", id="battle"):
                yield WeeklyBattle()
                yield Static(
                    "\n  [dim]The Weekly Boss Battle resets every Monday.\n"
                    "  Beat all 5 metrics to earn the Victory badge.\n"
                    "  3 consecutive victories = \"Unstoppable\" achievement![/dim]"
                )

        yield Footer()

    def action_switch_tab(self, tab_id: str) -> None:
        tabs = self.query_one(TabbedContent)
        tabs.active = tab_id

    def action_bump_streak(self, delta: int) -> None:
        streak = self.query_one("#streak", StreakCounter)
        streak.streak = max(0, streak.streak + delta)

    def action_quit(self) -> None:
        self.exit()


if __name__ == "__main__":
    app = HeatmapGamifyApp()
    app.run()
