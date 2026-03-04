# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 1.3.x   | Yes       |
| < 1.3   | No        |

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly:

1. **For sensitive vulnerabilities**, email **vishnu@hookwise.dev** with a description, reproduction steps, and impact assessment. Do not open a public issue for exploitable or unpatched vulnerabilities.
2. **For low-severity or already-patched issues**, you may use the [Security Vulnerability issue template](https://github.com/vishnujayvel/hookwise/issues/new?template=security_vulnerability.md).
3. You will receive an acknowledgment within 48 hours
4. We aim to release a fix within 7 days for confirmed vulnerabilities

## Security Review Process

hookwise undergoes a structured security review pipeline on every release:

1. **Automated analysis** -- 4 parallel review agents scan the full codebase (~80 source files) across isolated domains: core engine, CLI layer, feed producers, and Python TUI
2. **False-positive filtering** -- Every candidate finding is independently validated against a strict exclusion list and concrete exploitability criteria (confidence >= 8/10 required)
3. **PR-level review** -- Every pull request is diffed against `origin/HEAD` and reviewed for security regressions before merge

The most recent full-package review (v1.3.0) covered all source files and returned **zero confirmed vulnerabilities**.

## Trust Model

hookwise operates under the following trust boundaries:

| Input | Trust Level | Rationale |
|-------|-------------|-----------|
| `hookwise.yaml` | **Trusted** | User-authored local config, equivalent to a Makefile or package.json scripts |
| CLI arguments | **Trusted** | Provided by the local user |
| Environment variables | **Trusted** | Set by the local user or system |
| Claude Code hook payloads | **Semi-trusted** | JSON from Claude Code's hook system; parsed with `JSON.parse()` (no code execution) |
| External APIs (Open-Meteo, HN, RSS) | **Untrusted** | Consumed defensively with fail-open error handling and type checking |

## Security Practices

The codebase enforces the following security practices by design:

### Input Handling
- **Parameterized SQL everywhere** -- All SQLite queries use `?` or `@param` placeholders. No string concatenation in query construction.
- **Safe YAML parsing** -- Python uses `yaml.safe_load()`; TypeScript uses js-yaml v4.x which defaults to `DEFAULT_SAFE_SCHEMA` (no `!!js/function` or code execution tags).
- **JSON-only deserialization** -- All external data is parsed with `JSON.parse()`, which cannot execute code.

### Process Execution
- **No `eval()` or `Function()`** -- Explicitly avoided throughout the codebase and documented in source comments.
- **`execFileSync` for untrusted paths** -- Used where arguments could contain special characters (e.g., calendar credentials path).
- **`spawnSync` without shell** -- The dispatcher passes handler arguments as array elements, preventing shell injection.
- **Scoped `shell: true` usage** -- The status line renderer and test helper (`HookRunner`) use `shell: true` for trusted, locally-authored commands. The feed registry uses `exec()` for producer scripts defined in the user's own `hookwise.yaml` config (same trust level as a Makefile). These are intentional — external/untrusted input never reaches shell execution.

### File System
- **Restrictive permissions** -- Database files use `0o600`; directories use `0o700` (owner-only).
- **Atomic writes** -- Config, cache, and state files use temp-file-plus-rename to prevent corruption.
- **PID files with `O_EXCL`** -- Prevents TOCTOU race conditions in daemon startup.

### Architecture
- **Fail-open design** -- Internal errors in hookwise never block Claude Code tool calls. The `safeDispatch` wrapper ensures graceful degradation.
- **Read-only access to Claude Code data** -- The insights producer reads `~/.claude/usage-data/` but never writes to it.
- **No network listeners** -- hookwise does not open any ports or accept inbound connections. The daemon communicates via the filesystem (cache bus).

## Dependencies

hookwise maintains a minimal dependency footprint:

- **Runtime** (TypeScript): `better-sqlite3`, `ink`, `react`, `js-yaml`, `picomatch`
- **Runtime** (Python TUI): `textual`, `pyyaml`, `rich`, `anthropic`
- **No native HTTP server dependencies** -- External data is fetched via Node.js built-in `fetch` or Python `urllib`
- **npm publish with provenance** -- Packages are published with `--provenance` for supply chain verification via [npm provenance attestations](https://docs.npmjs.com/generating-provenance-statements)
