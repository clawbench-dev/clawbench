import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildInlineZoomJs } from '@/utils/exportMarkdownHtml'

/**
 * The exported document's inline-zoom script is a HAND-COPIED mirror of
 * `web/src/utils/inlineZoom.ts` (the export is one self-contained HTML file
 * with no bundler, so it cannot import the module).
 *
 * It drifted. At one point the wheel rule, the scale cap and the table-cell
 * exclusion were ALL behind the app at the same time, and the only test was
 * `expect(html).toContain('is-zooming')` — a substring check that cannot tell a
 * correct script from a broken one. These tests execute the real script against
 * a real DOM, so a divergence fails instead of shipping.
 *
 * Each assertion below corresponds to a rule the app already enforces; when the
 * app's rule changes, this file is the reminder that the mirror must follow.
 */

const script = buildInlineZoomJs()

/** Run the exported IIFE against the jsdom globals. */
function installExportedZoom(): void {
  // eslint-disable-next-line no-new-func
  new Function(script)()
}

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

interface Mounted {
  figure: HTMLElement
  svg: SVGElement
}

/** A figure shaped exactly like the renderer emits: wrapper > .mermaid > svg. */
function mountFigure(parent: Element = document.body): Mounted {
  const figure = document.createElement('div')
  figure.className = 'image-block-wrapper'
  const cell = document.createElement('div')
  cell.className = 'mermaid'
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
  Object.defineProperty(svg, 'viewBox', {
    value: { baseVal: { x: 0, y: 0, width: 300, height: 200 } },
    configurable: true,
  })
  cell.appendChild(svg)
  figure.appendChild(cell)
  parent.appendChild(figure)

  // jsdom has no layout: give the frame and cell a real box so the script's
  // scale maths produces observable numbers instead of 0.
  const rect = { width: 300, height: 200, top: 0, left: 0, right: 300, bottom: 200, x: 0, y: 0, toJSON: () => ({}) }
  for (const el of [figure, cell]) {
    Object.defineProperty(el, 'getBoundingClientRect', { value: () => rect, configurable: true })
  }
  return { figure, svg: svg as unknown as SVGElement }
}

function wheel(target: Element, deltaY: number, clientX = 150, clientY = 100): WheelEvent {
  const e = new WheelEvent('wheel', { deltaY, clientX, clientY, bubbles: true, cancelable: true })
  target.dispatchEvent(e)
  return e
}

function click(target: Element): void {
  target.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
}

describe('exported inline zoom — wheel belongs to the page at rest', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    withPointerDevice()
    installExportedZoom()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('does not zoom on an UPWARD notch at rest, and does not swallow it', () => {
    // This is the exact bug the mirror shipped with: `if (e.deltaY > 0) return`
    // meant an upward notch activated the zoom AND preventDefault'd, so a reader
    // scrolling back up the document got the diagram zoomed instead and lost
    // the scroll. The app-side module fixed this; the copy did not follow.
    const { figure, svg } = mountFigure()
    const e = wheel(svg, -120)

    expect(figure.classList.contains('is-zooming')).toBe(false)
    expect(e.defaultPrevented).toBe(false)
    expect(svg.style.transform).toBe('')
  })

  it('does not zoom on a DOWNWARD notch at rest either', () => {
    const { figure, svg } = mountFigure()
    const e = wheel(svg, 120)

    expect(figure.classList.contains('is-zooming')).toBe(false)
    expect(e.defaultPrevented).toBe(false)
  })

  it('enters the zoom on click, then the wheel belongs to the figure', () => {
    // Click-to-enter is the contract: the wheel only acts on a figure the reader
    // explicitly opened.
    const { figure, svg } = mountFigure()
    click(svg)
    expect(figure.classList.contains('is-zooming')).toBe(true)

    const e = wheel(svg, -120)
    expect(e.defaultPrevented).toBe(true)
  })
})

describe('exported inline zoom — scale cap matches the app', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    withPointerDevice()
    installExportedZoom()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('keeps zooming past 8x, up to the app cap', () => {
    // The mirror capped at 8 while the app moved to 64. At 8 the artwork is
    // barely larger than the frame, so the gesture reads as broken.
    const { svg } = mountFigure()
    click(svg)

    // 1.15^n; n=20 is ~16x, comfortably past the old cap.
    for (let i = 0; i < 20; i++) wheel(svg, -120)
    const wide = parseFloat(svg.style.width)
    // Base is 300px at scale 1 (fit of 300x200 artwork into a 300x200 box).
    expect(wide).toBeGreaterThan(300 * 9)
  })

  it('stops at the cap instead of growing without bound', () => {
    const { svg } = mountFigure()
    click(svg)
    for (let i = 0; i < 40; i++) wheel(svg, -120)
    const atCap = parseFloat(svg.style.width)
    for (let i = 0; i < 10; i++) wheel(svg, -120)
    expect(parseFloat(svg.style.width)).toBe(atCap)
    // 64 * 300px base — the same MAX_SAFE_SCALE the module uses.
    expect(atCap).toBeLessThanOrEqual(64 * 300 + 1)
  })
})

describe('exported inline zoom — table cells opt out', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    withPointerDevice()
    installExportedZoom()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('does not zoom a figure inside a table cell', () => {
    // A figure in a cell is a thumbnail in a grid; zooming it grows the row and
    // the cell has nowhere to pan. Checked structurally (td/th), matching the
    // app — the renderer emits no marker class to key off.
    const table = document.createElement('table')
    const tbody = document.createElement('tbody')
    const tr = document.createElement('tr')
    const td = document.createElement('td')
    tr.appendChild(td)
    tbody.appendChild(tr)
    table.appendChild(tbody)
    document.body.appendChild(table)

    const { figure, svg } = mountFigure(td)
    click(svg)
    expect(figure.classList.contains('is-zooming')).toBe(false)

    const e = wheel(svg, -120)
    expect(e.defaultPrevented).toBe(false)
  })

  it('still zooms the same figure once it is outside the table', () => {
    // Proves the exclusion is scoped to the cell and not a blanket opt-out.
    const { figure, svg } = mountFigure()
    click(svg)
    expect(figure.classList.contains('is-zooming')).toBe(true)
  })
})
