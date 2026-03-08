# Contributing to hookwise

Thanks for your interest in contributing to hookwise! Here's how to get started.

## Development Setup

```bash
git clone https://github.com/vishnujayvel/hookwise.git
cd hookwise
go test -race ./...    # Run all Go tests
task check             # Fast pre-commit checks
task test              # Full test suite
```

Requires: Go 1.25+, Python 3.11+ (for TUI), Docker (for Dagger pipeline).

## Project Structure

```
cmd/hookwise/     # CLI entry point (Cobra commands)
internal/
  core/           # Dispatch engine, guards, config, types
  analytics/      # Dolt-based analytics
  feeds/          # Feed producers and daemon
  bridge/         # Go→JSON→Python TUI bridge
  contract/       # Contract parity tests (33 JSON fixtures)
  arch/           # Architecture dependency lint
  proptest/       # Property-based tests
tui/              # Python Textual TUI (separate venv at tui/.venv/)
recipes/          # 12 built-in recipes
examples/         # Example configs
dagger/           # Dagger CI/CD pipeline module
```

## Making Changes

1. **Fork and branch:** Create a feature branch from `main`
2. **Write tests first:** All new features need tests. Use `testify/assert`.
3. **Follow existing patterns:** Look at similar code for conventions
4. **Keep it simple:** hookwise values clarity over cleverness
5. **Rebuild after changes:** Run `task install` to rebuild the binary

## Testing

```bash
go test -race ./...                           # All unit tests
go test -race -tags integration ./...         # + integration/chaos tests
go test -race -tags mutation ./...            # + mutation tests
cd tui && .venv/bin/python -m pytest tests/   # TUI tests
task pr                                       # Full PR readiness pipeline
dagger call test --src=.                      # Containerized (same as CI)
```

## Creating a Recipe

Recipes live in `recipes/{category}/{name}/`. Each recipe needs:
- `hooks.yaml` — The recipe configuration
- `README.md` — Documentation

See [Creating a Recipe](docs/guide/creating-a-recipe.md) for details.

## Pull Request Guidelines

- Keep PRs focused — one feature or fix per PR
- Update tests for any behavior changes
- Update README if adding user-facing features
- Run `task pr` or `dagger call test --src=.` before submitting

## Code of Conduct

Be kind. Be constructive. We're all here to make AI development safer and more productive.

## Questions?

Open an issue or start a discussion. We're happy to help!
