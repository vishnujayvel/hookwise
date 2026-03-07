# Tech Stack

A complete reference of every framework, library, and tool used in hookwise, organized by SDLC layer.

## Architecture Overview

Hookwise is a **polyglot project** with two runtimes:

- **Go core engine** -- a single compiled binary (config, dispatcher, feeds, analytics, CLI, status line)
- **Python TUI** -- a separate Textual app launched as a detached subprocess

They communicate via a **shared filesystem cache** (JSON file), not HTTP or IPC.

## Layer 1: Go Core (Runtime)

| Tool | Version | What It Does | Why This Choice |
|------|---------|-------------|-----------------|
| **Go** | 1.25 | Runtime and compiler for the core engine | Single binary, fast startup, static typing built-in |
| **gopkg.in/yaml.v3** | v3 | Parses `hookwise.yaml` config files | Standard Go YAML parser |
| **gobwas/glob** | -- | Glob pattern matching for guard file filters | Lightweight glob matcher for Go |
| **dolthub/driver** | -- | Embedded Dolt database (version-controlled SQL data layer) | Git-like versioning for analytics and event data |
| **modernc.org/sqlite** | -- | Pure-Go SQLite reader for migration from v1 | Reads legacy SQLite analytics DB during upgrade |
| **github.com/spf13/cobra** | -- | CLI framework for all hookwise commands | Industry standard Go CLI framework |
| **github.com/stretchr/testify** | -- | Test assertions and requirements | Expressive assertions (`assert`, `require`) for Go tests |

**Key patterns:**

- Single compiled binary -- no runtime dependencies, no npm
- Internal packages (`internal/`) enforce encapsulation at the compiler level
- `-ldflags` version injection at build time
- `SetMaxOpenConns(1)` for serialized Dolt writes (ARCH-2)

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

| Tool | What It Does | Config File |
|------|-------------|-------------|
| **go build** | Compiles the Go binary (`cmd/hookwise/main.go`) | `go.mod` |
| **-ldflags** | Injects version, commit hash, and build date at compile time | -- |
| **hatchling** | Python build backend for TUI package | `tui/pyproject.toml` |

## Layer 4: Testing

Multiple testing tiers across Go packages and Python TUI.

### Go Testing Stack

| Tool | What It Does | Config |
|------|-------------|--------|
| **go test** | Built-in test runner for all Go packages | `go.mod` |
| **go test -cover** | Built-in code coverage | `go test -coverprofile=coverage.out` |
| **testify/assert** | Expressive test assertions | -- |
| **testify/require** | Fatal assertions that stop test on failure | -- |
| **Test interfaces + DI** | Dependency injection for testability (no runtime mocking) | -- |

**Test directory structure:**

```text
cmd/hookwise/           # CLI tests (main_test.go)
internal/core/          # Unit tests
internal/feeds/         # Feed producer tests
internal/analytics/     # Analytics engine tests
internal/contract/      # Contract tests (33 JSON fixtures)
internal/migration/     # Migration tests
internal/bridge/        # TUI cache bridge tests
internal/notifications/ # Notification tests
internal/arch/          # Architecture linting (go/packages)
internal/proptest/      # Property-based testing (pgregory.net/rapid)
internal/chaos/         # Chaos/failure mode tests (integration build tag)
internal/mutation/      # Mutation testing (mutation build tag)
pkg/hookwise/testing/   # Test utility exports (hwtesting)
```

**Testing patterns used:**

- **Test interfaces** -- dependencies injected via interfaces, swapped with test doubles in tests
- **Build tags** -- `//go:build integration` and `//go:build mutation` separate test tiers
- **Contract fixtures** -- 33 JSON fixtures in `testdata/contracts/` validate byte-identical output parity
- **Real filesystem integration** (`os.MkdirTemp`) -- pipeline-wiring tests use real temp dirs + cache files
- **TTL freshness testing** -- feed segments tested with fresh vs stale cache entries
- **Race detector** -- `go test -race` clean across all packages

### Python Testing Stack

| Tool | What It Does | Config |
|------|-------------|--------|
| **pytest** | Test runner for TUI | `tui/pyproject.toml` |
| **pytest-asyncio** | Async test support (Textual is async) | `asyncio_mode = "auto"` |

3 test files: `test_weather.py`, `test_data.py`, `test_app.py`

### Advanced Testing Tiers (v2)

| Tool | Type | Prevents |
|------|------|----------|
| **pytest-textual-snapshot** | TUI visual regression (SVG snapshots) | Layout bugs |
| **go/packages** (internal/arch) | Architecture linting (import graph rules) | Package dependency violations |
| **pgregory.net/rapid** (internal/proptest) | Property-based testing (random input generation) | Scoring invariant violations |
| **internal/mutation** | Mutation testing (3 operators, 93.3% kill rate) | Weak test assertions |
| **internal/chaos** | Chaos/failure mode testing (12+ scenarios) | Unhandled failure paths |

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
| **goreleaser** / `go build` | Binary distribution (cross-compiled binaries) | `publish.yml` -- triggered on GitHub Release |

**Two workflows:**

1. `publish.yml` -- On release: `go build` (or goreleaser) then `test` then publish binaries
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
| **Dolt DB** (`~/.hookwise/dolt/`) | Dispatcher to Analytics to Stats CLI (version-controlled SQL) |
| **PID files** (`~/.hookwise/daemon.pid`, `tui.pid`) | Process lifecycle management |
| **os/exec with SysProcAttr** | Core to TUI launcher, Core to Daemon (detached subprocess) |
| **stdin pipe** | Claude Code to `hookwise status-line` (context_window, cost data) |
