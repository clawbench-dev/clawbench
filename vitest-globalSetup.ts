// Vitest globalSetup — force-exit safety net for pool cleanup hang.
//
// Vitest 4.x has a known bug where PoolRunner.stop() fails to force-kill
// fork workers that don't respond to the stop RPC within 60 seconds
// (STOP_TIMEOUT). The worker's IPC channel keeps the Node.js event loop
// alive indefinitely, producing zombie processes at 60-66% CPU.
// See: vitest-dev/vitest#8766, #9494, #8861, #9123.
//
// Primary defense: scripts/vitest-run.sh wrapper with timeout + zombie cleanup.
// This globalSetup is a secondary defense: if teardown() runs, it sets a
// force-exit deadline to ensure the process doesn't hang indefinitely.

import { execSync } from 'node:child_process'

const FORCE_EXIT_MS = 15_000

export function setup() {
  // No-op
}

export function teardown() {
  // Set a ref'd timer as hard deadline after teardown completes.
  // If pool.close() subsequently hangs (workers don't respond to stop RPC),
  // this timer fires and force-exits so zombie workers don't accumulate.
  const timer = setTimeout(() => {
    console.error(
      `[vitest-globalSetup] FORCE EXIT: process still alive ${FORCE_EXIT_MS}ms after teardown ` +
      '(vitest pool cleanup bug — vitest-dev/vitest#8766)'
    )
    // Kill orphaned workers
    try {
      const pids = execSync(
        `pgrep -f "vitest/dist/workers/forks" 2>/dev/null || true`,
        { encoding: 'utf-8', timeout: 3000 }
      ).trim().split('\n').filter(Boolean).map(Number)
      for (const pid of pids) {
        try { process.kill(pid, 'SIGKILL') } catch {}
      }
    } catch {}
    process.exit(process.exitCode ?? 1)
  }, FORCE_EXIT_MS)
}
