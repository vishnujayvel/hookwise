/**
 * Calendar feed producer: fetches upcoming events from Google Calendar
 * via a Python helper script.
 *
 * Implementation:
 *   1. Checks if credentials file exists — returns null if not
 *   2. Checks if Python 3 is available — returns null if not
 *   3. Spawns: python3 scripts/calendar-feed.py --lookahead N --credentials PATH
 *   4. Parses JSON stdout into CalendarEvent[]
 *   5. Sanitizes event titles by stripping HTML tags
 *   6. Finds the next upcoming event (first non-current event in the future)
 *
 * Returns null on any failure (missing credentials, script error, parse error).
 *
 * Requirements: FR-6.1, FR-6.2, FR-6.3, FR-6.5, FR-6.6, FR-6.7, NFR-3
 */

import type { CalendarFeedConfig, FeedProducer } from "../../types.js";
import { existsSync } from "node:fs";
import { execSync, execFileSync } from "node:child_process";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

export interface CalendarEvent {
  title: string;
  start: string;    // ISO 8601
  end: string;      // ISO 8601
  is_current: boolean;
}

export interface CalendarData {
  events: CalendarEvent[];
  next_event: CalendarEvent | null;
}

/**
 * Strip HTML tags from a string.
 * Used to sanitize event titles that may contain HTML markup.
 */
export function stripHtmlTags(text: string): string {
  return text.replace(/<[^>]*>/g, "");
}

/**
 * Find the script path relative to this module.
 * The script lives at the project root: scripts/calendar-feed.py
 */
function getScriptPath(): string {
  const thisFile = fileURLToPath(import.meta.url);
  // Navigate from src/core/feeds/producers/ up to project root
  const projectRoot = resolve(dirname(thisFile), "..", "..", "..", "..");
  return resolve(projectRoot, "scripts", "calendar-feed.py");
}

/**
 * Check if Python 3 is available on the system.
 */
function isPython3Available(): boolean {
  try {
    execSync("python3 --version", {
      encoding: "utf-8",
      stdio: ["pipe", "pipe", "pipe"],
      timeout: 5000,
    });
    return true;
  } catch {
    return false;
  }
}

/**
 * Create a FeedProducer for the calendar feed.
 *
 * @param credentialsPath - Path to the Google Calendar OAuth credentials file
 * @param config          - Calendar feed configuration
 */
export function createCalendarProducer(
  credentialsPath: string,
  config: CalendarFeedConfig,
): FeedProducer {
  return async (): Promise<Record<string, unknown> | null> => {
    try {
      // FR-6.5: Return null when credentials file is missing
      if (!existsSync(credentialsPath)) return null;

      // FR-6.6: Return null when Python 3 is not available
      if (!isPython3Available()) return null;

      const scriptPath = getScriptPath();
      const lookahead = config.lookaheadMinutes;

      // FR-6.1: Spawn the Python script (execFileSync avoids shell injection via credentialsPath)
      const stdout = execFileSync(
        "python3",
        [scriptPath, "--lookahead", String(lookahead), "--credentials", credentialsPath],
        {
          encoding: "utf-8",
          stdio: ["pipe", "pipe", "pipe"],
          timeout: 30_000,
        },
      );

      // FR-6.2: Parse JSON output
      const parsed = JSON.parse(stdout) as { events: CalendarEvent[] };
      const events: CalendarEvent[] = parsed.events.map((event) => ({
        ...event,
        // NFR-3: Sanitize event titles by stripping HTML tags
        title: stripHtmlTags(event.title),
      }));

      // FR-6.3: Find the next upcoming event
      const now = Date.now();
      const nextEvent = events.find(
        (e) => !e.is_current && Date.parse(e.start) > now,
      ) ?? null;

      const result: CalendarData = {
        events,
        next_event: nextEvent,
      };

      return result as unknown as Record<string, unknown>;
    } catch {
      // FR-6.7: Return null on any failure
      return null;
    }
  };
}
