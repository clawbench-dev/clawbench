// Vitest globalSetup — kill hung workers to unblock pool cleanup (vitest 4.x).
//
// PROBLEM: Vitest 4.x fork workers with open handles (Vite FILEHANDLEs,
// vue-i18n devtools promises, jsdom window listeners) cannot exit cleanly
// after their tests complete. PoolRunner.stop() waits for workers indefinitely,
// preventing pool.close() from returning and blocking the process from exiting.
//
// STRATEGY: Kill orphaned workers in teardown(), which runs after all tests
// complete but before pool.close(). Killing workers at this point lets
// pool.close() return, allowing vitest to exit normally.
//
// IMPORTANT: Do NOT start a timer in setup() to kill workers. On CI with
// multiple workers, tests can take 200+ seconds. A fixed-delay timer would
// kill workers while tests are still running, causing "Worker forks emitted
// error" and test failures. Instead, let tests complete naturally and only
// kill workers in teardown().
//
// Primary defense: scripts/vitest-run.sh wrapper with timeout + PID-tree kill.
// This globalSetup is a secondary (in-process) defense.

import { execSync } from 'node:child_process'

// Find fork workers that are children of THIS vitest process.
// Uses PPID matching (pgrep -P) instead of pgrep -f to avoid killing
// workers from other vitest instances (e.g., worktrees).
function killOurWorkers(label: string) {
  try {
    // Fork workers are direct children of the vitest main process.
    // process.pid is the vitest main process when running in teardown().
    const ourPid = process.pid
    const childPids = execSync(
      `pgrep -P ${ourPid} 2>/dev/null || true`,
      { encoding: 'utf-8', timeout: 3000 }
    ).trim().split('\n').filter(Boolean).map(Number)

    // Filter to only fork worker processes (not sh/npm children)
    const workerPids: number[] = []
    for (const pid of childPids) {
      try {
        // On Linux, /proc/PID/cmdline has full args separated by NUL.
        // On macOS, fall back to ps -o args= which shows full command line.
        const cmdline = execSync(
          `cat /proc/${pid}/cmdline 2>/dev/null || ps -o args= -p ${pid} 2>/dev/null || true`,
          { encoding: 'utf-8', timeout: 2000 }
        ).replace(/\0/g, ' ').trim()
        if (cmdline.includes('/vitest/dist/workers/forks')) {
          workerPids.push(pid)
        }
      } catch {}
    }

    if (workerPids.length > 0) {
      console.error(
        `[vitest-globalSetup] ${label}: killing ${workerPids.length} fork worker(s) ` +
        `(children of PID ${ourPid}, vitest pool cleanup hang — vitest-dev/vitest#8766)`
      )
      for (const pid of workerPids) {
        try { process.kill(pid, 'SIGKILL') } catch {}
      }
    }
  } catch {}
}

export function setup() {
  // No-op. Worker killing happens in teardown() after tests complete.
}

export function teardown() {
  // Kill orphaned workers immediately after teardown() is called.
  // At this point all tests have finished and vitest is about to call
  // pool.close(). If workers have open handles, pool.close() will hang.
  // Killing workers lets pool.close() return so vitest can exit.
  killOurWorkers('POST-TEST CLEANUP')

  // Secondary safety net: kill any workers that respawn or linger
  // after the first kill attempt.
  setTimeout(() => {
    killOurWorkers('POST-TEARDOWN CLEANUP')
  }, 3_000)
}
