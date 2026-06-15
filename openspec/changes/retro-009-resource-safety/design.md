## Context

On 2026-03-09, 642 hookwise.test processes consumed 93.5 GB and crashed the system. See retro-009.

## Goals

1. Prevent test process accumulation from ever exhausting system memory again
2. Give AI agents awareness of machine state (proprioception) before spawning heavy processes
3. Make the safety constraints self-documenting and testable, not fragile env var hacks
4. Support both macOS (primary dev) and Linux (CI) environments

## Non-Goals

- Reducing Dolt test binary size (structural, requires upstream changes)
- Modifying hookwise dispatch behavior (ARCH-1 fail-open must be preserved)
- Adding real-time memory monitoring to the hookwise daemon

## Design Decisions

### D1: Two-Layer Defense Architecture

**Layer 1 (Soft — MCP):** Agent queries `mcp-monitor` `get_memory_info` before deciding to test. This is advisory — the agent can choose to proceed or skip. It provides _proprioception_ — the agent can "feel" the machine.

**Layer 2 (Hard — Script):** `scripts/pre-test-resource-check.sh` runs as a Taskfile precondition. It blocks test execution (exit 1) if resources are insufficient. This is a hard gate — cannot be bypassed through the Taskfile.

**Why two layers:** Layer 1 prevents the agent from even attempting tests when resources are low. Layer 2 catches cases where Layer 1 is skipped (direct `go test` invocation, subagents that don't check MCP, etc.).

### D2: MB-Based Thresholds (Not GB)

Thresholds use megabytes internally to avoid integer truncation. 3.9 GB truncates to 3 GB in bash integer arithmetic, causing false blocks. Using 3993 MB is accurate.

| Threshold | Default | Purpose |
|-----------|---------|---------|
| `HOOKWISE_MIN_FREE_MB` | 2048 | Block below this |
| `HOOKWISE_WARN_FREE_MB` | 4096 | Warn below this |
| `HOOKWISE_MAX_TEST_PROCS` | 0 | Max existing test processes |

### D3: Testable Functions with Mock Injection

The resource check script exposes two testable functions (`count_test_procs`, `get_available_memory_mb`) that can be overridden via `HOOKWISE_TEST_MODE=1` with `HOOKWISE_MOCK_PROC_COUNT` and `HOOKWISE_MOCK_AVAIL_MB`. This makes the test suite deterministic (14 tests) without depending on actual system state.

### D4: Cross-Platform Memory Detection

macOS uses `vm_stat` (free + inactive pages × page_size). Linux uses `/proc/meminfo` (MemAvailable). Unknown platforms skip the memory check with a warning (fail-open for CI environments that may not have either).

### D5: Process Detection Without Self-Match

Uses `ps aux | grep '[h]ookwise\.test'` instead of `pgrep -f hookwise.test`. The `[h]` bracket trick prevents the grep process from matching itself. `pgrep` also returns exit 1 when no matches, which conflicts with `set -e`.
