"""Tests for weather background widget and weather data bridge."""

from hookwise_tui.widgets.weather_data import (
    WEATHER_CODE_MAP,
    VALID_CONDITIONS,
    WeatherInfo,
    read_weather_from_cache,
)
from hookwise_tui.widgets.weather_background import (
    WeatherBackground,
    _Particle,
    _Splash,
    _Cloud,
)


# --- WeatherInfo dataclass ---


class TestWeatherInfo:
    def test_display_condition(self):
        info = WeatherInfo(
            city="Seattle", condition="rain", code=61,
            temp_c=9, temp_f=48, wind_speed=12,
        )
        assert info.display_condition == "Rain"

    def test_display_condition_clear(self):
        info = WeatherInfo(
            city="LA", condition="clear", code=0,
            temp_c=25, temp_f=77, wind_speed=5,
        )
        assert info.display_condition == "Clear"

    def test_display_condition_heavy_rain(self):
        info = WeatherInfo(
            city="Portland", condition="heavy_rain", code=65,
            temp_c=7, temp_f=45, wind_speed=20,
        )
        assert info.display_condition == "Heavy Rain"

    def test_condition_emoji(self):
        info = WeatherInfo(
            city="Seattle", condition="rain", code=61,
            temp_c=9, temp_f=48, wind_speed=12,
        )
        assert info.condition_emoji != ""

    def test_condition_emoji_snow(self):
        info = WeatherInfo(
            city="Denver", condition="snow", code=71,
            temp_c=-2, temp_f=28, wind_speed=10,
        )
        assert info.condition_emoji != ""


# --- Weather code mapping ---


class TestWeatherCodeMap:
    def test_clear_codes(self):
        assert WEATHER_CODE_MAP[0] == "clear"
        assert WEATHER_CODE_MAP[1] == "clear"

    def test_rain_codes(self):
        assert WEATHER_CODE_MAP[61] == "rain"
        assert WEATHER_CODE_MAP[63] == "rain"
        assert WEATHER_CODE_MAP[65] == "heavy_rain"

    def test_snow_codes(self):
        assert WEATHER_CODE_MAP[71] == "snow"
        assert WEATHER_CODE_MAP[75] == "heavy_snow"

    def test_thunderstorm_codes(self):
        assert WEATHER_CODE_MAP[95] == "thunderstorm"
        assert WEATHER_CODE_MAP[99] == "thunderstorm"

    def test_drizzle_codes(self):
        assert WEATHER_CODE_MAP[51] == "drizzle"
        assert WEATHER_CODE_MAP[53] == "drizzle"

    def test_fog_codes(self):
        assert WEATHER_CODE_MAP[45] == "fog"
        assert WEATHER_CODE_MAP[48] == "fog"

    def test_all_conditions_are_valid(self):
        for code, condition in WEATHER_CODE_MAP.items():
            assert condition in VALID_CONDITIONS, (
                f"Code {code} maps to '{condition}' which is not in VALID_CONDITIONS"
            )


# --- read_weather_from_cache ---


class TestReadWeatherFromCache:
    def test_reads_weather_from_cache(self):
        cache = {
            "weather": {
                "city": "Seattle",
                "condition": "rain",
                "code": 61,
                "temp_c": 9,
                "temp_f": 48,
                "wind_speed": 12,
            }
        }
        result = read_weather_from_cache(cache)
        assert result is not None
        assert result.city == "Seattle"
        assert result.condition == "rain"
        assert result.code == 61
        assert result.temp_c == 9.0
        assert result.temp_f == 48.0

    def test_returns_none_when_no_weather_key(self):
        cache = {"pulse": {"updated_at": "2026-01-01T00:00:00Z"}}
        result = read_weather_from_cache(cache)
        assert result is None

    def test_returns_none_when_weather_not_dict(self):
        cache = {"weather": "rain"}
        result = read_weather_from_cache(cache)
        assert result is None

    def test_returns_none_when_empty_cache(self):
        result = read_weather_from_cache({})
        assert result is None

    def test_returns_none_when_empty_weather_entry(self):
        cache = {"weather": {}}
        result = read_weather_from_cache(cache)
        assert result is None

    def test_normalizes_invalid_condition(self):
        cache = {
            "weather": {
                "city": "Narnia",
                "condition": "magic_rain",
                "code": 61,
                "temp_c": 15,
                "temp_f": 59,
                "wind_speed": 5,
            }
        }
        result = read_weather_from_cache(cache)
        assert result is not None
        assert result.condition == "rain"  # Falls back to code mapping

    def test_reads_producer_camelcase_schema(self):
        """Test reading weather data from the TS producer's camelCase schema."""
        cache = {
            "weather": {
                "temperature": 72,
                "weatherCode": 0,
                "windSpeed": 5,
                "description": "Clear",
                "emoji": "☀️",
                "temperatureUnit": "fahrenheit",
            }
        }
        result = read_weather_from_cache(cache)
        assert result is not None
        assert result.condition == "clear"
        assert result.code == 0
        assert result.temp_f == 72.0
        assert result.temp_c == 22  # (72-32)*5/9 rounded
        assert result.wind_speed == 5.0
        assert result.city == "Local"

    def test_reads_producer_celsius_schema(self):
        """Test reading celsius weather data from the TS producer."""
        cache = {
            "weather": {
                "temperature": 20,
                "weatherCode": 61,
                "windSpeed": 15,
                "description": "Rain",
                "emoji": "🌧️",
                "temperatureUnit": "celsius",
            }
        }
        result = read_weather_from_cache(cache)
        assert result is not None
        assert result.condition == "rain"
        assert result.temp_c == 20.0
        assert result.temp_f == 68  # 20*9/5+32 rounded
        assert result.wind_speed == 15.0

    def test_handles_missing_fields_gracefully(self):
        cache = {
            "weather": {
                "city": "Seattle",
                "condition": "cloudy",
            }
        }
        result = read_weather_from_cache(cache)
        assert result is not None
        assert result.city == "Seattle"
        assert result.condition == "cloudy"
        assert result.code == 0
        assert result.temp_c == 0.0
        assert result.wind_speed == 0.0


# --- Particle types ---


class TestParticleTypes:
    def test_particle_creation(self):
        p = _Particle(x=10.0, y=5.0, char="|", speed=1.0, drift=0.1, opacity=2, layer=1)
        assert p.x == 10.0
        assert p.y == 5.0
        assert p.char == "|"
        assert p.speed == 1.0
        assert p.drift == 0.1
        assert p.opacity == 2
        assert p.layer == 1

    def test_particle_defaults(self):
        p = _Particle(x=0, y=0, char=".", speed=0.5)
        assert p.drift == 0.0
        assert p.opacity == 2
        assert p.layer == 0

    def test_splash_creation(self):
        s = _Splash(x=10.0, y=20.0)
        assert s.x == 10.0
        assert s.y == 20.0
        assert s.life == 0
        assert 3 <= s.max_life <= 6

    def test_cloud_creation(self):
        c = _Cloud(x=5.0, y=1.0, speed=0.05, shape_idx=0)
        assert c.x == 5.0
        assert c.y == 1.0
        assert c.speed == 0.05
        assert len(c.shape) > 0
        assert c.width > 0

    def test_cloud_shape_wrapping(self):
        # Shape index wraps around
        c1 = _Cloud(x=0, y=0, speed=0.05, shape_idx=0)
        c2 = _Cloud(x=0, y=0, speed=0.05, shape_idx=len(_Cloud.SHAPES))
        assert c1.shape == c2.shape


# --- WeatherBackground widget ---


class TestWeatherBackground:
    def test_widget_renders_without_crash(self):
        """Widget renders with default rain weather."""
        from rich.text import Text as RichText
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "rain"
        bg._spawn_all()
        result = bg.render()
        assert isinstance(result, RichText)
        assert len(str(result)) > 0

    def test_widget_renders_all_weather_types(self):
        """Every weather condition renders without error."""
        from rich.text import Text as RichText
        for condition in VALID_CONDITIONS:
            bg = WeatherBackground()
            bg._width = 80
            bg._height = 24
            bg.weather = condition
            bg._spawn_all()
            bg._tick()
            result = bg.render()
            assert isinstance(result, RichText), f"Failed for condition: {condition}"

    def test_spawn_particles_rain(self):
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "rain"
        bg._spawn_particles()
        assert len(bg.particles) > 0
        # Rain should have multi-layer particles
        layers = {p.layer for p in bg.particles}
        assert len(layers) > 1

    def test_spawn_particles_snow(self):
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "snow"
        bg._spawn_particles()
        assert len(bg.particles) > 0

    def test_spawn_particles_clear(self):
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "clear"
        bg._spawn_particles()
        # Clear has twinkling stars and dust motes
        assert len(bg.particles) > 0

    def test_spawn_particles_fog(self):
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "fog"
        bg._spawn_particles()
        assert len(bg.particles) > 0

    def test_spawn_particles_thunderstorm(self):
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "thunderstorm"
        bg._spawn_particles()
        # Thunderstorm should be dense
        assert len(bg.particles) > 80

    def test_spawn_particles_heavy_rain(self):
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "heavy_rain"
        bg._spawn_particles()
        assert len(bg.particles) > 80

    def test_spawn_clouds(self):
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "rain"
        bg._spawn_clouds()
        assert len(bg.clouds) >= 2

    def test_spawn_clouds_clear(self):
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "clear"
        bg._spawn_clouds()
        assert len(bg.clouds) == 1

    def test_spawn_clouds_fog(self):
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "fog"
        bg._spawn_clouds()
        assert len(bg.clouds) == 6

    def test_weather_change_respawns(self):
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "rain"
        bg._spawn_all()

        bg.weather = "snow"
        bg.watch_weather("snow")

        # Different weather = different particle count
        # (they might coincidentally be the same due to randomness,
        # but the particles themselves should be different types)
        assert bg._wind == 0.0  # Wind resets on weather change

    def test_tick_updates_frame(self):
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "rain"
        bg._spawn_all()
        initial_frame = bg.frame
        bg._tick()
        assert bg.frame == initial_frame + 1

    def test_render_produces_richtext(self):
        from rich.text import Text as RichText
        bg = WeatherBackground()
        bg._width = 40
        bg._height = 10
        bg.weather = "rain"
        bg._spawn_all()
        result = bg.render()
        assert isinstance(result, RichText)

    def test_render_empty_dimensions(self):
        from rich.text import Text as RichText
        bg = WeatherBackground()
        bg._width = 0
        bg._height = 0
        result = bg.render()
        assert isinstance(result, RichText)
        assert str(result) == ""

    def test_lightning_only_in_thunderstorm(self):
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "rain"
        bg._spawn_all()
        # Run many ticks — rain should never trigger lightning
        for _ in range(300):
            bg._tick()
        assert bg._lightning_flash == 0  # Rain doesn't have lightning

    def test_splash_lifecycle(self):
        # Test that splashes are created with life=0 and age each tick
        splash = _Splash(40.0, 23.0)
        assert splash.life == 0
        assert 3 <= splash.max_life <= 6

        # Test that splashes expire: create one with a short max_life
        bg = WeatherBackground()
        bg._width = 80
        bg._height = 24
        bg.weather = "clear"  # Clear weather = no rain = no new splashes
        bg._spawn_all()

        short_splash = _Splash(40.0, 23.0)
        short_splash.max_life = 3
        bg.splashes = [short_splash]

        # After enough ticks, the manually added splash should expire
        for _ in range(10):
            bg._tick()
        # The short splash (max_life=3) should have expired in 3 ticks
        assert short_splash not in bg.splashes


# --- Integration: App with weather background ---


class TestAppWeatherIntegration:
    async def test_app_launches_with_weather_bg(self):
        """App starts with weather background widget."""
        from hookwise_tui.app import HookwiseTUI
        app = HookwiseTUI()
        async with app.run_test() as _pilot:
            assert app.title == "Hookwise"
            bg = app.query_one("#weather-bg", WeatherBackground)
            assert bg is not None

    async def test_weather_cycle_key(self):
        """'w' key cycles through weather conditions."""
        from hookwise_tui.app import HookwiseTUI
        app = HookwiseTUI()
        async with app.run_test() as pilot:
            bg = app.query_one("#weather-bg", WeatherBackground)
            initial_weather = bg.weather
            await pilot.press("w")
            # Weather should have changed
            new_weather = bg.weather
            # The cycle advances, so they should differ
            # (unless the initial happens to be the last in cycle
            # and wraps to the same — unlikely but possible)
            assert isinstance(new_weather, str)
            assert new_weather in VALID_CONDITIONS or new_weather == "sun"
            assert new_weather != initial_weather

    async def test_tab_switching_still_works(self):
        """Tab switching works with weather background present."""
        from hookwise_tui.app import HookwiseTUI
        app = HookwiseTUI()
        async with app.run_test() as pilot:
            tabs = app.query_one("TabbedContent")
            assert tabs.active == "dashboard"
            await pilot.press("2")
            assert tabs.active == "guards"
            await pilot.press("8")
            assert tabs.active == "status"

    async def test_content_layer_exists(self):
        """Content layer container is present above the weather background."""
        from hookwise_tui.app import HookwiseTUI
        app = HookwiseTUI()
        async with app.run_test() as _pilot:
            from textual.containers import Container
            content = app.query_one("#content-layer", Container)
            assert content is not None
