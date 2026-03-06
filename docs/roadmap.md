# Hookwise Roadmap

hookwise is a developer-tool framework that wraps Claude Code with composable hooks, a feed-backed status line, coaching nudges, and a Textual TUI dashboard. It shipped its first public release (v1.3) on 2026-03-03. This roadmap covers the known bugs from that launch, the planned versions through v2.0, and the principles that govern what gets prioritized.

For architecture context, see [architecture.md](architecture.md). For design philosophy, see [philosophy.md](philosophy.md).

---

## Current Bugs (P0 — Fix Before Any New Features)

These seven bugs were identified in the v1.3 post-launch retrospective. None crashed or corrupted user sessions (the fail-open guarantee held), but all degrade the user experience. They cluster into four themes: TUI shipped without acceptance testing, wiring gaps between isolated components, missing self-monitoring, and feature scope without product validation.

| Bug | Task | GH Issue | Severity | Component | Status |
|-----|------|----------|----------|-----------|--------|
| Analytics DB never created | #161 | #9 | HIGH | Analytics | Open |
| practice_breadcrumb no producer | #162 | #8 | MEDIUM | Status Line | Fixed post-launch |
| Daemon not running, status silent | #164 | — | HIGH | Daemon | Open |
| TUI vertical text rendering | #165 | — | HIGH | TUI | Open |
| Coaching tab dummy content | #166 | — | MEDIUM | TUI | Open |
| Remove AI authorship metric | #167 | — | LOW | Analytics/TUI | Open |
| Status Line tab not configurable | #168 | — | MEDIUM | TUI | Open |

**Bug #161 — Analytics DB never created.** The dispatcher never calls the `AnalyticsDB` constructor, so the SQLite file at `~/.hookwise/analytics.db` is never created. The TUI analytics tab shows all zeroes, and the CLI reports "No analytics database found." The individual analytics modules are well-tested in isolation; the gap is that no code path wires them into the dispatch pipeline.

**Bug #162 — practice_breadcrumb segment has no producer.** The `practice_breadcrumb` segment was added to the default rotating layout in v1.1 but the `practice` producer was not created until v1.3 — a 16-day gap. During that window, the segment silently returned an empty string on every rotation. Fixed post-launch by shipping the practice producer.

**Bug #164 — Daemon crashes silently.** When the feed daemon crashes, fails to start, or becomes unresponsive, the status line silently degrades. Feed-backed segments display stale data until TTL expires, then vanish. There is no warning icon, no error segment, and no color change. The user cannot distinguish "no upcoming meetings" from "daemon crashed 45 minutes ago." The architecture has no feedback channel from the status line back to the daemon.

**Bug #165 — TUI vertical text rendering.** Dashboard card titles render vertically on terminals narrower than 100 columns. The `FeatureCard` widget has no `min-width` at any level of its hierarchy. Textual's default pilot test size is 80x24 — exactly where this bug manifests — but zero snapshot tests exist.

**Bug #166 — Coaching tab is display-only.** The coaching tab imports only `Static` widgets — no `Button`, `Switch`, or `Input`. It shows coaching data but provides zero interactive controls for enabling, disabling, or configuring coaching features. The data layer is explicitly read-only with no `write_config()` function.

**Bug #167 — AI authorship metric is meaningless.** The timing-based heuristic uses hardcoded thresholds with no empirical basis, no calibration data, and no ground-truth validation. The in-memory prompt cache resets on process restart, defaulting all scores to 0.30 regardless of actual behavior. Displaying it with a progress bar implies precision that does not exist. This metric should be removed from all user-facing surfaces until it can be properly validated.

**Bug #168 — Status Line tab does not configure anything.** The tab is titled "Segment Configurator" but yields only `Static` and `Container` widgets. It hardcodes 9 default segments while the TypeScript backend supports 20 built-in segments with a two-tier layout the TUI is completely unaware of.

---

## v1.4 — Stability & TUI Polish

**Focus:** Fix all P0 bugs, add testing infrastructure, make TUI tabs interactive. No new features until the existing surface area is solid.

### Bug Fixes

- **Fix all P0 bugs above.** Each bug has a clear root cause identified in the retrospective. The fixes range from wiring the analytics DB constructor into the dispatcher (#161) to implementing a `daemon_health` status line segment (#164).
- **Add Textual snapshot testing infrastructure.** Introduce `pytest-textual-snapshot` as a dev dependency. Create snapshot tests for every TUI tab at three terminal widths (80x24, 120x40, 60x20). Snapshot diffs must be committed to the repo and any visual regression must fail CI.
- **Add daemon health to the status line (self-monitoring).** Implement a `daemon_health` segment that reads a periodic `_daemon_heartbeat` cache key (written every 30s with 90s TTL) and displays a warning when the daemon is unresponsive. Split the ambiguous `_heartbeat` key into `_dispatch_heartbeat` (dispatcher) and `_daemon_heartbeat` (daemon) to eliminate liveness ambiguity.

### TUI Interactivity

- **Make the Coaching tab interactive.** Add enable/disable toggles for each coaching feature (metacognition prompts, builder's trap detection, communication nudges). Implement a `write_config()` path in the Python data layer so the TUI can persist configuration changes back to `hookwise.yaml`.
- **Make the Status Line tab configurable.** Add controls to add/remove segments, adjust segment order, set line count, and preview the status line at different widths. Sync the TUI's segment knowledge with the backend's full 20-segment registry and two-tier layout.
- **Remove AI authorship metric from all user-facing surfaces.** Remove the `ai_ratio` status line segment, the TUI progress bar, and the `AuthorshipLedger` from the public API. The underlying code may remain as internal infrastructure behind an experimental flag, but nothing user-facing until accuracy can be validated.

### Testing

- **Add TUI tests to CI pipeline.** Currently no GitHub Actions workflow runs the TUI tests. Add a CI step that installs the TUI package and runs `pytest tui/tests/` including snapshot tests.
- **Add integration tests for all producer-segment pairs.** Implement a reverse orphan detection test: iterate `BUILTIN_SEGMENTS`, identify which cache keys each segment reads, and assert that every daemon-managed cache key has a registered producer. This would have caught bug #162 at the v1.1 commit.
- **Add visual regression tests at 80-column minimum width.** Parameterize all TUI snapshot tests to include the minimum documented terminal width (80 columns per the TUI guide). Any tab that breaks at 80 columns fails CI.

---

## v1.5 — Multi-Agent Orchestration

**Focus:** Use hookwise's hook scripts to spawn and coordinate parallel coding agent sessions.

- **Hook-triggered parallel agent sessions.** When a hookwise hook fires, it can spawn parallel coding agent sessions — Claude Code, Gemini CLI, OpenAI Codex CLI, or any CLI-based agent — that work concurrently and write results back to a shared file or directory.
- **Use case.** When Claude Code is throttled or rate-limited, farm out independent tasks to other agents via hooks. A guard that detects throttling could automatically spawn a Gemini CLI session to handle the blocked task.
- **Scope.** hookwise controls the spawning (via hook scripts) and result collection (via the cache bus or file watchers). hookwise does NOT control the agents themselves — it does not manage their prompts, context windows, or internal state. Each agent is a black box that receives input and produces output.
- **Inspiration.** Similar to "Claude Code in a loop" patterns where autonomous agents are orchestrated externally, but hookwise provides the orchestration layer rather than the loop runner.

---

## v1.6 — Scheduled Hooks

**Focus:** Extend the hook system to support time-based triggers, not just Claude Code event triggers.

- **Cron-style hook scheduling.** Define hooks that run at specific times or intervals independent of Claude Code events. Express schedules using standard cron expressions in `hookwise.yaml`.
- **Use case.** Daily analytics summary emailed at 6 PM. Morning status refresh that pre-warms the cache before you start coding. Scheduled backups of session transcripts. Periodic health checks that verify the daemon and all producers are running.
- **Implementation.** Extend the daemon to support cron expressions alongside its existing interval-based polling. The daemon already runs continuously and manages a timer-based producer registry — adding cron triggers is a natural extension of the same scheduling infrastructure.
- **Scope.** Fully within hookwise's control. The daemon already has the event loop, the cache bus already supports writes from arbitrary sources, and the dispatcher already knows how to execute hook handlers. The new piece is a cron expression parser and a scheduler that triggers hooks at the right times.

---

## v1.7 — Claude Code Loop Integration

**Focus:** Provide guards, analytics, and coaching for autonomous AI development loop runners.

- **Context.** A growing ecosystem of autonomous AI development loop runners intercept Claude Code's exit signals and re-prompt until a task is complete. Examples include tools that create PRs in a loop, run continuous code review cycles, or chain multiple agent sessions together. These loops are powerful but unobserved — there is no cost tracking, no progress visibility, and no guard rails per iteration.
- **Integration.** hookwise could wrap and observe loop sessions: track loop iteration count, apply cost guards per cycle (not just per session), surface loop progress in the status line ("Iteration 7/20, $4.23 spent, 34min elapsed"), and trigger coaching nudges when a loop appears stuck (same error 3 iterations in a row).
- **Scope.** hookwise wraps and observes the loop — it does not implement the loop itself. The loop runner is an external tool that hookwise monitors through its existing hook events. hookwise sees each iteration as a Claude Code session and can aggregate across iterations.
- **Potential features:**
  - A `loop` feed producer that tracks current iteration count, total cost, and time elapsed
  - A `loop_guard` that enforces per-loop cost and iteration limits
  - A `loop_progress` status line segment showing iteration progress
  - Loop-aware coaching that detects repeated failures across iterations

---

## v2.0 — Community & Ecosystem

**Focus:** Grow hookwise from a single-user tool to a community platform.

- **Recipe marketplace.** Share and discover hookwise configurations. Users can publish their `hookwise.yaml` recipes (guards, coaching configs, feed setups) to a central registry, versioned and searchable. Community recipes install with `hookwise recipe add <name>`.
- **Team analytics (opt-in).** Aggregate insights across team members who opt in. See team-wide patterns: which guards fire most often, average session duration, common tool usage. All data stays local unless explicitly shared.
- **VS Code / Cursor extension.** Native IDE integration that surfaces the hookwise status line in the editor sidebar, shows guard decisions in real time, and provides a GUI for editing `hookwise.yaml`. Replaces the need to run the TUI in a separate terminal.
- **Webhook notifications.** Slack and Discord integration for alerts. Get notified when a guard blocks a dangerous operation, when a session exceeds a cost threshold, or when the daemon detects an anomaly. Configurable per-channel with severity filters.
- **Plugin API for custom feed producers.** A stable public API for writing custom feed producers that plug into the cache bus. Third parties can create producers for any data source (Jira tickets, CI status, deployment health) and distribute them as npm packages.

---

## Principles for Roadmap Prioritization

These principles emerged from the v1.3 launch retrospective and reflect lessons learned from shipping a framework with a large surface area.

1. **Fix before build.** All P0 bugs must be resolved before any new feature work begins. A system that looks healthy when it is broken is worse than a system with fewer features that works correctly. The v1.3 bugs collectively created an experience where the TUI showed zeroes, segments vanished silently, and metrics lied — even though every individual module passed its unit tests.

2. **Stay in scope.** Only build features within hookwise's control boundary. hookwise controls: hook dispatch, guard evaluation, context injection, side effects, the feed daemon, the cache bus, the status line, the TUI, and the config system. hookwise does NOT control: Claude Code itself, external APIs, other AI agents' internals, or IDE behavior. Features that require controlling things outside this boundary are out of scope. (See the architecture doc for the full system boundary diagram.)

3. **Test before ship.** Every feature must have integration tests, not just unit tests. The v1.3 bugs proved that unit test coverage is not integration coverage. The analytics module had >90% unit test coverage and every test passed, but the dispatcher never called the analytics DB constructor. One integration test that traces the full pipeline from config flag to state mutation is worth more than ten unit tests of isolated components.

4. **Product validate first.** New features need product skeptic review before spec generation. Before any new metric, feed, or UI element ships, it requires explicit sign-off answering: "Is this metric reliable enough to display to users?" and "What is the ground truth, and how do we validate accuracy?" The AI authorship metric shipped without this gate and now must be removed.

5. **TUI is production.** Treat TUI tabs as first-class features with acceptance criteria. The v1.3 TUI was treated as a "bonus" visual layer with render-only smoke tests. Three bugs shipped through this gap. Every TUI tab must have: snapshot tests at minimum documented width (80 columns), widget-composition assertions (does this tab contain the interactive widgets its title promises?), and interaction tests (does pressing this button trigger a handler?).

6. **Integration over unit.** One integration test that traces a full pipeline — from config load through dispatch through state mutation through status line rendering — is worth more than ten unit tests of isolated components. The most dangerous state for a codebase is "every module works, but the system doesn't." For every `enabled: boolean` config flag, require an integration test that sets it to `true`, dispatches an event, and asserts the expected state mutation occurred through the full pipeline.

---

← [Back to Home](index.md)
