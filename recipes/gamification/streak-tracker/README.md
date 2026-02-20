# Streak Tracker

Gamification recipe that tracks daily activity streaks and celebrates milestones.

## Tracked Streaks

- **Coding:** Consecutive days with Write/Edit tool usage
- **Testing:** Consecutive days with test commands (vitest, jest, pytest, etc.)
- **AI Ratio:** Consecutive days keeping AI authorship below threshold

## Milestones

Default: 7, 14, 30, 60, 90, 365 days

## Configuration

```yaml
include:
  - recipes/gamification/streak-tracker

# Override in hookwise.yaml:
# config.aiRatioThreshold: 0.7
# config.milestones: [7, 14, 30, 100]
```
