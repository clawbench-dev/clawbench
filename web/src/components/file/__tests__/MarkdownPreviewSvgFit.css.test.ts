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

  it('FILLS the viewport and contain-fits the artwork inside it', () => {
    expect(mediaRule).toBeTruthy()
    const rule = mediaRule![1]
    // The element must be the viewport size. `width: auto` + `max-width: 100%`
    // only ever shrank (contain does not scale a small image UP), so a diagram
    // whose intrinsic size was smaller than the column sat in the middle of an
    // empty frame — the reported bug ("默认大小应该占满全宽").
    expect(rule).toContain('width: 100%')
    expect(rule).toContain('height: 100%')
    expect(rule).toContain('object-fit: contain')
    // Centring: the percentage translate resolves against the element's own
    // box, so it works for a box both smaller (resting) and larger (zoomed)
    // than the frame.
    expect(rule).toContain('top: 50%')
    expect(rule).toContain('left: 50%')
    expect(rule).toContain('translate(-50%, -50%)')
    const declarations = rule.replace(/\/\*[\s\S]*?\*\//g, '')
    // `inset: 0` would pin all four edges (that part is fine) but the earlier
    // form paired it with `margin: auto`, which forced the box to the container
    // size and broke the ratio. The explicit 100% + translate form is the
    // contract.
    expect(declarations).not.toContain('inset: 0')
    // The old shrink-only sizing must not come back.
    expect(declarations).not.toMatch(/^\s*width:\s*auto/m)
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

  it('hugs the artwork height at rest and switches to 60dvh when active', () => {
    // A wide-and-flat diagram must not be padded into a 60dvh box: at rest the
    // frame takes the artwork's own ratio (written as `--media-ar` by
    // svgMediaFit.ts) and is only capped by max-height. Activation switches to
    // a fixed 60dvh viewport so panning has a stable frame.
    expect(cellRule).toBeTruthy()
    const rest = cellRule![1]
    expect(rest).toContain('aspect-ratio: var(--media-ar, auto)')
    expect(rest).toContain('max-height: 60dvh')
    // A fixed height at rest is the bug this replaced.
    expect(rest.replace(/\/\*[\s\S]*?\*\//g, '')).not.toMatch(/^\s*height:\s*60dvh/m)

    // The active rule must mirror the resting `:has()` in its selector:
    // `:has()` contributes its argument's specificity, so a plain
    // `.is-zooming > .mermaid` (0-3-0) loses to the resting
    // `:has(> .mermaid > svg)` (0-3-1) and the frame stays artwork-shaped.
    const active = css.match(
      /\.image-block-wrapper\.is-zooming:has\(> \.mermaid > svg\) > \.mermaid\s*\{([\s\S]*?)\}/,
    )
    expect(active, 'active rule must match the resting :has() specificity').toBeTruthy()
    expect(active![1]).toContain('height: 60dvh')
    expect(active![1]).toContain('aspect-ratio: auto')
  })

  it('does not cap the zoom scale at the fit width', () => {
    // "放大到全宽就无法再放大": the ceiling used to be 8x, reached almost
    // immediately after the artwork filled the frame.
    const src = readWebFile('src/utils/inlineZoom.ts')
    expect(src).not.toMatch(/MAX_SCALE\s*=\s*\d/)
    // A runaway guard only, far past any usable zoom.
    expect(src).toMatch(/MAX_SAFE_SCALE\s*=\s*\d+/)
    const cap = Number(src.match(/MAX_SAFE_SCALE\s*=\s*(\d+)/)![1])
    expect(cap).toBeGreaterThanOrEqual(32)
  })

  it('reveals the reset button only in the active state', () => {
    // Hidden by the base rule (also covers touch, where inline zoom never
    // installs) and shown only while `.is-zooming`.
    expect(css).toMatch(/\.image-block-wrapper \.image-block-reset-btn\s*\{[\s\S]*?display:\s*none/)
    expect(css).toMatch(/\.image-block-wrapper\.is-zooming \.image-block-reset-btn\s*\{[\s\S]*?display:\s*inline-flex/)
  })

  it('draws the figure and its toolbar with square corners', () => {
    // The figure is a media region, not a rounded card. `overflow: hidden` means
    // a radius on the frame would also round the artwork's own corners.
    const shell = css.match(/^\.image-block-wrapper\s*\{([\s\S]*?)\}/m)
    expect(shell).toBeTruthy()
    expect(shell![1]).toContain('border-radius: 0')
    expect(shell![1]).not.toContain('--radius-sm')

    const toolbar = css.match(/^\.image-block-header\s*\{([\s\S]*?)\}/m)
    expect(toolbar).toBeTruthy()
    expect(toolbar![1]).toContain('border-radius: 0')
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
