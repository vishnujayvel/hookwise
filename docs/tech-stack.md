# Tech Stack

A complete reference of every framework, library, and tool used in hookwise, organized by SDLC layer.

## Architecture Overview

Hookwise is a **polyglot project** with two runtimes:

- **TypeScript core engine** -- the npm package (config, dispatcher, feeds, analytics, CLI, status line)
- **Python TUI** -- a separate Textual app launched as a detached subprocess

They communicate via a **shared filesystem cache** (JSON file), not HTTP or IPC.

## Layer 1: TypeScript Core (Runtime)

| Tool | Version | What It Does | Why This Choice |
|------|---------|-------------|-----------------|
| **Node.js** | >=20.0.0 | Runtime for the core engine | Native ESM support, stable LTS |
| **TypeScript** | ^5.9.3 | Type safety for the entire TS codebase | Strict mode enabled, `ES2022` target |
| **js-yaml** | ^4.1.1 | Parses `hookwise.yaml` config files | Industry standard YAML parser |
| **picomatch** | ^4.0.3 | Glob pattern matching for guard file filters | Lightweight, zero-dep glob matcher |
| **better-sqlite3** | ^12.6.2 | Analytics database (session events, friction tracking) | Synchronous SQLite -- no async overhead for writes |
| **React** | ^19.2.4 | JSX rendering for the CLI (used with Ink) | Required by Ink for component model |
| **Ink** | ^6.8.0 | Terminal UI framework for CLI commands | React-based terminal rendering -- declarative CLI output |

**Key patterns:**

- ESM-only (`"type": "module"` in package.json)
- `NodeNext` module resolution (`.js` extensions in imports)
- `jsx: "react-jsx"` for Ink components
- Path alias: `@/*` maps to `./src/*`

## Layer 2: Python TUI (Runtime)

| Tool | Version | What It Does | Why This Choice |
|------|---------|-------------|-----------------|
| **Python** | >=3.10 | Runtime for the TUI | Required by Textual |
| **Textual** | >=1.0.0 | Full-featured terminal UI framework (tabs, widgets, CSS) | Rich widget library, CSS-based styling, async-native |
| **Rich** | >=13.0 | Text formatting, tables, syntax highlighting within TUI | Textual's rendering foundation |
| **PyYAML** | >=6.0 | Reads `hookwise.yaml` from Python side | Standard YAML parser for Python |
| **Anthropic SDK** | >=0.40.0 | Claude API calls for coaching tab suggestions | Official SDK for Claude integration |

**Key patterns:**

- Uses `hatchling` as build backend
- TUI has 8 tabs: Dashboard, Guards, Feeds, Analytics, Coaching, Recipes, Insights, Status
- Custom widgets: `FeatureCard`, `Sparkline`, `FeedHealth`, `WeatherBackground`

## Layer 3: Build and Bundling

| Tool | Version | What It Does | Config File |
|------|---------|-------------|-------------|
| **tsup** | ^8.5.1 | Bundles TypeScript to ESM JavaScript for npm distribution | `tsup.config.ts` |
| **tsx** | ^4.21.0 | Runs TypeScript files directly (daemon process: `node --import tsx`) | -- |
| **tsc** | ^5.9.3 | Type checking only (`tsc --noEmit`), not used for compilation | `tsconfig.json` |
| **hatchling** | -- | Python build backend for TUI package | `tui/pyproject.toml` |

tsup has two entry bundles:

1. **Core dispatcher** -- externals: react, ink; bundles: js-yaml, picomatch
2. **CLI + daemon + API** -- CLI app, daemon process, public API, testing utilities

## Layer 4: Testing

69 test files across multiple testing tiers.

### TypeScript Testing Stack

| Tool | Version | What It Does | Config |
|------|---------|-------------|--------|
| **Vitest** | ^4.0.18 | Unit + integration test runner | `vitest.config.ts` |
| **V8 Coverage** | built-in | Code coverage provider | `coverage.provider: "v8"` |
| **ink-testing-library** | ^4.0.0 | Renders Ink/React CLI components in memory for assertions | -- |
| **vi.mock() / vi.fn()** | built-in | Vitest's built-in mocking (no separate library needed) | -- |

**Test directory structure:**

```
tests/
  core/           # Unit tests
    feeds/        # Feed producers + segments
    coaching/     # Coaching engine
    analytics/    # Analytics engine
    status-line/  # Segment renderers + two-tier layout
  config/         # Config parsing + validation
  cli/            # CLI command rendering (Ink)
  integration/    # End-to-end dispatcher + pipeline wiring
  recipes/        # Recipe loading
  testing/        # Test utility exports
  performance/    # Performance benchmarks
  fixtures/       # Test data (usage-data, facets, session-meta)
```

**Testing patterns used:**

- **Fake timers** (`vi.useFakeTimers()`) -- for time-dependent segments (clock, duration, practice)
- **Filesystem mocking** (`vi.mock("node:fs")`) -- daemon tests mock PID files without touching real FS
- **Process mocking** (`process.kill = vi.fn()`) -- daemon health checks without killing real processes
- **Real filesystem integration** (`mkdtempSync`) -- pipeline-wiring tests use real temp dirs + cache files
- **TTL freshness testing** -- feed segments tested with fresh vs stale cache entries

### Python Testing Stack

| Tool | What It Does | Config |
|------|-------------|--------|
| **pytest** | Test runner for TUI | `tui/pyproject.toml` |
| **pytest-asyncio** | Async test support (Textual is async) | `asyncio_mode = "auto"` |

3 test files: `test_weather.py`, `test_data.py`, `test_app.py`

### Planned Testing Tools (v1.4)

| Tool | Type | Prevents |
|------|------|----------|
| **pytest-textual-snapshot** | TUI visual regression (SVG snapshots) | Layout bugs |
| **dependency-cruiser** | TypeScript architectural linting (import graph rules) | Orphaned segments, wiring gaps |
| **fast-check** | Property-based testing (random input generation) | Scoring invariant violations |

## Layer 5: Documentation

| Tool | Version | What It Does | Config |
|------|---------|-------------|--------|
| **VitePress** | ^1.6.4 | Static site generator for docs (Markdown to HTML) | `docs/` directory |
| **GitHub Pages** | -- | Hosting for docs site | `.github/workflows/docs.yml` |

Docs auto-deploy on push to `main` when `docs/**` changes.

## Layer 6: CI/CD and Distribution

| Tool | What It Does | Config |
|------|-------------|--------|
| **GitHub Actions** | CI/CD platform | `.github/workflows/` |
| **npm publish** | Package distribution (with provenance) | `publish.yml` -- triggered on GitHub Release |
| **Trusted Publishing** | OIDC-based npm auth (no tokens stored) | `id-token: write` permission |

**Two workflows:**

1. `publish.yml` -- On release: `npm ci` then `build` then `test` then `publish --provenance`
2. `docs.yml` -- On push to main (docs/ changes): build VitePress then deploy to GitHub Pages

## Layer 7: Dev Process (SDLC)

Tools used to build hookwise (not in package dependencies):

| Tool | What It Does |
|------|-------------|
| **Claude Code** | AI-assisted development (hookwise is built FOR this tool) |
| **PDLC Autopilot** | Director/Actor/Critic pattern for autonomous spec-driven development |
| **Kiro** | Spec generation (requirements.md to design.md to tasks.md) |
| **CodeRabbit** | Automated PR review |

## Cross-Cutting: IPC and Data Flow

| Mechanism | What It Connects |
|-----------|-----------------|
| **Filesystem JSON cache** (`~/.hookwise/cache.json`) | Daemon, Status Line, TUI |
| **YAML config** (`hookwise.yaml`) | User config to all components |
| **SQLite DB** (`~/.hookwise/analytics.db`) | Dispatcher to Analytics to Stats CLI |
| **PID files** (`~/.hookwise/daemon.pid`, `tui.pid`) | Process lifecycle management |
| **Detached subprocess** (`spawn` with `detached: true`) | Core to TUI launcher, Core to Daemon |
| **stdin pipe** | Claude Code to `hookwise status-line` (context_window, cost data) |
