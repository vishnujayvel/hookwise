/**
 * News feed producer: fetches top stories from Hacker News or an RSS feed.
 *
 * HN mode (default):
 *   1. Fetches top story IDs from the HN Firebase API
 *   2. Fetches details (title, score, URL) for each of the top N stories
 *   3. Maintains a rotation index that advances on a configurable interval
 *
 * RSS mode (when source === "rss" and rssUrl is set):
 *   Fetches the RSS feed URL and parses story titles and links from XML.
 *   Score is always 0 for RSS items (not available).
 *
 * Network failures are caught and return null.
 *
 * Requirements: FR-7.1, FR-7.2, FR-7.3, FR-7.4, FR-7.5, FR-7.6, FR-7.7
 */

import type { NewsFeedConfig, FeedProducer } from "../../types.js";
import { readKey } from "../cache-bus.js";
import type { CacheEntry } from "../../types.js";
import { DEFAULT_CACHE_PATH } from "../../constants.js";

export interface NewsStory {
  title: string;
  score: number;
  url: string;
  id: number;
}

export interface NewsData {
  stories: NewsStory[];
  current_index: number;
  current_story: NewsStory;
  last_rotation: string;  // ISO 8601
}

const HN_TOP_STORIES_URL = "https://hacker-news.firebaseio.com/v0/topstories.json";
const HN_ITEM_URL = "https://hacker-news.firebaseio.com/v0/item";

/**
 * Fetch top stories from the Hacker News Firebase API.
 *
 * @param maxStories - Maximum number of stories to fetch (default 5)
 * @returns Array of NewsStory objects, or null on failure
 */
async function fetchHNStories(maxStories: number): Promise<NewsStory[] | null> {
  const response = await fetch(HN_TOP_STORIES_URL);
  if (!response.ok) return null;

  const ids = (await response.json()) as number[];
  const topIds = ids.slice(0, maxStories);

  const stories = (await Promise.all(
    topIds.map(async (id): Promise<NewsStory | null> => {
      try {
        const itemResponse = await fetch(`${HN_ITEM_URL}/${id}.json`);
        if (!itemResponse.ok) return null;

        const item = (await itemResponse.json()) as {
          title?: string;
          score?: number;
          url?: string;
          id?: number;
        };

        if (item && item.title) {
          return {
            title: item.title,
            score: item.score ?? 0,
            url: item.url ?? "",
            id: item.id ?? id,
          };
        }
        return null;
      } catch {
        return null;
      }
    }),
  )).filter((s): s is NewsStory => s !== null);

  return stories.length > 0 ? stories : null;
}

/**
 * Fetch stories from an RSS feed URL.
 * Uses regex to parse XML <item> elements for <title> and <link>.
 *
 * @param rssUrl - The RSS feed URL to fetch
 * @param maxStories - Maximum number of stories to return
 * @returns Array of NewsStory objects, or null on failure
 */
async function fetchRSSStories(rssUrl: string, maxStories: number): Promise<NewsStory[] | null> {
  const response = await fetch(rssUrl);
  if (!response.ok) return null;

  const xml = await response.text();
  // Handle both plain text and CDATA-wrapped titles: <title>plain</title> or <title><![CDATA[wrapped]]></title>
  const itemRegex = /<item>[\s\S]*?<title>(?:<!\[CDATA\[)?([\s\S]*?)(?:\]\]>)?<\/title>[\s\S]*?<link>(?:<!\[CDATA\[)?([\s\S]*?)(?:\]\]>)?<\/link>[\s\S]*?<\/item>/g;
  const stories: NewsStory[] = [];
  let match: RegExpExecArray | null;
  let idCounter = 1;

  while ((match = itemRegex.exec(xml)) !== null && stories.length < maxStories) {
    stories.push({
      title: match[1],
      score: 0,
      url: match[2],
      id: idCounter++,
    });
  }

  return stories.length > 0 ? stories : null;
}

/**
 * Create a FeedProducer for the news feed.
 *
 * Rotation state (current_index, last_rotation) is read from and written
 * back to the cache bus entry, keeping the producer stateless. This allows
 * the daemon to restart without losing rotation position.
 *
 * @param config - News feed configuration
 * @param cachePath - Path to the cache bus file (for reading previous state)
 */
export function createNewsProducer(config: NewsFeedConfig, cachePath: string = DEFAULT_CACHE_PATH): FeedProducer {
  return async (): Promise<Record<string, unknown> | null> => {
    try {
      let stories: NewsStory[] | null;

      if (config.source === "rss" && config.rssUrl) {
        stories = await fetchRSSStories(config.rssUrl, config.maxStories);
      } else {
        stories = await fetchHNStories(config.maxStories);
      }

      if (!stories || stories.length === 0) return null;

      const now = Date.now();

      // Read previous rotation state from cache bus
      const previous = readKey<CacheEntry & { current_index?: number; last_rotation?: string }>(cachePath, "news");
      let currentIndex = previous?.current_index ?? 0;
      let lastRotation = previous?.last_rotation ?? new Date(now).toISOString();

      // Advance rotation if enough time has elapsed
      const lastRotationTime = Date.parse(lastRotation);
      const elapsedMinutes = (now - lastRotationTime) / 60_000;

      if (elapsedMinutes >= config.rotationMinutes) {
        currentIndex = (currentIndex + 1) % stories.length;
        lastRotation = new Date(now).toISOString();
      }

      // Ensure index is within bounds (stories list may have changed size)
      if (currentIndex >= stories.length) {
        currentIndex = 0;
      }

      const result: NewsData = {
        stories,
        current_index: currentIndex,
        current_story: stories[currentIndex],
        last_rotation: lastRotation,
      };

      return result as unknown as Record<string, unknown>;
    } catch {
      return null;
    }
  };
}
