/**
 * Tests for the weather feed producer.
 *
 * Covers:
 * - Successful weather fetch and cache shape
 * - Fail-open on network error (ARCH-3)
 * - Fail-open on malformed JSON
 * - WMO weather code to emoji mapping
 * - Temperature unit handling (fahrenheit/celsius)
 * - Wind speed indicator threshold
 * - Fail-open on API timeout
 *
 * All HTTP calls are mocked via vi.stubGlobal("fetch").
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createWeatherProducer, mapWeatherCode } from "../../../../src/core/feeds/producers/weather.js";
import type { WeatherFeedConfig } from "../../../../src/core/types.js";
import type { WeatherData } from "../../../../src/core/feeds/producers/weather.js";

// --- Helpers ---

function makeConfig(overrides?: Partial<WeatherFeedConfig>): WeatherFeedConfig {
  return {
    enabled: true,
    intervalSeconds: 600,
    latitude: 37.7749,
    longitude: -122.4194,
    temperatureUnit: "fahrenheit",
    ...overrides,
  };
}

/**
 * Create a mock Response object matching the Fetch API.
 */
function mockResponse(body: unknown, ok = true): Response {
  return {
    ok,
    status: ok ? 200 : 500,
    json: async () => body,
    text: async () => (typeof body === "string" ? body : JSON.stringify(body)),
  } as Response;
}

/**
 * Sample Open-Meteo API response for clear weather in San Francisco.
 */
const SAMPLE_WEATHER_RESPONSE = {
  current: {
    temperature_2m: 72.1,
    weather_code: 0,
    wind_speed_10m: 8.5,
  },
};

/**
 * Set up a mock fetch that returns weather data.
 */
function mockWeatherFetch(
  responseBody: unknown = SAMPLE_WEATHER_RESPONSE,
  ok = true,
) {
  const mockFetch = vi.fn(async () => mockResponse(responseBody, ok));
  vi.stubGlobal("fetch", mockFetch);
  return mockFetch;
}

// --- Tests ---

describe("createWeatherProducer — successful fetch", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("fetches weather data and returns correct cache shape", async () => {
    mockWeatherFetch();
    const producer = createWeatherProducer(makeConfig());
    const result = (await producer()) as WeatherData | null;

    expect(result).not.toBeNull();
    expect(result!.temperature).toBe(72.1);
    expect(result!.weatherCode).toBe(0);
    expect(result!.windSpeed).toBe(8.5);
    expect(result!.description).toBe("Clear");
    expect(result!.emoji).toBe("\u2600\uFE0F");
    expect(result!.temperatureUnit).toBe("fahrenheit");
  });

  it("builds the correct API URL with configured coordinates", async () => {
    const mockFetch = mockWeatherFetch();
    const config = makeConfig({ latitude: 40.7128, longitude: -74.006 });
    const producer = createWeatherProducer(config);
    await producer();

    expect(mockFetch).toHaveBeenCalledTimes(1);
    const calledUrl = mockFetch.mock.calls[0][0] as string;
    expect(calledUrl).toContain("latitude=40.7128");
    expect(calledUrl).toContain("longitude=-74.006");
  });

  it("includes temperature_unit in API URL", async () => {
    const mockFetch = mockWeatherFetch();
    const config = makeConfig({ temperatureUnit: "celsius" });
    const producer = createWeatherProducer(config);
    await producer();

    const calledUrl = mockFetch.mock.calls[0][0] as string;
    expect(calledUrl).toContain("temperature_unit=celsius");
  });

  it("returns celsius unit in result when configured", async () => {
    const celsiusResponse = {
      current: {
        temperature_2m: 22.3,
        weather_code: 1,
        wind_speed_10m: 5.0,
      },
    };
    mockWeatherFetch(celsiusResponse);
    const config = makeConfig({ temperatureUnit: "celsius" });
    const producer = createWeatherProducer(config);
    const result = (await producer()) as WeatherData | null;

    expect(result).not.toBeNull();
    expect(result!.temperature).toBe(22.3);
    expect(result!.temperatureUnit).toBe("celsius");
  });

  it("returns weather with wind data", async () => {
    const windyResponse = {
      current: {
        temperature_2m: 58.0,
        weather_code: 61,
        wind_speed_10m: 25.3,
      },
    };
    mockWeatherFetch(windyResponse);
    const producer = createWeatherProducer(makeConfig());
    const result = (await producer()) as WeatherData | null;

    expect(result).not.toBeNull();
    expect(result!.windSpeed).toBe(25.3);
    expect(result!.description).toBe("Rain");
    expect(result!.emoji).toBe("\uD83C\uDF27\uFE0F");
  });
});

describe("createWeatherProducer — fail-open (ARCH-3)", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("returns null when fetch throws a network error", async () => {
    const mockFetch = vi.fn(async () => {
      throw new Error("Network error");
    });
    vi.stubGlobal("fetch", mockFetch);

    const producer = createWeatherProducer(makeConfig());
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when API response is not ok (500)", async () => {
    mockWeatherFetch(null, false);
    const producer = createWeatherProducer(makeConfig());
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null on malformed JSON (missing current field)", async () => {
    mockWeatherFetch({ latitude: 37.7749 }); // no 'current' field
    const producer = createWeatherProducer(makeConfig());
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when current.temperature_2m is undefined", async () => {
    mockWeatherFetch({
      current: {
        weather_code: 0,
        wind_speed_10m: 5,
      },
    });
    const producer = createWeatherProducer(makeConfig());
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when current.weather_code is undefined", async () => {
    mockWeatherFetch({
      current: {
        temperature_2m: 72,
        wind_speed_10m: 5,
      },
    });
    const producer = createWeatherProducer(makeConfig());
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when current.wind_speed_10m is undefined", async () => {
    mockWeatherFetch({
      current: {
        temperature_2m: 72,
        weather_code: 0,
      },
    });
    const producer = createWeatherProducer(makeConfig());
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when fetch is aborted (timeout simulation)", async () => {
    const mockFetch = vi.fn(async () => {
      const error = new DOMException("The operation was aborted.", "AbortError");
      throw error;
    });
    vi.stubGlobal("fetch", mockFetch);

    const producer = createWeatherProducer(makeConfig());
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when JSON parsing throws", async () => {
    const mockFetch = vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => { throw new SyntaxError("Unexpected token"); },
      text: async () => "not json",
    }));
    vi.stubGlobal("fetch", mockFetch);

    const producer = createWeatherProducer(makeConfig());
    const result = await producer();

    expect(result).toBeNull();
  });
});

describe("mapWeatherCode — WMO code to description and emoji", () => {
  it("maps code 0 to Clear with sun emoji", () => {
    const result = mapWeatherCode(0);
    expect(result.description).toBe("Clear");
    expect(result.emoji).toBe("\u2600\uFE0F");
  });

  it("maps codes 1-3 to Cloudy", () => {
    expect(mapWeatherCode(1).description).toBe("Cloudy");
    expect(mapWeatherCode(2).description).toBe("Cloudy");
    expect(mapWeatherCode(3).description).toBe("Cloudy");
    expect(mapWeatherCode(1).emoji).toBe("\u26C5");
  });

  it("maps codes 45-48 to Fog", () => {
    expect(mapWeatherCode(45).description).toBe("Fog");
    expect(mapWeatherCode(48).description).toBe("Fog");
    expect(mapWeatherCode(45).emoji).toBe("\uD83C\uDF2B\uFE0F");
  });

  it("maps codes 51-67 to Rain", () => {
    expect(mapWeatherCode(51).description).toBe("Rain");
    expect(mapWeatherCode(61).description).toBe("Rain");
    expect(mapWeatherCode(67).description).toBe("Rain");
    expect(mapWeatherCode(51).emoji).toBe("\uD83C\uDF27\uFE0F");
  });

  it("maps codes 71-77 to Snow", () => {
    expect(mapWeatherCode(71).description).toBe("Snow");
    expect(mapWeatherCode(77).description).toBe("Snow");
    expect(mapWeatherCode(71).emoji).toBe("\u2744\uFE0F");
  });

  it("maps codes 80-82 to Showers", () => {
    expect(mapWeatherCode(80).description).toBe("Showers");
    expect(mapWeatherCode(82).description).toBe("Showers");
    expect(mapWeatherCode(80).emoji).toBe("\uD83C\uDF26\uFE0F");
  });

  it("maps codes 85-86 to Snow Showers", () => {
    expect(mapWeatherCode(85).description).toBe("Snow Showers");
    expect(mapWeatherCode(86).description).toBe("Snow Showers");
  });

  it("maps codes 95-99 to Storm", () => {
    expect(mapWeatherCode(95).description).toBe("Storm");
    expect(mapWeatherCode(99).description).toBe("Storm");
    expect(mapWeatherCode(95).emoji).toBe("\u26C8\uFE0F");
  });

  it("returns Unknown for unrecognized codes", () => {
    expect(mapWeatherCode(4).description).toBe("Unknown");
    expect(mapWeatherCode(100).description).toBe("Unknown");
    expect(mapWeatherCode(-1).description).toBe("Unknown");
  });

  it("returns Unknown for gap codes (e.g., 10, 44, 68-70, 78-79, 83-84, 87-94)", () => {
    expect(mapWeatherCode(10).description).toBe("Unknown");
    expect(mapWeatherCode(44).description).toBe("Unknown");
    expect(mapWeatherCode(68).description).toBe("Unknown");
    expect(mapWeatherCode(78).description).toBe("Unknown");
    expect(mapWeatherCode(83).description).toBe("Unknown");
    expect(mapWeatherCode(90).description).toBe("Unknown");
  });
});
