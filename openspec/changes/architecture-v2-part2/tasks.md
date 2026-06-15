## Tasks

### Batch G: Types Split (Task 8.1, 8.2)
- [ ] Read `internal/core/types.go` and identify domain boundaries
- [ ] Create `config_types.go` — move all config structs
- [ ] Create `dispatch_types.go` — move DispatchResult, HandlerResult, HookPayload
- [ ] Create `guard_types.go` — move GuardRuleConfig, GuardResult, ParsedCondition
- [ ] Create `feed_types.go` — move feed platform types
- [ ] Create `analytics_types.go` — move analytics event types
- [ ] Create `handler_types.go` — move handler/segment types
- [ ] Keep event type constants + ValidEventType in `types.go`
- [ ] Verify: `go vet ./...` passes
- [ ] Verify: `GOMEMLIMIT=4GiB go test -race -p 2 ./internal/core/...` passes

### Batch H: Time Parsing + HomeDir + CodeRabbit Producer Fixes (Tasks 9.1-9.2, 10.1-10.2 + CR findings)
- [ ] Replace ad-hoc `time.Parse` in `internal/core/warnings.go` with `ParseTimeFlex`
- [ ] Replace ad-hoc `time.Parse` in `cmd/hookwise/cmd_doctor.go` with `ParseTimeFlex`
- [ ] Replace ad-hoc `time.Parse` in `cmd/hookwise/cmd_status_line.go` with `ParseTimeFlex`
- [ ] Replace ad-hoc `time.Parse` in `internal/feeds/producer_insights.go` (6 calls) with `ParseTimeFlex`
- [ ] Export `homeDir()` → `HomeDir()` in `internal/core/constants.go`
- [ ] Replace `os.UserHomeDir()` in `internal/analytics/dolt.go` with `core.HomeDir()`
- [ ] Replace `hookwiseDir()` in `internal/migration/migration.go` with `core.HomeDir()`
- [ ] Replace `os.UserHomeDir()` in `internal/feeds/producer_insights.go` with `core.HomeDir()`
- [ ] CR: Add `context.WithTimeout` (30s) before git calls in `producer_project.go`
- [ ] CR: Honor incoming `ctx` in `producer_insights.go` Produce method
- [ ] CR: Use `core.AtomicWriteJSON` for token write-back in `producer_calendar.go`
- [ ] Verify: `go vet ./...` passes
- [ ] Verify: `GOMEMLIMIT=4GiB go test -race -p 2 ./...` passes

### Batch I: SQL Extraction + Dead Code + CodeRabbit Notification Fixes (Tasks 11.1-11.2, 13.1 + CR findings)
- [ ] Create typed `GuardBlockSummary` method on `analytics.DB`
- [ ] Move guard block SQL query from `notifications/producers.go` to analytics method
- [ ] Update notifications to call analytics method instead of raw SQL
- [ ] Delete `WriteFeedCacheJSON` and `WriteFeedCacheJSONTo` from `analytics/state.go`
- [ ] Delete associated tests for dead code
- [ ] CR: Narrow ALTER TABLE error handling in `dolt.go` — only ignore "duplicate column" errors
- [ ] CR: Mark expired notifications as surfaced in `Unsurfaced()` query to prevent rescan
- [ ] Verify: `go vet ./...` passes
- [ ] Verify: no raw SQL referencing analytics tables remains in notifications package

### Batch J: Feed Producer Test Coverage (Task 12.1)
- [ ] Add error path tests for each producer (API failure → fallback without error)
- [ ] Add envelope structure tests (exactly 3 keys: type, timestamp, data; no "source" key)
- [ ] Add malformed response handling tests
- [ ] Add missing configuration handling tests
- [ ] Target 1.2:1 test-to-source LOC ratio for feeds package
- [ ] Verify: `GOMEMLIMIT=4GiB go test -race -p 2 ./internal/feeds/...` passes

### Batch K: Final Integration Verification (Task 14.1)
- [ ] `go vet ./...` — all packages pass
- [ ] `GOMEMLIMIT=4GiB go test -race -p 2 ./...` — all tests pass
- [ ] Contract tests (33 fixtures in testdata/contracts/) pass unchanged
- [ ] Architecture tests (ARCH-3, no circular deps) pass
- [ ] Mutation tests maintain >= 93% kill rate
- [ ] `hookwise doctor` succeeds on installed binary
- [ ] `hookwise status-line` renders correctly

## Execution Order

G → H → I → J → K (sequential, each batch depends on previous for types/imports)

## Resource Safety (retro-009)

- ALL `go test` commands MUST use `-p 2` and `GOMEMLIMIT=4GiB`
- Check `mcp-monitor get_memory_info` before test runs
- Run `scripts/pre-test-resource-check.sh` before test runs
- NO parallel test execution across batches
