<div align="center">

```
 _                 _            _
| |__   ___   ___ | | ____      _(_)___  ___
| '_ \ / _ \ / _ \| |/ /\ \ /\ / / / __|/ _ \
| | | | (_) | (_) |   <  \ V  V /| \__ \  __/
|_| |_|\___/ \___/|_|\_\  \_/\_/ |_|___/\___|
```

**Guardrails & policy engine for Claude Code.**

[![CI](https://img.shields.io/github/actions/workflow/status/vishnujayvel/hookwise/ci.yml?branch=main)](https://github.com/vishnujayvel/hookwise/actions)
[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

<img src="screenshots/hero-banner.png" alt="hookwise — guardrails and policy engine for Claude Code" width="700">

</div>

## Stop the command before it runs

Your AI agent is one autocomplete away from `rm -rf`, a force push, or a `DROP TABLE`. hookwise puts a declarative policy layer between Claude Code and your shell:

```yaml
# hookwise.yaml
guards:
  - match: "Bash"
    action: block
    when: 'tool_input.command contains "rm -rf"'
    reason: "Dangerous command blocked"
```

When Claude tries it, the tool call is denied before execution and Claude sees your reason — no bash scripting, no wrapper processes, no cloud service. One YAML file, evaluated locally on every `PreToolUse` event.

<img src="screenshots/guard-demo.gif" alt="Guard blocking a dangerous rm -rf" width="700">

### Guard semantics

- **Three actions**: `block` (deny the tool call), `confirm` (force Claude Code's permission prompt), `warn` (inject a warning into Claude's context and continue).
- **Operators**: `contains`, `starts_with`, `ends_with`, `equals`/`==`, and `matches` (regex). Tool matching (`match:`) takes exact names or globs (`mcp__*`).
- **First-match-wins** — rules read top-to-bottom like a firewall.
- **Fail-open by design** — if hookwise itself errors, the dispatcher exits 0 and your session keeps working. A safety tool that can break your editor isn't a safety tool.
- **Local and declarative** — rules live in your repo, review like code, and need no network.

<div align="center">
<img src="screenshots/guard-evaluation-flow.png" alt="Guard evaluation flow — hookwise.yaml rules are matched by tool name, then condition, resulting in block, confirm, or warn actions" width="500">
</div>

### Without hookwise

```bash
# .claude/settings.json — one script per guard, scattered across your project
"PreToolUse": [{ "command": "bash scripts/check-rm.sh" }]

# scripts/check-rm.sh  (repeat for every rule...)
#!/bin/bash
INPUT=$(cat)
CMD=$(echo "$INPUT" | jq -r '.tool_input.command // ""')
if echo "$CMD" | grep -q "rm -rf"; then
  echo '{"decision":"block","reason":"dangerous"}'
fi
```

With hookwise: add a rule, remove a rule, done. Claude Code reads the YAML, understands it, and can even help you write new rules.

## How It Compares

| | hookwise | Raw hook scripts | Status line tools |
|---|---------|-----------------|------------------|
| Guard rails | Declarative YAML | Manual bash | No |
| Testing | GuardTester + HookRunner | Manual | N/A |
| Analytics | SQLite, queryable | DIY | Display-only |
| Cost tracking | Budgets + alerts | DIY | Current session only |
| Configuration | One YAML file | Scattered scripts | JSON/TUI |
| Recipes | 10 built-in, shareable | N/A | N/A |

## Quick Start

```bash
# Install (macOS / Linux · arm64 or amd64):
curl -fsSL https://raw.githubusercontent.com/vishnujayvel/hookwise/main/scripts/install.sh | sh

# …or build/install with Go:
go install github.com/vishnujayvel/hookwise/cmd/hookwise@latest

hookwise init          # scaffold hookwise.yaml + inventory your existing hooks
hookwise init --wire   # wire dispatch + status line into .claude/settings.json
hookwise doctor
```

> Prebuilt binaries (darwin/linux × amd64/arm64) are published to [GitHub Releases](https://github.com/vishnujayvel/hookwise/releases). The installer grabs the right one for your platform.

`hookwise init --wire` registers the dispatcher and status line in `.claude/settings.json` for you — idempotently, with a pre-flight safety audit, an automatic backup, and `--dry-run` / `--unwire` escape hatches. A single dispatcher binary understands all 13 hook events; `--wire` registers `PreToolUse` and `PostToolUse` by default, and `--events` adds more. Prefer to edit settings yourself? [Manual setup &rarr;](docs/guide/getting-started.md)

Before it writes anything, `hookwise init` also inventories the hooks you already have (user settings are never modified) and saves the pre-init snapshot to `~/.hookwise/hook-audit.json`.

## Cost Analytics & Budgets

Every session is tracked in a local SQLite database: tool calls, file edits, duration, and per-model token cost computed from your actual Claude Code transcripts.

```bash
hookwise stats     # today's sessions, cost, tool breakdown, dispatch latency
```

<img src="screenshots/stats.png" alt="hookwise stats" width="600">

Set a daily budget and pick how hard it bites:

```yaml
cost_tracking:
  enabled: true
  daily_budget: 25.00
  enforcement: warn      # notify when you cross the line (default)
  # enforcement: enforce # hard-deny further tool calls until tomorrow
```

In `enforce` mode the budget is a guard, not a suggestion: once today's spend crosses the limit, `PreToolUse` calls are denied with the reason. `hookwise diff` and `hookwise log` compare analytics snapshots over time.

> Why measure? Developers in a randomized trial predicted AI made them 24% faster — they were 19% slower, and still believed they were faster ([METR RCT, 2025](https://metr.org/blog/2025-07-10-early-2025-ai-experienced-os-dev-study/)). Local data closes the perception gap.

## Status Line with Live Feeds

Composable segments for Claude Code's status bar, powered by a [background daemon](docs/features/feeds.md) — the piece no display-only statusline tool has. Mix `cost`, `project`, `calendar`, `weather`, `news`, and `insights`:

```yaml
status_line: { enabled: true, segments: [cost, project, calendar, weather] }
```

<img src="screenshots/status-line-insights.gif" alt="hookwise status line — friction tips, pace metrics, and calendar awareness" width="700">

The daemon is a shared singleton: it polls 6 built-in feed producers (plus your custom feeds) on staggered intervals and writes an atomic JSON cache with per-key TTL. Segments read from cache with freshness checks; a stale or unavailable feed silently disappears instead of breaking your prompt (fail-open again). Cost is computed live from the analytics DB at render time. When two or more sessions are active, a fleet badge shows cross-session status (`fleet run:2 done:1`).

Feeds are configured once, in the global `~/.hookwise/config.yaml` — `hookwise doctor` reports feed health against the daemon's *actual* runtime config and warns if a project file carries a `feeds:` block (which the daemon ignores).

<div align="center">
<img src="screenshots/feed-architecture.png" alt="Feed platform architecture — data sources flow through a background daemon into an atomic cache bus, then to the status line segments" width="600">
</div>

## Keep the Whole Hook Setup Healthy

**`hookwise audit`** scans your Claude Code settings files and flags hook-safety issues: sprawl, missing binaries, network-dependent hooks on hot paths, and duplicate or overlapping hooks. `--json` emits a schema-versioned report for CI (exits 0 on PASS/WARN, 1 on FAIL); `--fix` interactively removes duplicates; `--project-dir` scans a project's `.claude/` settings instead of your user-level settings.

**`hookwise doctor`** checks your config, state directory, analytics DB, daemon liveness, and per-feed health — measured against the daemon's *effective* runtime config, not just what's on disk. It's honest about disabled subsystems: a feed you turned off reports as `disabled`, and $0 with cost tracking off is "expected", not a malfunction warning.

<div align="center">
<img src="screenshots/doctor-v1.3.png" alt="hookwise doctor output — all checks passed" width="600">
</div>

## How It Works

Every hook event passes through a three-phase execution engine. Guards protect, context enriches, side effects observe. If anything errors, it fails open — your AI keeps working.

<div align="center">
<img src="screenshots/execution-engine.png" alt="Three-phase execution engine — Phase 1: Guards (block/confirm/warn), Phase 2: Context (enrich tool calls), Phase 3: Side Effects (log, analytics). Fail-open: any error exits 0." width="650">
</div>

## Configuration

Everything lives in `hookwise.yaml`. [Full reference &rarr;](docs/guide/getting-started.md)

```yaml
version: 1
guards:
  - match: "Bash"
    action: block
    when: 'tool_input.command contains "rm -rf"'
    reason: "Dangerous command blocked"
analytics: { enabled: true }
status_line: { enabled: true, segments: [cost, project, calendar] }
```

Global config at `~/.hookwise/config.yaml` applies everywhere. Project-level `hookwise.yaml` overrides per workspace — with one exception: the `feeds:` block only takes effect in the global config, because the feed daemon is a shared singleton.

## Testing Guards Like Code

```go
import hwtesting "github.com/vishnujayvel/hookwise/pkg/hookwise/testing"

tester, err := hwtesting.NewGuardTester("hookwise.yaml")
// handle err
result := tester.Evaluate("Bash", map[string]any{"command": "rm -rf /"})
assert.Equal(t, "block", result.Action)
```

Go test helpers in `pkg/hookwise/testing`, plus `hookwise test` for YAML-defined guard scenarios. [Details &rarr;](docs/cli.md)

## Recipes

10 built-in — [see all](recipes/) or [create your own](docs/guide/creating-a-recipe.md): `block-dangerous-commands`, `secret-scanning`, `commit-without-tests`, `cost-tracking`, and more. Ready-made hook configs you can copy into a project and adapt.

## Maturity: What's Solid, What's Early

Honest labels, so you know what you're opting into:

- **Stable** — guards, dispatch engine, cost analytics, status line + feed daemon, `init`/`doctor`/`audit`. This is the core, and it's what the test suite (unit, contract, integration, chaos, mutation) hammers on.
- **Early / power-user opt-in** — the **interactive TUI**: a full-screen Python/[Textual](https://textual.textualize.io) dashboard with 7 tabs. It ships **separately** from the core binary (the core CLI needs no Python). Install from the `tui/` directory (e.g. `uv tool install ./tui`); auto-launch is **off by default** (`tui.auto_launch: true` to opt in).
- **Experimental** — **notifications**: today this is a single in-app producer (daily budget-threshold alert) surfaced via `hookwise notifications` and a status-line badge. There are no push/email/Slack channels, and we've deliberately de-scoped delivery — Claude Code ships native push notifications itself (since v2.1.110), so hookwise focuses on deciding *what's worth alerting on*, not on the pipe.
- **Narrow** — behavioral "coaching" exists only as two opt-in recipes (`builder-trap-detection`, `ai-dependency-tracker`), not as a first-class subsystem.

<div align="center">
<img src="screenshots/tui-insights.png" alt="hookwise TUI — Claude Code usage insights with session metrics, trends, and tool breakdown" width="700">
</div>

## Security

hookwise runs inside your Claude Code session — security is non-negotiable. The full codebase (~80 source files) is reviewed through a dedicated security pipeline on every release:

- **4-domain parallel review** covering core engine, CLI, feed producers, and Python TUI
- **False-positive filtering** with strict exploitability criteria (confidence >= 8/10)
- **Zero confirmed vulnerabilities** in the latest full-package audit (v1.3.0)

Key design choices: parameterized SQL everywhere, safe YAML parsing only, restrictive file permissions (0o600/0o700), fail-open architecture, and Go binary releases via [GitHub Releases](https://github.com/vishnujayvel/hookwise/releases).

[Full security policy, trust model, and reporting instructions &rarr;](SECURITY.md)

## Documentation

| Guide | Reference |
|-------|-----------|
| [Getting Started](docs/guide/getting-started.md) | [Guards](docs/features/guards.md) |
| [Creating a Recipe](docs/guide/creating-a-recipe.md) | [TUI Guide](docs/reference/tui-guide.md) |
| [Architecture](docs/architecture.md) | [Feeds](docs/features/feeds.md) |
| [Philosophy](docs/philosophy.md) | [Status Line](docs/features/status-line.md) |
| [CLI Reference](docs/cli.md) | [Analytics](docs/features/analytics.md) |

## Contributing

`git clone`, `go test -race ./...`, `task pr`. See [CONTRIBUTING.md](CONTRIBUTING.md).

[MIT](LICENSE) — Built by [Vishnu](https://github.com/vishnujayvel). *Guard rails should be boring. The exciting part is what you build when you're not worried about what your AI is doing.*
