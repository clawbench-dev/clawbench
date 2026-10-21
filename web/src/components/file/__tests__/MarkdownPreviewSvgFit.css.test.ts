import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Layout contract for proportional fill-width sizing of SVG media in rendered
 * markdown (file preview, share SPA, HTML export).
 *
 * An `.image-block-wrapper` is `width: fit-content`, so SVG media used to
 * render at its intrinsic size and leave the reading column mostly empty. The
 * fix makes SVG figures fill the column, with the 60dvh cap applied to the
 * content CELL as a scrolling height budget:
 *
 *   figure: width 100%; cell: max-height 60dvh + overflow-y auto
 *
 * Three invariants are load-bearing and easy to regress silently:
 *  1. The figure width must be a plain `100%`, NOT a ratio-derived
 *     `min(100%, 60dvh × ratio)`. The ratio form silently shrinks portrait
 *     media — a 302×1182 flowchart (ratio 0.255) came out 107px wide inside an
 *     880px column, a third of its authored size and unreadable (issue #570).
 *  2. The height cap must live on the content cell with `overflow-y: auto`.
 *     Putting `max-height` on the media itself clips the diagram instead of
 *     letting the reader scroll it.
 *  3. The opt-in must be scoped to `.svg-fit`. If the rules applied to every
 *     figure, raster images would be resized too, and the chat bubble's own
 *     200px thumbnail cap would be overridden.
 *
 * These are source-contract checks: jsdom cannot evaluate `dvh` units, and
 * cannot resolve an aspect ratio at all.
 */
describe('svg media fill-width sizing (media-block.css contract)', () => {
  const css = readWebFile('css/media-block.css')

  const figureRule = css.match(/\.image-block-wrapper\.svg-fit\s*\{[\s\S]*?\}/)
  const contentRule = css.match(
    /\.image-block-wrapper\.svg-fit\s*>\s*\.lightbox-img-wrap\s*>\s*img,[\s\S]*?\{[\s\S]*?\}/,
  )

  it('fills the column instead of deriving the width from the aspect ratio', () => {
    expect(figureRule).toBeTruthy()
    expect(figureRule![0]).toContain('width: 100%')
    // Regression guard for issue #570: the ratio-derived width shrank portrait
    // diagrams to a fraction of their authored size.
    expect(figureRule![0]).not.toContain('--svg-ar')
    expect(figureRule![0]).not.toContain('60dvh')
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

  it('releases the generic markdown img height cap on SVG-file media', () => {
    // markdown-common.css caps EVERY markdown `<img>` at `max-height: 60dvh`
    // with `object-fit: contain`. On a filled-width SVG that clamps the box
    // while `contain` letterboxes the PAINTED picture inside it — a 302×1182
    // SVG got an 880×492 box painting a 302×492 image, i.e. exactly as
    // unreadable as before the fix, now inside a mostly-empty frame.
    //
    // The media must therefore opt out of that cap; the CELL's own
    // max-height + overflow-y is what bounds it. Asserted against the real
    // source of the competing rule, not just media-block.css, so the two files
    // cannot drift apart silently.
    const common = readWebFile('css/markdown-common.css')
    const genericImg = common.match(/\.markdown-body img,[\s\S]*?\{[\s\S]*?\}/)
    expect(genericImg).toBeTruthy()
    expect(genericImg![0]).toContain('max-height: 60dvh')
    expect(genericImg![0]).toContain('object-fit: contain')

    const releaseBlock = [...css.matchAll(
      /\.image-block-wrapper\.svg-fit\s*>\s*\.lightbox-img-wrap\s*>\s*img,[\s\S]*?\{([\s\S]*?)\}/g,
    )].map(m => m[1]).find(body => body.includes('max-height'))
    expect(releaseBlock, 'svg-fit media must opt out of the generic max-height').toBeTruthy()
    expect(releaseBlock).toContain('max-height: none')
    // Guard against a future edit that "restores" the cap with a value instead
    // of removing it.
    expect(releaseBlock).not.toMatch(/max-height:\s*\d/)

    // Specificity ties with the generic rule (both 0-3-1 for `img.chat-img`),
    // so the opt-out only wins because media-block.css loads AFTER
    // markdown-common.css. index.html owns that order.
    const html = readWebFile('index.html')
    const commonAt = html.indexOf('/css/markdown-common.css')
    const mediaAt = html.indexOf('/css/media-block.css')
    expect(commonAt).toBeGreaterThan(-1)
    expect(mediaAt).toBeGreaterThan(commonAt)
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

  it('scrolls tall media inside the cell instead of clipping the diagram', () => {
    // The 60dvh budget must land on the CONTENT CELL as max-height +
    // overflow-y:auto. `max-height` on the media itself would clip the diagram
    // (the reader could not reach the rest of it), which is why the cap lives
    // one level up.
    const cellRule = css.match(
      /\.image-block-wrapper\.svg-fit\s*>\s*\.lightbox-img-wrap,[\s\S]*?\{[\s\S]*?\}/,
    )
    expect(cellRule).toBeTruthy()
    expect(cellRule![0]).toContain('max-height: 60dvh')
    expect(cellRule![0]).toContain('overflow-y: auto')
    // Every content-cell shape is covered — raster-SVG cell, bare inline svg
    // cell and the mermaid cell.
    expect(cellRule![0]).toContain('.lightbox-img-wrap')
    expect(cellRule![0]).toContain('.lightbox-svg-wrap')
    expect(cellRule![0]).toContain('.mermaid')
  })

  it('never reintroduces the ratio-derived width anywhere in the sheet', () => {
    // Issue #570 regression guard. `min(100%, 60dvh × ratio)` is only correct
    // for landscape media; it collapsed portrait diagrams to a fraction of
    // their authored width. The height budget must stay a cell `max-height`,
    // never a width formula. (A plain `width: 100%` elsewhere — e.g. the table
    // row modal's thumbnails — is unrelated and allowed.)
    expect(css).not.toContain('60dvh * var(--svg-ar')
    expect(css).not.toContain('--svg-ar')

    // The fill-width opt-in itself stays scoped to the marker class.
    const figureRule = css.match(/\.image-block-wrapper\.svg-fit\s*\{[\s\S]*?\}/)
    expect(figureRule![0]).toContain('width: 100%')
  })

  it('keeps the chat bubble thumbnail cap for non-diagram media', () => {
    // Chat never stamps .svg-fit, and the 200px cap must stay for images and
    // bare inline SVGs so a change to the figure rules cannot blow the bubble
    // out. Mermaid is deliberately exempt (see the next test).
    expect(css).toMatch(/\.chat-message \.image-block-wrapper\s*\{[\s\S]*?max-width:\s*200px/)
    expect(css).toMatch(
      /\.chat-message \.image-block-wrapper \.lightbox-svg-wrap svg\.lightbox-svg\s*\{[\s\S]*?max-width:\s*200px/,
    )
  })

  it('lets a chat mermaid diagram fill the bubble (issue #570)', () => {
    // The 200px thumbnail cap is right for a photo but wrong for a diagram:
    // combined with the height cap it drove a 302×1182 flowchart down to 47px
    // wide. The diagram must fill the bubble and scroll vertically instead.
    const override = css.match(/\.chat-message \.image-block-wrapper:has\(> \.mermaid\)\s*\{[\s\S]*?\}/)
    expect(override).toBeTruthy()
    expect(override![0]).toContain('max-width: 100%')
    expect(override![0]).toContain('width: 100%')

    const cell = css.match(/\.chat-message \.image-block-wrapper > \.mermaid\s*\{[\s\S]*?\}/)
    expect(cell).toBeTruthy()
    // A smaller budget than the 60dvh preview cap: a nested scroller that tall
    // would swallow swipes meant to scroll the conversation.
    expect(cell![0]).toContain('max-height: 45dvh')
    expect(cell![0]).toContain('overflow-y: auto')

    // Mermaid's inline `style="max-width: Npx"` still needs the override.
    const svg = css.match(/\.chat-message \.image-block-wrapper > \.mermaid svg\s*\{[\s\S]*?\}/)
    expect(svg).toBeTruthy()
    expect(svg![0]).toContain('max-width: 100% !important')
  })

  it('also stretches a mermaid diagram in a USER bubble', () => {
    // `.chat-message.user` is `align-items: flex-end`, so its `.msg-card` is
    // shrink-to-fit. A child's `width: 100%` then resolves against an
    // indefinite width and falls back to the media's intrinsic size — measured
    // 300px in a user bubble vs 760px in an assistant one for the same
    // diagram. The card must be stretched before the child percentage can
    // resolve.
    //
    // Source-contract only. The SELECTOR is exercised against the real DOM
    // tree in `chatMermaidFillWidth.dom.test.ts`: the figure sits four levels
    // below `.msg-card` (`.msg-content-wrapper > .content-blocks > div >
    // .image-block-wrapper`), so a `:has(> …)` child combinator here would
    // parse fine, match nothing, and pass this test while leaving the bug.
    const stretch = css.match(
      /\.chat-message\.user \.msg-card:has\(\.image-block-wrapper > \.mermaid\)\s*\{[\s\S]*?\}/,
    )
    expect(stretch).toBeTruthy()
    expect(stretch![0]).toContain('align-self: stretch')
    // Must not require the figure to be a direct child of the card.
    expect(stretch![0]).not.toContain(':has(>')
  })
})
