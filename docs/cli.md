# CLI Commands

All commands are invoked as `hookwise <command>`.

## Commands

```
hookwise init             Scaffold hookwise.yaml, ensure the state directory, and
                          inventory existing hooks (report: ~/.hookwise/hook-audit.json)

hookwise init --wire      Wire dispatch + status line into .claude/settings.json
                          (idempotent; --dry-run to preview, --unwire to remove)

hookwise audit            Scan Claude Code hook config for health issues
                          (--json for CI; exits 0 on PASS/WARN, 1 on FAIL)

hookwise doctor           Health check: config, state dir, analytics DB, daemon,
                          per-feed health vs the daemon's effective config
```

<div align="center">
<img src="../screenshots/doctor-v1.2.png" alt="hookwise doctor output" width="600">
</div>

```
hookwise stats            Analytics dashboard for today: tool calls, duration, cost

hookwise test             Run guard rule tests against scenarios

hookwise dispatch <event> Dispatch a hook event (called by Claude Code)

hookwise status-line      Render the status line (called by Claude Code)

hookwise daemon <start|stop>
                          Manage the background feed daemon

hookwise snapshot         Point-in-time VACUUM INTO copy of the analytics DB

hookwise log              Show analytics snapshot history

hookwise diff <from> <to> Row-count changes between two analytics snapshots

hookwise notifications    Show recent notification history

hookwise upgrade          Migrate data from a TypeScript hookwise installation
```

See the [CLI Reference](reference/cli-reference.md) for per-command flags.

## Testing Utilities

hookwise includes Go test helpers so you can validate guards in CI:

```go
import hwtesting "github.com/vishnujayvel/hookwise/pkg/hookwise/testing"

func TestGuards(t *testing.T) {
    tester, err := hwtesting.NewGuardTester("hookwise.yaml")
    require.NoError(t, err)

    // Test blocking
    blocked := tester.Evaluate("Bash", map[string]any{"command": "rm -rf /"})
    assert.Equal(t, "block", blocked.Action)

    // Test allowing
    allowed := tester.Evaluate("Bash", map[string]any{"command": "ls -la"})
    assert.Equal(t, "allow", allowed.Action)
}
```

Test helpers available in `pkg/hookwise/testing`:

- **`GuardTester`** -- In-process guard rule evaluation (fast, no subprocess)
- **Contract tests** -- 33 JSON fixtures in `testdata/contracts/` for byte-identical output validation

## Interactive TUI

Full-screen terminal UI built with Python Textual, shipped **separately** from the core binary as `hookwise-tui` (the core CLI needs no Python) -- 7 tabs:

| Key | Tab | Description |
|-----|-----|-------------|
| `1` | Dashboard | Feature overview with enabled/disabled status |
| `2` | Guards | Guard rules table with action descriptions |
| `3` | Analytics | Sparkline trends, tool breakdown, cost tracking |
| `4` | Feeds | Live feed dashboard with auto-refresh and health indicators |
| `5` | Insights | Claude Code usage metrics, trends, and daily AI summary |
| `6` | Recipes | Recipe browser grouped by category |
| `7` | Status | Status line preview and segment configurator |

Press `q` to exit the TUI. Install from the `tui/` directory (e.g. `uv tool install ./tui`), then launch it with `hookwise tui` (or run `hookwise-tui` directly).

---

← [Back to Home](/)
