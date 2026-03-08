# Migration from Python (v0.1.0)

This guide walks you through migrating from the original Python hookwise (v0.1.0) to the Go rewrite (v2.0).

## What Changed

| Aspect | v0.1.0 (Python) | v2.0 (Go) |
|--------|-----------------|-----------|
| Runtime | Python 3.10+ | Go binary (no runtime dependency) |
| Install | `pip install hookwise` | Download from [GitHub Releases](https://github.com/vishnujayvel/hookwise/releases) |
| Config format | hookwise.yaml (snake_case) | hookwise.yaml (snake_case, same file) |
| Test framework | pytest | `go test` + contract fixtures |
| Testing API | `GuardTester` (Python) | `hwtesting.GuardTester` (Go) |
| TUI | Textual | Python Textual (unchanged) |
| Recipes | 7 built-in | 12 built-in |
| Event support | 7 events | All 13 Claude Code events |
| Analytics | SQLite | Dolt (version-controlled SQL) |
| Distribution | pip/PyPI | Go binary via GitHub Releases |

## What Stayed the Same

- **hookwise.yaml format** -- Your config file works as-is. The YAML schema is backward compatible.
- **Guard rule syntax** -- Same `match`, `action`, `when`, `unless`, `reason` fields.
- **Condition operators** -- Same 6 operators: `contains`, `starts_with`, `ends_with`, `==`, `equals`, `matches`.
- **Three-phase execution** -- Guards, context injection, side effects.
- **Fail-open philosophy** -- Errors never accidentally block tool calls.
- **Python TUI** -- The Textual-based TUI remains Python, reading from Go's JSON cache.

## Step-by-Step Migration

### 1. Install Go hookwise

```bash
# Remove Python version
pip uninstall hookwise

# Download Go binary from GitHub Releases
# https://github.com/vishnujayvel/hookwise/releases
# Place it on your PATH (e.g., /usr/local/bin/hookwise)

# Or build from source:
git clone https://github.com/vishnujayvel/hookwise.git
cd hookwise
go build -ldflags "-X main.version=$(git describe --tags)" -o /usr/local/bin/hookwise ./cmd/hookwise/
```

### 2. Run the migration command

hookwise includes an automated migration tool:

```bash
hookwise migrate
```

This will:
- Replace the Python hookwise entry in Claude's `settings.json` with the Go binary
- Validate your existing `hookwise.yaml` against the current schema
- Report any incompatibilities

Use `--dry-run` to preview changes without applying them:

```bash
hookwise migrate --dry-run
```

### 3. Update handler scripts

If you have custom Python handlers, they continue to work — hookwise v2.0 automatically adds the `python3` prefix to `.py` handler commands:

```yaml
# v0.1.0 config — works as-is
handlers:
  - name: my-guard
    type: script
    events: [PreToolUse]
    command: hooks/guard.py        # auto-converted to "python3 hooks/guard.py"
```

### 4. Update Claude Code settings

The dispatch command is the same:

```json
{
  "hooks": {
    "PreToolUse": [
      { "command": "hookwise dispatch PreToolUse" }
    ]
  }
}
```

### 5. Verify

```bash
hookwise doctor
hookwise --version
```

## Config Key Mapping

hookwise v2.0 accepts both snake_case (YAML convention) and camelCase. The config engine automatically converts between them.

| v0.1.0 (snake_case) | v2.0 (camelCase internally) |
|---------------------|-----------------------------|
| `interval_seconds` | `intervalSeconds` |
| `builder_trap` | `builderTrap` |
| `tooling_patterns` | `toolingPatterns` |
| `practice_tools` | `practiceTools` |
| `log_level` | `logLevel` |
| `handler_timeout_seconds` | `handlerTimeoutSeconds` |
| `status_line` | `statusLine` |
| `cost_tracking` | `costTracking` |
| `transcript_backup` | `transcriptBackup` |

Write your YAML in snake_case -- it is the convention and hookwise handles the conversion.

## Troubleshooting

**"command not found: hookwise"**
Ensure the binary is on your PATH:
```bash
which hookwise
```

If not found, check where you placed the binary and add that directory to your PATH.

**Python handler still referenced**
The backward compatibility layer auto-adds `python3` prefix. If your Python handler needs a specific interpreter, update the command explicitly:
```yaml
command: "/usr/local/bin/python3.11 hooks/guard.py"
```

**Config validation warnings**
Run `hookwise doctor` to see detailed config validation with path and suggestions for each issue.
