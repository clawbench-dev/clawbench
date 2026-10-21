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
  /** The rule that sizes SVG media (inline svg and mermaid). Raster <img> has
   *  its own rule now — it has no `--svg-ar` to size from. */
  const mediaRule = css.match(
    /\.image-block-wrapper:has\(> \.lightbox-svg-wrap > svg\.lightbox-svg\) > \.lightbox-svg-wrap > svg\.lightbox-svg,[\s\S]*?\{([\s\S]*?)\}/,
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

  it('sizes the frame to the artwork at rest, and the viewport when active', () => {
    // Resting: the frame hugs the artwork. The media stays in NORMAL FLOW so it
    // can give the cell a width — an absolutely positioned child cannot, which
    // is why the frame used to be forced to `width: 100%` for every diagram
    // regardless of size.
    // The selector is the LAST in a comma-separated list, so anchor on the
    // opening brace rather than the line start.
    const figureRule = css.match(/\.image-block-wrapper:has\(> \.mermaid > svg\)\s*\{([\s\S]*?)\}/)
    expect(figureRule).toBeTruthy()
    expect(figureRule![1]).toContain('width: fit-content')
    expect(figureRule![1]).toContain('max-width: 100%')

    // Active: full column, so there is somewhere to pan to.
    expect(css).toMatch(/\.image-block-wrapper\.is-zooming:has\(> \.mermaid > svg\)\s*\{[\s\S]*?width:\s*100%/)

    // Resting media: in flow, shrink-to-fit, capped.
    expect(mediaRule).toBeTruthy()
    const rest = mediaRule![1]
    expect(rest).toContain('max-width: 100%')
    expect(rest).toContain('max-height: 60dvh')
    expect(rest.replace(/\/\*[\s\S]*?\*\//g, '')).not.toContain('position: absolute')

    // Active media: out of flow, so a zoomed artwork overflows without resizing
    // the frame.
    expect(css).toMatch(
      /\.image-block-wrapper\.is-zooming > \.mermaid > svg\s*\{[\s\S]*?position:\s*absolute/,
    )
  })

  it('sizes the media from the ratio, never from a percentage of the cell', () => {
    // The figure is shrink-to-fit, so any percentage width/max-width on the
    // media would be relative to the figure — which is itself sized by the
    // media. The browser breaks that cycle with the default 300px
    // replaced-element width, which squashed a 1156px diagram to 300px.
    //
    // The width must therefore be derived from the artwork's own ratio
    // (`--svg-ar`) or written as a resolved pixel value by `svgMediaFit.ts`.
    const declarations = css.replace(/\/\*[\s\S]*?\*\//g, '')
    expect(declarations).toContain('60dvh * var(--svg-ar')
    // mermaid states its natural size inline; only !important can override it.
    expect(declarations).toContain('max-width: none !important')
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

  it('caps a tall diagram at 60dvh at rest and fixes the frame when active', () => {
    // At rest the cell height comes from the in-flow artwork, capped by
    // max-height; a wide-and-flat diagram therefore gets a short frame instead
    // of being padded into a 60dvh box.
    expect(cellRule).toBeTruthy()
    const rest = cellRule![1]
    expect(rest).toContain('height: auto')
    expect(rest).toContain('max-height: 60dvh')
    expect(rest.replace(/\/\*[\s\S]*?\*\//g, '')).not.toMatch(/^\s*height:\s*60dvh/m)

    // Active: a fixed 60dvh viewport so panning has a stable frame.
    // The selector must mirror the resting `:has()`: `:has()` contributes its
    // argument's specificity, so a plain `.is-zooming > .mermaid` (0-3-0) loses
    // to the resting `:has(> .mermaid > svg)` (0-3-1).
    const active = css.match(
      /\.image-block-wrapper\.is-zooming:has\(> \.mermaid > svg\) > \.mermaid\s*\{([\s\S]*?)\}/,
    )
    expect(active, 'active rule must match the resting :has() specificity').toBeTruthy()
    expect(active![1]).toContain('height: 60dvh')
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

  it('sizes a raster <img> from its own intrinsic width, not the svg ratio', () => {
    // An <img> has no stamped `--svg-ar`, so the svg rule's
    // `calc(60dvh * var(--svg-ar, 1))` would resolve to `60dvh * 1` and put
    // every photo in a fixed 492px-wide box — while the shrink-to-fit figure
    // stretched to the column around it. The img needs its own `width: auto`.
    const imgRule = css.match(
      /\.image-block-wrapper:has\(> \.lightbox-img-wrap > img\) > \.lightbox-img-wrap > img\s*\{([\s\S]*?)\}/,
    )
    expect(imgRule).toBeTruthy()
    const decl = imgRule![1].replace(/\/\*[\s\S]*?\*\//g, '')
    expect(decl).toContain('width: auto')
    expect(decl).toContain('max-width: 100%')
    expect(decl).toContain('max-height: 60dvh')
    expect(decl).not.toContain('--svg-ar')
  })

  it('lifts the chat thumbnail cap while zoomed', () => {
    // A bubble caps media at 200px. Left unscoped, that cap survives activation
    // and the "zoom" happens inside a 200px box — the diagram stays tiny however
    // far it is zoomed (reported: 聊天界面点击聚焦后没有全宽).
    const chatRules = css.match(/^\s*\.chat-message[^{]*\{[^}]*\}/gm) || []
    expect(chatRules.length).toBeGreaterThan(0)
    for (const rule of chatRules) {
      if (!/max-(width|height):\s*200px/.test(rule)) continue
      // Every size cap must be rest-only.
      expect(rule, `chat cap must be scoped to :not(.is-zooming):\n${rule}`)
        .toContain(':not(.is-zooming)')
    }
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
    // The cap is rest-only (`:not(.is-zooming)`) so activation can span the
    // bubble; the selectors below therefore include the pseudo-class.
    expect(css).toMatch(
      /\.chat-message \.image-block-wrapper:not\(\.is-zooming\)[^{]*\{[\s\S]*?max-width:\s*200px/,
    )
    expect(css).toMatch(
      /\.chat-message \.image-block-wrapper:not\(\.is-zooming\) \.lightbox-svg-wrap svg\.lightbox-svg\s*\{[\s\S]*?max-width:\s*200px/,
    )
  })

  it('the chat cap outranks the generic :has() rule', () => {
    // Regression: the generic inline-zoom block sets
    //   `.image-block-wrapper:has(> .mermaid > svg) { max-width: 100% }`
    // which is 0-2-1, while the plain `.chat-message .image-block-wrapper`
    // cap is only 0-2-0 — the generic rule won and every diagram inside a
    // bubble grew to the full column (measured 540px in a 600px bubble).
    // A mere "the chat rule exists" assertion cannot catch that, so this pins
    // the chat selectors to the SAME `:has()` shape.
    //
    // Comments are stripped first: the file's own doc comments quote the old
    // `.chat-message .image-block-wrapper { max-width: 200px }` shape, and a
    // naive match found that instead of the real rule.
    const declarations = css.replace(/\/\*[\s\S]*?\*\//g, '')
    const chatCap = declarations.match(
      /\.chat-message \.image-block-wrapper:not\(\.is-zooming\)[^{]*\{[^}]*max-width:\s*200px/,
    )
    expect(chatCap, 'chat cap rule must exist').toBeTruthy()
    const selectors = chatCap![0].split('{')[0]
    expect(selectors, 'chat cap must include the mermaid :has() variant').toMatch(
      /\.chat-message \.image-block-wrapper:not\(\.is-zooming\):has\(> \.mermaid > svg\)/,
    )
    expect(selectors, 'chat cap must include the img :has() variant').toMatch(
      /\.chat-message \.image-block-wrapper:not\(\.is-zooming\):has\(> \.lightbox-img-wrap > img\)/,
    )
  })

  it('the chat media cap overrides the generic ratio width', () => {
    // The generic rule clears max-width with `!important` (to beat mermaid's
    // inline `max-width: Npx`), so a non-important chat cap would simply lose
    // and the svg would still be as wide as the ratio allows.
    const rule = css.match(
      /\.chat-message \.image-block-wrapper:not\(\.is-zooming\):has\(> \.mermaid > svg\) > \.mermaid > svg\s*\{([\s\S]*?)\}/,
    )
    expect(rule, 'chat media cap rule must exist').toBeTruthy()
    const body = rule![1].replace(/\/\*[\s\S]*?\*\//g, '')
    expect(body, 'must be !important to beat the generic max-width: none').toMatch(
      /max-width:\s*200px\s*!important/,
    )
  })
})
