# Feed Platform

A background daemon polls feed producers on configurable intervals and writes results to an atomic cache bus. Status line segments read from the cache using TTL-aware freshness checks (`isFresh()`). If a feed is unavailable or stale, its segments silently disappear (fail-open).

## Producers

8 built-in feed producers:

| Producer | Default Interval | What it provides |
|----------|-----------------|-----------------|
| `pulse` | 30s | Session heartbeat and idle time detection |
| `project` | 60s | Git repo name, branch, last commit age |
| `calendar` | 300s | Current/next calendar event and free time |
| `news` | 1800s | Hacker News top stories (rotates) |
| `insights` | 120s | Claude Code usage metrics from `~/.claude/usage-data/` |
| `practice` | 60s | Daily practice rep count and last practice time |
| `weather` | 900s | Local temperature, conditions, and wind speed |
| `memories` | 3600s | "On this day" session history and nostalgia |

## Feed Configuration

Configure feeds in `hookwise.yaml`:

```yaml
feeds:
  pulse:
    enabled: true
    interval_seconds: 30
  project:
    enabled: true
    interval_seconds: 60
  insights:
    enabled: true
    interval_seconds: 120
    staleness_days: 30
    usage_data_path: "~/.claude/usage-data"
```

## Insights Producer

The insights producer reads Claude Code's usage data from `~/.claude/usage-data/` (session-meta and facets JSON files), aggregates metrics within a configurable staleness window, and writes them to the cache bus under the `insights` key.

### Aggregated Metrics

- `total_sessions` -- count of sessions within the staleness window
- `total_messages` -- sum of user messages across sessions
- `total_lines_added` -- sum of lines added across sessions
- `avg_duration_minutes` -- mean session duration
- `top_tools` -- top 5 tools by total call count
- `friction_counts` -- aggregated friction categories (e.g., `{wrong_approach: 32, misunderstood_request: 14}`)
- `friction_total` -- sum of all friction instances
- `peak_hour` -- hour (0-23) with the most messages
- `days_active` -- unique calendar dates with at least one session
- `recent_session` -- most recent session summary (id, duration, lines added, friction count, outcome, tool errors)

### Self-Cleaning

Only sessions within the staleness window (default: 30 days) are included. Facets without a matching valid session are excluded. No manual cleanup needed.

### Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable/disable the insights producer |
| `interval_seconds` | number | `120` | Polling interval in seconds |
| `staleness_days` | number | `30` | Only include sessions newer than this many days |
| `usage_data_path` | string | `~/.claude/usage-data` | Path to Claude Code usage data directory |

### Fail-Open Behavior

Missing directory, malformed JSON files, and permission errors all result in graceful degradation -- the producer returns null and segments disappear.

## Daemon Behavior

The daemon starts automatically with `hookwise dispatch` and shuts down after a configurable period of inactivity (default: 120 minutes). Set `daemon.inactivity_timeout_minutes` in `hookwise.yaml` to adjust.

## Architecture

See [Architecture](../architecture.md#feed-platform-architecture) for the full feed platform diagram.

---

← [Back to Home](/)
