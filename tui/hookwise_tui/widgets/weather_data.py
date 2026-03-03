"""Weather data bridge — reads weather from hookwise cache or fetches live."""

from __future__ import annotations

import json
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from hookwise_tui.data import read_cache


# --- Weather code mapping (WMO standard) ---

WEATHER_CODE_MAP: dict[int, str] = {
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

# All valid weather conditions the background engine supports
VALID_CONDITIONS = frozenset({
    "clear", "sun", "cloudy", "fog", "drizzle",
    "rain", "heavy_rain", "snow", "heavy_snow", "thunderstorm",
})


def _safe_float(val: Any, default: float = 0.0) -> float:
    """Safely convert a value to float, returning *default* on failure."""
    try:
        return float(val) if val is not None else default
    except (TypeError, ValueError):
        return default


def _safe_int(val: Any, default: int = 0) -> int:
    """Safely convert a value to int, returning *default* on failure."""
    try:
        return int(float(val)) if val is not None else default
    except (TypeError, ValueError):
        return default


@dataclass
class WeatherInfo:
    """Parsed weather information for the TUI."""

    city: str
    condition: str
    code: int
    temp_c: float
    temp_f: float
    wind_speed: float

    @property
    def display_condition(self) -> str:
        """Human-readable condition with emoji."""
        emoji_map = {
            "clear": "Clear", "sun": "Clear",
            "cloudy": "Cloudy", "fog": "Fog",
            "drizzle": "Drizzle", "rain": "Rain",
            "heavy_rain": "Heavy Rain",
            "snow": "Snow", "heavy_snow": "Heavy Snow",
            "thunderstorm": "Thunderstorm",
        }
        return emoji_map.get(self.condition, self.condition.replace("_", " ").title())

    @property
    def condition_emoji(self) -> str:
        """Emoji for the weather condition."""
        emoji_map = {
            "clear": "\u2600\ufe0f", "sun": "\u2600\ufe0f",
            "cloudy": "\u2601\ufe0f", "fog": "\ud83c\udf2b\ufe0f",
            "drizzle": "\ud83c\udf26\ufe0f", "rain": "\ud83c\udf27\ufe0f",
            "heavy_rain": "\ud83c\udf27\ufe0f",
            "snow": "\ud83c\udf28\ufe0f", "heavy_snow": "\u2744\ufe0f",
            "thunderstorm": "\u26c8\ufe0f",
        }
        return emoji_map.get(self.condition, "\ud83c\udf24\ufe0f")


def read_weather_from_cache(
    cache: dict[str, Any] | None = None,
) -> WeatherInfo | None:
    """Read weather data from the hookwise status-line cache.

    The weather producer (when enabled) writes weather data into the cache bus
    under the 'weather' key. This function extracts and parses that data.

    Returns None if no weather data is available in the cache.
    """
    if cache is None:
        cache = read_cache()

    weather_entry = cache.get("weather")
    if not isinstance(weather_entry, dict) or not weather_entry:
        return None

    # The weather producer writes camelCase fields:
    #   temperature, weatherCode, windSpeed, description, emoji, temperatureUnit
    # The live fallback and legacy caches may write snake_case:
    #   city, condition, code, temp_c, temp_f, wind_speed
    # Support both schemas for robustness.

    code = weather_entry.get("weatherCode", weather_entry.get("code", 0))
    wind_speed = weather_entry.get("windSpeed", weather_entry.get("wind_speed", 0))
    temperature = weather_entry.get("temperature", 0)
    temp_unit = weather_entry.get("temperatureUnit", "")
    description = weather_entry.get("description", "")
    city = weather_entry.get("city", "")

    # Derive condition from description or WMO code
    condition = weather_entry.get("condition", "")
    if not condition and description:
        condition = description.lower().replace(" ", "_")
    if condition not in VALID_CONDITIONS:
        condition = WEATHER_CODE_MAP.get(_safe_int(code), "cloudy")

    # If no useful data at all, return None
    if not condition and not temperature and not code:
        return None

    # Compute temp_c/temp_f from the single temperature + unit, or from legacy fields
    if temp_unit == "celsius":
        temp_c = _safe_float(temperature) if temperature else _safe_float(weather_entry.get("temp_c"))
        temp_f = round(temp_c * 9 / 5 + 32)
    elif temp_unit == "fahrenheit" or temperature:
        temp_f = _safe_float(temperature) if temperature else _safe_float(weather_entry.get("temp_f"))
        temp_c = round((temp_f - 32) * 5 / 9)
    else:
        temp_c = _safe_float(weather_entry.get("temp_c"))
        temp_f = _safe_float(weather_entry.get("temp_f"))

    return WeatherInfo(
        city=str(city) if city else "Local",
        condition=condition,
        code=_safe_int(code),
        temp_c=temp_c,
        temp_f=temp_f,
        wind_speed=_safe_float(wind_speed),
    )


def fetch_weather_live() -> WeatherInfo:
    """Fetch real weather via IP geolocation + Open-Meteo. No API key needed.

    This is a fallback when no weather data is in the cache.
    Uses the same approach as the prototype.
    """
    try:
        req = urllib.request.Request(
            "https://ipinfo.io/json",
            headers={"User-Agent": "hookwise-tui"},
        )
        with urllib.request.urlopen(req, timeout=3) as resp:
            loc = json.loads(resp.read())
        city = loc.get("city", "Unknown")
        lat, lon = loc.get("loc", "47.6,-122.3").split(",")

        url = (
            f"https://api.open-meteo.com/v1/forecast?"
            f"latitude={lat}&longitude={lon}&current_weather=true"
        )
        req2 = urllib.request.Request(url, headers={"User-Agent": "hookwise-tui"})
        with urllib.request.urlopen(req2, timeout=3) as resp:
            weather = json.loads(resp.read())

        cw = weather.get("current_weather", {})
        code = cw.get("weathercode", 0)
        temp_c = cw.get("temperature", 10)
        wind_speed = cw.get("windspeed", 5)

        return WeatherInfo(
            city=city,
            condition=WEATHER_CODE_MAP.get(code, "cloudy"),
            code=code,
            temp_c=temp_c,
            temp_f=round(temp_c * 9 / 5 + 32),
            wind_speed=wind_speed,
        )
    except Exception:
        return WeatherInfo(
            city="Seattle",
            condition="rain",
            code=61,
            temp_c=9,
            temp_f=48,
            wind_speed=12,
        )


def get_weather() -> WeatherInfo:
    """Get weather info, preferring cache then falling back to live fetch.

    Priority:
    1. Read from hookwise cache (written by weather feed producer)
    2. Fetch live from Open-Meteo (no API key needed)
    """
    cached = read_weather_from_cache()
    if cached is not None:
        return cached
    return fetch_weather_live()
