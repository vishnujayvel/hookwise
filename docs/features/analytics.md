# Session Analytics

SQLite-backed tracking for session activity and tool usage.

## What Gets Tracked

- Session duration and tool call counts
- Tool breakdown by category
- Cost tracking with daily budget enforcement

<div align="center">
<img src="../../screenshots/stats.png" alt="hookwise stats output" width="600">
</div>

## Configuration

```yaml
analytics:
  enabled: true

cost_tracking:
  enabled: true
  daily_budget: 10
  enforcement: warn
```

View your stats with:

```bash
hookwise stats
```

## Snapshots

The daemon takes periodic point-in-time snapshots of the analytics database using SQLite `VACUUM INTO`, stored in `~/.hookwise/snapshots/`. Snapshot settings:

```yaml
analytics:
  snapshot_enabled: true
  snapshot_interval_minutes: 60   # default: hourly
  snapshot_retention: 24          # keep last 24 snapshots
```

List snapshots (newest-first):

```bash
hookwise log
```

Diff two snapshots by row-count deltas per table:

```bash
hookwise diff latest prev
hookwise diff <timestamp-prefix-a> <timestamp-prefix-b>
```

---

← [Back to Home](/)
