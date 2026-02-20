# Metacognition Prompts

Behavioral recipe that provides time-gated cognitive nudges during coding sessions.

## How It Works

- Monitors PostToolUse events
- After a configurable interval (default: 5 minutes), emits a reflective prompt
- Cycles through a list of prompts to avoid repetition

## Configuration

```yaml
include:
  - recipes/behavioral/metacognition-prompts

# Override in hookwise.yaml:
# config.intervalSeconds: 600
# config.prompts:
#   - "Are you solving the right problem?"
#   - "When was the last time you ran tests?"
```
