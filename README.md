<div align="center">

```
 _                 _            _
| |__   ___   ___ | | ____      _(_)___  ___
| '_ \ / _ \ / _ \| |/ /\ \ /\ / / / __|/ _ \
| | | | (_) | (_) |   <  \ V  V /| \__ \  __/
|_| |_|\___/ \___/|_|\_\  \_/\_/ |_|___/\___|
```

**Config-driven hook framework for Claude Code**

Guard rails, coaching, analytics, and an interactive TUI -- all from one YAML file.

[![npm version](https://img.shields.io/npm/v/hookwise.svg)](https://www.npmjs.com/package/hookwise)
[![CI](https://img.shields.io/github/actions/workflow/status/vishnujayvel/hookwise/ci.yml?branch=main)](https://github.com/vishnujayvel/hookwise/actions)
[![Tests](https://img.shields.io/badge/tests-920_passing-brightgreen.svg)]()
[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Node.js](https://img.shields.io/badge/node-%3E%3D20-339933.svg)](https://nodejs.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.9-3178C6.svg)](https://www.typescriptlang.org/)

</div>

---

## The Problem

You are pair-programming with Claude Code. It is 2am, you are in the zone, and your AI just ran `rm -rf /` while you were reading its previous output. Or it force-pushed to main. Or it has been "refactoring" the build system for 90 minutes while you assumed it was working on the feature.

Claude Code has a [hooks system](https://docs.anthropic.com/en/docs/claude-code/hooks) -- shell commands that fire on 13 different hook events. But writing hook scripts by hand means a pile of bash files, no consistency, and no way to share what works.

**hookwise** is one YAML file. Guards, analytics, coaching -- all in one place.

## Quick Start

```bash
# Install globally
npm install -g hookwise

# Initialize in your project
hookwise init --preset minimal

# Verify everything works
hookwise doctor
```

This creates `hookwise.yaml` in your project root and sets up the state directory. Then register hookwise in your Claude Code settings:

```json
{
  "hooks": {
    "PreToolUse": [{ "command": "hookwise dispatch PreToolUse" }],
    "PostToolUse": [{ "command": "hookwise dispatch PostToolUse" }],
    "SessionStart": [{ "command": "hookwise dispatch SessionStart" }],
    "SessionEnd": [{ "command": "hookwise dispatch SessionEnd" }],
    "Stop": [{ "command": "hookwise dispatch Stop" }],
    "Notification": [{ "command": "hookwise dispatch Notification" }],
    "SubagentStop": [{ "command": "hookwise dispatch SubagentStop" }]
  }
}
```

## Configuration

Everything lives in `hookwise.yaml`:

```yaml
version: 1

# Guard rules -- first match wins, like a firewall
guards:
  - match: "Bash"
    action: block
    when: 'tool_input.command contains "rm -rf"'
    reason: "Dangerous command blocked"

  - match: "Bash"
    action: confirm
    when: 'tool_input.command contains "--force"'
    reason: "Force flag requires confirmation"

  - match: "Read"
    action: warn
    when: 'tool_input.file_path ends_with ".env"'
    reason: "Accessing .env file -- may contain secrets"

  - match: "mcp__gmail__*"
    action: confirm
    reason: "Gmail tool requires confirmation"

# Ambient coaching -- nudges, not interruptions
coaching:
  metacognition:
    enabled: true
    interval_seconds: 300
  builder_trap:
    enabled: true
    thresholds:
      yellow: 30
      orange: 60
      red: 90

# Session analytics -- SQLite, queryable
analytics:
  enabled: true

# Cost tracking -- no surprise bills
cost_tracking:
  enabled: true
  daily_budget: 10
  enforcement: warn

# Status line -- pick your segments
status_line:
  enabled: true
  segments:
    - ai_ratio
    - session
    - builder_trap
    - cost

# Include recipes for reusable patterns
includes:
  - recipes/safety/block-dangerous-commands
  - recipes/behavioral/metacognition-prompts
```

### Presets

| Preset | What you get |
|--------|-------------|
| `minimal` | Guards only -- just the safety rails |
| `coaching` | Guards + metacognition + builder's trap + status line |
| `analytics` | Guards + SQLite session tracking |
| `full` | Everything enabled |

## Features

### Guard Rails

Declarative rules evaluated on every `PreToolUse` event. First matching rule wins (firewall semantics).

| Operator | Example |
|----------|---------|
| `contains` | `tool_input.command contains "rm -rf"` |
| `starts_with` | `tool_input.file_path starts_with "/etc"` |
| `ends_with` | `tool_input.file_path ends_with ".env"` |
| `equals` / `==` | `tool_name equals "Bash"` |
| `matches` | `tool_input.command matches "git push.*--force"` |

Three actions: **block** (reject the tool call), **confirm** (pause for human approval), **warn** (log and continue).

Glob patterns for tool names: `mcp__gmail__*` matches all Gmail MCP tools.

### Builder's Trap Detection

Monitors your tool usage patterns and categorizes them as coding, reviewing, or tooling. When you have been in "tooling mode" too long:

- **30 min** -- Yellow: "Is this moving the needle?"
- **60 min** -- Orange: "Time to refocus."
- **90 min** -- Red: "Stop and ask: what was my original goal?"

### Metacognition Coaching

Periodic prompts that interrupt autopilot mode:

- "Pause: Are you solving the right problem?"
- "What would you explain to a junior dev about this change?"
- "You just accepted a large AI change very quickly. Can you explain what it does?"

### Communication Coach

Grammar and communication analysis for interview prep and professional writing.

### Session Analytics

SQLite-backed tracking with `hookwise stats`:

- Session duration and tool call counts
- AI-vs-human authorship ratio with AI Confidence Score
- Tool breakdown by category
- Cost tracking with daily budget enforcement

### Composable Status Line

7 segments you can mix and match:

| Segment | Shows |
|---------|-------|
| `clock` | Current time |
| `session` | Duration + tool count |
| `ai_ratio` | AI-generated code percentage |
| `cost` | Session cost estimate |
| `builder_trap` | Alert level + nudge message |
| `mantra` | Rotating motivational prompt |
| `practice` | Daily practice rep counter |

### Interactive TUI

Full-screen terminal UI with 6 tabs:

| Key | Tab | Description |
|-----|-----|-------------|
| `1` | Dashboard | Session overview and quick status |
| `2` | Guards | Guard rules viewer and test interface |
| `3` | Coaching | Coaching status and configuration |
| `4` | Analytics | Session analytics and charts |
| `5` | Recipes | Browse and manage recipes |
| `6` | Status | Status line preview and configuration |

Press `q` or `Escape` to exit the TUI.

## Recipes

11 built-in recipes -- pre-configured patterns for common needs:

| Recipe | Category | What it does |
|--------|----------|-------------|
| `block-dangerous-commands` | safety | Blocks `rm -rf /`, `rm -rf ~`, force pushes |
| `secret-scanning` | safety | Detects and masks secrets in tool output |
| `ai-dependency-tracker` | behavioral | Tracks AI usage patterns over time |
| `metacognition-prompts` | behavioral | Periodic "step back and think" nudges |
| `builder-trap-detection` | behavioral | Detects over-engineering and scope creep |
| `cost-tracking` | compliance | API costs against daily budgets |
| `transcript-backup` | productivity | Saves session transcripts for review |
| `context-window-monitor` | productivity | Monitors context window usage |
| `streak-tracker` | gamification | Tracks coding streaks and consistency |
| `commit-without-tests` | quality | Warns when committing without running tests |
| `file-creation-police` | quality | Enforces file creation policies |

Include a recipe in your config:

```yaml
includes:
  - recipes/safety/block-dangerous-commands
  - recipes/behavioral/metacognition-prompts
```

Or create your own -- see [Creating a Recipe](docs/creating-a-recipe.md).

## CLI Commands

```
hookwise init [--preset minimal|coaching|analytics|full]
                          Generate hookwise.yaml and state directory

hookwise doctor           Health check: config, state dir, handlers

hookwise status           Show current configuration summary

hookwise stats            Session analytics: tool calls, authorship, cost

hookwise test             Run guard rule tests against scenarios

hookwise tui              Launch the interactive TUI for config management

hookwise migrate          Migrate from Python hookwise (v0.1.0) to TypeScript
```

## Testing Your Guards

hookwise includes testing utilities so you can validate guards in CI:

```typescript
import { GuardTester } from "hookwise/testing";

const tester = new GuardTester("hookwise.yaml");

// Test blocking
const blocked = tester.evaluate("Bash", { command: "rm -rf /" });
expect(blocked.action).toBe("block");

// Test allowing
const allowed = tester.evaluate("Bash", { command: "ls -la" });
expect(allowed.action).toBe("allow");
```

Three testing utilities are exported:

- **`GuardTester`** -- In-process guard rule evaluation (fast, no subprocess)
- **`HookRunner`** -- Subprocess-based hook execution (tests the real dispatch path)
- **`HookResult`** -- Assertion helpers (`assertBlocked()`, `assertAllowed()`, `assertWarns()`)

## Architecture

hookwise registers one dispatcher for all 13 Claude Code hook events:

```
13 Hook Events
  UserPromptSubmit, PreToolUse, PostToolUse, PostToolUseFailure,
  Notification, Stop, SubagentStart, SubagentStop, PreCompact,
  SessionStart, SessionEnd, PermissionRequest, Setup
              |
              v
    +-------------------+
    |    Dispatcher      |  <-- hookwise.yaml (YAML config)
    | (fail-open: any    |
    |  error -> exit 0)  |
    +---------+---------+
              |
     +--------+--------+
     |        |        |
     v        v        v
  Phase 1  Phase 2  Phase 3
  Guards   Context  Side Effects
  (block)  (enrich) (log, coach)
```

**Three-phase execution:**

1. **Guards** -- Decide if the tool call should proceed. First block wins (short-circuit). If any guard blocks, phases 2 and 3 are skipped.
2. **Context Injection** -- Enrich the tool call with additional context (greeting, metacognition prompts). Multiple context handlers merge their output.
3. **Side Effects** -- Non-blocking operations that observe and respond (analytics, coaching state, sounds, transcript backup).

**Fail-open guarantee:** Any unhandled exception anywhere in the dispatch pipeline results in `exit 0`. hookwise must never accidentally block a tool call due to internal errors.

**Config resolution:**
1. Global config (`~/.hookwise/config.yaml`)
2. Project config (`./hookwise.yaml`)
3. Deep merge: project values override global
4. Include resolution (recipes)
5. v0.1.0 backward compatibility transform
6. snake_case to camelCase conversion
7. Environment variable interpolation (`${VAR_NAME}`)
8. Defaults fill missing fields

## How It Compares

| | hookwise | Status line tools | Raw hook scripts |
|---|---------|------------------|-----------------|
| Guard rails | Declarative YAML | No | Manual bash |
| Session analytics | SQLite, queryable | Display-only | DIY |
| Coaching | Built-in | No | No |
| Configuration | One YAML file | JSON/TUI | Scattered scripts |
| Testing | GuardTester, HookRunner | N/A | Manual |
| Recipes | 11 built-in | N/A | N/A |
| Cost tracking | Budgets + alerts | Current session only | DIY |
| Interactive TUI | 6 tabs | N/A | N/A |

## Development

```bash
git clone https://github.com/vishnujayvel/hookwise.git
cd hookwise
npm install
npm test          # 920 tests via vitest
npm run build     # tsup build
npm run typecheck  # tsc --noEmit
```

### Project Structure

```
src/
  core/           # Dispatcher, config, guards, analytics, coaching
    analytics/    # SQLite analytics engine
    coaching/     # Metacognition, builder's trap, communication
    status-line/  # Composable status segments
  cli/            # CLI commands (init, doctor, status, stats, test, migrate)
    tui/          # Interactive TUI (React Ink)
  testing/        # HookRunner, HookResult, GuardTester

tests/            # 920 tests across 41 test files
  core/           # Unit tests for each module
  integration/    # End-to-end dispatch flow tests
  performance/    # Benchmarks and import boundary tests
  cli/            # CLI command tests
  tui/            # TUI component tests

recipes/          # 11 built-in recipes
examples/         # 4 example configs (minimal, coaching, analytics, full)
```

## Documentation

- [Getting Started](docs/getting-started.md)
- [Hook Events Reference](docs/hook-events-reference.md)
- [Creating a Recipe](docs/creating-a-recipe.md)
- [TUI Guide](docs/tui-guide.md)
- [Migration from Python](docs/migration-from-python.md)
- [Contributing](CONTRIBUTING.md)

## License

[MIT](LICENSE)

## Author

Built by [Vishnu](https://github.com/vishnujayvel). Born from watching Claude Code do amazing things -- and occasionally terrifying things.
