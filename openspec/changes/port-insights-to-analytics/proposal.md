# Proposal: Port Insights to Analytics DB

## Status
In progress — 2026-06-16

## Summary
Port the insights data source from flat-file reads to the analytics SQLite DB.
The insights readers have always pointed at flat files written by a TS-era producer
that was never ported to Go. As a result, all insights metrics (`TotalSessions`,
`TopTools`, `PeakHourUTC`, etc.) have returned zero/empty since the Go rewrite.
This is a silent, always-wrong data gap — the same class of problem as the cost-tracking
writer (WRITER AUDIT #99).

## Problem

The insights TUI tab and `insights` command read aggregates that were originally
produced by a TypeScript daemon writing to flat JSON/CSV files. The Go rewrite
ported the *readers* (the feed producer, the TUI binding) but never the *writers*.
The analytics DB already records all the raw data needed — sessions, events,
tool usage, lines added — as a side effect of dispatch. Nothing needs to be
written that is not already there; the data just needs to be *queried*.

A secondary issue: the existing insights code imports `feeds.InsightsSummary`, which
creates a cross-package coupling from `feeds` into the analytics domain. Under ARCH-3,
`feeds` must not import `analytics` (analytics is a production DB; feeds is an
output-only cache layer). The correct boundary is: analytics provides the query
method; a thin adapter in `cmd` bridges the two.

## Goals

1. Add `Analytics.InsightsSummary(ctx, cutoffDays)` — a pure analytics-DB query with
   no flat-file I/O and no feed imports (this PR).
2. Surface honest data in the doctor check so users know insights metrics are live.
3. Expose the analytics-local type through a `feeds.InsightsSource` interface so the
   feeds layer can call it without importing `analytics`.
4. Wire `cmd/hookwise` to call `InsightsSummary` and populate the feeds cache.
5. Repoint the Python TUI from the old flat-file path to the new feed cache key.

## Non-Goals

- Historical backfill of pre-analytics-DB insight data.
- Predictive insights (trend lines, forecasting).
- Per-project or per-repo breakdowns (single global view only for now).

## Solution

Re-source insights from the analytics DB. `Analytics.InsightsSummary` runs focused
SQL queries matching the `DailySummary`/`ToolBreakdown` style already proven in the
codebase. Types are analytics-local (`InsightsSummary`, `ToolCount`,
`RecentSessionInsight`) — feeds gets the values via interface, not the types.

This approach is ARCH-3-safe: the `analytics` package has no knowledge of `feeds`;
the adapter lives in `cmd` where both packages are already imported.

## Phased PR Plan

### PR 1 — Pure analytics query layer (THIS PR)
- `internal/analytics/insights_queries.go` — `InsightsSummary` method + local types.
- `internal/analytics/insights_queries_test.go` — hermetic unit tests.
- No wiring, no feed changes, no TUI changes. Compile-check + vet only.

### PR 2 — Doctor honesty
- Update the doctor check for insights to report "sourced from analytics DB" (not
  "reads flat files") and flag when the analytics DB is empty or unreachable.

### PR 3 — Feeds interface + adapter
- Add `InsightsSource` interface to `internal/feeds` (one method:
  `InsightsSummary(ctx, days) (feeds.InsightsSummary, error)`).
- Add adapter in `cmd/hookwise/analytics_adapter.go` that wraps `*analytics.Analytics`
  and satisfies `InsightsSource`, mapping analytics-local types to feeds types.
- No TUI impact yet.

### PR 4 — Cmd wiring
- Wire the `InsightsSource` adapter into the dispatch / daemon path so the feeds cache
  is populated on each session end.
- Update the `insights` command to read from the analytics DB (via the adapter) rather
  than the stale flat-file cache.

### PR 5 — Python TUI repoint
- Update `tui/data.py` to read the new feed cache key written by PR 4.
- Regenerate TUI snapshot fixtures.
- Add cross-boundary schema test to prevent the Bug #29 (field-name mismatch)
  class of regression.

## Testing Strategy

- PR 1: hermetic temp-dir SQLite DB; no disk I/O beyond the analytics DB itself;
  `-race -p 2` clean.
- PRs 3–4: interface contract tests verify adapter output against known analytics
  DB fixtures.
- PR 5: cross-boundary test in `tui/tests/` validates rendered output against
  Go-produced feed cache data (matching the Bug #29 fix pattern).
- All test execution via `task test` or `dagger call ci` — never bare `go test ./...`.

## Acceptance Criteria

- [ ] `InsightsSummary` returns correct values for all fields on a seeded analytics DB
- [ ] Empty DB returns zero-value summary (ARCH-1 fail-open)
- [ ] Cutoff filtering correctly excludes out-of-window sessions
- [ ] `AvgDurationMin` excludes sessions without ended_at
- [ ] TopTools capped at 10, ordered by count DESC
- [ ] RecentDaysActive always scoped to 7-day window regardless of cutoffDays
- [ ] Doctor check reflects live analytics-DB sourcing (PR 2)
- [ ] TUI insights tab shows real data from analytics DB (PR 5)
- [ ] No `feeds` → `analytics` import (ARCH-3 preserved across all PRs)

## Scope

### Files created (PR 1 — this PR)
- `internal/analytics/insights_queries.go`
- `internal/analytics/insights_queries_test.go`
- `openspec/changes/port-insights-to-analytics/` (this spec)
