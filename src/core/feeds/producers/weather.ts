/**
 * Weather feed producer: fetches current weather data from the Open-Meteo API.
 *
 * Implementation:
 *   1. Builds the Open-Meteo API URL with configured coordinates and temperature unit
 *   2. Fetches current weather data with a 10-second timeout
 *   3. Parses the JSON response for temperature, weather code, and wind speed
 *   4. Maps WMO weather codes to human-readable descriptions and emoji
 *   5. Returns structured weather data for the cache bus
 *
 * Returns null on any failure (network error, malformed JSON, timeout).
 * No API key required — Open-Meteo is a free, open-source weather API.
 *
 * Architecture: ARCH-3 fail-open — never throws, returns null on error.
 */

import type { WeatherFeedConfig, FeedProducer } from "../../types.js";

export interface WeatherData {
  temperature: number;
  weatherCode: number;
  windSpeed: number;
  description: string;
  emoji: string;
  temperatureUnit: "fahrenheit" | "celsius";
}

/**
 * WMO Weather interpretation codes (WW) mapped to description and emoji.
 * See: https://open-meteo.com/en/docs#weathervariables
 *
 * Code ranges:
 *   0       = Clear sky
 *   1-3     = Mainly clear, partly cloudy, overcast
 *   45, 48  = Fog, depositing rime fog
 *   51-67   = Drizzle and rain (various intensities)
 *   71-77   = Snow fall and snow grains
 *   80-82   = Rain showers
 *   85, 86  = Snow showers
 *   95-99   = Thunderstorm (with and without hail)
 */
export function mapWeatherCode(code: number): { description: string; emoji: string } {
  if (code === 0) return { description: "Clear", emoji: "\u2600\uFE0F" };
  if (code >= 1 && code <= 3) return { description: "Cloudy", emoji: "\u26C5" };
  if (code >= 45 && code <= 48) return { description: "Fog", emoji: "\uD83C\uDF2B\uFE0F" };
  if (code >= 51 && code <= 67) return { description: "Rain", emoji: "\uD83C\uDF27\uFE0F" };
  if (code >= 71 && code <= 77) return { description: "Snow", emoji: "\u2744\uFE0F" };
  if (code >= 80 && code <= 82) return { description: "Showers", emoji: "\uD83C\uDF26\uFE0F" };
  if (code >= 85 && code <= 86) return { description: "Snow Showers", emoji: "\uD83C\uDF28\uFE0F" };
  if (code >= 95 && code <= 99) return { description: "Storm", emoji: "\u26C8\uFE0F" };
  return { description: "Unknown", emoji: "\uD83C\uDF24\uFE0F" };
}

/**
 * Build the Open-Meteo API URL for current weather conditions.
 */
function buildApiUrl(config: WeatherFeedConfig): string {
  const { latitude, longitude, temperatureUnit } = config;
  return `https://api.open-meteo.com/v1/forecast?latitude=${latitude}&longitude=${longitude}&current=temperature_2m,weather_code,wind_speed_10m&temperature_unit=${temperatureUnit}`;
}

/**
 * Create a FeedProducer for the weather feed.
 *
 * @param config - Weather feed configuration (coordinates, temperature unit)
 */
export function createWeatherProducer(config: WeatherFeedConfig): FeedProducer {
  return async (): Promise<Record<string, unknown> | null> => {
    try {
      const url = buildApiUrl(config);

      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 10_000);

      let response: Response;
      try {
        response = await fetch(url, { signal: controller.signal });
      } finally {
        clearTimeout(timeoutId);
      }

      if (!response.ok) return null;

      const data = await response.json() as {
        current?: {
          temperature_2m?: number;
          weather_code?: number;
          wind_speed_10m?: number;
        };
      };

      if (!data?.current) return null;

      const temperature = data.current.temperature_2m;
      const weatherCode = data.current.weather_code;
      const windSpeed = data.current.wind_speed_10m;

      if (temperature === undefined || weatherCode === undefined || windSpeed === undefined) {
        return null;
      }

      const { description, emoji } = mapWeatherCode(weatherCode);

      const result: WeatherData = {
        temperature,
        weatherCode,
        windSpeed,
        description,
        emoji,
        temperatureUnit: config.temperatureUnit,
      };

      return result as unknown as Record<string, unknown>;
    } catch {
      // ARCH-3: Fail-open — return null on any error
      return null;
    }
  };
}
