#!/bin/bash
# pre-test-resource-check.sh — Hard-coded resource gate for test execution (retro-009)
#
# Two-layer defense:
#   Layer 1 (soft): Agent checks MCP mcp-monitor before deciding to test
#   Layer 2 (hard): THIS SCRIPT — Taskfile precondition that blocks test execution
#
# Checks:
#   1. No existing hookwise.test processes (prevents fan-out accumulation)
#   2. Sufficient free memory to run test suite safely
#
# Exit 0 = safe to proceed, Exit 1 = blocked with reason
#
# Testing:
#   Run: bash scripts/test-resource-check.sh
#   Override system calls: HOOKWISE_TEST_MODE=1 with injected values

set -uo pipefail

# --- Configurable thresholds ---
MIN_FREE_MB="${HOOKWISE_MIN_FREE_MB:-2048}"       # Minimum free RAM in MB (default 2 GB)
MAX_TEST_PROCS="${HOOKWISE_MAX_TEST_PROCS:-0}"     # Max existing hookwise.test processes (0 = none allowed)
WARN_FREE_MB="${HOOKWISE_WARN_FREE_MB:-4096}"      # Warn if free RAM below this (default 4 GB)

# --- Colors (disabled if not a terminal) ---
if [ -t 1 ]; then
    RED='\033[0;31m'
    YELLOW='\033[1;33m'
    GREEN='\033[0;32m'
    NC='\033[0m'
else
    RED='' YELLOW='' GREEN='' NC=''
fi

# --- Testable functions ---

# Count hookwise.test processes. Uses [h]ookwise.test trick to avoid matching grep itself.
count_test_procs() {
    if [ "${HOOKWISE_TEST_MODE:-}" = "1" ] && [ -n "${HOOKWISE_MOCK_PROC_COUNT:-}" ]; then
        echo "$HOOKWISE_MOCK_PROC_COUNT"
        return
    fi
    # [h] trick: ps grep for [h]ookwise.test won't match the grep process itself
    ps aux 2>/dev/null | grep '[h]ookwise\.test' | wc -l | tr -d ' '
}

# Get available memory in MB. Returns empty string if unavailable.
get_available_memory_mb() {
    if [ "${HOOKWISE_TEST_MODE:-}" = "1" ] && [ -n "${HOOKWISE_MOCK_AVAIL_MB:-}" ]; then
        echo "$HOOKWISE_MOCK_AVAIL_MB"
        return
    fi

    # macOS: use vm_stat
    if command -v vm_stat &>/dev/null; then
        local page_size
        page_size=$(sysctl -n hw.pagesize 2>/dev/null || echo 16384)

        local free_pages inactive_pages
        free_pages=$(vm_stat 2>/dev/null | awk '/Pages free/ {gsub(/\./,"",$3); print $3}')
        inactive_pages=$(vm_stat 2>/dev/null | awk '/Pages inactive/ {gsub(/\./,"",$3); print $3}')

        if [ -n "$free_pages" ] && [ -n "$inactive_pages" ]; then
            local available_bytes=$(( (free_pages + inactive_pages) * page_size ))
            echo $(( available_bytes / 1048576 ))  # Convert to MB
            return
        fi
    fi

    # Linux: use /proc/meminfo
    if [ -f /proc/meminfo ]; then
        local avail_kb
        avail_kb=$(awk '/MemAvailable/ {print $2}' /proc/meminfo 2>/dev/null)
        if [ -n "$avail_kb" ]; then
            echo $(( avail_kb / 1024 ))  # Convert to MB
            return
        fi
    fi

    # Unknown platform — return empty (skip memory check)
    echo ""
}

# --- Main ---

errors=0

# Check 1: Existing hookwise.test processes
test_proc_count=$(count_test_procs)
if [ "$test_proc_count" -gt "$MAX_TEST_PROCS" ]; then
    echo -e "${RED}BLOCKED (retro-009): $test_proc_count hookwise.test process(es) already running${NC}"
    echo "Each test binary is ~149 MB (Dolt embedded). With -race, ~300 MB."
    echo "Run: pkill -f hookwise.test"
    echo "See: .claude/specs/retro/retro-009-system-crash-memory-exhaustion.md"
    errors=$((errors + 1))
fi

# Check 2: Memory pressure
avail_mb=$(get_available_memory_mb)
if [ -n "$avail_mb" ]; then
    if [ "$avail_mb" -lt "$MIN_FREE_MB" ]; then
        echo -e "${RED}BLOCKED (retro-009): Only ~$((avail_mb / 1024)) GB ($avail_mb MB) free memory (minimum: $((MIN_FREE_MB / 1024)) GB)${NC}"
        echo "hookwise test suite needs ~600 MB (with -race -p 2)."
        echo "Close memory-heavy apps or wait for previous processes to complete."
        if [ "${HOOKWISE_TEST_MODE:-}" != "1" ]; then
            echo ""
            echo "Top memory consumers:"
            ps -eo pid,rss,comm 2>/dev/null | sort -k2 -rn | head -5 | awk '{printf "  PID %-8s %6.0f MB  %s\n", $1, $2/1024, $3}'
        fi
        errors=$((errors + 1))
    elif [ "$avail_mb" -lt "$WARN_FREE_MB" ]; then
        echo -e "${YELLOW}WARNING: ~$((avail_mb / 1024)) GB ($avail_mb MB) free (threshold: $((MIN_FREE_MB / 1024)) GB). Proceeding with caution.${NC}"
    fi
else
    echo -e "${YELLOW}WARNING: Could not determine available memory (unsupported platform). Skipping memory check.${NC}"
fi

# Final verdict
if [ "$errors" -gt 0 ]; then
    exit 1
fi

echo -e "${GREEN}Resource check passed: ${test_proc_count} test procs, ~${avail_mb:-?} MB free${NC}"
exit 0
