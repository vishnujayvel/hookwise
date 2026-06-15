## Why

Architecture-v2 part 1 (PR #72) completed batches A-F: envelope builder, warn-open observability, notification TTL, CLI split, producer split, and retro-009 resource safety mitigations. Part 2 completes the remaining batches G-L plus addresses 7 deferred CodeRabbit findings from PR #72 that improve resilience and correctness of feed producers and notifications.

## What Changes

**God Package Decomposition (continued):**
- Split `internal/core/types.go` (~400 lines) into domain-focused sub-files (config, dispatch, guard, feed, analytics, handler, test types)

**Phase 3 — Consolidation & Cleanup:**
- Consolidate ad-hoc `time.Parse()` calls across bridge, feeds, analytics into canonical `ParseTimeFlex`
- Export `HomeDir()` from core/constants.go and delete 3 duplicate `os.UserHomeDir()` implementations
- Extract guard block SQL from notifications into a typed analytics method
- Delete dead `WriteFeedCacheJSON` code from analytics
- Add error path + envelope structure tests for all feed producers

**CodeRabbit Findings (from PR #72):**
- Fix expired notifications being rescanned forever (mark as surfaced on expiry)
- Add atomic token write-back for calendar producer
- Honor context and limit materialization in insights producer
- Add git command deadline in project producer
- Narrow ALTER TABLE error handling in dolt.go migration

## Capabilities

### Modified Capabilities
- `types-organization`: Split monolithic types.go into 7 domain files, same package
- `time-parsing`: All time parsing goes through canonical `ParseTimeFlex`
- `home-directory`: Single `core.HomeDir()` function, no duplicates
- `notifications-ttl`: Expired rows properly excluded from future scans
- `feed-producers`: Context-aware, atomic writes, deadline-bounded git calls
- `analytics-sql`: Guard block query as typed method, dead code removed
- `feed-test-coverage`: Error paths and envelope structure tested per producer

## Impact

- **No API changes**: All modifications are internal (same package, same exports)
- **No behavioral changes**: ARCH-1 fail-open preserved, contract tests unchanged
- **Test coverage increase**: Feed producers gain error path + envelope tests
- **Performance**: Insights producer stops materializing full usage tree; project producer gains git timeout
- **Reliability**: Calendar token writes become atomic; expired notifications stop accumulating
