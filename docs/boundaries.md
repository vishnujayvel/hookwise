# Hookwise Boundaries & Control Diagram

## Purpose

This document defines what hookwise controls and what it does not. It exists for three reasons:

1. **Prevent scope creep.** The v1.3 retro identified feature scope without product validation as a recurring failure mode (Bug #167). This document provides a reference for evaluating whether a proposed feature belongs inside hookwise or outside it.
2. **Clarify contributor expectations.** New contributors should understand, before writing code, which layers hookwise owns versus merely reads from. A guard that blocks `rm -rf /` is in scope. A guard that rewrites the command to `rm -rf /tmp` is not.
3. **Protect the fail-open guarantee.** Every boundary hookwise crosses (writing to settings.json, spawning daemons, reading git state) is a potential failure point. Documenting these boundaries makes it explicit where fail-open wrappers are required.

---

## What Hookwise Controls (IN SCOPE)

### Configuration Layer

| Component | Source Files | Description |
|-----------|-------------|-------------|
| `hookwise.yaml` config | `src/core/config.ts` | Project-level and global YAML configuration with deep merge, env var interpolation, snake_case/camelCase conversion |
| Config validation | `src/core/config.ts` (`validateConfig`) | Structural validation of all 14 top-level sections with path-based error reporting |
| Config write-back | `src/core/config.ts` (`saveConfig`) | Atomic YAML serialization (temp file + rename) |
| Recipe system | `src/core/recipes.ts` | Load, validate, and merge recipes from `recipes/` directories (12 built-in across 6 categories: safety, quality, productivity, behavioral, compliance, gamification) |
| Include resolution | `src/core/config.ts` (`resolveIncludes`) | Merge included YAML files and recipe directories into the base config |
| Config migration | `src/core/config.ts` (`applyV010Compat`) | v0.1.0 backward compatibility transforms (Python-era `.py` script references) |

### Execution Layer

| Component | Source Files | Description |
|-----------|-------------|-------------|
| Three-phase dispatcher | `src/core/dispatcher.ts` | Entry point for all 13 Claude Code hook events: Guards -> Context Injection -> Side Effects |
| Guard evaluation | `src/core/guards.ts` | First-match-wins firewall rules with glob matching (picomatch), 6 condition operators (`contains`, `starts_with`, `ends_with`, `==`, `equals`, `matches`), `when`/`unless` clauses |
| Handler execution | `src/core/dispatcher.ts` | Executes builtin, script (child_process.spawnSync), and inline handler types with timeout enforcement |
| Context injection | `src/core/dispatcher.ts` (`executeContextPhase`) | Merges `additionalContext` strings from context-phase handlers into hook output |
| Greeting | `src/core/greeting.ts` | Weighted random quote selection from configured categories or custom quotes file |
| Metacognition prompts | `src/core/coaching/metacognition.ts` | Time-based and behavioral-trigger reminders (rapid acceptance detection, builder's trap alerts) |
| Communication coaching | `src/core/coaching/communication.ts` | Grammar analysis and communication style nudges |
| Builder's trap detection | `src/core/coaching/builder-trap.ts` | Mode classification (tooling vs. practice), time accumulation, alert level computation |

### Data Layer

| Component | Source Files | Description |
|-----------|-------------|-------------|
| Analytics SQLite DB | `src/core/analytics/db.ts` | Session tracking, tool call logging, authorship ledger (`~/.hookwise/analytics.db`) |
| Analytics engine | `src/core/analytics/session.ts` | Session start/end lifecycle, event recording |
| Stats queries | `src/core/analytics/stats.ts` | Daily summaries, tool breakdowns, authorship summaries |
| Coaching state | `src/core/coaching/` | JSON-based session state for metacognition intervals and builder's trap mode tracking |
| Cache bus | `src/core/feeds/cache-bus.ts` | Filesystem-based inter-process communication via atomic JSON reads/writes with per-key TTL |
| Transcript backup | `src/core/transcript.ts` | Timestamped JSON files of hook payloads with max directory size enforcement |
| Cost state | `src/core/cost.ts` | Per-session and daily cost accumulation with budget enforcement |
| Agent spans | `src/core/agents.ts` | Multi-agent lifecycle tracking in SQLite, file conflict detection, Mermaid diagram generation |

### Display Layer

| Component | Source Files | Description |
|-----------|-------------|-------------|
| Status line segments | `src/core/status-line/segments.ts` | 20 built-in segment renderers, each a pure function `(cache, config) => string` |
| Two-tier renderer | `src/core/status-line/two-tier.ts` | Line 1 (fixed: context_bar, mode_badge, cost, duration) + Line 2 (rotating: 9 contextual feeds) |
| ANSI color system | `src/core/status-line/ansi.ts` | Conditional ANSI colorization for segment output |
| TUI application | `tui/hookwise_tui/` | 8-tab Python Textual app (Dashboard, Guards, Coaching, Analytics, Status, Feeds, Insights, Recipes) with custom widgets (FeatureCard, FeedHealth, Sparkline) |
| TUI launcher | `src/core/tui-launcher.ts` | Detached process spawning with PID-based duplicate prevention, macOS Terminal.app integration |

### Feed Platform

| Component | Source Files | Description |
|-----------|-------------|-------------|
| Feed registry | `src/core/feeds/registry.ts` | Map-backed registration and lookup for feed definitions with duplicate rejection |
| Daemon process | `src/core/feeds/daemon-process.ts` | Background Node.js process that polls producers on staggered intervals |
| Daemon manager | `src/core/feeds/daemon-manager.ts` | Start/stop/status lifecycle management with PID file (signal 0 liveness checks) |
| 8 built-in producers | `src/core/feeds/producers/` | pulse (30s), project (60s), calendar (5m), news (30m), insights (2m), practice (2m), weather (10m), memories (1h) |
| Custom feed producer | `src/core/feeds/registry.ts` (`createCommandProducer`) | Wraps user-authored shell commands as feed producers |

### CLI Layer

| Component | Source Files | Description |
|-----------|-------------|-------------|
| `hookwise init` | CLI commands | Initialize hookwise.yaml and register hooks in settings.json |
| `hookwise doctor` | CLI commands | Validate config, check dependencies, verify hook registration |
| `hookwise status` | CLI commands | Show current status line output |
| `hookwise stats` | CLI commands | Query and display analytics data |
| `hookwise test` | CLI commands | Run guard rules against test scenarios |
| `hookwise daemon` | `src/cli/commands/daemon.ts` | Start/stop/status for the feed daemon |
| `hookwise setup` | `src/cli/commands/setup.ts` | Configure external integrations (Google Calendar OAuth) |
| `hookwise tui` | CLI commands | Launch the TUI application |
| `hookwise migrate` | CLI commands | Migrate config between versions |

### Testing Utilities

| Component | Source Files | Description |
|-----------|-------------|-------------|
| HookRunner | `src/testing/hook-runner.ts` | Programmatic hook dispatch for integration tests |
| HookResult | `src/testing/hook-result.ts` | Structured assertion helpers for dispatch results |
| GuardTester | `src/testing/guard-tester.ts` | Fluent API for testing guard rule evaluation |

---

## What Hookwise Does NOT Control (OUT OF SCOPE)

### Claude Code Internals

| Boundary | Why it is out of scope |
|----------|----------------------|
| Claude Code itself | Hookwise consumes hook events emitted by Claude Code but cannot modify Claude Code's behavior, model selection, or reasoning. Hookwise influences Claude Code only through the `additionalContext` field in hook output. |
| `settings.json` schema | Hookwise writes `hooks` and `statusLine.instructions` entries to `.claude/settings.json`, but Anthropic defines the schema. Hookwise must track schema changes reactively. |
| Hook event types | The 13 event types (`UserPromptSubmit`, `PreToolUse`, `PostToolUse`, etc.) are defined by Anthropic. Hookwise cannot create new event types or modify event payloads before they arrive. |
| Context window management | Hookwise can read context window usage (via `_stdin` cache) and display it, but cannot control compaction, context size, or model selection. |
| Tool call arguments | Hookwise can block, confirm, or warn on tool calls, but never modifies tool call arguments. The `PreToolUse` hook output supports `permissionDecision` (allow/deny/ask) but not argument rewriting. |
| Claude Code's prompt or system message | Hookwise injects `additionalContext` but does not control the base system prompt, model temperature, or other inference parameters. |

### External Services

| Boundary | Why it is out of scope |
|----------|----------------------|
| Google Calendar API | The calendar producer reads events but hookwise does not create, modify, or delete calendar events. OAuth credentials are stored locally; hookwise does not manage the Google Cloud project. |
| Hacker News API | The news producer reads top stories. Hookwise does not post, vote, or interact with HN. |
| Open-Meteo Weather API | The weather producer reads forecasts. Hookwise does not submit data. |
| Network connectivity | Feed producers degrade gracefully (return `null` on failure, TTL expiry hides stale data) but hookwise cannot diagnose or fix network issues. |
| RSS feeds | The news producer can consume RSS URLs but hookwise does not host or manage feed sources. |

### User Environment

| Boundary | Why it is out of scope |
|----------|----------------------|
| Git operations | The project producer reads git state (`git rev-parse`, `git log`) but hookwise never runs `git commit`, `git push`, `git checkout`, or any write operation against the repository. |
| User's source code | Hookwise observes tool calls (file edits, bash commands) via `PostToolUse` payloads but never reads, modifies, or creates files in the user's project. The only files hookwise writes are in `~/.hookwise/` and `.claude/settings.json`. |
| Other coding agents | The `AgentObserver` tracks sub-agent lifecycle spans for observability but does not start, stop, configure, or communicate with external agents (Gemini, Codex, Cursor agents). |
| IDE/editor state | Hookwise has no VS Code, Cursor, or JetBrains extension. It operates entirely through Claude Code's hook system and the terminal. |
| Terminal emulator | The TUI renders via Textual/Python in whatever terminal is available. Hookwise does not control terminal settings, font, or color scheme. |
| Python runtime | The TUI requires Python 3.10+ with Textual installed. Hookwise bundles a venv but falls back to system `python3`. Hookwise does not install or manage system Python. |
| Practice Tracker DB | The practice producer reads from `~/.practice-tracker/practice-tracker.db` but hookwise does not write to or manage this database. |

---

## Entity Relationships

### Data Flow Diagram (ASCII)

```
                                   HOOKWISE BOUNDARIES
  +---------------------------------------------------------------------------+
  |                                                                           |
  |   User                                                                    |
  |     |                                                                     |
  |     v                                                                     |
  |   Claude Code  (OUT OF SCOPE -- hookwise does not control)                |
  |     |                                                                     |
  |     | emits 13 hook events (stdin JSON)                                   |
  |     v                                                                     |
  | +----------------------------+                                            |
  | |  hookwise dispatcher       |  src/core/dispatcher.ts                    |
  | |  (entry point: dispatch()) |                                            |
  | +---+------+------+---------+                                             |
  |     |      |      |                                                       |
  |     v      v      v                                                       |
  |  Phase 1  Phase 2  Phase 3                                                |
  |  GUARDS   CONTEXT  SIDE EFFECTS                                           |
  |     |      |      |                                                       |
  |     |      |      +---> Analytics DB  (~/.hookwise/analytics.db)          |
  |     |      |      +---> Coaching State (JSON in ~/.hookwise/)             |
  |     |      |      +---> Transcript Backup (JSON files)                    |
  |     |      |      +---> Cost Accumulation                                 |
  |     |      |      +---> TUI Launch (on SessionStart)                      |
  |     |      |      +---> Sound Effects                                     |
  |     |      |                                                              |
  |     |      +---> additionalContext (greeting, metacognition, coaching)     |
  |     |                                                                     |
  |     +---> block / confirm / warn  (PreToolUse only)                       |
  |           Guards: declarative YAML rules + handler-based guards           |
  |           Output: permissionDecision in stdout JSON                       |
  |                                                                           |
  | +----------------------------+     +----------------------------+         |
  | |  Feed Daemon               |     |  External Data Sources     |         |
  | |  (background process)      |     |  (OUT OF SCOPE)            |         |
  | |  src/core/feeds/           |     |                            |         |
  | |  daemon-process.ts         |     |  Git repo state            |         |
  | |                            |     |  Google Calendar API       |         |
  | |  8 producers:              |<----|  Hacker News API           |         |
  | |  pulse    (30s)            |     |  Open-Meteo Weather API    |         |
  | |  project  (60s)            |     |  Claude Code usage-data    |         |
  | |  calendar (5m)             |     |  Practice Tracker DB       |         |
  | |  news     (30m)            |     |  Analytics DB              |         |
  | |  insights (2m)             |     +----------------------------+         |
  | |  practice (2m)             |                                            |
  | |  weather  (10m)            |                                            |
  | |  memories (1h)             |                                            |
  | +----------+-----------------+                                            |
  |            |                                                              |
  |            v                                                              |
  | +----------------------------+                                            |
  | |  Cache Bus                 |  ~/.hookwise/status-cache.json             |
  | |  Atomic JSON, per-key TTL  |                                            |
  | +----------+-----------------+                                            |
  |            |                                                              |
  |            v                                                              |
  | +----------------------------+      +----------------------------+        |
  | |  Status Line Renderer      |      |  TUI (Python Textual)      |        |
  | |  20 built-in segments      |      |  8 tabs, reads cache +     |        |
  | |  Two-tier layout           |      |  analytics DB              |        |
  | |  ANSI colors               |      |  Read-only data layer      |        |
  | +----------+-----------------+      +----------------------------+        |
  |            |                                                              |
  |            v                                                              |
  |   Claude Code statusLine.instructions  (.claude/settings.json)            |
  |   (hookwise writes content; Claude Code owns the display)                 |
  |                                                                           |
  | +----------------------------+                                            |
  | |  CLI                       |                                            |
  | |  init, doctor, status,     |                                            |
  | |  stats, test, daemon,      |----> All subsystems above                  |
  | |  setup, tui, migrate       |                                            |
  | +----------------------------+                                            |
  |                                                                           |
  | +----------------------------+                                            |
  | |  hookwise.yaml             |                                            |
  | |  (project + global config) |----> Configures all subsystems             |
  | |  + recipes/ includes       |                                            |
  | +----------------------------+                                            |
  |                                                                           |
  +---------------------------------------------------------------------------+
```

### Component Ownership Matrix

| Component | Hookwise Owns | Hookwise Reads | Hookwise Has No Access |
|-----------|--------------|----------------|----------------------|
| `hookwise.yaml` | Creates, validates, migrates, writes back | -- | -- |
| `.claude/settings.json` | Writes `hooks` entries and `statusLine.instructions` | Reads to detect existing config | Schema definition (Anthropic owns) |
| `~/.hookwise/analytics.db` | Creates, writes sessions/events/authorship | Queries for stats/TUI display | -- |
| `~/.hookwise/status-cache.json` | Creates, writes (cache bus atomic merges) | Reads for status line rendering | -- |
| `~/.hookwise/daemon.pid` | Creates, writes, removes (lifecycle management) | Reads for liveness checks | -- |
| `~/.hookwise/tui.pid` | Creates, writes, removes (lifecycle management) | Reads for duplicate prevention | -- |
| `~/.hookwise/daemon.log` | Creates, writes, rotates | Reads for CLI `daemon status` | -- |
| Hook event payloads (stdin) | -- | Reads and parses JSON from stdin | Cannot modify event shape or timing |
| Git repository | -- | Reads branch, HEAD, last commit timestamp | Never runs git write operations |
| Google Calendar | -- | Reads events via API (OAuth token stored locally) | Cannot create/modify/delete events |
| Hacker News / RSS | -- | Reads top stories / feed items | Cannot post or interact |
| Weather API | -- | Reads temperature, wind, conditions | Cannot submit data |
| Practice Tracker DB | -- | Reads practice session data | Cannot write practice data |
| Claude Code usage-data | -- | Reads `~/.claude/usage-data` files for insights | Cannot modify usage records |
| User's source code | -- | -- | Never reads or writes project files |
| Claude Code model/context | -- | -- | No access to model selection, context window, or inference parameters |
| Other AI agents | -- | Observes sub-agent lifecycle via hook events | Cannot start, stop, or configure external agents |

---

## Boundary Principles

1. **Read, don't write.** Hookwise reads git state, file contents in tool call payloads, and external API data but never modifies user code, git history, or external service state. The only files hookwise writes are in `~/.hookwise/` and `.claude/settings.json`.

2. **Configure, don't control.** Hookwise configures Claude Code's status line content and hook dispatch, but does not control Claude Code's behavior. The `additionalContext` field is a suggestion, not an instruction override. Claude Code decides how to use it.

3. **Degrade, don't fail.** When external services are unavailable, hookwise degrades gracefully. Feed producers return `null`, TTL expiry hides stale segments, the status line shows fewer segments rather than erroring. The fail-open guarantee (`exit 0` on any unhandled exception) ensures hookwise never blocks the user's Claude Code session.

4. **Observe, don't intercept.** Guards can block, confirm, or warn, but hookwise never silently modifies tool call arguments. The dispatcher outputs `permissionDecision` (allow/deny/ask) and `permissionDecisionReason` -- it never rewrites `tool_input`.

5. **Inform, don't silently swallow.** (Lesson from v1.3 retro, Bug #164.) Fail-open must not mean fail-silent. When a component degrades, hookwise should surface a diagnostic signal (cache bus key, status line segment, log entry) so the user knows something is degraded, even though the session continues unimpeded.

6. **Own the data layer, not the display.** Hookwise owns the analytics database, cache bus, and coaching state. It generates status line content as a string. But Claude Code owns how and where that string is displayed. Hookwise cannot control font, layout, or rendering of the status line within Claude Code's UI.

7. **Composition over extension.** New capabilities should be composable from existing subsystems (guards, feeds, segments, recipes) rather than requiring new architectural layers. A new feed producer is a function. A new guard is a YAML rule. A new recipe is a directory with `hooks.yaml`.

---

## Scope Creep Warning Signs

The following are examples of features that would violate hookwise's boundaries. Each is grounded in a real lesson from the v1.3 retro or the architecture's design constraints.

### Features that would violate "Read, don't write"

- **Auto-fixing lint errors detected by guards.** A guard can warn "this bash command has no error handling," but hookwise must not rewrite the command to add `set -e`. That crosses from observation into code modification.
- **Auto-committing on session end.** The project producer reads git state, but hookwise must never run `git commit` or `git push` on behalf of the user.
- **Creating GitHub issues from coaching alerts.** Hookwise can surface "you've been in tooling mode for 90 minutes" but should not create external artifacts (issues, PRs, Slack messages) without an explicit integration layer.

### Features that would violate "Configure, don't control"

- **Prompt injection via additionalContext.** Using `additionalContext` to override Claude Code's system prompt, force a specific coding style, or hijack the model's behavior. The context field is for nudges (metacognition prompts, greeting quotes), not directives.
- **Automatic context window compaction.** Hookwise can display context usage via `context_bar`, but triggering compaction or managing context size is Claude Code's responsibility.

### Features that would violate "Observe, don't intercept"

- **Argument rewriting guards.** A guard that changes `rm -rf /` to `rm -rf /tmp` instead of blocking it. Hookwise's guard output is `permissionDecision: deny`, not a modified `tool_input`.
- **Silent tool call filtering.** Dropping certain tool calls without informing the user. Guards must always produce a visible `reason` when they block.

### Features that would violate "Inform, don't silently swallow"

- **Metrics without accuracy validation.** (Direct lesson from Bug #167.) The AI authorship metric shipped with a timing heuristic that had no empirical calibration, displayed via a progress bar implying precision it could not deliver. Any new metric must answer: "What is the ground truth, and how do we validate accuracy?" If the answer is "we cannot," the metric must not be user-facing.
- **Segments without producers.** (Direct lesson from Bug #162.) A status line segment that reads from a cache key that no producer writes to. The `practice_breadcrumb` segment shipped 16 days before its producer existed. Every new segment must declare its data source.

### Features that would cross the TUI boundary

- **TUI writing to hookwise.yaml.** (Identified in retro, Lesson #6.) The TUI data layer (`tui/hookwise_tui/data.py`) is explicitly read-only. Adding a write path requires careful design because the TUI is a Python process reading a TypeScript-managed YAML file with no shared type system. This is a planned v1.4 feature but is currently out of scope.
- **TUI controlling the daemon directly.** The TUI can display daemon health but should not start/stop the daemon (that is the CLI's job). Adding daemon control to the TUI would create two control planes for the same process.

### General scope creep patterns

- **Adding a VS Code extension.** Listed in the v2.0+ roadmap but requires a new architectural layer (LSP, webview, extension API). Not a "just add a file" feature.
- **Webhook notifications.** Sending Slack/Discord alerts requires network I/O in the dispatch hot path or a new notification queue. This is v2.0+ scope.
- **Recipe marketplace.** Community recipe discovery, versioning, and installation requires a backend service. This is v2.0+ scope.
- **Team analytics.** Aggregating analytics across multiple users requires a shared data store and opt-in consent model. This is v2.0+ scope.

---

[Back to Home](/)
