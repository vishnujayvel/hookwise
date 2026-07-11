#!/bin/bash
# test-branch-containment-report.sh — Tests for branch-containment-report.sh
#
# Builds a throwaway toy git repo in a temp dir (never touches the real
# repo) with a synthetic github/main ref and branches engineered to hit
# every verdict path: CONTAINED (ancestry + patch-id), AHEAD, NOT-PROVEN.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TARGET="$SCRIPT_DIR/branch-containment-report.sh"

passed=0
failed=0
total=0

assert_line() {
    local name="$1"
    local expected="$2"

    total=$((total + 1))
    if printf '%s\n' "$REPORT" | grep -qxF "$expected"; then
        echo "  ✓ $name"
        passed=$((passed + 1))
    else
        echo "  ✗ $name — expected line '$expected' not found"
        failed=$((failed + 1))
    fi
}

commit_file() {
    local file="$1" content="$2" msg="$3"
    printf '%s\n' "$content" > "$file"
    git add "$file"
    git commit -q -m "$msg"
}

# --- Fixture: toy repo -------------------------------------------------

TMPDIR_FIXTURE=$(mktemp -d)
trap 'rm -rf "$TMPDIR_FIXTURE"' EXIT

cd "$TMPDIR_FIXTURE" || exit 1
git init -q repo
cd repo || exit 1
git config user.email "test@example.com"
git config user.name "Toy Fixture"
git config commit.gpgsign false
git checkout -q -b main

commit_file base.txt "base v1" "chore: base commit 1"
C1=$(git rev-parse HEAD)
commit_file base.txt "base v2" "chore: base commit 2"
C2=$(git rev-parse HEAD)

# Branch A — CONTAINED via=ancestry: tip is an ancestor of main.
git branch polecat/contained-ancestry "$C1"

# Branch B — CONTAINED via=patch-id: commit cherry-picked into main,
# so the SHA differs but the patch-id matches (new file → identical diff).
git checkout -q -b polecat/contained-patchid "$C1"
commit_file picked.txt "picked content" "feat: add picked file"
PICKED=$(git rev-parse HEAD)
git checkout -q main
git cherry-pick "$PICKED" >/dev/null

# Branch C — AHEAD n=2: two commits that never reached main, touching
# files unrelated to anything on main so range-diff cannot pair them.
git checkout -q -b polecat/ahead "$C2"
commit_file ahead1.txt "unmerged work one" "feat: unmerged work 1"
commit_file ahead2.txt "unmerged work two" "feat: unmerged work 2"

# Branch D — NOT-PROVEN: branch commit and a main commit share a subject
# and near-identical content (one line differs), so range-diff pairs
# them as modified ('!') — containment can't be proven either way.
git checkout -q -b gc-gastown-notproven "$C2"
{
    printf 'line %d\n' 1 2 3 4 5 6 7 8 9
    printf 'tail: branch flavour\n'
} > feature.txt
git add feature.txt
git commit -q -m "feat: add feature file"
git checkout -q main
{
    printf 'line %d\n' 1 2 3 4 5 6 7 8 9
    printf 'tail: main flavour\n'
} > feature.txt
git add feature.txt
git commit -q -m "feat: add feature file"

# A non-matching branch that must NOT appear in the report.
git branch unrelated-branch "$C2"

# Synthesize the github/main remote-tracking ref locally (no network).
git update-ref refs/remotes/github/main main
git checkout -q main

# --- Run the reporter --------------------------------------------------

echo "=== branch-containment-report.sh tests ==="
echo ""

REPORT=$(bash "$TARGET" 2>/dev/null)
STATUS=$?
echo "$REPORT" | sed 's/^/    | /'
echo ""

echo "Group 1: verdict paths"
assert_line "ancestor branch → CONTAINED via=ancestry" \
    "CONTAINED polecat/contained-ancestry via=ancestry"
assert_line "cherry-picked branch → CONTAINED via=patch-id" \
    "CONTAINED polecat/contained-patchid via=patch-id"
assert_line "unmerged branch → AHEAD n=2" \
    "AHEAD polecat/ahead n=2"
assert_line "modified-twin branch → NOT-PROVEN" \
    "NOT-PROVEN gc-gastown-notproven"

echo "Group 2: report hygiene"
total=$((total + 1))
if [ "$STATUS" -eq 0 ]; then
    echo "  ✓ exit code 0"
    passed=$((passed + 1))
else
    echo "  ✗ exit code 0 — got $STATUS"
    failed=$((failed + 1))
fi

total=$((total + 1))
line_count=$(printf '%s\n' "$REPORT" | grep -c .)
if [ "$line_count" -eq 4 ]; then
    echo "  ✓ exactly one line per matching branch (4 lines)"
    passed=$((passed + 1))
else
    echo "  ✗ expected 4 report lines, got $line_count"
    failed=$((failed + 1))
fi

total=$((total + 1))
if printf '%s\n' "$REPORT" | grep -q "unrelated-branch"; then
    echo "  ✗ non-matching branch leaked into report"
    failed=$((failed + 1))
else
    echo "  ✓ non-matching branch excluded"
    passed=$((passed + 1))
fi

echo "Group 3: safety rails"
total=$((total + 1))
# The measurement instrument must contain no ref-deleting git invocations.
if grep -nE 'branch[[:space:]]+-[dD]|push|update-ref[[:space:]]+-d|worktree[[:space:]]+(remove|prune)' "$TARGET"; then
    echo "  ✗ deletion/push code found in reporter (lines above)"
    failed=$((failed + 1))
else
    echo "  ✓ reporter contains no deletion or push commands"
    passed=$((passed + 1))
fi

total=$((total + 1))
BAD=$(bash "$TARGET" --base does/not-exist 2>&1)
if [ $? -eq 2 ] && printf '%s' "$BAD" | grep -q "base ref not found"; then
    echo "  ✓ missing base ref → exit 2 with clear error"
    passed=$((passed + 1))
else
    echo "  ✗ missing base ref handling wrong: $BAD"
    failed=$((failed + 1))
fi

echo ""
echo "=== $passed/$total passed, $failed failed ==="
[ "$failed" -eq 0 ]
