import { describe, it, expect } from "vitest";
import { getDefaultConfig } from "../../src/core/config.js";

describe("insights feed configuration", () => {
  it("default config has insights feed enabled", () => {
    const config = getDefaultConfig();
    expect(config.feeds.insights).toBeDefined();
    expect(config.feeds.insights.enabled).toBe(true);
  });

  it("default config has correct interval (120 seconds)", () => {
    const config = getDefaultConfig();
    expect(config.feeds.insights.intervalSeconds).toBe(120);
  });

  it("default config has correct staleness window (30 days)", () => {
    const config = getDefaultConfig();
    expect(config.feeds.insights.stalenessDays).toBe(30);
  });

  it("default config has correct usage data path", () => {
    const config = getDefaultConfig();
    expect(config.feeds.insights.usageDataPath).toBe("~/.claude/usage-data");
  });
});
