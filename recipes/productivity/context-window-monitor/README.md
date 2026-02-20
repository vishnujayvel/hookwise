# Context Window Monitor

Productivity recipe that tracks estimated context window usage and warns at thresholds.

## Thresholds

- **Warning (60%):** Informational message
- **Critical (80%):** Strong suggestion to compact or start new session

## How It Works

- Estimates token count from tool input/output sizes
- Accumulates across PostToolUse events
- Logs PreCompact events with pre/post context estimates
- Resets estimate after compaction (assumes ~30% retention)

## Configuration

```yaml
include:
  - recipes/productivity/context-window-monitor

# Override in hookwise.yaml:
# config.warningThreshold: 0.5
# config.criticalThreshold: 0.75
# config.maxTokens: 128000
```
