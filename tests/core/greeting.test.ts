/**
 * Tests for session greeting handler.
 *
 * Verifies:
 * - Weighted category selection (higher weight = higher probability)
 * - Custom quotes file loading (JSON array or {quotes: [...]})
 * - Empty/missing config returns null
 * - Malformed quotesFile returns null
 * - Disabled config returns null
 */

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { selectQuote } from "../../src/core/greeting.js";
import type { GreetingConfig } from "../../src/core/types.js";

describe("selectQuote", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = mkdtempSync(join(tmpdir(), "hookwise-greeting-"));
  });

  afterEach(() => {
    rmSync(tempDir, { recursive: true, force: true });
  });

  it("returns null when disabled", () => {
    const config: GreetingConfig = { enabled: false };
    expect(selectQuote(config)).toBeNull();
  });

  it("returns null when no categories and no quotesFile", () => {
    const config: GreetingConfig = { enabled: true };
    expect(selectQuote(config)).toBeNull();
  });

  it("returns null when categories is empty object", () => {
    const config: GreetingConfig = { enabled: true, categories: {} };
    expect(selectQuote(config)).toBeNull();
  });

  it("selects a quote from a single category", () => {
    const config: GreetingConfig = {
      enabled: true,
      categories: {
        motivation: {
          weight: 1,
          quotes: [{ text: "Just do it", author: "Nike" }],
        },
      },
    };
    const quote = selectQuote(config);
    expect(quote).not.toBeNull();
    expect(quote!.text).toBe("Just do it");
    expect(quote!.author).toBe("Nike");
  });

  it("weighted selection: high weight category dominates", () => {
    const config: GreetingConfig = {
      enabled: true,
      categories: {
        highWeight: {
          weight: 1000,
          quotes: [{ text: "HIGH" }],
        },
        lowWeight: {
          weight: 1,
          quotes: [{ text: "LOW" }],
        },
      },
    };

    // Run multiple times and count — high weight should dominate
    const counts = { HIGH: 0, LOW: 0 };
    for (let i = 0; i < 200; i++) {
      const quote = selectQuote(config);
      if (quote?.text === "HIGH") counts.HIGH++;
      else if (quote?.text === "LOW") counts.LOW++;
    }

    // HIGH should win the vast majority of the time
    expect(counts.HIGH).toBeGreaterThan(counts.LOW);
    expect(counts.HIGH).toBeGreaterThan(150);
  });

  it("returns null for categories with zero weight", () => {
    const config: GreetingConfig = {
      enabled: true,
      categories: {
        empty: {
          weight: 0,
          quotes: [{ text: "Never shown" }],
        },
      },
    };
    expect(selectQuote(config)).toBeNull();
  });

  it("returns null for categories with no quotes", () => {
    const config: GreetingConfig = {
      enabled: true,
      categories: {
        noQuotes: {
          weight: 10,
          quotes: [],
        },
      },
    };
    expect(selectQuote(config)).toBeNull();
  });

  describe("custom quotes file", () => {
    it("loads quotes from JSON array file", () => {
      const quotesPath = join(tempDir, "quotes.json");
      const quotes = [
        { text: "File quote 1", author: "Author 1" },
        { text: "File quote 2" },
      ];
      writeFileSync(quotesPath, JSON.stringify(quotes));

      const config: GreetingConfig = {
        enabled: true,
        quotesFile: quotesPath,
      };
      const quote = selectQuote(config);
      expect(quote).not.toBeNull();
      expect(["File quote 1", "File quote 2"]).toContain(quote!.text);
    });

    it("loads quotes from JSON object with quotes key", () => {
      const quotesPath = join(tempDir, "quotes-obj.json");
      writeFileSync(
        quotesPath,
        JSON.stringify({ quotes: [{ text: "Object quote" }] })
      );

      const config: GreetingConfig = {
        enabled: true,
        quotesFile: quotesPath,
      };
      const quote = selectQuote(config);
      expect(quote).not.toBeNull();
      expect(quote!.text).toBe("Object quote");
    });

    it("returns null for missing quotes file", () => {
      const config: GreetingConfig = {
        enabled: true,
        quotesFile: join(tempDir, "nonexistent.json"),
      };
      expect(selectQuote(config)).toBeNull();
    });

    it("returns null for malformed quotes file", () => {
      const quotesPath = join(tempDir, "bad.json");
      writeFileSync(quotesPath, "not valid json {{{");

      const config: GreetingConfig = {
        enabled: true,
        quotesFile: quotesPath,
      };
      expect(selectQuote(config)).toBeNull();
    });

    it("returns null for empty quotes file", () => {
      const quotesPath = join(tempDir, "empty.json");
      writeFileSync(quotesPath, "[]");

      const config: GreetingConfig = {
        enabled: true,
        quotesFile: quotesPath,
      };
      // Empty array in file, no categories = null
      expect(selectQuote(config)).toBeNull();
    });

    it("quotesFile takes priority over categories", () => {
      const quotesPath = join(tempDir, "priority.json");
      writeFileSync(
        quotesPath,
        JSON.stringify([{ text: "File wins" }])
      );

      const config: GreetingConfig = {
        enabled: true,
        quotesFile: quotesPath,
        categories: {
          fallback: {
            weight: 10,
            quotes: [{ text: "Category quote" }],
          },
        },
      };
      const quote = selectQuote(config);
      expect(quote).not.toBeNull();
      expect(quote!.text).toBe("File wins");
    });
  });
});
