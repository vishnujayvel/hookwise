# Troubleshooting

Common issues and their solutions.

## Daemon Not Starting

**Symptom:** `hookwise daemon start` exits without starting the daemon.

**Check for an existing daemon:**

```bash
hookwise daemon status
```

If a daemon is already running, stop it first:

```bash
hookwise daemon stop && hookwise daemon start
```

**Stale PID file:**

If the daemon crashed, the PID file may be left behind. The daemon auto-cleans stale PIDs, but you can manually remove it:

```bash
rm ~/.hookwise/daemon.pid
hookwise daemon start
```

**Check the log file:**

```bash
cat ~/.hookwise/daemon.log
```

Look for startup errors such as missing config, invalid feed definitions, or permission issues.

## Stale Feed Data

**Symptom:** Status line shows outdated information. `hookwise feeds` shows feeds as stale.

**Is the daemon running?**

```bash
hookwise daemon status
```

If stopped, restart it:

```bash
hookwise daemon start
```

**TTL too short:**

If a feed's `interval_seconds` is longer than its TTL in the cache, data will appear stale between polls. The cache entry TTL is set by the producer -- ensure your intervals match your expectations.

**Feed producer failing:**

Check the daemon log for errors from specific producers:

```bash
cat ~/.hookwise/daemon.log | grep "error"
```

## No Usage Data Directory

**Symptom:** Insights feed shows no data. `hookwise stats` shows empty metrics.

The insights producer reads from `~/.claude/usage-data/`. This directory is created by Claude Code during active sessions. If you have not run a Claude Code session yet, it will not exist.

**Check if the directory exists:**

```bash
ls -la ~/.claude/usage-data/
```

If missing, run a Claude Code session. The directory will be created automatically.

**Custom path:**

If your usage data is in a non-standard location, configure it:

```yaml
feeds:
  insights:
    usage_data_path: "/custom/path/to/usage-data/"
```

## Permissions Issues

hookwise uses restrictive permissions for security:

| Resource | Permission | Meaning |
|----------|------------|---------|
| `~/.hookwise/` | `0o700` | Owner-only directory access |
| `analytics.db` | `0o600` | Owner-only read/write |
| `daemon.pid` | `0o600` | Owner-only read/write |
| `daemon.log` | `0o600` | Owner-only read/write |

**Symptom:** "Permission denied" errors when running hookwise commands.

**Fix permissions:**

```bash
chmod 700 ~/.hookwise
chmod 600 ~/.hookwise/analytics.db
chmod 600 ~/.hookwise/daemon.pid
chmod 600 ~/.hookwise/daemon.log
```

**Multiple users:**

hookwise is designed for single-user use. The state directory should be owned by your user account. Do not share `~/.hookwise/` across users.

## TUI Not Launching

**Symptom:** `hookwise tui` exits immediately or shows rendering errors.

**Terminal requirements:**

The TUI requires a terminal that supports:
- ANSI color codes
- Minimum 80 columns width
- Unicode characters

Most modern terminals work (iTerm2, Kitty, Windows Terminal, VS Code integrated terminal). Some minimal terminals or CI environments may not support the TUI.

**Check terminal size:**

```bash
tput cols
```

If less than 80, resize your terminal window.

## Guard Not Triggering

**Symptom:** A guard rule that should block/warn/confirm is being ignored.

**First-match-wins:**

Guard rules evaluate in order and the first matching rule wins. If a more permissive rule appears before your restrictive rule, it will match first.

Check rule order in your `hookwise.yaml`:

```yaml
guards:
  # This "allow" pattern matches first, so the block rule never fires
  - match: "Bash"
    action: warn
    reason: "Generic bash warning"

  # This rule is unreachable if the above matches
  - match: "Bash"
    action: block
    when: 'tool_input.command contains "rm -rf"'
    reason: "Dangerous command"
```

Move specific rules before general ones.

**Match pattern syntax:**

The `match` field uses picomatch glob patterns:

| Pattern | Matches |
|---------|---------|
| `"Bash"` | Exact tool name `Bash` |
| `"mcp__gmail__*"` | Any Gmail MCP tool |
| `"*"` | Any tool |

**Condition operators:**

The `when` and `unless` fields support 6 operators:

| Operator | Example |
|----------|---------|
| `contains` | `tool_input.command contains "rm"` |
| `starts_with` | `tool_input.file_path starts_with "/etc"` |
| `ends_with` | `tool_input.file_path ends_with ".env"` |
| `==` | `tool_name == "Bash"` |
| `equals` | `tool_name equals "Bash"` |
| `matches` | `tool_input.command matches "rm.*-rf"` |

**Test your rules:**

Use the TUI Guards tab (key `2`) to test specific tool calls against your rules, or use the testing API:

```go
tester, err := hwtesting.NewGuardTester("hookwise.yaml")
// handle err
result := tester.Evaluate("Bash", map[string]any{"command": "rm -rf /"})
// result.Action == "block", result.Reason == "..."
```

## "Command Not Found: hookwise"

Ensure the hookwise binary is on your PATH:

```bash
which hookwise
```

If not found, check where you placed the binary and add that directory to your PATH. If you installed via `task install` or `make install`, the binary is at the location shown by `brew --prefix`/bin/hookwise.

Add the directory to your `~/.zshrc` or `~/.bashrc` to make it permanent.

## Config Validation Warnings

Run `hookwise doctor` to see detailed validation results with path references and suggestions for each issue.
