# Hookwise — Development Guide

## The #1 Rule

**After changing Go code, run `make install` before testing.**

Hookwise is invoked by Claude Code hooks via the binary at `/opt/homebrew/bin/hookwise`.
Source code changes have NO effect until the binary is rebuilt. This has caused real
incidents where everything "looked fine" in source but the installed binary was stale.

## Daily Dev Workflow

```bash
# After any Go code change:
make install          # Rebuild + install binary with version info

# Full check (install + doctor + status-line verify):
make dev

# Quick iteration without installing (runs from source):
make run ARGS="status-line"
make run ARGS="dispatch PreToolUse"

# Run tests:
make test             # Unit tests with race detector
make test-integration # Integration tests (chaos, etc.)
make test-tui         # Python TUI tests

# Check if installed binary matches source:
make version-check
```

## Pipeline (Dagger)

Dagger runs the full CI pipeline in containers — identical locally and in GitHub Actions.
Requires: Docker running + `brew install dagger/tap/dagger`.

```bash
dagger call ci --src=.              # Full pipeline (what CI runs)
dagger call check --src=.           # Tier 0: vet + compile + lint
dagger call test --src=.            # Tier 1: unit + contract + arch + PBT + TUI
dagger call validate --src=.        # Tier 2: integration + mutation + snapshots
dagger call build --src=.           # Build binary only
dagger call tui-test-matrix --src=. # TUI tests on Python 3.11/3.12/3.13
```

When to use what:
- `make install` / `make dev` — after code changes, fastest loop (no Docker)
- `dagger call check` — pre-commit in containers
- `dagger call ci` — pre-push, full pipeline (same as GitHub Actions)

## Project Structure

- `cmd/hookwise/` — CLI entry point (Cobra commands)
- `internal/core/` — Dispatch engine, guards, config, types
- `internal/analytics/` — Dolt-based analytics
- `internal/feeds/` — Feed producers and daemon
- `internal/bridge/` — Go→JSON→Python TUI bridge
- `internal/notifications/` — Notification platform
- `internal/migration/` — TypeScript→Go data migration
- `tui/` — Python Textual TUI (separate venv at `tui/.venv/`)

## Architecture Constraints

- **ARCH-1**: Fail-open — dispatch always exits 0 on error
- **ARCH-2**: Serialized Dolt writes via SetMaxOpenConns(1)
- **ARCH-5**: First-match-wins guards
- **ARCH-6**: Contract parity (byte-identical stdout with TypeScript)
- **ARCH-7**: Side effects non-blocking with per-goroutine recover()

## Testing

```bash
go test -race ./...                           # All unit tests
go test -race -tags integration ./...         # + integration/chaos tests
go test -race -tags mutation ./...            # + mutation tests
cd tui && .venv/bin/python -m pytest tests/   # TUI tests
```

- Use `testify/assert` + `require` (not stdlib testing alone)
- Contract tests use JSON fixtures in `testdata/contracts/`
- TUI snapshot tests: `pytest --snapshot-update` to regenerate

## Common Gotchas

1. **Stale binary**: `hookwise --version` shows "dev" or old commit → run `make install`
2. **Config parse errors are silent**: ARCH-1 fail-open means bad config = no output, exit 0
3. **SegmentConfig accepts strings**: `- session` works in YAML (custom UnmarshalYAML)
4. **Cross-language boundary**: Go feeds → JSON cache → Python TUI. Test both sides.
5. **TUI venv**: Always use `tui/.venv/bin/python`, never system python

## Build with Version Info

The Makefile bakes in version/commit/date via ldflags. Direct `go build` without
ldflags produces `version: dev, commit: none` — this is fine for quick tests but
`make install` is preferred for the installed binary.
