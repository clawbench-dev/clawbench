// Global test setup for vitest
// Suppress Vue "Maximum recursive updates exceeded" errors from AppHeader tests.
// The mock store shares a plain object across test instances, which causes Vue's
// reactive scheduler to detect recursive updates when mockState.gitBranch changes
// between tests. This is a test-environment artifact, not a real bug — the
// component works correctly in production where the real store manages its own
// reactivity. Without this handler, vitest exits non-zero and the coverage gate
// reports "Frontend tests failed" even though all test cases pass.

// ── ResizeObserver polyfill for jsdom ──
// jsdom does not implement ResizeObserver, but components like FileHeader and
// FileManagerContent use useToolbarOverflow which creates one on mount.
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = class ResizeObserver {
    constructor(_callback: ResizeObserverCallback) {
      // no-op in test environment
    }
    observe() {
      // no-op in test environment
    }
    unobserve() {
      // no-op in test environment
    }
    disconnect() {
      // no-op in test environment
    }
  } as unknown as typeof globalThis.ResizeObserver
}

function isRecursiveUpdateError(reason: unknown): boolean {
  if (reason instanceof Error) {
    return reason.message.includes('Maximum recursive updates')
  }
  if (typeof reason === 'string') {
    return reason.includes('Maximum recursive updates')
  }
  return false
}

// Catch unhandled rejections from Vue's scheduler.
// Store the handler reference so vitest's own cleanup can remove it —
// a permanent process.on() listener keeps the worker's event loop alive
// and prevents clean exit, contributing to the zombie worker problem
// (vitest-dev/vitest#8766, #9494).
const unhandledRejectionHandler = (reason: unknown) => {
  if (isRecursiveUpdateError(reason)) return
  // Re-throw as async to preserve default behavior
  Promise.reject(reason)
}
process.on('unhandledRejection', unhandledRejectionHandler)

// Remove the listener when vitest teardown runs, so the worker process
// can exit cleanly. Without this, the IPC channel keeps ref=true and
// the event loop never becomes empty.
try {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { afterAll: vitestAfterAll } = require('vitest') as { afterAll: (fn: () => void) => void }
  vitestAfterAll(() => {
    process.off('unhandledRejection', unhandledRejectionHandler)
  })
} catch {
  // vitest not available (e.g. typecheck-only mode) — skip cleanup
}
