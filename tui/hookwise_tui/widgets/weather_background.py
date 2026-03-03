"""Weather background widget — animated particle engine for the TUI.

Ported from tui/prototypes/rain_weather.py with the following adaptations:
- Reads weather from hookwise cache (weather feed producer) instead of live fetch
- Falls back to Open-Meteo live fetch if no cache data available
- Integrates as a background layer behind all tab content
- Uses Textual's timer system for non-blocking animation
"""

from __future__ import annotations

import math
import random

from rich.text import Text as RichText
from textual.reactive import reactive
from textual.widget import Widget

from hookwise_tui.widgets.weather_data import WeatherInfo, get_weather


# --- Particle Types ---


class _Particle:
    """A weather particle with physics."""

    __slots__ = ("x", "y", "char", "speed", "drift", "opacity", "layer")

    def __init__(
        self,
        x: float,
        y: float,
        char: str,
        speed: float,
        drift: float = 0.0,
        opacity: int = 2,
        layer: int = 0,
    ):
        self.x = x
        self.y = y
        self.char = char
        self.speed = speed
        self.drift = drift
        self.opacity = opacity  # 0=dim, 1=normal, 2=bright
        self.layer = layer  # 0=far, 1=mid, 2=near


class _Splash:
    """A splash effect when rain hits the ground."""

    __slots__ = ("x", "y", "life", "max_life")

    def __init__(self, x: float, y: float):
        self.x = x
        self.y = y
        self.life = 0
        self.max_life = random.randint(3, 6)


class _Cloud:
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


# --- Weather Background Engine ---


class WeatherBackground(Widget):
    """Multi-layer animated weather with particles, clouds, and effects.

    Renders as a full-screen background layer behind all content.
    Weather condition drives particle type, density, and behavior.
    """

    DEFAULT_CSS = """
    WeatherBackground {
        width: 100%;
        height: 100%;
    }
    """

    weather: reactive[str] = reactive("rain")
    frame: reactive[int] = reactive(0)

    def __init__(self, weather_info: WeatherInfo | None = None, **kwargs):
        super().__init__(**kwargs)
        self.particles: list[_Particle] = []
        self.splashes: list[_Splash] = []
        self.clouds: list[_Cloud] = []
        self._width = 80
        self._height = 24
        self._wind = 0.0
        self._wind_target = 0.0
        self._gust_timer = 0
        self._lightning_flash = 0
        self._lightning_timer = 0
        self._time_offset = 0.0
        self._weather_info = weather_info

    def on_mount(self) -> None:
        self._width = self.size.width or 80
        self._height = self.size.height or 24
        self._spawn_all()
        self.set_interval(0.06, self._tick)

    def on_resize(self) -> None:
        self._width = self.size.width or 80
        self._height = self.size.height or 24

    # --- Spawning ---

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
        elif self.weather == "fog":
            count = 6
        else:
            count = random.randint(2, 4)

        for _ in range(count):
            self.clouds.append(
                _Cloud(
                    x=random.uniform(-20, self._width + 20),
                    y=random.randint(0, 3),
                    speed=random.uniform(0.02, 0.08),
                    shape_idx=random.randint(0, 3),
                )
            )

    def _spawn_particles(self) -> None:
        self.particles = []
        w, h = self._width, self._height

        if self.weather in ("rain", "drizzle"):
            density = w if self.weather == "rain" else w // 3
            chars_near = ["|", "\u2502", "\u2503"]
            chars_mid = ["\u254e", "\u250a", "/"]
            chars_far = [".", ",", "'"]
            for _ in range(density):
                layer = random.choices([0, 1, 2], weights=[3, 4, 3])[0]
                if layer == 2:
                    chars, spd, opac = chars_near, random.uniform(1.0, 1.8), 2
                elif layer == 1:
                    chars, spd, opac = chars_mid, random.uniform(0.6, 1.0), 1
                else:
                    chars, spd, opac = chars_far, random.uniform(0.3, 0.5), 0
                self.particles.append(
                    _Particle(
                        x=random.uniform(0, w),
                        y=random.uniform(-h, h),
                        char=random.choice(chars),
                        speed=spd,
                        drift=random.uniform(-0.1, 0.05),
                        opacity=opac,
                        layer=layer,
                    )
                )

        elif self.weather == "heavy_rain":
            for _ in range(int(w * 1.5)):
                layer = random.choices([0, 1, 2], weights=[2, 3, 5])[0]
                chars = (
                    ["|", "\u2502", "\u2503", "\u2551"]
                    if layer >= 1
                    else ["\u254e", "/", "."]
                )
                self.particles.append(
                    _Particle(
                        x=random.uniform(0, w),
                        y=random.uniform(-h, h),
                        char=random.choice(chars),
                        speed=random.uniform(1.2, 2.5),
                        drift=random.uniform(-0.3, -0.05),
                        opacity=min(layer + 1, 2),
                        layer=layer,
                    )
                )

        elif self.weather == "thunderstorm":
            for _ in range(int(w * 1.8)):
                layer = random.choices([0, 1, 2], weights=[2, 3, 5])[0]
                chars = (
                    ["|", "\u2502", "\u2503", "\u2551"]
                    if layer >= 1
                    else ["\u254e", "/"]
                )
                self.particles.append(
                    _Particle(
                        x=random.uniform(0, w),
                        y=random.uniform(-h, h),
                        char=random.choice(chars),
                        speed=random.uniform(1.5, 3.0),
                        drift=random.uniform(-0.5, -0.1),
                        opacity=min(layer + 1, 2),
                        layer=layer,
                    )
                )

        elif self.weather in ("snow", "heavy_snow"):
            density = w // 2 if self.weather == "snow" else w
            chars = ["*", "\u00b7", "\u2022", "\u2218", "\u25e6",
                     "\u2726", "\u2744", "\u2746", "\u273b", "\u2042"]
            for _ in range(density):
                layer = random.choices([0, 1, 2], weights=[4, 4, 2])[0]
                self.particles.append(
                    _Particle(
                        x=random.uniform(0, w),
                        y=random.uniform(-h, h),
                        char=random.choice(chars),
                        speed=random.uniform(0.08, 0.25) * (1 + layer * 0.3),
                        drift=0.0,
                        opacity=layer,
                        layer=layer,
                    )
                )

        elif self.weather in ("clear", "sun"):
            # Twinkling stars
            chars = ["\u2726", "\u00b7", "\u02da", "\u00b0", "\u207a", "\u2217"]
            for _ in range(w // 6):
                self.particles.append(
                    _Particle(
                        x=random.uniform(0, w),
                        y=random.uniform(0, h),
                        char=random.choice(chars),
                        speed=0.0,
                        drift=0.0,
                        opacity=random.randint(0, 2),
                        layer=0,
                    )
                )
            # Floating dust motes
            for _ in range(w // 8):
                self.particles.append(
                    _Particle(
                        x=random.uniform(0, w),
                        y=random.uniform(0, h),
                        char=random.choice(["\u00b7", "\u2218", "'"]),
                        speed=random.uniform(0.02, 0.06),
                        drift=random.uniform(0.01, 0.04),
                        opacity=0,
                        layer=1,
                    )
                )

        elif self.weather == "fog":
            fog_chars = ["\u2591", "\u2592", "\u00b7", "\u2218", " "]
            for _ in range(w * 2):
                self.particles.append(
                    _Particle(
                        x=random.uniform(0, w),
                        y=random.uniform(0, h),
                        char=random.choice(fog_chars),
                        speed=0.0,
                        drift=random.uniform(0.01, 0.05),
                        opacity=random.randint(0, 1),
                        layer=0,
                    )
                )

        elif self.weather == "cloudy":
            for _ in range(w // 6):
                self.particles.append(
                    _Particle(
                        x=random.uniform(0, w),
                        y=random.uniform(0, h),
                        char=random.choice(["\u00b7", "\u2218", "'"]),
                        speed=random.uniform(0.01, 0.04),
                        drift=random.uniform(-0.02, 0.02),
                        opacity=0,
                        layer=0,
                    )
                )

    # --- Physics tick ---

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
                sway = (
                    math.sin(self._time_offset * 1.5 + p.x * 0.1 + p.layer) * 0.15
                )
                p.x += sway + self._wind * (0.5 + p.layer * 0.2)
                p.y += p.speed
            elif self.weather in ("clear", "sun") and p.layer == 0:
                # Twinkling
                if random.random() < 0.03:
                    p.opacity = (p.opacity + 1) % 3
                continue
            elif self.weather == "fog":
                p.x += p.drift + self._wind * 0.2
                p.y += math.sin(self._time_offset + p.x * 0.05) * 0.02
            else:
                # Rain/drizzle/thunderstorm
                p.y += p.speed
                p.x += p.drift + self._wind * (0.3 + p.layer * 0.2)

            # Dust motes in sun mode
            if self.weather in ("clear", "sun") and p.layer == 1:
                p.y += p.speed
                p.x += p.drift + math.sin(
                    self._time_offset * 0.5 + p.y * 0.2
                ) * 0.03

            # Respawn at top when off bottom
            if p.y > h:
                if (
                    self.weather in ("rain", "heavy_rain", "thunderstorm")
                    and p.layer >= 1
                ):
                    if random.random() < 0.3:
                        self.splashes.append(_Splash(p.x, h - 1))
                p.y = random.uniform(-4, -1)
                p.x = random.uniform(0, w)

            # Wrap horizontally
            if p.x < 0:
                p.x += w
            elif p.x >= w:
                p.x -= w

        # Update splashes
        self.splashes = [s for s in self.splashes if s.life < s.max_life]
        for s in self.splashes:
            s.life += 1

        self.frame = self.frame + 1

    # --- Reactive watchers ---

    def watch_weather(self, new_weather: str) -> None:
        self._wind = 0.0
        self._wind_target = 0.0
        self._lightning_flash = 0
        self._spawn_all()

    # --- Rendering ---

    def render(self) -> RichText:
        w, h = self._width, self._height
        if w <= 0 or h <= 0:
            return RichText("")

        # Build grid: (char, style)
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
            ["\u00b7", "\u2218", "\u00b7"],
            ["~", "\u2218", "~"],
            ["-", "\u00b7", "-"],
            [".", " ", "."],
        ]
        for s in self.splashes:
            sx, sy = int(s.x), int(s.y)
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

                # Lightning flash override
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
