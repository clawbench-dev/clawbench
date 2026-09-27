import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The failed-image placeholder is only half a fix unless the ORIGINAL <img> is
 * actually hidden.
 *
 * localMediaFallback.ts keeps the broken <img> in the DOM (so the fallback is
 * reversible when the file later appears) and stamps it `local-media-hidden`.
 * That class used to be hidden by a single descendant rule:
 *
 *   .image-block-wrapper .lightbox-img-wrap img.local-media-hidden { display: none }
 *
 * which only matches the rendered-markdown FIGURE. Every other surface that
 * reuses the same fallback — the full-screen image viewer, the media preview
 * card, the share SPA, a wallpaper — was left with the browser's broken-image
 * glyph (and its alt text) sitting next to the placeholder.
 *
 * The contract: the hide must be ancestor-INDEPENDENT. Because a bare
 * `img.local-media-hidden` (0-1-1) cannot outrank the figure's own
 * `.image-block-wrapper .lightbox-img-wrap img` (0-2-1), the rule must be
 * `!important` — a specificity tie would silently bring the glyph back.
 *
 * jsdom has no cascade, so this is a source-contract check (same pattern as
 * MarkdownPreviewSvgFit.css.test.ts / chatInlineImageSizing.css.test.ts).
 */

const stripComments = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, '')

const css = stripComments(readWebFile('css/media-block.css'))

interface Rule {
  selector: string
  decls: string
}

/** Every declaration block, with each selector of a comma list reported alone. */
function rulesOf(source: string): Rule[] {
  const out: Rule[] = []
  for (const m of source.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    for (const selector of m[1].split(',').map((s) => s.replace(/\s+/g, ' ').trim())) {
      if (selector) out.push({ selector, decls: m[2] })
    }
  }
  return out
}

/** Rules that hide a failed-media element. */
function hideRules(): Rule[] {
  return rulesOf(css).filter(
    (r) => /\.local-media-hidden\b/.test(r.selector) && /display\s*:\s*none/.test(r.decls),
  )
}

describe('failed-media hide rule (media-block.css contract)', () => {
  it('hides the broken <img> with a rule that needs no particular ancestor', () => {
    const rules = hideRules()
    expect(rules.length, 'a hide rule for .local-media-hidden must exist').toBeGreaterThan(0)

    // A descendant combinator (`A B`) means the rule only fires inside A. The
    // fallback is used on surfaces with no figure at all, so at least one hide
    // rule must be a plain, context-free selector.
    const global = rules.filter((r) => !/\s/.test(r.selector))
    expect(
      global.map((r) => r.selector),
      'the hide must not require an ancestor (e.g. .image-block-wrapper …)',
    ).not.toEqual([])
  })

  it('uses !important so no higher-specificity display rule can resurrect the glyph', () => {
    // `.image-block-wrapper .lightbox-img-wrap img` sets display: inline-block
    // at specificity 0-2-1, which outranks a bare `img.local-media-hidden`
    // (0-1-1). Without !important the figure keeps its broken image.
    const important = hideRules().filter((r) => /display\s*:\s*none\s*!important/.test(r.decls))
    expect(
      important.map((r) => r.selector),
      'a context-free hide rule must use display: none !important',
    ).not.toEqual([])
  })

  it('still covers the rendered-markdown figure', () => {
    // The figure case is the one the class was introduced for; the generalized
    // rule must keep hiding it (either via the global rule or a scoped one).
    const rules = hideRules()
    expect(rules.length).toBeGreaterThan(0)
  })
})
