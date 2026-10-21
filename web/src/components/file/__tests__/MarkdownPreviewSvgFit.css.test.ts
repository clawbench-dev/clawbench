import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Layout contract for SVG media in rendered markdown (file preview, share SPA,
 * HTML export, chat).
 *
 * An `.image-block-wrapper` is `width: fit-content` by default, so SVG media
 * used to render at its intrinsic size and leave the reading column mostly
 * empty. Two successive designs were tried and the SECOND one is the contract:
 *
 *  A. `width: min(100%, 60dvh × aspect-ratio)` — sized the frame by the
 *     artwork. Wrong: a portrait diagram shrank the frame to a narrow strip,
 *     so the column was mostly empty AND that empty space could not be used
 *     for zooming.
 *  B. **The frame is the viewport**: the figure is always the full column
 *     width, the media is contain-fitted and centred inside a 60dvh box, and
 *     the zoom grows the media into the empty space around it. This is the
 *     current contract.
 *
 * The invariants below are the ones that regress silently:
 *  1. The figure must be `width: 100%` — not the ratio formula.
 *  2. The media must be CENTRED (`top/left: 50%` + `translate(-50%,-50%)`), not
 *     merely `max-width`-constrained, or it would sit flush left in a wide
 *     frame.
 *  3. The zoom must be applied as LAYOUT size, never `transform: scale()`,
 *     because a transform rasterises once and stretches the pixels — a diagram
 *     zoomed 4x would be blurry, which is the whole complaint.
 *
 * These are source-contract checks: jsdom cannot evaluate `min()`/`calc()` with
 * `dvh`, cannot resolve an aspect ratio, and cannot measure a rendered box.
 */
describe('svg media layout (media-block.css contract)', () => {
  const css = readWebFile('css/media-block.css')

  /** The rule that sizes the figure itself. */
  const figureRule = css.match(/\.image-block-wrapper\.svg-fit\s*\{([\s\S]*?)\}/)

  /** The rule that establishes the 60dvh viewport (the content cell). */
  const cellRule = css.match(
    /\.image-block-wrapper:has\(> \.lightbox-img-wrap > img\) > \.lightbox-img-wrap,[\s\S]*?\{([\s\S]*?)\}/,
  )

  /** The rule that sizes and centres the media itself. */
  const mediaRule = css.match(
    /\.image-block-wrapper:has\(> \.lightbox-img-wrap > img\) > \.lightbox-img-wrap > img,[\s\S]*?\{([\s\S]*?)\}/,
  )

  it('makes the figure the full reading column, not the artwork width', () => {
    // Design A (`min(100%, calc(60dvh * var(--svg-ar, 1)))`) must not come back:
    // it shrank a portrait diagram's FRAME to ~110px, leaving the column empty
    // and giving the zoom no room to grow into.
    expect(figureRule).toBeTruthy()
    expect(figureRule![1]).toContain('width: 100%')
    expect(figureRule![1]).not.toContain('min(')
    expect(figureRule![1]).not.toContain('--svg-ar')
  })

  it('gives the content cell a fixed 60dvh viewport', () => {
    expect(cellRule).toBeTruthy()
    expect(cellRule![1]).toContain('height: 60dvh')
    expect(cellRule![1]).toContain('overflow: hidden')
    // The media is absolutely positioned against this box.
    expect(cellRule![1]).toContain('position: relative')
  })

  it('contain-fits and CENTRES the media inside the viewport', () => {
    expect(mediaRule).toBeTruthy()
    const rule = mediaRule![1]
    expect(rule).toContain('max-width: 100%')
    expect(rule).toContain('max-height: 100%')
    expect(rule).toContain('object-fit: contain')
    // Centring: `inset: 0` + `margin: auto` forces the box to the container
    // size instead of keeping the media's own, so the percentage-translate form
    // is the one that works for a box both smaller (resting) and larger
    // (zoomed) than the frame.
    expect(rule).toContain('top: 50%')
    expect(rule).toContain('left: 50%')
    expect(rule).toContain('translate(-50%, -50%)')
    // `inset: 0` would pin all four edges and force the box to the container
    // size. The assertion ignores the explanatory comment above the rule.
    expect(rule.replace(/\/\*[\s\S]*?\*\//g, '')).not.toContain('inset: 0')
  })

  it('never applies the ratio-driven width anywhere in the sheet', () => {
    // Regression guard for design A. A plain `width: 100%` elsewhere (e.g. the
    // table row modal's thumbnails) is unrelated and allowed.
    // Comments legitimately mention the abandoned formula; only real
    // declarations matter.
    const declarations = css.replace(/\/\*[\s\S]*?\*\//g, '')
    expect(declarations).not.toContain('60dvh * var(--svg-ar')
    expect(declarations).not.toContain('--svg-ar')
  })

  it('zooms with layout size, never with transform: scale()', () => {
    // The state rules must not introduce a scale transform: `inlineZoom.ts`
    // writes width/height and only ever uses `translate` for the pan.
    const zoomRules = [...css.matchAll(/\.image-block-wrapper\.is-zooming[^{]*\{([\s\S]*?)\}/g)]
      .map(m => m[1])
    expect(zoomRules.length).toBeGreaterThan(0)
    for (const body of zoomRules) {
      expect(body, 'zoom must not scale the media').not.toMatch(/scale\(/)
    }
    // And no `will-change: transform` — that promotes a layer whose pixels are
    // then stretched, which is the blurry path again.
    expect(css).not.toContain('will-change: transform')
  })

  it('marks the active state with a ring that does not affect layout', () => {
    // `outline` takes no part in layout, so entering/leaving the active state
    // cannot shift the document. A border would.
    const activeRule = css.match(/\.image-block-wrapper\.is-zooming\s*\{([\s\S]*?)\}/)
    expect(activeRule).toBeTruthy()
    expect(activeRule![1]).toContain('outline')
    expect(activeRule![1]).not.toMatch(/(^|[\s;])border\s*:/)
  })

  it('reveals the reset button only in the active state', () => {
    // Hidden by the base rule (also covers touch, where inline zoom never
    // installs) and shown only while `.is-zooming`.
    expect(css).toMatch(/\.image-block-wrapper \.image-block-reset-btn\s*\{[\s\S]*?display:\s*none/)
    expect(css).toMatch(/\.image-block-wrapper\.is-zooming \.image-block-reset-btn\s*\{[\s\S]*?display:\s*inline-flex/)
  })

  it('keeps the chat bubble thumbnail cap for non-diagram media', () => {
    // Chat never stamps .svg-fit, and the 200px cap must stay for images and
    // bare inline SVGs so a change to the figure rules cannot blow the bubble
    // out.
    expect(css).toMatch(/\.chat-message \.image-block-wrapper\s*\{[\s\S]*?max-width:\s*200px/)
    expect(css).toMatch(
      /\.chat-message \.image-block-wrapper \.lightbox-svg-wrap svg\.lightbox-svg\s*\{[\s\S]*?max-width:\s*200px/,
    )
  })
})
