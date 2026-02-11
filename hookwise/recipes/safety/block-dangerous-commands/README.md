# Block Dangerous Commands

Blocks or requires confirmation for dangerous shell commands.

## What it does

- **Blocks** `rm -rf /` and `rm -rf ~` (recursive delete of root or home)
- **Confirms** `force push` and `--force` flag usage

## Usage

```yaml
includes:
  - "builtin:safety/block-dangerous-commands"
```

## Override example

To allow force pushes without confirmation in a specific project:

```yaml
includes:
  - "builtin:safety/block-dangerous-commands"

guards:
  # Your project guards override recipe guards
  - match: "Bash"
    action: warn
    when: 'tool_input.command contains "force push"'
    reason: "Force push detected (warning only)"
```
