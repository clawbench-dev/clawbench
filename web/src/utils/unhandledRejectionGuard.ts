/**
 * Guard against re-entrant unhandled-rejection handling.
 *
 * The global test setup installs an `unhandledRejection` handler that
 * re-throws non-recursive reasons so vitest still surfaces them. Calling
 * `Promise.reject(reason)` inside that handler produces a *new* unhandled
 * rejection, which re-enters the same handler — an infinite loop that pegs a
 * worker at 100% CPU and hangs the whole run.
 *
 * This factory returns a handler that forwards each distinct reason exactly
 * once. Extracted from `test-setup.ts` so the re-entrancy behaviour is unit
 * testable.
 */

export function isRecursiveUpdateError(reason: unknown): boolean {
  if (reason instanceof Error) {
    return reason.message.includes('Maximum recursive updates')
  }
  if (typeof reason === 'string') {
    return reason.includes('Maximum recursive updates')
  }
  return false
}

/**
 * Builds an `unhandledRejection` handler that re-throws each reason at most
 * once. `onRethrow` is injectable for tests (defaults to `Promise.reject`).
 */
export function createUnhandledRejectionHandler(
  onRethrow: (reason: unknown) => void = (reason) => {
    Promise.reject(reason)
  },
): (reason: unknown) => void {
  const rethrown = new Set<unknown>()
  return (reason: unknown) => {
    // Vue's scheduler noise is intentionally swallowed.
    if (isRecursiveUpdateError(reason)) return
    // Already forwarded this exact reason — forwarding again would re-enter
    // this handler forever.
    if (rethrown.has(reason)) return
    rethrown.add(reason)
    onRethrow(reason)
  }
}
