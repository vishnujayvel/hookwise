# Tasks: Cost-Tracking Writer

All test execution is funneled through `task test` or `dagger call ci --src=.`.
**Never bare `go test ./...`** (retro-009 resource safety).

## Phase 1 — Pricing layer (PR #100 — merged)

- [x] 1.1 Create `internal/pricing/pricing.go`: rate table for opus/sonnet/haiku
      at $/MTok (input, output, cache-read, cache-write).
- [x] 1.2 Implement `Compute(model string, u Usage) float64` and
      `ComputeWithRates(model string, u Usage, overrides map[string]ModelRates) float64`.
- [x] 1.3 Unit tests: all model/tier combinations, custom rate overrides, zero usage.
- [x] 1.4 **Validate**: `task test` passes with `-race -p 2` — confirmed 2026-06-15.
- [x] 1.5 Open and merge PR #100.

## Phase 2 — Transcript reader (PR #101 — merged)

- [x] 2.1 Create `internal/transcript/transcript.go`: `SumUsage(path string)`
      reads `.jsonl`, sums per-model `usage` blocks from assistant messages.
- [x] 2.2 Handle edge cases: missing file → empty map (not error); malformed lines
      skipped; partial last line tolerated.
- [x] 2.3 Unit tests with `testdata/` fixtures: valid multi-model, partial file,
      empty file, non-assistant-role lines, malformed JSON.
- [x] 2.4 **Validate**: `task test` passes with `-race -p 2` — confirmed 2026-06-15.
- [x] 2.5 Open and merge PR #101.

## Phase 3 — Fork bomb guard (PR #102 — merged)

- [x] 3.1 Add `.test` suffix check to `spawnDaemon` in
      `cmd/hookwise/cmd_dispatch.go` (or daemon start path).
- [x] 3.2 Add `HOOKWISE_DISABLE_DAEMON_AUTOSTART=1` env var check.
- [x] 3.3 Unit test: verify `spawnDaemon` returns `nil` without spawning when
      either guard triggers.
- [x] 3.4 Propagate `HOOKWISE_DISABLE_DAEMON_AUTOSTART=1` in existing test helpers
      (`cli_test.go`) to prevent any future re-trigger.
- [x] 3.5 **Validate**: guarded gate (`task test`) passes with 0 `hookwise.test`
      zombie processes — confirmed 2026-06-15.
- [x] 3.6 Open and merge PR #102.

## Phase 4 — Dispatch wiring (PR #103 — MERGED)

- [x] 4.1 Add `TranscriptPath string \`json:"transcript_path"\`` to `HookPayload`
      in `internal/core/types.go` (additive, zero-value safe).
- [x] 4.2 In `cmd/hookwise/cmd_dispatch.go`, on the `Stop`/`SessionEnd` seam:
      - Call `transcript.SumUsage(payload.TranscriptPath)`
      - Call `pricing.ComputeWithRates(model, usage, cfg.Rates)` for each model in the map
      - Compute delta against `SessionCosts[session_id]` (in cost_state)
      - Update `cost_state.TotalToday`/`DailyCosts` and call `EndSession(EstimatedCostUSD)`
      - All I/O fail-open (ARCH-1); writes off the per-tool hot path (Stop only)
- [x] 4.3 `cmd_dispatch_cost_test.go`: inject real transcript fixture, fire Stop,
      assert `cost_state.TotalToday > 0`; fire second Stop, assert no double-count.
- [x] 4.4 Update `cli_test.go` for the new `recordAnalytics` signature.
- [x] 4.5 **Validate**: guarded gate green (`-race -p 2`, 0 zombies); CI pipeline green.
- [x] 4.6 Merge PR #103.

## Phase 5 — End-to-end verification

- [x] 5.1 Ran `hookwise dispatch Stop` with a seeded transcript (sonnet 1M+1M);
      `hookwise stats --data-dir <db>` shows **`Est. cost: $18.00`**; `cost_state`
      row = `total_today=18.0, session_costs={"verify1":18}`, `sessions.estimated_cost_usd=18.0`.
- [ ] 5.2 Verify `cost` status-line segment renders a dollar amount (not `$0`).
      BLOCKED by #105 — `stats`/status-line default to `~/.hookwise/analytics.db`,
      ignoring `analytics.db_path`; reader/writer must agree first.
- [ ] 5.3 Verify budget notification (#97 wiring) fires live on a real cost > budget
      (path is wired and reachable now that cost is non-zero; not yet live-fired).
- [x] 5.4 Confirm guarded gate is green with `pgrep -f hookwise.test` returning 0.

## Resource-safety invariants (apply to every phase)
- Never bare `go test ./...`. `dagger call ci` or `task test` only.
- `pgrep -f hookwise.test` before any host-side test run; kill zombies first.
- One test run at a time; no concurrent test execution across agents.
