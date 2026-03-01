/**
 * Tests for the news feed producer.
 *
 * Covers Task 4.3:
 * - HN API parsing: fetches top story IDs, then fetches each story's details
 * - Rotation logic: index advances after rotationMinutes, wraps at end
 * - RSS fallback: parses XML items with title and link, score = 0
 * - Network error handling: returns null on fetch failure
 *
 * All HTTP calls are mocked via vi.stubGlobal("fetch").
 *
 * Requirements: FR-7.1, FR-7.2, FR-7.3, FR-7.4, FR-7.5, FR-7.6, FR-7.7
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

// Mock cache-bus so the producer can read/write rotation state between calls.
// We store the last producer output and return it from readKey on subsequent calls.
let lastNewsResult: Record<string, unknown> | null = null;
vi.mock("../../../../src/core/feeds/cache-bus.js", () => ({
  readKey: vi.fn((_cachePath: string, key: string) => {
    if (key === "news" && lastNewsResult) {
      return {
        ...lastNewsResult,
        updated_at: new Date().toISOString(),
        ttl_seconds: 300,
      };
    }
    return null;
  }),
}));

import { createNewsProducer } from "../../../../src/core/feeds/producers/news.js";
import type { NewsFeedConfig } from "../../../../src/core/types.js";
import type { NewsData } from "../../../../src/core/feeds/producers/news.js";

// --- Helpers ---

function makeConfig(overrides?: Partial<NewsFeedConfig>): NewsFeedConfig {
  return {
    enabled: true,
    source: "hackernews",
    rssUrl: null,
    intervalSeconds: 300,
    maxStories: 3,
    rotationMinutes: 10,
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
 * Sample HN story data for mocking.
 */
const HN_STORIES = [
  { id: 100, title: "Story A", score: 150, url: "https://example.com/a" },
  { id: 200, title: "Story B", score: 120, url: "https://example.com/b" },
  { id: 300, title: "Story C", score: 90, url: "https://example.com/c" },
];

/**
 * Set up a mock fetch that simulates the HN Firebase API.
 */
function mockHNFetch(stories = HN_STORIES) {
  const ids = stories.map((s) => s.id);
  const mockFetch = vi.fn(async (url: string | URL | Request) => {
    const urlStr = typeof url === "string" ? url : url.toString();

    if (urlStr.includes("topstories.json")) {
      return mockResponse(ids);
    }

    // Match item/{id}.json
    const idMatch = urlStr.match(/\/item\/(\d+)\.json/);
    if (idMatch) {
      const id = parseInt(idMatch[1], 10);
      const story = stories.find((s) => s.id === id);
      return story ? mockResponse(story) : mockResponse(null, false);
    }

    return mockResponse(null, false);
  });

  vi.stubGlobal("fetch", mockFetch);
  return mockFetch;
}

/**
 * Sample RSS feed XML for mocking.
 */
const RSS_XML = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <item>
      <title>RSS Story 1</title>
      <link>https://rss.example.com/1</link>
      <description>First story</description>
    </item>
    <item>
      <title>RSS Story 2</title>
      <link>https://rss.example.com/2</link>
      <description>Second story</description>
    </item>
    <item>
      <title>RSS Story 3</title>
      <link>https://rss.example.com/3</link>
      <description>Third story</description>
    </item>
  </channel>
</rss>`;

/**
 * Set up a mock fetch that returns RSS XML.
 */
function mockRSSFetch(xml = RSS_XML, ok = true) {
  const mockFetch = vi.fn(async () => mockResponse(xml, ok));
  vi.stubGlobal("fetch", mockFetch);
  return mockFetch;
}

// --- Tests ---

/**
 * Call the producer and store the result for cache-bus readKey mock.
 * Simulates the daemon loop: producer runs → result written to cache → next call reads it.
 */
async function callAndStore(producer: () => Promise<Record<string, unknown> | null>): Promise<NewsData | null> {
  const result = await producer();
  lastNewsResult = result;
  return result as NewsData | null;
}

describe("createNewsProducer — HN mode", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-02-22T12:00:00Z"));
    lastNewsResult = null;
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("fetches top story IDs and their details from HN API (FR-7.1)", async () => {
    mockHNFetch();
    const producer = createNewsProducer(makeConfig());
    const result = (await producer()) as NewsData | null;

    expect(result).not.toBeNull();
    expect(result!.stories).toHaveLength(3);
    expect(result!.stories[0]).toEqual({
      id: 100,
      title: "Story A",
      score: 150,
      url: "https://example.com/a",
    });
    expect(result!.stories[1]).toEqual({
      id: 200,
      title: "Story B",
      score: 120,
      url: "https://example.com/b",
    });
    expect(result!.stories[2]).toEqual({
      id: 300,
      title: "Story C",
      score: 90,
      url: "https://example.com/c",
    });
  });

  it("respects maxStories config to limit fetched stories (FR-7.2)", async () => {
    const fiveStories = [
      ...HN_STORIES,
      { id: 400, title: "Story D", score: 80, url: "https://example.com/d" },
      { id: 500, title: "Story E", score: 70, url: "https://example.com/e" },
    ];
    mockHNFetch(fiveStories);

    const producer = createNewsProducer(makeConfig({ maxStories: 2 }));
    const result = (await producer()) as NewsData | null;

    expect(result).not.toBeNull();
    expect(result!.stories).toHaveLength(2);
    expect(result!.stories[0].id).toBe(100);
    expect(result!.stories[1].id).toBe(200);
  });

  it("starts with current_index 0 (FR-7.3)", async () => {
    mockHNFetch();
    const producer = createNewsProducer(makeConfig());
    const result = (await producer()) as NewsData | null;

    expect(result).not.toBeNull();
    expect(result!.current_index).toBe(0);
    expect(result!.current_story).toEqual(result!.stories[0]);
  });

  it("includes last_rotation as ISO 8601 timestamp (FR-7.3)", async () => {
    mockHNFetch();
    const producer = createNewsProducer(makeConfig());
    const result = (await producer()) as NewsData | null;

    expect(result).not.toBeNull();
    // Should be a parseable ISO date
    const parsed = Date.parse(result!.last_rotation);
    expect(Number.isNaN(parsed)).toBe(false);
  });

  it("does not rotate before rotationMinutes has elapsed (FR-7.4)", async () => {
    mockHNFetch();
    const config = makeConfig({ rotationMinutes: 10 });
    const producer = createNewsProducer(config);

    // First call — index 0
    const r1 = await callAndStore(producer);
    expect(r1!.current_index).toBe(0);

    // Advance 5 minutes (less than 10)
    vi.advanceTimersByTime(5 * 60_000);
    const r2 = await callAndStore(producer);
    expect(r2!.current_index).toBe(0);
  });

  it("rotates after rotationMinutes has elapsed (FR-7.4)", async () => {
    mockHNFetch();
    const config = makeConfig({ rotationMinutes: 10 });
    const producer = createNewsProducer(config);

    // First call — index 0
    await callAndStore(producer);

    // Advance 10 minutes
    vi.advanceTimersByTime(10 * 60_000);
    const r2 = await callAndStore(producer);
    expect(r2!.current_index).toBe(1);
    expect(r2!.current_story).toEqual(r2!.stories[1]);
  });

  it("wraps rotation index back to 0 after reaching end of stories (FR-7.5)", async () => {
    mockHNFetch();
    const config = makeConfig({ rotationMinutes: 1 });
    const producer = createNewsProducer(config);

    // index 0
    await callAndStore(producer);

    // Rotate to index 1
    vi.advanceTimersByTime(1 * 60_000);
    await callAndStore(producer);

    // Rotate to index 2
    vi.advanceTimersByTime(1 * 60_000);
    await callAndStore(producer);

    // Rotate to index 0 (wrap around, 3 stories)
    vi.advanceTimersByTime(1 * 60_000);
    const r4 = await callAndStore(producer);
    expect(r4!.current_index).toBe(0);
    expect(r4!.current_story).toEqual(r4!.stories[0]);
  });

  it("handles stories with missing url gracefully (defaults to empty string)", async () => {
    const stories = [
      { id: 100, title: "No URL Story", score: 50 },
    ];
    const mockFetch = vi.fn(async (url: string | URL | Request) => {
      const urlStr = typeof url === "string" ? url : url.toString();
      if (urlStr.includes("topstories.json")) return mockResponse([100]);
      if (urlStr.includes("/item/100.json")) return mockResponse(stories[0]);
      return mockResponse(null, false);
    });
    vi.stubGlobal("fetch", mockFetch);

    const producer = createNewsProducer(makeConfig({ maxStories: 1 }));
    const result = (await producer()) as NewsData | null;

    expect(result).not.toBeNull();
    expect(result!.stories[0].url).toBe("");
    expect(result!.stories[0].title).toBe("No URL Story");
  });

  it("returns null when top stories fetch fails (network error) (FR-7.7)", async () => {
    const mockFetch = vi.fn(async () => {
      throw new Error("Network error");
    });
    vi.stubGlobal("fetch", mockFetch);

    const producer = createNewsProducer(makeConfig());
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when top stories response is not ok (FR-7.7)", async () => {
    const mockFetch = vi.fn(async () => mockResponse(null, false));
    vi.stubGlobal("fetch", mockFetch);

    const producer = createNewsProducer(makeConfig());
    const result = await producer();

    expect(result).toBeNull();
  });

  it("skips individual stories that fail to fetch", async () => {
    const mockFetch = vi.fn(async (url: string | URL | Request) => {
      const urlStr = typeof url === "string" ? url : url.toString();
      if (urlStr.includes("topstories.json")) return mockResponse([100, 200, 300]);
      if (urlStr.includes("/item/100.json")) return mockResponse(HN_STORIES[0]);
      if (urlStr.includes("/item/200.json")) return mockResponse(null, false); // fails
      if (urlStr.includes("/item/300.json")) return mockResponse(HN_STORIES[2]);
      return mockResponse(null, false);
    });
    vi.stubGlobal("fetch", mockFetch);

    const producer = createNewsProducer(makeConfig());
    const result = (await producer()) as NewsData | null;

    expect(result).not.toBeNull();
    // Story B (id=200) was skipped
    expect(result!.stories).toHaveLength(2);
    expect(result!.stories[0].id).toBe(100);
    expect(result!.stories[1].id).toBe(300);
  });

  it("returns null when all individual story fetches fail", async () => {
    const mockFetch = vi.fn(async (url: string | URL | Request) => {
      const urlStr = typeof url === "string" ? url : url.toString();
      if (urlStr.includes("topstories.json")) return mockResponse([100, 200]);
      // All items fail
      return mockResponse(null, false);
    });
    vi.stubGlobal("fetch", mockFetch);

    const producer = createNewsProducer(makeConfig({ maxStories: 2 }));
    const result = await producer();

    expect(result).toBeNull();
  });
});

describe("createNewsProducer — RSS mode", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-02-22T12:00:00Z"));
    lastNewsResult = null;
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("parses RSS XML and returns stories with score 0 (FR-7.6)", async () => {
    mockRSSFetch();
    const config = makeConfig({
      source: "rss",
      rssUrl: "https://feeds.example.com/rss.xml",
      maxStories: 3,
    });
    const producer = createNewsProducer(config);
    const result = (await producer()) as NewsData | null;

    expect(result).not.toBeNull();
    expect(result!.stories).toHaveLength(3);
    expect(result!.stories[0]).toEqual({
      title: "RSS Story 1",
      score: 0,
      url: "https://rss.example.com/1",
      id: 1,
    });
    expect(result!.stories[1]).toEqual({
      title: "RSS Story 2",
      score: 0,
      url: "https://rss.example.com/2",
      id: 2,
    });
  });

  it("respects maxStories for RSS feeds (FR-7.2)", async () => {
    mockRSSFetch();
    const config = makeConfig({
      source: "rss",
      rssUrl: "https://feeds.example.com/rss.xml",
      maxStories: 1,
    });
    const producer = createNewsProducer(config);
    const result = (await producer()) as NewsData | null;

    expect(result).not.toBeNull();
    expect(result!.stories).toHaveLength(1);
    expect(result!.stories[0].title).toBe("RSS Story 1");
  });

  it("returns null when RSS fetch fails (FR-7.7)", async () => {
    mockRSSFetch(RSS_XML, false);
    const config = makeConfig({
      source: "rss",
      rssUrl: "https://feeds.example.com/rss.xml",
    });
    const producer = createNewsProducer(config);
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when RSS fetch throws a network error (FR-7.7)", async () => {
    const mockFetch = vi.fn(async () => {
      throw new Error("DNS resolution failed");
    });
    vi.stubGlobal("fetch", mockFetch);

    const config = makeConfig({
      source: "rss",
      rssUrl: "https://feeds.example.com/rss.xml",
    });
    const producer = createNewsProducer(config);
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when RSS XML has no matching items", async () => {
    mockRSSFetch("<rss><channel><title>Empty</title></channel></rss>");
    const config = makeConfig({
      source: "rss",
      rssUrl: "https://feeds.example.com/rss.xml",
    });
    const producer = createNewsProducer(config);
    const result = await producer();

    expect(result).toBeNull();
  });

  it("supports rotation logic for RSS stories just like HN (FR-7.4, FR-7.5)", async () => {
    mockRSSFetch();
    const config = makeConfig({
      source: "rss",
      rssUrl: "https://feeds.example.com/rss.xml",
      maxStories: 3,
      rotationMinutes: 5,
    });
    const producer = createNewsProducer(config);

    // Index 0
    const r1 = await callAndStore(producer);
    expect(r1!.current_index).toBe(0);

    // Advance 5 minutes -> rotate to index 1
    vi.advanceTimersByTime(5 * 60_000);
    const r2 = await callAndStore(producer);
    expect(r2!.current_index).toBe(1);

    // Advance 5 minutes -> rotate to index 2
    vi.advanceTimersByTime(5 * 60_000);
    const r3 = await callAndStore(producer);
    expect(r3!.current_index).toBe(2);

    // Advance 5 minutes -> wrap to index 0
    vi.advanceTimersByTime(5 * 60_000);
    const r4 = await callAndStore(producer);
    expect(r4!.current_index).toBe(0);
  });

  it("falls back to HN when source is rss but rssUrl is null (FR-7.6)", async () => {
    mockHNFetch();
    const config = makeConfig({
      source: "rss",
      rssUrl: null,  // not set, should fall back to HN
    });
    const producer = createNewsProducer(config);
    const result = (await producer()) as NewsData | null;

    expect(result).not.toBeNull();
    // Should have HN stories, not RSS
    expect(result!.stories[0].score).toBe(150);
  });
});
