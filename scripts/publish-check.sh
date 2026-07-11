#!/bin/bash
# publish-check.sh — local commit-subject gate for the publish flow
#
# Verifies every commit subject in a ref range:
#   (a) is <= 72 characters (commitlint header-max-length)
#   (b) starts with an allowed conventional-commit type
#
# Usage:
#   scripts/publish-check.sh [<range>]
#
# Default range: commits ahead of github/main on the current branch
# (falls back to origin/main if the github remote is absent).
#
# The allowed type enum is parsed from the repo's commitlint config
# (commitlint.config.js or .commitlintrc.yml, whichever exists). If no
# config is found or parsing yields nothing, falls back to:
#   feat fix docs test refactor chore
#
# Exit 0 = all subjects pass
# Exit 1 = offenders found (each printed with its length / bad type)
# Exit 2 = usage/environment error (unresolvable range, not a repo)
#
# Testing:
#   Run: bash scripts/test-publish-check.sh

set -uo pipefail

MAX_LEN=72
FALLBACK_TYPES="feat fix docs test refactor chore"

repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || {
    echo "publish-check: not inside a git repository" >&2
    exit 2
}

# --- Resolve the ref range ---

if [ $# -ge 1 ]; then
    RANGE="$1"
else
    RANGE=""
    for base in github/main origin/main; do
        if git rev-parse --verify --quiet "$base" >/dev/null; then
            RANGE="$base..HEAD"
            break
        fi
    done
    if [ -z "$RANGE" ]; then
        echo "publish-check: cannot resolve a base ref (github/main or origin/main); pass a range explicitly" >&2
        exit 2
    fi
fi

# --- Parse the allowed type enum from the commitlint config ---

# Both parsers scope to the type-enum block and keep bare lowercase
# tokens, dropping commitlint's severity/condition entries (2, always).
parse_types_js() {
    # commitlint.config.js: quoted strings inside the type-enum array.
    sed -n '/type-enum/,/\]/p' "$1" 2>/dev/null |
        grep -oE "['\"][a-z]+['\"]" |
        tr -d "'\"" |
        grep -vxE 'always|never' || true
}

parse_types_yml() {
    # .commitlintrc.yml: list items under the type-enum key, until the
    # next sibling rule key ends the block.
    awk '
        /^[[:space:]]*type-enum:/ { inblock = 1; next }
        inblock && /^[[:space:]]*-/ {
            line = $0
            gsub(/^[[:space:]]+/, "", line)
            gsub(/^(-[[:space:]]+)+/, "", line)
            gsub(/["'\'']/, "", line)
            if (line ~ /^[a-z]+$/ && line != "always" && line != "never") print line
            next
        }
        inblock && /^[[:space:]]*[A-Za-z-]+:/ { inblock = 0 }
    ' "$1" 2>/dev/null || true
}

types=""
if [ -f "$repo_root/commitlint.config.js" ]; then
    types=$(parse_types_js "$repo_root/commitlint.config.js")
elif [ -f "$repo_root/.commitlintrc.yml" ]; then
    types=$(parse_types_yml "$repo_root/.commitlintrc.yml")
fi
if [ -z "$types" ]; then
    types=$(tr ' ' '\n' <<<"$FALLBACK_TYPES")
fi

type_alt=$(printf '%s\n' "$types" | paste -sd'|' -)
subject_re="^(${type_alt})(\([^)]*\))?!?: .+"

# --- Check every commit subject in the range ---

subjects=$(git log --no-merges --format='%s' "$RANGE" -- 2>&1) || {
    echo "publish-check: invalid range '$RANGE'" >&2
    echo "$subjects" >&2
    exit 2
}

if [ -z "$subjects" ]; then
    echo "publish-check: PASS — no commits in range $RANGE"
    exit 0
fi

offenders=0
checked=0
while IFS= read -r subject; do
    checked=$((checked + 1))
    len=${#subject}
    if [ "$len" -gt "$MAX_LEN" ]; then
        echo "  ✗ subject is $len chars (max $MAX_LEN): $subject"
        offenders=$((offenders + 1))
        continue
    fi
    if ! printf '%s' "$subject" | grep -qE "$subject_re"; then
        echo "  ✗ bad type (allowed: $type_alt): $subject"
        offenders=$((offenders + 1))
    fi
done <<EOF
$subjects
EOF

if [ "$offenders" -gt 0 ]; then
    echo "publish-check: FAIL — $offenders of $checked commit subject(s) in $RANGE violate the gate"
    exit 1
fi

echo "publish-check: PASS — $checked commit subject(s) in $RANGE ok"
exit 0
