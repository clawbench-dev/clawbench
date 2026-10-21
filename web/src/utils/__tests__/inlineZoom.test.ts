import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  MAX_SCALE,
  MIN_SCALE,
  activeZoomScale,
  installInlineZoom,
  isZoomActive,
  resetActiveZoom,
  uninstallInlineZoom,
  zoomableMedia,
} from '@/utils/inlineZoom'

/**
 * Inline zoom for media figures (pointer devices only).
 *
 * jsdom cannot evaluate transforms or `(hover: hover) and (pointer: fine)`, so
 * the suite drives the real listeners with synthetic events and asserts on the
 * DOM the module writes (marker classes + inline transform). The pointer-device
 * gate is stubbed per test — that gate is the single most important behaviour
 * here, because a touch device must NEVER get inline zoom (it would fight page
 * scrolling).
 */

/** Build a figure the way the media-block factory does, inside a document. */
function mountFigure(kind: 'img' | 'svg' | 'mermaid' = 'img'): HTMLElement {
  const figure = document.createElement('div')
  figure.className = 'image-block-wrapper'
  const header = document.createElement('div')
  header.className = 'image-block-header'
  header.innerHTML = '<span class="image-block-header-actions">'
    + '<button class="image-block-view-btn"></button>'
    + '<button class="image-block-reset-btn"></button>'
    + '</span>'
  figure.appendChild(header)

  if (kind === 'mermaid') {
    figure.insertAdjacentHTML('beforeend', '<div class="mermaid"><svg viewBox="0 0 302 1182"></svg></div>')
  } else if (kind === 'svg') {
    figure.insertAdjacentHTML('beforeend', '<span class="lightbox-svg-wrap"><svg class="lightbox-svg" viewBox="0 0 400 100"></svg></span>')
  } else {
    figure.insertAdjacentHTML('beforeend', '<span class="lightbox-img-wrap"><img class="lightbox-img" src="a.png"></span>')
  }
  document.body.appendChild(figure)
  return figure
}

/** jsdom reports 0 for offset sizes; give the media a measurable box. */
function stubSize(el: Element, w: number, h: number): void {
  Object.defineProperty(el, 'offsetWidth', { value: w, configurable: true })
  Object.defineProperty(el, 'offsetHeight', { value: h, configurable: true })
}

/**
 * jsdom has no layout, so the module's contain-fit maths (which reads the
 * intrinsic size and the cell box) would divide by zero. Give the media its
 * intrinsic size and the cell a real box.
 */
function stubIntrinsic(figure: HTMLElement, natW: number, natH: number, cellW = 300, cellH = 200): void {
  const media = figure.querySelector('img, svg') as HTMLElement
  if (media.tagName === 'IMG') {
    Object.defineProperty(media, 'naturalWidth', { value: natW, configurable: true })
    Object.defineProperty(media, 'naturalHeight', { value: natH, configurable: true })
  } else {
    Object.defineProperty(media, 'viewBox', {
      value: { baseVal: { width: natW, height: natH } },
      configurable: true,
    })
  }
  const cell = media.parentElement!
  stubRect(cell, 0, 0, cellW, cellH)
}

/** jsdom returns a zero rect for every element; give the figure a real box. */
function stubRect(el: Element, left: number, top: number, width: number, height: number): void {
  vi.spyOn(el, 'getBoundingClientRect').mockReturnValue({
    left, top, width, height, right: left + width, bottom: top + height, x: left, y: top,
    toJSON: () => ({}),
  } as DOMRect)
}

/** Pretend the device has a hovering precise pointer. */
function withPointerDevice(matches = true): void {
  vi.stubGlobal('matchMedia', (q: string) => ({
    matches,
    media: q,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    onchange: null,
    dispatchEvent: () => false,
  }))
}

function wheel(target: Element, deltaY: number, clientX = 0, clientY = 0): void {
  const e = new WheelEvent('wheel', { deltaY, clientX, clientY, bubbles: true, cancelable: true })
  target.dispatchEvent(e)
}

function click(target: Element): void {
  target.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
}

function pointer(type: string, target: Element, clientX = 0, clientY = 0, extra: PointerEventInit = {}): void {
  const e = new PointerEvent(type, {
    clientX, clientY, bubbles: true, cancelable: true, pointerType: 'mouse', button: 0, ...extra,
  })
  target.dispatchEvent(e)
}

describe('inlineZoom — device gating', () => {
  beforeEach(() => { document.body.innerHTML = '' })
  afterEach(() => { uninstallInlineZoom(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

  it('never installs on a touch device', () => {
    // This is the load-bearing guarantee: touch must keep page scrolling and
    // use the lightbox instead. A regression here would make every diagram
    // swallow swipe gestures.
    //
    // Driven through the CLICK path, which is the only way into the zoom state:
    // asserting on the wheel alone would pass even with the device gate removed
    // (a resting figure ignores the wheel regardless), i.e. it could not tell a
    // gated build from an ungated one.
    withPointerDevice(false)
    installInlineZoom()

    const figure = mountFigure('mermaid')
    const svg = figure.querySelector('svg')!
    stubSize(svg, 300, 1000)
    stubRect(figure, 0, 0, 300, 400)

    click(svg)

    expect(isZoomActive()).toBe(false)
    expect(figure.classList.contains('is-zooming')).toBe(false)
    // And the wheel stays the page's, too.
    const e = new WheelEvent('wheel', { deltaY: -120, bubbles: true, cancelable: true })
    svg.dispatchEvent(e)
    expect(e.defaultPrevented).toBe(false)
  })

  it('enters the zoom state on click', () => {
    withPointerDevice(true)
    installInlineZoom()

    const figure = mountFigure('mermaid')
    const svg = figure.querySelector('svg')!
    stubSize(svg, 300, 1000)
    stubRect(figure, 0, 0, 300, 400)

    click(svg)

    expect(isZoomActive()).toBe(true)
    expect(figure.classList.contains('is-zooming')).toBe(true)
  })
})

describe('inlineZoom — wheel behaviour', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    withPointerDevice(true)
    installInlineZoom()
  })
  afterEach(() => { uninstallInlineZoom(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

  it('leaves the page wheel alone in BOTH directions while at rest', () => {
    // The load-bearing guarantee of click-to-enter. Hover-activation shipped
    // first and hijacked upward scrolling: with the pointer drifting over a
    // diagram, scrolling back up the document zoomed the diagram instead, and
    // trackpad momentum kept multiplying the scale. At rest the figure owns no
    // wheel events at all.
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubSize(img, 300, 200)
    stubRect(figure, 0, 0, 300, 200)

    for (const deltaY of [120, -120]) {
      const e = new WheelEvent('wheel', { deltaY, bubbles: true, cancelable: true })
      img.dispatchEvent(e)
      expect(e.defaultPrevented, `deltaY=${deltaY} must scroll the page`).toBe(false)
      expect(isZoomActive()).toBe(false)
    }
  })

  it('zooms in on wheel-up and prevents the page from scrolling', () => {
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubSize(img, 300, 200)
    stubRect(figure, 0, 0, 300, 200)
    click(img)

    const e = new WheelEvent('wheel', { deltaY: -120, bubbles: true, cancelable: true })
    img.dispatchEvent(e)

    expect(e.defaultPrevented).toBe(true)
    expect(activeZoomScale()).toBeGreaterThan(MIN_SCALE)
    // Zoom is LAYOUT sizing, not `transform: scale()`: a transform rasterises
    // the media once and stretches the pixels, so a diagram zoomed 4x turns
    // blurry. The inline width/height must be what grows.
    expect(img.style.width).not.toBe('')
    expect(img.style.height).not.toBe('')
    expect(img.style.transform).not.toContain('scale(')
  })

  it('clamps to MAX_SCALE instead of growing without bound', () => {
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubSize(img, 300, 200)
    stubRect(figure, 0, 0, 300, 200)

    click(img)
    for (let i = 0; i < 40; i++) wheel(img, -120, 150, 100)

    expect(activeZoomScale()).toBe(MAX_SCALE)
  })

  it('drops the zoom and keeps the notch when zooming out at fit scale', () => {
    // The gesture has to end somewhere: swallowing the notch avoids a page jump
    // mid-zoom, and the next notch scrolls normally.
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubSize(img, 300, 200)
    stubRect(figure, 0, 0, 300, 200)
    click(img)

    wheel(img, -120, 150, 100)
    expect(isZoomActive()).toBe(true)

    const out = new WheelEvent('wheel', { deltaY: 120, bubbles: true, cancelable: true })
    img.dispatchEvent(out)
    expect(out.defaultPrevented).toBe(true)
    expect(isZoomActive()).toBe(false)
    expect(img.style.transform).toBe('')

    // The next notch is a plain page scroll.
    const after = new WheelEvent('wheel', { deltaY: 120, bubbles: true, cancelable: true })
    img.dispatchEvent(after)
    expect(after.defaultPrevented).toBe(false)
  })

  it('zooms around the cursor, not the centre', () => {
    // Anchoring on the pointer is what makes "zoom into that node" feel direct.
    // Zooming at the far right must shift the media left relative to zooming at
    // the centre — otherwise the anchor is ignored.
    const mk = () => {
      const figure = mountFigure()
      const img = figure.querySelector('img')!
      stubIntrinsic(figure, 300, 200, 300, 200)
      stubRect(figure, 0, 0, 300, 200)
      click(img)
      return img
    }
    const txOf = (el: HTMLElement) => {
      const m = el.style.transform.match(/calc\(-50% \+ (-?[\d.]+)px\)/)
      return m ? parseFloat(m[1]) : NaN
    }

    const atCentre = mk()
    wheel(atCentre, -120, 150, 100)
    const centreTx = txOf(atCentre)

    const atRight = mk()
    wheel(atRight, -120, 290, 100)
    const rightTx = txOf(atRight)

    expect(Number.isNaN(centreTx)).toBe(false)
    expect(Number.isNaN(rightTx)).toBe(false)
    // Zooming near the right edge pulls the content left relative to centre.
    expect(rightTx).toBeLessThan(centreTx)
  })
})

describe('inlineZoom — panning', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    withPointerDevice(true)
    installInlineZoom()
  })
  afterEach(() => { uninstallInlineZoom(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

  it('does not pan at fit scale (nothing to move)', () => {
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubSize(img, 300, 200)
    stubRect(figure, 0, 0, 300, 200)

    pointer('pointerdown', img, 100, 100)
    pointer('pointermove', img, 140, 140)

    expect(img.style.transform).toBe('')
    expect(figure.classList.contains('is-panning')).toBe(false)
  })

  it('pans while zoomed, clamping so the media cannot leave the frame', () => {
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubIntrinsic(figure, 300, 200, 300, 200)
    stubRect(figure, 0, 0, 300, 200)
    click(img)

    wheel(img, -120, 150, 100)
    const before = img.style.transform

    pointer('pointerdown', img, 100, 100)
    pointer('pointermove', img, 140, 140)
    expect(figure.classList.contains('is-panning')).toBe(true)
    expect(img.style.transform).not.toBe(before)

    // A huge drag is clamped to the overflow, not applied verbatim. The media
    // must never be draggable out of the frame.
    pointer('pointermove', img, 99999, 99999)
    const tx = parseFloat(img.style.transform.match(/calc\(-50% \+ (-?[\d.]+)px\)/)![1])
    const scaledW = parseFloat(img.style.width)
    const maxX = Math.max(0, (scaledW - 300) / 2)
    expect(Math.abs(tx)).toBeLessThanOrEqual(maxX + 0.5)
  })

  it('stops panning on pointerup', () => {
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubSize(img, 300, 200)
    stubRect(figure, 0, 0, 300, 200)

    wheel(img, -120, 150, 100)
    pointer('pointerdown', img, 100, 100)
    pointer('pointermove', img, 140, 140)
    pointer('pointerup', img, 140, 140)

    expect(figure.classList.contains('is-panning')).toBe(false)
  })
})

describe('inlineZoom — activation lifecycle', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    withPointerDevice(true)
    installInlineZoom()
  })
  afterEach(() => { uninstallInlineZoom(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

  it('drops the zoom when the pointer leaves the figure', () => {
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubSize(img, 300, 200)
    stubRect(figure, 0, 0, 300, 200)
    click(img)

    wheel(img, -120, 150, 100)
    expect(isZoomActive()).toBe(true)

    // `relatedTarget: null` means the pointer went somewhere outside the
    // figure (another element or out of the window).
    const out = new PointerEvent('pointerout', {
      bubbles: true, pointerType: 'mouse', clientX: 400, clientY: 400, relatedTarget: null,
    })
    img.dispatchEvent(out)

    expect(isZoomActive()).toBe(false)
    expect(figure.classList.contains('is-zooming')).toBe(false)
    expect(img.style.transform).toBe('')
  })

  it('keeps the zoom when a finished pan leaves the pointer inside the figure', () => {
    // A drag delivers `pointerup` (ending the pan) and then `pointerout` with a
    // `relatedTarget` outside the figure, even though the pointer is still
    // geometrically inside it. Treating that as "left the figure" would undo
    // the pan the user just made.
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubIntrinsic(figure, 300, 200, 300, 200)
    stubRect(figure, 0, 0, 300, 200)
    click(img)
    wheel(img, -120, 150, 100)

    pointer('pointerdown', img, 100, 100)
    pointer('pointermove', img, 160, 140)
    pointer('pointerup', img, 160, 140)

    // Pointer is at (160,140) — inside the 300x200 figure at (0,0).
    const out = new PointerEvent('pointerout', {
      bubbles: true, pointerType: 'mouse', clientX: 160, clientY: 140, relatedTarget: null,
    })
    img.dispatchEvent(out)

    expect(isZoomActive()).toBe(true)
    expect(figure.classList.contains('is-zooming')).toBe(true)
  })

  it('does not hijack wheel events over the floating toolbar', () => {
    // The toolbar carries the view/reset buttons; zooming it would make the
    // buttons unreachable.
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubSize(img, 300, 200)
    stubRect(figure, 0, 0, 300, 200)

    const btn = figure.querySelector('.image-block-view-btn')!
    const e = new WheelEvent('wheel', { deltaY: -120, bubbles: true, cancelable: true })
    btn.dispatchEvent(e)

    expect(e.defaultPrevented).toBe(false)
    expect(isZoomActive()).toBe(false)
  })

  it('keeps only one figure zoomed at a time', () => {
    const a = mountFigure()
    const b = mountFigure()
    const imgA = a.querySelector('img')!
    const imgB = b.querySelector('img')!
    stubSize(imgA, 300, 200)
    stubSize(imgB, 300, 200)
    stubRect(a, 0, 0, 300, 200)
    stubRect(b, 0, 300, 300, 200)

    click(imgA)
    wheel(imgA, -120, 150, 100)
    expect(a.classList.contains('is-zooming')).toBe(true)

    click(imgB)
    wheel(imgB, -120, 150, 400)
    expect(b.classList.contains('is-zooming')).toBe(true)
    // Starting a second zoom must release the first.
    expect(a.classList.contains('is-zooming')).toBe(false)
    expect(imgA.style.transform).toBe('')
  })

  it('leaves the zoom when clicking outside the figure', () => {
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubSize(img, 300, 200)
    stubRect(figure, 0, 0, 300, 200)

    click(img)
    expect(isZoomActive()).toBe(true)

    const outside = document.createElement('div')
    document.body.appendChild(outside)
    click(outside)

    expect(isZoomActive()).toBe(false)
    expect(img.style.transform).toBe('')
  })

  it('does not leave the zoom when clicking the figure again', () => {
    // Once zoomed, a press on the media belongs to the pan gesture; toggling on
    // click would make every pan attempt also exit the zoom.
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubSize(img, 300, 200)
    stubRect(figure, 0, 0, 300, 200)

    click(img)
    click(img)

    expect(isZoomActive()).toBe(true)
  })

  it('keeps the zoom when clicking the figure toolbar', () => {
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubSize(img, 300, 200)
    stubRect(figure, 0, 0, 300, 200)

    click(img)
    click(figure.querySelector('.image-block-view-btn')!)

    expect(isZoomActive()).toBe(true)
  })

  it('makes a figure focusable on first pointer-over (keyboard path)', () => {
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    expect(figure.hasAttribute('tabindex')).toBe(false)

    pointer('pointerover', img, 10, 10)

    expect(figure.getAttribute('tabindex')).toBe('0')
  })
})

describe('inlineZoom — layout reset', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    withPointerDevice(true)
    installInlineZoom()
  })
  afterEach(() => { uninstallInlineZoom(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

  it('returns the active figure to fit scale and centres it', () => {
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubIntrinsic(figure, 300, 200, 300, 200)
    stubRect(figure, 0, 0, 300, 200)
    click(img)

    wheel(img, -120, 150, 100)
    wheel(img, -120, 150, 100)
    expect(activeZoomScale()).toBeGreaterThan(MIN_SCALE)

    const resetBtn = figure.querySelector('.image-block-reset-btn')!
    resetBtn.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))

    expect(activeZoomScale()).toBe(MIN_SCALE)
    expect(img.style.transform).toContain('calc(-50% + 0px)')
    // At scale 1 the layout size equals the contain-fit size.
    expect(parseFloat(img.style.width)).toBeCloseTo(300, 0)
    // The zoom stays engaged so the user can immediately zoom again.
    expect(figure.classList.contains('is-zooming')).toBe(true)
  })

  it('reports false when nothing is zoomed', () => {
    expect(resetActiveZoom()).toBe(false)
  })
})

describe('inlineZoom — keyboard', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    withPointerDevice(true)
    installInlineZoom()
  })
  afterEach(() => { uninstallInlineZoom(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

  it('zooms with + / - and returns to fit with Escape', () => {
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubSize(img, 300, 200)
    stubRect(figure, 0, 0, 300, 200)
    figure.setAttribute('tabindex', '0')

    figure.dispatchEvent(new KeyboardEvent('keydown', { key: '+', bubbles: true, cancelable: true }))
    expect(activeZoomScale()).toBeGreaterThan(MIN_SCALE)

    figure.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(isZoomActive()).toBe(false)
    expect(img.style.transform).toBe('')
  })

  it('exits on Escape even when focus is on the document body', () => {
    // The user clicks the media to enter (the media is not focusable by
    // default), so the keydown target is usually <body> — not the figure.
    // Keying Escape off the event target would make it a no-op exactly when the
    // user needs it.
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubIntrinsic(figure, 300, 200, 300, 200)
    stubRect(figure, 0, 0, 300, 200)
    click(img)
    expect(isZoomActive()).toBe(true)

    const e = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    document.body.dispatchEvent(e)

    expect(e.defaultPrevented).toBe(true)
    expect(isZoomActive()).toBe(false)
  })

  it('lets Escape through when nothing is zoomed', () => {
    // Escape may belong to an enclosing overlay (lightbox, drawer); swallowing
    // it here would make those unclosable.
    const figure = mountFigure()
    const e = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    figure.dispatchEvent(e)

    expect(e.defaultPrevented).toBe(false)
  })
})

describe('inlineZoom — layout contract', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    withPointerDevice(true)
    installInlineZoom()
  })
  afterEach(() => { uninstallInlineZoom(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

  it('scales by layout size, never by transform (keeps vector art sharp)', () => {
    // `transform: scale()` rasterises once at the layout size and stretches the
    // pixels, so a diagram zoomed 4x is blurry — the exact complaint this
    // feature exists to fix. The inline width/height must carry the zoom.
    const figure = mountFigure('mermaid')
    const svg = figure.querySelector('svg')!
    stubIntrinsic(figure, 300, 1000, 300, 400)
    stubRect(figure, 0, 0, 300, 400)
    click(svg)

    wheel(svg, -120, 150, 200)
    wheel(svg, -120, 150, 200)

    const scale = activeZoomScale()
    expect(scale).toBeGreaterThan(1.3)
    // Layout size grew by the scale factor on top of the contain-fit size.
    expect(parseFloat(svg.style.width)).toBeGreaterThan(100)
    expect(svg.style.transform).not.toContain('scale(')
  })

  it('restores the resting layout exactly on exit', () => {
    // "布局恢复原状" — every inline size the zoom wrote must be cleared, or the
    // media would stay at a stale pixel size after leaving the active state.
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubIntrinsic(figure, 300, 200, 300, 200)
    stubRect(figure, 0, 0, 300, 200)

    click(img)
    wheel(img, -120, 150, 100)
    wheel(img, -120, 150, 100)
    expect(img.style.width).not.toBe('')

    click(document.body)

    expect(isZoomActive()).toBe(false)
    expect(img.style.width).toBe('')
    expect(img.style.height).toBe('')
    expect(img.style.maxWidth).toBe('')
    expect(img.style.maxHeight).toBe('')
    expect(img.style.transform).toBe('')
    expect(figure.classList.contains('is-zooming')).toBe(false)
  })

  it('marks the active figure with the state class so CSS can ring it', () => {
    const figure = mountFigure()
    const img = figure.querySelector('img')!
    stubIntrinsic(figure, 300, 200, 300, 200)
    stubRect(figure, 0, 0, 300, 200)

    expect(figure.classList.contains('is-zooming')).toBe(false)
    click(img)
    expect(figure.classList.contains('is-zooming')).toBe(true)
  })

  it('never lets a pan move the media out of the frame on an axis that fits', () => {
    // A diagram narrower than the frame cannot move horizontally at all —
    // otherwise it could be dragged off-screen leaving an empty box.
    const figure = mountFigure('mermaid')
    const svg = figure.querySelector('svg')!
    // Tall and narrow: fits horizontally, overflows vertically once zoomed.
    stubIntrinsic(figure, 100, 1000, 300, 200)
    stubRect(figure, 0, 0, 300, 200)
    click(svg)
    wheel(svg, -120, 150, 100)

    pointer('pointerdown', svg, 150, 100)
    pointer('pointermove', svg, 9999, 100)

    const m = svg.style.transform.match(/calc\(-50% \+ (-?[\d.]+)px\)/)
    const tx = m ? parseFloat(m[1]) : NaN
    expect(tx).toBe(0)
  })
})

describe('zoomableMedia', () => {
  afterEach(() => { document.body.innerHTML = '' })

  it('resolves each of the three content-media shapes', () => {
    expect(zoomableMedia(mountFigure('img'))?.tagName).toBe('IMG')
    expect(zoomableMedia(mountFigure('svg'))?.tagName).toBe('svg')
    expect(zoomableMedia(mountFigure('mermaid'))?.tagName).toBe('svg')
  })

  it('ignores the toolbar icon svg', () => {
    const figure = mountFigure('img')
    // The header's own icon must never be mistaken for content media.
    figure.querySelector('.image-block-header')!.innerHTML =
      '<svg viewBox="0 0 24 24"></svg><span class="image-block-header-actions"></span>'
    expect(zoomableMedia(figure)?.tagName).toBe('IMG')
  })

  it('returns null for a figure with no media', () => {
    const figure = document.createElement('div')
    figure.className = 'image-block-wrapper'
    figure.innerHTML = '<div class="image-block-header"></div>'
    document.body.appendChild(figure)
    expect(zoomableMedia(figure)).toBeNull()
  })

  it('returns null for null input', () => {
    expect(zoomableMedia(null)).toBeNull()
  })
})
