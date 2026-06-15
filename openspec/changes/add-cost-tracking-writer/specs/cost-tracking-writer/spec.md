# Cost-Tracking Writer

Per-session cost computation and accumulation from Claude Code conversation transcripts.

## ADDED Requirements

### Requirement: Transcript-based token extraction

The transcript reader MUST extract per-model token usage from the Claude Code conversation
transcript `.jsonl` file.

#### Scenario: Valid transcript with assistant usage blocks
- GIVEN a `.jsonl` file with assistant messages containing `usage` blocks
- WHEN `SumUsage(path)` is called
- THEN it MUST return a map from model name to summed `Usage{Input, Output, CacheRead, CacheWrite}`
- AND the sums MUST equal the total across all assistant messages for that model

#### Scenario: Multi-model transcript
- GIVEN a `.jsonl` file with messages from multiple models (e.g., haiku and sonnet)
- WHEN `SumUsage(path)` is called
- THEN it MUST return separate entries for each model in the result map

#### Scenario: Missing transcript file
- GIVEN a path to a file that does not exist
- WHEN `SumUsage(path)` is called
- THEN it MUST return an empty map (not an error)

#### Scenario: Malformed lines in transcript
- GIVEN a `.jsonl` file where some lines are invalid JSON or missing the `usage` field
- WHEN `SumUsage(path)` is called
- THEN it MUST skip malformed lines silently
- AND MUST return valid sums for the remaining valid lines

#### Scenario: Non-assistant messages
- GIVEN a `.jsonl` file with `role:"user"` or `role:"tool"` messages
- WHEN `SumUsage(path)` is called
- THEN it MUST ignore those messages (not include their data in sums)

### Requirement: Per-model pricing computation

The pricing layer MUST compute USD cost from model name and token usage.

#### Scenario: Known model pricing
- GIVEN a known model string (e.g., `claude-opus-4-5`, `claude-sonnet-4-5`, `claude-haiku-4-5`)
- AND a Usage struct with non-zero token counts
- WHEN `Compute(model, usage)` is called
- THEN it MUST return a positive float64 representing USD cost
- AND the result MUST equal `(input * inputRate + output * outputRate + cacheRead * cacheReadRate + cacheWrite * cacheWriteRate) / 1_000_000`

#### Scenario: Custom rate overrides
- GIVEN a `CostTrackingConfig.Rates` map with an override for a specific model
- WHEN `ComputeWithRates(model, usage, overrides)` is called
- THEN it MUST use the override rates instead of the built-in table

#### Scenario: Unknown model
- GIVEN a model string not in the rate table
- WHEN `Compute(model, usage)` is called
- THEN it MUST return 0.0 (not an error, not a panic)

#### Scenario: Zero usage
- GIVEN a Usage struct with all zero token counts
- WHEN `Compute(model, usage)` is called
- THEN it MUST return 0.0

### Requirement: Idempotent cost accumulation on Stop

The dispatch `Stop` handler MUST accumulate cost idempotently.

#### Scenario: First Stop for a session
- GIVEN a session with no previously recorded cost
- WHEN a `Stop` event fires with a transcript path
- THEN `cost_state.TotalToday` MUST increase by the session cost
- AND `sessions.estimated_cost_usd` MUST be set to the session cost

#### Scenario: Second Stop for the same session (idempotent)
- GIVEN a session whose cost was already accumulated
- WHEN a second `Stop` event fires with the same transcript (no new turns)
- THEN `cost_state.TotalToday` MUST NOT increase further
- AND the accumulated delta MUST be approximately 0

#### Scenario: Stop with missing or empty transcript
- GIVEN a `Stop` event where `transcript_path` is empty or the file does not exist
- WHEN the Stop handler runs
- THEN it MUST exit 0 (ARCH-1 fail-open)
- AND `cost_state.TotalToday` MUST remain unchanged

### Requirement: HookPayload carries transcript_path

`HookPayload` MUST expose the `transcript_path` field from the Claude Code hook envelope.

#### Scenario: Stop event with transcript_path in stdin JSON
- GIVEN a `Stop` stdin payload containing `"transcript_path": "/path/to/transcript.jsonl"`
- WHEN the payload is unmarshalled into `HookPayload`
- THEN `HookPayload.TranscriptPath` MUST equal the provided path

### Requirement: Fork bomb guard in spawnDaemon

`spawnDaemon` MUST refuse to re-exec when running inside a test binary.

#### Scenario: Executing as a test binary
- WHEN `os.Executable()` returns a path ending in `.test`
- THEN `spawnDaemon` MUST return without spawning any process

#### Scenario: HOOKWISE_DISABLE_DAEMON_AUTOSTART set
- WHEN `HOOKWISE_DISABLE_DAEMON_AUTOSTART=1` is set in the environment
- THEN `spawnDaemon` MUST return without spawning any process

#### Scenario: Normal binary execution
- WHEN `os.Executable()` returns a path not ending in `.test`
- AND `HOOKWISE_DISABLE_DAEMON_AUTOSTART` is unset
- THEN `spawnDaemon` MUST proceed normally
