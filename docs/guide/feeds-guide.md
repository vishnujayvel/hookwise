# Feeds Guide

The feed platform is a background data aggregation system introduced in hookwise v1.1, with the insights producer added in v1.2. It collects data from multiple sources, caches it with TTL-aware freshness, and surfaces it through the status line and TUI.

## Architecture

```
Daemon (background process)
  ├── Feed Registry (registration + lookup)
  │     ├── pulse     (heartbeat / idle detection)
  │     ├── project   (git branch + last commit)
  │     ├── calendar  (Google Calendar events)
  │     ├── news      (Hacker News top stories)
  │     ├── insights  (Claude Code usage metrics)
  │     └── custom[]  (user-defined shell commands)
  │
  └── Cache Bus (per-key atomic merge + TTL reads)
        └── ~/.hookwise/state/status-line-cache.json
```

The daemon polls each producer at its configured interval, merges results into the cache bus, and the status line reads from the cache on every render.

## Producers

### Pulse

The heartbeat producer tracks session liveness. It writes a timestamp on every dispatch event and the daemon checks for staleness.

```yaml
feeds:
  pulse:
    enabled: true
    interval_seconds: 30
    thresholds:
      green: 30     # seconds since last activity
      yellow: 60
      orange: 120
      red: 300
      skull: 600
```

The status line shows a colored indicator based on how long since the last activity.

### Project

Reads git metadata from the current working directory.

```yaml
feeds:
  project:
    enabled: true
    interval_seconds: 60
    show_branch: true
    show_last_commit: true
```

Surfaces the current branch name and last commit message in the status line.

### Calendar

Fetches upcoming events from Google Calendar.

```yaml
feeds:
  calendar:
    enabled: true
    interval_seconds: 300
    lookahead_minutes: 60
    calendars:
      - primary
```

Requires a one-time setup:

```bash
hookwise setup calendar
```

This stores credentials at `~/.hookwise/calendar-credentials.json`.

### News

Fetches top stories from Hacker News (or a custom RSS feed).

```yaml
feeds:
  news:
    enabled: true
    source: hackernews    # or "rss"
    rss_url: null          # required if source is "rss"
    interval_seconds: 600
    max_stories: 5
    rotation_minutes: 10
```

Stories rotate periodically so the status line shows fresh content.

### Insights (v1.2)

Reads Claude Code usage data from `~/.claude/usage-data/` and aggregates 10 metrics:

- **Session frequency** -- how often you start sessions
- **Tool friction** -- ratio of blocked/warned tool calls
- **Pace trends** -- tool calls per minute over time
- **Days active** -- activity streak tracking
- And more

```yaml
feeds:
  insights:
    enabled: true
    interval_seconds: 120
    staleness_days: 7
    usage_data_path: "~/.claude/usage-data/"
```

Surfaces through 3 status line segments: `insights_friction`, `insights_pace`, `insights_trend`.

### Custom Feeds

You can define your own feeds that run arbitrary shell commands:

```yaml
feeds:
  custom:
    - name: weather
      command: "curl -s wttr.in/?format=3"
      interval_seconds: 1800
      enabled: true
      timeout_seconds: 10
```

The command must output valid JSON to stdout. Non-JSON output is discarded. Failed or timed-out commands return null and the cache entry stays stale until the next successful run.

## Cache Bus

The cache bus is the central data store for all feed data. It provides:

- **Per-key atomic merge** -- each producer writes to its own key without affecting others
- **TTL-aware reads** -- `isFresh()` checks `updated_at + ttl_seconds` against the current time
- **Fail-open on corruption** -- corrupt or missing cache files are treated as empty objects

Cache location: `~/.hookwise/state/status-line-cache.json`

Each entry in the cache has this shape:

```json
{
  "pulse": {
    "updated_at": "2026-02-23T10:00:00.000Z",
    "ttl_seconds": 30,
    "idle_minutes": 2
  },
  "project": {
    "updated_at": "2026-02-23T10:00:00.000Z",
    "ttl_seconds": 60,
    "branch": "main",
    "last_commit": "fix: resolve race condition"
  }
}
```

## Daemon Management

The daemon is a detached background process that polls producers and writes to the cache bus.

### Start

```bash
hookwise daemon start
```

Creates a PID file at `~/.hookwise/daemon.pid` and logs to `~/.hookwise/daemon.log`.

### Stop

```bash
hookwise daemon stop
```

Sends SIGTERM to the daemon process and removes the PID file.

### Status

```bash
hookwise daemon status
```

Reports whether the daemon is running, its PID, and uptime.

### Configuration

```yaml
daemon:
  auto_start: false           # start daemon automatically on SessionStart
  inactivity_timeout_minutes: 30  # stop after no activity
  log_file: "~/.hookwise/daemon.log"
```

The daemon includes stale PID cleanup -- if the PID file references a dead process, it is removed automatically.

### Staggered Intervals

Producers do not all poll at the same time. The daemon staggers their schedules to avoid burst I/O. Each producer runs on its own interval independently.

## Feed Dashboard

View live feed health with:

```bash
hookwise feeds
```

This shows:
- Daemon status (running/stopped)
- Each feed's last update time and freshness
- Cache bus size and key count

Use `--once` for a snapshot that exits immediately:

```bash
hookwise feeds --once
```

## Troubleshooting

**Daemon not starting**
- Check if another daemon is already running: `hookwise daemon status`
- Check the log file: `cat ~/.hookwise/daemon.log`
- Verify the PID file is not stale: `cat ~/.hookwise/daemon.pid`

**Stale feed data**
- The daemon may have stopped. Restart it: `hookwise daemon start`
- Check TTL values -- very short TTLs may show stale data between polls
- Run `hookwise feeds` to see which feeds are fresh vs. stale

**No usage-data directory**
- The insights producer needs `~/.claude/usage-data/` to exist
- This directory is created by Claude Code during sessions
- If it does not exist, insights metrics will be empty until your first session
