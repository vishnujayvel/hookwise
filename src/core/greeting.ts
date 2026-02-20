/**
 * Session greeting handler for hookwise v1.0
 *
 * Provides weighted random quote selection from configured categories
 * or a custom quotes file. Fail-open: returns null on any error.
 */

import { existsSync, readFileSync } from "node:fs";
import type { GreetingConfig, QuoteEntry } from "./types.js";

/**
 * Select a random quote using weighted category selection.
 *
 * Algorithm:
 * 1. Collect all categories with their weights
 * 2. Weighted random pick: category weight determines probability
 * 3. Uniform random pick within the selected category
 *
 * @param config - Greeting configuration with categories or quotesFile
 * @returns A randomly selected quote, or null if none available
 */
export function selectQuote(config: GreetingConfig): QuoteEntry | null {
  try {
    if (!config.enabled) return null;

    // Try loading from quotesFile first
    if (config.quotesFile) {
      const fileQuotes = loadQuotesFile(config.quotesFile);
      if (fileQuotes && fileQuotes.length > 0) {
        return fileQuotes[Math.floor(Math.random() * fileQuotes.length)];
      }
    }

    // Fall back to configured categories
    if (!config.categories || Object.keys(config.categories).length === 0) {
      return null;
    }

    return weightedSelect(config.categories);
  } catch {
    // Fail-open: return null on any error
    return null;
  }
}

/**
 * Load quotes from a JSON file.
 *
 * Expected format: QuoteEntry[] or { quotes: QuoteEntry[] }
 * Returns null on any error (missing file, malformed JSON, etc.)
 */
function loadQuotesFile(filePath: string): QuoteEntry[] | null {
  try {
    if (!existsSync(filePath)) return null;
    const content = readFileSync(filePath, "utf-8");
    const parsed = JSON.parse(content);

    if (Array.isArray(parsed)) {
      return parsed as QuoteEntry[];
    }
    if (parsed && typeof parsed === "object" && Array.isArray(parsed.quotes)) {
      return parsed.quotes as QuoteEntry[];
    }
    return null;
  } catch {
    return null;
  }
}

/**
 * Weighted random selection across categories.
 *
 * Each category has a weight that determines the probability of selecting
 * a quote from that category. Within a category, quotes are selected
 * uniformly at random.
 */
function weightedSelect(
  categories: Record<string, { weight: number; quotes: QuoteEntry[] }>
): QuoteEntry | null {
  const entries = Object.values(categories).filter(
    (cat) => cat.quotes && cat.quotes.length > 0 && cat.weight > 0
  );

  if (entries.length === 0) return null;

  const totalWeight = entries.reduce((sum, cat) => sum + cat.weight, 0);
  if (totalWeight <= 0) return null;

  let random = Math.random() * totalWeight;

  for (const cat of entries) {
    random -= cat.weight;
    if (random <= 0) {
      return cat.quotes[Math.floor(Math.random() * cat.quotes.length)];
    }
  }

  // Fallback: return from last category (should not reach here)
  const lastCat = entries[entries.length - 1];
  return lastCat.quotes[Math.floor(Math.random() * lastCat.quotes.length)];
}
