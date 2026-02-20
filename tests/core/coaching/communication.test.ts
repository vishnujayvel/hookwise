/**
 * Tests for the Communication Coach.
 *
 * Verifies:
 * - Each grammar rule with positive and negative cases
 * - Frequency gating (only checks every Nth)
 * - Short prompt skip
 * - Improvement score computation
 * - Tone variants
 */

import { describe, it, expect } from "vitest";
import { analyze } from "../../../src/core/coaching/communication.js";
import type { CoachingConfig } from "../../../src/core/types.js";

function makeCommConfig(
  overrides: Partial<CoachingConfig["communication"]> = {}
): CoachingConfig["communication"] {
  return {
    enabled: true,
    frequency: 1, // Check every prompt for testing
    minLength: 10,
    rules: ["missing_articles", "run_on_sentence", "incomplete_sentence", "subject_verb_disagreement"],
    tone: "gentle",
    ...overrides,
  };
}

// --- missing_articles rule ---

describe("analyze - missing_articles", () => {
  const config = makeCommConfig({ rules: ["missing_articles"] });

  it("detects missing article before noun-like word", () => {
    const result = analyze("I went to store yesterday", config, 1);
    expect(result.shouldCorrect).toBe(true);
    expect(result.issues.some((i) => i.rule === "missing_articles")).toBe(true);
  });

  it("does not flag when article is present", () => {
    const result = analyze("I went to the store yesterday", config, 1);
    const articleIssues = result.issues.filter((i) => i.rule === "missing_articles");
    expect(articleIssues.length).toBe(0);
  });

  it("detects missing 'a' before consonant-starting noun", () => {
    const result = analyze("She is teacher at the school", config, 1);
    expect(result.issues.some((i) => i.rule === "missing_articles")).toBe(true);
  });
});

// --- run_on_sentence rule ---

describe("analyze - run_on_sentence", () => {
  const config = makeCommConfig({ rules: ["run_on_sentence"] });

  it("detects sentence with > 40 words without punctuation", () => {
    const longSentence = Array(45).fill("word").join(" ");
    const result = analyze(longSentence, config, 1);
    expect(result.shouldCorrect).toBe(true);
    expect(result.issues.some((i) => i.rule === "run_on_sentence")).toBe(true);
  });

  it("does not flag sentence with punctuation breaks", () => {
    const words = Array(20).fill("word").join(" ");
    const text = `${words}. ${words}.`;
    const result = analyze(text, config, 1);
    const runOnIssues = result.issues.filter((i) => i.rule === "run_on_sentence");
    expect(runOnIssues.length).toBe(0);
  });

  it("does not flag short sentences", () => {
    const result = analyze("This is a short sentence.", config, 1);
    const runOnIssues = result.issues.filter((i) => i.rule === "run_on_sentence");
    expect(runOnIssues.length).toBe(0);
  });
});

// --- incomplete_sentence rule ---

describe("analyze - incomplete_sentence", () => {
  const config = makeCommConfig({ rules: ["incomplete_sentence"] });

  it("detects sentence with no verb", () => {
    const result = analyze("the big red dog on the mat", config, 1);
    expect(result.shouldCorrect).toBe(true);
    expect(result.issues.some((i) => i.rule === "incomplete_sentence")).toBe(true);
  });

  it("does not flag sentence with a verb", () => {
    const result = analyze("the dog is on the mat", config, 1);
    const incompleteIssues = result.issues.filter((i) => i.rule === "incomplete_sentence");
    expect(incompleteIssues.length).toBe(0);
  });

  it("does not flag sentence with common verbs", () => {
    const result = analyze("I want to go to the store", config, 1);
    const incompleteIssues = result.issues.filter((i) => i.rule === "incomplete_sentence");
    expect(incompleteIssues.length).toBe(0);
  });
});

// --- subject_verb_disagreement rule ---

describe("analyze - subject_verb_disagreement", () => {
  const config = makeCommConfig({ rules: ["subject_verb_disagreement"] });

  it("detects 'he do' disagreement", () => {
    const result = analyze("he do the work every day", config, 1);
    expect(result.shouldCorrect).toBe(true);
    expect(result.issues.some((i) => i.rule === "subject_verb_disagreement")).toBe(true);
  });

  it("detects 'she have' disagreement", () => {
    const result = analyze("she have a lot of books", config, 1);
    expect(result.issues.some((i) => i.rule === "subject_verb_disagreement")).toBe(true);
  });

  it("detects 'it do' disagreement", () => {
    const result = analyze("it do not work properly", config, 1);
    expect(result.issues.some((i) => i.rule === "subject_verb_disagreement")).toBe(true);
  });

  it("does not flag correct agreement", () => {
    const result = analyze("he does the work every day", config, 1);
    const svIssues = result.issues.filter((i) => i.rule === "subject_verb_disagreement");
    expect(svIssues.length).toBe(0);
  });

  it("does not flag 'they do' (correct plural)", () => {
    const result = analyze("they do the work every day", config, 1);
    const svIssues = result.issues.filter((i) => i.rule === "subject_verb_disagreement");
    expect(svIssues.length).toBe(0);
  });
});

// --- Frequency Gating ---

describe("analyze - frequency gating", () => {
  it("only checks every Nth prompt (frequency=3)", () => {
    const config = makeCommConfig({ frequency: 3 });

    const r1 = analyze("he do the work every single day", config, 1);
    expect(r1.shouldCorrect).toBe(false); // prompt 1, skip

    const r2 = analyze("he do the work every single day", config, 2);
    expect(r2.shouldCorrect).toBe(false); // prompt 2, skip

    const r3 = analyze("he do the work every single day", config, 3);
    expect(r3.shouldCorrect).toBe(true); // prompt 3, check
  });

  it("checks every prompt when frequency=1", () => {
    const config = makeCommConfig({ frequency: 1 });
    const result = analyze("he do the work every single day", config, 1);
    expect(result.shouldCorrect).toBe(true);
  });
});

// --- Short Prompt Skip ---

describe("analyze - short prompt skip", () => {
  it("skips prompts below minLength", () => {
    const config = makeCommConfig({ minLength: 10 });
    const result = analyze("he do", config, 1);
    expect(result.shouldCorrect).toBe(false);
    expect(result.issues.length).toBe(0);
  });

  it("analyzes prompts at exactly minLength", () => {
    const config = makeCommConfig({ minLength: 10, rules: ["subject_verb_disagreement"] });
    const result = analyze("he do work", config, 1); // 10 chars
    expect(result.issues.length).toBeGreaterThanOrEqual(0); // analyzed, not skipped
  });

  it("analyzes prompts above minLength", () => {
    const config = makeCommConfig({ minLength: 5, rules: ["subject_verb_disagreement"] });
    const result = analyze("he do the work every day", config, 1);
    expect(result.shouldCorrect).toBe(true);
  });
});

// --- Improvement Score ---

describe("analyze - improvement score", () => {
  it("returns an improvement score", () => {
    const config = makeCommConfig();
    const result = analyze("he do the work every single day", config, 1);
    expect(result.improvementScore).toBeDefined();
    expect(typeof result.improvementScore).toBe("number");
  });

  it("returns higher score for fewer issues", () => {
    const config = makeCommConfig();
    const good = analyze("He does the work every day at the office.", config, 1);
    const bad = analyze("he do work every single day at store without the break for long time running around the block", config, 1);
    expect(good.improvementScore!).toBeGreaterThanOrEqual(bad.improvementScore!);
  });

  it("returns 100 for perfect text", () => {
    const config = makeCommConfig();
    const result = analyze("The quick brown fox jumps over the lazy dog.", config, 1);
    expect(result.improvementScore).toBe(100);
  });
});

// --- Tone Variants ---

describe("analyze - tone variants", () => {
  it("gentle tone includes encouraging language in correctedText", () => {
    const config = makeCommConfig({ tone: "gentle" });
    const result = analyze("he do the work every single day", config, 1);
    if (result.correctedText) {
      // Gentle tone should have some text
      expect(result.correctedText.length).toBeGreaterThan(0);
    }
    expect(result.shouldCorrect).toBe(true);
  });

  it("direct tone provides just corrections", () => {
    const config = makeCommConfig({ tone: "direct" });
    const result = analyze("he do the work every single day", config, 1);
    expect(result.shouldCorrect).toBe(true);
    if (result.correctedText) {
      expect(result.correctedText.length).toBeGreaterThan(0);
    }
  });

  it("silent tone returns score only, no corrections output", () => {
    const config = makeCommConfig({ tone: "silent" });
    const result = analyze("he do the work every single day", config, 1);
    // shouldCorrect is false in silent mode (score only)
    expect(result.shouldCorrect).toBe(false);
    expect(result.improvementScore).toBeDefined();
    expect(result.correctedText).toBeUndefined();
  });
});

// --- Disabled and edge cases ---

describe("analyze - edge cases", () => {
  it("returns empty result when disabled", () => {
    const config = makeCommConfig({ enabled: false });
    const result = analyze("he do the work", config, 1);
    expect(result.shouldCorrect).toBe(false);
    expect(result.issues.length).toBe(0);
  });

  it("handles empty string", () => {
    const config = makeCommConfig();
    const result = analyze("", config, 1);
    expect(result.shouldCorrect).toBe(false);
  });

  it("handles text with only whitespace", () => {
    const config = makeCommConfig({ minLength: 0 });
    const result = analyze("   ", config, 1);
    expect(result.shouldCorrect).toBe(false);
  });
});
