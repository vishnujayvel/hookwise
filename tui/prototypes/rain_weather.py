"""Prototype 1: Seattle Rain Particles + Weather-Themed Dashboard.

Run: python3 prototypes/rain_weather.py

Features:
- Multi-layer rain with varying speeds, wind gusts, and splash effects
- Drifting clouds across the top of the screen
- Lightning flashes during thunderstorm mode
- Gentle snowfall with sinusoidal swaying
- Sun mode with twinkling stars and floating dust motes
- Wind gusts that periodically shift all particle drift
- Auto-detect weather via Open-Meteo (no API key)
- Time-of-day color palette shifts
- Pulsing heartbeat for daemon status
"""

import random
import math
import urllib.request
import json
from datetime import datetime

from textual.app import App, ComposeResult
from textual.containers import Container, Horizontal
from textual.widget import Widget
from textual.widgets import Static, Header, Footer, TabbedContent, TabPane
from textual.reactive import reactive
from rich.text import Text as RichText


# ─── Weather Data Fetcher ────────────────────────────────────────────

WEATHER_CODE_MAP = {
    0: "clear", 1: "clear", 2: "cloudy", 3: "cloudy",
    45: "fog", 48: "fog",
    51: "drizzle", 53: "drizzle", 55: "drizzle",
    61: "rain", 63: "rain", 65: "heavy_rain",
    66: "rain", 67: "heavy_rain",
    71: "snow", 73: "snow", 75: "heavy_snow",
    77: "snow",
    80: "rain", 81: "rain", 82: "heavy_rain",
    85: "snow", 86: "heavy_snow",
    95: "thunderstorm", 96: "thunderstorm", 99: "thunderstorm",
}


def fetch_weather() -> dict:
    """Fetch real weather via IP geolocation + Open-Meteo. No API key needed."""
    try:
        # Step 1: Get location from IP
        req = urllib.request.Request("https://ipinfo.io/json", headers={"User-Agent": "hookwise-tui"})
        with urllib.request.urlopen(req, timeout=3) as resp:
            loc = json.loads(resp.read())
        city = loc.get("city", "Unknown")
        lat, lon = loc.get("loc", "47.6,-122.3").split(",")

        # Step 2: Get weather
        url = (f"https://api.open-meteo.com/v1/forecast?"
               f"latitude={lat}&longitude={lon}&current_weather=true")
        req2 = urllib.request.Request(url, headers={"User-Agent": "hookwise-tui"})
        with urllib.request.urlopen(req2, timeout=3) as resp:
            weather = json.loads(resp.read())

        cw = weather.get("current_weather", {})
        code = cw.get("weathercode", 0)
        temp_c = cw.get("temperature", 10)
        wind_speed = cw.get("windspeed", 5)

        return {
            "city": city,
            "condition": WEATHER_CODE_MAP.get(code, "cloudy"),
            "code": code,
            "temp_c": temp_c,
            "temp_f": round(temp_c * 9 / 5 + 32),
            "wind_speed": wind_speed,
        }
    except Exception:
        return {
            "city": "Seattle",
            "condition": "rain",
            "code": 61,
            "temp_c": 9,
            "temp_f": 48,
            "wind_speed": 12,
        }


# ─── Particle Types ──────────────────────────────────────────────────

class Particle:
    """A weather particle with physics."""
    __slots__ = ("x", "y", "char", "speed", "drift", "opacity", "layer")

    def __init__(self, x: float, y: float, char: str, speed: float,
                 drift: float = 0.0, opacity: int = 2, layer: int = 0):
        self.x = x
        self.y = y
        self.char = char
        self.speed = speed
        self.drift = drift
        self.opacity = opacity  # 0=dim, 1=normal, 2=bright
        self.layer = layer      # 0=far, 1=mid, 2=near


class Splash:
    """A splash effect when rain hits the ground."""
    __slots__ = ("x", "y", "life", "max_life")

    def __init__(self, x: float, y: float):
        self.x = x
        self.y = y
        self.life = 0
        self.max_life = random.randint(3, 6)


class Cloud:
    """A drifting cloud formation."""
    __slots__ = ("x", "y", "shape", "speed", "width")

    SHAPES = [
        ["  .-~~~-.", " /       \\", "(  ~cloud~ )", " \\_______/"],
        ["  .--.", " /    \\", "(  ~~  )", " \\__/"],
        ["    .-~~-.", "  /      \\", " (  ~~~~  )", "  \\______/"],
        [" .~.", "/   \\", "( ~ )", " '-'"],
    ]

    def __init__(self, x: float, y: float, speed: float, shape_idx: int = 0):
        self.x = x
        self.y = y
        self.shape = self.SHAPES[shape_idx % len(self.SHAPES)]
        self.speed = speed
        self.width = max(len(line) for line in self.shape)


# ─── Weather Background Engine ───────────────────────────────────────

class WeatherBackground(Widget):
    """Multi-layer animated weather with particles, clouds, and effects."""

    DEFAULT_CSS = """
    WeatherBackground {
        width: 100%;
        height: 100%;
        layer: below;
    }
    """

    weather: reactive[str] = reactive("rain")
    frame: reactive[int] = reactive(0)

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        self.particles: list[Particle] = []
        self.splashes: list[Splash] = []
        self.clouds: list[Cloud] = []
        self._width = 80
        self._height = 24
        self._wind = 0.0          # current wind drift offset
        self._wind_target = 0.0   # target wind (lerps toward this)
        self._gust_timer = 0
        self._lightning_flash = 0  # frames remaining for lightning
        self._lightning_timer = 0
        self._time_offset = 0.0   # for sinusoidal animations

    def on_mount(self) -> None:
        self._width = self.size.width or 80
        self._height = self.size.height or 24
        self._spawn_all()
        self.set_interval(0.06, self._tick)

    def on_resize(self) -> None:
        self._width = self.size.width or 80
        self._height = self.size.height or 24

    def _spawn_all(self) -> None:
        self.particles = []
        self.splashes = []
        self.clouds = []
        self._spawn_clouds()
        self._spawn_particles()

    def _spawn_clouds(self) -> None:
        self.clouds = []
        if self.weather in ("clear", "sun"):
            count = 1
        elif self.weather in ("fog",):
            count = 6
        else:
            count = random.randint(2, 4)

        for i in range(count):
            self.clouds.append(Cloud(
                x=random.uniform(-20, self._width + 20),
                y=random.randint(0, 3),
                speed=random.uniform(0.02, 0.08),
                shape_idx=random.randint(0, 3),
            ))

    def _spawn_particles(self) -> None:
        self.particles = []
        w, h = self._width, self._height

        if self.weather in ("rain", "drizzle"):
            density = w if self.weather == "rain" else w // 3
            chars_near = ["|", "│", "┃"]
            chars_mid = ["╎", "┊", "/"]
            chars_far = [".", ",", "'"]
            for _ in range(density):
                layer = random.choices([0, 1, 2], weights=[3, 4, 3])[0]
                if layer == 2:
                    chars, spd, opac = chars_near, random.uniform(1.0, 1.8), 2
                elif layer == 1:
                    chars, spd, opac = chars_mid, random.uniform(0.6, 1.0), 1
                else:
                    chars, spd, opac = chars_far, random.uniform(0.3, 0.5), 0
                self.particles.append(Particle(
                    x=random.uniform(0, w),
                    y=random.uniform(-h, h),
                    char=random.choice(chars),
                    speed=spd,
                    drift=random.uniform(-0.1, 0.05),
                    opacity=opac,
                    layer=layer,
                ))

        elif self.weather == "heavy_rain":
            for _ in range(int(w * 1.5)):
                layer = random.choices([0, 1, 2], weights=[2, 3, 5])[0]
                chars = ["|", "│", "┃", "║"] if layer >= 1 else ["╎", "/", "."]
                self.particles.append(Particle(
                    x=random.uniform(0, w),
                    y=random.uniform(-h, h),
                    char=random.choice(chars),
                    speed=random.uniform(1.2, 2.5),
                    drift=random.uniform(-0.3, -0.05),
                    opacity=min(layer + 1, 2),
                    layer=layer,
                ))

        elif self.weather == "thunderstorm":
            for _ in range(int(w * 1.8)):
                layer = random.choices([0, 1, 2], weights=[2, 3, 5])[0]
                chars = ["|", "│", "┃", "║"] if layer >= 1 else ["╎", "/"]
                self.particles.append(Particle(
                    x=random.uniform(0, w),
                    y=random.uniform(-h, h),
                    char=random.choice(chars),
                    speed=random.uniform(1.5, 3.0),
                    drift=random.uniform(-0.5, -0.1),
                    opacity=min(layer + 1, 2),
                    layer=layer,
                ))

        elif self.weather in ("snow", "heavy_snow"):
            density = w // 2 if self.weather == "snow" else w
            chars = ["*", "·", "•", "∘", "◦", "✦", "❄", "❆", "✻", "⁂"]
            for _ in range(density):
                layer = random.choices([0, 1, 2], weights=[4, 4, 2])[0]
                self.particles.append(Particle(
                    x=random.uniform(0, w),
                    y=random.uniform(-h, h),
                    char=random.choice(chars),
                    speed=random.uniform(0.08, 0.25) * (1 + layer * 0.3),
                    drift=0.0,  # drift is computed dynamically via sine wave
                    opacity=layer,
                    layer=layer,
                ))

        elif self.weather in ("clear", "sun"):
            chars = ["✦", "·", "˚", "°", "⁺", "∗"]
            for _ in range(w // 6):
                self.particles.append(Particle(
                    x=random.uniform(0, w),
                    y=random.uniform(0, h),
                    char=random.choice(chars),
                    speed=0.0,
                    drift=0.0,
                    opacity=random.randint(0, 2),
                    layer=0,
                ))
            # Floating dust motes
            for _ in range(w // 8):
                self.particles.append(Particle(
                    x=random.uniform(0, w),
                    y=random.uniform(0, h),
                    char=random.choice(["·", "∘", "'"]),
                    speed=random.uniform(0.02, 0.06),
                    drift=random.uniform(0.01, 0.04),
                    opacity=0,
                    layer=1,
                ))

        elif self.weather == "fog":
            fog_chars = ["░", "▒", "·", "∘", " "]
            for _ in range(w * 2):
                self.particles.append(Particle(
                    x=random.uniform(0, w),
                    y=random.uniform(0, h),
                    char=random.choice(fog_chars),
                    speed=0.0,
                    drift=random.uniform(0.01, 0.05),
                    opacity=random.randint(0, 1),
                    layer=0,
                ))

        elif self.weather == "cloudy":
            # Just a few scattered drops
            for _ in range(w // 6):
                self.particles.append(Particle(
                    x=random.uniform(0, w),
                    y=random.uniform(0, h),
                    char=random.choice(["·", "∘", "'"]),
                    speed=random.uniform(0.01, 0.04),
                    drift=random.uniform(-0.02, 0.02),
                    opacity=0,
                    layer=0,
                ))

    def _tick(self) -> None:
        self._time_offset += 0.06
        w, h = self._width, self._height

        # Wind gusts
        self._gust_timer += 1
        if self._gust_timer > random.randint(60, 180):
            self._gust_timer = 0
            if self.weather in ("rain", "heavy_rain", "drizzle", "thunderstorm"):
                self._wind_target = random.uniform(-0.6, 0.1)
            elif self.weather in ("snow", "heavy_snow"):
                self._wind_target = random.uniform(-0.3, 0.3)
            else:
                self._wind_target = random.uniform(-0.05, 0.05)

        # Lerp wind toward target
        self._wind += (self._wind_target - self._wind) * 0.03
        # Wind decays back to calm
        self._wind_target *= 0.998

        # Lightning timer (thunderstorm only)
        if self.weather == "thunderstorm":
            self._lightning_timer += 1
            if self._lightning_timer > random.randint(80, 250):
                self._lightning_timer = 0
                self._lightning_flash = random.randint(2, 5)
        if self._lightning_flash > 0:
            self._lightning_flash -= 1

        # Update clouds
        for cloud in self.clouds:
            cloud.x += cloud.speed + self._wind * 0.3
            if cloud.x > w + 30:
                cloud.x = -cloud.width - random.randint(5, 30)
                cloud.y = random.randint(0, 3)
            elif cloud.x < -cloud.width - 30:
                cloud.x = w + random.randint(5, 30)

        # Update particles
        for p in self.particles:
            if self.weather in ("snow", "heavy_snow"):
                # Sinusoidal sway for snow
                sway = math.sin(self._time_offset * 1.5 + p.x * 0.1 + p.layer) * 0.15
                p.x += sway + self._wind * (0.5 + p.layer * 0.2)
                p.y += p.speed
            elif self.weather in ("clear", "sun") and p.layer == 0:
                # Twinkling: randomly change opacity
                if random.random() < 0.03:
                    p.opacity = (p.opacity + 1) % 3
                # Dust motes don't fall
                continue
            elif self.weather == "fog":
                p.x += p.drift + self._wind * 0.2
                # Fog particles slowly shift vertically
                p.y += math.sin(self._time_offset + p.x * 0.05) * 0.02
            else:
                # Rain/drizzle/thunderstorm
                p.y += p.speed
                p.x += p.drift + self._wind * (0.3 + p.layer * 0.2)

            # Dust motes in sun mode
            if self.weather in ("clear", "sun") and p.layer == 1:
                p.y += p.speed
                p.x += p.drift + math.sin(self._time_offset * 0.5 + p.y * 0.2) * 0.03

            # Respawn at top when off bottom
            if p.y > h:
                # Create splash for rain
                if self.weather in ("rain", "heavy_rain", "thunderstorm") and p.layer >= 1:
                    if random.random() < 0.3:
                        self.splashes.append(Splash(p.x, h - 1))
                p.y = random.uniform(-4, -1)
                p.x = random.uniform(0, w)

            # Wrap horizontally
            if p.x < 0:
                p.x += w
            elif p.x >= w:
                p.x -= w

        # Update splashes
        new_splashes = []
        for s in self.splashes:
            s.life += 1
            if s.life < s.max_life:
                new_splashes.append(s)
        self.splashes = new_splashes

        self.frame = self.frame + 1

    def watch_weather(self, new_weather: str) -> None:
        self._wind = 0.0
        self._wind_target = 0.0
        self._lightning_flash = 0
        self._spawn_all()

    def render(self) -> RichText:
        w, h = self._width, self._height
        if w <= 0 or h <= 0:
            return RichText("")

        # Build grid with style info: (char, style)
        grid = [[(" ", "")] * w for _ in range(h)]

        # Layer 0: Clouds
        for cloud in self.clouds:
            cx = int(cloud.x)
            for row_idx, line in enumerate(cloud.shape):
                ry = int(cloud.y) + row_idx
                if 0 <= ry < h:
                    for col_idx, ch in enumerate(line):
                        rx = cx + col_idx
                        if 0 <= rx < w and ch != " ":
                            grid[ry][rx] = (ch, "dim white")

        # Layer 1: Particles
        for p in self.particles:
            px, py = int(p.x), int(p.y)
            if 0 <= px < w and 0 <= py < h:
                if self.weather in ("rain", "heavy_rain", "drizzle", "thunderstorm"):
                    styles = ["#1a3a5c", "#4488bb", "bold #66bbff"]
                elif self.weather in ("snow", "heavy_snow"):
                    styles = ["#555577", "#9999bb", "bold #ddeeff"]
                elif self.weather in ("clear", "sun"):
                    styles = ["#554400", "#aa8800", "bold #ffdd44"]
                elif self.weather == "fog":
                    styles = ["#333344", "#555566", "#777788"]
                else:
                    styles = ["dim", "", "bold"]
                style = styles[min(p.opacity, len(styles) - 1)]
                grid[py][px] = (p.char, style)

        # Layer 2: Splash effects
        splash_frames = [
            ["·", "∘", "·"],
            ["~", "∘", "~"],
            ["-", "·", "-"],
            [".", " ", "."],
        ]
        for s in self.splashes:
            sx = int(s.x)
            sy = int(s.y)
            if 0 <= sy < h:
                frame_idx = min(s.life, len(splash_frames) - 1)
                splash = splash_frames[frame_idx]
                for i, ch in enumerate(splash):
                    rx = sx - 1 + i
                    if 0 <= rx < w and ch != " ":
                        grid[sy][rx] = (ch, "#4488bb")

        # Build Rich text with per-character styling
        text = RichText()
        for row_idx in range(h):
            for col_idx in range(w):
                char, style = grid[row_idx][col_idx]

                # Lightning flash: override all styles to bright white briefly
                if self._lightning_flash > 0 and char != " ":
                    if self._lightning_flash >= 3:
                        style = "bold white on #333355"
                    else:
                        style = "bold #aaaacc"

                if style:
                    text.append(char, style=style)
                else:
                    text.append(char)

            if row_idx < h - 1:
                text.append("\n")

        return text


# ─── Pulsing Heartbeat Indicator ─────────────────────────────────────

class HeartbeatIndicator(Widget):
    """Pulsing dot that indicates daemon is alive."""

    DEFAULT_CSS = """
    HeartbeatIndicator {
        width: auto;
        height: 1;
    }
    """

    beat: reactive[int] = reactive(0)
    alive: reactive[bool] = reactive(True)

    def on_mount(self) -> None:
        self.set_interval(0.4, self._pulse)

    def _pulse(self) -> None:
        if self.alive:
            self.beat = (self.beat + 1) % 4

    def render(self) -> RichText:
        if not self.alive:
            return RichText("○ Daemon: offline", style="dim red")

        # Heartbeat: bright → dim → dim → bright (lub-dub pattern)
        styles = ["bold #00ff88", "#00cc66", "dim #006633", "#00cc66"]
        dots = ["●", "●", "·", "●"]
        return RichText(f"{dots[self.beat]} Daemon: running", style=styles[self.beat])


# ─── Time-of-Day Theme ──────────────────────────────────────────────

def get_time_theme() -> dict:
    hour = datetime.now().hour
    if 6 <= hour < 9:
        return {"name": "Dawn", "emoji": "🌅", "accent": "#f4845f"}
    elif 9 <= hour < 12:
        return {"name": "Morning", "emoji": "☀️", "accent": "#f9c74f"}
    elif 12 <= hour < 17:
        return {"name": "Afternoon", "emoji": "🌤️", "accent": "#4cc9f0"}
    elif 17 <= hour < 20:
        return {"name": "Golden Hour", "emoji": "🌇", "accent": "#f77f00"}
    elif 20 <= hour < 23:
        return {"name": "Evening", "emoji": "🌙", "accent": "#7b68ee"}
    else:
        return {"name": "Night", "emoji": "🌑", "accent": "#4a4a8a"}


# ─── Weather Info Card ───────────────────────────────────────────────

class WeatherCard(Widget):
    DEFAULT_CSS = """
    WeatherCard {
        height: auto;
        padding: 1 2;
        margin: 1;
        border: heavy $accent;
        background: $surface;
    }
    """

    def __init__(self, weather_data: dict, **kwargs):
        super().__init__(**kwargs)
        self.weather_data = weather_data

    def compose(self) -> ComposeResult:
        theme = get_time_theme()
        wd = self.weather_data
        condition_emoji = {
            "clear": "☀️ Clear", "cloudy": "☁️ Cloudy", "fog": "🌫️ Fog",
            "drizzle": "🌦️ Drizzle", "rain": "🌧️ Rain", "heavy_rain": "🌧️ Heavy Rain",
            "snow": "🌨️ Snow", "heavy_snow": "❄️ Heavy Snow",
            "thunderstorm": "⛈️ Thunderstorm",
        }
        cond = condition_emoji.get(wd["condition"], "🌤️ " + wd["condition"])

        yield Static(
            f"{theme['emoji']} {theme['name']} in {wd['city']}  •  {cond}\n"
            f"[dim]{wd['temp_f']}°F / {wd['temp_c']}°C  •  "
            f"Wind: {wd['wind_speed']} km/h[/dim]\n"
            f"[dim italic]Live weather via Open-Meteo • Theme shifts with time of day[/dim italic]"
        )


# ─── Metric Card ────────────────────────────────────────────────────

class MetricCard(Widget):
    DEFAULT_CSS = """
    MetricCard {
        height: auto;
        padding: 1 2;
        margin: 0 1 1 0;
        border: round $primary;
        background: $surface-darken-1;
        width: 1fr;
    }
    """

    def __init__(self, label: str, value: str, trend: str = "", **kwargs):
        super().__init__(**kwargs)
        self.label = label
        self.value = value
        self.trend = trend

    def compose(self) -> ComposeResult:
        yield Static(f"{self.label}\n[bold cyan]{self.value}[/bold cyan] {self.trend}")


# ─── Animated Sparkline ─────────────────────────────────────────────

class SparklineDemo(Widget):
    DEFAULT_CSS = """
    SparklineDemo {
        height: 2;
        padding: 0 2;
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
            self.visible += 1

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
        return RichText.from_markup(f"  {self.label}: [{self.color}]{bars}[/] {current}")


# ─── Main App ─────────────────────────────────────────────────────────

CONDITION_TO_WEATHER = {
    "clear": "sun", "cloudy": "cloudy", "fog": "fog",
    "drizzle": "drizzle", "rain": "rain", "heavy_rain": "heavy_rain",
    "snow": "snow", "heavy_snow": "heavy_snow",
    "thunderstorm": "thunderstorm",
}


class RainWeatherApp(App):
    """Prototype 1: Weather-Animated Dashboard with live data."""

    CSS = """
    Screen {
        layers: below content;
        background: #0a1520;
    }

    #weather-bg {
        layer: below;
        dock: top;
        width: 100%;
        height: 100%;
        position: absolute;
    }

    #content-layer {
        layer: content;
        width: 100%;
        height: 100%;
    }

    .metric-row {
        layout: horizontal;
        height: auto;
        margin: 0 0 1 0;
    }

    .section-title {
        text-style: bold;
        color: #4cc9f0;
        margin: 1 0 0 2;
    }

    .activity-section {
        margin: 1 2;
        padding: 1 2;
        border: round #4cc9f0;
        background: rgba(10, 21, 32, 0.88);
        height: auto;
    }

    TabPane {
        padding: 1;
        background: rgba(10, 21, 32, 0.75);
    }
    """

    TITLE = "hookwise"
    BINDINGS = [
        ("1", "switch_tab('dashboard')", "Dashboard"),
        ("2", "switch_tab('feeds')", "Feeds"),
        ("3", "switch_tab('weather')", "Weather Ctrl"),
        ("r", "set_weather('rain')", "Rain"),
        ("d", "set_weather('drizzle')", "Drizzle"),
        ("t", "set_weather('thunderstorm')", "Thunder"),
        ("s", "set_weather('snow')", "Snow"),
        ("h", "set_weather('heavy_snow')", "Blizzard"),
        ("u", "set_weather('sun')", "Sun"),
        ("f", "set_weather('fog')", "Fog"),
        ("q", "quit", "Quit"),
    ]

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        self.weather_data = fetch_weather()

    def compose(self) -> ComposeResult:
        yield Header()

        initial_weather = CONDITION_TO_WEATHER.get(
            self.weather_data["condition"], "rain"
        )
        yield WeatherBackground(id="weather-bg")

        with Container(id="content-layer"):
            with TabbedContent():
                with TabPane("Dashboard", id="dashboard"):
                    yield WeatherCard(self.weather_data)
                    yield HeartbeatIndicator()

                    yield Static("Activity", classes="section-title")
                    with Horizontal(classes="metric-row"):
                        yield MetricCard("Sessions (7d)", "47", "↑ 12%")
                        yield MetricCard("Lines Added", "2,841", "↗ 8%")
                        yield MetricCard("Guard Blocks", "12", "↓ 23%")
                        yield MetricCard("Recipes Active", "8", "→ stable")

                    yield Static("Trends (30d)", classes="section-title")
                    with Container(classes="activity-section"):
                        sessions = [3, 5, 2, 7, 4, 6, 8, 3, 5, 9, 4, 6, 7, 3, 5,
                                    8, 6, 4, 7, 9, 5, 3, 6, 8, 4, 7, 5, 6, 8, 7]
                        lines_data = [120, 340, 80, 450, 200, 380, 560, 150, 290, 620,
                                      180, 350, 470, 130, 280, 510, 320, 200, 400, 580,
                                      250, 140, 370, 520, 190, 430, 260, 350, 510, 440]
                        yield SparklineDemo("Sessions/day", sessions, "#4cc9f0")
                        yield SparklineDemo("Lines/day   ", lines_data, "#39d353")

                with TabPane("Feeds", id="feeds"):
                    yield Static(
                        "[bold]Live Feeds[/bold]\n\n"
                        "  [green]●[/green] pulse      💓 12m idle\n"
                        "  [green]●[/green] project    ⎇ main (3m ago)\n"
                        "  [yellow]●[/yellow] calendar   📅 2 events today\n"
                        "  [green]●[/green] news       📰 Updated 5m ago\n"
                        "  [green]●[/green] insights   📊 Updated 2m ago\n\n"
                        "[dim]Weather particles continue behind all tabs[/dim]"
                    )

                with TabPane("Weather Ctrl", id="weather"):
                    yield Static(
                        f"[bold]Weather Controls[/bold]\n\n"
                        f"Auto-detected: [bold cyan]{self.weather_data['city']}[/bold cyan] — "
                        f"{self.weather_data['condition']}\n\n"
                        "Switch weather with these keys:\n\n"
                        "  [bold cyan]r[/bold cyan]  🌧️  Rain          "
                        "[bold cyan]d[/bold cyan]  🌦️  Drizzle\n"
                        "  [bold cyan]t[/bold cyan]  ⛈️  Thunderstorm  "
                        "[bold cyan]s[/bold cyan]  🌨️  Snow\n"
                        "  [bold cyan]h[/bold cyan]  ❄️  Blizzard      "
                        "[bold cyan]u[/bold cyan]  ☀️  Sun\n"
                        "  [bold cyan]f[/bold cyan]  🌫️  Fog\n\n"
                        "[dim]Features:[/dim]\n"
                        "  • Multi-layer particles (near/mid/far depth)\n"
                        "  • Wind gusts shift all particles periodically\n"
                        "  • Clouds drift across the top\n"
                        "  • Rain splashes on the ground\n"
                        "  • Snow sways with sinusoidal drift\n"
                        "  • Lightning flashes during thunderstorms\n"
                        "  • Twinkling stars + dust motes for sun\n"
                        "  • Fog particles drift and undulate"
                    )

        yield Footer()

    def on_mount(self) -> None:
        # Set initial weather from auto-detection
        bg = self.query_one("#weather-bg", WeatherBackground)
        bg.weather = CONDITION_TO_WEATHER.get(
            self.weather_data["condition"], "rain"
        )

    def action_switch_tab(self, tab_id: str) -> None:
        self.query_one(TabbedContent).active = tab_id

    def action_set_weather(self, weather: str) -> None:
        self.query_one("#weather-bg", WeatherBackground).weather = weather

    def action_quit(self) -> None:
        self.exit()


if __name__ == "__main__":
    app = RainWeatherApp()
    app.run()
