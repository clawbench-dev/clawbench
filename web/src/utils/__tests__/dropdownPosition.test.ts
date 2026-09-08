import { describe, expect, it, vi, afterEach } from 'vitest'
import { computeDropdownStyle, estimatePanelWidth, deferPosition } from '@/utils/dropdownPosition'

const rect = { left: 10, right: 50, bottom: 50, top: 26, width: 40 }

describe('estimatePanelWidth', () => {
  it('falls back to 200 when there is no anchor', () => {
    expect(estimatePanelWidth(null, 1024)).toBe(200)
  })
  it('returns anchor width when between min and max', () => {
    expect(estimatePanelWidth({ width: 240 }, 1024)).toBe(240)
  })
  it('clamps to the minimum width', () => {
    expect(estimatePanelWidth({ width: 40 }, 1024)).toBe(180)
  })
  it('clamps to the viewport-limited maximum on narrow screens', () => {
    // maxPanelWidth = min(320, 300-16) = 284
    expect(estimatePanelWidth({ width: 400 }, 300)).toBe(284)
  })
})

describe('computeDropdownStyle', () => {
  it('positions below the anchor, centered horizontally', () => {
    const style = computeDropdownStyle(rect, 200, 200, 1024, 768)
    // center = 10 + 20 - 100 = -70 → clamped to margin 8
    expect(style.position).toBe('fixed')
    expect(style.top).toBe('54px') // bottom(50) + 4
    expect(style.left).toBe('8px') // clamped to margin
    expect(style.minWidth).toBe('180px') // max(180, 40)
    expect(style.maxWidth).toBe('320px')
  })

  it('flips upward when there is not enough room below', () => {
    const nearBottom = { ...rect, bottom: 700, top: 676 }
    const style = computeDropdownStyle(nearBottom, 200, 200, 1024, 768)
    // bottom+4+height = 904 > 768-8 → flip: top = 676 - 200 - 4 = 472
    expect(style.top).toBe('472px')
  })

  it('clamps panel width to the viewport on narrow screens', () => {
    const style = computeDropdownStyle(rect, 400, 200, 300, 768)
    const maxW = parseInt(style.maxWidth, 10)
    expect(maxW).toBeLessThanOrEqual(300)
    expect(style.left).toBeDefined()
  })

  it('uses the default height when the panel is not laid out', () => {
    const style = computeDropdownStyle(rect, 200, 0, 1024, 768)
    // height = 320 default
    expect(style.top).toBe('54px') // fits below
  })

  it('caps the used width at the max panel width', () => {
    const style = computeDropdownStyle(rect, 600, 200, 1024, 768)
    // panelWidth min(600, 320) = 320; width min(320, 320)=320
    // left = 10 + 20 - 160 = -130 → clamped to 8
    expect(style.left).toBe('8px')
  })
})

describe('deferPosition', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('runs the update only after a double rAF', () => {
    const update = vi.fn()
    const rafCallbacks: FrameRequestCallback[] = []
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      rafCallbacks.push(cb)
      return rafCallbacks.length
    })

    deferPosition(update)
    expect(update).not.toHaveBeenCalled()

    // First rAF fires → it schedules the nested rAF, still no update.
    rafCallbacks[0](0)
    expect(rafCallbacks.length).toBe(2)
    expect(update).not.toHaveBeenCalled()

    // Second rAF fires → update finally runs.
    rafCallbacks[1](0)
    expect(update).toHaveBeenCalledTimes(1)
  })
})
