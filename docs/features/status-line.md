# Composable Status Line

Composable segments you can mix and match to build a personalized status line.

<div align="center">
<img src="../../screenshots/status-line-demo.gif" alt="Status line color progression from green to red" width="700">
<br>
<em>Status line adapts -- green at start, yellow mid-session, red when overdue</em>
</div>

## Built-in Segments

| Segment | Shows |
|---------|-------|
| `cost` | Today's estimated cost (from the analytics DB; omitted when $0) |
| `project` | Repo + branch + last commit (derived live from the current directory) |
| `calendar` | Current/next event |
| `weather` | Local temperature, conditions, and wind indicator |
| `news` | Hacker News headlines |
| `insights` | Claude Code usage summary |
| `insights_friction` | Friction health: warns on recent friction, shows clean status otherwise |
| `insights_pace` | Productivity: messages/day, total lines, session count |
| `insights_trend` | Patterns: top tools and peak coding hour |

Feed-backed segments read from the [feed daemon's](feeds.md) cache and silently disappear when their feed is stale, disabled, or has no real data (fail-open).

A cross-session **fleet badge** is appended automatically when 2 or more Claude Code sessions are live — it is not configured as a segment.

## Configuration

```yaml
status_line:
  enabled: true
  segments:
    - cost
    - project
    - calendar
```

Unknown segment names render as a gray placeholder label. The old `session` segment was removed — configs that still list it render nothing for it.

---

← [Back to Home](/)
