import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Layout contract for a meeting key frame (`.image-block-wrapper--thumb`).
 *
 * The frame must END where the picture ends. It did not: `width: fit-content`
 * does not follow the media's cap, because that cap carries a percentage
 * (`min(100%, 260dp)`) which is indefinite while the frame is being sized from
 * its content. The browser then resolves the cell↔figure cycle to an
 * intermediate width — measured 267dp for a 480px thumb, 282px on the real
 * share page — so the frame ran on for 22–28px past the picture and left a dead
 * band to its right, with the floating view button sitting inside it.
 *
 * The fix restates the media cap on the frame (`+ 2px` for its own borders).
 * These are source-contract checks: jsdom neither resolves `min()`/`calc()` with
 * `var(--dp)` nor measures a laid-out box, so the numbers have to be read out of
 * the stylesheet.
 */

/** Strip comments so prose about a property is never mistaken for the property. */
const decls = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '')

/** The rule that sizes the FRAME (both selectors of the thumb variant). */
const frameRule = decls(readWebFile('css/media-block.css')).match(
  /\.image-block-wrapper--thumb,\s*\n\.image-block-wrapper--thumb:has\(> \.lightbox-img-wrap > img\)\s*\{([\s\S]*?)\}/,
)

/** The rule that sizes the MEDIA inside it. */
const mediaRule = decls(readWebFile('css/media-block.css')).match(
  /\.image-block-wrapper--thumb > \.lightbox-img-wrap > img,\s*\n\.image-block-wrapper--thumb:has\(> \.lightbox-img-wrap > img\) > \.lightbox-img-wrap > img\s*\{([\s\S]*?)\}/,
)

/** The design-pixel number inside `calc(N * var(--dp))` / `min(…, calc(N * …))`. */
function dpValue(decl: string, prop: string): number | null {
  const m = decl.match(new RegExp(`${prop}:\\s*([^;]+);`))
  if (!m) return null
  const n = m[1].match(/calc\(\s*(\d+(?:\.\d+)?)\s*\*\s*var\(--dp\)/)
  return n ? Number(n[1]) : null
}

describe('meeting key frame: the frame hugs the thumbnail', () => {
  it('declares both rules where the layout reads them', () => {
    expect(frameRule, 'thumb frame rule not found').toBeTruthy()
    expect(mediaRule, 'thumb media rule not found').toBeTruthy()
  })

  it('caps the frame at the media width, not at the column', () => {
    // `max-width: 100%` is the regression: it lets the frame run to whatever
    // width the cell↔figure cycle resolves to, which is wider than the capped
    // picture. The cap must be a length derived from the media's own cap.
    const frame = frameRule![1]
    expect(frame).not.toMatch(/max-width:\s*100%;/)
    expect(frame, 'frame must clamp to the media cap').toMatch(
      /max-width:\s*min\(\s*100%,\s*calc\([^)]*var\(--dp\)[^)]*\)\s*\);/,
    )
  })

  it('uses the SAME width number as the media cap, plus the frame borders', () => {
    // Drift here is the whole bug: change the thumbnail width and the frame
    // silently stops hugging it again.
    const mediaW = dpValue(mediaRule![1], 'max-width')
    const frameW = dpValue(frameRule![1], 'max-width')
    expect(mediaW, 'media width cap must be a dp length').toBeTypeOf('number')
    expect(frameW, 'frame width cap must be a dp length').toBe(mediaW)

    // `box-sizing: border-box` (css/base.css): the frame's own 1px borders live
    // inside its max-width, so the cap has to grow by exactly those two pixels.
    expect(frameRule![1]).toMatch(/max-width:\s*min\(\s*100%,\s*calc\(\s*260\s*\*\s*var\(--dp\)\s*\+\s*2px\s*\)\s*\);/)
  })

  it('still caps the thumbnail itself (width guard + height budget)', () => {
    const media = mediaRule![1]
    expect(media).toContain('display: block')
    expect(media).toContain('width: auto')
    // Width: secondary guard for very wide captures; height: the primary budget
    // so a portrait frame does not become a tall block.
    expect(dpValue(media, 'max-width')).toBe(260)
    expect(dpValue(media, 'max-height')).toBe(150)
    // The percentage must stay: it is what keeps a narrow column from
    // overflowing. Removing it (to make `fit-content` behave) would crop the
    // picture instead.
    expect(media).toMatch(/max-width:\s*min\(\s*100%,\s*calc\(\s*260\s*\*\s*var\(--dp\)\s*\)\s*\);/)
  })

  it('keeps the frame shrink-to-fit and flush', () => {
    const frame = frameRule![1]
    expect(frame).toContain('width: fit-content')
    // The figure is a block in a flex column; a stray margin would push it off
    // the row's text baseline.
    expect(frame).toContain('margin: 0')
  })

  it('drops BOTH caps once the reader zooms, so the frame spans the column', () => {
    // Same behaviour markdown preview has: `.is-zooming { width: 100% }` turns
    // the figure into the pan viewport. The thumbnail caps above are resting
    // sizes and would otherwise clamp the enlarged picture back to 260x150dp
    // and leave the frame 262px wide with nowhere to pan to.
    //
    // The media override must carry the `:has()` shape: the resting thumb rule
    // is 0-3-2, so the generic `.is-zooming > .lightbox-img-wrap > img`
    // (0-3-1) loses to it.
    const all = decls(readWebFile('css/media-block.css'))
    const frameZoom = all.match(
      /\.image-block-wrapper--thumb\.is-zooming,\s*\n\.image-block-wrapper--thumb\.is-zooming:has\(> \.lightbox-img-wrap > img\)\s*\{([\s\S]*?)\}/,
    )
    expect(frameZoom, 'zoomed thumb frame rule not found').toBeTruthy()
    expect(frameZoom![1]).toContain('max-width: 100%')

    const mediaZoom = all.match(
      /\.image-block-wrapper--thumb\.is-zooming > \.lightbox-img-wrap > img,\s*\n\.image-block-wrapper--thumb\.is-zooming:has\(> \.lightbox-img-wrap > img\) > \.lightbox-img-wrap > img\s*\{([\s\S]*?)\}/,
    )
    expect(mediaZoom, 'zoomed thumb media rule not found').toBeTruthy()
    expect(mediaZoom![1]).toContain('max-width: none')
    expect(mediaZoom![1]).toContain('max-height: none')
  })
})
