#!/usr/bin/env bash
# vitest-run.sh — Run vitest with timeout, zombie cleanup, and watchdog.
#
# Vitest 4.x has a known bug where fork workers can become zombies
# when pool cleanup hangs (vitest-dev/vitest#8766). This wrapper:
# 1. Runs vitest in the background
# 2. Starts a watchdog that monitors for zombie workers after vitest exits
# 3. Kills orphaned worker processes and reports the exit code
#
# Usage: ./scripts/vitest-run.sh [args passed to vitest]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

# Timeout: 3 minutes by default (override with VITEST_TIMEOUT_S env)
TIMEOUT_S="${VITEST_TIMEOUT_S:-300}"

# Cleanup function: kill any orphaned vitest workers
cleanup_workers() {
  local pids
  pids=$(pgrep -f "vitest/dist/workers/forks" 2>/dev/null || true)
  if [ -n "$pids" ]; then
    local count
    count=$(echo "$pids" | wc -l)
    echo "[vitest-run] Cleaning up $count orphaned vitest worker(s)" >&2
    echo "$pids" | xargs kill -9 2>/dev/null || true
  fi
}

# Run vitest with a hard timeout. On timeout (exit 124), the timeout
# command sends SIGTERM then SIGKILL after 5s to the vitest process.
set +e
timeout --signal=TERM --kill-after=5s "$TIMEOUT_S" npx vitest run "$@"
EXIT_CODE=$?
set -e

# Timeout exit code is 124
if [ "$EXIT_CODE" -eq 124 ]; then
  echo "[vitest-run] VITEST TIMED OUT after ${TIMEOUT_S}s — killing orphaned workers" >&2
fi

# Always clean up orphaned vitest workers. They outlive the main process
# because PoolRunner.stop() fails to force-kill fork workers that don't
# respond to the stop RPC within 60s (vitest 4.x pool cleanup bug).
cleanup_workers

exit "$EXIT_CODE"
