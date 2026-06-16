# Insights Analytics Query

Per-window aggregate metrics sourced from the analytics SQLite DB.

## ADDED Requirements

### Requirement: Rolling-window aggregate summary

`Analytics.InsightsSummary` MUST return a populated `InsightsSummary` for any
non-empty analytics DB.

#### Scenario: Empty database
- GIVEN an analytics DB with no sessions or events
- WHEN `InsightsSummary(ctx, 30)` is called
- THEN it MUST return a zero-value `InsightsSummary{}` and nil error (ARCH-1 fail-open)

#### Scenario: Session and event aggregation
- GIVEN sessions and events seeded across multiple dates within the cutoff window
- WHEN `InsightsSummary(ctx, cutoffDays)` is called
- THEN `TotalSessions` MUST equal the count of sessions with `started_at >= cutoff`
- AND `TotalLinesAdded` MUST equal the sum of `events.lines_added` for in-window sessions
- AND `DaysActive` MUST equal the count of distinct calendar dates in `started_at`
- AND `TopTools` MUST list up to 10 tool names ordered by event count DESC

#### Scenario: Duration average excludes open sessions
- GIVEN a mix of sessions with and without `ended_at`
- WHEN `InsightsSummary(ctx, cutoffDays)` is called
- THEN `AvgDurationMin` MUST be computed only over sessions WHERE `ended_at IS NOT NULL`
- AND sessions with NULL `ended_at` MUST be counted in `TotalSessions` and `DaysActive`

#### Scenario: Cutoff window filtering
- GIVEN sessions older than `now - cutoffDays` days
- WHEN `InsightsSummary(ctx, cutoffDays)` is called with a short window
- THEN sessions outside the window MUST be excluded from ALL aggregate fields
- AND their events MUST NOT contribute to `TotalLinesAdded`, `TopTools`, or `PeakHourUTC`

#### Scenario: No cutoff (cutoffDays <= 0)
- GIVEN `cutoffDays <= 0`
- WHEN `InsightsSummary(ctx, cutoffDays)` is called
- THEN ALL sessions MUST be included regardless of age

### Requirement: Peak hour detection

`InsightsSummary` MUST identify the UTC hour (0–23) with the highest event volume
within the cutoff window.

#### Scenario: PeakHourUTC
- GIVEN events distributed across multiple UTC hours
- WHEN `InsightsSummary` is called
- THEN `PeakHourUTC` MUST be the hour (0–23) with the highest event count
- AND ties MUST be broken by whichever hour SQLite returns first (non-deterministic
  tie-breaking is acceptable)

#### Scenario: No events
- GIVEN sessions with no events
- WHEN `InsightsSummary` is called
- THEN `PeakHourUTC` MUST be 0 (zero-value)

### Requirement: RecentDaysActive is always a 7-day window

`InsightsSummary.RecentDaysActive` MUST always reflect the last 7 calendar days,
independent of the `cutoffDays` parameter supplied to `InsightsSummary`.

#### Scenario: RecentDaysActive scope
- GIVEN sessions spanning more than 7 days (all within the cutoffDays window)
- WHEN `InsightsSummary(ctx, cutoffDays)` is called
- THEN `RecentDaysActive` MUST count only distinct dates within the last 7 days
- AND `DaysActive` MUST count all distinct dates within the full cutoff window

### Requirement: Recent session sub-struct

`InsightsSummary.Recent` MUST be populated with metrics from the session with the
latest `started_at` timestamp in the cutoff window.

#### Scenario: Most-recent session fields
- GIVEN at least one session in the cutoff window
- WHEN `InsightsSummary` is called
- THEN `Recent.ID` MUST be the ID of the session with the latest `started_at`
- AND `Recent.DurationMin` MUST be `(ended_at - started_at)` in minutes, or 0 if
  `ended_at IS NULL`
- AND `Recent.LinesAdded` MUST be the SUM of `events.lines_added` for that session
- AND `Recent.EstCostUSD` MUST equal `sessions.estimated_cost_usd` for that session

### Requirement: TopTools capped at 10

`InsightsSummary.TopTools` MUST contain at most 10 entries, ordered by event count
descending. Tool names that are empty or NULL MUST be excluded from the list.

#### Scenario: More than 10 distinct tools
- GIVEN events using more than 10 distinct tool names
- WHEN `InsightsSummary` is called
- THEN `TopTools` MUST contain at most 10 entries (the top 10 by count)
- AND entries with empty or NULL tool_name MUST be excluded

### Requirement: ARCH-3 package boundary preserved

`internal/analytics` MUST NOT import `internal/feeds` or any other hookwise
package. The `InsightsSummary` method and its types are analytics-local; the
bridge to feeds is an adapter in `cmd/hookwise` only.

#### Scenario: analytics does not import feeds
- GIVEN `internal/analytics/insights_queries.go`
- WHEN the Go compiler processes the file
- THEN it MUST NOT import `internal/feeds` or any other hookwise package
  except standard library and `database/sql`
