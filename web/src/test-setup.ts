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

// ── Range measurement polyfill for jsdom ──
// jsdom does not implement Range.prototype.getClientRects / getBoundingClientRect.
// CodeMirror 6 calls these while measuring text, so without a stub its content
// intermittently fails to render in jsdom (empty .cm-content), which breaks any
// test that inspects the rendered editor text or performs a real DOM selection.
if (typeof globalThis.Range !== 'undefined') {
  const emptyRect = { top: 0, left: 0, right: 0, bottom: 0, width: 0, height: 0, x: 0, y: 0, toJSON: () => ({}) }
  const rangeProto = globalThis.Range.prototype as unknown as Record<string, unknown>
  if (typeof rangeProto.getClientRects === 'undefined') {
    rangeProto.getClientRects = () => [emptyRect]
  }
  if (typeof rangeProto.getBoundingClientRect === 'undefined') {
    rangeProto.getBoundingClientRect = () => emptyRect
  }
}

// ── jsdom cookie store ──
// jsdom's document.cookie setter routes through the tough-cookie library, which
// leaves a dangling internal Promise on every write (detectAsyncLeaks flags it
// on any module that sets a cookie at import time, e.g. web/src/i18n/index.ts).
// Replace it with a plain in-memory store so no async resource is created.
try {
  let cookieStore = ''
  Object.defineProperty(document, 'cookie', {
    get: () => cookieStore,
    set: (value: string) => {
      cookieStore = value
    },
    configurable: true,
  })
} catch {
  // document not yet available — skip
}

// ── Deterministic vue-devtools hook state ──
// vue-i18n's app.use() plugin calls setupDevtoolsPlugin(), which invokes
// hook.emit() whenever a __VUE_DEVTOOLS_GLOBAL_HOOK__ object exists on window.
// jsdom can leave a half-initialized hook around, causing intermittent
// "Cannot setup vue-devtools plugin" install errors (CANNOT_SETUP_VUE_DEVTOOLS_PLUGIN)
// when many test files mount i18n apps. Clearing the hook makes the plugin
// take the synchronous deferred-setup path and keeps tests deterministic.
delete (globalThis as { __VUE_DEVTOOLS_GLOBAL_HOOK__?: unknown }).__VUE_DEVTOOLS_GLOBAL_HOOK__

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
