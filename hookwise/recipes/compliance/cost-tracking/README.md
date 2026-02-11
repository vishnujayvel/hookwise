# Cost Tracking

Tracks API usage costs and warns when approaching a daily budget.

## What it does

- Enables cost tracking with a $10.00/day default budget
- Warns at 80% of daily budget
- Shows cost in the status line

## Usage

```yaml
includes:
  - "builtin:compliance/cost-tracking"
```

## Customization

```yaml
includes:
  - "builtin:compliance/cost-tracking"

cost_tracking:
  daily_budget_usd: 25.00
  warn_at_percent: 90
```
