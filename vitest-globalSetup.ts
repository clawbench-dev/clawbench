// Vitest globalSetup — kill hung workers to unblock test completion (vitest 4.x).
//
// PROBLEM: Vitest 4.x fork workers with open handles (Vite FILEHANDLEs,
// vue-i18n devtools promises, jsdom window listeners) cannot exit cleanly
// after their tests complete. When a worker hangs, it blocks the test run
// from completing (ctx.start() never returns), which prevents:
//   - coverage data from being written
//   - globalSetup teardown from running
//   - the process from exiting
//
// STRATEGY: Start a timer in setup() that kills orphaned workers after a
// delay. This unblocks the test run so it can complete, write coverage,
// and exit normally. The timer delay must be long enough for all tests to
// finish, but short enough to fire before the external watchdog timeout.
//
// TIMING: Tests typically finish in ~120-160s on CI. The pool cleanup hang
// starts immediately after. We kill workers at 200s, giving ~40-80s of
// margin after test completion. CI timeout is 600s, so there's plenty of
// room for coverage generation and file writes (typically ~30s).
//
// Primary defense: scripts/vitest-run.sh wrapper with timeout + PID-tree kill.
// This globalSetup is a secondary (in-process) defense.

import { execSync } from 'node:child_process'

function killOrphanedWorkers(label: string) {
  try {
    const pids = execSync(
      `pgrep -f "vitest/dist/workers/forks" 2>/dev/null || true`,
      { encoding: 'utf-8', timeout: 3000 }
    ).trim().split('\n').filter(Boolean).map(Number)
    if (pids.length > 0) {
      console.error(
        `[vitest-globalSetup] ${label}: killing ${pids.length} orphaned vitest worker(s) ` +
        '(vitest pool cleanup hang — vitest-dev/vitest#8766)'
      )
      for (const pid of pids) {
        try { process.kill(pid, 'SIGKILL') } catch {}
      }
    }
  } catch {}
}

export function setup() {
  // Kill workers after 200s. This allows tests to finish (~120-160s) and
  // then kills any workers that are hanging due to open handles.
  // After workers are killed, the test run can complete, generate coverage,
  // and exit normally.
  const KILL_DELAY_MS = 200_000
  setTimeout(() => {
    killOrphanedWorkers('WORKER CLEANUP')
  }, KILL_DELAY_MS).unref()
}

export function teardown() {
  // Kill any remaining workers after teardown. This handles the edge case
  // where workers respawn or where the test run completed without hanging
  // but workers still linger during the close phase.
  killOrphanedWorkers('POST-TEARDOWN CLEANUP')
  setTimeout(() => {
    killOrphanedWorkers('POST-TEARDOWN DELAYED CLEANUP')
  }, 3_000)
}
