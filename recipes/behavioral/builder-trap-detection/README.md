# Builder's Trap Detection

Behavioral recipe that alerts when too much time is spent on tooling/infrastructure.

## Alert Levels

- **Yellow (30 min):** Gentle reminder
- **Orange (60 min):** Stronger nudge
- **Red (90 min):** Direct alert

## Configuration

```yaml
include:
  - recipes/behavioral/builder-trap-detection

# Override in hookwise.yaml:
# config.thresholds:
#   yellow: 20
#   orange: 45
#   red: 75
```
