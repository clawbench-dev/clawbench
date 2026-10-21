import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { installInlineZoom, isZoomActive, uninstallInlineZoom } from '@/utils/inlineZoom'
// Importing svgMediaFit is what registers its refit implementation with
// inlineZoom (setRefitHook at module scope). This suite exists to drive the
// exit path THROUGH that hook — testing `refitFigure` on its own cannot see the
// ordering bug it was written for.
import { stampSvgFigure } from '@/utils/svgMediaFit'

/**
 * Leaving the zoom must re-measure the resting width against the CURRENT
 * column.
 *
 * `deactivate()` restores the width captured at activation, which was measured
 * before the column could change (window resize, side panel toggled). The refit
 * exists to replace that stale value — but it used to run BEFORE the
 * `is-zooming` class was removed, and `refitFigure` returns early while that
 * class is set. So the hook was a no-op on every exit and the diagram went back
 * to the stale size, which is precisely what its own comment promised it would
 * not do.
 *
 * The unit tests in svgMediaFit.test.ts call `refitFigure` directly, so they
 * pass whether or not `deactivate` ever calls it — this is the only test that
 * would have caught it.
 */

function withPointerDevice(): void {
  vi.stubGlobal('matchMedia', (q: string) => ({
    matches: true,
    media: q,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    onchange: null,
    dispatchEvent: () => false,
  }))
}

/** A stamped Mermaid figure inside a column whose width the test controls. */
function mountColumn(vbW: number, vbH: number) {
  document.body.innerHTML = ''
  const column = document.createElement('div')
  const figure = document.createElement('div')
  figure.className = 'image-block-wrapper'
  const cell = document.createElement('div')
  cell.className = 'mermaid'
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
  svg.setAttribute('viewBox', `0 0 ${vbW} ${vbH}`)
  cell.appendChild(svg)
  figure.appendChild(cell)
  column.appendChild(figure)
  document.body.appendChild(column)

  let colW = 800
  Object.defineProperty(column, 'getBoundingClientRect', {
    value: () => ({ width: colW, height: 0, top: 0, left: 0, right: colW, bottom: 0, x: 0, y: 0, toJSON() {} }),
    configurable: true,
  })
  // jsdom has no layout: the zoom maths needs a frame box for the figure.
  Object.defineProperty(figure, 'getBoundingClientRect', {
    value: () => ({ width: colW, height: 400, top: 0, left: 0, right: colW, bottom: 400, x: 0, y: 0, toJSON() {} }),
    configurable: true,
  })
  return { column, figure, svg, setColumnWidth: (w: number) => { colW = w } }
}

function click(target: Element): void {
  target.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
}

describe('inline zoom × svgMediaFit — exit refit', () => {
  beforeEach(() => {
    withPointerDevice()
    installInlineZoom()
  })
  afterEach(() => {
    uninstallInlineZoom()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it('re-measures the resting width against the CURRENT column on exit', () => {
    // 1600x70 artwork: wide and short, so the column is the limiting factor and
    // the resting width tracks it directly.
    const { figure, svg, setColumnWidth } = mountColumn(1600, 70)
    stampSvgFigure(figure)
    expect(parseFloat(svg.style.width)).toBe(800)

    click(svg)
    expect(isZoomActive()).toBe(true)

    // The column changes while the figure is zoomed — a side panel collapsed.
    setColumnWidth(500)

    click(document.body)
    expect(isZoomActive()).toBe(false)

    // Not the 800 captured at activation: the current column.
    expect(parseFloat(svg.style.width)).toBe(500)
  })

  it('also picks up a WIDER column on exit', () => {
    // Guards against a fix that only shrinks: restoring the captured width would
    // fail this the other way round.
    const { figure, svg, setColumnWidth } = mountColumn(1600, 70)
    stampSvgFigure(figure)
    expect(parseFloat(svg.style.width)).toBe(800)

    click(svg)
    setColumnWidth(1200)
    click(document.body)

    expect(parseFloat(svg.style.width)).toBe(1200)
  })

  it('leaves the figure unzoomed before the refit runs', () => {
    // The hook is allowed to write sizing only when the active marker is gone;
    // if the class were still present the measurement would see the zoomed
    // fixed-viewport frame instead of the column.
    const { figure, svg, setColumnWidth } = mountColumn(1600, 70)
    stampSvgFigure(figure)
    click(svg)
    setColumnWidth(640)
    click(document.body)

    expect(figure.classList.contains('is-zooming')).toBe(false)
    expect(parseFloat(svg.style.width)).toBe(640)
  })
})
