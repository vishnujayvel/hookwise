# Daemon-Based Dispatch — Eliminate Per-Event Binary Fork

> ⚠️ **SUPERSEDED — premise obsolete (see issue #85, 2026-06-13).**
> This proposal predates the Dolt→SQLite migration (#74). Its cost/benefit case
> rests on the 144 MB Dolt binary and ~59 MB-per-fork RSS, which no longer exist:
> measured today the binary is **19 MB** and a dispatch event is **~21 MB RSS /
> ~20 ms**. The memory crisis that motivated this refactor was resolved by #74
> itself. Do NOT implement as written — re-measure aggregate cost under heavy
> subagent fan-out and rewrite against post-SQLite reality first. Original text
> below preserved for history.

## Problem

Every Claude Code hook event (PreToolUse, PostToolUse, etc.) spawns a fresh `hookwise dispatch` process — a 144 MB Go binary with Dolt embedded. At 59 MB RSS and 70ms per invocation, this creates:

- **118 MB peak memory per tool call** (Pre + Post dispatch)
- **~59 MB per status-line poll** (opens Dolt synchronously, polled every 5-10s)
- **200+ processes/minute** with subagents active

Meanwhile, the hookwise **daemon is already running** at 67 MB RSS with config loaded and feed producers active. But dispatch events bypass it entirely — each one forks a cold process.

See: `.claude/specs/retro/retro-010-hook-memory-audit.md`

## Goals

1. Route all dispatch events through the running daemon via Unix domain socket
2. Create a thin client script (~14 KB) that Claude Code hooks invoke instead of the full binary
3. Eliminate per-event binary loading (59 MB → ~0.5 MB per event)
4. Maintain ARCH-1 (fail-open): if daemon is down, hooks silently pass

## Non-Goals

- Removing Dolt from the main binary entirely (separate effort)
- Changing the Python quality-of-life hooks (they're lightweight, keep as-is)
- Modifying the feed producer architecture
- Changing the TUI

## Design

### Component 1: Socket Listener (daemon side)

Add a Unix domain socket listener to the existing daemon at `~/.hookwise/dispatch.sock`.

The listener goroutine:
1. Accepts connections on the socket
2. Reads JSON hook payload from the connection (same format as stdin today)
3. Calls the existing `core.Dispatch()` function (already in the daemon's import graph)
4. Writes the dispatch result JSON back on the connection
5. Closes the connection

**Socket lifecycle:**
- Created when daemon starts (`daemon run`)
- Removed on clean shutdown (defer `os.Remove`)
- Stale socket file detected and cleaned up on startup

**Concurrency:** Each connection handled in its own goroutine. Guard evaluation is stateless and safe to parallelize. Analytics recording already uses `SetMaxOpenConns(1)` for serialization.

### Component 2: Thin Dispatch Client (hook side)

A shell script (`hookwise-dispatch.sh`) that:
1. Reads JSON from stdin (hook payload from Claude Code)
2. Sends it to `~/.hookwise/dispatch.sock` via `socat` or `nc -U`
3. Prints the response to stdout
4. If socket doesn't exist or connection fails → exit 0 (fail-open, ARCH-1)

**Why shell, not Go?** A Go binary would still be ~5-8 MB (Go runtime). A shell script is ~1 KB and uses the system's `socat`/`nc` which are already in memory. Zero binary load.

**Fallback:** If `socat` is not available, fall back to `nc -U` (netcat with Unix socket). If neither available, exit 0 (fail-open).

### Component 3: Status Line via Daemon

Add a `status-line` endpoint to the daemon socket protocol. The daemon already has feed cache access. Add Dolt query caching (refresh every 30s, not every poll) for daily summary and notifications.

The status line hook becomes:
```bash
echo '{"command":"status-line"}' | socat - UNIX-CONNECT:~/.hookwise/dispatch.sock
```

### Wire Protocol

Simple newline-delimited JSON over Unix socket:

**Request (client → daemon):**
```json
{"type":"dispatch","event":"PreToolUse","payload":{...hook stdin JSON...}}
```
or:
```json
{"type":"status-line"}
```

**Response (daemon → client):**
```json
{"result":{...dispatch result JSON...}}
```
or for status-line:
```json
{"statusLine":"segment1 | segment2 | ..."}
```

**Error (daemon → client):**
```json
{"error":"message"}
```

Client reads one line of JSON response, prints the appropriate output, exits.

## Testing Strategy

1. **Unit tests:** Socket listener accepts connections, dispatches events, returns results
2. **Integration test:** Full round-trip — client script → socket → daemon → response
3. **Fail-open test:** Client with no daemon running exits 0 with no output
4. **Contract parity:** Same guard evaluation results via socket as via direct binary invocation
5. **Concurrency test:** 10 simultaneous dispatch requests return correct results

**IMPORTANT:** All tests MUST use `-p 2` flag (retro-009 resource safety). Do NOT run uncontrolled `go test ./...`.

## Acceptance Criteria

- [ ] Daemon listens on `~/.hookwise/dispatch.sock` when started
- [ ] `hookwise-dispatch.sh` sends events and receives guard results
- [ ] Guard evaluation produces identical results to current `hookwise dispatch`
- [ ] Analytics recording works through daemon (if enabled)
- [ ] Status line renders through daemon with Dolt query caching
- [ ] Fail-open: missing daemon = exit 0, no output, no error
- [ ] Stale socket cleanup on daemon start
- [ ] Clean socket removal on daemon shutdown
- [ ] Memory per hook event: < 1 MB (just the shell script + socat)
- [ ] All existing contract tests still pass

## Scope

### Files to modify:
- `internal/feeds/daemon.go` — Add socket listener goroutine
- `internal/feeds/socket.go` — New: socket protocol handler
- `cmd/hookwise/cmd_daemon.go` — Wire socket listener into daemon start

### Files to create:
- `scripts/hookwise-dispatch.sh` — Thin client shell script
- `internal/feeds/socket_test.go` — Socket listener tests

### Files NOT touched:
- `internal/core/dispatcher.go` — Reused as-is by the daemon
- `internal/analytics/` — Reused as-is by the daemon
- Python hooks — Unchanged
- `hookwise.yaml` — No config changes needed
