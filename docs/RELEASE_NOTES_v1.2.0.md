# hookwise v1.2.0 — Insights Producer

**Release date:** February 2026
**Previous version:** 1.1.0

---

## What's New in v1.2.0

hookwise now reads your Claude Code usage data and surfaces actionable insights directly in the status line. The new **Insights Producer** aggregates session metrics from `~/.claude/usage-data/`, computes friction patterns, pace statistics, and tool trends, then renders them through three new status line segments -- all without leaving the terminal.

No new dependencies. No new infrastructure. The insights producer slots into the existing feed platform alongside pulse, project, calendar, and news.

---

## Features Added

### Insights Producer

A new built-in feed producer that reads Claude Code's local usage data:

- **Reads** `~/.claude/usage-data/session-meta/*.json` (per-session metadata: tool counts, duration, lines added, message timestamps) and `~/.claude/usage-data/facets/*.json` (per-session friction analysis, outcomes, goal categories)
- **Computes** aggregated metrics across all sessions within a configurable staleness window (default: 30 days): total sessions, messages, lines added, average duration, top 5 tools, friction counts by category, peak coding hour, days active, and a recent session summary
- **Self-cleans** by excluding sessions older than the staleness window on every poll. No manual garbage collection, no intermediate cache state -- each poll does a full scan and produces a fresh aggregate
- **Fail-open** on all errors: malformed JSON files are skipped, missing directories return null, and segments gracefully disappear when data is unavailable
- **Read-only** access to Claude Code's usage-data directory. hookwise never writes to it

### 3 New Status Line Segments (19 total, up from 16)

| Segment | Key | What it shows |
|---------|-----|---------------|
| Friction Health | `insights_friction` | Friction events in the most recent session, or a clean-session indicator with lifetime friction total |
| Pace Metrics | `insights_pace` | Messages per day, total lines added, and session count across the staleness window |
| Tool Trends | `insights_trend` | Top 2 most-used tools and peak coding time of day |

All three segments check cache freshness via `isFresh()` and return an empty string when data is stale or missing, following the existing fail-open pattern.

### Friction Alert Recipe

A new built-in recipe at `recipes/productivity/friction-alert/` that fires on the `Stop` event (end of a Claude Code session):

- Reads the `insights` cache key from the cache bus
- If the most recent session had friction events meeting or exceeding the configured threshold (default: 3), prints a non-blocking warning identifying the count and top friction category
- Silent when below threshold, when cache is missing, or when cache is stale -- no false alarms
- Advisory only: never blocks execution

---

## Configuration

The insights feed is **enabled by default** when you run `hookwise init`. Add or customize it in `hookwise.yaml`:

```yaml
feeds:
  insights:
    enabled: true
    interval_seconds: 120
    staleness_days: 30
    usage_data_path: "~/.claude/usage-data"
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable or disable the insights producer |
| `interval_seconds` | number | `120` | How often the daemon polls usage data (seconds) |
| `staleness_days` | number | `30` | Only aggregate sessions from the last N days |
| `usage_data_path` | string | `~/.claude/usage-data` | Path to Claude Code's usage data directory |

To add the insights segments to your status line:

```yaml
status_line:
  enabled: true
  segments:
    - insights_friction
    - insights_pace
    - insights_trend
```

To include the friction alert recipe:

```yaml
includes:
  - recipes/productivity/friction-alert
```

And configure its threshold in the recipe's `hooks.yaml` or override it in your project config:

```yaml
# recipes/productivity/friction-alert/hooks.yaml
config:
  enabled: true
  threshold: 3
```

---

## Example Status Line Output

### insights_friction

When the most recent session had friction events:

```
⚠️ 5 friction in last session
```

When the most recent session was clean but there is historical friction:

```
✅ Clean session | 12 total friction
```

When no friction has been detected across the staleness window:

```
✅ No friction detected
```

### insights_pace

Shows messages-per-day average, total lines added (with `k` suffix for thousands), and session count:

```
📊 47 msgs/day | 28k+ lines | 104 sessions
```

For smaller codebases or fewer sessions:

```
📊 12 msgs/day | 830+ lines | 9 sessions
```

### insights_trend

Shows the top 2 most-used tools and peak coding time of day (morning: 6-12, afternoon: 12-18, evening: 18-24, night: 0-6):

```
🔧 Top: Bash, Read | Peak: afternoon
```

```
🔧 Top: Edit, Write | Peak: evening
```

### Friction Alert Recipe Output

When friction meets or exceeds the threshold, printed at session end:

```
⚡ Pattern detected: 5 friction events in last session. Top friction: wrong_approach. Consider research-first prompting.
```

---

## Upgrade Guide

### For existing hookwise users

The insights producer is **enabled by default** in hookwise's default configuration. After upgrading to v1.2.0:

1. The daemon will automatically start polling `~/.claude/usage-data/` every 120 seconds
2. If you have `insights_friction`, `insights_pace`, or `insights_trend` in your `status_line.segments` list, they will render automatically
3. The friction-alert recipe is opt-in -- add it to your `includes` list if you want session-end friction warnings

**No configuration changes are required.** The insights producer will silently return empty data if `~/.claude/usage-data/` does not exist or contains no sessions within the staleness window.

### To disable insights

Set `enabled: false` in your `hookwise.yaml`:

```yaml
feeds:
  insights:
    enabled: false
```

### Requirements

- Claude Code must have usage data at `~/.claude/usage-data/` (created automatically by Claude Code during normal usage)
- The hookwise daemon must be running (`hookwise daemon start`)
- Node.js >= 20.0.0

---

## Stats

| Metric | Value |
|--------|-------|
| New tests | 105 |
| Total tests | 1,363 (up from 1,258) |
| Total test files | 64 (up from 56) |
| New source files | 10 |
| Modified source files | 8 |
| Version | 1.1.0 → 1.2.0 |
| New segments | 3 (insights_friction, insights_pace, insights_trend) |
| Total segments | 19 (up from 16) |
| New recipes | 1 (friction-alert) |
| Total recipes | 12 (up from 11) |
| New dependencies | 0 |
