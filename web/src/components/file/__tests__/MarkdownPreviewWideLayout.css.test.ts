import { describe, expect, it } from 'vitest'
import { readFileSync } from 'fs'
import { resolve } from 'path'

/**
 * Layout contract for the markdown file preview reading column.
 *
 * On wide screens the preview used to center the content with
 * `max-width: min(900px, 100%)` + `margin: 0 auto` ON the `.markdown-body`
 * element. Since that element is ALSO the scroll container (overflow-y: auto,
 * see css/content.css), shrinking it dragged the scrollbar in from the right
 * edge. The fix keeps the scroll container full-width (scrollbar hugs the
 * window edge) and produces the same centered 900px reading column with
 * symmetric horizontal padding instead:
 *
 *   padding: 10px calc(10px + max(0px, (100% - 900px) / 2));
 *
 * These tests are source-contract checks (jsdom cannot evaluate CSS `max()`
 * inside `calc()`), guarding against regressions that would re-center the
 * preview by re-shrinking the scroll element or silently drop the gutter on
 * wide screens.
 */
describe('markdown preview wide-screen layout (scrollbar flush, padding cap)', () => {
  const contentCss = readFileSync(
    resolve(__dirname, '../../../../css/content.css'),
    'utf8',
  )
  const previewVue = readFileSync(
    resolve(__dirname, '../../file/MarkdownPreview.vue'),
    'utf8',
  )

  it('keeps the preview scroll container full-width (no element shrink)', () => {
    // The preview override must neutralize the base max-width/margin centering,
    // otherwise the element (and its scrollbar) re-centers on wide screens.
    const bodyRule = contentCss.match(/\.markdown-preview \.markdown-body\s*\{[\s\S]*?\}/)
    expect(bodyRule).toBeTruthy()
    expect(bodyRule![0]).toContain('max-width: none')
    expect(bodyRule![0]).toContain('margin: 0')
  })

  it('caps the reading column with symmetric horizontal padding on the same element', () => {
    // The 900px column cap must come from horizontal padding (box-sizing is
    // border-box), never from a max-width/margin combo on the scroll element.
    expect(contentCss).toMatch(
      /\.markdown-preview \.markdown-body\s*\{[\s\S]*?padding:\s*10px calc\(10px \+ max\(0px,\s*\(100% - 900px\) \/ 2\)\);/,
    )
  })

  it('positions diff markers at the reading column right edge', () => {
    // Markers are absolutely positioned children of the full-width scroll
    // container. With the old max-width layout a marker at right:0 sat at the
    // element border (the column edge). Now that the container is full-width
    // and the column is inset by padding, the marker must be inset by the same
    // half-slack (max(0px,(100%−900px)/2)) to keep hugging the text.
    const markerRule = previewVue.match(
      /\.markdown-preview \.markdown-body \.diff-marker-inline\s*\{[\s\S]*?\}/,
    )
    expect(markerRule).toBeTruthy()
    expect(markerRule![0]).toContain('right: max(0px, (100% - 900px) / 2)')
  })

  it('does not leak the full-width behavior outside the file preview', () => {
    // The base .markdown-body keeps its original centered layout; only the
    // .markdown-preview context (the file/share markdown preview) becomes a
    // full-width scroll container with the padding cap. Other .markdown-body
    // users (inline summaries, task prompt cards, session-search chunks) are
    // untouched.
    const baseRule = contentCss.match(/^\.markdown-body\s*\{[\s\S]*?\}/m)
    expect(baseRule).toBeTruthy()
    expect(baseRule![0]).toContain('max-width: min(900px, 100%)')
    expect(baseRule![0]).toContain('margin: 0 auto')
  })
})
