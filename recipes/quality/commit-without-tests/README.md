# Commit Without Tests Guard

Quality recipe that prevents git commits when no tests have been run.

## How It Works

- Tracks PostToolUse events for Bash commands matching test patterns
- On PreToolUse for `git commit`, checks if tests were run
- Blocks commit if no tests were run; warns if last tests failed

## Configuration

```yaml
include:
  - recipes/quality/commit-without-tests

# Override in hookwise.yaml:
# config.testPatterns: ["vitest", "jest", "pytest"]
# config.action: warn  # or "block"
```
