# Wave 0: Fix PR #73 CodeRabbit Findings + Commitlint

## Problem

PR #73 ("feat: architecture-v2 part 2 — batches G-K + CodeRabbit fixes") has 14 unresolved
CodeRabbit review comments and a failing commitlint CI check. This blocks merging the
architecture-v2 work into main.

## Goals

1. Address all 14 CodeRabbit findings across 9 files
2. Fix the commitlint CI failure
3. Get PR #73 merged to main

## Non-Goals

- No new features
- No refactoring beyond what CodeRabbit flagged
- No changes to files not mentioned in the review

## Findings to Address

### Major (3)

1. **`internal/analytics/dolt.go:355`** — `GuardBlockSummaries` bakes in `HAVING cnt >= 5`
   threshold that duplicates logic in `internal/notifications/producers.go` lines 83-86.
   Fix: Extract threshold to a shared constant or remove the duplicate filter.

2. **`internal/core/dispatch_types.go:8`** — HookPayload may need custom UnmarshalJSON
   or validation. Verify the type assertion chain is safe.

3. **`internal/notifications/notifications.go:120`** — Expired-row cleanup in `Unsurfaced()`
   is synchronous and misses per-goroutine `recover()` required by ARCH-7.
   Fix: Make non-blocking with goroutine + recover wrapper.

### Minor (8)

4. **`internal/feeds/producer_calendar_test.go:126`** — Unchecked type assertion may panic.
   Fix: Use `require` assertions for safe type assertion chains.

5. **`internal/feeds/producer_calendar_test.go:97`** — Test doesn't prove `TokenPath`
   defaulting is actually exercised. Strengthen the assertion.

6. **`internal/feeds/producer_insights_test.go:53`** — Test doesn't verify all data keys
   from `zeroedEnvelope`. Add assertions for `recent_msgs_per_day`, `recent_messages`.

7. **`internal/feeds/producer_insights_test.go:186`** — Unchecked type assertion may panic.
   Fix: Use `require` assertions.

8. **`internal/feeds/producer_insights_test.go:150`** — Cancelled-context test can pass
   without exercising cancellation. Strengthen to prove the cancel path ran.

9. **`internal/feeds/producer_news_test.go:33`** — Missing assertion on `story.score` field.

10. **`internal/feeds/producer_news_test.go:69`** — Should use `assertValidEnvelope` helper
    instead of duplicated checks.

11. **`internal/feeds/producer_project_test.go:97`** — Cancelled-context test only validates
    envelope shape, not that fallback payload differs from normal.

12. **`internal/feeds/producer_weather_test.go:274`** — Unchecked type assertion may panic.

### Trivial (2)

13. **`internal/core/config_types.go:108`** — JSON tag `maxSizeMb` inconsistent with field
    name `MaxSizeMB`. Fix: Change tag to `maxSizeMB` or `max_size_mb`.

14. **`internal/feeds/producer_weather_test.go:254`** — Test doesn't prove 503 fallback
    path actually hit the server. Add `callCount` assertion.

### Commitlint

The commitlint CI check is failing. Likely a commit message format issue in the PR's
commits. Fix: Ensure all commit messages follow conventional commits format.

## Testing Strategy

- All 583 existing tests must continue to pass
- New/modified test assertions must be verified with `-race -p 2`
- Contract tests must remain byte-identical
- Run `go vet ./...` clean

## Files Changed

- `internal/analytics/dolt.go`
- `internal/core/config_types.go`
- `internal/core/dispatch_types.go`
- `internal/notifications/notifications.go`
- `internal/feeds/producer_calendar_test.go`
- `internal/feeds/producer_insights_test.go`
- `internal/feeds/producer_news_test.go`
- `internal/feeds/producer_project_test.go`
- `internal/feeds/producer_weather_test.go`
