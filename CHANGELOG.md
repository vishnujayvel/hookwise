# Changelog

All notable changes to hookwise will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.3.0] - 2026-03-03

### Added

- **3 new feed producers:**
  - `weather` -- Open-Meteo API integration with configurable lat/lon coordinates
  - `memories` -- Mem0 MCP integration for surfacing relevant memories
  - `practice` -- spaced repetition data from practice-tracker MCP
- **2 new status line segments:**
  - `weather` -- current temperature and conditions
  - `memories` -- relevant memory count
- **Integration tests** -- comprehensive test suite for end-to-end validation
- **TUI weather background** -- weather-reactive background in Python TUI dashboard

### Fixed

- Input validation and sanitization across producers
- Path traversal prevention in file-based operations
- YAML safe loading enforced in config parsing

## [1.2.0] - 2026-02-23

### Added

- **Insights Producer** -- 5th feed producer that reads Claude Code usage data from `~/.claude/usage-data/`, aggregates session metrics within a configurable staleness window (default: 30 days), and writes results to the cache bus. Polls every 120 seconds by default.
- **3 insights status line segments:**
  - `insights_friction` -- friction health indicator (warns on recent friction, shows clean status otherwise)
  - `insights_pace` -- productivity metrics (messages/day, total lines, session count)
  - `insights_trend` -- usage patterns (top tools and peak coding hour)
- **friction-alert recipe** -- post-hook (`Stop` event) that warns when recent session friction meets or exceeds a configurable threshold (default: 3). Reads from the insights cache key. Non-blocking, advisory only.
- `InsightsFeedConfig` type with `enabled`, `intervalSeconds`, `stalenessDays`, and `usageDataPath` fields
- 105 new tests across 4 test files (1363 total, up from 1258)

## [1.1.0] - 2026-02-15

### Added

- **Feed Platform** -- background daemon with feed registry, cache bus, and staggered polling
- **4 feed producers:** pulse (heartbeat), project (git info), calendar (upcoming events), news (Hacker News)
- **4 feed status line segments:** `pulse`, `project`, `calendar`, `news`
- **Two-tier status line segments:** `context_bar`, `mode_badge`, `duration`, `practice_breadcrumb`
- **Daemon CLI:** `hookwise daemon start`, `hookwise daemon stop`, `hookwise daemon status`
- Cache bus with per-key atomic merge, TTL-aware reads via `isFresh()`, fail-open on corruption
- Daemon auto-start, PID file management, stale PID cleanup, inactivity timeout (default: 120 min)

## [1.0.0] - 2026-01-20

### Added

- Initial release
- YAML-driven hook dispatcher for all 13 Claude Code hook events
- Declarative guard rules with glob patterns, conditions, and 3 actions (block, warn, confirm)
- Three-phase execution: guards, context injection, side effects
- Builder's trap detection with configurable thresholds
- Metacognition coaching with periodic prompts
- Communication coach for grammar analysis
- SQLite session analytics with AI confidence scoring
- Cost tracking with daily budget enforcement
- Composable status line with 7 segments
- Interactive TUI with 6 tabs
- 11 built-in recipes
- Config resolution: global + project merge, includes, env var interpolation
- Testing utilities: GuardTester, HookRunner, HookResult
- `hookwise init`, `doctor`, `status`, `stats`, `test`, `tui`, `migrate` CLI commands
