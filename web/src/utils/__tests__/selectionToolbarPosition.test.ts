import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

/**
 * Positioning for the floating selection toolbar (issue #544).
 *
 * The math is trivial; the reason this is tested at all is the ZOOM contract:
 * `getBoundingClientRect()` returns zoom-scaled coordinates while
 * `position: fixed` (and `offsetWidth`) are pre-zoom. If the two spaces are
 * mixed the bar lands in the wrong place *only* under the app's "UI scale"
 * setting, which no other test would catch. The zoom cases below are therefore
 * the load-bearing ones.
 *
 * Coordinate contract (from useSettingsConfig):
 *   - getBoundingClientRect() coordinates ARE scaled by zoom.
 *   - window.innerWidth / innerHeight are NOT scaled by zoom.
 *   - position: fixed CSS values are pre-zoom.
 *   - fixedCSS = viewportCoord / zoom.
 */

// Mock the two accessors so the test controls zoom and viewport deterministically
// instead of depending on jsdom's 1024×768 default and a real <html> zoom style.
const viewport = vi.hoisted(() => ({ width: 1000, height: 800 }))
const zoom = vi.hoisted(() => ({ value: 1 }))

vi.mock('@/composables/useSettingsConfig', () => ({
  getZoomedViewport: () => ({ width: viewport.width, height: viewport.height }),
  toFixedCSS: (v: number) => v / zoom.value,
}))

import { computeSelectionToolbarStyle } from '../selectionToolbarPosition'

function rect(top: number, bottom: number, left: number, right: number) {
  return { top, bottom, left, right }
}

describe('computeSelectionToolbarStyle', () => {
  beforeEach(() => {
    viewport.width = 1000
    viewport.height = 800
    zoom.value = 1
  })

  afterEach(() => {
    zoom.value = 1
  })

  it('centres horizontally on the selection', () => {
    // Selection centred at x=500 → bar left = 500 - 100/2 = 450.
    const style = computeSelectionToolbarStyle(rect(300, 320, 400, 600), {
      barWidth: 100,
      barHeight: 30,
    })
    expect(style.position).toBe('fixed')
    expect(style.left).toBe('450px')
  })

  it('places the bar above the selection by default (anchored by bottom)', () => {
    const style = computeSelectionToolbarStyle(rect(300, 320, 400, 600), {
      barWidth: 100,
      barHeight: 30,
      gap: 8,
    })
    // bottom = viewportHeight - rect.top + gap = 800 - 300 + 8
    expect(style.bottom).toBe('508px')
    expect(style.top).toBeUndefined()
  })

  it('flips below when there is not enough room above', () => {
    // rect.top = 20, barHeight 30 + gap 8 + edgeMargin 8 = 46 > 20 → below.
    const style = computeSelectionToolbarStyle(rect(20, 40, 400, 600), {
      barWidth: 100,
      barHeight: 30,
      gap: 8,
    })
    expect(style.top).toBe('48px') // rect.bottom + gap
    expect(style.bottom).toBeUndefined()
  })

  it('clamps to the left edge when the selection is near the start', () => {
    // Centre at x=20 → naive left = -30, clamped to edgeMargin.
    const style = computeSelectionToolbarStyle(rect(300, 320, 0, 40), {
      barWidth: 100,
      barHeight: 30,
      edgeMargin: 8,
    })
    expect(style.left).toBe('8px')
  })

  it('clamps to the right edge when the selection is near the end', () => {
    // Centre at x=980 → naive left = 930, right edge 1000 → max left = 1000-100-8 = 892.
    const style = computeSelectionToolbarStyle(rect(300, 320, 960, 1000), {
      barWidth: 100,
      barHeight: 30,
      edgeMargin: 8,
    })
    expect(style.left).toBe('892px')
  })

  it('converts zoom-scaled rect coordinates into pre-zoom fixed values', () => {
    // Zoom 2: innerWidth/innerHeight are NOT scaled, the rect IS.
    // Selection at pre-zoom CSS (x 200..400, y 300..320) → rect doubles to
    // (left 400, right 800, top 600, bottom 640).
    zoom.value = 2
    const style = computeSelectionToolbarStyle(rect(600, 640, 400, 800), {
      barWidth: 100,
      barHeight: 30,
      gap: 8,
    })

    // centre rect x=600 → pre-zoom 300 → left = 300 - 50 = 250
    expect(style.left).toBe('250px')
    // bottom = (innerHeight - rect.top) / zoom + gap = (800 - 600)/2 + 8
    expect(style.bottom).toBe('108px')
  })

  it('clamps horizontally against the pre-zoom viewport, not the rect space', () => {
    // Zoom 2: the pre-zoom viewport width is innerWidth/zoom = 500, so the
    // right-most legal left is 500 - 100 - 8 = 392. A selection whose rect
    // centre converts to 450 must therefore be pulled back to 392 — proving the
    // clamp compares pre-zoom values on both sides rather than mixing spaces
    // (mixing would leave it at 450, ~58px past the real edge).
    zoom.value = 2
    const style = computeSelectionToolbarStyle(rect(600, 640, 800, 1200), {
      barWidth: 100,
      barHeight: 30,
      edgeMargin: 8,
    })
    expect(style.left).toBe('392px')
  })

  it('shrinks to the available width when the bar is wider than the viewport allows', () => {
    // Narrow phone (375) under zoom 2 → pre-zoom viewport 187.5px. A 220px
    // toolbar cannot fit between two 8px margins (171.5px available), so it
    // must be capped AND centred against the capped width — clamping `left`
    // against the full 220px would pin it to the left edge while it still
    // overflowed the right.
    zoom.value = 2
    viewport.width = 375
    viewport.height = 800

    const style = computeSelectionToolbarStyle(rect(300, 320, 200, 260), {
      barWidth: 220,
      barHeight: 32,
      edgeMargin: 8,
    })

    expect(style.maxWidth).toBe('171.5px')
    // The capped bar exactly fills the available width, so the only legal
    // position is the left edge margin — a clamp against the UNcapped 220px
    // would instead compute 187.5 - 220 - 8 < 0 and pin it to 8 by accident,
    // while the bar still overflowed the right edge.
    expect(style.left).toBe('8px')
  })

  it('never produces NaN/Infinity when the bar size is unknown (zero)', () => {
    // The component falls back to constants, but a future caller passing 0
    // must not emit `left: NaNpx`.
    const style = computeSelectionToolbarStyle(rect(300, 320, 400, 600), {
      barWidth: 0,
      barHeight: 0,
    })
    expect(style.left).toBe('500px')
    expect(style.left).not.toContain('NaN')
    expect(style.bottom).not.toContain('NaN')
  })
})
