import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import {
  DRAG_SCROLL_THRESHOLD,
  canDragScroll,
  clampScrollLeft,
  attachDragScroll,
} from '@/utils/dragScroll'

/**
 * A minimal element stand-in: jsdom elements have no scroll geometry (scrollWidth
 * and clientWidth are always 0) and no setPointerCapture, so the drag logic is
 * driven against a controllable fake.
 */
function makeEl(scrollLeft: number, scrollWidth: number, clientWidth: number) {
  const listeners = new Map<string, EventListener>()
  const el = {
    scrollLeft,
    scrollWidth,
    clientWidth,
    classList: {
      _set: new Set<string>(),
      add(c: string) {
        this._set.add(c)
      },
      remove(c: string) {
        this._set.delete(c)
      },
      contains(c: string) {
        return this._set.has(c)
      },
    },
    setPointerCapture: vi.fn(),
    releasePointerCapture: vi.fn(),
    addEventListener: vi.fn((type: string, fn: EventListener) => listeners.set(type, fn)),
    removeEventListener: vi.fn((type: string) => listeners.delete(type)),
    _listeners: listeners,
  }
  return el as unknown as HTMLElement & { scrollLeft: number; _listeners: Map<string, EventListener> }
}

function pointerDown(el: HTMLElement, { pointerId = 1, pointerType = 'mouse', button = 0, clientX = 100 } = {}) {
  const fn = (el as unknown as { _listeners: Map<string, EventListener> })._listeners.get('pointerdown')
  fn?.({ pointerId, pointerType, button, clientX } as unknown as Event)
}

/** Dispatch a window-level pointer event (the drag tracks move/up on window). */
function fireWindow(type: string, init: Record<string, unknown>) {
  window.dispatchEvent(Object.assign(new Event(type), init))
}

describe('canDragScroll', () => {
  it('allows a primary-button mouse press on an overflowing strip', () => {
    expect(canDragScroll('mouse', 0, 800, 300)).toBe(true)
  })

  // A strip that fits has nothing to scroll: intercepting the press would only
  // swallow the gesture and grow a grab cursor for nothing.
  it('refuses when the strip does not overflow', () => {
    expect(canDragScroll('mouse', 0, 300, 300)).toBe(false)
    // Sub-pixel rounding can leave a 1px "overflow" that is visually flush.
    expect(canDragScroll('mouse', 0, 301, 300)).toBe(false)
  })

  // Touch pans natively with momentum; hijacking it would fight the platform.
  it('refuses touch, which already pans natively', () => {
    expect(canDragScroll('touch', 0, 800, 300)).toBe(false)
  })

  it('refuses secondary buttons', () => {
    expect(canDragScroll('mouse', 2, 800, 300)).toBe(false)
  })
})

describe('clampScrollLeft', () => {
  it('clamps into range', () => {
    expect(clampScrollLeft(120, 800, 300)).toBe(120)
    expect(clampScrollLeft(-40, 800, 300)).toBe(0)
    expect(clampScrollLeft(999, 800, 300)).toBe(500)
  })

  it('returns 0 when there is nothing to scroll', () => {
    expect(clampScrollLeft(50, 300, 300)).toBe(0)
  })
})

describe('attachDragScroll', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('registers a pointerdown listener and returns a disposer that removes it', () => {
    const el = makeEl(0, 800, 300)
    const dispose = attachDragScroll(el)
    expect(el.addEventListener).toHaveBeenCalledWith('pointerdown', expect.any(Function))

    dispose()
    expect(el.removeEventListener).toHaveBeenCalledWith('pointerdown', expect.any(Function))
  })

  it('does not begin a drag below the threshold', () => {
    const el = makeEl(200, 800, 300)
    attachDragScroll(el)
    pointerDown(el, { clientX: 100 })
    fireWindow('pointermove', { pointerId: 1, clientX: 100 + DRAG_SCROLL_THRESHOLD - 1 })

    expect(el.scrollLeft).toBe(200)
    expect(el.classList.contains('is-dragging')).toBe(false)
    // Capture is taken only once the gesture is a drag — capturing on
    // pointerdown would retarget the click away from the button under the press.
    expect(el.setPointerCapture).not.toHaveBeenCalled()
  })

  it('scrolls left when dragging right (content follows the pointer)', () => {
    const el = makeEl(200, 800, 300)
    attachDragScroll(el)
    pointerDown(el, { clientX: 100 })
    fireWindow('pointermove', { pointerId: 1, clientX: 40 })

    expect(el.scrollLeft).toBe(260)
    expect(el.classList.contains('is-dragging')).toBe(true)
  })

  it('scrolls right when dragging left', () => {
    const el = makeEl(200, 800, 300)
    attachDragScroll(el)
    pointerDown(el, { clientX: 100 })
    fireWindow('pointermove', { pointerId: 1, clientX: 160 })

    expect(el.scrollLeft).toBe(140)
  })

  it('captures the pointer once the drag starts, and releases it on pointerup', () => {
    const el = makeEl(200, 800, 300)
    attachDragScroll(el)
    pointerDown(el, { clientX: 100 })
    fireWindow('pointermove', { pointerId: 1, clientX: 40 })
    expect(el.setPointerCapture).toHaveBeenCalledWith(1)

    fireWindow('pointerup', { pointerId: 1 })
    expect(el.releasePointerCapture).toHaveBeenCalledWith(1)
    expect(el.classList.contains('is-dragging')).toBe(false)
  })

  it('clamps at both ends', () => {
    const el = makeEl(50, 800, 300)
    attachDragScroll(el)
    pointerDown(el, { clientX: 100 })
    fireWindow('pointermove', { pointerId: 1, clientX: 400 })
    expect(el.scrollLeft).toBe(0)

    fireWindow('pointerup', { pointerId: 1 })

    const el2 = makeEl(480, 800, 300)
    attachDragScroll(el2)
    pointerDown(el2, { clientX: 100 })
    fireWindow('pointermove', { pointerId: 1, clientX: 20 })
    expect(el2.scrollLeft).toBe(500)
  })

  it('ignores move/up from a different pointer id', () => {
    const el = makeEl(200, 800, 300)
    attachDragScroll(el)
    pointerDown(el, { pointerId: 7, clientX: 100 })
    fireWindow('pointermove', { pointerId: 8, clientX: 40 })

    expect(el.scrollLeft).toBe(200)
    expect(el.classList.contains('is-dragging')).toBe(false)
  })

  it('stops tracking after pointerup (no scroll from a stray move)', () => {
    const el = makeEl(200, 800, 300)
    attachDragScroll(el)
    pointerDown(el, { clientX: 100 })
    fireWindow('pointermove', { pointerId: 1, clientX: 40 })
    fireWindow('pointerup', { pointerId: 1 })
    fireWindow('pointermove', { pointerId: 1, clientX: 0 })

    expect(el.scrollLeft).toBe(260)
  })

  it('ends the drag on pointercancel', () => {
    const el = makeEl(200, 800, 300)
    attachDragScroll(el)
    pointerDown(el, { clientX: 100 })
    fireWindow('pointermove', { pointerId: 1, clientX: 40 })
    fireWindow('pointercancel', { pointerId: 1 })

    expect(el.classList.contains('is-dragging')).toBe(false)
    expect(el.releasePointerCapture).toHaveBeenCalledWith(1)
  })

  it('ignores touch presses (native panning owns them)', () => {
    const el = makeEl(200, 800, 300)
    attachDragScroll(el)
    pointerDown(el, { pointerType: 'touch', clientX: 100 })
    fireWindow('pointermove', { pointerId: 1, clientX: 20 })

    expect(el.scrollLeft).toBe(200)
  })

  it('disposer drops an in-flight drag state and listeners', () => {
    const el = makeEl(200, 800, 300)
    const dispose = attachDragScroll(el)
    pointerDown(el, { clientX: 100 })
    fireWindow('pointermove', { pointerId: 1, clientX: 40 })
    expect(el.classList.contains('is-dragging')).toBe(true)

    dispose()

    expect(el.classList.contains('is-dragging')).toBe(false)
    expect(el.releasePointerCapture).toHaveBeenCalledWith(1)
    fireWindow('pointermove', { pointerId: 1, clientX: 0 })
    expect(el.scrollLeft).toBe(260)
  })
})
