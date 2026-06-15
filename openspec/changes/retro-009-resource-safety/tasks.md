## Tasks

### 1. Taskfile Test Parallelism (M1 + M5)
- [x] Add `TEST_PARALLEL: "2"` and `GOMEMLIMIT: 4GiB` vars to Taskfile.yml
- [x] Add `-p {{.TEST_PARALLEL}}` to all 7 Go test task commands
- [x] Add `GOMEMLIMIT` env block to all 7 Go test tasks
- [x] **Validate**: `go vet ./...` passes (exit 0) — confirmed 2026-03-18
- [x] **Validate**: `go test -race -p 2 -v ./internal/core/...` passes with GOMEMLIMIT=4GiB — confirmed 2026-03-18

### 2. Pre-Test Resource Guard Script (M2 + M6)
- [x] Create `scripts/pre-test-resource-check.sh` with process count + memory checks
- [x] Fix `set -e` issue (pgrep exit 1 on no match)
- [x] Fix self-matching grep (use `[h]ookwise\.test` trick)
- [x] Fix integer truncation (use MB not GB)
- [x] Add Linux `/proc/meminfo` fallback
- [x] Add `HOOKWISE_TEST_MODE=1` mock injection for testability
- [x] Wire into Taskfile as `test:guard` task
- [x] Create `scripts/test-resource-check.sh` with 14 test cases
- [x] All 14 tests pass — confirmed 2026-03-18
- [x] **Validate**: Test suite passes in clean shell — confirmed 2026-03-18 (14/14)
- [x] **Validate**: Real system check exits 0 with valid output — confirmed 2026-03-18 (4362 MB free)

### 3. MCP System Monitor (M13)
- [x] Clone and build seekrays/mcp-monitor (Go binary, 9.4 MB)
- [x] Register via `claude mcp add mcp-monitor -s user`
- [x] Add `mcp__mcp-monitor__*` permission to global settings
- [x] **Validate**: MCP get_memory_info returns real data (24 GB total, 3.6 GB avail) — confirmed 2026-03-18
- [x] **Validate**: MCP get_process_info returns real data (1008 processes) — confirmed 2026-03-18
- [x] **Validate**: MCP accessible as live tool — `get_memory_info` returns 3.5 GB avail/24 GB total, `get_process_info` returns 952 procs, 0 hookwise.test — confirmed 2026-03-18

### 4. CLAUDE.md Documentation (M3 + M5)
- [x] Add "Resource Safety (retro-009)" section with `-p 2`, GOMEMLIMIT, critic no-test rule
- [x] **Validate**: CLAUDE.md loaded by Claude Code this session without errors — confirmed 2026-03-18

### 5. Emergency Cleanup Tasks (M4)
- [x] Add `kill:tests` task to Taskfile
- [x] Add `clean:worktrees` task to Taskfile
- [x] **Validate**: Taskfile YAML parses correctly — confirmed 2026-03-18

### 6. Retro-009 Status Update
- [x] Mark M1-M6, M13 as DONE in retro-009 mitigations table
- [x] Add Resource Profile section with measured memory values
- [x] Add MCP recommendation section
- [x] Add First-Principles Analysis (7 principles)
- [x] **Validate**: All marked-DONE items have actual implementations — confirmed 2026-03-18

### 7. Integration Verification
- [x] `go vet ./...` — all packages pass (exit 0) — confirmed 2026-03-18
- [x] `GOMEMLIMIT=4GiB go test -race -p 2 ./internal/core/...` — passes (11 subtests) — confirmed 2026-03-18
- [x] `bash scripts/test-resource-check.sh` — 14/14 pass — confirmed 2026-03-18
- [x] `bash scripts/pre-test-resource-check.sh` — exits 0, reports 4362 MB free — confirmed 2026-03-18
- [x] hookwise dispatch PreToolUse exits 0, status-line renders — confirmed 2026-03-18

## All Tasks Complete
- [x] All validation tasks passed — confirmed 2026-03-18
- [x] MCP monitor live-validated via tool call in same session (not just assumed)
