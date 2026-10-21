import type { QuoteData } from '@/composables/useChatContext.ts'

/**
 * Selection → quote bridge for a sandboxed preview iframe.
 *
 * Two rendered previews load their content into a same-origin sandboxed iframe
 * (OpenAPI/Swagger docs, and an HTML file preview). Both have the same problem:
 *
 *   - a text selection inside the iframe NEVER fires the host document's
 *     `selectionchange`, so the global quote-bar handler (useQuoteQuestion)
 *     cannot see it;
 *   - pointer events inside the iframe do not bubble to the host document,
 *     so the host's pointer-drag guard (pointerCount) never observes them.
 *
 * This factory re-creates the selection → showBar/hideBar pipeline INSIDE the
 * iframe's own document/window — the same escape hatch CodeMirrorViewer uses
 * for its internal selection — so selecting previewed text surfaces the shared
 * QuoteQuestionBar (which lives in the host document and is rendered above the
 * iframe).
 *
 * Pure logic, no Vue/iframe coupling: callers pass a structurally-typed
 * window-like object, which also lets unit tests drive it with a fake.
 */

export interface IframeBridgeWindow {
  getSelection(): Selection | null
  document: {
    addEventListener(type: string, listener: EventListenerOrEventListenerObject, opts?: boolean | AddEventListenerOptions): void
    removeEventListener(type: string, listener: EventListenerOrEventListenerObject, opts?: boolean | EventListenerOptions): void
  }
}

export interface IframeSelectionQuoteOptions {
  /** The iframe's own window (used for getSelection + document listeners). */
  win: IframeBridgeWindow
  /** Lazily resolve the current file path (the file may change under the iframe). */
  filePath: () => string
  /** Surface the quote bar with a QuoteData payload. */
  show: (data: QuoteData) => void
  /** Hide the quote bar (empty selection / collapsed). */
  hide: () => void
  /** Debounce for `selectionchange` (mirrors the host's 150ms). */
  debounceMs?: number
  /** Delay after pointer release before re-evaluating a settled selection. */
  releaseDelayMs?: number
  /** Self-heal timeout for the pointer guard when release events are swallowed. */
  pointerStuckMs?: number
}

/**
 * Wait for an iframe's browsing context, then return its window.
 *
 * Browsers expose `contentWindow` as soon as "load" fires, but jsdom can
 * dispatch the load event before it finishes attaching the browsing context —
 * and may even replace the iframe element mid-navigation, so a captured element
 * can go stale. Re-query the LIVE element each turn; the bounded microtask loop
 * is deterministic and leak-free in tests, and real browsers succeed on the
 * first check.
 *
 * Returns null when no browsing context appeared (cross-origin sandbox, an
 * iframe that never loads, or a detached element).
 */
export async function resolveIframeWindow(
  liveIframe: () => HTMLIFrameElement | null,
  maxTurns = 8,
): Promise<Window | null> {
  for (let i = 0; i <= maxTurns; i++) {
    const win = liveIframe()?.contentWindow
    if (win) return win
    if (i < maxTurns) await microtaskSleep(1)
  }
  return null
}

function microtaskSleep(n: number): Promise<void> {
  let p = Promise.resolve()
  for (let i = 0; i < n; i++) p = p.then(() => {})
  return p
}

/**
 * Forward Ctrl/Cmd+F pressed INSIDE an iframe to the host.
 *
 * A keydown in the frame's document never reaches the host's listeners, so
 * App.vue's global Ctrl+F handler is blind to it — the user clicks into the
 * previewed page, presses Ctrl+F, and either nothing happens or the browser's
 * own find bar opens over the app. This attaches a listener in the FRAME that
 * claims the chord and hands it to `onTrigger` (which opens the inline find
 * bar that searches this very document).
 *
 * Only the plain chord is claimed: Shift/Alt variants belong to the browser
 * (Ctrl+Shift+F is "search in files"), matching App.vue's own guard.
 *
 * Returns a dispose function.
 */
export function installIframeSearchShortcut(
  win: Window,
  onTrigger: () => void,
): () => void {
  function onKeyDown(e: KeyboardEvent) {
    if (!(e.ctrlKey || e.metaKey)) return
    if (e.shiftKey || e.altKey) return
    if (e.key.toLowerCase() !== 'f') return
    // Claim it: without this the browser's find bar opens as well.
    e.preventDefault()
    onTrigger()
  }
  win.addEventListener('keydown', onKeyDown)
  return () => win.removeEventListener('keydown', onKeyDown)
}

/**
 * Attach the quote bridge to an iframe window. Returns a dispose function that
 * removes all listeners and clears pending timers.
 */
export function attachIframeSelectionQuoteBridge(opts: IframeSelectionQuoteOptions): () => void {
  const {
    win,
    filePath,
    show,
    hide,
    debounceMs = 150,
    releaseDelayMs = 120,
    pointerStuckMs = 700,
  } = opts

  const doc = win.document

  // Active pointer (mouse/touch) count inside the iframe. Browsers fire
  // selectionchange while the user is still dragging; while any pointer is
  // held the bar is held back and pointerup re-runs the check — mirroring the
  // host useQuoteQuestion guard (which cannot see iframe events).
  let pointerCount = 0
  let pointerStuckTimer: ReturnType<typeof setTimeout> | null = null
  let debounceTimer: ReturnType<typeof setTimeout> | null = null
  let disposed = false

  function scheduleEvaluate(delayMs: number) {
    if (disposed) return
    if (debounceTimer) clearTimeout(debounceTimer)
    debounceTimer = setTimeout(evaluateSelection, delayMs)
  }

  function clearDebounce() {
    if (debounceTimer) {
      clearTimeout(debounceTimer)
      debounceTimer = null
    }
  }

  function evaluateSelection() {
    debounceTimer = null
    if (disposed) return

    const sel = win.getSelection()
    const text = sel && !sel.isCollapsed ? sel.toString().trim() : ''
    if (!text) {
      hide()
      return
    }
    // The pointer is still pressed — the selection is mid-drag and not final.
    if (pointerCount > 0) return

    show({
      text,
      filePath: filePath(),
      language: '',
      startLine: 0,
      endLine: 0,
    })
  }

  function onSelectionChange() {
    if (disposed) return
    scheduleEvaluate(debounceMs)
  }

  function onPointerDown() {
    pointerCount++
    // Mobile native selection UI can swallow the matching pointerup/touchend,
    // leaving the guard held forever. Self-heal after a short window so a
    // settling selection can still surface the bar.
    if (pointerStuckTimer) clearTimeout(pointerStuckTimer)
    pointerStuckTimer = setTimeout(() => {
      pointerStuckTimer = null
      if (pointerCount > 0) {
        pointerCount = 0
        scheduleEvaluate(0)
      }
    }, pointerStuckMs)
  }

  function releasePointer() {
    if (pointerStuckTimer) {
      clearTimeout(pointerStuckTimer)
      pointerStuckTimer = null
    }
    if (disposed) return
    if (pointerCount > 0) pointerCount--
    if (pointerCount === 0) {
      // Deferred: on touch the final selection often settles only after the
      // pointer is released, so an immediate evaluate may see an empty range.
      scheduleEvaluate(releaseDelayMs)
    }
  }

  function onPointerUp() {
    releasePointer()
  }

  doc.addEventListener('selectionchange', onSelectionChange)
  doc.addEventListener('pointerdown', onPointerDown)
  doc.addEventListener('pointerup', onPointerUp)
  doc.addEventListener('pointercancel', onPointerUp)
  doc.addEventListener('touchend', onPointerUp)
  doc.addEventListener('touchcancel', onPointerUp)

  return function dispose() {
    if (disposed) return
    disposed = true
    doc.removeEventListener('selectionchange', onSelectionChange)
    doc.removeEventListener('pointerdown', onPointerDown)
    doc.removeEventListener('pointerup', onPointerUp)
    doc.removeEventListener('pointercancel', onPointerUp)
    doc.removeEventListener('touchend', onPointerUp)
    doc.removeEventListener('touchcancel', onPointerUp)
    clearDebounce()
    if (pointerStuckTimer) {
      clearTimeout(pointerStuckTimer)
      pointerStuckTimer = null
    }
    pointerCount = 0
  }
}
