# ADR-0001: Replace Dolt with modernc.org/sqlite

**Status:** Accepted  
**Date:** 2026-06-07  
**Branch:** feat/dolt-to-sqlite

---

## Context

hookwise used Dolt (dolthub/go-mysql-server embedded) as its analytics backend from v1.0 through v1.4. Dolt was chosen for its git-like versioning features — the expectation was that hookwise would use branch, merge, time-travel, and rollback to provide a version-controlled audit trail of session data.

In practice, **none of Dolt's version-control features were ever used**. A full audit of the codebase found:

- No calls to `dolt_checkout`, `dolt_branch`, `dolt_merge`, `dolt_reset`, or any time-travel API
- `DOLT_COMMIT` (auto-commit) was used, and `dolt_log` / `dolt_diff_summary` were called by `hookwise log` / `hookwise diff` — but these two CLI commands are the only features that leveraged Dolt beyond plain SQL reads and writes
- The promised "rollback on failure" was never implemented

Dolt's embedded binary was responsible for two production incidents documented in retro-009 and retro-010:

- **retro-009 (system crash / kernel panic):** Uncontrolled `go test ./...` spawned 642 concurrent `hookwise.test` processes totalling > 95 GB of virtual memory. Each test binary embedded the full Dolt engine at ~149 MB (300 MB with `-race`). The system crashed with a kernel panic. Recovery required a force-restart.
- **retro-010 (the "Dolt Tax"):** The hookwise binary was 137 MB — entirely due to Dolt's embedded MySQL-compatible engine. Every Claude Code hook invocation (`hookwise dispatch`) paid a 137 MB binary load. On a machine with many concurrent Claude Code sessions this was directly observable in memory pressure.

Dolt also imposed **ARCH-2** (serialized writes via `SetMaxOpenConns(1)`) as a hard requirement because Dolt's embedded engine was not concurrent-write safe. That constraint carried unnecessary cognitive overhead.

---

## Decision

Replace Dolt with **modernc.org/sqlite** — a pure-Go, CGO-free SQLite driver that uses WAL (Write-Ahead Logging) mode.

The `hookwise log` and `hookwise diff` CLI commands, which previously read from `dolt_log` and `dolt_diff_summary` virtual tables, are reimplemented using **periodic VACUUM INTO snapshots**:

- The daemon takes a VACUUM INTO copy of `analytics.db` into `~/.hookwise/snapshots/` on a configurable interval (default: hourly, config key `snapshot_interval_minutes`).
- `hookwise log` lists snapshots newest-first, showing name, timestamp, size, and session/event counts.
- `hookwise diff <a> <b>` computes per-table row-count deltas between two snapshots. Refs accept `latest`, `prev`, a full timestamp, or a timestamp prefix.
- Retention is configurable via `snapshot_retention` (default: 24 snapshots).

Any pre-existing Dolt data directory (`~/.hookwise/dolt/`) is archived to `~/.hookwise/dolt.dolt.bak` on first open of the new analytics DB. No Dolt data is read or migrated; the archive is a safety net only.

---

## Consequences

### Positive

- **Binary size:** 137 MB → 28 MB dev / **~19 MB stripped release**. The `hookwise dispatch` hot path is now negligible to load.
- **Test binary size:** ~149 MB → ~12 MB (~19 MB with `-race`). retro-009 root cause eliminated.
- **CGO-free:** modernc.org/sqlite is a pure-Go SQLite driver. No CGO toolchain required. Cross-compilation is trivial. CI containers do not need a C compiler.
- **Dependency footprint:** go.mod reduced from 167 lines (12 dolthub modules) to 70 lines.
- **ARCH-2 simplified:** `SetMaxOpenConns(1)` is retained but now as a deliberate single-writer choice under WAL (WAL allows concurrent readers + one writer). It is no longer a Dolt threading workaround.
- **ARCH-3 motivation updated:** The daemon-writes-JSON-not-DB split was partly motivated by Dolt's filesystem lock behaviour. Under WAL that pressure is gone; the pattern is retained for architectural clarity but annotated accordingly.
- **retro-009 and retro-010 root cause removed.**

### Trade-offs / Negative

- **Lost Dolt version-control features** — but the audit confirmed these were never used. The gap between what Dolt promised and what hookwise actually used was 100%. Snapshots provide coarser-grained point-in-time history (hourly by default vs. per-dispatch auto-commit), but that granularity was never surfaced in any user-facing feature.
- **`hookwise log` / `hookwise diff` semantics changed** from Dolt commit history to snapshot-based deltas. Existing users of these commands will see different output format and different ref syntax. The new semantics are documented in `docs/features/analytics.md`.
- **No per-dispatch commit hash.** Dolt produced a commit hash for every dispatch (`DOLT_COMMIT`). The SQLite backend has no equivalent. The events table serves as the audit trail; snapshots provide point-in-time anchors.

---

## References

- retro-009: `.claude/specs/retro/retro-009-system-crash-memory-exhaustion.md`
- retro-010: `.claude/specs/retro/retro-010-dolt-tax.md` (if present)
- Implementation branch: `feat/dolt-to-sqlite`
- Phase 1 commit: `2183342` (swap Dolt backend for modernc SQLite)
