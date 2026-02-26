# Migration from Python (v0.1.0)

This guide walks you through migrating from the original Python hookwise (v0.1.0) to the TypeScript rewrite (v1.0).

## What Changed

| Aspect | v0.1.0 (Python) | v1.0 (TypeScript) |
|--------|-----------------|-------------------|
| Runtime | Python 3.10+ | Node.js 20+ |
| Install | `pip install hookwise` | `npm install -g hookwise` |
| Config format | hookwise.yaml (snake_case) | hookwise.yaml (snake_case, same file) |
| Test framework | pytest | vitest |
| Testing API | `GuardTester` (Python) | `GuardTester`, `HookRunner`, `HookResult` |
| TUI | Textual | React Ink |
| Recipes | 7 built-in | 11 built-in |
| Event support | 7 events | All 13 Claude Code events |
| Package format | pip/PyPI | npm |

## What Stayed the Same

- **hookwise.yaml format** -- Your config file works as-is. The YAML schema is backward compatible.
- **Guard rule syntax** -- Same `match`, `action`, `when`, `unless`, `reason` fields.
- **Condition operators** -- Same 6 operators: `contains`, `starts_with`, `ends_with`, `==`, `equals`, `matches`.
- **Three-phase execution** -- Guards, context injection, side effects.
- **Fail-open philosophy** -- Errors never accidentally block tool calls.

## Step-by-Step Migration

### 1. Install TypeScript hookwise

```bash
# Remove Python version
pip uninstall hookwise

# Install TypeScript version
npm install -g hookwise
```

### 2. Run the migration command

hookwise includes an automated migration tool:

```bash
hookwise migrate
```

This will:
- Detect your existing `hookwise.yaml`
- Check for Python handler scripts (`.py` files)
- Suggest the `python3` prefix for any Python scripts
- Validate your config against the v1.0 schema
- Report any incompatibilities

### 3. Update handler scripts

If you have custom Python handlers, you have two options:

**Option A: Keep Python handlers (recommended for existing scripts)**

hookwise v1.0 automatically adds the `python3` prefix to `.py` handler commands:

```yaml
# v0.1.0 config
handlers:
  - name: my-guard
    type: script
    events: [PreToolUse]
    command: hooks/guard.py        # <-- auto-converted to "python3 hooks/guard.py"
```

No changes needed -- the backward compatibility layer handles this.

**Option B: Rewrite in TypeScript/Node**

For better performance and tighter integration, rewrite handlers in TypeScript:

```typescript
// hooks/guard.ts
import { readFileSync } from "node:fs";

const input = readFileSync(0, "utf-8");
const payload = JSON.parse(input);

if (payload.tool_name === "Bash") {
  const cmd = payload.tool_input?.command ?? "";
  if (cmd.includes("rm -rf")) {
    process.stdout.write(
      JSON.stringify({ decision: "block", reason: "Dangerous command" })
    );
    process.exit(0);
  }
}

// Allow by default
process.stdout.write(JSON.stringify({}));
```

Update your config:

```yaml
handlers:
  - name: my-guard
    type: script
    events: [PreToolUse]
    command: "node hooks/guard.ts"  # or use tsx for TypeScript
```

### 4. Update Claude Code settings

Replace the Python dispatch command with the Node.js one:

```json
{
  "hooks": {
    "PreToolUse": [
      { "command": "hookwise dispatch PreToolUse" }
    ]
  }
}
```

The CLI command is the same (`hookwise dispatch <event>`) -- only the underlying runtime changed.

### 5. Update test scripts

Replace pytest-based tests with vitest:

**Before (Python):**
```python
from hookwise.testing import GuardTester

def test_blocks_rm_rf():
    tester = GuardTester("hookwise.yaml")
    result = tester.evaluate("Bash", {"command": "rm -rf /"})
    assert result.blocked
```

**After (TypeScript):**
```typescript
import { GuardTester } from "hookwise/testing";
import { describe, it, expect } from "vitest";

describe("guards", () => {
  it("blocks rm -rf", () => {
    const tester = new GuardTester("hookwise.yaml");
    const result = tester.evaluate("Bash", { command: "rm -rf /" });
    expect(result.action).toBe("block");
  });
});
```

### 6. Verify

```bash
# Check config is valid
hookwise doctor

# Run your guard tests
npx vitest run

# View stats (if analytics was enabled)
hookwise stats
```

## Config Key Mapping

hookwise v1.0 accepts both snake_case (YAML convention) and camelCase. The config engine automatically converts between them.

| v0.1.0 (snake_case) | v1.0 (camelCase internally) |
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

## Analytics Data

The v1.0 analytics uses SQLite (same as v0.1.0) but the schema has been enhanced. Your existing analytics data will continue to work, but new columns are available for:

- AI Confidence Score (per-file authorship classification)
- Cost tracking
- Multi-agent observability

## Troubleshooting

**"command not found: hookwise"**
Make sure the npm global bin is in your PATH:
```bash
export PATH="$(npm config get prefix)/bin:$PATH"
```

**"Cannot find module" errors**
hookwise v1.0 requires Node.js 20+. Check your version:
```bash
node --version
```

**Python handler still referenced**
The v0.1.0 compatibility layer auto-adds `python3` prefix. If your Python handler needs a specific interpreter, update the command explicitly:
```yaml
command: "/usr/local/bin/python3.11 hooks/guard.py"
```

**Config validation warnings**
Run `hookwise doctor` to see detailed config validation with path and suggestions for each issue.
