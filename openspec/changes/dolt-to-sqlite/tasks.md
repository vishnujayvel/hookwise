# Tasks: Dolt → SQLite + periodic snapshots

Execution is on a dedicated branch off `main`. **All test execution is funneled
through ONE serialized point — Dagger (`dagger call ci/test`) or a single
`task test`. No agent runs `go test` directly. No parallel test execution.**
(retro-009 guard.)

## Phase 1 — Backend swap (contract-green before anything else)
- [x] 1.1 Rename `internal/analytics/dolt.go` → `sqlite.go`; open `modernc.org/sqlite`
      at `~/.hookwise/analytics.db` in WAL mode (busy_timeout, synchronous=NORMAL).
- [x] 1.2 Translate the 10-table schema per design §4 (AUTOINCREMENT, REAL, TEXT/JSON,
      separate CREATE INDEX). Remove all `CALL DOLT_*`.
- [x] 1.3 First-run logic (design §3): if legacy Dolt dir exists, rename `*.dolt.bak`;
      create fresh empty tables; idempotent.
- [x] 1.4 Relax `SetMaxOpenConns(1)` per ARCH-2 note; single writer + read pool.
- [x] 1.5 Verify: `task test` → contract suite (ARCH-6) passes for
      `status-line`/`stats` (byte-identical stdout). STOP if not green. ✓ green

## Phase 2 — Snapshots
- [x] 2.1 `hookwise snapshot` → `VACUUM INTO ~/.hookwise/snapshots/<ts>.db`
      (filename `20060102T150405Z.db` — colon-free, lexicographically sortable).
- [x] 2.2 Retention: prune oldest beyond N (default 24); keep<=0 retains all.
- [x] 2.3 Daemon-scheduled interval (default hourly), config-gated, default ON when
      analytics enabled. Scheduler in `cmd/hookwise` (package main) — ARCH-3 compliant.
- [x] 2.4 Unit tests for create/retention/prune (green via `task test:go:unit`).

## Phase 3 — diff/log over snapshots
- [x] 3.1 `hookwise log` → list snapshots newest-first (ts, size, row counts).
- [x] 3.2 `hookwise diff <a> <b>` → resolve to snapshot files (prefix/latest/prev),
      open read-only, per-table aggregate deltas.
- [x] 3.3 Update ARCH-6 contract fixtures IF output legitimately changed (note why).
      No contract fixtures reference diff/log; the dispatch-event fixtures are
      unaffected. ARCH-6 parity preserved with no fixture changes.

## Phase 4 — De-Dolt + build
- [x] 4.1 Remove `dolthub/*` from go.mod; `go mod tidy`; `grep dolthub go.mod` empty.
      go.mod 167→70 lines, go.sum 0 dolthub refs.
- [x] 4.2 `CGO_ENABLED=0 go build ./cmd/hookwise` succeeds; binary < 20 MB.
      Dev build 28 MB; release (`-s -w` strip) = 19 MB. Baked `-s -w`+`CGO_ENABLED=0`
      into `task install`; flipped Dagger goContainer to CGO=0 (gozstd gone).
      Test binary 149 MB → 12 MB (~19 MB w/ -race) — retro-009 M9 remediated.
- [x] 4.3 Update ARCH-2/ARCH-3 notes; add decision record for Dolt removal.
      docs/decisions/0001-dolt-to-sqlite.md (wired into vitepress nav); de-Dolted
      26 tracked files (comments/strings/docs only, no logic). Retro docs left
      forensically intact. ARCH-2 now "deliberate single-writer under WAL".

## Phase 5 — Verify + ship
- [x] 5.1 Full suite green. (Dagger unavailable — Docker not running; substituted
      the equivalent guarded `task test`: unit + contract + arch + pbt + 125 TUI
      + 13 cross-schema boundary tests all green. `go vet` clean across default/
      integration/mutation tags.)
- [x] 5.2 `task install` (no Makefile; CLAUDE.md ref was stale); binary 19 MB,
      `hookwise --version` matches HEAD; `hookwise doctor` green; smoked
      stats/status-line/snapshot/log/diff on real data (7 sessions, 1330 events).
- [x] 5.3 Open PR on dedicated branch. → PR #74
      https://github.com/vishnujayvel/hookwise/pull/74

## Resource-safety invariants (apply to EVERY phase)
- Never bare `go test ./...`. Dagger or `task test` only.
- One test run at a time. No concurrent/parallel test execution across agents.
- `pgrep -f hookwise.test` before any host-side test; kill zombies first.
- Do not touch `dolt sql-server` PID 4420 (Gas City's).
