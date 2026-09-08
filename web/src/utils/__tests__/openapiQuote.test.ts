import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { attachOpenApiQuoteBridge } from '../openapiQuote.ts'
import type { OpenApiBridgeWindow } from '../openapiQuote.ts'
import type { QuoteData } from '@/composables/useChatContext.ts'

// ── Fake iframe window ──
// attachOpenApiQuoteBridge takes a structurally-typed window-like object, so
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
}

function createFakeWin(opts: { selection?: FakeSelection | null; doc?: FakeDoc } = {}): {
  win: OpenApiBridgeWindow
  doc: FakeDoc
  getSelectionMock: ReturnType<typeof vi.fn>
} {
  const doc = opts.doc ?? new FakeDoc()
  const selection = opts.selection === undefined ? new FakeSelection('selected text') : opts.selection
  const getSelectionMock = vi.fn(() => selection)
  const win: OpenApiBridgeWindow = {
    getSelection: getSelectionMock,
    document: doc,
  }
  return { win, doc, getSelectionMock }
}

function makeBridge(overrides: Partial<Parameters<typeof attachOpenApiQuoteBridge>[0]> = {}) {
  const show = vi.fn()
  const hide = vi.fn()
  const filePath = vi.fn(() => '/specs/api.yaml')
  const { win, doc, getSelectionMock } = createFakeWin()
  const dispose = attachOpenApiQuoteBridge({
    win,
    filePath,
    show,
    hide,
    ...overrides,
  })
  return { win, doc, getSelectionMock, show, hide, filePath, dispose }
}

describe('attachOpenApiQuoteBridge', () => {
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
