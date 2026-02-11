"""Communication coach for hookwise.

Analyzes user prompt text for grammar issues using simple regex-based
rules. Designed for voice-to-text scenarios where common grammatical
errors occur. Applies frequency gating (every Nth prompt) and skips
short prompts.

Built-in rules:
- missing_articles: Detects likely missing "a", "an", "the"
- run_on_sentences: Detects very long sentences without punctuation
- incomplete_sentences: Detects prompts that appear to end mid-thought
- subject_verb_disagreement: Detects common subject-verb mismatches

All corrections are formatted as gentle suggestions in additionalContext.
A rolling improvement score is tracked over the last 20 prompts.
"""

from __future__ import annotations

import logging
import re
from typing import Any

logger = logging.getLogger("hookwise")

# Default frequency: check every Nth prompt
DEFAULT_FREQUENCY = 3

# Minimum prompt length to analyze
DEFAULT_MIN_LENGTH = 10

# Maximum number of scores to keep for rolling average
MAX_SCORE_HISTORY = 20

# Tone templates
TONE_TEMPLATES = {
    "gentle": "Gentle suggestion: {correction}",
    "direct": "Note: {correction}",
    "minimal": "{correction}",
}


# ---------------------------------------------------------------------------
# Grammar rules
# ---------------------------------------------------------------------------


def _check_missing_articles(text: str) -> list[str]:
    """Check for likely missing articles (a, an, the).

    Detects patterns where common nouns follow verbs/prepositions
    without an article. This is a heuristic and will have false positives.

    Args:
        text: The user's prompt text.

    Returns:
        List of correction suggestion strings.
    """
    issues = []
    # Pattern: common verbs/preps followed by a noun-like word without article
    # e.g., "create file", "open window", "fix bug"
    pattern = re.compile(
        r"\b(create|open|fix|add|write|make|build|update|delete|remove|set|get|find|check|run|start)\s+"
        r"(?!a\b|an\b|the\b|this\b|that\b|my\b|your\b|our\b|some\b|any\b|each\b|every\b|all\b|no\b)"
        r"([a-z]{3,})\b",
        re.IGNORECASE,
    )
    matches = pattern.findall(text)
    if matches:
        for verb, noun in matches[:2]:  # Limit to 2 suggestions
            issues.append(
                f'Consider adding an article before "{noun}" '
                f'(e.g., "a {noun}" or "the {noun}")'
            )
    return issues


def _check_run_on_sentences(text: str) -> list[str]:
    """Check for run-on sentences (very long without punctuation).

    A sentence with more than 40 words without a period, semicolon,
    or other sentence-ending punctuation is flagged.

    Args:
        text: The user's prompt text.

    Returns:
        List of correction suggestion strings.
    """
    issues = []
    # Split on sentence-ending punctuation
    sentences = re.split(r"[.!?;]", text)
    for sentence in sentences:
        words = sentence.strip().split()
        if len(words) > 40:
            issues.append(
                "This might be a run-on sentence. Consider breaking it into "
                "shorter sentences for clarity."
            )
            break  # One suggestion is enough
    return issues


def _check_incomplete_sentences(text: str) -> list[str]:
    """Check for prompts that appear to end mid-thought.

    Detects prompts ending with conjunctions, prepositions, or
    other words that suggest more was intended.

    Args:
        text: The user's prompt text.

    Returns:
        List of correction suggestion strings.
    """
    issues = []
    stripped = text.rstrip()
    if not stripped:
        return issues

    # Check if ending with a conjunction or preposition
    trailing_pattern = re.compile(
        r"\b(and|but|or|with|from|to|for|in|on|at|by|the|a|an|that|which|when|if|so|because|then)\s*$",
        re.IGNORECASE,
    )
    if trailing_pattern.search(stripped):
        issues.append(
            "Your prompt appears to end mid-thought. "
            "Did you mean to continue?"
        )
    return issues


def _check_subject_verb_disagreement(text: str) -> list[str]:
    """Check for common subject-verb agreement issues.

    Detects simple patterns like "it don't", "he don't", "they was",
    "we was", etc.

    Args:
        text: The user's prompt text.

    Returns:
        List of correction suggestion strings.
    """
    issues = []
    patterns = [
        (r"\b(it|he|she)\s+don't\b", '"doesn\'t" instead of "don\'t"'),
        (r"\b(they|we)\s+was\b", '"were" instead of "was"'),
        (r"\b(I|you|we|they)\s+has\b", '"have" instead of "has"'),
        (r"\b(he|she|it)\s+have\b", '"has" instead of "have"'),
    ]
    for pattern, suggestion in patterns:
        if re.search(pattern, text, re.IGNORECASE):
            issues.append(f"Consider using {suggestion}")
    return issues


# Rule registry
GRAMMAR_RULES = {
    "missing_articles": _check_missing_articles,
    "run_on_sentences": _check_run_on_sentences,
    "incomplete_sentences": _check_incomplete_sentences,
    "subject_verb_disagreement": _check_subject_verb_disagreement,
}


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------


def analyze_prompt(
    text: str,
    cache: dict[str, Any],
    config: dict[str, Any],
) -> dict[str, Any] | None:
    """Analyze a user prompt for grammar issues with frequency gating.

    Increments the prompt check counter and only performs analysis
    every Nth prompt (configurable). Skips short prompts.

    Args:
        text: The user's prompt text.
        cache: The coaching cache dict (mutated in place).
        config: The communication coach config section.

    Returns:
        Dict with "corrections" (list of strings) and "score" (float 0-1),
        or None if analysis was skipped (not enabled, frequency gate, too short).
    """
    if not config.get("enabled", False):
        return None

    frequency = config.get("frequency", DEFAULT_FREQUENCY)
    min_length = config.get("min_length", DEFAULT_MIN_LENGTH)

    # Increment counter
    counter = cache.get("prompt_check_counter", 0) + 1
    cache["prompt_check_counter"] = counter

    # Frequency gating: only analyze every Nth prompt
    if counter % frequency != 0:
        return None

    # Skip short prompts
    if len(text.strip()) < min_length:
        return None

    # Run configured rules
    active_rules = config.get("rules", list(GRAMMAR_RULES.keys()))
    tone = config.get("tone", "gentle")
    template = TONE_TEMPLATES.get(tone, TONE_TEMPLATES["gentle"])

    all_issues: list[str] = []
    for rule_name in active_rules:
        rule_fn = GRAMMAR_RULES.get(rule_name)
        if rule_fn is None:
            continue
        try:
            issues = rule_fn(text)
            all_issues.extend(issues)
        except Exception as exc:
            logger.debug("Grammar rule %s failed: %s", rule_name, exc)

    # Compute score: 1.0 = perfect, 0.0 = many issues
    if all_issues:
        # More issues = lower score, capped at 0.0
        score = max(0.0, 1.0 - (len(all_issues) * 0.25))
    else:
        score = 1.0

    # Update rolling scores
    scores = cache.get("grammar_scores", [])
    scores.append(score)
    if len(scores) > MAX_SCORE_HISTORY:
        scores = scores[-MAX_SCORE_HISTORY:]
    cache["grammar_scores"] = scores

    if not all_issues:
        return None

    # Format corrections
    corrections = [template.format(correction=issue) for issue in all_issues]

    return {
        "corrections": corrections,
        "score": score,
        "rolling_average": sum(scores) / len(scores) if scores else 1.0,
    }
