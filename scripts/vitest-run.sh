#!/usr/bin/env bash
# vitest-run.sh — Run vitest with timeout, zombie cleanup, and force-exit.
#
# Vitest 4.x can hang on pool cleanup (vitest-dev/vitest#8766). Some test
# files also leave open handles (timers, observers, EventSource) that prevent
# the process from exiting. This wrapper:
# 1. Runs vitest with a hard timeout
# 2. Kills the entire vitest process tree on timeout
# 3. Cleans up orphaned worker processes after exit
#
# Usage: ./scripts/vitest-run.sh [args passed to vitest]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

# Timeout: 5 minutes by default (override with VITEST_TIMEOUT_S env)
TIMEOUT_S="${VITEST_TIMEOUT_S:-300}"

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

# Run vitest in a new process group so we can kill the entire tree.
set -m  # Enable job control for process group
npx vitest run "$@" &
VITEST_PID=$!

# Watchdog: wait for vitest, kill process group on timeout
WAITED=0
while kill -0 "$VITEST_PID" 2>/dev/null; do
  sleep 1
  WAITED=$((WAITED + 1))
  if [ "$WAITED" -ge "$TIMEOUT_S" ]; then
    echo "[vitest-run] VITEST TIMED OUT after ${TIMEOUT_S}s — killing process tree" >&2
    # Kill the entire process group
    kill -TERM -- -"$VITEST_PID" 2>/dev/null || true
    sleep 2
    kill -9 -- -"$VITEST_PID" 2>/dev/null || true
    sleep 1
    cleanup_vitest
    exit 124
  fi
done
set +m

# Get vitest exit code
wait "$VITEST_PID"
EXIT_CODE=$?

# Always clean up orphaned vitest workers.
cleanup_vitest

exit "$EXIT_CODE"
