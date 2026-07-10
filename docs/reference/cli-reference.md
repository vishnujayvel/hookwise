# CLI Reference

All commands are invoked as `hookwise <command>`. Run `hookwise <command> --help` for the authoritative flag list.

## init

Initialize hookwise in the current directory.

```bash
hookwise init
```

Without flags, `init` does three things:

1. **Scans your existing Claude Code hooks** before writing anything, and saves the pre-init inventory (hooks, findings, parse errors) to `~/.hookwise/hook-audit.json`. The scan is advisory — it never modifies your Claude Code settings, and a scan failure never aborts init.
2. Creates a default `hookwise.yaml` in the current directory (skipped if one already exists).
3. Ensures the `~/.hookwise/` state directory exists.

### init --wire / --unwire

Wire hookwise into Claude Code's `settings.json` — idempotently and safely:

```bash
hookwise init --wire                # wire dispatch hooks + statusLine
hookwise init --wire --dry-run      # preview the settings.json diff without writing
hookwise init --unwire              # remove hookwise's own hooks, touching nothing else
```

| Flag | Description |
|------|-------------|
| `--wire` | Wire hookwise into Claude Code settings.json |
| `--unwire` | Remove hookwise hooks from Claude Code settings.json |
| `--dry-run` | Print what would change without writing |
| `--events <list>` | Hook events to wire, comma-separated (default `PreToolUse,PostToolUse`) |
| `--no-status-line` | Skip wiring the statusLine entry |
| `--settings <path>` | Override path to Claude Code settings.json |
| `--force` | Wire even if pre-flight finds FAIL-level issues |

Wiring runs a pre-flight safety audit first and refuses to proceed on FAIL-level findings unless `--force` is given. A backup of settings.json is written before any change.

## audit

Scan Claude Code hook configuration for health issues.

```bash
hookwise audit [--json] [--project-dir <dir>]
```

| Flag | Description |
|------|-------------|
| `--json` | Emit a schema-versioned JSON report (`schema_version: 1`) instead of text |
| `--project-dir <dir>` | Scan `<dir>/.claude/settings.json` + `settings.local.json` instead of the user-level settings |

Scans settings files for hook-safety issues: inventory/sprawl, missing binaries, network-dependent hooks on hot paths, and duplicate or overlapping hooks. The report includes a full hook inventory (by event, matcher, command, source file), findings with levels, and a `PASS` / `WARN` / `FAIL` summary.

**Exit codes:** 0 on PASS or WARN, 1 on FAIL — suitable for CI gates. Malformed settings files become FAIL findings rather than crashes.

## doctor

Run system health checks.

```bash
hookwise doctor
```

Checks:

- `hookwise.yaml` exists and parses correctly
- State directory exists
- Analytics DB opens and is queryable
- Daemon liveness (socket dial)
- **Feed config source** — when the daemon is running, doctor queries its runtime feed config (`GET /feeds`) and reports against what the daemon is *actually* polling; when it isn't, doctor falls back to the on-disk global config and says so
- **Feed config drift** — warns when the daemon is polling a stale config (you edited `~/.hookwise/config.yaml` after daemon startup) and suggests a restart
- **Project `feeds:` block** — warns if the project `hookwise.yaml` carries feed settings, which the singleton daemon ignores (move them to the global config)
- Per-feed health (fresh / stale / placeholder / no data) and status-line segment cross-check
- Existing Claude Code hook settings parse cleanly
- **Cost-tracking honesty** — sessions recorded today with $0 computed *while cost tracking is enabled* is flagged as a likely dead cost writer; with cost tracking disabled (the default), $0 is reported as expected, not a malfunction

Doctor is honest about disabled subsystems: a disabled feed reports as `INFO disabled`, not as a stale-cache warning.

## stats

Display today's analytics dashboard.

```bash
hookwise stats [--data-dir <path>]
```

| Flag | Description |
|------|-------------|
| `--data-dir <path>` | Path to the analytics SQLite DB file (defaults to config `analytics.db_path` / `~/.hookwise/analytics.db`) |

Opens the analytics database and displays today's daily summary and tool breakdown. Requires analytics to be enabled in `hookwise.yaml`.

## test

Evaluate guard test scenarios.

```bash
hookwise test [--project-dir <dir>]
```

| Flag | Description |
|------|-------------|
| `--project-dir <dir>` | Project directory (defaults to cwd) |

Loads config, creates synthetic test payloads for each guard rule, evaluates them, and reports pass/fail.

## dispatch

Dispatch a hook event. This is the fast-path command called by Claude Code on every hook event.

```bash
hookwise dispatch <EventType> [--project-dir <dir>]
```

Reads the hook payload from stdin as JSON and routes it through the three-phase execution pipeline (guard, context, side effect).

**Event types:** `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `Notification`, `Stop`, `SubagentStart`, `SubagentStop`, `PreCompact`, `SessionStart`, `SessionEnd`, `PermissionRequest`, `Setup`

This command is registered in `.claude/settings.json` — `hookwise init --wire` does it for you, or see [Getting Started](/guide/getting-started) for manual setup.

## status-line

Render the status line. Called automatically by Claude Code's `statusLine` setting.

```bash
hookwise status-line [--project-dir <dir>]
```

Loads config and the feed cache, then renders a single ANSI-colored status line to stdout. Auto-starts the feed daemon if it isn't running.

## tui

Launch the interactive TUI dashboard.

```bash
hookwise tui [--launch-method <newWindow|background>]
```

Launches the `hookwise-tui` dashboard if it is not already running, using the same singleton guard as session auto-launch (concurrent invocations spawn at most one TUI). Requires `hookwise-tui` on your PATH — the TUI ships separately from the core binary; install it from the `tui/` directory (e.g. `uv tool install ./tui`). See the [TUI Guide](tui-guide.md).

## daemon

Manage the background feed daemon.

```bash
hookwise daemon <start|stop>
```

| Subcommand | Description |
|------------|-------------|
| `start` | Start the feed daemon (connect-or-start) |
| `stop` | Stop the feed daemon |

The daemon is a singleton — one socket, one cache, one process per state directory. Its feed configuration comes from the **global** config (`~/.hookwise/config.yaml`) only, regardless of which project directory started it; a project-level `feeds:` block is ignored (`hookwise doctor` warns about this). It polls feed producers at their configured intervals and writes results to the cache bus.

## snapshot

Take a point-in-time snapshot of the analytics database.

```bash
hookwise snapshot [--data-dir <path>] [--snapshots-dir <dir>] [--retention <n>]
```

| Flag | Description |
|------|-------------|
| `--data-dir <path>` | Path to the analytics SQLite DB file |
| `--snapshots-dir <dir>` | Directory to write snapshots to (defaults to `~/.hookwise/snapshots`) |
| `--retention <n>` | Number of snapshots to keep (defaults to config `snapshot_retention`) |

Writes a consistent `VACUUM INTO` copy of the analytics database and prunes older snapshots beyond the retention limit.

## log

Show analytics snapshot history.

```bash
hookwise log [--limit <n>] [--snapshots-dir <dir>]
```

| Flag | Description |
|------|-------------|
| `--limit <n>` | Maximum number of snapshots to show, 0 = all (default 10) |
| `--snapshots-dir <dir>` | Path to snapshots directory |

Displays recent snapshots (newest first). Snapshots are created periodically by the daemon via `VACUUM INTO`.

## diff

Show row-count changes between two analytics snapshots.

```bash
hookwise diff <from-ref> <to-ref> [--snapshots-dir <dir>]
```

Refs accept `latest` (newest snapshot), `prev` (second-newest), an exact snapshot name, or a date prefix like `20260101` (newest match wins).

```bash
hookwise diff prev latest
hookwise diff 20260101 20260102
```

## notifications

Display notification history.

```bash
hookwise notifications [--limit <n>] [--data-dir <path>]
```

| Flag | Description |
|------|-------------|
| `--limit <n>` | Maximum number of notifications to show (default 20) |
| `--data-dir <path>` | Path to the analytics SQLite DB file |

Shows recent notifications from the budget producer, stored in the analytics database and surfaced via the status line or this command.

## upgrade

Migrate data from a TypeScript hookwise installation.

```bash
hookwise upgrade [--dry-run] [--data-dir <path>] [--project-dir <dir>]
```

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview migration without making changes |
| `--data-dir <path>` | Path to the analytics SQLite DB file |
| `--project-dir <dir>` | Project directory for config validation |

Detects an existing TypeScript hookwise installation, imports the data into the Go SQLite analytics database, and validates config parity. Original files are never modified (non-destructive).

See the [Migration guide](/reference/migration) for full details.
