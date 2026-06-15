# Design: Cost-Tracking Writer

## Context

The hookwise analytics schema has had `cost_state` and `sessions.estimated_cost_usd`
since the architecture-v2 rewrite. Every query that surfaces cost (the `cost` segment,
`stats`, `notifications.CheckBudget`) reads from those columns. But nothing ever wrote
to them: the original TypeScript writer was not ported to Go.

The recon work that preceded this change revealed two separate reasons why a naive writer
would have been silently wrong even if it existed:

1. **Hook payloads carry no token data.** Claude Code sends `PreToolUse`,
   `PostToolUse`, and `Stop` events to `hookwise dispatch` via stdin. None of these
   payloads include `usage` (input/output/cache tokens). The only place token data
   lives is in the conversation transcript `.jsonl`.

2. **`transcript_path` was silently dropped.** `HookPayload.Extra` is tagged
   `json:"-"`, which means any unknown fields in the stdin JSON (including
   `transcript_path`) were parsed and discarded. The path was present in the wire
   data but never reachable by Go code.

Both bugs had to be fixed before a writer could produce correct values.

## Goals

1. Compute cost at the `Stop`/`SessionEnd` boundary (once per turn, not per tool call).
2. Accumulate idempotently into `cost_state` so repeated `Stop` events (reconnect,
   double-fire) do not double-count.
3. Keep all three ARCH constraints: fail-open (ARCH-1), single-writer (ARCH-2),
   non-blocking side effects (ARCH-7).
4. Zero hot-path impact: cost computation runs only on `Stop`, not on the high-frequency
   `PreToolUse`/`PostToolUse` events that fire on every tool call.

## Non-Goals

- Cost enforcement (block/warn at budget) — the `MaxTodayUSD` config key exists
  but action is unimplemented.
- Historical cost backfill.
- `coaching_state` / `insights` writer ports (same class of problem, WRITER AUDIT #99).

## Design Decisions

### D1: Three Independent Layers

**Why:** Isolating pricing, I/O, and wiring into three PRs lets each be reviewed,
tested, and merged independently. The pricing layer has zero I/O risk; the transcript
reader has no DB risk; only the wiring PR touches the hot dispatch path.

**Layer 1 — `internal/pricing`:** Pure function mapping model + token counts to USD.
No imports from `internal/*`. Testable in total isolation. Rate table covers the three
Claude families (opus / sonnet / haiku) at four token tiers (input, output, cache-read,
cache-write). `CostTrackingConfig.Rates` overrides apply per-model to support custom
pricing and third-party proxies.

**Layer 2 — `internal/transcript`:** `SumUsage(path) → map[model]pricing.Usage`.
Reads the `.jsonl` line by line. Each line that is a valid JSON object with
`role:"assistant"` and a `usage` block contributes to the per-model sum. Malformed
lines are skipped (no error returned). Missing file returns empty map, not error
(fail-open for sessions with no transcript yet).

**Layer 3 — Dispatch wiring:** The `Stop` handler in `cmd_dispatch.go` is the right
seam: it fires once per conversation turn, after Claude has finished, and the transcript
is complete. `PostToolUse` is the wrong seam — it fires after each individual tool call
mid-turn, when the transcript is still being written.

### D2: Idempotent Delta Accumulation

```
delta = sessionCost(transcript) − SessionCosts[session_id]
cost_state.TotalToday += delta
SessionCosts[session_id] = sessionCost(transcript)
```

`SessionCosts` is an in-process map (not persisted). On first `Stop` for a session,
`SessionCosts[sid]` is zero, so delta = full cost. On a second `Stop` (reconnect,
crash-restart with same session ID), delta = new_total − old_total, which is
approximately 0 if nothing changed. This prevents double-counting without requiring
a DB read on every `Stop`.

**Trade-off:** The in-process map is lost on binary restart. A restart between two
`Stop` events for the same session would double-count the cost of that session. This is
an acceptable trade-off for a first implementation: sessions are short-lived (minutes),
restarts during a session are rare, and the cost data is advisory telemetry rather than
billing. A persistent `SessionCosts` map (in `cost_state` table) is a follow-up.

### D3: Stop Seam, Not PostToolUse

The transcript `.jsonl` is written incrementally by Claude Code as the conversation
progresses. Mid-turn (during `PostToolUse` events), the file exists but may be
partially written for the current turn. Reading it during `PostToolUse` would produce
undercounts (missing the current turn's tokens) and cause unnecessary I/O on every
tool call.

The `Stop` event fires after Claude has finished responding for the turn. The transcript
is complete at this point. This is the correct and only correct seam for transcript-based
cost computation.

### D4: `HookPayload.TranscriptPath` as Additive Field

Adding `TranscriptPath string` (exported, `json:"transcript_path"`) to `HookPayload`
is additive — zero-value safe, backward-compatible, requires no migration. The field
was already present in the Claude Code wire format; this change just stops discarding it.

The `Extra map[string]any` (tagged `json:"-"`) is intentionally unexported for other
unknown fields — this pattern is preserved. `transcript_path` is the only field
promoted because it is the only one needed by the cost writer.

### D5: Fork Bomb Guard (PR #102)

`status-line` calls `spawnDaemon()` to ensure the daemon is running. `spawnDaemon`
uses `os.Executable()` to re-exec the binary. Under `go test`, `os.Executable()`
returns the test binary path (e.g., `hookwise.test`). Re-execing a test binary
restarts the test suite — which again runs `status-line` — recursively, until the
machine runs out of process slots.

This is the same mechanism as retro-009 (zombie process accumulation → kernel panic)
and was confirmed as the root cause of issue #84 (observed ~1,900 `hookwise.test`
processes at 2/3 machine process capacity, growing ~5/sec).

**Fix:** Two guards in `spawnDaemon`:
- `strings.HasSuffix(exe, ".test")` — refuses if running as a test binary.
- `os.Getenv("HOOKWISE_DISABLE_DAEMON_AUTOSTART") == "1"` — explicit opt-out for
  test harnesses that can't control the binary name.

Both are checked before any process spawn. ARCH-1 is honored: the function returns
`nil` (allow dispatch to proceed) rather than an error.

## Testing Strategy

All test execution via `task test` or `dagger call ci` — never bare `go test ./...`.

- **pricing**: table-driven, deterministic. No fixtures needed.
- **transcript**: real `.jsonl` fixture files in `testdata/` covering valid, partial,
  multi-model, empty, and malformed inputs. All checked with `-race`.
- **dispatch cost**: `cmd_dispatch_cost_test.go` — injects a real transcript fixture,
  fires a synthetic `Stop` event, asserts `cost_state.TotalToday > 0` after the call,
  and fires a second `Stop` to verify idempotency.
- **fork bomb guard**: unit test in the dispatch package verifying `spawnDaemon` early-
  returns when the `.test` suffix guard triggers and when the env var is set.

## Relation to Other Specs

| Spec | Relation |
|------|----------|
| retro-009-resource-safety | Fork bomb guard is the retro-009 mechanism re-triggered in test isolation; PR #102 is a targeted remediation |
| dolt-to-sqlite | `cost_state` schema carries over unchanged; no Dolt references remain |
| daemon-dispatch | Independent; cost writer fires in the existing `cmd_dispatch.go` path (not the daemon socket path) |
