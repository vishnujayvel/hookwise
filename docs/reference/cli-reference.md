# CLI Reference

hookwise provides 13 CLI commands. All commands are invoked as `hookwise <command>`.

## init

Initialize hookwise in the current directory.

```bash
hookwise init [--preset <name>]
```

| Flag | Description |
|------|-------------|
| `--preset <name>` | Use a preset configuration: `minimal`, `coaching`, `analytics`, `full` |

Creates `hookwise.yaml` in the current directory and `~/.hookwise/` state directory.

**Presets:**

| Preset | Includes |
|--------|----------|
| `minimal` | Guards only |
| `coaching` | Guards + metacognition + builder's trap |
| `analytics` | Guards + SQLite session tracking |
| `full` | All features (guards, coaching, analytics, feeds, insights, status line, cost tracking) |

## doctor

Check system health and configuration.

```bash
hookwise doctor
```

Validates:
- `hookwise.yaml` exists and parses correctly
- State directory (`~/.hookwise/`) exists with correct permissions
- All referenced handlers are accessible
- Recipe includes resolve correctly
- Feed configuration is valid
- Daemon status

## status

Show current configuration summary.

```bash
hookwise status [--config <path>]
```

| Flag | Description |
|------|-------------|
| `--config <path>` | Path to config file (default: `hookwise.yaml` in cwd) |

Displays which features are enabled, guard rule count, included recipes, and feed status.

## status-line

Render the two-tier status line. Reads a JSON payload from stdin (provided by Claude Code).

```bash
echo '{"session_id":"abc","context_window":{"used_percentage":0.67}}' | hookwise status-line
```

This command is called automatically by Claude Code hooks -- you rarely need to invoke it manually.

## stats

Display session analytics and tool usage.

```bash
hookwise stats [--json] [--agents] [--cost] [--streaks]
```

| Flag | Description |
|------|-------------|
| `--json` | Output structured JSON |
| `--agents` | Include multi-agent activity summary |
| `--cost` | Include cost breakdown |
| `--streaks` | Include coding streak information |

Requires analytics to be enabled in `hookwise.yaml`.

## test

Run the hookwise guard test suite.

```bash
hookwise test
```

Loads test scenarios from `hookwise.yaml` and runs them against your guard rules, reporting pass/fail for each scenario.

## tui

Launch the interactive terminal UI.

```bash
hookwise tui
```

Opens a full-screen interface with 7 tabs:

| Key | Tab | Description |
|-----|-----|-------------|
| `1` | Dashboard | Session status, feature toggles, quick stats |
| `2` | Guards | Rule browser, test interface, match visualization |
| `3` | Coaching | Metacognition, builder's trap, communication status |
| `4` | Analytics | Daily summary, tool breakdown, authorship metrics |
| `5` | Recipes | Installed recipes, details, active includes |
| `6` | Status | Status line preview, segment configuration |
| `7` | Feeds | Live feed data, daemon health, cache bus state |

Press `q` or `Escape` to exit.

## migrate

Migrate from Python hookwise (v0.1.0).

```bash
hookwise migrate [--dry-run]
```

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview changes without applying them |

Detects existing `hookwise.yaml`, checks for Python handler scripts, suggests `python3` prefix, and validates against the v1.0 schema.

See the [Migration guide](/reference/migration) for full details.

## dispatch

Dispatch a hook event. This is the fast-path command called by Claude Code on every hook event.

```bash
hookwise dispatch <event-type>
```

Reads the hook payload from stdin as JSON and routes it through the three-phase execution pipeline (guard, context, side effect).

**Event types:** `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `Notification`, `Stop`, `SubagentStart`, `SubagentStop`, `PreCompact`, `SessionStart`, `SessionEnd`, `PermissionRequest`, `Setup`

This command is registered in `.claude/settings.json` -- see [Getting Started](/guide/getting-started) for setup instructions.

## daemon

Manage the background feed daemon.

```bash
hookwise daemon <start|stop|status> [--config <path>]
```

| Subcommand | Description |
|------------|-------------|
| `start` | Start the daemon as a detached background process |
| `stop` | Send SIGTERM and remove the PID file |
| `status` | Show daemon PID and running state |

| Flag | Description |
|------|-------------|
| `--config <path>` | Path to config file |

The daemon polls feed producers at their configured intervals and writes results to the cache bus.

## feeds

Live feed dashboard showing daemon health and cache bus state.

```bash
hookwise feeds [--once] [--config <path>]
```

| Flag | Description |
|------|-------------|
| `--once` | Show a snapshot and exit (no auto-refresh) |
| `--config <path>` | Path to config file |

Displays each feed's status, last update time, freshness, and cached data.

## setup

Set up external integrations.

```bash
hookwise setup <target>
```

| Target | Description |
|--------|-------------|
| `calendar` | Configure Google Calendar OAuth for the calendar feed |

Stores credentials at `~/.hookwise/calendar-credentials.json`.
