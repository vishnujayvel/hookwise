# Block Dangerous Commands

Safety recipe that prevents execution of destructive shell commands.

## Blocked Patterns

- `rm -rf` / `rm -fr` — recursive forced deletion
- `--force` / `force push` — git force push
- `git reset --hard` — discard uncommitted changes
- `git clean -fd` — delete untracked files
- `git checkout .` — discard working tree changes
- `DROP TABLE` / `DROP DATABASE` / `TRUNCATE TABLE` — destructive SQL

## Configuration

```yaml
include:
  - recipes/safety/block-dangerous-commands

# Override patterns in your hookwise.yaml:
# config.patterns: ["rm -rf", "DROP TABLE"]
```

## How It Works

This recipe uses guard rules (PreToolUse event) to check Bash tool invocations.
The handler function also provides programmatic pattern matching for use in custom integrations.
