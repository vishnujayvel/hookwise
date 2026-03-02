/**
 * Tests for the calendar feed producer.
 *
 * Covers Task 4.4:
 * - Normal event list: parses JSON output from Python script
 * - No events: returns empty events array with null next_event
 * - No credentials: returns null when credentials file is missing
 * - Script failure: returns null on non-zero exit or parse error
 * - HTML title sanitization: strips HTML tags from event titles
 * - Next event detection: finds first future non-current event
 *
 * Mocks: node:child_process (execSync, execFileSync), node:fs (existsSync)
 *
 * Requirements: FR-6.1, FR-6.2, FR-6.3, FR-6.5, FR-6.6, FR-6.7, NFR-3
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import type { CalendarFeedConfig } from "../../../../src/core/types.js";
import type { CalendarData } from "../../../../src/core/feeds/producers/calendar.js";

// Mock child_process and fs before importing the module under test
vi.mock("node:child_process", () => ({
  execSync: vi.fn(),
  execFileSync: vi.fn(),
}));

vi.mock("node:fs", () => ({
  existsSync: vi.fn(),
}));

import { execSync, execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { createCalendarProducer, stripHtmlTags } from "../../../../src/core/feeds/producers/calendar.js";

const mockExecSync = vi.mocked(execSync);
const mockExecFileSync = vi.mocked(execFileSync);
const mockExistsSync = vi.mocked(existsSync);

// --- Helpers ---

const CREDENTIALS_PATH = "/home/user/.config/hookwise/calendar-credentials.json";

function makeConfig(overrides?: Partial<CalendarFeedConfig>): CalendarFeedConfig {
  return {
    enabled: true,
    intervalSeconds: 300,
    lookaheadMinutes: 60,
    calendars: ["primary"],
    ...overrides,
  };
}

/**
 * Sample event data for mocking script output.
 */
const SAMPLE_EVENTS = {
  events: [
    {
      title: "Team standup",
      start: "2026-02-22T10:00:00Z",
      end: "2026-02-22T10:15:00Z",
      is_current: true,
    },
    {
      title: "Code review",
      start: "2026-02-22T11:00:00Z",
      end: "2026-02-22T11:30:00Z",
      is_current: false,
    },
    {
      title: "Lunch",
      start: "2026-02-22T12:00:00Z",
      end: "2026-02-22T13:00:00Z",
      is_current: false,
    },
  ],
};

/**
 * Set up mocks for a successful script execution.
 * By default: credentials exist, python3 available, script returns SAMPLE_EVENTS.
 */
function mockSuccess(events = SAMPLE_EVENTS) {
  mockExistsSync.mockReturnValue(true);
  // execSync is used for python3 --version check
  mockExecSync.mockReturnValue("Python 3.12.0\n");
  // execFileSync is used for the calendar script invocation
  mockExecFileSync.mockReturnValue(JSON.stringify(events));
}

// --- stripHtmlTags unit tests ---

describe("stripHtmlTags", () => {
  it("removes simple HTML tags from text (NFR-3)", () => {
    expect(stripHtmlTags("<b>Bold meeting</b>")).toBe("Bold meeting");
  });

  it("removes multiple different tags", () => {
    expect(stripHtmlTags("<h1>Title</h1><p>Paragraph</p>")).toBe("TitleParagraph");
  });

  it("handles self-closing tags", () => {
    expect(stripHtmlTags("Before<br/>After")).toBe("BeforeAfter");
  });

  it("handles tags with attributes", () => {
    expect(stripHtmlTags('<a href="https://example.com">Link</a>')).toBe("Link");
  });

  it("returns plain text unchanged", () => {
    expect(stripHtmlTags("No HTML here")).toBe("No HTML here");
  });

  it("handles empty string", () => {
    expect(stripHtmlTags("")).toBe("");
  });

  it("handles nested tags", () => {
    expect(stripHtmlTags("<div><span>Nested</span></div>")).toBe("Nested");
  });
});

// --- createCalendarProducer tests ---

describe("createCalendarProducer", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // Set "now" to 2026-02-22T10:05:00Z so we can test current/future events
    vi.setSystemTime(new Date("2026-02-22T10:05:00Z"));
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  // --- Normal event list ---

  it("returns events from the Python script output (FR-6.1, FR-6.2)", async () => {
    mockSuccess();
    const producer = createCalendarProducer(CREDENTIALS_PATH, makeConfig());
    const result = (await producer()) as CalendarData | null;

    expect(result).not.toBeNull();
    expect(result!.events).toHaveLength(3);
    expect(result!.events[0].title).toBe("Team standup");
    expect(result!.events[1].title).toBe("Code review");
    expect(result!.events[2].title).toBe("Lunch");
  });

  it("identifies the next upcoming event (FR-6.3)", async () => {
    mockSuccess();
    const producer = createCalendarProducer(CREDENTIALS_PATH, makeConfig());
    const result = (await producer()) as CalendarData | null;

    expect(result).not.toBeNull();
    // "Team standup" is current, so next_event should be "Code review"
    expect(result!.next_event).not.toBeNull();
    expect(result!.next_event!.title).toBe("Code review");
    expect(result!.next_event!.is_current).toBe(false);
  });

  it("returns null for next_event when all events are current (FR-6.3)", async () => {
    const allCurrent = {
      events: [
        {
          title: "All-day event",
          start: "2026-02-22T00:00:00Z",
          end: "2026-02-23T00:00:00Z",
          is_current: true,
        },
      ],
    };
    mockSuccess(allCurrent);
    const producer = createCalendarProducer(CREDENTIALS_PATH, makeConfig());
    const result = (await producer()) as CalendarData | null;

    expect(result).not.toBeNull();
    expect(result!.next_event).toBeNull();
  });

  it("returns null for next_event when all future events are current", async () => {
    const data = {
      events: [
        { title: "Meeting A", start: "2026-02-22T11:00:00Z", end: "2026-02-22T12:00:00Z", is_current: true },
        { title: "Meeting B", start: "2026-02-22T12:00:00Z", end: "2026-02-22T13:00:00Z", is_current: true },
      ],
    };
    mockSuccess(data);
    const producer = createCalendarProducer(CREDENTIALS_PATH, makeConfig());
    const result = (await producer()) as CalendarData | null;

    expect(result).not.toBeNull();
    expect(result!.next_event).toBeNull();
  });

  // --- No events ---

  it("returns empty events and null next_event when no events (FR-6.2)", async () => {
    mockSuccess({ events: [] });
    const producer = createCalendarProducer(CREDENTIALS_PATH, makeConfig());
    const result = (await producer()) as CalendarData | null;

    expect(result).not.toBeNull();
    expect(result!.events).toHaveLength(0);
    expect(result!.next_event).toBeNull();
  });

  // --- No credentials ---

  it("returns null when credentials file does not exist (FR-6.5)", async () => {
    mockExistsSync.mockReturnValue(false);
    const producer = createCalendarProducer(CREDENTIALS_PATH, makeConfig());
    const result = await producer();

    expect(result).toBeNull();
    // Should not attempt to check python or spawn the script
    expect(mockExecSync).not.toHaveBeenCalled();
    expect(mockExecFileSync).not.toHaveBeenCalled();
  });

  // --- Python not available ---

  it("returns null when Python 3 is not available (FR-6.6)", async () => {
    mockExistsSync.mockReturnValue(true);
    mockExecSync.mockImplementation(() => {
      throw new Error("python3: command not found");
    });

    const producer = createCalendarProducer(CREDENTIALS_PATH, makeConfig());
    const result = await producer();

    expect(result).toBeNull();
  });

  // --- Script failure ---

  it("returns null when the Python script exits with non-zero (FR-6.7)", async () => {
    mockExistsSync.mockReturnValue(true);
    mockExecSync.mockReturnValue("Python 3.12.0\n");
    mockExecFileSync.mockImplementation(() => {
      throw new Error("Command failed with exit code 1");
    });

    const producer = createCalendarProducer(CREDENTIALS_PATH, makeConfig());
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when script output is not valid JSON (FR-6.7)", async () => {
    mockExistsSync.mockReturnValue(true);
    mockExecSync.mockReturnValue("Python 3.12.0\n");
    mockExecFileSync.mockReturnValue("not valid json {{{");

    const producer = createCalendarProducer(CREDENTIALS_PATH, makeConfig());
    const result = await producer();

    expect(result).toBeNull();
  });

  // --- HTML sanitization ---

  it("strips HTML tags from event titles (NFR-3)", async () => {
    const htmlEvents = {
      events: [
        {
          title: "<b>Important</b> meeting with <em>team</em>",
          start: "2026-02-22T11:00:00Z",
          end: "2026-02-22T12:00:00Z",
          is_current: false,
        },
        {
          title: 'Sprint <a href="#">review</a>',
          start: "2026-02-22T13:00:00Z",
          end: "2026-02-22T14:00:00Z",
          is_current: false,
        },
      ],
    };
    mockSuccess(htmlEvents);
    const producer = createCalendarProducer(CREDENTIALS_PATH, makeConfig());
    const result = (await producer()) as CalendarData | null;

    expect(result).not.toBeNull();
    expect(result!.events[0].title).toBe("Important meeting with team");
    expect(result!.events[1].title).toBe("Sprint review");
  });

  it("sanitized titles are also used in next_event (NFR-3)", async () => {
    const htmlEvents = {
      events: [
        {
          title: "<b>Next Event</b>",
          start: "2026-02-22T11:00:00Z",
          end: "2026-02-22T12:00:00Z",
          is_current: false,
        },
      ],
    };
    mockSuccess(htmlEvents);
    const producer = createCalendarProducer(CREDENTIALS_PATH, makeConfig());
    const result = (await producer()) as CalendarData | null;

    expect(result).not.toBeNull();
    expect(result!.next_event).not.toBeNull();
    expect(result!.next_event!.title).toBe("Next Event");
  });

  // --- Config passthrough ---

  it("passes lookaheadMinutes to the script command (FR-6.1)", async () => {
    mockSuccess();
    const config = makeConfig({ lookaheadMinutes: 120 });
    const producer = createCalendarProducer(CREDENTIALS_PATH, config);
    await producer();

    // execFileSync receives args as array: ["python3", [scriptPath, "--lookahead", "120", ...]]
    expect(mockExecFileSync).toHaveBeenCalled();
    const args = mockExecFileSync.mock.calls[0][1] as string[];
    expect(args).toContain("--lookahead");
    expect(args).toContain("120");
  });

  it("passes credentials path to the script command (FR-6.1)", async () => {
    mockSuccess();
    const producer = createCalendarProducer(CREDENTIALS_PATH, makeConfig());
    await producer();

    expect(mockExecFileSync).toHaveBeenCalled();
    const args = mockExecFileSync.mock.calls[0][1] as string[];
    expect(args).toContain("--credentials");
    expect(args).toContain(CREDENTIALS_PATH);
  });

  // --- Edge: events in the past ---

  it("skips past events when finding next_event (FR-6.3)", async () => {
    const pastAndFuture = {
      events: [
        {
          title: "Past meeting",
          start: "2026-02-22T08:00:00Z",
          end: "2026-02-22T09:00:00Z",
          is_current: false,
        },
        {
          title: "Future meeting",
          start: "2026-02-22T14:00:00Z",
          end: "2026-02-22T15:00:00Z",
          is_current: false,
        },
      ],
    };
    mockSuccess(pastAndFuture);
    const producer = createCalendarProducer(CREDENTIALS_PATH, makeConfig());
    const result = (await producer()) as CalendarData | null;

    expect(result).not.toBeNull();
    // "Past meeting" start is before now, so next_event should be "Future meeting"
    expect(result!.next_event).not.toBeNull();
    expect(result!.next_event!.title).toBe("Future meeting");
  });
});
