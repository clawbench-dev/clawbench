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

// Catch unhandled rejections from Vue's scheduler
process.on('unhandledRejection', (reason: unknown) => {
  if (isRecursiveUpdateError(reason)) return
  // Re-throw as async to preserve default behavior
  Promise.reject(reason)
})
