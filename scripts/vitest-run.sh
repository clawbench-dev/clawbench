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

# How long workers must make NO CPU progress before we call them stuck. This
# handles "tests done but pool.close() hangs" without waiting for the full
# VITEST_TIMEOUT_S.
#
# It is deliberately measured as *CPU-idle* time, not wall-clock age. An earlier
# version killed any worker that had merely existed for this long, which on a
# long suite kills workers that are still actively running tests: the full
# suite takes ~7 min, so the 45s timer fired repeatedly mid-run, discarding the
# coverage of every file those workers had not yet reported (measured: 11537 of
# 11953 tests reported, and FileManagerContent.vue coverage fell from 85.3% to
# 36.7%, failing the diff gate). A worker blocked in pool.close() burns no CPU,
# so sampling CPU time distinguishes the two cases.
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

# worker_cpu_total: total CPU time (user+system, in clock ticks) consumed by the
# given worker PIDs. Used to tell a worker that is still running tests (CPU time
# climbing) from one blocked in pool.close() (CPU time frozen). Prints 0 when
# nothing can be sampled, which reads as "no progress" — callers must therefore
# only act on a frozen total, never on a zero one.
worker_cpu_total() {
  local pids=$1
  local total=0
  local pid utime stime
  for pid in $pids; do
    if [ -r "/proc/$pid/stat" ]; then
      # Fields 14/15 (utime/stime) come after the comm field, which is wrapped
      # in parens and may itself contain spaces and ')'. Greedily strip through
      # the LAST ')' so the split is anchored on the real field boundary; the
      # remaining fields then start at state, making utime/stime $12/$13.
      read -r utime stime < <(
        sed 's/.*) //' "/proc/$pid/stat" 2>/dev/null |
          awk '{print $12, $13}' 2>/dev/null
      ) || continue
      [[ "$utime" =~ ^[0-9]+$ ]] || continue
      [[ "$stime" =~ ^[0-9]+$ ]] || continue
      total=$((total + utime + stime))
    else
      # macOS: ps reports cumulative CPU time as [[dd-]hh:]mm:ss.
      local t
      t=$(ps -o time= -p "$pid" 2>/dev/null | tr -d ' ' || true)
      [ -z "$t" ] && continue
      local secs
      secs=$(awk -F'[:.-]' '{n=NF; s=0; m=1; for(i=n;i>=1;i--){s+=$i*m; m*=60} print s}' <<< "$t" 2>/dev/null || echo 0)
      [[ "$secs" =~ ^[0-9]+$ ]] || secs=0
      total=$((total + secs))
    fi
  done
  echo "$total"
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

# Stuck-worker detection state: the last observed total worker CPU time, and
# how many seconds have passed with that total unchanged. CPU time only moves
# while a worker is actually executing; a worker parked in pool.close() leaves
# it frozen. See WORKER_STUCK_THRESHOLD_S for why wall-clock age is wrong here.
WORKER_LAST_CPU=""
WORKER_CPU_IDLE_S=0

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

  # Proactive stuck-worker detection: a worker whose CPU time has not advanced
  # for WORKER_STUCK_THRESHOLD_S is blocked (pool.close() with open handles),
  # not working. Killing it unblocks the pool. The globalSetup teardown() should
  # have handled this already, but if it didn't (e.g., pgrep -f matched wrong
  # PIDs), we do it here.
  #
  # Progress is measured by CPU time, NOT wall-clock age: the suite runs for
  # minutes, so "worker has existed for 45s" is normal and killing on it
  # discards the coverage of every file that worker still had to report.
  # Don't start checking until at least 60s in (tests need time to run).
  if [ "$WAITED" -gt 60 ]; then
    local_workers=$(find_our_workers)
    if [ -n "$local_workers" ]; then
      current_cpu=$(worker_cpu_total "$local_workers")
      if [ "$current_cpu" = "$WORKER_LAST_CPU" ]; then
        WORKER_CPU_IDLE_S=$((WORKER_CPU_IDLE_S + 1))
      else
        WORKER_LAST_CPU="$current_cpu"
        WORKER_CPU_IDLE_S=0
      fi
      if [ "$WORKER_CPU_IDLE_S" -ge "$WORKER_STUCK_THRESHOLD_S" ]; then
        wcount=$(echo "$local_workers" | wc -l)
        echo "[vitest-run] $wcount worker(s) made no CPU progress for ${WORKER_CPU_IDLE_S}s (threshold=${WORKER_STUCK_THRESHOLD_S}s) — killing to unblock pool.close()" >&2
        kill_our_workers
        # Re-snapshot after killing (workers may have changed)
        save_worker_pids
        # Reset so we don't keep trying every second
        WORKER_LAST_CPU=""
        WORKER_CPU_IDLE_S=0
      fi
    else
      # No workers — reset
      WORKER_LAST_CPU=""
      WORKER_CPU_IDLE_S=0
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
