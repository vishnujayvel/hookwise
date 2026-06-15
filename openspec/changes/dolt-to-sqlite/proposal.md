# Proposal: Replace Dolt with embedded SQLite + periodic snapshots

## Status
Draft — 2026-06-07

## Summary
Replace the Dolt embedded data layer in hookwise with pure-Go SQLite
(`modernc.org/sqlite`, already a dependency), plus a periodic file-snapshot
mechanism that preserves the existing `hookwise diff` and `hookwise log`
commands. No historical data is migrated: fresh tables are created on first run
and any existing Dolt data directory is archived.

## Problem
Hookwise carries Dolt ("Git for data") — a 137 MB SQL engine pulling 12
`dolthub/*` modules — but uses **none** of Dolt's differentiating features:

- **Used:** `DOLT_COMMIT` (auto-commit after each dispatch), `dolt_diff_summary`
  (via `hookwise diff`), `dolt_log` (via `hookwise log`).
- **Never used:** branch, merge, checkout, reset, time-travel (`AS OF`),
  row-level history. The promised "rollback" was never implemented.

The data is append-only, low-stakes, regenerable telemetry (sessions, events,
cost, coaching state, notifications, feed cache). Dolt's weight is the direct
cause of two incidents:

- **retro-009** — 642 zombie `hookwise.test` processes (149 MB each, Dolt
  embedded) exhausted 95.7 GB on a 24 GB machine → kernel panic.
- **retro-010** — chronic "Dolt Tax": every hook invocation forks a 144 MB
  binary (~59 MB RSS) for work that needs ~2 MB.

Dolt is also the *sole* reason for two architecture constraints
(ARCH-2 serialized writes "Dolt embedded is single-threaded"; ARCH-3
daemon-writes-cache to dodge Dolt's filesystem lock) and much of the motivation
for the in-flight daemon-dispatch effort.

## Goals
- Remove all `dolthub/*` dependencies; back `analytics.DB` with `modernc.org/sqlite`.
- Collapse binary from ~144 MB toward < 20 MB; enable `CGO_ENABLED=0` builds.
- Preserve the full existing feature set, including `hookwise diff`/`log`.
- Keep tests green, run only via Dagger or `task test` (never bare `go test ./...`).

## Non-Goals
- Migrating historical Dolt data (start fresh; archive old dir).
- Merging the daemon-dispatch branch (reassess separately after Dolt is gone).
- Touching the 10 open CodeRabbit threads on PR #73, the 4 TUI bug issues, or
  the 19 enhancement issues.

## Approach (B): periodic file snapshots
- A `hookwise snapshot` command + a daemon-scheduled interval run
  `VACUUM INTO ~/.hookwise/snapshots/<RFC3339>.db`, with a retention cap
  (keep last N).
- `hookwise log` lists snapshots (the Dolt-commit analog).
- `hookwise diff <a> <b>` opens two snapshot DBs and compares per-table
  aggregates / row counts.

See `design.md` for the full design.

## Risks
- **Parity drift** in `diff`/`log` output — mitigated by the ARCH-6 contract suite.
- **Snapshot disk growth** — mitigated by the retention cap.
- **SQL dialect gaps** — schema is standard CRUD; translation rules in design.md.
