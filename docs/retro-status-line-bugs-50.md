# Retrospective: Status-Line Pipeline Disconnection (Bug #50+)

**Date:** 2026-03-08
**Severity:** Medium (user-visible but non-blocking)
**Root Cause Category:** Integration gap — components work in isolation but fail end-to-end

## What Happened

The status line showed `session: -- | cost: -- | project: -- | calendar: --` — all placeholder
values despite data being available. Weather (the only live producer) was missing entirely.

## Five Bugs, One Root Cause Pattern

| Bug | Symptom | Root Cause | Layer |
|-----|---------|------------|-------|
| **B1: session: --** | No session count displayed | `dispatch` never called `analytics.StartSession()` — the analytics API was built but never wired to the dispatch pipeline | Integration |
| **B2: cost: --** | No cost displayed | Same as B1 — $0.00 because no sessions recorded | Integration |
| **B3: weather missing** | Weather segment not shown | `hookwise.yaml` segments list omitted `weather` despite weather feed being enabled and producing real data | Config |
| **B4: project/calendar: --** | Always shows `--` | Producers return `source: "placeholder"` — correctly filtered but ships as "done" | Design debt |
| **B5: timezone mismatch** | Same-connection queries return 0 | `StartSession` stores UTC timestamps but `DailySummary` was queried with local date — fails in negative UTC offsets (PST evening → UTC next day) | Data |

## First-Principle Analysis

### Why does each component work in isolation but fail end-to-end?

The hookwise architecture has 4 independent data pipelines:

```text
Pipeline 1: Daemon → Feed JSON → bridge.CollectFeedCache → renderSegment
Pipeline 2: Dispatch → [MISSING] → Dolt → DailySummary → renderBuiltinSegment
Pipeline 3: Config YAML → LoadConfig → StatusLine.Segments → segment loop
Pipeline 4: StartSession(UTC) → Dolt → DailySummary(local date) → mismatch
```

Each pipeline was tested within its own boundary:
- Feed producers have unit tests proving they write correct JSON
- Analytics API has tests proving StartSession/DailySummary work (with UTC-safe fixed dates)
- Config loading has tests proving YAML parsing works
- Segment rendering has tests proving each segment renders correctly

But **no test ever validated the full pipeline from dispatch → Dolt → status-line → screen**.

### The "Mock Confidence Trap" (Pattern #2)

This is the exact same pattern as Bug #29 (weather feed):

> Mock-based tests pass on both sides of a boundary while the real system fails.

**Bug #29:** Go producer writes `unit` → Python TUI reads `temperatureUnit` → both sides' tests pass.
**Bug #50:** Go dispatch has analytics infrastructure → Go status-line reads from Dolt → both sides' tests pass but dispatch never writes.

The mock confidence trap has three variants:

1. **Field mismatch** (Bug #29): Both sides mock the field name they expect
2. **Integration gap** (Bug #50 B1/B2): Both sides work independently but the bridge between them was never built
3. **Format mismatch** (Bug #50 B5): Both sides work with their own format (UTC vs local) but never tested together

### Why didn't tests catch this?

| Testing Layer | What It Validates | What It Misses |
|---------------|-------------------|----------------|
| Unit tests | `StartSession` writes rows; `DailySummary` queries rows | Whether dispatch calls StartSession at all |
| Contract tests | JSON stdout format matches spec | Whether analytics writes happen |
| Integration tests | Dispatch returns correct exit codes | Whether side effects (analytics) fire |
| Architecture tests | Package dependency rules | Whether wiring between packages exists |
| Property-based tests | Invariants within a single domain | Cross-domain data flow |

**The gap:** No test validates the **existence of a connection** between two components, only the correctness of each component in isolation.

### Why did placeholder producers ship?

The project/calendar/pulse/news producers were implemented as placeholders (`source: "placeholder"`) and marked as "done" in the task tracker. The renderer correctly filters placeholders, so the status line shows `--`. But:

1. The spec didn't distinguish "producer skeleton exists" from "producer returns real data"
2. The `feedData()` filter for `source: "placeholder"` was added as a safety net, not as a signal that the feature is incomplete
3. No acceptance test validates that a segment shows **non-placeholder** data

## Fixes Applied

1. **B1/B2:** Added `recordAnalytics()` function in dispatch — writes SessionStart/PostToolUse/SessionEnd to Dolt with commit
2. **B3:** Added `weather` to `hookwise.yaml` segments list and global config
3. **B5:** Changed all `time.Now().Format("2006-01-02")` to `time.Now().UTC().Format("2006-01-02")` in status-line and stats commands to match analytics' UTC storage
4. **B4:** No code fix — producers need real implementations (tracked separately)

## New Tests Added

1. `TestPersistAcrossConnections` — validates Dolt data survives close+reopen (analytics package)
2. `TestRecordAnalytics_SessionStart` — validates dispatch→Dolt→DailySummary pipeline
3. `TestRecordAnalytics_PostToolUse` — validates event recording pipeline
4. All use UTC dates to match production behavior

## Lessons Learned

### L1: "Infrastructure-ready" ≠ "Wired"

The dispatch command had goroutine infrastructure, 50ms grace period, and side-effect handler patterns — everything EXCEPT the actual analytics call. The comment `// Brief grace period for side-effect goroutines (analytics, coaching)` promised work that didn't exist. **Lesson:** Comments describing intended behavior are not substitutes for code.

### L2: Timezone bugs hide in happy-path tests

All existing analytics tests used `time.Date(2025, 3, 6, 9, 0, 0, 0, time.UTC)` — a UTC timestamp where local and UTC dates are the same. The bug only manifests in negative UTC offsets during evening hours. **Lesson:** Test with edge-case timestamps (midnight, day boundaries, DST transitions).

### L3: Config completeness is untestable without a "full pipeline" smoke test

Weather was enabled in feeds config but absent from segments config. No test validates that enabled feeds appear in the status line. **Lesson:** A single e2e test that runs `hookwise status-line` with a known config and validates all expected segments appear would catch this instantly.

### L4: The Mock Confidence Trap is a recurring pattern

This is the third time (after Bug #29 and the original status-line stub bug) that both sides of a boundary pass all tests while the real system fails. **Lesson:** After shipping any feature that crosses a producer→consumer boundary, add an integration test that validates the full pipeline with real (not mocked) data.

## Proposed E2E Test

> **Note:** This is pseudocode illustrating the test structure, not a runnable test.

```go
// TestStatusLine_EndToEnd_DispatchThenRender validates the full pipeline:
// dispatch SessionStart → Dolt write → status-line reads → session count appears
func TestStatusLine_EndToEnd_DispatchThenRender(t *testing.T) {
    tmpDir := t.TempDir()
    // 1. Write config with known segments
    // 2. Call recordAnalytics(SessionStart) with tmpDir
    // 3. Write weather.json with real data to cacheDir
    // 4. Run status-line command
    // 5. Assert: session count > 0, weather shows temperature, weather not "--"
}
```

## Action Items

- [ ] Implement real project producer (git repo detection)
- [ ] Implement real calendar producer (Google Calendar MCP)
- [ ] Add e2e smoke test for status-line pipeline
- [ ] Add UTC-edge-case timestamp to analytics test suite
- [ ] Consider adding a CI job that runs `hookwise status-line` with a test config and validates non-empty output
