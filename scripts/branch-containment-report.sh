#!/bin/bash
# branch-containment-report.sh — Measurement instrument ONLY.
#
# Reports whether each local branch matching polecat/* or gc-gastown* is
# content-contained in github/main. This script contains NO deletion code
# by design: it never deletes, moves, or rewrites any ref. The prune
# decision is a separate human act, made from this report's evidence.
#
# Evidence methods, tried in order per branch:
#   1. ancestry   — git merge-base --is-ancestor <branch> <base>
#   2. patch-id   — git cherry <base> <branch> (all commits patch-equivalent)
#   3. range-diff — git range-diff <base>...<branch> pairing summary
#
# Output: exactly one line per matching branch, one of:
#   CONTAINED <branch> via=<ancestry|patch-id|range-diff>
#   AHEAD <branch> n=<count>       (count = commits only on the branch)
#   NOT-PROVEN <branch>            (ambiguous or evidence tooling failed)
#
# Usage:
#   scripts/branch-containment-report.sh [--base <ref>] [--fetch]
#     --base <ref>   containment target (default: github/main)
#     --fetch        run 'git fetch github' first — the ONLY network
#                    operation this script may perform (off by default)

set -uo pipefail

BASE_REF="github/main"
DO_FETCH=0

while [ $# -gt 0 ]; do
    case "$1" in
        --base)
            [ $# -ge 2 ] || { echo "error: --base requires a ref" >&2; exit 2; }
            BASE_REF="$2"; shift 2 ;;
        --fetch)
            DO_FETCH=1; shift ;;
        *)
            echo "error: unknown argument: $1" >&2; exit 2 ;;
    esac
done

if [ "$DO_FETCH" -eq 1 ]; then
    git fetch github || { echo "error: git fetch github failed" >&2; exit 2; }
fi

if ! git rev-parse --verify --quiet "${BASE_REF}^{commit}" >/dev/null; then
    echo "error: base ref not found: $BASE_REF (try --fetch)" >&2
    exit 2
fi

# Classify one branch and print its single verdict line.
classify() {
    local branch="$1"

    # Method 1: ancestry — branch tip reachable from base.
    if git merge-base --is-ancestor "$branch" "$BASE_REF" 2>/dev/null; then
        echo "CONTAINED $branch via=ancestry"
        return
    fi

    # Method 2: patch-id — every branch commit has a patch-equivalent
    # commit in base ('git cherry' marks unmatched commits with '+').
    local cherry_out cherry_ok=1
    cherry_out=$(git cherry "$BASE_REF" "$branch" 2>/dev/null) || cherry_ok=0
    if [ "$cherry_ok" -eq 1 ] && ! printf '%s\n' "$cherry_out" | grep -q '^+'; then
        echo "CONTAINED $branch via=patch-id"
        return
    fi

    # Method 3: range-diff — fuzzy commit pairing since the merge base.
    # -s suppresses inner diffs so only pairing header lines remain.
    local rd_out
    if ! rd_out=$(git range-diff --no-color -s "${BASE_REF}...${branch}" 2>/dev/null); then
        echo "NOT-PROVEN $branch"
        return
    fi

    # Header lines look like:  "1:  abc1234 = 1:  abc1234 subject"
    # (left/right side may be "-:  -------" for unpaired commits).
    # The pairing marker (= ! < >) is always the third field.
    local markers n_new n_mod
    markers=$(printf '%s\n' "$rd_out" | awk '$3 ~ /^[=!<>]$/ { print $3 }')
    n_new=$(printf '%s\n' "$markers" | grep -c '>')
    n_mod=$(printf '%s\n' "$markers" | grep -c '!')

    if [ "$n_mod" -gt 0 ]; then
        # Some branch commit pairs with a base commit but the content
        # differs — containment can be neither proven nor ruled out.
        echo "NOT-PROVEN $branch"
    elif [ "$n_new" -gt 0 ]; then
        echo "AHEAD $branch n=$n_new"
    else
        # Every branch-side commit paired equal ('<' lines are commits
        # only in base, which is expected as base moves ahead).
        echo "CONTAINED $branch via=range-diff"
    fi
}

found_any=0
while IFS= read -r branch; do
    [ -n "$branch" ] || continue
    found_any=1
    classify "$branch"
done < <(git for-each-ref --format='%(refname:short)' \
    'refs/heads/polecat/*' 'refs/heads/gc-gastown*')

if [ "$found_any" -eq 0 ]; then
    echo "note: no local branches match polecat/* or gc-gastown*" >&2
fi

exit 0
