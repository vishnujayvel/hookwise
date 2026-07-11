#!/bin/bash
# tenet-gate.sh — CI tenet gate: code PRs need a bead ref or exception label
#
# hookwise code is authored by the Gas City factory (bead -> polecat -> gate -> PR).
# This gate FAILS a PR when ALL of:
#   (a) the diff touches code files (*.go, *.py, *.ts, *.js)
#   (b) neither PR title nor body contains a bead reference (hw-xxxx as a word)
#   (c) the PR lacks the 'tenet-exception' label
#
# Inputs (env — injected by the workflow, mockable in tests):
#   TENET_PR_TITLE       PR title
#   TENET_PR_BODY        PR body
#   TENET_PR_LABELS      Label names, comma- or newline-separated
#   TENET_CHANGED_FILES  Changed file paths, newline-separated
#
# Exit 0 = pass, Exit 1 = fail with reason
#
# Testing:
#   Run: bash scripts/test-tenet-gate.sh

set -uo pipefail

TITLE="${TENET_PR_TITLE:-}"
BODY="${TENET_PR_BODY:-}"
LABELS="${TENET_PR_LABELS:-}"
FILES="${TENET_CHANGED_FILES:-}"

# Bead ref as a word: 'hw-' must not be glued to a preceding word character,
# and the id must not be followed by one (portable stand-in for \b).
BEAD_REF_PATTERN='(^|[^[:alnum:]_])hw-[a-z0-9]+([^[:alnum:]_]|$)'

# (a) Gate only applies when the diff touches code files.
code_files=$(printf '%s\n' "$FILES" | grep -E '\.(go|py|ts|js)$' || true)
if [ -z "$code_files" ]; then
    echo "tenet-gate: PASS — no code files (*.go *.py *.ts *.js) in diff"
    exit 0
fi

# (b) A bead reference in title or body satisfies the gate.
if printf '%s\n%s\n' "$TITLE" "$BODY" | grep -qE "$BEAD_REF_PATTERN"; then
    echo "tenet-gate: PASS — bead reference (hw-xxxx) found in PR title/body"
    exit 0
fi

# (c) The 'tenet-exception' label is the sanctioned escape hatch.
if printf '%s' "$LABELS" | tr ',' '\n' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | grep -qxF 'tenet-exception'; then
    echo "tenet-gate: PASS — 'tenet-exception' label applied"
    exit 0
fi

echo "tenet-gate: FAIL — hookwise code is authored by the Gas City factory (bead -> polecat -> gate -> PR); reference a bead (hw-xxxx) in the PR title or body."
echo "For the sanctioned exception path, apply the 'tenet-exception' label to this PR."
exit 1
