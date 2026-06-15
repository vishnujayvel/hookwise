# Design: Dolt → SQLite + periodic snapshots

## 1. Architecture overview

`internal/analytics` exposes a `*DB` type with `Exec`/`Query` semantics. Eleven
files consume that abstraction; **only** `internal/analytics/dolt.go` imports the
Dolt driver. The migration swaps the backend behind `analytics.DB` and leaves the
eleven consumers untouched, except `cmd_diff.go`/`cmd_log.go`, which switch from
Dolt VC calls to the snapshot mechanism.

```
cmd/* , notifications, producers ──> analytics.DB (unchanged interface)
                                          │
                              ┌───────────┴───────────┐
                         (before) Dolt          (after) modernc/sqlite
                                                       │
                                          ~/.hookwise/analytics.db (WAL)
                                          ~/.hookwise/snapshots/<ts>.db
```

## 2. Backend swap

- Open `modernc.org/sqlite` at `~/.hookwise/analytics.db` in **WAL mode**
  (`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA busy_timeout=5000`).
- **ARCH-2 relaxation:** the `SetMaxOpenConns(1)` serialization existed because
  "Dolt embedded is single-threaded." WAL permits concurrent readers + one
  writer. Keep a single writer connection for safety but allow a read pool.
  Update the ARCH-2 note to record this is now a deliberate choice, not a Dolt
  constraint.
- **ARCH-3:** the daemon-writes-cache split was partly to dodge Dolt's filesystem
  lock. WAL removes that pressure. Leave the existing behavior but annotate that
  the constraint's original driver is gone.
- Rename `dolt.go` → `sqlite.go`; drop the `DOLT_COMMIT`/`DOLT_ADD` calls
  entirely (they were the auto-commit-everything audit trail, now replaced by the
  events table itself + snapshots).

## 3. First-run behavior (no data migration)

On open, if a legacy Dolt data dir exists (the path Dolt used under
`~/.hookwise/`), rename it to `<dir>.dolt.bak` and proceed to create fresh empty
SQLite tables. Never read the old Dolt data. Log a single INFO line noting the
archive path. This is idempotent: if `analytics.db` already exists, do nothing.

## 4. Schema translation (10 tables)

Tables: `sessions`, `events`, `authorship_ledger`, `metacognition_logs`,
`agent_spans`, `feed_cache`, `coaching_state`, `cost_state`, `notifications`,
`schema_meta`. All are plain CRUD. Translation rules:

| Dolt / MySQL dialect | SQLite |
|---|---|
| `INT PRIMARY KEY AUTO_INCREMENT` | `INTEGER PRIMARY KEY AUTOINCREMENT` |
| `DOUBLE` | `REAL` |
| `JSON` column | `TEXT` (type affinity; app already (de)serializes) |
| inline `INDEX idx_x (col)` in CREATE TABLE | separate `CREATE INDEX idx_x ON t(col)` |
| `CALL DOLT_COMMIT(...)` | removed |
| `DATETIME` / timestamp text | `TEXT` storing RFC3339 (unchanged at app layer) |

No stored procedures, ENGINE clauses, or MySQL-specific functions are used.
`notifications.ttl_seconds` (added in arch-v2 Batch C) carries over as `INTEGER`.

## 5. Snapshot mechanism (Approach B)

- **Command:** `hookwise snapshot` runs `VACUUM INTO
  '~/.hookwise/snapshots/<RFC3339Z>.db'`. Atomic, consistent point-in-time copy.
- **Schedule:** the daemon triggers a snapshot on a configurable interval
  (default e.g. hourly) via its existing tick loop. Config key under the existing
  analytics/daemon config block; default ON when analytics is enabled.
- **Retention:** keep the last N snapshots (default e.g. 24); prune oldest beyond
  N on each snapshot. Prevents unbounded disk growth (resource-discipline).
- **Storage:** `~/.hookwise/snapshots/`, filenames sortable by timestamp.

## 6. `diff` / `log` reimplementation

- **`hookwise log`** — list snapshots newest-first: timestamp + file size +
  derived row counts (sessions/events) read cheaply from each snapshot. Replaces
  `dolt_log`. The events table also provides a finer-grained activity log if a
  `--events` flag is desired (optional, can be deferred).
- **`hookwise diff <a> <b>`** — resolve `<a>`/`<b>` to snapshot files (accept full
  timestamp, prefix, or relative like `latest`/`prev`). Open both read-only,
  compute per-table aggregate deltas (row-count change per table; optionally
  cost/session deltas). Replaces `dolt_diff_summary`. Output format must satisfy
  the ARCH-6 contract fixtures (or fixtures updated in lockstep with rationale).

## 7. Testing strategy (resource-safe — non-negotiable)

- **Never** run bare `go test ./...`. Use `dagger call ci --src=.` (containerized,
  memory-bounded — structurally cannot reproduce retro-009) or `task test`
  (`-p 2` + `GOMEMLIMIT=4GiB` + zombie guard).
- New unit tests: schema creation, first-run archive+fresh behavior, snapshot
  create/retention/prune, diff/log over snapshots, WAL concurrency.
- **Contract tests (ARCH-6)** are the parity oracle: stdout for `status-line`,
  `stats`, etc. must stay byte-identical. If `diff`/`log` output legitimately
  changes, update fixtures *with* the change and note it.
- Each `hookwise.test` binary should drop from ~149 MB toward a few MB once Dolt
  is gone — itself a partial remediation of retro-009 M9.

## 8. Sequencing

Dedicated branch off `main` (not `feat/architecture-v2-part2`). Implementation
order: (1) backend swap + schema + first-run, contract green; (2) snapshot
command + retention; (3) diff/log over snapshots; (4) daemon schedule; (5) drop
`dolthub` from go.mod, `CGO_ENABLED=0` build < 20 MB; (6) `make install`, doctor
green, open PR. Execute via the dynamic workflow with the test-safety rule baked
into every agent's brief.

## 9. ARCH constraint updates

- **ARCH-2** — note retained as a deliberate single-writer choice; Dolt rationale removed.
- **ARCH-3** — note that the filesystem-lock motivation is obsolete under WAL.
- Add an ADR/decision record: "Dolt removed — git-for-data features unused; SQLite
  + periodic snapshots preserve diff/log at ~1/70th the binary weight."
