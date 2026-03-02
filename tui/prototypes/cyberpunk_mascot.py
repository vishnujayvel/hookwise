"""Prototype 3: Neon Cyberpunk Theme + ASCII Mascot.

Run: python3 prototypes/cyberpunk_mascot.py

Features:
- Neon cyberpunk color scheme (hot pink, electric cyan, acid green)
- Scanline effect on alternate rows
- ASCII mascot that reacts to dashboard stats
- Matrix rain easter egg (Ctrl+M)
- "Your Day in Code" narrative storytelling card
- Witty contextual loading messages
"""

import random
from datetime import datetime

from textual.app import App, ComposeResult
from textual.containers import Container, Horizontal, Vertical
from textual.widget import Widget
from textual.widgets import Static, Header, Footer, TabbedContent, TabPane
from textual.reactive import reactive
from rich.text import Text as RichText


# ─── ASCII Mascot ────────────────────────────────────────────────────

MASCOT_HAPPY = r"""
    ╭───╮
    │ ◠◠│
    │ ╰╯│ ♪
    ╰─┬─╯
      │
    ╭─┴─╮
    │   │
    ╰───╯
"""

MASCOT_EXCITED = r"""
    ╭───╮
    │ ★★│
    │ ╰╯│ ♫♪
    ╰─┬─╯
     \│/
    ╭─┴─╮
    │   │
    ╰───╯
"""

MASCOT_THINKING = r"""
    ╭───╮  ?
    │ ──│ ○
    │ ╰╯│○
    ╰─┬─╯
      │
    ╭─┴─╮
    │   │
    ╰───╯
"""

MASCOT_SLEEPING = r"""
    ╭───╮
    │ ──│ z
    │ ──│  z
    ╰─┬─╯  z
      │
    ╭─┴─╮
    │   │
    ╰───╯
"""

MASCOT_STRESSED = r"""
    ╭───╮
    │ ◉◉│ !
    │ ╰╯│ !
    ╰─┬─╯
      │ 💦
    ╭─┴─╮
    │   │
    ╰───╯
"""

MASCOT_CELEBRATE = r"""
    ╭───╮ 🎉
    │ ◠◠│
    │ ╰╯│
    ╰─┬─╯
    \\│//
    ╭─┴─╮
    │   │
    ╰───╯
"""


class AsciiMascot(Widget):
    """Hookwise mascot that reacts to dashboard stats."""

    DEFAULT_CSS = """
    AsciiMascot {
        height: 12;
        width: 22;
        padding: 0 1;
        border: round #ff6ec7;
        background: #0a0a1a;
    }
    """

    mood: reactive[str] = reactive("happy")
    blink: reactive[bool] = reactive(False)

    def on_mount(self) -> None:
        self.set_interval(3.0, self._blink)
        self.set_interval(10.0, self._change_mood)

    def _blink(self) -> None:
        self.blink = not self.blink

    def _change_mood(self) -> None:
        moods = ["happy", "excited", "thinking", "happy", "celebrate"]
        self.mood = random.choice(moods)

    def render(self) -> RichText:
        mascots = {
            "happy": MASCOT_HAPPY,
            "excited": MASCOT_EXCITED,
            "thinking": MASCOT_THINKING,
            "sleeping": MASCOT_SLEEPING,
            "stressed": MASCOT_STRESSED,
            "celebrate": MASCOT_CELEBRATE,
        }

        art = mascots.get(self.mood, MASCOT_HAPPY)

        mood_labels = {
            "happy": "[#ff6ec7]Hooky[/] is happy",
            "excited": "[#ff6ec7]Hooky[/] is excited!",
            "thinking": "[#ff6ec7]Hooky[/] is thinking...",
            "sleeping": "[#ff6ec7]Hooky[/] is sleeping",
            "stressed": "[#ff6ec7]Hooky[/] is stressed!",
            "celebrate": "[#ff6ec7]Hooky[/] celebrates!",
        }

        label = mood_labels.get(self.mood, "")
        return RichText.from_markup(f"[#00ffff]{art}[/]\n{label}")


# ─── Matrix Rain Easter Egg ──────────────────────────────────────────

MATRIX_CHARS = "ﾊﾐﾋｰｳｼﾅﾓﾆｻﾜﾂｵﾘｱﾎﾃﾏｹﾒｴｶｷﾑﾕﾗｾﾈｽﾀﾇﾍ0123456789ABCDEF"


class MatrixRain(Widget):
    """Matrix-style falling green characters."""

    DEFAULT_CSS = """
    MatrixRain {
        width: 100%;
        height: 100%;
    }
    """

    frame: reactive[int] = reactive(0)

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        self.columns: list[dict] = []
        self._width = 80
        self._height = 24
        self._active = True

    def on_mount(self) -> None:
        self._width = self.size.width or 80
        self._height = self.size.height or 24
        self._init_columns()
        self.set_interval(0.06, self._tick)
        # Auto-dismiss after 5 seconds
        self.set_timer(5.0, self._dismiss)

    def _dismiss(self) -> None:
        self._active = False
        self.display = False

    def _init_columns(self) -> None:
        self.columns = []
        for col in range(self._width):
            self.columns.append({
                "pos": random.randint(-self._height, 0),
                "speed": random.uniform(0.3, 1.0),
                "length": random.randint(5, 15),
                "chars": [random.choice(MATRIX_CHARS) for _ in range(20)],
            })

    def _tick(self) -> None:
        if not self._active:
            return
        for col in self.columns:
            col["pos"] += col["speed"]
            if col["pos"] > self._height + col["length"]:
                col["pos"] = random.randint(-self._height, -5)
                col["speed"] = random.uniform(0.3, 1.0)
                col["chars"] = [random.choice(MATRIX_CHARS) for _ in range(20)]
        self.frame = self.frame + 1

    def render(self) -> RichText:
        if not self._active:
            return RichText("")

        grid = [[" "] * self._width for _ in range(self._height)]
        brightness = [[0] * self._width for _ in range(self._height)]

        for col_idx, col in enumerate(self.columns):
            if col_idx >= self._width:
                break
            head = int(col["pos"])
            for i in range(col["length"]):
                row = head - i
                if 0 <= row < self._height:
                    char_idx = i % len(col["chars"])
                    grid[row][col_idx] = col["chars"][char_idx]
                    if i == 0:
                        brightness[row][col_idx] = 3  # head = white
                    elif i < 3:
                        brightness[row][col_idx] = 2  # bright green
                    else:
                        brightness[row][col_idx] = 1  # dim green

        text = RichText()
        for row_idx in range(self._height):
            for col_idx in range(self._width):
                char = grid[row_idx][col_idx]
                b = brightness[row_idx][col_idx]
                if b == 3:
                    text.append(char, style="bold white")
                elif b == 2:
                    text.append(char, style="bold #00ff00")
                elif b == 1:
                    text.append(char, style="#008800")
                else:
                    text.append(char, style="#001100")
            if row_idx < self._height - 1:
                text.append("\n")

        return text


# ─── Your Day in Code (Narrative Storytelling) ───────────────────────

NARRATIVES = [
    "You ran [bold cyan]12 sessions[/] today across [bold cyan]3 projects[/]. "
    "Your guards blocked [bold red]2[/] potentially risky operations. "
    "You're [bold #39d353]40% more active[/] than your typical {weekday}. "
    "Peak focus: [bold #ff6ec7]2pm-4pm[/].",

    "Quiet day so far — [bold cyan]4 sessions[/], mostly in the afternoon. "
    "Zero guard blocks means clean sailing. "
    "Your [bold #f9c74f]streak is intact[/] at 14 days. Keep it rolling.",

    "A heavy coding day — [bold cyan]18 sessions[/] and [bold cyan]3,200 lines[/] added. "
    "The insights producer caught [bold red]3 friction events[/] — all wrong_approach. "
    "Maybe time for a break? [bold #ff6ec7]Hooky thinks so[/].",

    "Classic morning sprint: [bold cyan]8 sessions[/] before noon, then quiet. "
    "Your most-used tool today: [bold #00ffff]Edit[/] (47 calls). "
    "AI authorship ratio: [bold #7b68ee]73%[/] — Claude is pulling its weight.",
]


class DayInCode(Widget):
    """Narrative storytelling card with typewriter effect."""

    DEFAULT_CSS = """
    DayInCode {
        height: auto;
        padding: 1 2;
        margin: 1;
        border: round #ff6ec7;
        background: #0a0a1a;
    }
    """

    chars_shown: reactive[int] = reactive(0)

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        weekday = datetime.now().strftime("%A")
        self.narrative = random.choice(NARRATIVES).replace("{weekday}", weekday)
        # Strip markup for length calculation
        self.plain_length = len(self.narrative)

    def on_mount(self) -> None:
        self.set_interval(0.02, self._type)

    def _type(self) -> None:
        if self.chars_shown < self.plain_length:
            self.chars_shown = min(self.chars_shown + 1, self.plain_length)

    def render(self) -> RichText:
        header = "[bold #ff6ec7]📝 Your Day in Code[/]\n\n"

        # Show full markup but truncate visible characters
        # For simplicity, show the whole thing progressively
        shown = self.narrative[:self.chars_shown]
        cursor = "█" if self.chars_shown < self.plain_length else ""

        return RichText.from_markup(f"{header}{shown}[dim]{cursor}[/]")


# ─── Neon Metric Card ────────────────────────────────────────────────

class NeonMetric(Widget):
    """Cyberpunk-styled metric card."""

    DEFAULT_CSS = """
    NeonMetric {
        height: auto;
        padding: 1 2;
        margin: 0 1 1 0;
        border: round #ff6ec7;
        background: #0a0a1a;
        width: 1fr;
    }
    """

    def __init__(self, label: str, value: str, icon: str = "", **kwargs):
        super().__init__(**kwargs)
        self.label = label
        self.value = value
        self.icon = icon

    def compose(self) -> ComposeResult:
        yield Static(
            f"[dim]{self.icon} {self.label}[/dim]\n"
            f"[bold #00ffff]{self.value}[/bold #00ffff]"
        )


# ─── Witty Loading Messages ─────────────────────────────────────────

LOADING_MESSAGES = [
    "Untangling hooks...",
    "Consulting the weather oracle...",
    "Counting your commits so you don't have to...",
    "Brewing Seattle's finest data...",
    "Polishing your sparklines...",
    "Asking the daemon nicely...",
    "Warming up the cache bus...",
    "Calibrating the flux capacitor...",
    "Downloading more RAM...",
    "Reticulating splines...",
    "Feeding the hamsters that power the daemon...",
    "Defragmenting your dopamine receptors...",
]


class WittyLoader(Widget):
    """Animated loading with witty messages."""

    DEFAULT_CSS = """
    WittyLoader {
        height: auto;
        padding: 1 2;
        margin: 1;
        border: dashed #ff6ec7;
        background: #0a0a1a;
    }
    """

    message_idx: reactive[int] = reactive(0)
    spinner_frame: reactive[int] = reactive(0)

    def on_mount(self) -> None:
        self.set_interval(2.0, self._next_message)
        self.set_interval(0.15, self._spin)

    def _next_message(self) -> None:
        self.message_idx = (self.message_idx + 1) % len(LOADING_MESSAGES)

    def _spin(self) -> None:
        self.spinner_frame = (self.spinner_frame + 1) % 10

    def render(self) -> RichText:
        frames = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"]
        spinner = frames[self.spinner_frame]
        msg = LOADING_MESSAGES[self.message_idx]

        return RichText.from_markup(
            f"[bold #ff6ec7]{spinner}[/] [dim italic]{msg}[/dim italic]"
        )


# ─── Scanline Background Effect ─────────────────────────────────────

class ScanlineOverlay(Widget):
    """Subtle scanline effect for cyberpunk aesthetic."""

    DEFAULT_CSS = """
    ScanlineOverlay {
        height: auto;
        padding: 0;
        margin: 1 2;
    }
    """

    def __init__(self, content: str, **kwargs):
        super().__init__(**kwargs)
        self.content_text = content

    def render(self) -> RichText:
        lines = self.content_text.split("\n")
        text = RichText()

        for i, line in enumerate(lines):
            if i % 2 == 0:
                text.append(line + "\n", style="")
            else:
                text.append(line + "\n", style="dim")

        return text


# ─── Main App ─────────────────────────────────────────────────────────

class CyberpunkMascotApp(App):
    """Prototype 3: Neon Cyberpunk + ASCII Mascot."""

    CSS = """
    Screen {
        background: #0a0a1a;
    }

    Header {
        background: #1a0a2e;
        color: #ff6ec7;
    }

    Footer {
        background: #1a0a2e;
        color: #00ffff;
    }

    TabPane {
        padding: 1;
    }

    TabbedContent ContentSwitcher {
        background: #0a0a1a;
    }

    Tab {
        color: #ff6ec7;
    }

    Tab.-active {
        color: #00ffff;
        text-style: bold;
    }

    .section-title {
        text-style: bold;
        color: #ff6ec7;
        margin: 1 0 0 2;
    }

    .neon-row {
        layout: horizontal;
        height: auto;
        margin: 0 0 1 0;
    }

    .mascot-row {
        layout: horizontal;
        height: auto;
        margin: 1 0;
    }

    .mascot-col {
        width: 24;
    }

    .narrative-col {
        width: 1fr;
    }

    .cyber-box {
        margin: 1 2;
        padding: 1 2;
        border: round #00ffff;
        background: #0a0a1a;
        height: auto;
    }

    #matrix-rain {
        layer: above;
        dock: top;
        width: 100%;
        height: 100%;
        display: none;
    }
    """

    TITLE = "hookwise — CYBER MODE"
    BINDINGS = [
        ("1", "switch_tab('dashboard')", "Dashboard"),
        ("2", "switch_tab('narrative')", "Story"),
        ("3", "switch_tab('loader')", "Loader"),
        ("m", "matrix", "Matrix"),
        ("q", "quit", "Quit"),
    ]

    def compose(self) -> ComposeResult:
        yield Header()
        yield MatrixRain(id="matrix-rain")

        with TabbedContent():
            with TabPane("Dashboard", id="dashboard"):
                yield Static(
                    "[bold #ff6ec7]╔══════════════════════════════════════════╗[/]\n"
                    "[bold #ff6ec7]║[/]  [bold #00ffff]H O O K W I S E[/]  "
                    "[dim]// NEON EDITION //[/]  [bold #ff6ec7]║[/]\n"
                    "[bold #ff6ec7]╚══════════════════════════════════════════╝[/]"
                )

                with Horizontal(classes="neon-row"):
                    yield NeonMetric("SESSIONS", "47", "▸")
                    yield NeonMetric("LINES", "2,841", "▸")
                    yield NeonMetric("BLOCKS", "12", "▸")
                    yield NeonMetric("STREAK", "14d 🔥", "▸")

                with Horizontal(classes="mascot-row"):
                    with Container(classes="mascot-col"):
                        yield AsciiMascot()
                    with Container(classes="narrative-col"):
                        yield DayInCode()

                with Container(classes="cyber-box"):
                    yield Static(
                        "[bold #00ffff]SYS.STATUS[/]\n"
                        "  [#39d353]●[/] Daemon:  [#39d353]ONLINE[/]  "
                        "[dim]PID 42069 • uptime 3h 47m[/dim]\n"
                        "  [#39d353]●[/] Feeds:   [#39d353]5/5 ACTIVE[/]  "
                        "[dim]pulse | project | calendar | news | insights[/dim]\n"
                        "  [#f9c74f]●[/] Guards:  [#f9c74f]12 RULES[/]  "
                        "[dim]2 blocks today • first-match-wins[/dim]\n"
                        "  [#ff6ec7]●[/] Recipes: [#ff6ec7]8 LOADED[/]  "
                        "[dim]friction-alert active[/dim]"
                    )

            with TabPane("Your Story", id="narrative"):
                yield Static("[bold #ff6ec7]// DATA NARRATIVE //[/]", classes="section-title")
                yield DayInCode()
                yield Static("")
                yield Static(
                    "  [bold #00ffff]TREND REPORT[/]\n\n"
                    "  Sessions/day  [#39d353]▁▂▃▄▅▆▇█▇▆▅▆▇█▇▅▄▅▆▇▅▃▄▅▆▇▅▆▇[/]  7\n"
                    "  Lines/day     [#58a6ff]▂▅▁▆▃▅▇▂▄█▃▅▆▂▄▇▅▃▆█▄▂▅▇▃▆▄▅▇▆[/]  440\n"
                    "  Friction      [#f85149]▂░▁▃░▁░▂▁░░▁▂░▁░▁░▂░▁░░▁░▂░▁░░[/]  0\n"
                )
                yield Static(
                    "  [dim]Press [bold]m[/bold] for a surprise...[/dim]"
                )

            with TabPane("Loading Demo", id="loader"):
                yield Static("[bold #ff6ec7]// LOADING MESSAGES //[/]", classes="section-title")
                yield Static(
                    "\n  [dim]hookwise replaces boring \"Loading...\" with personality:[/dim]\n"
                )
                yield WittyLoader()
                yield WittyLoader()
                yield WittyLoader()
                yield Static(
                    "\n  [dim]Each module gets its own set of contextual messages.[/dim]"
                )

        yield Footer()

    def action_switch_tab(self, tab_id: str) -> None:
        tabs = self.query_one(TabbedContent)
        tabs.active = tab_id

    def action_matrix(self) -> None:
        """Toggle Matrix rain easter egg."""
        matrix = self.query_one("#matrix-rain", MatrixRain)
        if not matrix.display:
            matrix.display = True
            matrix._active = True
            matrix._init_columns()
            # Auto-hide after 5 seconds
            self.set_timer(5.0, lambda: setattr(matrix, "display", False))

    def action_quit(self) -> None:
        self.exit()


if __name__ == "__main__":
    app = CyberpunkMascotApp()
    app.run()
