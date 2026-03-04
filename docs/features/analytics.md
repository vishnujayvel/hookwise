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

---

← [Back to Home](/)
