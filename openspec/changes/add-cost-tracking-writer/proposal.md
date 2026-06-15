# Proposal: Cost-Tracking Writer — Per-Session Cost Computation and Accumulation

## Status
In progress — 2026-06-15

## Summary
Port the missing *writer* side of cost tracking. The TS→Go rewrite ported cost
*readers* (`stats`, the `cost` status-line segment, `notifications.CheckBudget`)
but never the writer that populates those values. As a result, `cost_state.TotalToday`
and `sessions.estimated_cost_usd` were always 0, so cost displayed `$0` everywhere
and the budget notification fired on every session regardless of spend (or never,
depending on the comparison direction). This is a silent, always-wrong data gap.

## Problem

Claude Code hook payloads (`PreToolUse`, `PostToolUse`, `Stop`) do **not** carry
token counts — the hook envelope omits usage data entirely. The only reliable source
of truth is the conversation transcript `.jsonl`, where each assistant message
carries a `usage` block with `input_tokens`, `output_tokens`, `cache_read_input_tokens`,
and `cache_creation_input_tokens`.

An additional compounding bug: `HookPayload` was silently dropping the
`transcript_path` field because its `Extra` map is tagged `json:"-"`, so the path
was available in the raw stdin JSON but never reached Go code.

With no path and no token data, no amount of writer logic could have computed a
non-zero cost. Both gaps had to be fixed together.

## Goals

1. Add a pure per-model pricing layer (`internal/pricing`) decoupled from I/O.
2. Add a transcript reader (`internal/transcript`) that extracts per-model token
   usage from the `.jsonl` file produced by Claude Code.
3. Wire pricing + transcript reading into the dispatch `Stop`/`SessionEnd` seam,
   idempotently accumulating cost into `cost_state` and `sessions`.
4. Fix the `HookPayload.transcript_path` drop so the path reaches the writer.
5. Fix the daemon auto-start fork bomb (re-exec of `.test` binary = recursive
   test suite, the retro-009 mechanism reproduced in test isolation).
6. Keep ARCH-1 (fail-open), ARCH-2 (single-writer), and ARCH-7 (non-blocking).

## Non-Goals

- Cost *enforcement* (block/warn when budget is hit) — config keys exist but
  logic is unimplemented; tracked as a follow-up.
- `coaching_state` and `insights` writer ports — same readers-not-writers class,
  tracked in WRITER AUDIT #99.
- Doctor test-isolation leak — tracked separately as #104.
- Migrating historical cost data.

## Design

See `design.md` for full rationale. Three layers, each independently mergeable:

### Layer 1 — `internal/pricing` (PR #100)
Pure function: rate table (opus / sonnet / haiku at $/MTok for input, output,
cache-read, cache-write) → `Compute(model, usage) float64`. No I/O, no DB,
no side effects. Honors `CostTrackingConfig.Rates` overrides for custom model
rates. Zero hot-path risk.

### Layer 2 — `internal/transcript` (PR #101)
`SumUsage(path string) (map[string]pricing.Usage, error)` — reads the transcript
`.jsonl` line by line, skips malformed/non-assistant entries, sums `usage` blocks
per model. Tolerant of partial writes (truncated last line). Returns an empty map
(not an error) on missing or empty file.

### Layer 3 — Dispatch wiring (PR #103)
- **`HookPayload`** gains an exported `TranscriptPath` field (additive, zero-value
  safe) so `cmd_dispatch.go` can read it from stdin.
- **`cmd_dispatch.go`** fires the writer on the `Stop`/`SessionEnd` seam only —
  once per conversation turn, NOT on every `PostToolUse` event. This prevents
  per-tool-use transcript reads (the transcript is written by Claude Code after
  each full turn, not after each tool call, so reading mid-turn is both wrong and
  expensive).
- **Idempotent accumulation**: delta = sessionCost − SessionCosts[session\_id].
  If `Stop` fires twice for the same session (e.g., reconnect), the second call
  subtracts the already-recorded cost before adding the new total. No
  double-counting.
- `EndSession` receives `EstimatedCostUSD` and persists it to
  `sessions.estimated_cost_usd`.

### Safety fix — fork bomb guard (PR #102)
`status-line` auto-starts the daemon by re-exec'ing `os.Executable()`. Under
`go test`, `os.Executable()` resolves to the `<pkg>.test` binary, so `spawnDaemon`
was re-executing the test suite recursively. This reproduced the retro-009
kernel-panic mechanism and was the true root cause of issue #84 (~1,900 self-spawning
processes growing ~5/sec, consuming 2/3 of all machine processes).

Fix: `spawnDaemon` refuses to proceed when `os.Executable()` ends in `.test`
or when `HOOKWISE_DISABLE_DAEMON_AUTOSTART=1` is set.

## Testing Strategy

- `internal/pricing`: table-driven unit tests covering all model/tier combinations,
  custom rate overrides, zero-usage input.
- `internal/transcript`: unit tests with real `.jsonl` fixture files (valid, partial,
  multi-model, malformed lines); verified with `-race -p 2`.
- Dispatch wiring: existing `cmd_dispatch_cost_test.go` — verifies Stop event
  produces non-zero cost with a real transcript fixture; verifies idempotency
  (second Stop does not double the accumulated value).
- Fork-bomb guard: unit test verifies `spawnDaemon` exits early on `.test` binary
  path and on env var override.
- All tests run via `task test` or `dagger call ci` — never bare `go test ./...`
  (retro-009 safety rule).

## Acceptance Criteria

- [x] `internal/pricing` computes correct USD cost per model/usage combination
- [x] `internal/transcript` reads `.jsonl` and returns per-model token sums
- [ ] `cost_state.TotalToday` and `sessions.estimated_cost_usd` are non-zero
      after a `Stop` event with a real transcript (in-flight: PR #103)
- [ ] A second `Stop` for the same session does not double the cost (idempotent)
- [ ] `cost` status-line segment and `stats` command display real dollar amounts
- [ ] Budget notification (PR #97 wiring) is able to fire once cost is non-zero
- [x] `spawnDaemon` fork-bomb guard in place; guarded gate passes with 0 zombie
      processes (PR #102, merged)
- [ ] Guarded gate green end-to-end with PRs #100–#103 merged

## Scope

### Files modified
- `internal/core/types.go` — add `TranscriptPath` to `HookPayload`
- `cmd/hookwise/cmd_dispatch.go` — wire Stop seam: transcript read + cost accumulate
- `cmd/hookwise/cli_test.go` — update dispatch tests for new HookPayload field

### Files created
- `internal/pricing/pricing.go` — rate table + Compute function
- `internal/pricing/pricing_test.go`
- `internal/transcript/transcript.go` — SumUsage reader
- `internal/transcript/transcript_test.go`
- `cmd/hookwise/cmd_dispatch_cost_test.go` — cost accumulation integration test
