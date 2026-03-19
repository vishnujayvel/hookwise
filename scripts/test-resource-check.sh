#!/bin/bash
# test-resource-check.sh — Tests for pre-test-resource-check.sh
#
# Uses HOOKWISE_TEST_MODE=1 to inject mock values instead of reading real system state.
# This makes tests deterministic and portable (no dependency on actual memory/processes).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TARGET="$SCRIPT_DIR/pre-test-resource-check.sh"

passed=0
failed=0
total=0

# --- Test helpers ---

run_test() {
    local name="$1"
    local expected_exit="$2"
    local expected_pattern="$3"
    shift 3
    total=$((total + 1))

    # Run with test mode and capture output + exit code
    local output exit_code
    output=$(HOOKWISE_TEST_MODE=1 "$@" bash "$TARGET" 2>&1) || true
    exit_code=$?
    # Re-run to get actual exit code (the || true above masks it)
    HOOKWISE_TEST_MODE=1 "$@" bash "$TARGET" >/dev/null 2>&1
    exit_code=$?

    local status="PASS"
    local reason=""

    # Check exit code
    if [ "$exit_code" -ne "$expected_exit" ]; then
        status="FAIL"
        reason="expected exit $expected_exit, got $exit_code"
    fi

    # Check output pattern (if provided)
    if [ -n "$expected_pattern" ] && ! echo "$output" | grep -q "$expected_pattern"; then
        status="FAIL"
        reason="${reason:+$reason; }expected pattern '$expected_pattern' not found in output"
    fi

    if [ "$status" = "PASS" ]; then
        echo "  ✓ $name"
        passed=$((passed + 1))
    else
        echo "  ✗ $name — $reason"
        echo "    output: $(echo "$output" | head -3)"
        failed=$((failed + 1))
    fi
}

# Better test runner that captures exit code properly
assert() {
    local name="$1"
    local expected_exit="$2"
    local expected_pattern="$3"
    shift 3

    total=$((total + 1))

    local output
    local exit_code
    output=$("$@" bash "$TARGET" 2>&1)
    exit_code=$?

    local ok=1
    local reason=""

    if [ "$exit_code" -ne "$expected_exit" ]; then
        ok=0
        reason="expected exit $expected_exit, got $exit_code"
    fi

    if [ -n "$expected_pattern" ] && ! echo "$output" | grep -q "$expected_pattern"; then
        ok=0
        reason="${reason:+$reason; }pattern '$expected_pattern' not found"
    fi

    if [ "$ok" -eq 1 ]; then
        echo "  ✓ $name"
        passed=$((passed + 1))
    else
        echo "  ✗ $name — $reason"
        echo "    output: $(echo "$output" | head -2)"
        failed=$((failed + 1))
    fi
}

echo "=== pre-test-resource-check.sh tests ==="
echo ""

# --- Test 1: Happy path — no procs, plenty of memory ---
echo "Group 1: Pass conditions"

assert "No procs, 8 GB free → PASS" \
    0 "Resource check passed" \
    env HOOKWISE_TEST_MODE=1 HOOKWISE_MOCK_PROC_COUNT=0 HOOKWISE_MOCK_AVAIL_MB=8192

assert "No procs, exactly at threshold (2048 MB) → PASS" \
    0 "Resource check passed" \
    env HOOKWISE_TEST_MODE=1 HOOKWISE_MOCK_PROC_COUNT=0 HOOKWISE_MOCK_AVAIL_MB=2048

assert "No procs, 3 GB free → PASS with warning" \
    0 "WARNING" \
    env HOOKWISE_TEST_MODE=1 HOOKWISE_MOCK_PROC_COUNT=0 HOOKWISE_MOCK_AVAIL_MB=3072

echo ""

# --- Test 2: Process blocking ---
echo "Group 2: Process check"

assert "1 hookwise.test proc → BLOCKED" \
    1 "BLOCKED" \
    env HOOKWISE_TEST_MODE=1 HOOKWISE_MOCK_PROC_COUNT=1 HOOKWISE_MOCK_AVAIL_MB=8192

assert "5 hookwise.test procs → BLOCKED" \
    1 "5 hookwise.test" \
    env HOOKWISE_TEST_MODE=1 HOOKWISE_MOCK_PROC_COUNT=5 HOOKWISE_MOCK_AVAIL_MB=8192

assert "Custom threshold: 2 procs allowed, 1 running → PASS" \
    0 "Resource check passed" \
    env HOOKWISE_TEST_MODE=1 HOOKWISE_MOCK_PROC_COUNT=1 HOOKWISE_MOCK_AVAIL_MB=8192 HOOKWISE_MAX_TEST_PROCS=2

echo ""

# --- Test 3: Memory blocking ---
echo "Group 3: Memory check"

assert "0 procs, 1 GB free → BLOCKED" \
    1 "BLOCKED" \
    env HOOKWISE_TEST_MODE=1 HOOKWISE_MOCK_PROC_COUNT=0 HOOKWISE_MOCK_AVAIL_MB=1024

assert "0 procs, 512 MB free → BLOCKED" \
    1 "BLOCKED" \
    env HOOKWISE_TEST_MODE=1 HOOKWISE_MOCK_PROC_COUNT=0 HOOKWISE_MOCK_AVAIL_MB=512

assert "Custom threshold: 4 GB required, 3 GB available → BLOCKED" \
    1 "BLOCKED" \
    env HOOKWISE_TEST_MODE=1 HOOKWISE_MOCK_PROC_COUNT=0 HOOKWISE_MOCK_AVAIL_MB=3072 HOOKWISE_MIN_FREE_MB=4096

echo ""

# --- Test 4: Both failures ---
echo "Group 4: Combined failures"

assert "2 procs + 500 MB free → BLOCKED (both)" \
    1 "BLOCKED" \
    env HOOKWISE_TEST_MODE=1 HOOKWISE_MOCK_PROC_COUNT=2 HOOKWISE_MOCK_AVAIL_MB=500

echo ""

# --- Test 5: Edge cases ---
echo "Group 5: Edge cases"

assert "0 procs, 2047 MB free (just under threshold) → BLOCKED" \
    1 "BLOCKED" \
    env HOOKWISE_TEST_MODE=1 HOOKWISE_MOCK_PROC_COUNT=0 HOOKWISE_MOCK_AVAIL_MB=2047

assert "0 procs, 0 MB free → BLOCKED" \
    1 "BLOCKED" \
    env HOOKWISE_TEST_MODE=1 HOOKWISE_MOCK_PROC_COUNT=0 HOOKWISE_MOCK_AVAIL_MB=0

echo ""

# --- Test 6: Real system (non-mocked) ---
echo "Group 6: Real system integration"

total=$((total + 1))
real_output=$(bash "$TARGET" 2>&1)
real_exit=$?
if [ "$real_exit" -eq 0 ] || [ "$real_exit" -eq 1 ]; then
    echo "  ✓ Real system check runs without error (exit: $real_exit)"
    passed=$((passed + 1))
else
    echo "  ✗ Real system check returned unexpected exit: $real_exit"
    failed=$((failed + 1))
fi

# Verify real system reports actual memory
total=$((total + 1))
if echo "$real_output" | grep -qE '(MB free|GB free|check passed|BLOCKED|WARNING|unsupported)'; then
    echo "  ✓ Real system output contains memory info or status"
    passed=$((passed + 1))
else
    echo "  ✗ Real system output missing expected content"
    echo "    output: $real_output"
    failed=$((failed + 1))
fi

echo ""

# --- Summary ---
echo "=== Results: $passed/$total passed, $failed failed ==="

if [ "$failed" -gt 0 ]; then
    exit 1
fi
exit 0
