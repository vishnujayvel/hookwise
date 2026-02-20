# AI Dependency Tracker

Behavioral recipe that tracks the ratio of AI-authored vs human-authored code changes.

## How It Works

- Monitors PostToolUse events for Write/Edit/NotebookEdit tools
- Tracks lines changed per file and per session
- Warns when AI authorship ratio exceeds threshold (default: 80%)

## Configuration

```yaml
include:
  - recipes/behavioral/ai-dependency-tracker

# Override in hookwise.yaml:
# config.aiRatioThreshold: 0.7
# config.warnOnHighRatio: true
```
