#!/usr/bin/env bash
# hooks/agent-tracker.sh
#
# Claude Code hook for SubagentStart / SubagentStop events.
# Reads JSON from stdin, maintains state at ~/.hookwise/cache/active-agents.json.
# Always exits 0 (fail-open).
set -euo pipefail

STATE_DIR="${HOOKWISE_STATE_DIR:-$HOME/.hookwise}"
CACHE_DIR="$STATE_DIR/cache"
STATE_FILE="$CACHE_DIR/active-agents.json"
STALE_SECONDS=600  # 10 minutes

mkdir -p "$CACHE_DIR"

# Read stdin JSON
INPUT=$(cat)

# Extract event type from CLAUDE_CODE_HOOK_EVENT_NAME env var
EVENT="${CLAUDE_CODE_HOOK_EVENT_NAME:-}"

# Parse fields from stdin JSON
agent_id=$(echo "$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('agent_id',''))" 2>/dev/null || echo "")
session_id=$(echo "$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('session_id',''))" 2>/dev/null || echo "")
worktree=$(echo "$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('cwd',''))" 2>/dev/null || echo "")
team_name=$(echo "$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('team_name',''))" 2>/dev/null || echo "")
strategy=$(echo "$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('strategy',''))" 2>/dev/null || echo "")

if [ -z "$agent_id" ]; then
  exit 0
fi

# Derive a short name from worktree basename or truncated agent_id
if [ -n "$worktree" ]; then
  name=$(basename "$worktree")
else
  name="${agent_id:0:12}"
fi

NOW=$(date +%s)

# Read existing state (or start fresh)
if [ -f "$STATE_FILE" ]; then
  CURRENT=$(cat "$STATE_FILE" 2>/dev/null || echo '{"agents":[],"team_name":"","strategy":""}')
else
  CURRENT='{"agents":[],"team_name":"","strategy":""}'
fi

# Use python3 for JSON manipulation (available on macOS)
UPDATED=$(python3 -c "
import json, sys

current = json.loads('''$CURRENT''')
agents = current.get('agents', [])
event = '$EVENT'
agent_id = '$agent_id'
name = '$name'
now = $NOW
stale = $STALE_SECONDS
team_name = '$team_name' or current.get('team_name', '')
strategy = '$strategy' or current.get('strategy', '')

# Clean stale entries (older than 10 minutes)
agents = [a for a in agents if (now - a.get('started_at', now)) < stale]

if event == 'SubagentStart':
    # Remove existing entry with same agent_id (if re-started)
    agents = [a for a in agents if a.get('agent_id') != agent_id]
    agents.append({
        'agent_id': agent_id,
        'name': name,
        'status': 'working',
        'started_at': now
    })
elif event == 'SubagentStop':
    for a in agents:
        if a.get('agent_id') == agent_id:
            a['status'] = 'done'
            a['stopped_at'] = now
            break

result = {
    'agents': agents,
    'team_name': team_name,
    'strategy': strategy,
    'updated_at': now
}
print(json.dumps(result, indent=2))
" 2>/dev/null) || exit 0

# Atomic write via temp file + rename
TMPFILE="$CACHE_DIR/.active-agents.tmp.$$"
echo "$UPDATED" > "$TMPFILE"
mv "$TMPFILE" "$STATE_FILE"

exit 0
