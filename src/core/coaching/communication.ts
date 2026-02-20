/**
 * Communication Coach for hookwise coaching system.
 *
 * Rules-based grammar analysis with NO LLM calls.
 * Checks for common grammar issues and provides gentle corrections.
 *
 * All functions are synchronous and fail-open.
 */

import type {
  CoachingConfig,
  GrammarResult,
  GrammarIssue,
} from "../types.js";

/**
 * Common verbs for incomplete sentence detection.
 * This is intentionally a basic list for pattern matching.
 */
const COMMON_VERBS = new Set([
  "is", "are", "was", "were", "be", "been", "being",
  "have", "has", "had", "having",
  "do", "does", "did", "doing",
  "will", "would", "shall", "should", "may", "might", "can", "could", "must",
  "go", "goes", "went", "gone", "going",
  "get", "gets", "got", "getting",
  "make", "makes", "made", "making",
  "know", "knows", "knew", "known", "knowing",
  "think", "thinks", "thought", "thinking",
  "take", "takes", "took", "taken", "taking",
  "see", "sees", "saw", "seen", "seeing",
  "come", "comes", "came", "coming",
  "want", "wants", "wanted", "wanting",
  "use", "uses", "used", "using",
  "find", "finds", "found", "finding",
  "give", "gives", "gave", "given", "giving",
  "tell", "tells", "told", "telling",
  "work", "works", "worked", "working",
  "call", "calls", "called", "calling",
  "try", "tries", "tried", "trying",
  "ask", "asks", "asked", "asking",
  "need", "needs", "needed", "needing",
  "feel", "feels", "felt", "feeling",
  "become", "becomes", "became", "becoming",
  "leave", "leaves", "left", "leaving",
  "put", "puts", "putting",
  "mean", "means", "meant", "meaning",
  "keep", "keeps", "kept", "keeping",
  "let", "lets", "letting",
  "begin", "begins", "began", "begun", "beginning",
  "show", "shows", "showed", "shown", "showing",
  "hear", "hears", "heard", "hearing",
  "play", "plays", "played", "playing",
  "run", "runs", "ran", "running",
  "move", "moves", "moved", "moving",
  "live", "lives", "lived", "living",
  "believe", "believes", "believed", "believing",
  "bring", "brings", "brought", "bringing",
  "happen", "happens", "happened", "happening",
  "write", "writes", "wrote", "written", "writing",
  "provide", "provides", "provided", "providing",
  "sit", "sits", "sat", "sitting",
  "stand", "stands", "stood", "standing",
  "lose", "loses", "lost", "losing",
  "pay", "pays", "paid", "paying",
  "meet", "meets", "met", "meeting",
  "include", "includes", "included", "including",
  "continue", "continues", "continued", "continuing",
  "set", "sets", "setting",
  "learn", "learns", "learned", "learning",
  "change", "changes", "changed", "changing",
  "lead", "leads", "led", "leading",
  "understand", "understands", "understood", "understanding",
  "watch", "watches", "watched", "watching",
  "follow", "follows", "followed", "following",
  "stop", "stops", "stopped", "stopping",
  "create", "creates", "created", "creating",
  "speak", "speaks", "spoke", "spoken", "speaking",
  "read", "reads", "reading",
  "spend", "spends", "spent", "spending",
  "grow", "grows", "grew", "grown", "growing",
  "open", "opens", "opened", "opening",
  "walk", "walks", "walked", "walking",
  "win", "wins", "won", "winning",
  "teach", "teaches", "taught", "teaching",
  "offer", "offers", "offered", "offering",
  "remember", "remembers", "remembered", "remembering",
  "consider", "considers", "considered", "considering",
  "appear", "appears", "appeared", "appearing",
  "buy", "buys", "bought", "buying",
  "serve", "serves", "served", "serving",
  "die", "dies", "died", "dying",
  "send", "sends", "sent", "sending",
  "build", "builds", "built", "building",
  "stay", "stays", "stayed", "staying",
  "fall", "falls", "fell", "fallen", "falling",
  "cut", "cuts", "cutting",
  "reach", "reaches", "reached", "reaching",
  "kill", "kills", "killed", "killing",
  "remain", "remains", "remained", "remaining",
  "jumps",
]);

/**
 * Subject-verb disagreement patterns (3rd person singular requires -s/-es).
 */
const SV_DISAGREEMENT_PATTERNS: Array<{ pattern: RegExp; original: string; suggestion: string }> = [
  { pattern: /\bhe do\b/i, original: "he do", suggestion: "he does" },
  { pattern: /\bshe do\b/i, original: "she do", suggestion: "she does" },
  { pattern: /\bit do\b/i, original: "it do", suggestion: "it does" },
  { pattern: /\bhe have\b/i, original: "he have", suggestion: "he has" },
  { pattern: /\bshe have\b/i, original: "she have", suggestion: "she has" },
  { pattern: /\bit have\b/i, original: "it have", suggestion: "it has" },
  { pattern: /\bhe go\b/i, original: "he go", suggestion: "he goes" },
  { pattern: /\bshe go\b/i, original: "she go", suggestion: "she goes" },
  { pattern: /\bit go\b/i, original: "it go", suggestion: "it goes" },
  { pattern: /\bhe make\b/i, original: "he make", suggestion: "he makes" },
  { pattern: /\bshe make\b/i, original: "she make", suggestion: "she makes" },
  { pattern: /\bit make\b/i, original: "it make", suggestion: "it makes" },
  { pattern: /\bhe take\b/i, original: "he take", suggestion: "he takes" },
  { pattern: /\bshe take\b/i, original: "she take", suggestion: "she takes" },
  { pattern: /\bit take\b/i, original: "it take", suggestion: "it takes" },
];

/**
 * Common nouns that often need articles.
 * Used for basic missing article detection.
 */
const COMMON_NOUNS_NEEDING_ARTICLES = new Set([
  "store", "house", "car", "dog", "cat", "book", "table", "chair",
  "door", "window", "school", "office", "computer", "phone", "city",
  "country", "world", "teacher", "doctor", "student", "problem",
  "answer", "question", "story", "movie", "game", "road", "building",
  "room", "kitchen", "garden", "park", "library", "hospital",
  "restaurant", "hotel", "airport", "station", "market", "shop",
]);

/**
 * Prepositions that commonly precede nouns requiring articles.
 */
const PREPOSITIONS_BEFORE_ARTICLES = new Set([
  "to", "in", "at", "on", "from", "with", "by", "for", "about",
  "into", "through", "during", "before", "after", "above", "below",
  "between", "under", "over", "near",
]);

// --- Rule Implementations ---

function checkMissingArticles(text: string): GrammarIssue[] {
  const issues: GrammarIssue[] = [];
  const words = text.split(/\s+/);

  for (let i = 0; i < words.length - 1; i++) {
    const current = words[i].toLowerCase().replace(/[.,!?;:]/g, "");
    const next = words[i + 1].toLowerCase().replace(/[.,!?;:]/g, "");

    // Check: preposition followed by noun without article
    if (
      PREPOSITIONS_BEFORE_ARTICLES.has(current) &&
      COMMON_NOUNS_NEEDING_ARTICLES.has(next) &&
      // The word before the noun is not already an article or possessive
      !["a", "an", "the", "my", "your", "his", "her", "its", "our", "their"].includes(current)
    ) {
      // Verify there's no article between (already handled by adjacency check)
      const position = text.indexOf(words[i + 1], text.indexOf(words[i]));
      issues.push({
        rule: "missing_articles",
        original: `${words[i]} ${words[i + 1]}`,
        suggestion: `${words[i]} the ${words[i + 1]}`,
        position,
      });
    }

    // Check: "is/was" followed directly by a noun that needs an article
    if (
      ["is", "was"].includes(current) &&
      COMMON_NOUNS_NEEDING_ARTICLES.has(next)
    ) {
      const position = text.indexOf(words[i + 1], text.indexOf(words[i]));
      issues.push({
        rule: "missing_articles",
        original: `${words[i]} ${words[i + 1]}`,
        suggestion: `${words[i]} a ${words[i + 1]}`,
        position,
      });
    }
  }

  return issues;
}

function checkRunOnSentence(text: string): GrammarIssue[] {
  const issues: GrammarIssue[] = [];

  // Split by sentence-ending punctuation
  const sentences = text.split(/[.!?;]+/).filter((s) => s.trim().length > 0);

  for (const sentence of sentences) {
    const words = sentence.trim().split(/\s+/);
    if (words.length > 40) {
      issues.push({
        rule: "run_on_sentence",
        original: sentence.trim().slice(0, 60) + "...",
        suggestion: "Consider breaking this into shorter sentences.",
        position: text.indexOf(sentence.trim()),
      });
    }
  }

  return issues;
}

function checkIncompleteSentence(text: string): GrammarIssue[] {
  const issues: GrammarIssue[] = [];
  const words = text.toLowerCase().split(/\s+/).map((w) => w.replace(/[.,!?;:]/g, ""));

  const hasVerb = words.some((w) => COMMON_VERBS.has(w));
  if (!hasVerb && words.length >= 3) {
    issues.push({
      rule: "incomplete_sentence",
      original: text.slice(0, 60),
      suggestion: "This appears to be missing a verb.",
      position: 0,
    });
  }

  return issues;
}

function checkSubjectVerbDisagreement(text: string): GrammarIssue[] {
  const issues: GrammarIssue[] = [];

  for (const { pattern, original, suggestion } of SV_DISAGREEMENT_PATTERNS) {
    const match = pattern.exec(text);
    if (match) {
      issues.push({
        rule: "subject_verb_disagreement",
        original,
        suggestion,
        position: match.index,
      });
    }
  }

  return issues;
}

// --- Rule registry ---

const RULE_CHECKERS: Record<string, (text: string) => GrammarIssue[]> = {
  missing_articles: checkMissingArticles,
  run_on_sentence: checkRunOnSentence,
  incomplete_sentence: checkIncompleteSentence,
  subject_verb_disagreement: checkSubjectVerbDisagreement,
};

// --- Tone formatting ---

function formatCorrections(
  issues: GrammarIssue[],
  tone: "gentle" | "direct" | "silent"
): string | undefined {
  if (tone === "silent") return undefined;
  if (issues.length === 0) return undefined;

  if (tone === "gentle") {
    const corrections = issues.map(
      (i) => `  Tip: "${i.original}" -> "${i.suggestion}"`
    );
    return `Here are some gentle suggestions:\n${corrections.join("\n")}`;
  }

  // Direct tone
  const corrections = issues.map(
    (i) => `"${i.original}" -> "${i.suggestion}"`
  );
  return corrections.join("; ");
}

/**
 * Analyze a prompt text for grammar issues.
 *
 * @param promptText - The user's prompt text to analyze
 * @param config - Communication coaching configuration
 * @param promptNumber - The sequential prompt number (for frequency gating)
 * @returns GrammarResult with issues and optional corrections
 */
export function analyze(
  promptText: string,
  config: CoachingConfig["communication"],
  promptNumber: number
): GrammarResult {
  const emptyResult: GrammarResult = {
    shouldCorrect: false,
    issues: [],
    improvementScore: undefined,
  };

  // Disabled check
  if (!config.enabled) return emptyResult;

  // Short prompt skip
  if (promptText.trim().length < config.minLength) return emptyResult;

  // Frequency gating
  if (promptNumber % config.frequency !== 0) {
    return emptyResult;
  }

  // Run all configured rules
  const allIssues: GrammarIssue[] = [];
  for (const ruleName of config.rules) {
    const checker = RULE_CHECKERS[ruleName];
    if (checker) {
      try {
        const issues = checker(promptText);
        allIssues.push(...issues);
      } catch {
        // Fail-open: skip broken rules
      }
    }
  }

  // Compute improvement score (100 = perfect, 0 = many issues)
  const wordCount = promptText.split(/\s+/).length;
  const issueRate = allIssues.length / Math.max(wordCount, 1);
  const improvementScore = Math.max(0, Math.round(100 * (1 - issueRate * 10)));

  // Silent tone: score only, no corrections
  if (config.tone === "silent") {
    return {
      shouldCorrect: false,
      issues: allIssues,
      improvementScore,
    };
  }

  const shouldCorrect = allIssues.length > 0;
  const correctedText = formatCorrections(allIssues, config.tone);

  return {
    shouldCorrect,
    issues: allIssues,
    correctedText,
    improvementScore,
  };
}
