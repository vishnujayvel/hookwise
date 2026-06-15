## Context

Part 1 (PR #72) completed architecture-v2 batches A-F. This continues with G-L plus CodeRabbit findings.

## Goals

1. Complete the architecture-v2 spec (all 14 tasks)
2. Address 5 actionable CodeRabbit findings from PR #72
3. Maintain all ARCH constraints and contract test parity
4. Increase feed producer test coverage to 1.2:1 test-to-source LOC ratio

## Non-Goals

- Changing the Dolt embedded dependency or binary size
- Modifying the Python TUI
- Adding new features beyond what the spec defines

## Design Decisions

### D1: Types Split Strategy (Batch H)

Split `internal/core/types.go` into 7 files by domain. All files stay in package `core` — no export changes, no import changes for consumers.

| File | Contents |
|------|----------|
| `types.go` | Event type constants + `ValidEventType()` (kept as anchor) |
| `config_types.go` | All config structs (HookwiseConfig, GuardConfig, FeedConfig, etc.) |
| `dispatch_types.go` | DispatchResult, HandlerResult, HookPayload |
| `guard_types.go` | GuardRuleConfig, GuardResult, ParsedCondition |
| `feed_types.go` | FeedCacheEntry, FeedDefinition, feed platform types |
| `analytics_types.go` | AnalyticsEvent, SessionSummary, AuthorshipSummary |
| `handler_types.go` | Handler/segment types |

### D2: Time Parsing Consolidation (Batch I)

Replace all ad-hoc `time.Parse()` calls with `ParseTimeFlex()`. Already partially done in part 1 (RFC3339Nano added). Remaining calls in:
- `internal/core/warnings.go` (2 calls)
- `cmd/hookwise/cmd_doctor.go` (1 call)
- `cmd/hookwise/cmd_status_line.go` (1 call)
- `internal/feeds/producer_insights.go` (6 calls with RFC3339 + Nano fallback patterns)

### D3: Guard Block SQL Extraction (Batch J)

Move `queryGuardBlocks()` from `internal/notifications/producers.go` to a new typed method on `analytics.DB`. The notifications package then calls the analytics method instead of embedding raw SQL that references analytics table schemas.

### D4: CodeRabbit Findings Integration

Interleave CodeRabbit fixes into the batches that touch the same files:
- **Batch I** (touches producers): Add git deadline to project producer, honor ctx in insights producer, atomic token write in calendar producer
- **Batch J** (touches notifications + analytics): Fix expired notifications rescan, narrow ALTER TABLE error handling

### D5: Retro-009 Compliance

All test execution MUST use `-p 2` and `GOMEMLIMIT=4GiB`. Check `mcp-monitor get_memory_info` before running tests. Critics review code only — no test re-runs.
