# Builder Trap Detection

Detects when the user has been in a continuous building session for too long
without stepping back to think about architecture or design.

## What it does

- Tracks session duration and alerts at configurable thresholds
- Yellow (30 min), Orange (60 min), Red (90 min) alert levels
- Shows builder trap status in the status line

## Usage

```yaml
includes:
  - "builtin:behavioral/builder-trap-detection"
```

## Customization

```yaml
includes:
  - "builtin:behavioral/builder-trap-detection"

coaching:
  builder_trap:
    yellow_threshold_minutes: 45
    orange_threshold_minutes: 90
    red_threshold_minutes: 120
```
