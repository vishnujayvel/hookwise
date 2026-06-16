# Tasks: Port Insights to Analytics DB

All test execution is funneled through `task test` or `dagger call ci --src=.`.
**Never bare `go test ./...`** (retro-009 resource safety).

## PR 1 — Pure analytics query layer (THIS PR — in progress)

- [x] 1.1 Define analytics-local types: `ToolCount`, `RecentSessionInsight`,
      `InsightsSummary` in `internal/analytics/insights_queries.go`.
- [x] 1.2 Implement `Analytics.InsightsSummary(ctx, cutoffDays int)` decomposed into
      five focused sub-queries (session counts, avg duration, event aggregates,
      recent days active, recent session).
- [x] 1.3 Unit tests: empty DB, multi-session fixture (all fields), no-ended-at
      exclusion, cutoff filtering, TopTools cap, RecentDaysActive scoping.
- [ ] 1.4 **Validate**: `go build ./...` and `go vet ./internal/analytics/...` clean.
- [ ] 1.5 Open PR 1.

## PR 2 — Doctor honesty

- [ ] 2.1 Update doctor insights check to report sourcing from analytics DB.
- [ ] 2.2 Flag when analytics DB is empty or unreachable in doctor output.
- [ ] 2.3 **Validate**: `task test` passes.
- [ ] 2.4 Open PR 2.

## PR 3 — Feeds interface + adapter

- [ ] 3.1 Add `InsightsSource` interface to `internal/feeds` (method:
      `InsightsSummary(ctx context.Context, days int) (InsightsSummary, error)`).
- [ ] 3.2 Define `feeds.InsightsSummary` type (mirroring analytics-local shape).
- [ ] 3.3 Create `cmd/hookwise/analytics_adapter.go`: adapter wrapping
      `*analytics.Analytics`, satisfying `feeds.InsightsSource`.
- [ ] 3.4 Verify no `feeds` → `analytics` import (ARCH-3): `go vet` + arch lint.
- [ ] 3.5 **Validate**: `task test` passes.
- [ ] 3.6 Open PR 3.

## PR 4 — Cmd wiring

- [ ] 4.1 Wire `InsightsSource` adapter into the dispatch/daemon path: populate
      the feeds cache on each session end.
- [ ] 4.2 Update the `insights` command to read from the analytics DB via adapter
      rather than stale flat-file cache.
- [ ] 4.3 Create `docs/design/insights-writer-port.md` (full design doc).
- [ ] 4.4 **Validate**: `task test` passes; `hookwise insights` shows real data.
- [ ] 4.5 Open PR 4.

## PR 5 — Python TUI repoint

- [ ] 5.1 Update `tui/data.py` to read the new feed cache key written by PR 4.
- [ ] 5.2 Regenerate TUI snapshot fixtures (`pytest --snapshot-update`).
- [ ] 5.3 Add cross-boundary schema test in `tui/tests/` validating rendered output
      against Go-produced feed cache data (Bug #29 prevention pattern).
- [ ] 5.4 **Validate**: `task test:tui:unit` green on Python 3.11/3.12/3.13.
- [ ] 5.5 Open PR 5.

## Resource-safety invariants (apply to every PR)
- Never bare `go test ./...`. `dagger call ci` or `task test` only.
- `pgrep -f hookwise.test` before any host-side test run; kill zombies first.
- One test run at a time; no concurrent test execution across agents.
