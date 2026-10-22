import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  SVG_FIT_CLASS,
  SVG_AR_PROP,
  isSvgImageSrc,
  svgMediaAspectRatio,
  refitFigure,
  stampSvgFigure,
  stampSvgFigures,
  stampSvgFiguresAsync,
  installSvgMediaAutoFit,
} from '@/utils/svgMediaFit'

/** Build a detached figure the way the media-block factory does. */
function figure(inner: string): HTMLElement {
  const host = document.createElement('div')
  host.innerHTML = `<div class="image-block-wrapper">${inner}</div>`
  return host.querySelector('.image-block-wrapper') as HTMLElement
}

const svgWrap = (svg: string) => `<span class="lightbox-svg-wrap">${svg}</span>`
const imgWrap = (src: string, cls = 'lightbox-img') =>
  `<span class="lightbox-img-wrap"><img class="${cls}" src="${src}"></span>`

/** jsdom never loads images, so natural size must be injected. */
function withNaturalSize(img: HTMLImageElement, w: number, h: number): void {
  Object.defineProperty(img, 'naturalWidth', { value: w, configurable: true })
  Object.defineProperty(img, 'naturalHeight', { value: h, configurable: true })
}

describe('isSvgImageSrc', () => {
  it('matches every served form of an SVG reference', () => {
    expect(isSvgImageSrc('chart.svg')).toBe(true)
    expect(isSvgImageSrc('/api/fs/raw/docs/chart.svg?t=1')).toBe(true)
    expect(isSvgImageSrc('/api/fs/raw/docs/chart.svg#frag')).toBe(true)
    // Share mode / absolute paths encode the target in a query parameter.
    expect(isSvgImageSrc('/api/share/tok/local?path=%2Ftmp%2Fchart.svg&t=1')).toBe(true)
    expect(isSvgImageSrc('data:image/svg+xml;base64,PHN2Zy8+')).toBe(true)
  })

  it('does not match raster formats or unrelated srcs', () => {
    expect(isSvgImageSrc('photo.png')).toBe(false)
    expect(isSvgImageSrc('/api/fs/thumb?target=docs/a.jpg&w=1200')).toBe(false)
    expect(isSvgImageSrc('')).toBe(false)
    expect(isSvgImageSrc(null)).toBe(false)
    expect(isSvgImageSrc(undefined)).toBe(false)
  })

  it('does not match a longer extension that merely starts with .svg', () => {
    expect(isSvgImageSrc('archive.svgz')).toBe(false)
  })
})

describe('svgMediaAspectRatio — inline <svg>', () => {
  it('uses width/height attributes when both are present', () => {
    const el = new DOMParser().parseFromString(
      '<svg viewBox="0 0 100 400" width="200" height="100"></svg>', 'text/html'
    ).querySelector('svg')!
    // Attributes win over the (contradictory) viewBox.
    expect(svgMediaAspectRatio(el)).toBeCloseTo(2)
  })

  it('accepts px-suffixed lengths', () => {
    const el = new DOMParser().parseFromString(
      '<svg width="300px" height="100px"></svg>', 'text/html'
    ).querySelector('svg')!
    expect(svgMediaAspectRatio(el)).toBeCloseTo(3)
  })

  it('falls back to viewBox when width/height are absent', () => {
    const el = new DOMParser().parseFromString(
      '<svg viewBox="0 0 400 100"></svg>', 'text/html'
    ).querySelector('svg')!
    expect(svgMediaAspectRatio(el)).toBeCloseTo(4)
  })

  it('falls back to viewBox when only one dimension attribute is present', () => {
    const el = new DOMParser().parseFromString(
      '<svg viewBox="0 0 400 100" width="200"></svg>', 'text/html'
    ).querySelector('svg')!
    expect(svgMediaAspectRatio(el)).toBeCloseTo(4)
  })

  it('accepts a comma-separated viewBox', () => {
    const el = new DOMParser().parseFromString(
      '<svg viewBox="0,0,300,100"></svg>', 'text/html'
    ).querySelector('svg')!
    expect(svgMediaAspectRatio(el)).toBeCloseTo(3)
  })

  it('returns null when neither attributes nor a usable viewBox exist', () => {
    const el = new DOMParser().parseFromString('<svg></svg>', 'text/html').querySelector('svg')!
    expect(svgMediaAspectRatio(el)).toBeNull()
  })

  it('rejects a degenerate viewBox (zero height) instead of dividing by zero', () => {
    const el = new DOMParser().parseFromString(
      '<svg viewBox="0 0 100 0"></svg>', 'text/html'
    ).querySelector('svg')!
    expect(svgMediaAspectRatio(el)).toBeNull()
  })

  it('rejects percentage dimensions — they cannot express a ratio', () => {
    const el = new DOMParser().parseFromString(
      '<svg width="100%" height="50%" viewBox="0 0 10 5"></svg>', 'text/html'
    ).querySelector('svg')!
    // Falls through to viewBox.
    expect(svgMediaAspectRatio(el)).toBeCloseTo(2)
  })
})

describe('svgMediaAspectRatio — <img>', () => {
  it('uses the intrinsic size of an SVG image once decoded', () => {
    const host = document.createElement('div')
    host.innerHTML = imgWrap('chart.svg')
    const img = host.querySelector('img')!
    withNaturalSize(img, 400, 100)
    expect(svgMediaAspectRatio(img)).toBeCloseTo(4)
  })

  it('returns null for a raster image (only SVG media is resized)', () => {
    const host = document.createElement('div')
    host.innerHTML = imgWrap('photo.png')
    const img = host.querySelector('img')!
    withNaturalSize(img, 400, 100)
    expect(svgMediaAspectRatio(img)).toBeNull()
  })

  it('returns null for an SVG image that has not decoded yet', () => {
    const host = document.createElement('div')
    host.innerHTML = imgWrap('chart.svg')
    // jsdom reports 0x0 until a size is injected — mirrors the real "not loaded".
    expect(svgMediaAspectRatio(host.querySelector('img')!)).toBeNull()
  })

  it('returns null for a non-media element', () => {
    const host = document.createElement('div')
    host.innerHTML = '<div class="mermaid"></div>'
    expect(svgMediaAspectRatio(host.querySelector('.mermaid'))).toBeNull()
  })

  it('returns null for null input', () => {
    expect(svgMediaAspectRatio(null)).toBeNull()
  })
})

describe('stampSvgFigure', () => {
  it('stamps the marker class and ratio on an inline-svg figure', () => {
    const w = figure(svgWrap('<svg class="lightbox-svg" viewBox="0 0 400 100"></svg>'))
    expect(stampSvgFigure(w)).toBe(true)
    expect(w.classList.contains(SVG_FIT_CLASS)).toBe(true)
    expect(w.style.getPropertyValue(SVG_AR_PROP)).toBe('4')
  })

  it('stamps an SVG-file image figure once the image has decoded', () => {
    const w = figure(imgWrap('/api/fs/raw/docs/chart.svg?t=1'))
    withNaturalSize(w.querySelector('img')!, 100, 400)
    expect(stampSvgFigure(w)).toBe(true)
    expect(w.style.getPropertyValue(SVG_AR_PROP)).toBe('0.25')
  })

  it('leaves a raster image figure untouched', () => {
    const w = figure(imgWrap('/api/fs/thumb?target=docs/a.png&w=1200'))
    withNaturalSize(w.querySelector('img')!, 400, 100)
    expect(stampSvgFigure(w)).toBe(false)
    expect(w.classList.contains(SVG_FIT_CLASS)).toBe(false)
  })

  it('stamps a mermaid figure from its diagram viewBox', () => {
    // Mermaid renders through its own path (mermaid.ts → armMermaidFigure) as
    // `.image-block-wrapper > .mermaid > svg` with NO `lightbox-svg` marker.
    // It is still SVG media and must be sized like the rest — a wide flowchart
    // otherwise collapses to the 300px default replaced-element width inside
    // the fit-content figure.
    const w = figure('<div class="mermaid"><svg viewBox="0 0 1008.125 70"></svg></div>')
    expect(stampSvgFigure(w)).toBe(true)
    expect(w.classList.contains(SVG_FIT_CLASS)).toBe(true)
    expect(w.style.getPropertyValue(SVG_AR_PROP)).toBe(String(1008.125 / 70))
  })

  it('stamps a mermaid figure whose svg carries width/height attributes', () => {
    const w = figure('<div class="mermaid"><svg width="400" height="100" viewBox="0 0 400 100"></svg></div>')
    expect(stampSvgFigure(w)).toBe(true)
    expect(w.style.getPropertyValue(SVG_AR_PROP)).toBe('4')
  })

  it('ignores the toolbar icon svg inside the header', () => {
    // Only the CONTENT cell media counts; the view button's own icon has a
    // viewBox too and must not be picked up.
    const w = figure(
      '<div class="image-block-header"><span class="image-block-header-actions">'
      + '<button class="image-block-view-btn"><svg viewBox="0 0 24 24"></svg></button>'
      + '</span></div>'
      + imgWrap('/api/fs/raw/docs/photo.png')
    )
    withNaturalSize(w.querySelector('img')!, 400, 100)
    expect(stampSvgFigure(w)).toBe(false)
    expect(w.classList.contains(SVG_FIT_CLASS)).toBe(false)
  })

  it('returns false for an svg with no resolvable ratio', () => {
    const w = figure(svgWrap('<svg class="lightbox-svg"></svg>'))
    expect(stampSvgFigure(w)).toBe(false)
    expect(w.classList.contains(SVG_FIT_CLASS)).toBe(false)
  })

  it('is idempotent and re-stamps when the ratio changes', () => {
    const w = figure(svgWrap('<svg class="lightbox-svg" viewBox="0 0 400 100"></svg>'))
    stampSvgFigure(w)
    stampSvgFigure(w)
    expect(w.style.getPropertyValue(SVG_AR_PROP)).toBe('4')
    // A later pass with a corrected ratio overwrites rather than duplicating.
    const svg = w.querySelector('svg')!
    svg.setAttribute('viewBox', '0 0 100 400')
    stampSvgFigure(w)
    expect(w.style.getPropertyValue(SVG_AR_PROP)).toBe('0.25')
    expect(w.className).toBe('image-block-wrapper svg-fit')
  })
})

describe('stampSvgFigures', () => {
  it('stamps every eligible figure under a root and skips the rest', () => {
    const host = document.createElement('div')
    host.innerHTML = [
      figure(svgWrap('<svg class="lightbox-svg" viewBox="0 0 400 100"></svg>')).outerHTML,
      figure(imgWrap('/api/fs/raw/docs/chart.svg?t=1')).outerHTML,
      figure(imgWrap('/api/fs/thumb?target=a.png&w=1200')).outerHTML,
    ].join('')
    withNaturalSize(host.querySelectorAll('img')[0], 100, 400)

    expect(stampSvgFigures(host)).toBe(2)
    const wraps = host.querySelectorAll('.image-block-wrapper')
    expect(wraps[0].classList.contains(SVG_FIT_CLASS)).toBe(true)
    expect(wraps[1].classList.contains(SVG_FIT_CLASS)).toBe(true)
    expect(wraps[2].classList.contains(SVG_FIT_CLASS)).toBe(false)
  })

  it('returns 0 for a null root', () => {
    expect(stampSvgFigures(null)).toBe(0)
  })
})

describe('stampSvgFiguresAsync', () => {
  it('awaits decode of an unloaded SVG image before stamping', async () => {
    const w = figure(imgWrap('chart.svg'))
    const img = w.querySelector('img')!
    let decoded = false
    Object.defineProperty(img, 'decode', {
      value: () => Promise.resolve().then(() => {
        decoded = true
        withNaturalSize(img, 400, 100)
      }),
      configurable: true,
    })

    await stampSvgFiguresAsync(w.parentElement)
    expect(decoded).toBe(true)
    expect(w.classList.contains(SVG_FIT_CLASS)).toBe(true)
    expect(w.style.getPropertyValue(SVG_AR_PROP)).toBe('4')
  })

  it('does not await decode for an already-sized SVG image', async () => {
    const w = figure(imgWrap('chart.svg'))
    const img = w.querySelector('img')!
    withNaturalSize(img, 200, 100)
    let decodeCalls = 0
    Object.defineProperty(img, 'decode', {
      value: () => { decodeCalls++; return Promise.resolve() },
      configurable: true,
    })

    await stampSvgFiguresAsync(w.parentElement)
    expect(decodeCalls).toBe(0)
    expect(w.style.getPropertyValue(SVG_AR_PROP)).toBe('2')
  })

  it('does not await decode for a raster image', async () => {
    const w = figure(imgWrap('photo.png'))
    const img = w.querySelector('img')!
    let decodeCalls = 0
    Object.defineProperty(img, 'decode', {
      value: () => { decodeCalls++; return Promise.resolve() },
      configurable: true,
    })

    await stampSvgFiguresAsync(w.parentElement)
    expect(decodeCalls).toBe(0)
    expect(w.classList.contains(SVG_FIT_CLASS)).toBe(false)
  })

  it('tolerates a decode rejection without throwing', async () => {
    const w = figure(imgWrap('broken.svg'))
    const img = w.querySelector('img')!
    Object.defineProperty(img, 'decode', {
      value: () => Promise.reject(new Error('broken')),
      configurable: true,
    })

    await expect(stampSvgFiguresAsync(w.parentElement)).resolves.toBeUndefined()
    // No size available — the figure keeps its default (shrink-only) sizing.
    expect(w.classList.contains(SVG_FIT_CLASS)).toBe(false)
  })

  it('resolves for a null root', async () => {
    await expect(stampSvgFiguresAsync(null)).resolves.toBeUndefined()
  })
})

describe('resting media width (svgMediaFit)', () => {
  const mkSvg = (vbW: number, vbH: number, inlineMax?: number) => {
    const wrapper = document.createElement('div')
    wrapper.className = 'image-block-wrapper'
    const cell = document.createElement('div')
    cell.className = 'mermaid'
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
    svg.setAttribute('viewBox', `0 0 ${vbW} ${vbH}`)
    if (inlineMax) svg.style.maxWidth = `${inlineMax}px`
    cell.appendChild(svg)
    wrapper.appendChild(cell)
    const column = document.createElement('div')
    column.appendChild(wrapper)
    document.body.appendChild(column)
    // jsdom has no layout; give the column a width the module can measure.
    Object.defineProperty(column, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 0, top: 0, left: 0, right: 800, bottom: 0, x: 0, y: 0, toJSON() {} }),
      configurable: true,
    })
    return { wrapper, svg, column }
  }

  afterEach(() => { document.body.innerHTML = '' })

  it('keeps a small diagram at its stated size instead of stretching it', () => {
    // "没有聚焦的时候容器宽度以元素实际宽度为准" — a 250px diagram must not be
    // blown up to the 800px column at rest; growing is the active state's job.
    const { wrapper, svg } = mkSvg(250, 70, 250)
    stampSvgFigure(wrapper)
    expect(parseFloat(svg.style.width)).toBe(250)
  })

  it('shrinks a diagram wider than the column down to the column', () => {
    const { wrapper, svg } = mkSvg(1600, 70, 1600)
    stampSvgFigure(wrapper)
    expect(parseFloat(svg.style.width)).toBeLessThanOrEqual(800)
  })

  it('ignores a percentage width attribute instead of reading it as pixels', () => {
    // mermaid writes `width="100%"`. `parseFloat('100%')` is 100, so treating
    // the attribute as a pixel count shrank the diagram to a 100px sliver.
    const { wrapper, svg } = mkSvg(1600, 70)
    svg.setAttribute('width', '100%')
    stampSvgFigure(wrapper)
    expect(parseFloat(svg.style.width)).not.toBe(100)
    // Falls through to the viewBox width, clamped to the column.
    expect(parseFloat(svg.style.width)).toBe(800)
  })

  it('writes a resolved pixel width, never a percentage', () => {
    // A percentage would be relative to the shrink-to-fit figure, which is sized
    // by this element — the cycle that collapses everything to 300px.
    const { wrapper, svg } = mkSvg(1600, 70, 1600)
    stampSvgFigure(wrapper)
    expect(svg.style.width).toMatch(/^[\d.]+px$/)
    expect(svg.style.width).not.toContain('%')
  })
})

/**
 * Portrait diagrams at rest.
 *
 * #570 reported a 302x1182 flowchart rendered as an unreadable sliver and the
 * obvious-looking fix is to drop the height budget from
 * `applyMediaWidth`'s `Math.min(intrinsic, avail, heightBudget * ar)`.
 *
 * That fix does NOT work, and the reason is geometry rather than code:
 * `media-block.css` caps the media at `max-height: 60dvh` with
 * `object-fit: contain`. Once the height is pinned, a 1:3.9 artwork's width is
 * forced — 60dvh * 0.255. Measured in Chromium against the real stylesheet
 * (viewport 900px, 800px column):
 *
 *   width from the budget (current): figure 138px, drawing 138px, gutters 0
 *   width from the column  (dropped): figure 302px, drawing 138px, gutters 82px
 *
 * The drawing is IDENTICAL; dropping the budget only adds 82px of empty frame
 * on each side — the "578px empty box" symptom the same issue complained about.
 * So the budget is load-bearing and these assertions pin it, to stop the next
 * reader from "fixing" it a third time.
 */
describe('portrait diagram resting width (height budget is intentional)', () => {
  /** 302x1182 is the flowchart from #570 — the case the whole debate is about. */
  const PORTRAIT_W = 302
  const PORTRAIT_H = 1182

  const mkPortrait = () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'image-block-wrapper'
    const cell = document.createElement('div')
    cell.className = 'mermaid'
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
    svg.setAttribute('viewBox', `0 0 ${PORTRAIT_W} ${PORTRAIT_H}`)
    cell.appendChild(svg)
    wrapper.appendChild(cell)
    const column = document.createElement('div')
    column.appendChild(wrapper)
    document.body.appendChild(column)
    Object.defineProperty(column, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 0, top: 0, left: 0, right: 800, bottom: 0, x: 0, y: 0, toJSON() {} }),
      configurable: true,
    })
    return { wrapper, svg, column }
  }

  const originalInnerHeight = window.innerHeight
  afterEach(() => {
    document.body.innerHTML = ''
    Object.defineProperty(window, 'innerHeight', { value: originalInnerHeight, configurable: true })
  })

  function setViewportHeight(h: number) {
    Object.defineProperty(window, 'innerHeight', { value: h, configurable: true })
  }

  it('sizes a tall diagram from the height budget, not the column', () => {
    // 900px viewport -> 60dvh = 540px -> 540 * (302/1182) = 138px.
    setViewportHeight(900)
    const { wrapper, svg } = mkPortrait()
    stampSvgFigure(wrapper)

    expect(parseFloat(svg.style.width)).toBeCloseTo(138, 0)
    // The column is 800px; taking it would leave ~82px of empty frame per side
    // while the artwork stayed exactly the same size.
    expect(parseFloat(svg.style.width)).toBeLessThan(800)
  })

  it('scales the resting width with the viewport, because the height cap drives it', () => {
    // This is the property that makes the budget necessary: the cap is on
    // HEIGHT, so the width must track it. If someone removes the budget both
    // viewports would produce the same 302px, which is what the assertions
    // below distinguish.
    setViewportHeight(600)
    const a = mkPortrait()
    stampSvgFigure(a.wrapper)
    const short = parseFloat(a.svg.style.width)
    document.body.innerHTML = ''

    setViewportHeight(1080)
    const b = mkPortrait()
    stampSvgFigure(b.wrapper)
    const tall = parseFloat(b.svg.style.width)

    expect(short).toBeLessThan(tall)
    // 600 * 0.6 * ar and 1080 * 0.6 * ar.
    expect(short).toBeCloseTo(600 * 0.6 * (PORTRAIT_W / PORTRAIT_H), 0)
    expect(tall).toBeCloseTo(1080 * 0.6 * (PORTRAIT_W / PORTRAIT_H), 0)
  })

  it('never exceeds the column for a tall diagram on a very tall viewport', () => {
    // At some point the budget-derived width would outgrow the column; the
    // column clamp still wins, so a portrait diagram can never overflow.
    setViewportHeight(4000)
    const { wrapper, svg } = mkPortrait()
    stampSvgFigure(wrapper)
    expect(parseFloat(svg.style.width)).toBeLessThanOrEqual(800)
  })

  it('keeps the frame hugging the media (no empty gutters at rest)', () => {
    // The reason the budget matters: at rest the figure is shrink-to-fit around
    // the media's resolved width, so the frame and the artwork have the SAME
    // width and there are no empty bands. jsdom cannot lay this out, so the
    // contract asserted here is the input to it — the svg carries a definite
    // pixel width equal to the budget-derived value, and the CSS that makes the
    // frame hug it is pinned by MarkdownPreviewSvgFit.css.test.ts.
    setViewportHeight(900)
    const { wrapper, svg } = mkPortrait()
    stampSvgFigure(wrapper)

    const written = parseFloat(svg.style.width)
    const budgetWidth = 900 * 0.6 * (PORTRAIT_W / PORTRAIT_H)
    // Within a pixel of the budget: no extra width for the frame to pad out.
    expect(Math.abs(written - budgetWidth)).toBeLessThan(1)
  })
})

describe('responsive refit (svgMediaFit)', () => {
  const mk = (vbW: number, vbH: number) => {
    const wrapper = document.createElement('div')
    wrapper.className = 'image-block-wrapper svg-fit'
    const cell = document.createElement('div')
    cell.className = 'mermaid'
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
    svg.setAttribute('viewBox', `0 0 ${vbW} ${vbH}`)
    cell.appendChild(svg)
    wrapper.appendChild(cell)
    const column = document.createElement('div')
    column.appendChild(wrapper)
    document.body.appendChild(column)
    let colW = 800
    Object.defineProperty(column, 'getBoundingClientRect', {
      value: () => ({ width: colW, height: 0, top: 0, left: 0, right: colW, bottom: 0, x: 0, y: 0, toJSON() {} }),
      configurable: true,
    })
    return { wrapper, svg, setColumnWidth: (w: number) => { colW = w } }
  }

  afterEach(() => { document.body.innerHTML = ''; vi.restoreAllMocks() })

  it('re-measures against the CURRENT column on refit', () => {
    // The width is written as a resolved pixel value (that is what breaks the
    // figure↔media cycle), so it is frozen at the moment it was written. Without
    // a refit, collapsing a side panel left the diagram at its old width.
    const { wrapper, svg, setColumnWidth } = mk(1600, 70)
    stampSvgFigure(wrapper)
    expect(parseFloat(svg.style.width)).toBe(800)

    setColumnWidth(500)
    refitFigure(wrapper)
    expect(parseFloat(svg.style.width)).toBe(500)

    setColumnWidth(1200)
    refitFigure(wrapper)
    expect(parseFloat(svg.style.width)).toBe(1200)
  })

  it('does not fight the zoom while a figure is active', () => {
    // `inlineZoom` owns the media size while zoomed; writing here would
    // overwrite the zoomed size on every mutation tick.
    const { wrapper, svg, setColumnWidth } = mk(1600, 70)
    stampSvgFigure(wrapper)
    wrapper.classList.add('is-zooming')
    svg.style.width = '3000px'

    setColumnWidth(400)
    refitFigure(wrapper)

    expect(svg.style.width).toBe('3000px')
  })

  it('ignores figures that were never stamped', () => {
    const { wrapper, svg } = mk(1600, 70)
    wrapper.classList.remove('svg-fit')
    svg.style.width = '123px'
    refitFigure(wrapper)
    expect(svg.style.width).toBe('123px')
  })
})

/**
 * `installSvgMediaAutoFit` wires the two signals that keep a stamped figure's
 * frozen pixel width correct: a ResizeObserver on each column, and a
 * MutationObserver for newly rendered figures.
 *
 * The defect these cases pin: the mutation path used to re-measure EVERY figure
 * in the document on every mutation. The chat transcript replaces innerHTML on
 * each streaming frame, so a session with a dozen diagrams paid a full-document
 * forced synchronous layout per frame — while only the newly added figure
 * actually needed a first measurement.
 */
describe('installSvgMediaAutoFit — scope of a refit pass', () => {
  /**
   * A stamped figure whose column records each measurement.
   *
   * refitFigure measures the COLUMN (the figure's parent), so the probe has to
   * sit there — a probe on the wrapper would never fire.
   */
  function mkFigure(vbW: number, vbH: number, colW = 800) {
    const wrapper = document.createElement('div')
    wrapper.className = 'image-block-wrapper svg-fit'
    const cell = document.createElement('div')
    cell.className = 'mermaid'
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
    svg.setAttribute('viewBox', `0 0 ${vbW} ${vbH}`)
    cell.appendChild(svg)
    wrapper.appendChild(cell)
    const column = document.createElement('div')
    column.appendChild(wrapper)
    document.body.appendChild(column)
    const measured: number[] = []
    Object.defineProperty(column, 'getBoundingClientRect', {
      value: () => {
        measured.push(1)
        return { width: colW, height: 0, top: 0, left: 0, right: colW, bottom: 0, x: 0, y: 0, toJSON() {} }
      },
      configurable: true,
    })
    return { wrapper, svg, column, measured }
  }

  /**
   * Capture rAF callbacks instead of running them, so a test can decide when a
   * coalesced pass executes. Returns a function that drains them.
   */
  function captureFrames() {
    const callbacks: FrameRequestCallback[] = []
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      callbacks.push(cb)
      return callbacks.length
    })
    return () => {
      const queued = callbacks.splice(0)
      for (const cb of queued) cb(0)
    }
  }

  /** MutationObserver delivers as a microtask; give it a turn. */
  const settle = () => new Promise((r) => setTimeout(r, 0))

  afterEach(() => {
    document.body.innerHTML = ''
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  function stubResizeObserver() {
    vi.stubGlobal('ResizeObserver', class {
      observe() {}
      disconnect() {}
    })
  }

  it('re-measures only the figures a mutation added', async () => {
    stubResizeObserver()
    const runFrames = captureFrames()

    const existing = mkFigure(1600, 70)
    stampSvgFigure(existing.wrapper)

    const stop = installSvgMediaAutoFit()
    await settle()
    runFrames()
    existing.measured.length = 0

    // A streaming render appends a NEW figure; the old one is untouched.
    const fresh = mkFigure(400, 70)
    stampSvgFigure(fresh.wrapper)
    await settle()
    runFrames()

    expect(existing.measured.length, 'the untouched figure must not be re-measured').toBe(0)
    expect(fresh.measured.length, 'the added figure must be measured').toBeGreaterThan(0)

    stop()
  })

  it('re-measures every figure on a viewport resize', async () => {
    stubResizeObserver()
    const runFrames = captureFrames()

    const a = mkFigure(1600, 70)
    const b = mkFigure(1600, 70)
    stampSvgFigure(a.wrapper)
    stampSvgFigure(b.wrapper)

    const stop = installSvgMediaAutoFit()
    await settle()
    runFrames()
    a.measured.length = 0
    b.measured.length = 0

    // The 60dvh height budget changes with the viewport, so every figure needs
    // a new width — not just the ones the observer happened to name.
    window.dispatchEvent(new Event('resize'))
    runFrames()

    expect(a.measured.length).toBeGreaterThan(0)
    expect(b.measured.length).toBeGreaterThan(0)

    stop()
  })

  it('does nothing on a mutation that adds no figure', async () => {
    stubResizeObserver()
    const runFrames = captureFrames()

    const existing = mkFigure(1600, 70)
    stampSvgFigure(existing.wrapper)

    const stop = installSvgMediaAutoFit()
    await settle()
    runFrames()
    existing.measured.length = 0

    // The common streaming case: text is appended into an existing node.
    const para = document.createElement('p')
    para.textContent = 'streaming text'
    document.body.appendChild(para)
    await settle()
    runFrames()

    expect(existing.measured.length, 'a text-only mutation must not trigger a refit').toBe(0)

    stop()
  })

  it('stops observing after teardown', async () => {
    stubResizeObserver()
    const runFrames = captureFrames()

    const fig = mkFigure(1600, 70)
    stampSvgFigure(fig.wrapper)

    const stop = installSvgMediaAutoFit()
    await settle()
    runFrames()
    fig.measured.length = 0
    stop()

    window.dispatchEvent(new Event('resize'))
    runFrames()

    expect(fig.measured.length).toBe(0)
  })
})
