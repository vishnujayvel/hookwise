# TUI Guide

hookwise includes a full-screen interactive terminal UI (TUI) built with React Ink. It provides a visual interface for browsing your configuration, viewing analytics, and testing guards.

## Launching the TUI

```bash
hookwise tui
```

Or use the interactive flag with any command:

```bash
hookwise --interactive
```

## Navigation

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `1` | Switch to Dashboard tab |
| `2` | Switch to Guards tab |
| `3` | Switch to Coaching tab |
| `4` | Switch to Analytics tab |
| `5` | Switch to Recipes tab |
| `6` | Switch to Status tab |
| `q` | Quit the TUI |
| `Escape` | Quit the TUI |

Tabs are numbered 1-6. Press the corresponding number key to switch instantly.

## Tabs

### 1. Dashboard

The main overview tab showing:

- **Session status** -- Current session ID and duration
- **Feature toggles** -- Quick view of which features are enabled (guards, coaching, analytics, etc.)
- **Quick stats** -- Summary of recent activity
- **Configuration path** -- Location of the active hookwise.yaml

### 2. Guards

Browse and inspect your guard rules:

- **Rule list** -- All configured guard rules with match patterns, actions, and reasons
- **Rule details** -- Select a rule to see its `when`/`unless` conditions
- **Test interface** -- Enter a tool name and input to test against your rules
- **Match visualization** -- See which rule would match a given tool call

### 3. Coaching

View coaching module status:

- **Metacognition** -- Enabled/disabled, interval, last prompt time
- **Builder's Trap** -- Current mode, alert level, time in current mode
- **Communication Coach** -- Enabled/disabled, frequency, grammar rules
- **Prompt history** -- Recent metacognition prompts that were shown

### 4. Analytics

Session analytics and metrics:

- **Daily summary** -- Tool calls, lines added/removed, session count
- **Tool breakdown** -- Which tools are called most often
- **Authorship metrics** -- AI vs human code ratio with classification
- **Cost summary** -- Today's spending vs daily budget

### 5. Recipes

Browse and manage recipes:

- **Installed recipes** -- List of recipes found in recipes/ and node_modules/
- **Recipe details** -- Select a recipe to see its description, events, and config
- **Active includes** -- Which recipes are currently included in your config

### 6. Status

Status line preview and configuration:

- **Live preview** -- See what the status line looks like with current data
- **Segment list** -- All available segments and their current values
- **Configuration** -- Delimiter, cache path, enabled segments

## Configuration

The TUI reads from the same `hookwise.yaml` that the dispatch pipeline uses. Changes made in the TUI are written back to the config file.

## Requirements

The TUI requires a terminal that supports:
- ANSI color codes
- Minimum 80 columns width
- Unicode characters (for status indicators)

Most modern terminals (iTerm2, Kitty, Windows Terminal, VS Code integrated terminal) work well.

## Exiting

Press `q` or `Escape` to exit the TUI cleanly. The TUI does not run any hooks -- it is a read-only viewer and configuration editor.
