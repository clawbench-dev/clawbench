import { describe, expect, it } from 'vitest'
import {
  SVG_FIT_CLASS,
  SVG_AR_PROP,
  isSvgImageSrc,
  svgMediaAspectRatio,
  stampSvgFigure,
  stampSvgFigures,
  stampSvgFiguresAsync,
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
