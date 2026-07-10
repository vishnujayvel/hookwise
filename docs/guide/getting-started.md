# Getting Started

This guide walks you through installing hookwise, creating your first configuration, and verifying everything works.

## Prerequisites

- **Go 1.25+** (for building from source) or download from [GitHub Releases](https://github.com/vishnujayvel/hookwise/releases)
- **Claude Code** installed and configured

## Installation

Download the latest binary from [GitHub Releases](https://github.com/vishnujayvel/hookwise/releases) and place it on your PATH.

Or build from source:

```bash
git clone https://github.com/vishnujayvel/hookwise.git
cd hookwise
go build -o /usr/local/bin/hookwise ./cmd/hookwise/
```

## First Run

### 1. Initialize your project

Navigate to your project directory and run:

```bash
hookwise init
```

This does three things:
- **Scans your existing Claude Code hooks first** and saves the pre-init inventory to `~/.hookwise/hook-audit.json` — so you have a record of what was configured before hookwise touched anything. The scan is read-only; your Claude Code settings are never modified.
- Creates `hookwise.yaml` in your project root (the configuration file)
- Ensures the `~/.hookwise/` state directory exists (for analytics, logs, and cache)

### 2. Register hooks in Claude Code

The recommended way is `--wire`, which edits `.claude/settings.json` for you — idempotently, after a pre-flight safety audit, with a backup:

```bash
hookwise init --wire              # wires dispatch hooks + statusLine
hookwise init --wire --dry-run    # preview the diff first
hookwise init --unwire            # removes hookwise's hooks, touching nothing else
```

By default `--wire` registers the `PreToolUse` and `PostToolUse` events (customize with `--events`) and the statusLine entry (skip with `--no-status-line`). If the pre-flight finds FAIL-level issues it refuses to write unless you pass `--force`.

Alternatively, add hookwise as a hook handler manually in `.claude/settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      { "command": "hookwise dispatch PreToolUse" }
    ],
    "PostToolUse": [
      { "command": "hookwise dispatch PostToolUse" }
    ],
    "SessionStart": [
      { "command": "hookwise dispatch SessionStart" }
    ],
    "SessionEnd": [
      { "command": "hookwise dispatch SessionEnd" }
    ],
    "Stop": [
      { "command": "hookwise dispatch Stop" }
    ],
    "Notification": [
      { "command": "hookwise dispatch Notification" }
    ],
    "SubagentStop": [
      { "command": "hookwise dispatch SubagentStop" }
    ]
  }
}
```

### 3. Verify your setup

```bash
hookwise doctor
```

Doctor checks that your configuration file exists and is valid YAML, the state directory and analytics database are healthy, and the feed daemon is running. It reports per-feed health against the daemon's *effective* runtime config (what the daemon is actually polling — falling back to the on-disk global config when the daemon is down), warns when the daemon is polling a stale config or when a project-level `feeds:` block is being ignored, and cross-checks your status-line segments against feed data.

Doctor is honest about disabled subsystems: a disabled feed reports as `disabled` (not as a stale-cache warning), and $0 cost with cost tracking off is "expected", not a malfunction.

### 4. Audit your hook setup

```bash
hookwise audit
```

This inventories every hook across your Claude Code settings files and flags safety issues: missing binaries, network-dependent hooks on hot paths, and duplicate or overlapping hooks. Add `--json` for a schema-versioned report (useful in CI — exits 0 on PASS/WARN, 1 on FAIL), or `--project-dir <dir>` to scan a project's `.claude/` settings instead of your user-level settings.

## Basic Configuration

### Adding Guard Rules

Guard rules are the core of hookwise. They evaluate on every `PreToolUse` event using first-match-wins semantics:

```yaml
guards:
  # Block dangerous deletions
  - match: "Bash"
    action: block
    when: 'tool_input.command contains "rm -rf /"'
    reason: "Blocked: recursive delete of root directory"

  # Require confirmation for force operations
  - match: "Bash"
    action: confirm
    when: 'tool_input.command contains "--force"'
    reason: "Force flag detected, please confirm"

  # Warn when reading sensitive files
  - match: "Read"
    action: warn
    when: 'tool_input.file_path ends_with ".env"'
    reason: "Accessing .env file - may contain secrets"

  # Block all Gmail MCP tools by default
  - match: "mcp__gmail__*"
    action: confirm
    reason: "Gmail tool requires human confirmation"
```

### Enabling Coaching

```yaml
coaching:
  metacognition:
    enabled: true
    interval_seconds: 300  # prompt every 5 minutes

  builder_trap:
    enabled: true
    thresholds:
      yellow: 30   # gentle nudge at 30 min
      orange: 60   # stronger nudge at 60 min
      red: 90      # urgent at 90 min
```

### Enabling Analytics

```yaml
analytics:
  enabled: true
```

View your stats with:

```bash
hookwise stats
```

## Including Recipes

Recipes are pre-built configurations you can include:

```yaml
includes:
  - recipes/safety/block-dangerous-commands
  - recipes/behavioral/metacognition-prompts
  - recipes/compliance/cost-tracking
```

See [Creating a Recipe](./creating-a-recipe.md) for writing your own.

## Feeds and Insights

hookwise includes a feed platform with a background daemon that aggregates data from multiple sources.

The daemon is a singleton shared by every project, so feeds are configured in the **global** config at `~/.hookwise/config.yaml` — not in a project's `hookwise.yaml`. A project-level `feeds:` block is ignored for polling, and `hookwise doctor` warns about it.

```yaml
# ~/.hookwise/config.yaml
feeds:
  project:
    enabled: true
    interval_seconds: 60
  calendar:
    enabled: true
    interval_seconds: 300
  news:
    enabled: true
    interval_seconds: 600
  insights:
    enabled: true
    interval_seconds: 120
  weather:
    enabled: true
    interval_seconds: 900
  memories:
    enabled: true
    interval_seconds: 3600
```

Start the daemon to begin collecting feeds:

```bash
hookwise daemon start
```

Check feed health:

```bash
hookwise doctor
```

Doctor queries the daemon's runtime feed config, so what it reports is what the daemon is actually polling — including a warning if you edited the global config after the daemon started.

The insights producer reads your Claude Code usage data from `~/.claude/usage-data/` and surfaces metrics like session frequency, tool friction, and pace trends in your status line.

See the [Feeds Guide](./feeds-guide.md) for a deep dive into the feed platform architecture.

## Environment Variables

hookwise supports environment variable interpolation in config values:

```yaml
settings:
  state_dir: "${HOME}/.hookwise"
```

The state directory itself can be overridden:

```bash
export HOOKWISE_STATE_DIR=/custom/path
```

## Next Steps

- Browse the [Hook Events Reference](/reference/hook-events) to understand all 13 event types
- Explore the [TUI Guide](/reference/tui-guide) for the interactive terminal interface
- Read about [Creating a Recipe](./creating-a-recipe.md) to share your patterns
- Learn about the [Feed Platform](./feeds-guide.md) and the background daemon
- Explore [Analytics](./analytics-guide.md) for session tracking and AI authorship metrics
- If migrating from v0.1.0, see [Migration](/reference/migration)
