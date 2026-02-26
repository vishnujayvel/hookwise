# TUI Guide

hookwise includes a full-screen interactive terminal UI (TUI) built with [Python Textual](https://textual.textualize.io/). It provides a rich, colorful dashboard for browsing your configuration, viewing analytics, monitoring feeds, and exploring Claude Code usage insights.

## Installation

The TUI is a separate Python package bundled in the `tui/` directory:

```bash
cd tui && pip install -e .
```

Or install from the venv that ships with hookwise:

```bash
hookwise tui  # Auto-detects the bundled venv
```

## Launching the TUI

```bash
hookwise tui
```

## Navigation

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `1` | Switch to Dashboard tab |
| `2` | Switch to Guards tab |
| `3` | Switch to Coaching tab |
| `4` | Switch to Analytics tab |
| `5` | Switch to Feeds tab |
| `6` | Switch to Insights tab |
| `7` | Switch to Recipes tab |
| `8` | Switch to Status tab |
| `q` | Quit the TUI |

Tabs are numbered 1-8. Press the corresponding number key to switch instantly.

## Tabs

### 1. Dashboard

Feature overview showing all hookwise capabilities:

- **Feature cards** -- Each feature (Analytics, Guards, Coaching, Feeds, Status Line, Cost Tracking, Greeting, Transcript Backup) displayed with description and enabled/disabled badge
- **Configuration summary** -- Guard count, active coaching modules, active feeds
- **Quick status** -- See at a glance what's on and what's off

### 2. Guards

Guard rules table with action descriptions:

- **Action legend** -- What BLOCK, WARN, and CONFIRM mean
- **Rule table** -- All configured guard rules with match patterns, actions, reasons, and conditions
- **First-match-wins** -- Rules are evaluated top-to-bottom

### 3. Coaching

Three coaching features with user-friendly explanations:

- **Metacognition** -- "Periodic nudges to reflect on your approach -- prevents autopilot coding"
- **Builder's Trap** -- "Alerts when you've been in tooling/config too long without shipping value"
- **Communication Coach** -- "Grammar and clarity checks on your prompts (pattern-based, no LLM cost)"
- Each card shows current configuration values (interval, thresholds, tone)

### 4. Analytics

Coding patterns, tool usage, and AI authorship:

- **Session metrics** -- Sessions, tool calls, lines added (7-day window)
- **Sparkline trends** -- Sessions/day and lines/day visualized as sparklines
- **AI authorship ratio** -- Visual progress bar showing AI vs human code percentage
- **Tool breakdown** -- Table of most-used tools with call counts and line changes

See the [Analytics Guide](/guide/analytics-guide) for details on what is tracked and how authorship scoring works.

### 5. Feeds

Live feed dashboard with auto-refresh (every 3 seconds):

- **Daemon status** -- Running/stopped, PID, uptime
- **Feed health** -- Per-feed indicators (HEALTHY/STALE/DISABLED) with last update time
- **Refresh timer** -- "Last refresh: HH:MM:SS | Next in 3s | Refresh #N"
- **Architecture diagram** -- Visual overview of Daemon → Registry → Cache Bus → Producers
- **Feed descriptions** -- What each feed provides (pulse, project, calendar, news, insights)

See the [Feeds Guide](/guide/feeds-guide) for architecture details and configuration.

### 6. Insights

Claude Code usage analytics (dedicated tab, new in v1.2):

- **Key metrics** -- Total sessions, messages, lines added, avg duration, peak hour, days active, friction events
- **Sparkline trends** -- 30-day trends for sessions/day, messages/day, lines/day
- **Top tools** -- Bar chart of most-used tools
- **Friction breakdown** -- Categorized friction events (wrong_approach, tool_failure, etc.)
- **Daily AI summary** -- LLM-generated narrative about your coding patterns, top insight, and suggested focus area (uses Haiku, ~$0.01/day)
- **Refresh button** -- Manually regenerate the daily summary

### 7. Recipes

Recipe browser grouped by category:

- **Active includes** -- Which recipes are currently included in your config
- **Recipe tree** -- All available recipes organized by category (safety, coaching, productivity, etc.)
- **Recipe descriptions** -- What each recipe configures

### 8. Status

Status line preview and segment configurator:

- **Live preview** -- Real-time rendering of what the status line looks like with current cache data
- **Segment list** -- All segments with data availability indicators
- **Configuration** -- Enabled/disabled status, delimiter, cache path
- Auto-refreshes every 3 seconds

## Configuration

The TUI reads from the same `hookwise.yaml` that the dispatch pipeline uses. It is a read-only viewer -- configuration changes should be made by editing hookwise.yaml directly.

## Data Sources

The TUI reads from these locations (all read-only):

| Source | Path | Content |
|--------|------|---------|
| Config | `hookwise.yaml` | YAML configuration |
| Cache Bus | `~/.hookwise/state/status-line-cache.json` | Feed cache with TTL |
| Analytics | `~/.hookwise/analytics.db` | SQLite session analytics |
| Usage Data | `~/.claude/usage-data/` | Session-meta and facets JSON |
| Daemon PID | `~/.hookwise/daemon.pid` | Background daemon status |

## Requirements

- Python 3.10+ with Textual installed
- Terminal with ANSI color support
- Minimum 80 columns width
- Unicode support (for sparklines and status indicators)

Most modern terminals (iTerm2, Kitty, Windows Terminal, VS Code integrated terminal) work well.
