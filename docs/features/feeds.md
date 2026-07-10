# Feed Platform

A background daemon polls feed producers on configurable intervals and writes results to an atomic cache bus. Status line segments read from the cache using TTL-aware freshness checks (`isFresh()`). If a feed is unavailable or stale, its segments silently disappear (fail-open).

## Producers

6 built-in feed producers:

| Producer | What it provides |
|----------|-----------------|
| `project` | Git repo name, branch, last commit age |
| `news` | Hacker News top stories (rotates) |
| `calendar` | Current/next calendar event and free time |
| `weather` | Local temperature, conditions, and wind speed |
| `memories` | "On this day" session history and nostalgia |
| `insights` | Claude Code usage metrics from `~/.claude/usage-data/` |

Each enabled feed polls at its configured `interval_seconds`, defaulting to 60s when unset. User-defined custom feed producers can be added under `feeds.custom`.

## Feed Configuration

Feeds are polled by a single shared daemon, so they are configured in the **global** config at `~/.hookwise/config.yaml` — not in a project's `hookwise.yaml`. A `feeds:` block in a project config is ignored for polling, and `hookwise doctor` warns about it and lists the keys to move.

```yaml
# ~/.hookwise/config.yaml
feeds:
  project:
    enabled: true
    interval_seconds: 60
  insights:
    enabled: true
    interval_seconds: 120
    staleness_days: 30
    usage_data_path: "~/.claude/usage-data"
```

`hookwise doctor` reports feed health against the daemon's *effective* runtime config (queried from the running daemon), falling back to the on-disk global config when the daemon is down — and warns when the daemon is polling a stale config after a global-config edit.

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

The daemon is auto-started by `hookwise status-line` when it isn't running (or start it explicitly with `hookwise daemon start`), and shuts down after a configurable period of inactivity (default: 120 minutes). Set `daemon.inactivity_timeout_minutes` in the global `~/.hookwise/config.yaml` to adjust.

The daemon is a singleton — one socket, one cache, one process per state directory — and its entire config comes from the global config file, independent of which project started it.

## Architecture

See [Architecture](../architecture.md#feed-platform-architecture) for the full feed platform diagram.

---

← [Back to Home](/)
