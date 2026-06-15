# openlore Onboarding — Spec Drift Report

**Date:** 2026-06-15
**Branch:** `feat/openlore-openspec-onboarding`
**Tool:** `openlore drift` (structural, git-diff based — no LLM)

## Result

```
Spec Drift Detection
  Base ref: main
  Branch: feat/openlore-openspec-onboarding
  Spec domains: 7
  Mapped source files: 32

[ok] No spec drift detected. Specs are in sync with code changes.
  Duration: 110ms
```

## Interpretation

`openlore drift` compares the git diff (branch vs `main`) against the spec→source mappings.
This branch changes only documentation/config (`.gitignore`, `CLAUDE.md` OpenLore block,
`.mcp.json`, `openspec/`, `.openlore/decisions/`) — **no Go source files changed** — so there is
**no code that has drifted away from its generated spec**. The 7 living specs in `openspec/specs/`
(analytics, architecture, feeds, notifications, overview, pricing, transcript) were generated from
the current `main` code and match it.

Going forward, `openlore drift` (or the `check_spec_drift` MCP tool) run on any branch that touches
the 32 mapped source files will flag code changes not yet reflected in the specs. Any genuine
divergence should be written up as a decision record in `.openlore/decisions/` (per the onboarding
convention), not silently dropped.

## How to re-run

```bash
openlore drift            # structural, fast
openlore drift --use-llm  # semantic drift analysis (slower, needs provider)
```
