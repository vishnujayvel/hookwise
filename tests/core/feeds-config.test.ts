/**
 * Tests for Feed Platform v1.1 configuration: types, defaults, and validation.
 *
 * Covers Tasks 1.1-1.3:
 * - Feed and daemon types added to HooksConfig
 * - Default config includes all feed and daemon fields
 * - Backward compatibility when feeds section is absent
 * - Validation of pulse thresholds, intervals, news source, custom feeds, daemon
 * - feeds and daemon recognized as valid top-level sections
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import {
  getDefaultConfig,
  validateConfig,
  loadConfig,
} from "../../src/core/config.js";

// --- Task 1.1 / 1.2: Default config includes feed and daemon fields ---

describe("feeds and daemon default config", () => {
  it("default config includes all feed and daemon fields", () => {
    const config = getDefaultConfig();

    // Feeds top-level exists
    expect(config.feeds).toBeDefined();
    expect(config.daemon).toBeDefined();

    // Pulse
    expect(config.feeds.pulse).toBeDefined();
    expect(config.feeds.pulse.enabled).toBe(true);
    expect(config.feeds.pulse.intervalSeconds).toBe(30);
    expect(config.feeds.pulse.thresholds).toEqual({
      green: 0,
      yellow: 30,
      orange: 60,
      red: 120,
      skull: 180,
    });

    // Project
    expect(config.feeds.project).toBeDefined();
    expect(config.feeds.project.enabled).toBe(true);
    expect(config.feeds.project.intervalSeconds).toBe(60);
    expect(config.feeds.project.showBranch).toBe(true);
    expect(config.feeds.project.showLastCommit).toBe(true);

    // Calendar
    expect(config.feeds.calendar).toBeDefined();
    expect(config.feeds.calendar.enabled).toBe(false);
    expect(config.feeds.calendar.intervalSeconds).toBe(300);
    expect(config.feeds.calendar.lookaheadMinutes).toBe(120);
    expect(config.feeds.calendar.calendars).toEqual(["primary"]);

    // News
    expect(config.feeds.news).toBeDefined();
    expect(config.feeds.news.enabled).toBe(false);
    expect(config.feeds.news.source).toBe("hackernews");
    expect(config.feeds.news.rssUrl).toBeNull();
    expect(config.feeds.news.intervalSeconds).toBe(1800);
    expect(config.feeds.news.maxStories).toBe(5);
    expect(config.feeds.news.rotationMinutes).toBe(30);

    // Custom
    expect(config.feeds.custom).toEqual([]);

    // Daemon
    expect(config.daemon.autoStart).toBe(true);
    expect(config.daemon.inactivityTimeoutMinutes).toBe(120);
    expect(typeof config.daemon.logFile).toBe("string");
    expect(config.daemon.logFile).toContain("daemon.log");
  });

  it("default config has pulse and project enabled, calendar and news disabled", () => {
    const config = getDefaultConfig();
    expect(config.feeds.pulse.enabled).toBe(true);
    expect(config.feeds.project.enabled).toBe(true);
    expect(config.feeds.calendar.enabled).toBe(false);
    expect(config.feeds.news.enabled).toBe(false);
  });

  it("backward compatibility: config without feeds section loads with defaults via loadConfig", () => {
    // Create a real v1.0-style YAML file with no feeds or daemon sections
    const tempDir = mkdtempSync(join(tmpdir(), "hookwise-compat-"));
    try {
      writeFileSync(
        join(tempDir, "hookwise.yaml"),
        "version: 1\nguards: []\n",
        "utf-8"
      );
      const config = loadConfig(tempDir);

      // feeds and daemon should be filled from defaults via deep-merge
      expect(config.feeds.pulse.enabled).toBe(true);
      expect(config.feeds.pulse.intervalSeconds).toBe(30);
      expect(config.feeds.project.enabled).toBe(true);
      expect(config.feeds.calendar.enabled).toBe(false);
      expect(config.feeds.news.enabled).toBe(false);
      expect(config.feeds.custom).toEqual([]);
      expect(config.daemon.autoStart).toBe(true);
      expect(config.daemon.inactivityTimeoutMinutes).toBe(120);
    } finally {
      rmSync(tempDir, { recursive: true, force: true });
    }
  });
});

// --- Task 1.3: Validation ---

describe("feeds and daemon validation", () => {
  it("ascending pulse thresholds pass validation", () => {
    const result = validateConfig({
      feeds: {
        pulse: {
          thresholds: { green: 0, yellow: 30, orange: 60, red: 120, skull: 180 },
        },
      },
    });
    expect(result.valid).toBe(true);
    expect(result.errors).toHaveLength(0);
  });

  it("non-ascending pulse thresholds fail validation (green > yellow)", () => {
    const result = validateConfig({
      feeds: {
        pulse: {
          thresholds: { green: 50, yellow: 30, orange: 60, red: 120, skull: 180 },
        },
      },
    });
    expect(result.valid).toBe(false);
    const thresholdError = result.errors.find((e) => e.path === "feeds.pulse.thresholds");
    expect(thresholdError).toBeDefined();
    expect(thresholdError!.message).toContain("ascending");
  });

  it("zero or negative feed interval fails validation", () => {
    const result = validateConfig({
      feeds: {
        pulse: { interval_seconds: 0 },
      },
    });
    expect(result.valid).toBe(false);
    const intervalError = result.errors.find((e) =>
      e.path.includes("interval_seconds")
    );
    expect(intervalError).toBeDefined();
    expect(intervalError!.message).toContain("positive");

    // Negative interval
    const result2 = validateConfig({
      feeds: {
        project: { interval_seconds: -5 },
      },
    });
    expect(result2.valid).toBe(false);
    const intervalError2 = result2.errors.find((e) =>
      e.path.includes("interval_seconds")
    );
    expect(intervalError2).toBeDefined();
  });

  it("invalid news source fails validation", () => {
    const result = validateConfig({
      feeds: {
        news: { source: "twitter" },
      },
    });
    expect(result.valid).toBe(false);
    const sourceError = result.errors.find((e) => e.path === "feeds.news.source");
    expect(sourceError).toBeDefined();
    expect(sourceError!.message).toContain("Invalid news source");
  });

  it("source 'rss' without rss_url fails validation", () => {
    const result = validateConfig({
      feeds: {
        news: { source: "rss" },
      },
    });
    expect(result.valid).toBe(false);
    const rssError = result.errors.find((e) => e.path === "feeds.news.rss_url");
    expect(rssError).toBeDefined();
    expect(rssError!.message).toContain("rss_url is required");
  });

  it("source 'rss' with valid rss_url passes validation", () => {
    const result = validateConfig({
      feeds: {
        news: { source: "rss", rss_url: "https://example.com/feed.xml" },
      },
    });
    expect(result.valid).toBe(true);
  });

  it("custom feed missing name/command fails validation", () => {
    const result = validateConfig({
      feeds: {
        custom: [
          { interval_seconds: 60 }, // missing name and command
        ],
      },
    });
    expect(result.valid).toBe(false);
    const nameError = result.errors.find((e) =>
      e.path.includes("custom[0].name")
    );
    const commandError = result.errors.find((e) =>
      e.path.includes("custom[0].command")
    );
    expect(nameError).toBeDefined();
    expect(commandError).toBeDefined();
  });

  it("valid custom feed passes validation", () => {
    const result = validateConfig({
      feeds: {
        custom: [
          { name: "weather", command: "curl wttr.in", interval_seconds: 600 },
        ],
      },
    });
    expect(result.valid).toBe(true);
  });

  it("negative daemon inactivityTimeoutMinutes fails validation", () => {
    const result = validateConfig({
      daemon: { inactivity_timeout_minutes: -10 },
    });
    expect(result.valid).toBe(false);
    const timeoutError = result.errors.find((e) =>
      e.path.includes("inactivity_timeout_minutes")
    );
    expect(timeoutError).toBeDefined();
    expect(timeoutError!.message).toContain("positive");
  });

  it("feeds and daemon are recognized as valid top-level sections", () => {
    // feeds and daemon should NOT trigger "Unknown config section" errors
    const result = validateConfig({
      version: 1,
      feeds: { pulse: { enabled: true } },
      daemon: { auto_start: true },
    });
    expect(result.valid).toBe(true);
    const unknownErrors = result.errors.filter((e) =>
      e.message.includes("Unknown config section")
    );
    expect(unknownErrors).toHaveLength(0);
  });
});
