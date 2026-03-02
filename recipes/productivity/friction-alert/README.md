# Friction Alert

Productivity recipe that warns when friction patterns are detected in recent Claude Code sessions.

## How It Works

- Fires on the `Stop` event (end of a Claude Code session)
- Reads the `insights` cache key from the cache bus (written by the insights feed producer)
- If `recent_session.friction_count` meets or exceeds the threshold, prints a non-blocking warning with the top friction category
- If the cache is missing, stale, or friction is below threshold, the recipe is silent

## Example Output

```text
⚡ Pattern detected: 5 friction events in last session. Top friction: wrong_approach. Consider research-first prompting.
```

## Configuration

```yaml
includes:
  - recipes/productivity/friction-alert

# Override in hookwise.yaml:
# config.enabled: true
# config.threshold: 3
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable/disable the friction alert |
| `threshold` | number | `3` | Minimum friction count to trigger a warning |

## Prerequisites

The insights feed producer must be enabled for this recipe to function. The producer writes aggregated usage data (including friction counts) to the cache bus. Without it, the recipe has no data to read and remains silent.

```yaml
feeds:
  insights:
    enabled: true
```
