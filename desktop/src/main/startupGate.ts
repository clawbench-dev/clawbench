/**
 * Holds the main window's navigation until the embedded server's startup has
 * settled.
 *
 * Why this exists: `createMainWindow` can run while `startEmbeddedServer()` is
 * still working. The window is created by a second launch of the shortcut
 * (the first copy gets `second-instance`), by a notification click, or — the
 * normal case now — by the startup path itself so the loading ring is on screen
 * immediately. Loading `serverUrl` at that moment reads the PREVIOUS run's
 * address: the server that answers it may be an old instance that is about to
 * exit, and if the new server comes up on another port the page is pinned to a
 * dead address. Its WebSocket then reconnects forever, and only logging out and
 * back in (which navigates to the address the shell just stored) recovers.
 *
 * So: the window shows the loading overlay over the local login page and waits.
 * `index.ts` settles the gate after `startEmbeddedServerIfPresent()` returns,
 * success or failure; `window.ts` then reads the final `serverUrl` and
 * navigates. A thin client settles it before creating the window, so its cold
 * start is unchanged.
 *
 * Kept as a separate module so the ordering can be unit-tested without
 * Electron (the same split as `splashPolicy.ts` / `localServerPolicy.ts`).
 */

let settled = false
let release: (() => void) | null = null
let pending: Promise<void> | null = null

/**
 * The promise waiters share. Created lazily: a settle with no waiters must not
 * leave an unresolved promise behind (the test runner reports it as a leak,
 * and so would anything that counts pending work).
 */
function pendingPromise(): Promise<void> {
  if (!pending) {
    pending = new Promise<void>((resolve) => {
      release = resolve
    })
  }
  return pending
}

/** Mark startup as settled. Idempotent. */
export function settleStartupGate(): void {
  if (settled) return
  settled = true
  release?.()
  release = null
  pending = null
}

/** Whether startup has settled (the window may navigate now). */
export function isStartupSettled(): boolean {
  return settled
}

/** Resolve once startup has settled; resolves immediately when it already has. */
export function whenStartupSettled(): Promise<void> {
  return settled ? Promise.resolve() : pendingPromise()
}

/** Test seam: put the module back into its pending state. */
export function _resetStartupGateForTesting(): void {
  settled = false
  release = null
  pending = null
}
