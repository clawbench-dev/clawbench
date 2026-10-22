import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { attachIframeSelectionQuoteBridge, installIframeSearchShortcut } from '../iframeSelectionQuote.ts'
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
  fire(type: string, event?: Event) {
    for (const l of this.listeners.get(type) ?? []) {
      ;(l as EventListener).call(this, event ?? new Event(type))
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

/** A pointer event carrying only the two fields the bridge reads. */
function pointerEvent(pointerType: string, buttons: number): PointerEvent {
  return { pointerType, buttons } as PointerEvent
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
    // Drag liveness (issue #572) and auto-scroll are observed too.
    expect(doc.listenerCount('pointermove')).toBe(1)
    expect(doc.listenerCount('scroll')).toBe(1)
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
    })
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

    expect(show).toHaveBeenCalledWith(expect.objectContaining({ text: 'GET /pets' }))
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

  /**
   * Issue #572: the self-heal timer used to fire unconditionally, so a sweep
   * lasting longer than 700ms lost its guard mid-drag and the bar surfaced over
   * text that was still being selected. The mirror of the host fix.
   */
  it('keeps the guard up through a drag longer than the self-heal window', () => {
    const { doc, show, hide, dispose } = makeBridge()
    doc.fire('pointerdown')
    // ONE selectionchange, as a real drag produces at its start. Firing it every
    // tick would keep resetting the 150ms debounce, so the evaluation would
    // never run mid-drag and the defect could not show.
    doc.fire('selectionchange')

    for (let elapsed = 0; elapsed < 2000; elapsed += 100) {
      doc.fire('pointermove', pointerEvent('mouse', 1))
      vi.advanceTimersByTime(100)
      // The bar must not surface, and nothing may hide one either: a hide here
      // would mean the timer surfaced a bar that the drag then retracted — the
      // flicker. Asserting only `show` would miss exactly that.
      expect(show).not.toHaveBeenCalled()
      expect(hide).not.toHaveBeenCalled()
    }

    doc.fire('pointerup')
    vi.advanceTimersByTime(120)
    expect(show).toHaveBeenCalledTimes(1)
    dispose()
  })

  it('treats a scroll during a drag as pointer activity (auto-scroll)', () => {
    const { doc, show, dispose } = makeBridge()
    doc.fire('pointerdown')

    // Content moves under a stationary pointer: no pointermove at all.
    for (let elapsed = 0; elapsed < 2000; elapsed += 100) {
      doc.fire('scroll')
      vi.advanceTimersByTime(100)
      expect(show).not.toHaveBeenCalled()
    }

    doc.fire('pointerup')
    vi.advanceTimersByTime(120)
    expect(show).toHaveBeenCalledTimes(1)
    dispose()
  })

  it('releases the guard on a button-less mouse move when pointerup was swallowed', () => {
    const { doc, show, dispose } = makeBridge()
    doc.fire('pointerdown')
    // No pointerup reaches the bridge; the next mouse move proves the release.
    doc.fire('pointermove', pointerEvent('mouse', 0))
    vi.advanceTimersByTime(120)

    expect(show).toHaveBeenCalledTimes(1)
    dispose()
  })

  it('ignores a button-less TOUCH move (engines report buttons inconsistently)', () => {
    // Treating a 0 there as "released" would surface the bar in the middle of
    // every touch selection, so only the mouse path may release the guard.
    const { doc, show, dispose } = makeBridge()
    doc.fire('pointerdown')
    doc.fire('pointermove', pointerEvent('touch', 0))
    vi.advanceTimersByTime(120)

    expect(show).not.toHaveBeenCalled()
    dispose()
  })

  it('takes back a bar the self-heal timer surfaced once the drag resumes', () => {
    const { doc, show, hide, dispose } = makeBridge()
    doc.fire('pointerdown')

    // Pause past the window: the timer drops the guard and the bar surfaces.
    vi.advanceTimersByTime(800)
    expect(show).toHaveBeenCalledTimes(1)

    // Still dragging after all — the bar must be taken back.
    doc.fire('pointermove', pointerEvent('mouse', 1))
    expect(hide).toHaveBeenCalledTimes(1)

    doc.fire('pointerup')
    vi.advanceTimersByTime(120)
    expect(show).toHaveBeenCalledTimes(2)
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
