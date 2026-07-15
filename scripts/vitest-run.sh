#!/usr/bin/env bash
# vitest-run.sh — Run vitest with timeout, zombie cleanup, and force-exit.
#
# Vitest 4.x can hang on pool cleanup (vitest-dev/vitest#8766). Some test
# files also leave open handles (timers, observers, EventSource) that prevent
# the process from exiting. This wrapper:
# 1. Runs vitest with a hard timeout
# 2. Kills the vitest process tree on timeout (PID-tree walk, CI-safe)
# 3. Cleans up orphaned worker processes after exit
#
# Usage: ./scripts/vitest-run.sh [args passed to vitest]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

# Timeout: 10 minutes by default (override with VITEST_TIMEOUT_S env)
TIMEOUT_S="${VITEST_TIMEOUT_S:-600}"

# Cleanup function: kill any orphaned vitest fork workers
cleanup_vitest() {
  local pids
  pids=$(pgrep -f "vitest/dist/workers/forks" 2>/dev/null || true)
  if [ -n "$pids" ]; then
    local count
    count=$(echo "$pids" | wc -l)
    echo "[vitest-run] Cleaning up $count orphaned vitest worker(s)" >&2
    echo "$pids" | xargs kill -9 2>/dev/null || true
  fi
}

# kill_tree: recursively kill a PID and all its descendants.
# Works on both CI (no job control) and local (with job control).
# Uses /proc for efficiency on Linux, falls back to ps on macOS.
kill_tree() {
  local root_pid=$1
  local sig=$2

  # Collect child PIDs recursively (BFS)
  local all_pids=("$root_pid")
  local queue=("$root_pid")
  while [ ${#queue[@]} -gt 0 ]; do
    local parent=${queue[0]}
    queue=("${queue[@]:1}")
    local children
    if [ -d "/proc" ]; then
      # Linux: fast path via /proc
      children=$(pgrep -P "$parent" 2>/dev/null || true)
    else
      # macOS/BSD fallback
      children=$(ps -o pid= -o ppid= | awk -v p="$parent" '$2 == p { print $1 }')
    fi
    if [ -n "$children" ]; then
      for child in $children; do
        all_pids+=("$child")
        queue+=("$child")
      done
    fi
  done

  # Send signal to all collected PIDs (children first, then root)
  local reversed=()
  for pid in "${all_pids[@]}"; do
    reversed=("$pid" "${reversed[@]}")
  done
  for pid in "${reversed[@]}"; do
    kill "$sig" "$pid" 2>/dev/null || true
  done
}

# Run vitest in background
npx vitest run "$@" &
VITEST_PID=$!

# Watchdog: wait for vitest, kill process tree on timeout
WAITED=0
while kill -0 "$VITEST_PID" 2>/dev/null; do
  sleep 1
  WAITED=$((WAITED + 1))
  if [ "$WAITED" -ge "$TIMEOUT_S" ]; then
    echo "[vitest-run] VITEST TIMED OUT after ${TIMEOUT_S}s — killing process tree" >&2
    # Graceful SIGTERM first, then SIGKILL after 5s
    kill_tree "$VITEST_PID" "-TERM"
    sleep 5
    # If still alive, SIGKILL
    if kill -0 "$VITEST_PID" 2>/dev/null; then
      kill_tree "$VITEST_PID" "-9"
    fi
    sleep 1
    cleanup_vitest
    exit 124
  fi
done

# Get vitest exit code
wait "$VITEST_PID"
EXIT_CODE=$?

# Always clean up orphaned vitest workers.
cleanup_vitest

exit "$EXIT_CODE"
