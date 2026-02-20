# Cost Tracking

Compliance recipe that estimates token costs and enforces daily budget limits.

## How It Works

- Estimates token count from tool input size
- Accumulates daily and session costs
- Warns or blocks when daily budget is exceeded

## Configuration

```yaml
include:
  - recipes/compliance/cost-tracking

# Override in hookwise.yaml:
# config.dailyBudget: 20
# config.enforcement: enforce  # or "warn"
```
