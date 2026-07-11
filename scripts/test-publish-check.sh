#!/bin/bash
# test-publish-check.sh — Tests for publish-check.sh
#
# Each scenario builds a throwaway git repo with empty commits on a topic
# branch off main, then runs the gate against an explicit or default range.
# Mirrors the test-tenet-gate.sh harness.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TARGET="$SCRIPT_DIR/publish-check.sh"

passed=0
failed=0
total=0
REPO=""

cleanup() {
    [ -n "$REPO" ] && rm -rf "$REPO"
}
trap cleanup EXIT

# --- Test helpers ---

new_repo() {
    cleanup
    REPO=$(mktemp -d "${TMPDIR:-/tmp}/publish-check-test.XXXXXX")
    git -C "$REPO" -c init.defaultBranch=main init -q
    repo_commit "chore: base commit"
    git -C "$REPO" checkout -q -b topic
}

repo_commit() {
    git -C "$REPO" -c user.email=test@test -c user.name=test \
        commit -q --allow-empty -m "$1"
}

# assert <name> <expected_exit> <expected_pattern> [range...]
# Runs the gate inside $REPO; no range args → default-range resolution.
assert() {
    local name="$1"
    local expected_exit="$2"
    local expected_pattern="$3"
    shift 3

    total=$((total + 1))

    local output
    local exit_code
    output=$(cd "$REPO" && bash "$TARGET" "$@" 2>&1)
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
        echo "    output: $(echo "$output" | head -3)"
        failed=$((failed + 1))
    fi
}

echo "=== publish-check.sh tests ==="
echo ""

# --- Group 1: Passing fixtures ---
echo "Group 1: Valid subjects pass"

new_repo
repo_commit "feat: add the publish gate (hw-vwpw)"
repo_commit "fix(scripts): handle empty ranges"
assert "Valid subjects → PASS" \
    0 "PASS" main..HEAD

new_repo
# "feat: " (6) + 66 x's = exactly 72 chars — the boundary must pass.
repo_commit "feat: $(printf 'x%.0s' $(seq 1 66))"
assert "Exactly-72-char subject → PASS" \
    0 "PASS" main..HEAD

new_repo
assert "Empty range → PASS" \
    0 "no commits in range" main..HEAD

echo ""

# --- Group 2: Refusal fixtures ---
echo "Group 2: Offending subjects are refused"

new_repo
# "feat: " (6) + 67 x's = 73 chars — one over the limit.
repo_commit "feat: $(printf 'x%.0s' $(seq 1 67))"
assert "73-char subject → FAIL, reports length 73" \
    1 "73 chars" main..HEAD

new_repo
repo_commit "yolo: not a real commit type"
assert "Disallowed type → FAIL" \
    1 "bad type" main..HEAD

new_repo
repo_commit "no conventional prefix at all"
assert "Missing type prefix → FAIL" \
    1 "bad type" main..HEAD

new_repo
repo_commit "feat: good subject"
repo_commit "yolo: bad subject"
assert "Mixed good+bad commits → FAIL, counts one offender" \
    1 "1 of 2" main..HEAD

echo ""

# --- Group 3: Type enum comes from the commitlint config ---
echo "Group 3: Config parsing"

new_repo
cat > "$REPO/.commitlintrc.yml" <<'YML'
rules:
  type-enum:
    - 2
    - always
    - - feat
      - wibble
  header-max-length:
    - 2
    - always
    - 72
YML
git -C "$REPO" add .commitlintrc.yml
repo_commit "feat: track commitlint config"
repo_commit "wibble: custom type from yml config"
assert "Custom type in .commitlintrc.yml is honored → PASS" \
    0 "PASS" main..HEAD

new_repo
cat > "$REPO/commitlint.config.js" <<'JS'
module.exports = {
  rules: {
    'type-enum': [2, 'always', ['feat', 'zonk']],
  },
};
JS
git -C "$REPO" add commitlint.config.js
repo_commit "feat: track commitlint js config"
repo_commit "zonk: custom type from js config"
assert "Custom type in commitlint.config.js is honored → PASS" \
    0 "PASS" main..HEAD

new_repo
repo_commit "perf: perf is not in the fallback enum"
assert "No config → fallback enum rejects perf" \
    1 "bad type" main..HEAD

echo ""

# --- Group 4: Default range resolution ---
echo "Group 4: Default range"

new_repo
git -C "$REPO" update-ref refs/remotes/github/main main
repo_commit "feat: ahead of github main"
assert "No args → checks commits ahead of github/main" \
    0 "github/main..HEAD" # no range args

new_repo
git -C "$REPO" update-ref refs/remotes/github/main main
repo_commit "yolo: bad commit ahead of github main"
assert "No args → refuses bad commit ahead of github/main" \
    1 "bad type" # no range args

new_repo
git -C "$REPO" update-ref refs/remotes/origin/main main
repo_commit "feat: ahead of origin main"
assert "No args, no github/main → falls back to origin/main" \
    0 "origin/main..HEAD" # no range args

new_repo
assert "No github/origin base and no args → exit 2" \
    2 "cannot resolve" # no range args

new_repo
assert "Garbage range → exit 2" \
    2 "invalid range" "not-a-ref..HEAD"

echo ""

# --- Summary ---
echo "=== Results: $passed/$total passed, $failed failed ==="

if [ "$failed" -gt 0 ]; then
    exit 1
fi
exit 0
