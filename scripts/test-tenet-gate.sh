#!/bin/bash
# test-tenet-gate.sh — Tests for tenet-gate.sh
#
# All inputs are injected via TENET_* env vars, so tests are deterministic
# and need no GitHub context. Mirrors the test-resource-check.sh harness.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TARGET="$SCRIPT_DIR/tenet-gate.sh"

passed=0
failed=0
total=0

# --- Test helpers ---

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

echo "=== tenet-gate.sh tests ==="
echo ""

# --- Group 1: Non-code diffs pass unconditionally ---
echo "Group 1: Non-code diffs (gate does not apply)"

assert "Docs-only diff → PASS" \
    0 "no code files" \
    env TENET_PR_TITLE="docs: update readme" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES=$'README.md\ndocs/guide.md'

assert "Config-only diff (yaml) → PASS" \
    0 "no code files" \
    env TENET_PR_TITLE="chore: bump ci" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES=$'.github/workflows/ci.yml\nhookwise.yaml'

assert "Scripts-only diff (.sh) → PASS" \
    0 "no code files" \
    env TENET_PR_TITLE="chore: tweak script" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="scripts/install.sh"

assert "Empty changed-files list → PASS" \
    0 "no code files" \
    env TENET_PR_TITLE="empty" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES=""

assert "go.mod is not code (extension is .mod) → PASS" \
    0 "no code files" \
    env TENET_PR_TITLE="chore: bump deps" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES=$'go.mod\ngo.sum'

echo ""

# --- Group 2: Code diffs with a bead ref pass ---
echo "Group 2: Bead reference satisfies the gate"

assert "Go file + hw ref in title → PASS" \
    0 "bead reference" \
    env TENET_PR_TITLE="feat: add gate (hw-1uvd)" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

assert "Go file + hw ref in body → PASS" \
    0 "bead reference" \
    env TENET_PR_TITLE="feat: add gate" TENET_PR_BODY="Implements bead hw-abc123." TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

assert "Ref in parentheses → PASS" \
    0 "bead reference" \
    env TENET_PR_TITLE="fix(core): thing (hw-9z8)" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="cmd/hookwise/main.go"

assert "Mixed docs+code diff still gated, ref present → PASS" \
    0 "bead reference" \
    env TENET_PR_TITLE="feat: x" TENET_PR_BODY="Tracked as hw-k268" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES=$'README.md\ntui/app.py'

echo ""

# --- Group 3: Code diffs without ref or label fail ---
echo "Group 3: Gate failures"

assert "Go file, no ref, no label → FAIL" \
    1 "Gas City factory" \
    env TENET_PR_TITLE="feat: sneaky change" TENET_PR_BODY="no tracking here" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

assert "Failure message names the exception label" \
    1 "tenet-exception" \
    env TENET_PR_TITLE="feat: sneaky change" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

assert "Python file triggers gate → FAIL" \
    1 "Gas City factory" \
    env TENET_PR_TITLE="fix tui" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="tui/app.py"

assert "TypeScript file triggers gate → FAIL" \
    1 "Gas City factory" \
    env TENET_PR_TITLE="chore: danger tweak" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="dangerfile.ts"

assert "JavaScript file triggers gate → FAIL" \
    1 "Gas City factory" \
    env TENET_PR_TITLE="chore: js" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="hooks/helper.js"

echo ""

# --- Group 4: Ref pattern edge cases ---
echo "Group 4: Bead ref word-boundary matching"

assert "Glued prefix 'shw-123' is not a ref → FAIL" \
    1 "Gas City factory" \
    env TENET_PR_TITLE="feat: shw-123 thing" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

assert "Bare 'hw-' without id is not a ref → FAIL" \
    1 "Gas City factory" \
    env TENET_PR_TITLE="feat: hw- placeholder" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

assert "Uppercase 'HW-123' is not a ref → FAIL" \
    1 "Gas City factory" \
    env TENET_PR_TITLE="feat: HW-123" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

assert "Ref glued to trailing word char 'hw-123_x' → FAIL" \
    1 "Gas City factory" \
    env TENET_PR_TITLE="feat: hw-123_x" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

assert "Ref at end of line → PASS" \
    0 "bead reference" \
    env TENET_PR_TITLE="feat: implement hw-1uvd" TENET_PR_BODY="" TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

assert "Ref on its own line in multi-line body → PASS" \
    0 "bead reference" \
    env TENET_PR_TITLE="feat: x" TENET_PR_BODY=$'Summary of change.\n\nhw-42ab\n\nDetails.' TENET_PR_LABELS="" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

echo ""

# --- Group 5: Exception label ---
echo "Group 5: tenet-exception label"

assert "Label alone → PASS" \
    0 "tenet-exception" \
    env TENET_PR_TITLE="feat: emergency fix" TENET_PR_BODY="" TENET_PR_LABELS="tenet-exception" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

assert "Label among others (comma-separated) → PASS" \
    0 "tenet-exception" \
    env TENET_PR_TITLE="feat: emergency fix" TENET_PR_BODY="" TENET_PR_LABELS="bug, tenet-exception, urgent" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

assert "Label among others (newline-separated) → PASS" \
    0 "tenet-exception" \
    env TENET_PR_TITLE="feat: emergency fix" TENET_PR_BODY="" TENET_PR_LABELS=$'bug\ntenet-exception' \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

assert "Superstring label 'tenet-exception-extra' does not count → FAIL" \
    1 "Gas City factory" \
    env TENET_PR_TITLE="feat: x" TENET_PR_BODY="" TENET_PR_LABELS="tenet-exception-extra" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

assert "Substring label 'exception' does not count → FAIL" \
    1 "Gas City factory" \
    env TENET_PR_TITLE="feat: x" TENET_PR_BODY="" TENET_PR_LABELS="exception" \
        TENET_CHANGED_FILES="internal/core/dispatch.go"

echo ""

# --- Group 6: Failure message contract ---
echo "Group 6: Failure message stays within 2 lines"

total=$((total + 1))
fail_output=$(env TENET_PR_TITLE="feat: x" TENET_PR_BODY="" TENET_PR_LABELS="" \
    TENET_CHANGED_FILES="internal/core/dispatch.go" bash "$TARGET" 2>&1)
line_count=$(echo "$fail_output" | wc -l | tr -d ' ')
if [ "$line_count" -le 2 ]; then
    echo "  ✓ Failure message is $line_count line(s) (≤2)"
    passed=$((passed + 1))
else
    echo "  ✗ Failure message is $line_count lines (expected ≤2)"
    echo "    output: $fail_output"
    failed=$((failed + 1))
fi

echo ""

# --- Summary ---
echo "=== Results: $passed/$total passed, $failed failed ==="

if [ "$failed" -gt 0 ]; then
    exit 1
fi
exit 0
