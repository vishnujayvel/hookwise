# Contributing to hookwise

Thanks for your interest in contributing to hookwise! Here's how to get started.

## Development Setup

```bash
git clone https://github.com/vishnujayvel/hookwise.git
cd hookwise
npm install
npm test          # 1363 tests via vitest
npm run build     # tsup build
npm run typecheck # tsc --noEmit
```

## Project Structure

```
src/
  core/           # Dispatcher, config, guards, analytics, coaching
    analytics/    # SQLite analytics engine
    coaching/     # Metacognition, builder's trap, communication
    feeds/        # Feed platform: producers, cache bus, registry
    status-line/  # Composable status segments
  cli/            # CLI commands (init, doctor, status, stats, test, migrate)
  testing/        # HookRunner, HookResult, GuardTester

tests/            # 1363+ tests
recipes/          # 12 built-in recipes
examples/         # Example configs
```

## Making Changes

1. **Fork and branch:** Create a feature branch from `main`
2. **Write tests first:** All new features need tests. Use vitest.
3. **Follow existing patterns:** Look at similar code for conventions
4. **Keep it simple:** hookwise values clarity over cleverness
5. **Test your guards:** Use `GuardTester` for guard rule testing

## Testing

```bash
npm test              # Run all tests
npm run test:watch    # Watch mode
npm run typecheck     # Type checking
```

## Creating a Recipe

Recipes live in `recipes/{category}/{name}/`. Each recipe needs:
- `recipe.yaml` — The recipe configuration
- `README.md` — Documentation

See [Creating a Recipe](docs/creating-a-recipe.md) for details.

## Pull Request Guidelines

- Keep PRs focused — one feature or fix per PR
- Update tests for any behavior changes
- Update README if adding user-facing features
- Run the full test suite before submitting

## Code of Conduct

Be kind. Be constructive. We're all here to make AI development safer and more productive.

## Questions?

Open an issue or start a discussion. We're happy to help!
