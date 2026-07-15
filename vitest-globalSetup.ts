// Vitest globalSetup — force-exit safety net for pool cleanup hang.
//
// Vitest 4.x has a known bug where PoolRunner.stop() fails to force-kill
// fork workers that don't respond to the stop RPC within 60 seconds
// (STOP_TIMEOUT). The worker's IPC channel keeps the Node.js event loop
// alive indefinitely, producing zombie processes at 100% CPU.
// See: vitest-dev/vitest#8766, #9494, #8861, #9123.
//
// STRATEGY: Start the force-exit timer in setup() (not teardown()).
// If pool.close() hangs, teardown() is never called, so a timer set in
// teardown() would never fire. By starting it in setup(), the timer runs
// independently and fires even if pool.close() blocks the main process.
//
// The timer duration is set to VITEST_TIMEOUT_S (default 600s) minus a
// buffer, so it fires just before the external vitest-run.sh watchdog.
// This gives vitest maximum time to complete normally while still
// guaranteeing a clean exit if pool cleanup hangs.
//
// Primary defense: scripts/vitest-run.sh wrapper with timeout + PID-tree kill.
// This globalSetup is a secondary (in-process) defense.

import { execSync } from 'node:child_process'

// Default: fire 30s before the external watchdog (600s - 30s = 570s)
const FORCE_EXIT_MS = ((Number(process.env.VITEST_TIMEOUT_S) || 600) - 30) * 1000

let forceExitTimer: ReturnType<typeof setTimeout> | undefined

export function setup() {
  // Start the force-exit timer immediately. It will be cleared in teardown()
  // if vitest exits normally. If pool.close() hangs, this timer fires first.
  forceExitTimer = setTimeout(() => {
    console.error(
      `[vitest-globalSetup] FORCE EXIT: process still alive after ${FORCE_EXIT_MS / 1000}s ` +
      '(vitest pool cleanup hang — vitest-dev/vitest#8766)'
    )
    // Kill orphaned fork workers
    try {
      const pids = execSync(
        `pgrep -f "vitest/dist/workers/forks" 2>/dev/null || true`,
        { encoding: 'utf-8', timeout: 3000 }
      ).trim().split('\n').filter(Boolean).map(Number)
      for (const pid of pids) {
        try { process.kill(pid, 'SIGKILL') } catch {}
      }
    } catch {}
    // When all tests pass, vitest sets process.exitCode = 0 before running
    // globalSetup teardown. But the force-exit may cause Node to report 1.
    // Preserve the original exit code (0 if tests passed) so downstream
    // scripts don't misinterpret pool cleanup as a test failure.
    process.exit(process.exitCode ?? 0)
  }, FORCE_EXIT_MS)
  // Prevent the timer from keeping the process alive if it would otherwise
  // exit normally — but if pool.close() hangs, other open handles keep
  // the event loop alive, so this unref() doesn't matter in the hang case.
  forceExitTimer.unref()
}

export function teardown() {
  // Normal exit: clear the force-exit timer (not needed if pool cleanup works)
  if (forceExitTimer) {
    clearTimeout(forceExitTimer)
    forceExitTimer = undefined
  }

  // Set a short timer as secondary safety net: if something after teardown
  // hangs (unlikely with forks pool), force-exit after 2s.
  setTimeout(() => {
    console.error(
      '[vitest-globalSetup] FORCE EXIT: process still alive 2s after teardown completed ' +
      '(vitest pool cleanup bug — vitest-dev/vitest#8766)'
    )
    try {
      const pids = execSync(
        `pgrep -f "vitest/dist/workers/forks" 2>/dev/null || true`,
        { encoding: 'utf-8', timeout: 3000 }
      ).trim().split('\n').filter(Boolean).map(Number)
      for (const pid of pids) {
        try { process.kill(pid, 'SIGKILL') } catch {}
      }
    } catch {}
    process.exit(process.exitCode ?? 0)
  }, 2_000)
}
