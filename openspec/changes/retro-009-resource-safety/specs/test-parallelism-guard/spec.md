# Test Parallelism Guard

Taskfile-level enforcement of safe test parallelism and memory limits.

## ADDED Requirements

### Requirement: All Go test tasks use limited parallelism

Every Go test task in Taskfile.yml MUST include `-p {{.TEST_PARALLEL}}` in its command.

#### Scenario: Unit tests
- WHEN `test:go:unit` runs
- THEN the `go test` command MUST include `-p 2`

#### Scenario: Contract tests
- WHEN `test:go:contract` runs
- THEN the `go test` command MUST include `-p 2`

#### Scenario: Integration tests
- WHEN `test:go:integration` runs
- THEN the `go test` command MUST include `-p 2`

### Requirement: All Go test tasks set GOMEMLIMIT

Every Go test task MUST set `GOMEMLIMIT=4GiB` in its environment.

#### Scenario: Environment variable present
- WHEN any Go test task runs
- THEN the `GOMEMLIMIT` environment variable MUST be set to `4GiB`

### Requirement: All Go test tasks depend on test:guard

Every Go test task MUST list `test:guard` in its `deps` array.

#### Scenario: Guard precondition
- WHEN `test:go:unit` is invoked
- THEN `test:guard` MUST run before the test command
- AND if `test:guard` fails (exit 1), the test command MUST NOT execute
