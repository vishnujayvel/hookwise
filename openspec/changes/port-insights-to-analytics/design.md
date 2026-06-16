# Design: Port Insights to Analytics DB

## Context

The insights feature has had readers and a TUI tab since the architecture-v2 rewrite,
but the underlying data source — flat JSON/CSV files written by the TypeScript daemon —
was never ported. Every insights metric has silently returned zero. The analytics DB
already records all required raw data (sessions, events, tool usage, lines added) as a
side effect of dispatch. The gap is purely query-layer: nothing new needs to be written.

## Full Design

See `docs/design/insights-writer-port.md` (created in PR 4 alongside the cmd wiring).
This file summarises the key architectural decisions relevant to PR 1 (the pure query
layer).

## ARCH-3 Constraint

**Feeds must not import analytics. The interface lives in feeds; the adapter lives in
cmd.**

```
analytics  ──query──▶  cmd/adapter  ──satisfies──▶  feeds.InsightsSource
```

`internal/feeds` defines the `InsightsSource` interface (one method). The adapter in
`cmd/hookwise/analytics_adapter.go` wraps `*analytics.Analytics` and maps
analytics-local types (`InsightsSummary`, `ToolCount`, `RecentSessionInsight`) to
`feeds` types. Neither `internal/analytics` nor `internal/feeds` imports the other.
Only `cmd/hookwise` (where both are already imported) knows about the adapter.

This mirrors the established pattern used for the cost-tracking writer (PR #103), where
`transcript` and `pricing` remain fully decoupled and only `cmd_dispatch.go` combines
them.

## Analytics-Local InsightsSummary Decision

`InsightsSummary`, `ToolCount`, and `RecentSessionInsight` are defined in
`internal/analytics` (not in `feeds` or `internal/core`). Rationale:

1. **Colocation**: the types exist only to express the result of a single analytics
   query. Defining them next to that query is the lowest-coupling choice.
2. **No shared dependency**: putting them in `core` would make `core` depend on
   analytics semantics; putting them in `feeds` would force `feeds` to know about
   analytics internals — both violate the layering.
3. **Mapping is cheap**: the `cmd` adapter translates these types to `feeds`-level
   types (which may be identical in shape initially but are intentionally separate).

## Query Design

`InsightsSummary` decomposes into five focused sub-queries, matching the style of
`DailySummary` and `ToolBreakdown`:

| Sub-query | What it measures |
|-----------|-----------------|
| `querySessionCounts` | `TotalSessions`, `DaysActive` (single QueryRow) |
| `queryAvgDuration` | `AvgDurationMin` (sessions with ended_at only) |
| `queryEventAggregates` | `TotalLinesAdded`, `PeakHourUTC`, `TopTools` |
| `queryRecentDaysActive` | `RecentDaysActive` (always 7-day window) |
| `queryRecentSession` | `Recent` struct (most-recent session by started_at) |

One giant query was deliberately avoided — it would require multiple CTEs, be harder to
test in isolation, and make the error messages less actionable.

## Cutoff Handling

Timestamps in SQLite are stored as UTC ISO-8601 TEXT (`time.RFC3339` format:
`2025-03-06T09:00:00Z`). RFC3339 strings are lexicographically comparable, so
`started_at >= ?` with a cutoff string produced by `time.Now().UTC().Add(-N*24h).Format(time.RFC3339)`
is correct without any `date()` casting.

`RecentDaysActive` always uses a 7-day window regardless of `cutoffDays`, because it
measures "how many days have you been active recently" as a distinct signal from the
broader window's `DaysActive`.

## Fail-Open (ARCH-1)

An empty DB, or a DB with no sessions in the window, returns `InsightsSummary{}` with
`nil` error. All sub-queries use `sql.NullFloat64` / `sql.NullInt64` to handle
`NULL` aggregates (e.g. `AVG()` on an empty set returns NULL in SQLite).

## Testing Strategy

Unit tests use the existing `testOpen`/`testAnalytics` harness (temp-dir SQLite,
cleaned up by `defer cleanup()`). All tests are hermetic — no file system state
outside the temp DB. Test cases:

1. Empty DB → zero-value summary, nil error.
2. Multi-session fixture → every field asserted with known values.
3. Session without ended_at → excluded from AvgDurationMin, counted in TotalSessions.
4. Cutoff filtering → old session excluded, recent session included.
5. TopTools cap → never exceeds 10 entries.
6. RecentDaysActive scoping → always 7-day regardless of cutoffDays param.
