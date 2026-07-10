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
| `hookwise.yaml` config | `internal/core/config.go` | Project-level and global YAML configuration (gopkg.in/yaml.v3) with deep merge, env var interpolation, snake_case/camelCase conversion |
| Config validation | `internal/core/config.go` (`ValidateConfig`) | Structural validation of all 14 top-level sections with path-based error reporting |
| Include resolution | `internal/core/config.go` (`ResolveIncludes`) | Merge included YAML files (including recipe files) into the base config |
| Recipe library | `recipes/` | 11 built-in recipes across 6 categories (safety, quality, productivity, behavioral, compliance, gamification), consumed as YAML config includes |

### Execution Layer

| Component | Source Files | Description |
|-----------|-------------|-------------|
| Three-phase dispatcher | `internal/core/dispatcher.go` | Entry point for all 13 Claude Code hook events: Guards -> Context Injection -> Side Effects |
| Guard evaluation | `internal/core/guards.go` | First-match-wins firewall rules with glob matching (gobwas/glob), 6 condition operators (`contains`, `starts_with`, `ends_with`, `==`, `equals`, `matches`), `when`/`unless` clauses |
| Handler execution | `internal/core/handlers.go` | Executes builtin, script (os/exec), and inline handler types with timeout enforcement |
| Context injection | `internal/core/dispatcher.go` (`executeContextPhase`) | Merges `additionalContext` strings from context-phase handlers into hook output |

### Data Layer

| Component | Source Files | Description |
|-----------|-------------|-------------|
| Analytics SQLite DB | `internal/analytics/sqlite.go` | Session tracking and tool call logging (`~/.hookwise/analytics.db`); WAL mode, single-writer connection (ARCH-2) |
| Analytics snapshots | `internal/analytics/snapshot.go`, `internal/analytics/history.go` | Point-in-time `VACUUM INTO` snapshots of the analytics DB, with history and row-count diffing (`snapshot`/`diff`/`log` commands) |
| Cost state | `internal/analytics/state.go` (`CostState`, `UpdateCostState`), `internal/pricing/` | Per-session and daily USD cost accumulation from token usage, with budget enforcement |
| Transcript usage parsing | `internal/transcript/` | Parses Claude Code `.jsonl` transcripts to aggregate token usage for cost computation |
| Go→TUI cache bridge | `internal/bridge/bridge.go` | Collects fresh feed envelopes and writes the merged TUI cache (`~/.hookwise/status-line-cache.json`) via `FlattenForTUI` |
| Notifications | `internal/notifications/` | Notification service and producers (e.g. budget threshold checks) persisted in the analytics DB, surfaced via `hookwise notifications` |

### Display Layer

| Component | Source Files | Description |
|-----------|-------------|-------------|
| Status line renderer | `cmd/hookwise/cmd_status_line.go` | Config-ordered segment rendering with 9 built-in segments (`cost`, `project`, `calendar`, `weather`, `news`, `insights`, `insights_friction`, `insights_pace`, `insights_trend`) and conditional ANSI colorization |
| Fleet badge | `internal/fleet/fleet.go`, `cmd/hookwise/cmd_status_line_fleet.go` | Read-only cross-session "fleet status" snapshot aggregated from live session data |
| TUI application | `tui/hookwise_tui/` | 7-tab Python Textual app (Dashboard, Guards, Analytics, Feeds, Insights, Recipes, Status) with a read-only data layer |
| TUI launcher | `cmd/hookwise/cmd_tui.go` | Launches the separately-installed `hookwise-tui` (PATH lookup) through the singleton mtime-marker guard; `newWindow` or `background` launch methods |

### Feed Platform

| Component | Source Files | Description |
|-----------|-------------|-------------|
| Feed registry | `internal/feeds/producer.go` (`Registry`) | Map-backed registration and lookup for feed producers |
| Daemon process | `internal/feeds/daemon.go`, `internal/feeds/socket.go` | Background Go process that polls producers on staggered intervals and serves feed state over a local socket |
| Daemon lifecycle | `cmd/hookwise/cmd_daemon.go` | `start`/`stop` subcommands (connect-or-start) with PID-file liveness checks (`~/.hookwise/daemon.pid`), plus a hidden foreground `run`; includes the TUI singleton watchdog |
| 6 built-in producers | `internal/feeds/builtin.go`, `internal/feeds/producer_*.go` | project (60s), insights (2m), calendar (5m), weather (10m), news (30m), memories (1h) |
| Custom feed producer | `internal/feeds/custom.go` (`NewCustomProducer`) | Wraps user-authored shell commands as feed producers |

### CLI Layer

| Command | Source File | Description |
|---------|------------|-------------|
| `hookwise dispatch` | `cmd/hookwise/cmd_dispatch.go` | Dispatch a hook event (called by Claude Code) |
| `hookwise init` | `cmd/hookwise/cmd_init.go` | Create a default `hookwise.yaml` in the current directory and wire hooks into settings.json |
| `hookwise doctor` | `cmd/hookwise/cmd_doctor.go` | Run system health checks |
| `hookwise status-line` | `cmd/hookwise/cmd_status_line.go` | Render the status line |
| `hookwise stats` | `cmd/hookwise/cmd_stats.go` | Show analytics dashboard for today |
| `hookwise test` | `cmd/hookwise/cmd_test_runner.go` | Evaluate guard test scenarios |
| `hookwise audit` | `cmd/hookwise/cmd_audit.go` | Scan Claude Code hook configuration for health issues (`--fix` applies repairs) |
| `hookwise daemon` | `cmd/hookwise/cmd_daemon.go` | Manage the feed daemon: `start`, `stop`, and a hidden foreground `run` (no `status` subcommand) |
| `hookwise snapshot` | `cmd/hookwise/cmd_snapshot.go` | Take a point-in-time snapshot of the analytics database |
| `hookwise diff` | `cmd/hookwise/cmd_diff.go` | Show row-count changes between two analytics snapshots |
| `hookwise log` | `cmd/hookwise/cmd_log.go` | Show analytics snapshot history |
| `hookwise notifications` | `cmd/hookwise/cmd_notifications.go` | Display notification history |
| `hookwise upgrade` | `cmd/hookwise/cmd_upgrade.go` | Migrate data from a TypeScript hookwise installation |
| `hookwise tui` | `cmd/hookwise/cmd_tui.go` | Launch the interactive TUI |

### Testing Utilities

| Component | Source Files | Description |
|-----------|-------------|-------------|
| HookRunner | `pkg/hookwise/testing/hook_runner.go` | Programmatic hook dispatch for integration tests |
| HookResult | `pkg/hookwise/testing/hook_runner.go` | Structured assertion helpers for dispatch results |
| GuardTester | `pkg/hookwise/testing/guard_tester.go` | Fluent API for testing guard rule evaluation |

---

## What Hookwise Does NOT Control (OUT OF SCOPE)

### Claude Code Internals

| Boundary | Why it is out of scope |
|----------|----------------------|
| Claude Code itself | Hookwise consumes hook events emitted by Claude Code but cannot modify Claude Code's behavior, model selection, or reasoning. Hookwise influences Claude Code only through the `additionalContext` field in hook output. |
| `settings.json` schema | Hookwise writes `hooks` and `statusLine` entries to `.claude/settings.json`, but Anthropic defines the schema. Hookwise must track schema changes reactively. |
| Hook event types | The 13 event types (`UserPromptSubmit`, `PreToolUse`, `PostToolUse`, etc.) are defined by Anthropic. Hookwise cannot create new event types or modify event payloads before they arrive. |
| Context window management | Hookwise can read what hook payloads report and display it, but cannot control compaction, context size, or model selection. |
| Tool call arguments | Hookwise can block, confirm, or warn on tool calls, but never modifies tool call arguments. The `PreToolUse` hook output supports `permissionDecision` (allow/deny/ask) but not argument rewriting. |
| Claude Code's prompt or system message | Hookwise injects `additionalContext` but does not control the base system prompt, model temperature, or other inference parameters. |

### External Services

| Boundary | Why it is out of scope |
|----------|----------------------|
| Google Calendar API | The calendar producer reads events but hookwise does not create, modify, or delete calendar events. OAuth credentials are stored locally; hookwise does not manage the Google Cloud project. |
| Hacker News API | The news producer reads top stories. Hookwise does not post, vote, or interact with HN. |
| Open-Meteo Weather API | The weather producer reads forecasts. Hookwise does not submit data. |
| Network connectivity | Feed producers degrade gracefully (return `null` on failure, TTL expiry hides stale data) but hookwise cannot diagnose or fix network issues. |

### User Environment

| Boundary | Why it is out of scope |
|----------|----------------------|
| Git operations | The project producer reads git state (`git rev-parse`, `git log`) but hookwise never runs `git commit`, `git push`, `git checkout`, or any write operation against the repository. |
| User's source code | Hookwise observes tool calls (file edits, bash commands) via `PostToolUse` payloads but never reads, modifies, or creates the user's own project files. The only files hookwise writes are in `~/.hookwise/`, `.claude/settings.json`, and the `hookwise.yaml` created by `hookwise init`. |
| Other coding agents | Hookwise receives `SubagentStart`/`SubagentStop` hook events for observability but does not start, stop, configure, or communicate with external agents (Gemini, Codex, Cursor agents). |
| IDE/editor state | Hookwise has no VS Code, Cursor, or JetBrains extension. It operates entirely through Claude Code's hook system and the terminal. |
| Terminal emulator | The TUI renders via Textual/Python in whatever terminal is available. Hookwise does not control terminal settings, font, or color scheme. |
| Python runtime | The TUI is a separate Python/Textual package (`hookwise-tui`, installed from the repo with `uv tool install ./tui`) that the Go binary looks up on PATH. Hookwise does not install or manage system Python. The Go binary writes JSON cache that the TUI reads. |

---

## Entity Relationships

### Data Flow Diagram (ASCII)

```text
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
  | |  hookwise dispatcher       |  internal/core/dispatcher.go               |
  | |  (entry point: Dispatch()) |                                            |
  | +---+------+------+---------+                                             |
  |     |      |      |                                                       |
  |     v      v      v                                                       |
  |  Phase 1  Phase 2  Phase 3                                                |
  |  GUARDS   CONTEXT  SIDE EFFECTS                                           |
  |     |      |      |                                                       |
  |     |      |      +---> Analytics DB  (~/.hookwise/analytics.db)          |
  |     |      |      +---> Cost Accumulation (CostState + pricing)           |
  |     |      |      +---> Notifications (analytics DB)                      |
  |     |      |      +---> User-configured script handlers                   |
  |     |      |            (non-blocking goroutines, ARCH-7)                 |
  |     |      |                                                              |
  |     |      +---> additionalContext (context-phase handlers)               |
  |     |                                                                     |
  |     +---> block / confirm / warn  (PreToolUse only)                       |
  |           Guards: declarative YAML rules + handler-based guards           |
  |           Output: permissionDecision in stdout JSON                       |
  |                                                                           |
  | +----------------------------+     +----------------------------+         |
  | |  Feed Daemon               |     |  External Data Sources     |         |
  | |  (background process)      |     |  (OUT OF SCOPE)            |         |
  | |  internal/feeds/daemon.go  |     |                            |         |
  | |                            |     |  Git repo state            |         |
  | |  6 producers:              |     |  Google Calendar API       |         |
  | |  project  (60s)            |<----|  Hacker News API           |         |
  | |  insights (2m)             |     |  Open-Meteo Weather API    |         |
  | |  calendar (5m)             |     |  Claude Code usage-data    |         |
  | |  weather  (10m)            |     |  Analytics DB              |         |
  | |  news     (30m)            |     +----------------------------+         |
  | |  memories (1h)             |                                            |
  | +----------+-----------------+                                            |
  |            |                                                              |
  |            v                                                              |
  | +----------------------------+                                            |
  | |  Cache Bridge              |  internal/bridge/bridge.go                 |
  | |  Atomic JSON, TTL-aware    |  ~/.hookwise/status-line-cache.json        |
  | +----------+-----------------+                                            |
  |            |                                                              |
  |            v                                                              |
  | +----------------------------+      +----------------------------+        |
  | |  Status Line Renderer      |      |  TUI (Python Textual)      |        |
  | |  9 built-in segments       |      |  7 tabs, reads cache +     |        |
  | |  config-ordered            |      |  analytics DB              |        |
  | |  ANSI colors               |      |  Read-only data layer      |        |
  | +----------+-----------------+      +----------------------------+        |
  |            |                                                              |
  |            v                                                              |
  |   Claude Code statusLine  (.claude/settings.json)                         |
  |   (hookwise writes content; Claude Code owns the display)                 |
  |                                                                           |
  | +----------------------------+                                            |
  | |  CLI                       |                                            |
  | |  dispatch, init, doctor,   |                                            |
  | |  status-line, stats, test, |----> All subsystems above                  |
  | |  audit, daemon, snapshot,  |                                            |
  | |  diff, log, notifications, |                                            |
  | |  upgrade, tui              |                                            |
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
| `hookwise.yaml` | Creates (`hookwise init`), loads, validates | -- | -- |
| `.claude/settings.json` | Writes `hooks` and `statusLine` entries | Reads to detect existing config | Schema definition (Anthropic owns) |
| `~/.hookwise/analytics.db` | Creates, writes sessions/events/cost state (SQLite WAL) | Queries for stats/TUI display | -- |
| `~/.hookwise/status-line-cache.json` | Creates, writes (atomic merged writes via the cache bridge) | Reads for status line rendering | -- |
| `~/.hookwise/daemon.pid` | Creates, writes, removes (lifecycle management) | Reads for liveness checks | -- |
| `~/.hookwise/tui.pid` | Creates, writes, removes (lifecycle management) | Reads for duplicate prevention | -- |
| `~/.hookwise/daemon.log` | Creates, writes | Reads for diagnostics | -- |
| Hook event payloads (stdin) | -- | Reads and parses JSON from stdin | Cannot modify event shape or timing |
| Git repository | -- | Reads branch, HEAD, last commit timestamp | Never runs git write operations |
| Google Calendar | -- | Reads events via API (OAuth token stored locally) | Cannot create/modify/delete events |
| Hacker News | -- | Reads top stories | Cannot post or interact |
| Weather API | -- | Reads temperature, wind, conditions | Cannot submit data |
| Claude Code usage-data | -- | Reads `~/.claude/usage-data` files for insights | Cannot modify usage records |
| User's source code | -- | -- | Never reads or writes the user's project files |
| Claude Code model/context | -- | -- | No access to model selection, context window, or inference parameters |
| Other AI agents | -- | Observes `SubagentStart`/`SubagentStop` hook events | Cannot start, stop, or configure external agents |

---

## Boundary Principles

1. **Read, don't write.** Hookwise reads git state, file contents in tool call payloads, and external API data but never modifies user code, git history, or external service state. The only files hookwise writes are in `~/.hookwise/`, `.claude/settings.json`, and the `hookwise.yaml` created by `hookwise init`.

2. **Configure, don't control.** Hookwise configures Claude Code's status line content and hook dispatch, but does not control Claude Code's behavior. The `additionalContext` field is a suggestion, not an instruction override. Claude Code decides how to use it.

3. **Degrade, don't fail.** When external services are unavailable, hookwise degrades gracefully. Feed producers return `null`, TTL expiry hides stale segments, the status line shows fewer segments rather than erroring. The fail-open guarantee (`exit 0` on any unhandled exception) ensures hookwise never blocks the user's Claude Code session.

4. **Observe, don't intercept.** Guards can block, confirm, or warn, but hookwise never silently modifies tool call arguments. The dispatcher outputs `permissionDecision` (allow/deny/ask) and `permissionDecisionReason` -- it never rewrites `tool_input`.

5. **Inform, don't silently swallow.** (Lesson from v1.3 retro, Bug #164.) Fail-open must not mean fail-silent. When a component degrades, hookwise should surface a diagnostic signal (cache key, status line segment, log entry) so the user knows something is degraded, even though the session continues unimpeded.

6. **Own the data layer, not the display.** Hookwise owns the analytics database and the feed cache. It generates status line content as a string. But Claude Code owns how and where that string is displayed. Hookwise cannot control font, layout, or rendering of the status line within Claude Code's UI.

7. **Composition over extension.** New capabilities should be composable from existing subsystems (guards, feeds, segments, recipes) rather than requiring new architectural layers. A new feed producer is a function. A new guard is a YAML rule. A new recipe is a directory of YAML config.

---

## Scope Creep Warning Signs

The following are examples of features that would violate hookwise's boundaries. Each is grounded in a real lesson from the v1.3 retro or the architecture's design constraints.

### Features that would violate "Read, don't write"

- **Auto-fixing lint errors detected by guards.** A guard can warn "this bash command has no error handling," but hookwise must not rewrite the command to add `set -e`. That crosses from observation into code modification.
- **Auto-committing on session end.** The project producer reads git state, but hookwise must never run `git commit` or `git push` on behalf of the user.
- **Creating GitHub issues from coaching alerts.** A coaching-style nudge could surface "you've been in tooling mode for 90 minutes," but hookwise should not create external artifacts (issues, PRs, Slack messages) without an explicit integration layer.

### Features that would violate "Configure, don't control"

- **Prompt injection via additionalContext.** Using `additionalContext` to override Claude Code's system prompt, force a specific coding style, or hijack the model's behavior. The context field is for nudges, not directives.
- **Automatic context window compaction.** Hookwise can display context usage, but triggering compaction or managing context size is Claude Code's responsibility.

### Features that would violate "Observe, don't intercept"

- **Argument rewriting guards.** A guard that changes `rm -rf /` to `rm -rf /tmp` instead of blocking it. Hookwise's guard output is `permissionDecision: deny`, not a modified `tool_input`.
- **Silent tool call filtering.** Dropping certain tool calls without informing the user. Guards must always produce a visible `reason` when they block.

### Features that would violate "Inform, don't silently swallow"

- **Metrics without accuracy validation.** (Direct lesson from Bug #167.) The AI authorship metric shipped with a timing heuristic that had no empirical calibration, displayed via a progress bar implying precision it could not deliver. Any new metric must answer: "What is the ground truth, and how do we validate accuracy?" If the answer is "we cannot," the metric must not be user-facing.
- **Segments without producers.** (Direct lesson from Bug #162.) A status line segment that reads from a cache key that no producer writes to. The `practice_breadcrumb` segment shipped 16 days before its producer existed. Every new segment must declare its data source.

### Features that would cross the TUI boundary

- **TUI writing to hookwise.yaml.** (Identified in retro, Lesson #6.) The TUI data layer (`tui/hookwise_tui/data.py`) is explicitly read-only. Adding a write path requires careful design because the TUI is a Python process reading a Go-managed YAML file with no shared type system. This is currently out of scope.
- **TUI controlling the daemon directly.** The TUI can display daemon health but should not start/stop the daemon (that is the CLI's job). Adding daemon control to the TUI would create two control planes for the same process.

### General scope creep patterns

- **Adding a VS Code extension.** Listed in the v2.0+ roadmap but requires a new architectural layer (LSP, webview, extension API). Not a "just add a file" feature.
- **Webhook notifications.** Sending Slack/Discord alerts requires network I/O in the dispatch hot path or a new notification queue. This is v2.0+ scope.
- **Recipe marketplace.** Community recipe discovery, versioning, and installation requires a backend service. This is v2.0+ scope.
- **Team analytics.** Aggregating analytics across multiple users requires a shared data store and opt-in consent model. This is v2.0+ scope.

---

[Back to Home](/)
