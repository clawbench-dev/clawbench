import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Layout contract for proportional fill-width sizing of SVG media in rendered
 * markdown (file preview, share SPA, HTML export).
 *
 * An `.image-block-wrapper` is `width: fit-content`, so SVG media used to
 * render at its intrinsic size and leave the reading column mostly empty. The
 * fix gives SVG figures a width driven by the media's aspect ratio:
 *
 *   width = min(100%, 60dvh × var(--svg-ar))  →  height ≤ 60dvh, ratio kept
 *
 * Two invariants are load-bearing and easy to regress silently:
 *  1. The figure width must be the RATIO-DRIVEN `min(100%, …)` expression. A
 *     plain `width: 100%` combined with `max-height` would let the browser
 *     clamp the height without shrinking the width, stretching the media.
 *  2. The opt-in must be scoped to `.svg-fit`. If the rules applied to every
 *     figure, raster images and mermaid diagrams would be resized too, and the
 *     chat bubble's own 200px thumbnail cap would be overridden.
 *
 * These are source-contract checks: jsdom cannot evaluate `min()`/`calc()` with
 * `dvh`, and cannot resolve an aspect ratio at all.
 */
describe('svg media fill-width sizing (media-block.css contract)', () => {
  const css = readWebFile('css/media-block.css')

  const figureRule = css.match(/\.image-block-wrapper\.svg-fit\s*\{[\s\S]*?\}/)
  const contentRule = css.match(
    /\.image-block-wrapper\.svg-fit\s*>\s*\.lightbox-img-wrap\s*>\s*img,[\s\S]*?\{[\s\S]*?\}/,
  )

  it('drives the figure width from the aspect ratio, capped at 60dvh', () => {
    expect(figureRule).toBeTruthy()
    expect(figureRule![0]).toContain('min(100%, calc(60dvh * var(--svg-ar, 1)))')
  })

  it('stretches the content media to the figure width with height:auto', () => {
    expect(contentRule).toBeTruthy()
    expect(contentRule![0]).toContain('width: 100%')
    expect(contentRule![0]).toContain('height: auto')
    // Both content cell shapes are covered — an <img> pointing at a .svg file
    // and a bare inline <svg>.
    expect(contentRule![0]).toContain('.lightbox-img-wrap > img')
    expect(contentRule![0]).toContain('.lightbox-svg-wrap > svg.lightbox-svg')
  })

  it('lets a mermaid diagram fill the figure despite its inline max-width', () => {
    // Mermaid writes its natural size as an INLINE `style="max-width: Npx"`.
    // An inline declaration outranks any stylesheet rule, so the override needs
    // !important — without it a wide flowchart stays squashed at its natural
    // width instead of growing to the column.
    const mermaidRule = css.match(/\.image-block-wrapper\.svg-fit\s*>\s*\.mermaid\s*>\s*svg\s*\{[\s\S]*?\}/)
    expect(mermaidRule).toBeTruthy()
    expect(mermaidRule![0]).toContain('width: 100%')
    expect(mermaidRule![0]).toContain('height: auto')
    expect(mermaidRule![0]).toContain('max-width: 100% !important')
  })

  it('does not cap mermaid height by clipping (the ratio-driven width caps it)', () => {
    // Height must come from the figure's ratio-driven width, not max-height:
    // clipping would cut the diagram off, and a bare max-height on a
    // width:100% child would distort it.
    const mermaidRule = css.match(/\.image-block-wrapper\.svg-fit\s*>\s*\.mermaid\s*>\s*svg\s*\{[\s\S]*?\}/)
    expect(mermaidRule![0]).not.toContain('max-height')
  })

  it('never applies the ratio-driven width to an unstamped figure', () => {
    // The only width rules touching .image-block-wrapper are the base
    // fit-content and the .svg-fit opt-in — nothing may widen every figure.
    const widthRules = [...css.matchAll(/(^|\n)([^\n{}]*\.image-block-wrapper[^\n{}]*)\{([\s\S]*?)\}/g)]
      .filter(m => /(^|;)\s*width\s*:/.test(m[3]))
      .map(m => ({ selector: m[2].trim(), body: m[3] }))
    expect(widthRules.length).toBeGreaterThan(0)
    for (const rule of widthRules) {
      if (rule.body.includes('min(100%, calc(60dvh')) {
        expect(rule.selector, `ratio width must be .svg-fit-scoped: ${rule.selector}`).toContain('.svg-fit')
      }
    }
  })

  it('keeps the chat bubble thumbnail cap (svg media in chat is not enlarged)', () => {
    // Chat never stamps .svg-fit, but the cap must remain so a future change to
    // the figure rules cannot blow the bubble out.
    expect(css).toMatch(/\.chat-message \.image-block-wrapper\s*\{[\s\S]*?max-width:\s*200px/)
  })
})
