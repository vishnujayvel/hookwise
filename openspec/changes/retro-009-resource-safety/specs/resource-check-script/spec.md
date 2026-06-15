# Resource Check Script

Pre-test resource gate that blocks test execution when system resources are insufficient.

## ADDED Requirements

### Requirement: Process accumulation detection

The script MUST detect existing hookwise.test processes and block test execution when any are running.

#### Scenario: No test processes running
- WHEN `count_test_procs` returns 0
- AND `HOOKWISE_MAX_TEST_PROCS` is 0 (default)
- THEN the script MUST exit 0 (allow)

#### Scenario: Test processes already running
- WHEN `count_test_procs` returns 1 or more
- AND `HOOKWISE_MAX_TEST_PROCS` is 0 (default)
- THEN the script MUST exit 1 (block)
- AND output MUST contain "BLOCKED"

#### Scenario: Custom process threshold
- WHEN `HOOKWISE_MAX_TEST_PROCS` is set to 2
- AND `count_test_procs` returns 1
- THEN the script MUST exit 0 (allow, under threshold)

### Requirement: Memory pressure detection

The script MUST detect available system memory and block when below threshold.

#### Scenario: Sufficient memory (macOS)
- WHEN available memory is >= 2048 MB
- THEN the script MUST exit 0

#### Scenario: Insufficient memory (macOS)
- WHEN available memory is < 2048 MB
- THEN the script MUST exit 1
- AND output MUST contain "BLOCKED"

#### Scenario: Warning zone
- WHEN available memory is >= 2048 MB but < 4096 MB
- THEN the script MUST exit 0
- AND output MUST contain "WARNING"

#### Scenario: Linux environment
- WHEN `vm_stat` is not available
- AND `/proc/meminfo` exists with `MemAvailable` field
- THEN the script MUST read memory from `/proc/meminfo`

#### Scenario: Unknown platform
- WHEN neither `vm_stat` nor `/proc/meminfo` is available
- THEN the script MUST exit 0 with a warning (fail-open)

### Requirement: Mock injection for testing

The script MUST support deterministic testing via mock injection.

#### Scenario: Test mode active
- WHEN `HOOKWISE_TEST_MODE=1` is set
- AND `HOOKWISE_MOCK_PROC_COUNT` and `HOOKWISE_MOCK_AVAIL_MB` are set
- THEN the script MUST use mock values instead of real system calls

### Requirement: No self-matching in process detection

The script MUST NOT match its own grep process when counting hookwise.test processes.

#### Scenario: Script running with no real test processes
- WHEN no hookwise.test binaries are running
- THEN `count_test_procs` MUST return 0 (not 1 from self-match)
