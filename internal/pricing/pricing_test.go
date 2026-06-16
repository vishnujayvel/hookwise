package pricing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishnujayvel/hookwise/internal/pricing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const delta = 1e-9

// knownCost computes the expected cost manually for a given set of rates
// (all in USD per 1,000,000 tokens).
func knownCost(u pricing.Usage, inRate, outRate, crRate, cwRate float64) float64 {
	return float64(u.InputTokens)/1e6*inRate +
		float64(u.OutputTokens)/1e6*outRate +
		float64(u.CacheReadTokens)/1e6*crRate +
		float64(u.CacheCreationTokens)/1e6*cwRate
}

// ---------------------------------------------------------------------------
// Zero usage
// ---------------------------------------------------------------------------

func TestZeroUsage_ReturnsZero(t *testing.T) {
	cost := pricing.Compute("claude-opus-4-5", pricing.Usage{})
	assert.InDelta(t, 0.0, cost, delta, "zero usage must return 0.0")
}

func TestZeroUsage_UnknownModel_ReturnsZero(t *testing.T) {
	cost := pricing.Compute("totally-unknown-model-xyz", pricing.Usage{})
	assert.InDelta(t, 0.0, cost, delta, "zero usage on unknown model must return 0.0")
}

// ---------------------------------------------------------------------------
// Opus family
// ---------------------------------------------------------------------------

// Published rates: input=15, output=75, cacheRead=1.50, cacheWrite=18.75 ($/MTok)

func TestOpus_InputOnly(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	cost := pricing.Compute("claude-opus-4", u)
	assert.InDelta(t, 15.0, cost, delta)
}

func TestOpus_OutputOnly(t *testing.T) {
	u := pricing.Usage{OutputTokens: 1_000_000}
	cost := pricing.Compute("claude-opus-4", u)
	assert.InDelta(t, 75.0, cost, delta)
}

func TestOpus_AllTokenTypes(t *testing.T) {
	u := pricing.Usage{
		InputTokens:         1_000_000,
		OutputTokens:        500_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
	}
	want := knownCost(u, 15.0, 75.0, 1.50, 18.75)
	cost := pricing.Compute("claude-opus-4", u)
	assert.InDelta(t, want, cost, delta)
}

// Suffix variant: "claude-opus-4-8" must still resolve to Opus family.
func TestOpus_SuffixVariant(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	cost := pricing.Compute("claude-opus-4-8", u)
	assert.InDelta(t, 15.0, cost, delta, "claude-opus-4-8 should map to Opus rates")
}

// ---------------------------------------------------------------------------
// Sonnet family
// ---------------------------------------------------------------------------

// Published rates: input=3, output=15, cacheRead=0.30, cacheWrite=3.75 ($/MTok)

func TestSonnet_InputOnly(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	cost := pricing.Compute("claude-sonnet-4", u)
	assert.InDelta(t, 3.0, cost, delta)
}

func TestSonnet_AllTokenTypes(t *testing.T) {
	u := pricing.Usage{
		InputTokens:         2_000_000,
		OutputTokens:        1_000_000,
		CacheReadTokens:     500_000,
		CacheCreationTokens: 250_000,
	}
	want := knownCost(u, 3.0, 15.0, 0.30, 3.75)
	cost := pricing.Compute("claude-sonnet-4", u)
	assert.InDelta(t, want, cost, delta)
}

// Legacy model ID: "claude-3-5-sonnet-20241022" must map to Sonnet family.
func TestSonnet_LegacyModelID(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	cost := pricing.Compute("claude-3-5-sonnet-20241022", u)
	assert.InDelta(t, 3.0, cost, delta, "claude-3-5-sonnet-20241022 should map to Sonnet rates")
}

// Sonnet 4.5 variant.
func TestSonnet_45Variant(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	cost := pricing.Compute("claude-sonnet-4-5", u)
	assert.InDelta(t, 3.0, cost, delta, "claude-sonnet-4-5 should map to Sonnet rates")
}

// ---------------------------------------------------------------------------
// Haiku family
// ---------------------------------------------------------------------------

// Published rates: input=0.80, output=4, cacheRead=0.08, cacheWrite=1 ($/MTok)

func TestHaiku_InputOnly(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	cost := pricing.Compute("claude-haiku-4", u)
	assert.InDelta(t, 0.80, cost, delta)
}

func TestHaiku_AllTokenTypes(t *testing.T) {
	u := pricing.Usage{
		InputTokens:         1_000_000,
		OutputTokens:        1_000_000,
		CacheReadTokens:     1_000_000,
		CacheCreationTokens: 1_000_000,
	}
	want := knownCost(u, 0.80, 4.0, 0.08, 1.0)
	cost := pricing.Compute("claude-haiku-4", u)
	assert.InDelta(t, want, cost, delta)
}

// Legacy Haiku: "claude-3-haiku-20240307"
func TestHaiku_LegacyModelID(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	cost := pricing.Compute("claude-3-haiku-20240307", u)
	assert.InDelta(t, 0.80, cost, delta, "claude-3-haiku-20240307 should map to Haiku rates")
}

// ---------------------------------------------------------------------------
// Case-insensitive matching
// ---------------------------------------------------------------------------

func TestCaseInsensitive_Opus(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	upper := pricing.Compute("CLAUDE-OPUS-4", u)
	lower := pricing.Compute("claude-opus-4", u)
	require.NotZero(t, lower, "lower-case should not be zero")
	assert.InDelta(t, lower, upper, delta, "matching must be case-insensitive")
}

func TestCaseInsensitive_Haiku(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	mixed := pricing.Compute("Claude-Haiku-4", u)
	assert.InDelta(t, 0.80, mixed, delta, "mixed-case Haiku should match")
}

// ---------------------------------------------------------------------------
// Unknown model falls back to Sonnet without panic
// ---------------------------------------------------------------------------

func TestUnknownModel_FallsBackToSonnet(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	cost := pricing.Compute("some-future-model-9999", u)
	// Must not panic AND must return a positive, sane cost (Sonnet default = $3/MTok)
	assert.InDelta(t, 3.0, cost, delta, "unknown model should fall back to Sonnet input rate")
}

func TestUnknownModel_EmptyString(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	cost := pricing.Compute("", u)
	assert.InDelta(t, 3.0, cost, delta, "empty model string should fall back gracefully")
}

// ---------------------------------------------------------------------------
// Cache token costs are included
// ---------------------------------------------------------------------------

func TestCacheRead_NonZeroContribution(t *testing.T) {
	base := pricing.Usage{InputTokens: 1_000_000}
	withCache := pricing.Usage{InputTokens: 1_000_000, CacheReadTokens: 1_000_000}
	costBase := pricing.Compute("claude-sonnet-4", base)
	costCache := pricing.Compute("claude-sonnet-4", withCache)
	assert.Greater(t, costCache, costBase, "cache read tokens must add to cost")
}

func TestCacheCreation_NonZeroContribution(t *testing.T) {
	base := pricing.Usage{InputTokens: 1_000_000}
	withWrite := pricing.Usage{InputTokens: 1_000_000, CacheCreationTokens: 1_000_000}
	costBase := pricing.Compute("claude-sonnet-4", base)
	costWrite := pricing.Compute("claude-sonnet-4", withWrite)
	assert.Greater(t, costWrite, costBase, "cache creation tokens must add to cost")
}

// ---------------------------------------------------------------------------
// ComputeWithRates — override map changes the result
// ---------------------------------------------------------------------------

// Override key convention: "<family>.input", "<family>.output",
// "<family>.cache_read", "<family>.cache_write"
// where <family> is "opus", "sonnet", or "haiku".

func TestComputeWithRates_OverrideInput(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	overrides := map[string]float64{
		"sonnet.input": 99.0, // user-supplied custom rate
	}
	cost := pricing.ComputeWithRates("claude-sonnet-4", u, overrides)
	assert.InDelta(t, 99.0, cost, delta, "override map must replace built-in input rate")
}

func TestComputeWithRates_OverrideAllRates(t *testing.T) {
	u := pricing.Usage{
		InputTokens:         1_000_000,
		OutputTokens:        1_000_000,
		CacheReadTokens:     1_000_000,
		CacheCreationTokens: 1_000_000,
	}
	overrides := map[string]float64{
		"opus.input":       10.0,
		"opus.output":      20.0,
		"opus.cache_read":  2.0,
		"opus.cache_write": 5.0,
	}
	want := 10.0 + 20.0 + 2.0 + 5.0 // 1 MTok each
	cost := pricing.ComputeWithRates("claude-opus-4", u, overrides)
	assert.InDelta(t, want, cost, delta)
}

func TestComputeWithRates_PartialOverride_MixesWithBuiltin(t *testing.T) {
	// Override only output; input should remain at built-in Haiku rate (0.80).
	u := pricing.Usage{
		InputTokens:  1_000_000,
		OutputTokens: 1_000_000,
	}
	overrides := map[string]float64{
		"haiku.output": 100.0,
	}
	cost := pricing.ComputeWithRates("claude-haiku-4", u, overrides)
	// input=0.80 (built-in) + output=100.0 (override)
	assert.InDelta(t, 100.80, cost, delta)
}

func TestComputeWithRates_NilOverrides_BehavesLikeCompute(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000, OutputTokens: 500_000}
	plain := pricing.Compute("claude-sonnet-4", u)
	withNil := pricing.ComputeWithRates("claude-sonnet-4", u, nil)
	assert.InDelta(t, plain, withNil, delta)
}

func TestComputeWithRates_EmptyOverrides_BehavesLikeCompute(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	plain := pricing.Compute("claude-haiku-4", u)
	withEmpty := pricing.ComputeWithRates("claude-haiku-4", u, map[string]float64{})
	assert.InDelta(t, plain, withEmpty, delta)
}

// Unknown model with override — the fallback family key "sonnet" must be
// honoured if present.
func TestComputeWithRates_UnknownModel_FallbackOverride(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	overrides := map[string]float64{
		"sonnet.input": 7.5,
	}
	cost := pricing.ComputeWithRates("totally-unknown", u, overrides)
	assert.InDelta(t, 7.5, cost, delta, "unknown model falls back to sonnet; override applies")
}

// ---------------------------------------------------------------------------
// Prefix / substring matching edge cases
// ---------------------------------------------------------------------------

// Model IDs with dates appended must still resolve correctly.
func TestPrefixMatch_SonnetWithDate(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	cost := pricing.Compute("claude-3-5-sonnet-20241022", u)
	assert.InDelta(t, 3.0, cost, 1e-6)
}

func TestPrefixMatch_OpusWithDate(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	// Hypothetical future date-stamped Opus ID.
	cost := pricing.Compute("claude-opus-4-20250601", u)
	assert.InDelta(t, 15.0, cost, 1e-6)
}

// Ensure "sonnet" does not accidentally match "haiku-sonnet" (no real model,
// but verifies the matcher doesn't over-generalise).
func TestSubstringMatch_DoesNotConfuseHaikuForSonnet(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000}
	// "haiku" appears before "sonnet" in priority; a string containing "haiku"
	// must resolve to Haiku rates regardless of suffix.
	cost := pricing.Compute("claude-haiku-3-20240307", u)
	assert.InDelta(t, 0.80, cost, delta, "haiku prefix must win over sonnet substring")
}

// ---------------------------------------------------------------------------
// Fractional token counts (large numbers, not exact MTok)
// ---------------------------------------------------------------------------

func TestFractionalTokens_Sonnet(t *testing.T) {
	u := pricing.Usage{InputTokens: 123_456, OutputTokens: 78_901}
	want := float64(123_456)/1e6*3.0 + float64(78_901)/1e6*15.0
	cost := pricing.Compute("claude-sonnet-4", u)
	assert.InDelta(t, want, cost, 1e-9)
}

// ---------------------------------------------------------------------------
// Recognized (audit #26: fallback telemetry)
// ---------------------------------------------------------------------------

// TestRecognized verifies that known families report true (case-insensitively,
// via substring) and that unknown IDs report false so the cost path can warn
// when a cost is derived from fallback Sonnet rates.
func TestRecognized(t *testing.T) {
	known := []string{
		"claude-opus-4-8",
		"CLAUDE-OPUS-4-20250601",
		"claude-sonnet-4-6",
		"claude-3-5-haiku-20241022",
		"some-haiku-variant",
	}
	for _, m := range known {
		assert.True(t, pricing.Recognized(m), "expected %q to be recognized", m)
	}

	unknown := []string{
		"",
		"gpt-4o",
		"gemini-2.5-pro",
		"claude-x-9",
		"unknown-model",
	}
	for _, m := range unknown {
		assert.False(t, pricing.Recognized(m), "expected %q to be unrecognized", m)
	}
}

// TestRecognized_FallbackStillComputes confirms the telemetry change did not
// alter cost math: an unrecognized model still computes at Sonnet rates and is
// identical to an explicitly-Sonnet model with the same usage.
func TestRecognized_FallbackStillComputes(t *testing.T) {
	u := pricing.Usage{InputTokens: 1_000_000, OutputTokens: 500_000}

	unknownCost := pricing.Compute("gpt-4o", u)
	sonnetCost := pricing.Compute("claude-sonnet-4-6", u)

	require.False(t, pricing.Recognized("gpt-4o"))
	assert.InDelta(t, sonnetCost, unknownCost, delta,
		"unknown model must still compute at Sonnet fallback rates")
}
