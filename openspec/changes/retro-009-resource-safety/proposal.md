## Why

During PDLC autopilot execution of the architecture-v2 spec, 642 zombie `hookwise.test` processes accumulated from uncoordinated subagent test runs, consuming 93.5 GB of virtual memory on a 24 GB MacBook Air. This caused a kernel panic, WindowServer death, and full system restart. The root cause is a process-level coordination gap: each Dolt-embedded test binary is ~149 MB resident (~300 MB with `-race`), and the default `go test ./...` runs 10+ packages simultaneously. Multiple agents running tests independently with no backpressure created unbounded memory fan-out.

Full forensics in `.claude/specs/retro/retro-009-system-crash-memory-exhaustion.md`.

## What Changes

- **Taskfile test parallelism capped** (`-p 2`) across all Go test tasks to limit concurrent test binaries
- **Go runtime memory capped** (`GOMEMLIMIT=4GiB`) to prevent individual test processes from growing unbounded
- **Pre-test resource guard script** blocks test execution if hookwise.test processes already exist or system memory is critically low
- **System monitoring MCP server** (seekrays/mcp-monitor) gives AI agents awareness of machine state before spawning heavy processes
- **CLAUDE.md resource safety section** documents constraints so all agents (including subagents) follow them
- **Emergency cleanup tasks** (`kill:tests`, `clean:worktrees`) for manual intervention

## Capabilities

### New Capabilities
- `test-parallelism-guard`: Taskfile enforces `-p 2` and `GOMEMLIMIT=4GiB` on all Go test invocations via centralized variables
- `resource-check-script`: Bash script with testable functions that checks process count + memory pressure before allowing test execution
- `mcp-system-monitor`: seekrays/mcp-monitor MCP server providing `get_memory_info` and `get_process_info` to AI agents

### Modified Capabilities
- `Taskfile.yml`: All 7 Go test tasks gain `test:guard` dependency, `-p {{.TEST_PARALLEL}}`, and `GOMEMLIMIT` env
- `CLAUDE.md`: New "Resource Safety (retro-009)" section with mandatory constraints for agents

## Impact

- **Test execution**: 2 packages concurrent instead of 10+ (memory usage drops from 4.5 GB to ~600 MB per `go test` invocation)
- **Agent behavior**: Subagents that read CLAUDE.md will see the `-p 2` constraint; MCP monitor provides pre-check capability
- **CI/CD**: Linux compatibility via `/proc/meminfo` fallback in resource check script
- **No behavioral changes**: hookwise dispatch, guards, feeds, status-line all unchanged (ARCH-1 preserved)
