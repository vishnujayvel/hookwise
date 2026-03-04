# Getting Started

This guide walks you through installing hookwise, creating your first configuration, and verifying everything works.

## Prerequisites

- **Node.js 20+** (check with `node --version`)
- **Claude Code** installed and configured

## Installation

Install hookwise globally:

```bash
npm install -g hookwise
```

Or use it directly with npx:

```bash
npx hookwise init
```

## First Run

### 1. Initialize your project

Navigate to your project directory and run:

```bash
hookwise init --preset minimal
```

This creates two things:
- `hookwise.yaml` in your project root (the configuration file)
- `~/.hookwise/` state directory (for analytics, logs, and cache)

Available presets:

| Preset | Description |
|--------|-------------|
| `minimal` | Guards only -- blocks dangerous commands |
| `coaching` | Guards + metacognition prompts + builder's trap detection |
| `analytics` | Guards + SQLite session tracking and authorship metrics |
| `full` | All features enabled |

### 2. Register hooks in Claude Code

Add hookwise as a hook handler in your Claude Code settings (`.claude/settings.json`):

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

This runs 9 checks:
- Node.js version (20+)
- Claude directory exists (`~/.claude/`)
- Configuration file exists and is valid YAML
- State directory has correct permissions
- Status line segments resolve correctly
- Daemon process is running
- Insights data is accessible
- Calendar credentials are valid
- Python 3 is available (for TUI)

### 4. Check your configuration

```bash
hookwise status
```

This shows a summary of your current configuration: which features are enabled, how many guard rules are active, and which recipes are included.

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

hookwise includes a feed platform with a background daemon that aggregates data from multiple sources:

```yaml
feeds:
  pulse:
    enabled: true
    interval_seconds: 30
  project:
    enabled: true
    interval_seconds: 60
  calendar:
    enabled: true
    interval_seconds: 300
  news:
    enabled: true
    source: hackernews
    interval_seconds: 600
  insights:
    enabled: true
    interval_seconds: 120
  practice:
    enabled: true
    interval_seconds: 60
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
hookwise feeds
```

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
