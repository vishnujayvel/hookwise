// Package pricing computes USD cost from Anthropic token usage for a given model.
//
// # Rate table
//
// Built-in rates are specified in USD per 1,000,000 tokens ($/MTok) and match
// Anthropic's published pricing for current Claude model families:
//
//	Family   Input    Output   Cache-read   Cache-write
//	──────── ──────── ──────── ──────────── ────────────
//	Opus     15.00    75.00       1.50         18.75
//	Sonnet    3.00    15.00       0.30          3.75
//	Haiku     0.80     4.00       0.08          1.00
//
// # Model matching
//
// Model IDs are matched case-insensitively via substring search, checked in
// priority order: opus → sonnet → haiku.  Any model ID that contains "opus"
// (e.g. "claude-opus-4-8", "CLAUDE-OPUS-4-20250601") maps to Opus rates.
// The same rule applies for "sonnet" and "haiku".  Unknown model IDs fall
// back to Sonnet rates.
//
// # Override key convention (for CostTrackingConfig.Rates)
//
// The overrides map accepted by [ComputeWithRates] uses keys of the form:
//
//	"<family>.<rate-type>"
//
// where <family> is one of "opus", "sonnet", "haiku" and <rate-type> is one
// of "input", "output", "cache_read", "cache_write".  Examples:
//
//	"sonnet.input"       → override Sonnet input rate
//	"opus.cache_write"   → override Opus cache-write rate
//
// Only keys present in the map are overridden; absent keys retain their
// built-in values.  A nil or empty map is a no-op (identical to [Compute]).
package pricing

import "strings"

// Usage holds the four token-count buckets reported in an Anthropic transcript.
// All counts are in raw tokens (not millions).
type Usage struct {
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// modelRates holds per-MTok USD rates for a single model family.
type modelRates struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
}

// family is an internal enum for the three supported model families.
type family string

const (
	familyOpus   family = "opus"
	familySonnet family = "sonnet"
	familyHaiku  family = "haiku"
)

// defaultFamily is the fallback when the model ID does not match any family.
const defaultFamily = familySonnet

// builtinRates holds the canonical per-MTok USD rates per family.
var builtinRates = map[family]modelRates{
	familyOpus: {
		Input:      15.00,
		Output:     75.00,
		CacheRead:  1.50,
		CacheWrite: 18.75,
	},
	familySonnet: {
		Input:      3.00,
		Output:     15.00,
		CacheRead:  0.30,
		CacheWrite: 3.75,
	},
	familyHaiku: {
		Input:      0.80,
		Output:     4.00,
		CacheRead:  0.08,
		CacheWrite: 1.00,
	},
}

// matchFamily returns the model family for the given model ID.
// Matching is case-insensitive substring search in priority order:
// opus → sonnet → haiku.  Unrecognised IDs return defaultFamily.
func matchFamily(model string) family {
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "opus"):
		return familyOpus
	case strings.Contains(lower, "sonnet"):
		return familySonnet
	case strings.Contains(lower, "haiku"):
		return familyHaiku
	default:
		return defaultFamily
	}
}

// resolveRates returns the effective modelRates for the given family, applying
// any overrides from the supplied map.  The override map may be nil.
func resolveRates(fam family, overrides map[string]float64) modelRates {
	r := builtinRates[fam] // copy of the struct
	prefix := string(fam) + "."
	if len(overrides) == 0 {
		return r
	}
	if v, ok := overrides[prefix+"input"]; ok {
		r.Input = v
	}
	if v, ok := overrides[prefix+"output"]; ok {
		r.Output = v
	}
	if v, ok := overrides[prefix+"cache_read"]; ok {
		r.CacheRead = v
	}
	if v, ok := overrides[prefix+"cache_write"]; ok {
		r.CacheWrite = v
	}
	return r
}

// computeCost calculates the USD cost from token counts and a resolved rate table.
func computeCost(u Usage, r modelRates) float64 {
	return float64(u.InputTokens)/1e6*r.Input +
		float64(u.OutputTokens)/1e6*r.Output +
		float64(u.CacheReadTokens)/1e6*r.CacheRead +
		float64(u.CacheCreationTokens)/1e6*r.CacheWrite
}

// Compute returns the estimated USD cost for the given model and token usage
// using built-in rate tables.  Unknown model IDs fall back to Sonnet rates
// and never panic.  Zero usage always returns 0.0.
func Compute(model string, u Usage) float64 {
	fam := matchFamily(model)
	r := resolveRates(fam, nil)
	return computeCost(u, r)
}

// ComputeWithRates returns the estimated USD cost, applying per-family rate
// overrides from the supplied map before computing.  The override map uses
// keys of the form "<family>.<rate-type>" (see package doc for details).
// A nil or empty overrides map produces identical results to [Compute].
// Unknown model IDs fall back to Sonnet rates; unknown override keys are
// silently ignored.  Never panics.
func ComputeWithRates(model string, u Usage, overrides map[string]float64) float64 {
	fam := matchFamily(model)
	r := resolveRates(fam, overrides)
	return computeCost(u, r)
}
