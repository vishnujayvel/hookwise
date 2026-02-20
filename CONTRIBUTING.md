# Contributing to hookwise

Thank you for your interest in contributing to hookwise. This guide covers everything you need to get started.

## Development Setup

### Prerequisites

- **Node.js 20+** (`node --version`)
- **npm** (comes with Node.js)

### Clone and Install

```bash
git clone https://github.com/vishnujayvel/hookwise.git
cd hookwise
npm install
```

### Build

```bash
npm run build
```

This runs `tsup` with two build entries:
- `dist/core/dispatcher.js` -- The dispatch hot path (no React/Ink)
- `dist/cli/` and `dist/index.js` -- CLI, TUI, and public API

### Type Check

```bash
npm run typecheck
```

Runs `tsc --noEmit` against the entire codebase.

### Run Tests

```bash
# Run all tests
npm test

# Watch mode
npm run test:watch

# With coverage
npm run test:coverage
```

The test suite has 911+ tests across 41 test files. All tests must pass before a PR is merged.

### Test Structure

```
tests/
  core/              # Unit tests for each module
    guards.test.ts
    config.test.ts
    dispatcher.test.ts
    ...
  integration/       # End-to-end dispatch flow tests
    dispatch-flow.test.ts
    compat.test.ts
  performance/       # Benchmarks and import boundary checks
    benchmarks.test.ts
  cli/               # CLI command tests
  tui/               # TUI component tests (uses ink-testing-library)
  recipes/           # Recipe loading and merging tests
  fixtures/          # Test fixtures (YAML configs, etc.)
```

## Project Structure

```
src/
  core/              # Core modules (no React/Ink)
    dispatcher.ts    # Three-phase dispatch engine
    config.ts        # YAML config loading and merging
    guards.ts        # Guard rule evaluation engine
    types.ts         # TypeScript type definitions
    errors.ts        # Error handling and logging
    state.ts         # Atomic state management
    constants.ts     # Default paths and settings
    analytics/       # SQLite analytics engine
    coaching/        # Metacognition, builder's trap, communication
    status-line/     # Composable status segments
    ...
  cli/               # CLI layer
    app.tsx          # CLI app shell (React Ink)
    commands/        # CLI commands (init, doctor, status, stats, test, migrate)
    tui/             # Interactive TUI
      app.tsx        # TUI shell with tab navigation
      tabs/          # Tab components
  testing/           # Public testing API
    hook-runner.ts   # Subprocess-based hook testing
    hook-result.ts   # Assertion helpers
    guard-tester.ts  # In-process guard testing

recipes/             # Built-in recipes (included in npm package)
examples/            # Example configurations
docs/                # Documentation
```

## Architecture Rules

### Import Boundary

The most critical architectural rule: **`src/core/dispatcher.ts` must NOT import React or Ink.**

The dispatcher is the hot path -- it runs on every hook event. Importing React would add startup latency and break the single-concern design. The performance tests verify this boundary.

Core modules (`guards.ts`, `config.ts`, `errors.ts`, `state.ts`, `types.ts`) must also avoid React/Ink imports.

### Fail-Open Philosophy

hookwise must never accidentally block a tool call due to internal errors. Every dispatch path is wrapped in `safeDispatch()` which catches all exceptions and returns `exit 0`.

When writing new code:
- Wrap handler execution in try/catch
- Log errors but never throw from the dispatch path
- Default to "allow" when in doubt

### Three-Phase Execution

All hook events flow through three phases:

1. **Guards** -- Block/allow decisions. First block wins.
2. **Context** -- Enrichment (greeting, coaching prompts). Multiple handlers merge.
3. **Side Effects** -- Non-blocking observations (analytics, sounds, transcript).

New handlers must fit into one of these phases.

## Creating a Recipe

See [docs/creating-a-recipe.md](docs/creating-a-recipe.md) for the full guide. In summary:

1. Create a directory under `recipes/<category>/<name>/`
2. Add `hooks.yaml` with name, version, events, config
3. Add `handler.ts` with the implementation
4. Add `README.md` with documentation
5. Add tests

## Testing

### Unit Tests

Each core module has a corresponding test file. Follow existing patterns:

```typescript
import { describe, it, expect } from "vitest";

describe("myFunction", () => {
  it("does the expected thing", () => {
    const result = myFunction(input);
    expect(result).toBe(expected);
  });
});
```

### Integration Tests

Integration tests in `tests/integration/` create real temp directories with `hookwise.yaml` files and exercise the full dispatch pipeline:

```typescript
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { dispatch } from "../../src/core/dispatcher.js";

let tempDir: string;

beforeEach(() => {
  tempDir = mkdtempSync(join(tmpdir(), "hookwise-test-"));
  process.env.HOOKWISE_STATE_DIR = tempDir;
});

afterEach(() => {
  delete process.env.HOOKWISE_STATE_DIR;
  rmSync(tempDir, { recursive: true, force: true });
});
```

### Performance Tests

Performance tests verify latency targets and import boundaries:

```typescript
import { performance } from "node:perf_hooks";

it("completes in < 50ms", () => {
  fn(); // warmup
  const start = performance.now();
  fn(); // measured run
  expect(performance.now() - start).toBeLessThan(50);
});
```

### TUI Tests

TUI components use `ink-testing-library`:

```typescript
import { render } from "ink-testing-library";

it("renders the component", () => {
  const { lastFrame } = render(<MyComponent />);
  expect(lastFrame()).toContain("Expected text");
});
```

## Commit Messages

Use clear, descriptive commit messages:

- `feat: add context-window-monitor recipe`
- `fix: guard glob matching for nested patterns`
- `test: add integration tests for config merge`
- `docs: update getting-started guide`
- `perf: optimize guard evaluation hot path`

## Pull Requests

1. Create a feature branch from `main`
2. Make your changes with tests
3. Ensure all tests pass: `npm test`
4. Ensure types check: `npm run typecheck`
5. Ensure build succeeds: `npm run build`
6. Open a PR with a clear description

## Code Style

- TypeScript with strict mode
- ES modules (`type: "module"` in package.json)
- Prefer `const` over `let`
- Use descriptive variable names
- JSDoc comments on all exported functions
- No `eval()` -- use regex-based parsing for conditions

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
