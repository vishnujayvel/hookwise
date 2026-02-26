/**
 * Tests for FeedRegistry: registration, lookup, filtering, and custom command producers.
 *
 * Covers Task 3.1:
 * - Register a feed and retrieve it by name
 * - Duplicate feed name rejection
 * - getAll() returns all registered feeds
 * - getEnabled() filters out disabled feeds
 * - Custom feed command producer: successful JSON, non-JSON output, command failure, timeout
 *
 * Requirements: FR-1.1, FR-1.2, FR-1.3, FR-1.4, FR-1.5
 */

import { describe, it, expect, vi, afterEach } from "vitest";
import { createFeedRegistry, createCommandProducer } from "../../../src/core/feeds/registry.js";
import type { FeedDefinition } from "../../../src/core/types.js";

// --- Helper: build a FeedDefinition with defaults ---

function makeFeed(overrides: Partial<FeedDefinition> & { name: string }): FeedDefinition {
  return {
    intervalSeconds: 60,
    producer: async () => ({ ok: true }),
    enabled: true,
    ...overrides,
  };
}

// --- Registry: register and retrieve ---

describe("createFeedRegistry", () => {
  describe("register and get", () => {
    it("registers a feed and retrieves it by name (FR-1.1)", () => {
      const registry = createFeedRegistry();
      const feed = makeFeed({ name: "pulse" });

      registry.register(feed);

      const retrieved = registry.get("pulse");
      expect(retrieved).toBeDefined();
      expect(retrieved!.name).toBe("pulse");
      expect(retrieved!.intervalSeconds).toBe(60);
      expect(retrieved!.enabled).toBe(true);
    });

    it("returns undefined for an unregistered feed name", () => {
      const registry = createFeedRegistry();
      expect(registry.get("nonexistent")).toBeUndefined();
    });

    it("stores the exact producer function reference", async () => {
      const registry = createFeedRegistry();
      const producer = async () => ({ temp: 72 });
      const feed = makeFeed({ name: "weather", producer });

      registry.register(feed);

      const retrieved = registry.get("weather");
      expect(retrieved!.producer).toBe(producer);
      const result = await retrieved!.producer();
      expect(result).toEqual({ temp: 72 });
    });

    it("registers multiple feeds with different names", () => {
      const registry = createFeedRegistry();
      registry.register(makeFeed({ name: "pulse" }));
      registry.register(makeFeed({ name: "project" }));
      registry.register(makeFeed({ name: "calendar" }));

      expect(registry.get("pulse")).toBeDefined();
      expect(registry.get("project")).toBeDefined();
      expect(registry.get("calendar")).toBeDefined();
    });
  });

  // --- Duplicate rejection ---

  describe("duplicate rejection", () => {
    it("throws when registering a duplicate feed name (FR-1.2)", () => {
      const registry = createFeedRegistry();
      registry.register(makeFeed({ name: "pulse" }));

      expect(() => {
        registry.register(makeFeed({ name: "pulse" }));
      }).toThrow('Feed "pulse" is already registered');
    });

    it("throws on duplicate even if other fields differ", () => {
      const registry = createFeedRegistry();
      registry.register(makeFeed({ name: "test", intervalSeconds: 30, enabled: true }));

      expect(() => {
        registry.register(makeFeed({ name: "test", intervalSeconds: 120, enabled: false }));
      }).toThrow('Feed "test" is already registered');
    });
  });

  // --- getAll ---

  describe("getAll", () => {
    it("returns empty array when no feeds are registered", () => {
      const registry = createFeedRegistry();
      expect(registry.getAll()).toEqual([]);
    });

    it("returns all registered feeds (FR-1.3)", () => {
      const registry = createFeedRegistry();
      registry.register(makeFeed({ name: "pulse", enabled: true }));
      registry.register(makeFeed({ name: "project", enabled: false }));
      registry.register(makeFeed({ name: "calendar", enabled: true }));

      const all = registry.getAll();
      expect(all).toHaveLength(3);
      const names = all.map((f) => f.name).sort();
      expect(names).toEqual(["calendar", "project", "pulse"]);
    });

    it("returns a new array each call (not the internal structure)", () => {
      const registry = createFeedRegistry();
      registry.register(makeFeed({ name: "pulse" }));

      const first = registry.getAll();
      const second = registry.getAll();
      expect(first).not.toBe(second);
      expect(first).toEqual(second);
    });
  });

  // --- getEnabled ---

  describe("getEnabled", () => {
    it("returns empty array when no feeds are registered", () => {
      const registry = createFeedRegistry();
      expect(registry.getEnabled()).toEqual([]);
    });

    it("returns only enabled feeds (FR-1.4)", () => {
      const registry = createFeedRegistry();
      registry.register(makeFeed({ name: "pulse", enabled: true }));
      registry.register(makeFeed({ name: "project", enabled: false }));
      registry.register(makeFeed({ name: "calendar", enabled: true }));
      registry.register(makeFeed({ name: "news", enabled: false }));

      const enabled = registry.getEnabled();
      expect(enabled).toHaveLength(2);
      const names = enabled.map((f) => f.name).sort();
      expect(names).toEqual(["calendar", "pulse"]);
    });

    it("returns empty when all feeds are disabled", () => {
      const registry = createFeedRegistry();
      registry.register(makeFeed({ name: "pulse", enabled: false }));
      registry.register(makeFeed({ name: "project", enabled: false }));

      expect(registry.getEnabled()).toEqual([]);
    });

    it("returns all when all feeds are enabled", () => {
      const registry = createFeedRegistry();
      registry.register(makeFeed({ name: "pulse", enabled: true }));
      registry.register(makeFeed({ name: "project", enabled: true }));

      expect(registry.getEnabled()).toHaveLength(2);
    });
  });

  // --- Registry isolation ---

  describe("isolation", () => {
    it("separate registries do not share feeds", () => {
      const registry1 = createFeedRegistry();
      const registry2 = createFeedRegistry();

      registry1.register(makeFeed({ name: "pulse" }));

      expect(registry1.get("pulse")).toBeDefined();
      expect(registry2.get("pulse")).toBeUndefined();
      expect(registry2.getAll()).toHaveLength(0);
    });
  });
});

// --- createCommandProducer ---

describe("createCommandProducer", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("returns parsed JSON from a successful command (FR-1.5)", async () => {
    const producer = createCommandProducer('echo \'{"temp":72,"unit":"F"}\'');
    const result = await producer();

    expect(result).toEqual({ temp: 72, unit: "F" });
  });

  it("returns null when command outputs non-JSON", async () => {
    const producer = createCommandProducer("echo 'not json at all'");
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when command outputs a JSON array (not an object)", async () => {
    const producer = createCommandProducer('echo \'[1, 2, 3]\'');
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when command outputs a JSON primitive", async () => {
    const producer = createCommandProducer("echo '42'");
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when command outputs JSON null", async () => {
    const producer = createCommandProducer("echo 'null'");
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when command fails (non-zero exit)", async () => {
    const producer = createCommandProducer("exit 1");
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when command does not exist", async () => {
    const producer = createCommandProducer("nonexistent_command_xyz_12345");
    const result = await producer();

    expect(result).toBeNull();
  });

  it("returns null when command times out", async () => {
    // sleep 10 with a 100ms timeout should trigger the timeout
    const producer = createCommandProducer("sleep 10", 100);
    const result = await producer();

    expect(result).toBeNull();
  }, 5000);

  it("handles nested JSON objects correctly", async () => {
    const producer = createCommandProducer(
      'echo \'{"data":{"nested":true},"count":5}\'',
    );
    const result = await producer();

    expect(result).toEqual({ data: { nested: true }, count: 5 });
  });

  it("uses default timeout from constants when not specified", async () => {
    // Just verify it does not throw and completes with a fast command
    const producer = createCommandProducer('echo \'{"ok":true}\'');
    const result = await producer();

    expect(result).toEqual({ ok: true });
  });

  it("returns null for empty stdout", async () => {
    const producer = createCommandProducer("echo -n ''");
    const result = await producer();

    expect(result).toBeNull();
  });
});
