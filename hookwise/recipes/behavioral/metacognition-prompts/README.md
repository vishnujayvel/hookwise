# Metacognition Prompts

Periodically prompts the user with metacognitive questions to encourage
reflection on their development process.

## What it does

- Enables coaching with metacognition prompts every 5 minutes
- Communication coach is disabled by default

## Usage

```yaml
includes:
  - "builtin:behavioral/metacognition-prompts"
```

## Customization

Override the interval in your project config:

```yaml
includes:
  - "builtin:behavioral/metacognition-prompts"

coaching:
  metacognition:
    interval_minutes: 10  # every 10 minutes instead of 5
```
