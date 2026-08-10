#!/usr/bin/env bash
# vitest-run.sh — Run vitest with timeout and zombie cleanup.
#
# Vitest 4.x can hang on pool cleanup (vitest-dev/vitest#8766). Some test
# files also leave open handles (timers, observers, EventSource) that prevent
# the process from exiting. This wrapper:
# 1. Runs vitest with a hard timeout
# 2. Kills the vitest process tree on timeout (PID-tree walk, CI-safe)
# 3. Detects "tests done but vitest stuck on pool.close()" and kills workers
# 4. Cleans up orphaned worker processes scoped to THIS vitest run only
#
# The primary hang mitigation is in vitest-globalSetup.ts, which kills
# orphaned workers in teardown() to unblock pool.close(). This script is
# a secondary defense for cases where the in-process kill doesn't work.
#
# Usage: ./scripts/vitest-run.sh [args passed to vitest]

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

# Timeout: 10 minutes by default (override with VITEST_TIMEOUT_S env)
TIMEOUT_S="${VITEST_TIMEOUT_S:-1200}"

# After vitest's test output stops, if workers remain for this many seconds,
# kill them. This handles "tests done but pool.close() hangs" without waiting
# for the full VITEST_TIMEOUT_S.
WORKER_STUCK_THRESHOLD_S="${WORKER_STUCK_THRESHOLD_S:-45}"

# collect_descendants: recursively collect all descendant PIDs of a process.
# Prints one PID per line to stdout.
collect_descendants() {
  local root_pid=$1
  local queue=("$root_pid")
  local visited=""
  while [ ${#queue[@]} -gt 0 ]; do
    local parent=${queue[0]}
    queue=("${queue[@]:1}")
    # Validate PID format to prevent visited-set corruption
    [[ "$parent" =~ ^[0-9]+$ ]] || continue
    # Skip if already visited (prevents loops from PID reuse)
    case " $visited " in
      *" $parent "*) continue ;;
    esac
    visited="$visited $parent"
    local children
    if [ -d "/proc" ]; then
      children=$(pgrep -P "$parent" 2>/dev/null || true)
    else
      children=$(ps -o pid= -o ppid= | awk -v p="$parent" '$2 == p { print $1 }')
    fi
    if [ -n "$children" ]; then
      for child in $children; do
        echo "$child"
        queue+=("$child")
      done
    fi
  done
}

# is_fork_worker: check if a PID is a vitest fork worker process.
# Works on Linux (/proc/PID/cmdline) and macOS (ps -o args=).
is_fork_worker() {
  local pid=$1
  local cmdline
  if [ -d "/proc/$pid" ]; then
    cmdline=$(cat "/proc/$pid/cmdline" 2>/dev/null | tr '\0' ' ' || true)
  else
    # macOS fallback: ps -o args= shows full command line with arguments
    cmdline=$(ps -o args= -p "$pid" 2>/dev/null || true)
  fi
  [[ "$cmdline" == *"/vitest/dist/workers/forks"* ]]
}

# find_our_workers: find fork workers that belong to THIS vitest run.
# Uses process tree descent from $VITEST_PID instead of pgrep -f (which
# matches ALL vitest instances on the system, including worktrees).
find_our_workers() {
  if [ -z "${VITEST_PID:-}" ]; then return; fi
  local descendants
  descendants=$(collect_descendants "$VITEST_PID")
  if [ -z "$descendants" ]; then return; fi
  # Filter to only fork workers
  for pid in $descendants; do
    if is_fork_worker "$pid"; then
      echo "$pid"
    fi
  done
}

# kill_our_workers: kill fork workers belonging to this vitest run.
kill_our_workers() {
  local workers
  workers=$(find_our_workers)
  if [ -n "$workers" ]; then
    local count
    count=$(echo "$workers" | wc -l)
    echo "[vitest-run] Killing $count fork worker(s) belonging to this vitest run" >&2
    for pid in $workers; do
      kill -9 "$pid" 2>/dev/null || true
    done
  fi
}

# save_worker_pids: record current workers to a temp file for post-exit cleanup.
# After vitest exits, workers get reparented to PID 1 and we can't use
# PPID descent anymore. The marker file lets us track them.
# IMPORTANT: must be called while vitest is still alive, otherwise workers
# have already been reparented to PID 1 and find_our_workers won't find them.
# Uses atomic write to avoid losing the previous snapshot on failure.
save_worker_pids() {
  local new_pids
  new_pids=$(find_our_workers 2>/dev/null || true)
  # Only overwrite if find_our_workers succeeded (even if result is empty,
  # meaning all workers exited normally). If it failed, keep the old snapshot.
  printf '%s\n' "$new_pids" > "$WORKER_MARKER_FILE"
}

# cleanup_orphans: kill fork workers orphaned by THIS vitest run.
# NOTE: There is a small TOCTOU risk — a PID could be reused by an unrelated
# process between the is_fork_worker check and the kill. The cmdline check
# makes this extremely unlikely in practice.
cleanup_orphans() {
  if [ ! -f "$WORKER_MARKER_FILE" ]; then return; fi
  local to_kill=()
  while IFS= read -r pid; do
    [ -z "$pid" ] && continue
    # Check if still alive and still a vitest fork worker
    if kill -0 "$pid" 2>/dev/null && is_fork_worker "$pid"; then
      to_kill+=("$pid")
    fi
  done < "$WORKER_MARKER_FILE"
  if [ ${#to_kill[@]} -gt 0 ]; then
    echo "[vitest-run] Cleaning up ${#to_kill[@]} orphaned worker(s) from this run" >&2
    for pid in "${to_kill[@]}"; do
      kill -9 "$pid" 2>/dev/null || true
    done
  fi
  rm -f "$WORKER_MARKER_FILE"
}

# kill_tree: recursively kill a PID and all its descendants.
kill_tree() {
  local root_pid=$1
  local sig=$2

  # Collect all PIDs (root + descendants)
  local all_pids=("$root_pid")
  local descendants
  descendants=$(collect_descendants "$root_pid")
  if [ -n "$descendants" ]; then
    while IFS= read -r pid; do
      all_pids+=("$pid")
    done <<< "$descendants"
  fi

  # Send signal to all collected PIDs (children first, then root)
  local reversed=()
  for pid in "${all_pids[@]}"; do
    reversed=("$pid" "${reversed[@]}")
  done
  for pid in "${reversed[@]}"; do
    kill "$sig" "$pid" 2>/dev/null || true
  done
}

# Marker file to track workers for post-exit cleanup
WORKER_MARKER_FILE=$(mktemp "${TMPDIR:-/tmp}/vitest-workers.XXXXXX")

# Ensure marker file is cleaned up on exit
trap 'rm -f "$WORKER_MARKER_FILE"' EXIT

# Run vitest in background
npx vitest run "$@" &
VITEST_PID=$!

# Track when we last saw fork workers — used for stuck detection
WORKER_FIRST_SEEN_S=0

# Watchdog: wait for vitest, kill process tree on timeout
WAITED=0
while kill -0 "$VITEST_PID" 2>/dev/null; do
  sleep 1
  WAITED=$((WAITED + 1))

  if [ "$WAITED" -ge "$TIMEOUT_S" ]; then
    echo "[vitest-run] VITEST TIMED OUT after ${TIMEOUT_S}s — killing hung workers and process tree" >&2

    # Kill hung fork workers first — this unblocks pool.close() so
    # vitest can complete teardown and write coverage data
    kill_our_workers

    # Wait up to 10s for vitest to exit (write coverage, teardown)
    grace=0
    while kill -0 "$VITEST_PID" 2>/dev/null && [ $grace -lt 10 ]; do
      sleep 1
      grace=$((grace + 1))
    done

    # If still alive, SIGTERM then SIGKILL
    if kill -0 "$VITEST_PID" 2>/dev/null; then
      echo "[vitest-run] Vitest did not exit after worker kill, sending SIGTERM" >&2
      kill_tree "$VITEST_PID" "-TERM"
      sleep 5
    fi

    if kill -0 "$VITEST_PID" 2>/dev/null; then
      echo "[vitest-run] Vitest did not exit after SIGTERM, sending SIGKILL" >&2
      kill_tree "$VITEST_PID" "-9"
    fi

    # Snapshot and clean up orphans
    save_worker_pids
    cleanup_orphans
    exit 124
  fi

  # Periodically snapshot worker PIDs (every 5s) while vitest is alive.
  # This ensures we know worker PIDs even if vitest exits and they get
  # reparented to PID 1 before we can walk the process tree.
  if [ $((WAITED % 5)) -eq 0 ]; then
    save_worker_pids
  fi

  # Proactive stuck-worker detection: if fork workers exist for longer
  # than WORKER_STUCK_THRESHOLD_S while the vitest process is still running,
  # they likely have open handles and are blocking pool.close().
  # The globalSetup teardown() should have killed them already, but if it
  # didn't work (e.g., pgrep -f matched wrong PIDs), we do it here.
  # Don't start checking until at least 60s in (tests need time to run).
  if [ "$WAITED" -gt 60 ]; then
    local_workers=$(find_our_workers)
    if [ -n "$local_workers" ]; then
      if [ "$WORKER_FIRST_SEEN_S" -eq 0 ]; then
        WORKER_FIRST_SEEN_S=$WAITED
      fi
      worker_duration=$((WAITED - WORKER_FIRST_SEEN_S))
      if [ "$worker_duration" -ge "$WORKER_STUCK_THRESHOLD_S" ]; then
        wcount=$(echo "$local_workers" | wc -l)
        echo "[vitest-run] $wcount worker(s) stuck for ${worker_duration}s (threshold=${WORKER_STUCK_THRESHOLD_S}s) — killing to unblock pool.close()" >&2
        kill_our_workers
        # Re-snapshot after killing (workers may have changed)
        save_worker_pids
        # Reset timer so we don't keep trying every second
        WORKER_FIRST_SEEN_S=0
      fi
    else
      # No workers — reset timer
      WORKER_FIRST_SEEN_S=0
    fi
  fi
done

# Get vitest exit code
wait "$VITEST_PID"
EXIT_CODE=$?

# Clean up orphaned workers from this run.
# Worker PIDs were saved during the watchdog loop; after vitest exits,
# they may have been reparented to PID 1, so find_our_workers won't work,
# but cleanup_orphans uses the saved marker file.
cleanup_orphans

exit "$EXIT_CODE"
