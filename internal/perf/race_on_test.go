//go:build integration && race

package perf

// raceEnabled reports whether the race detector is compiled in.
// Race instrumentation slows hot paths 2-10x, so latency budgets
// must be scaled to avoid false failures.
const raceEnabled = true
