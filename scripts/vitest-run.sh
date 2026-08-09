#!/usr/bin/env bash
# vitest-run.sh — Run vitest, keep it fast, and guarantee no orphan workers.
#
# Two concerns:
#  1. SPEED — vitest 4.x fork workers can fail to exit cleanly and stall
#     pool.close() (vitest-dev/vitest#8766). We proactively kill fork workers
#     that are still alive 45s after tests should have finished, which unblocks
#     pool.close() so the suite finishes quickly.
#  2. ORPHANS — a fork worker that slips through cleanup gets reparented to
#     PID 1 and lingers as a CPU-burning process. We launch vitest with
#     `setsid` so it and every worker live in one dedicated process group, then
#     kill that group on every exit path. Killing the group deterministically
#     reaps every worker, even ones already reparented to PID 1.
#
# Usage: ./scripts/vitest-run.sh [args passed to vitest]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

TIMEOUT_S="${VITEST_TIMEOUT_S:-600}"
WORKER_STUCK_THRESHOLD_S="${WORKER_STUCK_THRESHOLD_S:-45}"

# --- Worker detection (process-group aware) -------------------------------
# is_fork_worker: is a PID a vitest fork worker process?
is_fork_worker() {
  local pid=$1
  local cmdline
  if [ -d "/proc/$pid" ]; then
    cmdline=$(cat "/proc/$pid/cmdline" 2>/dev/null | tr '\0' ' ' || true)
  else
    cmdline=$(ps -o args= -p "$pid" 2>/dev/null || true)
  fi
  [[ "$cmdline" == *"/vitest/dist/workers/forks"* ]]
}

# Launch vitest in its own session/process group (PGID = $VITEST_PID). All fork
# workers inherit this PGID, so a process-group kill reaps them all.
setsid npx vitest run "$@" &
VITEST_PID=$!

# kill_group: signal the whole vitest process group (TERM or KILL).
kill_group() {
  local sig=$1
  kill "-$sig" -- "-$VITEST_PID" 2>/dev/null || true
}

# cleanup_group: TERM, then KILL, the process group. This is the guaranteed
# orphan reaper — even workers reparented to PID 1 stay in our group.
cleanup_group() {
  kill_group "TERM"
  sleep 2
  kill_group "KILL"
}

# find_our_workers: fork workers that are descendants of $VITEST_PID.
find_our_workers() {
  pgrep -P "$VITEST_PID" 2>/dev/null | while read -r pid; do
    if is_fork_worker "$pid"; then
      echo "$pid"
    fi
  done
}

# kill_our_workers: kill stuck fork workers to unblock pool.close().
kill_our_workers() {
  local workers
  workers=$(find_our_workers)
  if [ -n "$workers" ]; then
    local count
    count=$(echo "$workers" | wc -l)
    echo "[vitest-run] Killing $count stuck fork worker(s) to unblock pool.close()" >&2
    for pid in $workers; do
      kill -9 "$pid" 2>/dev/null || true
    done
  fi
}

# On every exit path, reap the process group so no orphan survives.
on_exit() {
  cleanup_group
}
trap on_exit EXIT

# Watchdog: wait for vitest; kill stuck workers to keep the run fast, and kill
# the whole process group on timeout.
WAITED=0
WORKER_FIRST_SEEN_S=0
while kill -0 "$VITEST_PID" 2>/dev/null; do
  sleep 1
  WAITED=$((WAITED + 1))

  if [ "$WAITED" -ge "$TIMEOUT_S" ]; then
    echo "[vitest-run] TIMED OUT after ${TIMEOUT_S}s — killing process group (PGID $VITEST_PID)" >&2
    kill_group "TERM"
    sleep 5
    kill_group "KILL"
    exit 124
  fi

  # Proactively kill fork workers stuck past the threshold (tests should have
  # finished long ago). This unblocks pool.close() so the suite ends promptly.
  if [ "$WAITED" -gt 60 ]; then
    local_workers=$(find_our_workers)
    if [ -n "$local_workers" ]; then
      if [ "$WORKER_FIRST_SEEN_S" -eq 0 ]; then
        WORKER_FIRST_SEEN_S=$WAITED
      fi
      worker_duration=$((WAITED - WORKER_FIRST_SEEN_S))
      if [ "$worker_duration" -ge "$WORKER_STUCK_THRESHOLD_S" ]; then
        kill_our_workers
        WORKER_FIRST_SEEN_S=0
      fi
    else
      WORKER_FIRST_SEEN_S=0
    fi
  fi
done

# vitest exited. Propagate its exit code (trap reaps any leftover workers).
wait "$VITEST_PID"
exit "$?"
