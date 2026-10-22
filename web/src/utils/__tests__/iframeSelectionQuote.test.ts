import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  attachIframeSelectionQuoteBridge,
  iframeSelectionAnchor,
  installIframeSearchShortcut,
  mapIframeRectToHost,
} from '../iframeSelectionQuote.ts'
import type { IframeBridgeWindow } from '../iframeSelectionQuote.ts'
import type { QuoteData } from '@/composables/useChatContext.ts'

// ── Fake iframe window ──
// attachIframeSelectionQuoteBridge takes a structurally-typed window-like object, so
// tests drive it with a lightweight fake (no real iframe / srcdoc navigation,
// which jsdom does not run). Listeners registered on the fake doc are stored
// by type so tests can fire them.
type Listener = EventListenerOrEventListenerObject
class FakeDoc {
  listeners = new Map<string, Listener[]>()
  addEventListener(type: string, listener: Listener) {
    const arr = this.listeners.get(type) ?? []
    arr.push(listener)
    this.listeners.set(type, arr)
  }
  removeEventListener(type: string, listener: Listener) {
    const arr = this.listeners.get(type) ?? []
    const idx = arr.indexOf(listener)
    if (idx >= 0) arr.splice(idx, 1)
  }
  fire(type: string) {
    for (const l of this.listeners.get(type) ?? []) {
      ;(l as EventListener).call(this, new Event(type))
    }
  }
  listenerCount(type: string) {
    return (this.listeners.get(type) ?? []).length
  }
}

class FakeSelection {
  private text: string
  collapsed: boolean
  constructor(text: string, collapsed = false) {
    this.text = text
    this.collapsed = collapsed
  }
  toString() {
    return this.text
  }
  // The bridge asks for the selection's line boxes to place the bar. This double
  // has no layout, so it reports none — the anchor is then null (the bar's
  // pinned fallback). Anchor mapping itself is covered by the dedicated tests
  // at the bottom of this file.
  get rangeCount() {
    return this.collapsed ? 0 : 1
  }
  getRangeAt() {
    return { getClientRects: () => [] } as unknown as Range
  }
}

function createFakeWin(opts: { selection?: FakeSelection | null; doc?: FakeDoc } = {}): {
  win: IframeBridgeWindow
  doc: FakeDoc
  getSelectionMock: ReturnType<typeof vi.fn>
} {
  const doc = opts.doc ?? new FakeDoc()
  const selection = opts.selection === undefined ? new FakeSelection('selected text') : opts.selection
  const getSelectionMock = vi.fn(() => selection)
  const win: IframeBridgeWindow = {
    getSelection: getSelectionMock,
    document: doc,
  }
  return { win, doc, getSelectionMock }
}

function makeBridge(overrides: Partial<Parameters<typeof attachIframeSelectionQuoteBridge>[0]> = {}) {
  const show = vi.fn()
  const hide = vi.fn()
  const filePath = vi.fn(() => '/specs/api.yaml')
  const { win, doc, getSelectionMock } = createFakeWin()
  const dispose = attachIframeSelectionQuoteBridge({
    win,
    filePath,
    show,
    hide,
    ...overrides,
  })
  return { win, doc, getSelectionMock, show, hide, filePath, dispose }
}

describe('attachIframeSelectionQuoteBridge', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.runOnlyPendingTimers()
    vi.useRealTimers()
  })

  it('registers the selection + pointer listeners on the iframe document', () => {
    const { doc, dispose } = makeBridge()
    expect(doc.listenerCount('selectionchange')).toBe(1)
    expect(doc.listenerCount('pointerdown')).toBe(1)
    expect(doc.listenerCount('pointerup')).toBe(1)
    expect(doc.listenerCount('pointercancel')).toBe(1)
    expect(doc.listenerCount('touchend')).toBe(1)
    expect(doc.listenerCount('touchcancel')).toBe(1)
    dispose()
  })

  it('shows the bar with QuoteData for a non-empty selection', () => {
    const { doc, show, filePath, dispose } = makeBridge()
    doc.fire('selectionchange')
    vi.advanceTimersByTime(150)

    expect(show).toHaveBeenCalledTimes(1)
    expect(show).toHaveBeenCalledWith({
      text: 'selected text',
      filePath: '/specs/api.yaml',
      language: '',
      startLine: 0,
      endLine: 0,
    }, null)
    expect(filePath).toHaveBeenCalled()
    dispose()
  })

  it('hides the bar for an empty / collapsed selection', () => {
    const { doc, getSelectionMock, show, hide, dispose } = makeBridge()
    const emptySel = new FakeSelection('', true)
    getSelectionMock.mockReturnValue(emptySel)

    doc.fire('selectionchange')
    vi.advanceTimersByTime(150)

    expect(hide).toHaveBeenCalledTimes(1)
    expect(show).not.toHaveBeenCalled()
    dispose()
  })

  it('trims the selected text (whitespace-only counts as empty)', () => {
    const { doc, getSelectionMock, show, hide, dispose } = makeBridge()
    getSelectionMock.mockReturnValue(new FakeSelection('   \n  '))

    doc.fire('selectionchange')
    vi.advanceTimersByTime(150)

    expect(hide).toHaveBeenCalledTimes(1)
    expect(show).not.toHaveBeenCalled()
    dispose()
  })

  it('trims non-empty selections before surfacing them', () => {
    const { doc, getSelectionMock, show, dispose } = makeBridge()
    getSelectionMock.mockReturnValue(new FakeSelection('  GET /pets  '))

    doc.fire('selectionchange')
    vi.advanceTimersByTime(150)

    expect(show).toHaveBeenCalledWith(expect.objectContaining({ text: 'GET /pets' }), null)
    dispose()
  })

  it('suppresses evaluation while the pointer is still pressed (mid-drag)', () => {
    const { doc, show, hide, dispose } = makeBridge()
    doc.fire('pointerdown')
    doc.fire('selectionchange')
    vi.advanceTimersByTime(150)

    expect(show).not.toHaveBeenCalled()
    expect(hide).not.toHaveBeenCalled()
    dispose()
  })

  it('shows the bar after pointerup re-evaluates a settled selection', () => {
    const { doc, show, dispose } = makeBridge()
    doc.fire('pointerdown')
    doc.fire('selectionchange')
    vi.advanceTimersByTime(150)
    expect(show).not.toHaveBeenCalled()

    doc.fire('pointerup')
    vi.advanceTimersByTime(120)

    expect(show).toHaveBeenCalledTimes(1)
    dispose()
  })

  it('releases the guard on touchend when pointerup is swallowed (mobile)', () => {
    const { doc, show, dispose } = makeBridge()
    doc.fire('pointerdown')
    doc.fire('touchend')
    vi.advanceTimersByTime(120)

    expect(show).toHaveBeenCalledTimes(1)
    dispose()
  })

  it('self-heals the guard when no release event fires', () => {
    const { doc, show, dispose } = makeBridge()
    doc.fire('pointerdown')
    // A selection settles while the guard is stuck (no pointerup / touchend).
    doc.fire('selectionchange')
    vi.advanceTimersByTime(150)
    expect(show).not.toHaveBeenCalled()

    // The stuck-guard safety timer drops the held count and re-evaluates.
    vi.advanceTimersByTime(700)

    expect(show).toHaveBeenCalledTimes(1)
    dispose()
  })

  it('resolves the file path lazily on each show', () => {
    const { doc, show, filePath, dispose } = makeBridge()

    doc.fire('selectionchange')
    vi.advanceTimersByTime(150)
    doc.fire('selectionchange')
    vi.advanceTimersByTime(150)

    expect(show).toHaveBeenCalledTimes(2)
    expect(filePath).toHaveBeenCalledTimes(2)
    dispose()
  })

  it('dispose removes listeners, clears timers, and has no further effect', () => {
    const { doc, show, dispose } = makeBridge()
    dispose()

    expect(doc.listenerCount('selectionchange')).toBe(0)
    expect(doc.listenerCount('pointerdown')).toBe(0)
    expect(doc.listenerCount('pointerup')).toBe(0)
    expect(doc.listenerCount('pointercancel')).toBe(0)
    expect(doc.listenerCount('touchend')).toBe(0)
    expect(doc.listenerCount('touchcancel')).toBe(0)

    // A late selectionchange must not surface the bar after disposal.
    doc.fire('selectionchange')
    vi.advanceTimersByTime(150)
    expect(show).not.toHaveBeenCalled()
  })

  it('does not surface a stale show after hide (collapsed selection clears the bar)', () => {
    const { doc, getSelectionMock, show, hide, dispose } = makeBridge()
    // First: a real selection shows the bar.
    doc.fire('selectionchange')
    vi.advanceTimersByTime(150)
    expect(show).toHaveBeenCalledTimes(1)

    // Then the selection collapses — the bar must hide.
    getSelectionMock.mockReturnValue(new FakeSelection('', true))
    doc.fire('selectionchange')
    vi.advanceTimersByTime(150)
    expect(hide).toHaveBeenCalledTimes(1)
    dispose()
  })

  it('QuoteData payload sent to the bar matches the QuoteData contract', () => {
    const captured: QuoteData[] = []
    const { doc, dispose } = makeBridge({
      show: (d: QuoteData) => { captured.push(d) },
    })
    doc.fire('selectionchange')
    vi.advanceTimersByTime(150)

    expect(captured).toHaveLength(1)
    expect(captured[0]).toEqual({
      text: 'selected text',
      filePath: '/specs/api.yaml',
      language: '',
      startLine: 0,
      endLine: 0,
    })
    dispose()
  })
})

/**
 * Ctrl+F inside a preview iframe.
 *
 * A keydown in the frame's document never reaches the host, so App.vue's global
 * handler is blind to it: the user clicks into the previewed page, presses
 * Ctrl+F, and gets the browser's own find bar over the app. The forwarder must
 * claim the chord and ask the host to open its inline find bar.
 */
describe('installIframeSearchShortcut', () => {
  function makeWin() {
    const listeners = new Map<string, EventListener[]>()
    const win = {
      addEventListener: (type: string, fn: EventListener) => {
        listeners.set(type, [...(listeners.get(type) ?? []), fn])
      },
      removeEventListener: (type: string, fn: EventListener) => {
        listeners.set(type, (listeners.get(type) ?? []).filter((l) => l !== fn))
      },
    }
    function press(init: KeyboardEventInit) {
      const e = new KeyboardEvent('keydown', { cancelable: true, ...init })
      for (const l of listeners.get('keydown') ?? []) l(e)
      return e
    }
    return { win: win as unknown as Window, press, listenerCount: () => (listeners.get('keydown') ?? []).length }
  }

  it('claims Ctrl+F / Cmd+F and triggers the host callback', () => {
    const { win, press } = makeWin()
    const onTrigger = vi.fn()
    installIframeSearchShortcut(win, onTrigger)

    const e = press({ key: 'f', ctrlKey: true })
    expect(onTrigger).toHaveBeenCalledTimes(1)
    // Without preventDefault the browser's own find bar opens on top of ours.
    expect(e.defaultPrevented).toBe(true)

    press({ key: 'f', metaKey: true })
    expect(onTrigger).toHaveBeenCalledTimes(2)
  })

  it('leaves Shift/Alt variants and other keys to the browser', () => {
    const { win, press } = makeWin()
    const onTrigger = vi.fn()
    installIframeSearchShortcut(win, onTrigger)

    // Ctrl+Shift+F is "search in files" — App.vue's own guard excludes it too.
    const shift = press({ key: 'F', ctrlKey: true, shiftKey: true })
    expect(onTrigger).not.toHaveBeenCalled()
    expect(shift.defaultPrevented).toBe(false)

    press({ key: 'f', ctrlKey: true, altKey: true })
    press({ key: 'g', ctrlKey: true })
    press({ key: 'f' })
    expect(onTrigger).not.toHaveBeenCalled()
  })

  it('stops forwarding after dispose', () => {
    const { win, press, listenerCount } = makeWin()
    const onTrigger = vi.fn()
    const dispose = installIframeSearchShortcut(win, onTrigger)
    dispose()
    expect(listenerCount()).toBe(0)

    press({ key: 'f', ctrlKey: true })
    expect(onTrigger).not.toHaveBeenCalled()
  })
})

/**
 * Anchor mapping: the selection rect must reach the bar in HOST viewport
 * coordinates.
 *
 * A frame's Range reports rects relative to the FRAME's viewport, while the
 * quote bar is `position: fixed` in the HOST's. Passing the frame rect straight
 * through put the bar at the top of the page instead of beside the selected
 * text — the reported defect.
 */
describe('mapIframeRectToHost', () => {
  const frame = { left: 300, top: 120, width: 800, height: 600 }

  it('offsets the frame-relative rect by the frame position', () => {
    const out = mapIframeRectToHost(
      { left: 40, top: 25, width: 100, height: 18 },
      frame,
      { width: 800, height: 600 },
    )
    expect(out).toEqual({ left: 340, top: 145, width: 100, height: 18 })
  })

  it('scales by the rendered/laid-out ratio (CSS zoom on <html>)', () => {
    // The app zooms <html>, so the iframe ELEMENT is rendered at 2x while the
    // document inside keeps laying out in unscaled CSS px. Without the ratio the
    // bar drifts by the zoom factor.
    const out = mapIframeRectToHost(
      { left: 40, top: 25, width: 100, height: 18 },
      { left: 0, top: 0, width: 1600, height: 1200 },
      { width: 800, height: 600 },
    )
    expect(out).toEqual({ left: 80, top: 50, width: 200, height: 36 })
  })

  it('does not divide by zero on an unmeasured viewport', () => {
    const out = mapIframeRectToHost(
      { left: 10, top: 10, width: 10, height: 10 },
      frame,
      { width: 0, height: 0 },
    )
    expect(out).toEqual({ left: 310, top: 130, width: 10, height: 10 })
  })
})

describe('iframeSelectionAnchor', () => {
  function selectionWithRects(rects: Array<{ top: number; left: number; width: number; height: number }>): Selection {
    return {
      rangeCount: 1,
      getRangeAt: () => ({ getClientRects: () => rects }) as unknown as Range,
    } as unknown as Selection
  }

  function fakeWin(overrides: Partial<Parameters<typeof iframeSelectionAnchor>[0]> = {}) {
    return {
      getSelection: () => null,
      document: {} as Document,
      frameElement: { getBoundingClientRect: () => ({ left: 300, top: 120, width: 800, height: 600 }) } as unknown as Element,
      innerWidth: 800,
      innerHeight: 600,
      ...overrides,
    }
  }

  it('maps the LAST line box into host coordinates', () => {
    // The last rect is the bottom line of a multi-line selection — the point the
    // pointer just left, which is where "just below the text" reads correctly.
    const rect = iframeSelectionAnchor(
      fakeWin(),
      selectionWithRects([
        { top: 10, left: 20, width: 200, height: 18 },
        { top: 40, left: 20, width: 120, height: 18 },
      ]),
    )
    expect(rect).not.toBeNull()
    expect(rect!.top).toBe(160)
    expect(rect!.left).toBe(320)
  })

  it('returns null when the frame element is unavailable (no layout to map)', () => {
    expect(iframeSelectionAnchor(fakeWin({ frameElement: null }), selectionWithRects([
      { top: 10, left: 20, width: 200, height: 18 },
    ]))).toBeNull()
  })

  it('returns null for a selection with no laid-out boxes', () => {
    expect(iframeSelectionAnchor(fakeWin(), selectionWithRects([]))).toBeNull()
  })

  it('the bridge hands the anchor to show() (null when unmeasurable)', () => {
    // This describe block runs on real timers (the bridge's own suite uses fake
    // ones), so wait the debounce out instead of advancing a clock.
    const shown: Array<DOMRect | null> = []
    const { doc, dispose } = makeBridge({
      show: (_d: QuoteData, anchor: DOMRect | null) => { shown.push(anchor) },
    })
    doc.fire('selectionchange')
    return new Promise<void>((resolve) => {
      setTimeout(() => {
        // The fake window in makeBridge has no frameElement, so the anchor is
        // null — what matters is that the argument is threaded through at all.
        expect(shown).toHaveLength(1)
        expect(shown[0]).toBeNull()
        dispose()
        resolve()
      }, 200)
    })
  })
})
