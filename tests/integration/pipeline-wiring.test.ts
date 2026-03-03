/**
 * Pipeline Wiring Integration Tests — Batch B (Tasks 2.1, 2.2, 2.3)
 *
 * Verifies that feed producer output flows through the real cache bus
 * to segment renderers with no mocking of internal modules (ARCH-1).
 *
 * Each test uses its own temp directory (ARCH-2) and real file I/O.
 * File is named pipeline-WIRING.test.ts to avoid collision with the
 * existing pipeline.test.ts (ARCH-4).
 */

import { describe, it, expect, afterEach, vi, beforeEach } from "vitest";
import { createTestEnv, seedCache } from "./helpers.js";
import type { TestEnv } from "./helpers.js";
import { mergeKey, readKey, readAll } from "../../src/core/feeds/cache-bus.js";
import { BUILTIN_SEGMENTS } from "../../src/core/status-line/segments.js";
import { createPulseProducer } from "../../src/core/feeds/producers/pulse.js";
import type { PulseFeedConfig, CacheEntry } from "../../src/core/types.js";

// ---------------------------------------------------------------------------
// Shared state for afterEach cleanup
// ---------------------------------------------------------------------------

let env: TestEnv;

// ---------------------------------------------------------------------------
// Task 2.1 — Cache round-trip and basic pipeline wiring
// ---------------------------------------------------------------------------

describe("pipeline-wiring: cache round-trip and basic wiring", () => {
  afterEach(() => {
    env?.cleanup();
  });

  it("producer output → mergeKey → readKey returns data (cache round-trip)", () => {
    env = createTestEnv();

    // Simulate pulse producer output: write data through real cache bus
    const producerOutput = {
      value: "\u{1F7E2}",
      elapsed_minutes: 5,
      session_start: new Date().toISOString(),
    };

    mergeKey(env.cachePath, "pulse", producerOutput, 300);
    const result = readKey<CacheEntry>(env.cachePath, "pulse");

    expect(result).not.toBeNull();
    expect(result!.value).toBe("\u{1F7E2}");
    expect(result!.elapsed_minutes).toBe(5);
    expect(result!.updated_at).toBeDefined();
    expect(result!.ttl_seconds).toBe(300);
  });

  it("producer output → mergeKey → segment renders non-empty (full pipeline wiring)", () => {
    env = createTestEnv();

    // Write pulse data to the cache via real mergeKey
    const pulseData = {
      value: "\u{1F7E2}",
      elapsed_minutes: 10,
      session_start: new Date().toISOString(),
    };
    mergeKey(env.cachePath, "pulse", pulseData, 300);

    // Read back the full cache and pass to the segment renderer
    const cache = readAll(env.cachePath);
    const segmentFn = BUILTIN_SEGMENTS["pulse"];
    const output = segmentFn(cache, { builtin: "pulse" });

    expect(output).toBeTruthy();
    expect(output).toContain("\u{1F7E2}");
  });

  it("producer returns null → segment renders empty (fail-open)", () => {
    env = createTestEnv();

    // Do NOT write any pulse data — simulates a producer that returned null
    const cache = readAll(env.cachePath);
    const segmentFn = BUILTIN_SEGMENTS["pulse"];
    const output = segmentFn(cache, { builtin: "pulse" });

    expect(output).toBe("");
  });
});

// ---------------------------------------------------------------------------
// Task 2.2 — TTL, atomic merge, orphan detection
// ---------------------------------------------------------------------------

describe("pipeline-wiring: TTL, atomic merge, and orphan detection", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-03-03T12:00:00Z"));
  });

  afterEach(() => {
    vi.useRealTimers();
    env?.cleanup();
  });

  it("cache TTL expired → readKey returns null → segment empty (ARCH-5)", () => {
    env = createTestEnv();

    // Write pulse data with a 60-second TTL
    const pulseData = {
      value: "\u{1F7E1}",
      elapsed_minutes: 35,
      session_start: "2026-03-03T11:25:00Z",
    };
    mergeKey(env.cachePath, "pulse", pulseData, 60);

    // Verify it reads fine while fresh
    const freshResult = readKey<CacheEntry>(env.cachePath, "pulse");
    expect(freshResult).not.toBeNull();

    // Advance time past the TTL
    vi.setSystemTime(new Date("2026-03-03T12:02:00Z")); // 2 min later

    // readKey should now return null (TTL expired)
    const staleResult = readKey<CacheEntry>(env.cachePath, "pulse");
    expect(staleResult).toBeNull();

    // Segment should render empty for stale data
    const cache = readAll(env.cachePath);
    const segmentFn = BUILTIN_SEGMENTS["pulse"];
    const output = segmentFn(cache, { builtin: "pulse" });
    expect(output).toBe("");
  });

  it("multiple producers write different keys → keys don't overwrite (atomic merge)", () => {
    vi.useRealTimers(); // This test doesn't need fake timers
    env = createTestEnv();

    // Producer 1: pulse
    const pulseData = {
      value: "\u{1F7E2}",
      elapsed_minutes: 5,
      session_start: new Date().toISOString(),
    };
    mergeKey(env.cachePath, "pulse", pulseData, 300);

    // Producer 2: project (simulate project data)
    const projectData = {
      repo: "hookwise",
      branch: "main",
      last_commit_ts: Math.floor(Date.now() / 1000),
      detached: false,
      has_commits: true,
    };
    mergeKey(env.cachePath, "project", projectData, 300);

    // Producer 3: news (simulate news data)
    const newsData = {
      stories: [{ title: "Test Story", score: 42, url: "https://example.com", id: 1 }],
      current_index: 0,
      current_story: { title: "Test Story", score: 42, url: "https://example.com", id: 1 },
      last_rotation: new Date().toISOString(),
    };
    mergeKey(env.cachePath, "news", newsData, 300);

    // All keys should be independently readable
    const pulseResult = readKey<CacheEntry>(env.cachePath, "pulse");
    const projectResult = readKey<CacheEntry>(env.cachePath, "project");
    const newsResult = readKey<CacheEntry>(env.cachePath, "news");

    expect(pulseResult).not.toBeNull();
    expect(pulseResult!.value).toBe("\u{1F7E2}");

    expect(projectResult).not.toBeNull();
    expect(projectResult!.repo).toBe("hookwise");

    expect(newsResult).not.toBeNull();
    expect(newsResult!.current_story).toEqual(
      expect.objectContaining({ title: "Test Story", score: 42 }),
    );
  });

  it("each builtin producer has a corresponding segment (orphan detection)", () => {
    vi.useRealTimers(); // No timer manipulation needed

    // The builtin feed producers and their corresponding segment names.
    // A producer is "orphaned" if its cache key has no segment that reads it.
    //
    // Mapping:
    //   pulse    → "pulse" segment
    //   project  → "project" segment
    //   calendar → "calendar" segment
    //   news     → "news" segment
    //   insights → "insights_friction", "insights_pace", "insights_trend"
    //   practice → "practice", "practice_breadcrumb"
    //   weather  → "weather" segment
    //   memories → "memories" segment
    const PRODUCER_TO_SEGMENTS: Record<string, string[]> = {
      pulse: ["pulse"],
      project: ["project"],
      calendar: ["calendar"],
      news: ["news"],
      insights: ["insights_friction", "insights_pace", "insights_trend"],
      practice: ["practice", "practice_breadcrumb"],
      weather: ["weather"],
      memories: ["memories"],
    };

    const allSegmentNames = Object.keys(BUILTIN_SEGMENTS);

    for (const [producer, expectedSegments] of Object.entries(PRODUCER_TO_SEGMENTS)) {
      for (const segName of expectedSegments) {
        expect(
          allSegmentNames,
          `Producer "${producer}" expects segment "${segName}" but it is not in BUILTIN_SEGMENTS`,
        ).toContain(segName);
      }
    }
  });
});

// ---------------------------------------------------------------------------
// Task 2.3 — Reusable pipeline flow helper
// ---------------------------------------------------------------------------

/**
 * Reusable helper that verifies the full producer-to-segment pipeline:
 *   1. Calls the producer function
 *   2. Writes its output to the cache via mergeKey
 *   3. Reads the cache back
 *   4. Passes the cache to the named segment renderer
 *   5. Asserts the rendered output contains the expected substring
 *
 * @param producerFn        - Async function returning producer data or null
 * @param segmentName       - Name of the BUILTIN_SEGMENTS entry to render
 * @param cachePath         - Path to the cache file
 * @param expectedSubstring - Substring the rendered segment output must contain
 */
async function testPipelineFlow(
  producerFn: () => Promise<Record<string, unknown> | null>,
  segmentName: string,
  cachePath: string,
  expectedSubstring: string,
): Promise<void> {
  // Step 1: Call the producer
  const data = await producerFn();
  expect(data).not.toBeNull();

  // Step 2: Write to cache via real mergeKey
  // The cache key is the segment name for simple 1:1 mappings.
  // For insights segments (insights_friction, etc.) the cache key is "insights".
  const cacheKey = segmentName.startsWith("insights_") ? "insights" : segmentName;
  mergeKey(cachePath, cacheKey, data!, 300);

  // Step 3: Read back full cache
  const cache = readAll(cachePath);

  // Step 4: Render through the segment
  const segmentFn = BUILTIN_SEGMENTS[segmentName];
  expect(segmentFn, `Segment "${segmentName}" not found in BUILTIN_SEGMENTS`).toBeDefined();
  const output = segmentFn(cache, { builtin: segmentName });

  // Step 5: Assert output contains expected substring
  expect(output).toContain(expectedSubstring);
}

describe("pipeline-wiring: reusable testPipelineFlow helper", () => {
  afterEach(() => {
    env?.cleanup();
  });

  it("testPipelineFlow works with pulse producer", async () => {
    env = createTestEnv();

    // Pulse producer requires a session entry in cache with startedAt
    const sessionData = {
      startedAt: new Date(Date.now() - 5 * 60_000).toISOString(), // 5 min ago
      toolCalls: 3,
    };
    mergeKey(env.cachePath, "session", sessionData, 300);

    // Create the pulse producer and run it through the pipeline helper
    const pulseFeedConfig: PulseFeedConfig = {
      enabled: true,
      intervalSeconds: 30,
      thresholds: { green: 0, yellow: 30, orange: 60, red: 120, skull: 180 },
    };
    const pulseProducer = createPulseProducer(env.cachePath, pulseFeedConfig);

    await testPipelineFlow(
      pulseProducer,
      "pulse",
      env.cachePath,
      "\u{1F7E2}", // green circle (< 30 min)
    );
  });

  it("testPipelineFlow works with insights data (insights_pace segment)", async () => {
    env = createTestEnv();

    // Simulate insights producer output — we directly create the data
    // since the real insights producer reads from ~/.claude/usage-data/
    // which we cannot easily populate in a temp dir.
    const insightsData = {
      total_sessions: 12,
      total_messages: 240,
      total_lines_added: 5000,
      days_active: 10,
      top_tools: [
        { name: "Edit", count: 150 },
        { name: "Read", count: 80 },
      ],
      peak_hour: 14,
      friction_total: 3,
      recent_session: {
        friction_count: 0,
      },
    };

    // Use a synthetic producer that returns the simulated data
    const syntheticInsightsProducer = async () => insightsData;

    await testPipelineFlow(
      syntheticInsightsProducer,
      "insights_pace",
      env.cachePath,
      "msgs/day", // insights_pace renders "X msgs/day"
    );
  });
});

// ---------------------------------------------------------------------------
// Task: Practice producer → practice & practice_breadcrumb segments (GH#8)
// ---------------------------------------------------------------------------

describe("pipeline-wiring: practice producer to segment pipeline", () => {
  afterEach(() => {
    env?.cleanup();
  });

  it("practice producer output → mergeKey → practice segment renders today count", async () => {
    env = createTestEnv();

    // Simulate practice producer output: write practice data through real cache bus
    const practiceData = {
      todayTotal: 3,
      dueReviews: 5,
      last_at: new Date().toISOString(),
    };

    mergeKey(env.cachePath, "practice", practiceData, 300);

    // Read back the full cache and pass to the segment renderer
    const cache = readAll(env.cachePath);
    const segmentFn = BUILTIN_SEGMENTS["practice"];
    const output = segmentFn(cache, { builtin: "practice" });

    expect(output).toBeTruthy();
    expect(output).toContain("3 today");
  });

  it("practice producer output → mergeKey → practice_breadcrumb renders relative time", async () => {
    env = createTestEnv();

    // Write practice data with a last_at timestamp from 10 minutes ago
    const tenMinAgo = new Date(Date.now() - 10 * 60_000).toISOString();
    const practiceData = {
      todayTotal: 2,
      dueReviews: 4,
      last_at: tenMinAgo,
    };

    mergeKey(env.cachePath, "practice", practiceData, 300);

    const cache = readAll(env.cachePath);
    const segmentFn = BUILTIN_SEGMENTS["practice_breadcrumb"];
    const output = segmentFn(cache, { builtin: "practice_breadcrumb" });

    expect(output).toBeTruthy();
    expect(output).toContain("Last practice:");
    expect(output).toContain("ago");
  });

  it("practice producer returns null → practice segment renders empty", () => {
    env = createTestEnv();

    // Do NOT write any practice data — simulates a producer that returned null
    const cache = readAll(env.cachePath);
    const segmentFn = BUILTIN_SEGMENTS["practice"];
    const output = segmentFn(cache, { builtin: "practice" });

    expect(output).toBe("");
  });

  it("testPipelineFlow works with practice data (practice segment)", async () => {
    env = createTestEnv();

    // Simulate practice producer output
    const practiceData = {
      todayTotal: 5,
      dueReviews: 2,
      last_at: new Date().toISOString(),
    };

    const syntheticPracticeProducer = async () =>
      practiceData as unknown as Record<string, unknown>;

    await testPipelineFlow(
      syntheticPracticeProducer,
      "practice",
      env.cachePath,
      "5 today",
    );
  });

  it("practice producer registered in orphan detection map", () => {
    // Verify the practice producer has corresponding segments
    const PRODUCER_TO_SEGMENTS: Record<string, string[]> = {
      practice: ["practice", "practice_breadcrumb"],
    };

    const allSegmentNames = Object.keys(BUILTIN_SEGMENTS);

    for (const [producer, expectedSegments] of Object.entries(PRODUCER_TO_SEGMENTS)) {
      for (const segName of expectedSegments) {
        expect(
          allSegmentNames,
          `Producer "${producer}" expects segment "${segName}" but it is not in BUILTIN_SEGMENTS`,
        ).toContain(segName);
      }
    }
  });
});

// ---------------------------------------------------------------------------
// Task: Weather producer → weather segment pipeline
// ---------------------------------------------------------------------------

describe("pipeline-wiring: weather producer to segment pipeline", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-03-03T12:00:00Z"));
  });

  afterEach(() => {
    vi.useRealTimers();
    env?.cleanup();
  });

  it("weather producer output → mergeKey → weather segment renders temp", async () => {
    env = createTestEnv();

    // Simulate weather producer output
    const weatherData = {
      temperature: 72,
      weatherCode: 0,
      windSpeed: 8.5,
      description: "Clear",
      emoji: "\u2600\uFE0F",
      temperatureUnit: "fahrenheit",
    };

    mergeKey(env.cachePath, "weather", weatherData, 600);

    const cache = readAll(env.cachePath);
    const segmentFn = BUILTIN_SEGMENTS["weather"];
    const output = segmentFn(cache, { builtin: "weather" });

    expect(output).toBeTruthy();
    expect(output).toContain("72");
    expect(output).toContain("\u00B0F");
    expect(output).toContain("\u2600\uFE0F");
  });

  it("weather producer output with high wind → segment shows wind indicator", async () => {
    env = createTestEnv();

    const weatherData = {
      temperature: 58,
      weatherCode: 61,
      windSpeed: 25.3,
      description: "Rain",
      emoji: "\uD83C\uDF27\uFE0F",
      temperatureUnit: "fahrenheit",
    };

    mergeKey(env.cachePath, "weather", weatherData, 600);

    const cache = readAll(env.cachePath);
    const segmentFn = BUILTIN_SEGMENTS["weather"];
    const output = segmentFn(cache, { builtin: "weather" });

    expect(output).toContain("\uD83D\uDCA8"); // wind emoji
  });

  it("weather producer returns null → weather segment renders empty", () => {
    env = createTestEnv();

    // Do NOT write any weather data — simulates a producer that returned null
    const cache = readAll(env.cachePath);
    const segmentFn = BUILTIN_SEGMENTS["weather"];
    const output = segmentFn(cache, { builtin: "weather" });

    expect(output).toBe("");
  });

  it("testPipelineFlow works with weather data (weather segment)", async () => {
    env = createTestEnv();

    const weatherData = {
      temperature: 65,
      weatherCode: 2,
      windSpeed: 12,
      description: "Cloudy",
      emoji: "\u26C5",
      temperatureUnit: "fahrenheit",
    };

    const syntheticWeatherProducer = async () =>
      weatherData as unknown as Record<string, unknown>;

    await testPipelineFlow(
      syntheticWeatherProducer,
      "weather",
      env.cachePath,
      "65",
    );
  });

  it("weather producer registered in orphan detection map", () => {
    vi.useRealTimers();

    const PRODUCER_TO_SEGMENTS: Record<string, string[]> = {
      weather: ["weather"],
    };

    const allSegmentNames = Object.keys(BUILTIN_SEGMENTS);

    for (const [producer, expectedSegments] of Object.entries(PRODUCER_TO_SEGMENTS)) {
      for (const segName of expectedSegments) {
        expect(
          allSegmentNames,
          `Producer "${producer}" expects segment "${segName}" but it is not in BUILTIN_SEGMENTS`,
        ).toContain(segName);
      }
    }
  });
});

// ---------------------------------------------------------------------------
// Task: Memories producer → memories segment pipeline (GH#80)
// ---------------------------------------------------------------------------

describe("pipeline-wiring: memories producer to segment pipeline", () => {
  afterEach(() => {
    env?.cleanup();
  });

  it("memories producer output → mergeKey → memories segment renders", async () => {
    env = createTestEnv();

    // Simulate memories producer output
    const memoriesData = {
      memories: [
        { date: "2025-03-03", daysSince: 365, label: "1 year ago", toolCalls: 42, filesEdited: 15 },
        { date: "2026-02-24", daysSince: 7, label: "1 week ago", toolCalls: 18, filesEdited: 6 },
      ],
      hasMemories: true,
    };

    mergeKey(env.cachePath, "memories", memoriesData, 3600);

    const cache = readAll(env.cachePath);
    const segmentFn = BUILTIN_SEGMENTS["memories"];
    const output = segmentFn(cache, { builtin: "memories" });

    expect(output).toBeTruthy();
    expect(output).toContain("On this day");
    expect(output).toContain("2 sessions");
  });

  it("memories producer returns null → memories segment renders empty", () => {
    env = createTestEnv();

    // Do NOT write any memories data — simulates a producer that returned null
    const cache = readAll(env.cachePath);
    const segmentFn = BUILTIN_SEGMENTS["memories"];
    const output = segmentFn(cache, { builtin: "memories" });

    expect(output).toBe("");
  });

  it("testPipelineFlow works with memories data", async () => {
    env = createTestEnv();

    const memoriesData = {
      memories: [
        { date: "2025-03-03", daysSince: 365, label: "1 year ago", toolCalls: 50, filesEdited: 20 },
      ],
      hasMemories: true,
    };

    const syntheticMemoriesProducer = async () =>
      memoriesData as unknown as Record<string, unknown>;

    await testPipelineFlow(
      syntheticMemoriesProducer,
      "memories",
      env.cachePath,
      "On this day",
    );
  });

  it("memories producer registered in orphan detection map", () => {
    const PRODUCER_TO_SEGMENTS: Record<string, string[]> = {
      memories: ["memories"],
    };

    const allSegmentNames = Object.keys(BUILTIN_SEGMENTS);

    for (const [producer, expectedSegments] of Object.entries(PRODUCER_TO_SEGMENTS)) {
      for (const segName of expectedSegments) {
        expect(
          allSegmentNames,
          `Producer "${producer}" expects segment "${segName}" but it is not in BUILTIN_SEGMENTS`,
        ).toContain(segName);
      }
    }
  });
});
