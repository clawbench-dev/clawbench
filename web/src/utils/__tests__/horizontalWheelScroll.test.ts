import { describe, expect, it, vi } from 'vitest'
import {
  wheelDeltaPx,
  resolveHorizontalWheel,
  onHorizontalWheel,
} from '@/utils/horizontalWheelScroll'

/**
 * A minimal element stand-in: the real one is not constructible in jsdom with
 * controllable scroll geometry, and scrollLeft must be writable/readable.
 */
function makeEl(scrollLeft: number, scrollWidth: number, clientWidth: number) {
  return { scrollLeft, scrollWidth, clientWidth } as unknown as HTMLElement & { scrollLeft: number }
}

/** A wheel event whose currentTarget we control (jsdom does not set it). */
function makeEvent(
  el: HTMLElement | null,
  { deltaX = 0, deltaY = 0, deltaMode = 0, defaultPrevented = false } = {},
) {
  const preventDefault = vi.fn()
  const e = {
    deltaX,
    deltaY,
    deltaMode,
    currentTarget: el,
    defaultPrevented,
    preventDefault,
  }
  return e as unknown as WheelEvent & { preventDefault: ReturnType<typeof vi.fn> }
}

describe('wheelDeltaPx', () => {
  it('uses deltaY for a plain vertical mouse wheel', () => {
    expect(wheelDeltaPx({ deltaX: 0, deltaY: 120, deltaMode: 0 })).toBe(120)
  })

  // A trackpad two-finger horizontal swipe already reports deltaX; using deltaY
  // there would fight the gesture.
  it('prefers deltaX when the gesture is already horizontal', () => {
    expect(wheelDeltaPx({ deltaX: 80, deltaY: 5, deltaMode: 0 })).toBe(80)
  })

  // Firefox reports line mode for a plain wheel; unscaled, one notch would move
  // a few px and feel broken.
  it('scales line mode to pixels', () => {
    expect(wheelDeltaPx({ deltaX: 0, deltaY: 3, deltaMode: 1 })).toBe(120)
  })

  it('scales page mode to pixels', () => {
    expect(wheelDeltaPx({ deltaX: 0, deltaY: 1, deltaMode: 2 })).toBe(800)
  })
})

describe('resolveHorizontalWheel', () => {
  // A strip that fits has nothing to scroll, so the page must keep the gesture.
  it('does not consume when the strip does not overflow', () => {
    expect(resolveHorizontalWheel(0, 300, 300, 120)).toEqual({ consume: false, nextScrollLeft: 0 })
  })

  it('does not consume a zero delta', () => {
    expect(resolveHorizontalWheel(0, 800, 300, 0)).toEqual({ consume: false, nextScrollLeft: 0 })
  })

  it('consumes and advances while there is room', () => {
    expect(resolveHorizontalWheel(0, 800, 300, 120)).toEqual({ consume: true, nextScrollLeft: 120 })
  })

  // The boundary handoff: consuming here would trap the page (the strip cannot
  // move, and the page never scrolls either).
  it('does not consume when already at the end and pushing further', () => {
    expect(resolveHorizontalWheel(500, 800, 300, 120)).toEqual({ consume: false, nextScrollLeft: 500 })
  })

  it('does not consume when already at the start and pushing back', () => {
    expect(resolveHorizontalWheel(0, 800, 300, -120)).toEqual({ consume: false, nextScrollLeft: 0 })
  })

  it('clamps the target into range', () => {
    expect(resolveHorizontalWheel(450, 800, 300, 120).nextScrollLeft).toBe(500)
  })

  // Sub-pixel scrollLeft can sit a hair inside the bound while looking flush;
  // without the tolerance the last notch would do nothing.
  it('treats a sub-pixel offset at the end as the end', () => {
    expect(resolveHorizontalWheel(499.5, 800, 300, 120).consume).toBe(false)
  })

  it('scrolls back from the end', () => {
    const d = resolveHorizontalWheel(500, 800, 300, -120)
    expect(d.consume).toBe(true)
    expect(d.nextScrollLeft).toBe(380)
  })
})

describe('onHorizontalWheel', () => {
  it('scrolls the strip and prevents the default when there is room', () => {
    const el = makeEl(0, 800, 300)
    const e = makeEvent(el, { deltaY: 120 })

    onHorizontalWheel(e)

    expect(el.scrollLeft).toBe(120)
    expect(e.preventDefault).toHaveBeenCalled()
  })

  it('leaves the page alone once the strip is at the end', () => {
    const el = makeEl(500, 800, 300)
    const e = makeEvent(el, { deltaY: 120 })

    onHorizontalWheel(e)

    expect(el.scrollLeft).toBe(500)
    expect(e.preventDefault).not.toHaveBeenCalled()
  })

  // The two strips are NESTED and wheel bubbles inner → outer. Without the
  // defaultPrevented check, one notch would advance BOTH strips.
  it('defers to an inner strip that already handled the notch', () => {
    const outer = makeEl(0, 800, 300)
    const e = makeEvent(outer, { deltaY: 120, defaultPrevented: true })

    onHorizontalWheel(e)

    expect(outer.scrollLeft).toBe(0)
    expect(e.preventDefault).not.toHaveBeenCalled()
  })

  it('is a no-op without a currentTarget', () => {
    const e = makeEvent(null, { deltaY: 120 })
    expect(() => onHorizontalWheel(e)).not.toThrow()
    expect(e.preventDefault).not.toHaveBeenCalled()
  })

  it('scrolls back with a negative delta', () => {
    const el = makeEl(300, 800, 300)
    const e = makeEvent(el, { deltaY: -120 })

    onHorizontalWheel(e)

    expect(el.scrollLeft).toBe(180)
    expect(e.preventDefault).toHaveBeenCalled()
  })
})
